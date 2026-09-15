// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package supervisor

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/agentrq/agentrq/daemon/wire"
)

// writes records what reached the terminal.
func (f *fakePTY) writes() []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]byte(nil), f.written...)
}

func startedSession(t *testing.T) (*Supervisor, *recordingStarter) {
	t.Helper()
	st := &recordingStarter{}
	s := New(st.start, 0, 0)
	if _, err := s.Start(t.Context(), "work", claudeRequest(t, 1)); err != nil {
		t.Fatalf("Start: %v", err)
	}
	return s, st
}

func inputFrame(t *testing.T, id uint64, b []byte) wire.Frame {
	t.Helper()
	f, err := wire.SessionFrame(wire.TypeInput, id, b)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// Esc is 0x1b and gets no special handling — that is the point. Any layer that
// enumerated "special keys" would already be wrong for the next one.
func TestInputReachesTheTerminalUntouched(t *testing.T) {
	s, st := startedSession(t)
	_, p := st.last()

	inputs := map[string][]byte{
		"escape":          {0x1b},
		"escape then key": {0x1b, 'b'}, // Alt-b, or Esc then b
		"ctrl-c":          {0x03},
		"enter is CR":     {0x0d}, // what a terminal actually sends
		"arrow up":        {0x1b, '[', 'A'},
		"invalid utf8":    {0xff, 0xfe},
		"nul":             {0x00},
		"every byte":      allBytes(),
	}

	var want []byte
	for _, in := range []string{"escape", "escape then key", "ctrl-c", "enter is CR", "arrow up", "invalid utf8", "nul", "every byte"} {
		payload := inputs[in]
		if err := s.HandleFrame(inputFrame(t, 1, payload)); err != nil {
			t.Fatalf("%s: HandleFrame: %v", in, err)
		}
		want = append(want, payload...)
	}

	if got := p.writes(); !bytes.Equal(got, want) {
		t.Errorf("the terminal received different bytes:\n got %#v\nwant %#v", got, want)
	}
}

func allBytes() []byte {
	b := make([]byte, 256)
	for i := range b {
		b[i] = byte(i)
	}
	return b
}

func TestResizeReachesTheTerminal(t *testing.T) {
	s, st := startedSession(t)
	_, p := st.last()

	body, err := json.Marshal(wire.Resize{Cols: 120, Rows: 40})
	if err != nil {
		t.Fatal(err)
	}
	f, err := wire.SessionFrame(wire.TypeResize, 1, body)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.HandleFrame(f); err != nil {
		t.Fatalf("HandleFrame: %v", err)
	}

	if c, r := p.size(); c != 120 || r != 40 {
		t.Errorf("terminal size = %dx%d, want 120x40", c, r)
	}
}

// A zero dimension is not a window, and passing it through would have the
// platform decide what it means.
func TestResizeRefusesANonWindow(t *testing.T) {
	s, _ := startedSession(t)
	for _, size := range []wire.Resize{{Cols: 0, Rows: 40}, {Cols: 120, Rows: 0}} {
		body, err := json.Marshal(size)
		if err != nil {
			t.Fatal(err)
		}
		f, err := wire.SessionFrame(wire.TypeResize, 1, body)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.HandleFrame(f); err == nil {
			t.Errorf("HandleFrame accepted %+v", size)
		}
	}
}

// Output, replay and exit travel the other way. A backend sending one has
// misunderstood the protocol, and that is worth refusing rather than ignoring.
func TestTheDaemonRefusesFramesThatTravelTheOtherWay(t *testing.T) {
	s, _ := startedSession(t)
	for _, typ := range []wire.Type{wire.TypeOutput, wire.TypeReplay, wire.TypeExit} {
		f, err := wire.SessionFrame(typ, 1, []byte("x"))
		if err != nil {
			t.Fatal(err)
		}
		if err := s.HandleFrame(f); err == nil {
			t.Errorf("the daemon accepted a %s frame", typ)
		}
	}
}

// One terminal has closed; the other never existed. They mean different things
// to the person looking at the screen.
func TestInputToAFinishedSessionIsDistinguishedFromAnUnknownOne(t *testing.T) {
	s, st := startedSession(t)
	_, p := st.last()
	p.exit(0, nil)
	waitFor(t, func() bool {
		sess, _ := s.Get(1)
		state, _, _ := sess.State()
		return state.Terminal()
	}, "never finished")

	err := s.HandleFrame(inputFrame(t, 1, []byte("too late")))
	if !errors.Is(err, ErrSessionNotRunning) {
		t.Errorf("error = %v, want ErrSessionNotRunning", err)
	}

	err = s.HandleFrame(inputFrame(t, 999, []byte("x")))
	if !errors.Is(err, ErrNoSuchSession) {
		t.Errorf("error = %v, want ErrNoSuchSession", err)
	}
}

func TestEmptyInputIsHarmless(t *testing.T) {
	s, st := startedSession(t)
	_, p := st.last()
	f := wire.Frame{Type: wire.TypeInput, SessionID: 1}
	if err := s.HandleFrame(f); err != nil {
		t.Errorf("HandleFrame with no payload = %v", err)
	}
	if len(p.writes()) != 0 {
		t.Error("an empty input wrote to the terminal")
	}
}

func TestMalformedResizeIsAnError(t *testing.T) {
	s, _ := startedSession(t)
	f := wire.Frame{Type: wire.TypeResize, SessionID: 1, Payload: []byte("{not json")}
	if err := s.HandleFrame(f); err == nil {
		t.Error("HandleFrame accepted a malformed resize")
	}
}
