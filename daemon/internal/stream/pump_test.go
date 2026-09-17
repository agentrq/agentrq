// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package stream

import (
	"bytes"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agentrq/agentrq/daemon/wire"
)

type fakeSender struct {
	mu      sync.Mutex
	frames  []wire.Frame
	err     error
	failFor int // fail this many sends, then succeed
}

func (s *fakeSender) Send(f wire.Frame) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failFor > 0 {
		s.failFor--
		return ErrBackpressure
	}
	if s.err != nil {
		return s.err
	}
	s.frames = append(s.frames, f)
	return nil
}

func (s *fakeSender) all() []wire.Frame {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]wire.Frame(nil), s.frames...)
}

func (s *fakeSender) ofType(t wire.Type) []wire.Frame {
	var out []wire.Frame
	for _, f := range s.all() {
		if f.Type == t {
			out = append(out, f)
		}
	}
	return out
}

func newTestPump(t *testing.T) (*Pump, *fakeSender, *time.Time) {
	t.Helper()
	snd := &fakeSender{}
	now := t0
	p := NewPump(7, NewScreen(40, 10), snd)
	p.Now = func() time.Time { return now }
	return p, snd, &now
}

// The screen has to be correct the moment somebody attaches, and it cannot be
// rebuilt later from bytes nobody kept.
func TestTheScreenIsFedEvenWithNoViewer(t *testing.T) {
	p, snd, _ := newTestPump(t)

	if err := p.Feed([]byte("written while detached\r\n")); err != nil {
		t.Fatalf("Feed: %v", err)
	}
	if len(snd.all()) != 0 {
		t.Error("output was sent with nobody attached")
	}

	if err := p.Attach(); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	replays := snd.ofType(wire.TypeReplay)
	if len(replays) != 1 {
		t.Fatalf("attach produced %d replays, want 1", len(replays))
	}
	if !bytes.Contains(replays[0].Payload, []byte("written while detached")) {
		t.Error("the replay does not contain what was written while detached")
	}
}

func TestAttachSendsARedrawNotHistory(t *testing.T) {
	p, snd, _ := newTestPump(t)

	// A thousand redraws of one line.
	for i := 0; i < 1000; i++ {
		if err := p.Feed([]byte("\rworking...")); err != nil {
			t.Fatal(err)
		}
	}
	if err := p.Attach(); err != nil {
		t.Fatal(err)
	}

	replays := snd.ofType(wire.TypeReplay)
	if len(replays) != 1 {
		t.Fatalf("replays = %d", len(replays))
	}
	// One line's worth, not a thousand.
	if n := len(replays[0].Payload); n > 2048 {
		t.Errorf("replay is %d bytes for one redrawn line", n)
	}
	if strings.Count(string(replays[0].Payload), "working...") > 1 {
		t.Error("the replay contains the line more than once — it is history, not a screen")
	}
}

func TestOutputIsSentOnceTheWindowCloses(t *testing.T) {
	p, snd, now := newTestPump(t)
	if err := p.Attach(); err != nil {
		t.Fatal(err)
	}

	if err := p.Feed([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	if len(snd.ofType(wire.TypeOutput)) != 0 {
		t.Error("output was sent before the window closed")
	}

	*now = now.Add(Window)
	if err := p.Feed([]byte(" world")); err != nil {
		t.Fatal(err)
	}

	outs := snd.ofType(wire.TypeOutput)
	if len(outs) != 1 {
		t.Fatalf("output frames = %d, want 1", len(outs))
	}
	if got := string(outs[0].Payload); got != "hello world" {
		t.Errorf("payload = %q, want the whole batch", got)
	}
}

// The assertion the terminal feature rests on, through the pump this time.
func TestArbitraryBytesSurviveThePump(t *testing.T) {
	p, snd, now := newTestPump(t)
	if err := p.Attach(); err != nil {
		t.Fatal(err)
	}

	payload := []byte{0x1b, '[', '2', 'K', 0xff, 0xfe, 0x00, 'a'}
	if err := p.Feed(payload); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(Window)
	if err := p.Flush(); err != nil {
		t.Fatal(err)
	}

	outs := snd.ofType(wire.TypeOutput)
	if len(outs) != 1 {
		t.Fatalf("frames = %d", len(outs))
	}
	if !bytes.Equal(outs[0].Payload, payload) {
		t.Errorf("payload changed:\n got %#v\nwant %#v", outs[0].Payload, payload)
	}
}

// Dropping bytes without a redraw would leave the viewer's terminal
// permanently wrong. This is the only safe way to shed load.
func TestBackpressureResyncsRatherThanDropping(t *testing.T) {
	snd := &fakeSender{}
	now := t0
	p := NewPump(7, NewScreen(40, 10), snd)
	p.Now = func() time.Time { return now }

	if err := p.Attach(); err != nil {
		t.Fatal(err)
	}
	if err := p.Feed([]byte("some output\r\n")); err != nil {
		t.Fatal(err)
	}

	// The next send is refused.
	snd.mu.Lock()
	snd.failFor = 1
	snd.mu.Unlock()

	now = now.Add(Window)
	if err := p.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	if p.Resyncs() != 1 {
		t.Errorf("resyncs = %d, want 1", p.Resyncs())
	}
	// A redraw arrived in place of the dropped batch, so the viewer's screen
	// is correct rather than missing a chunk.
	replays := snd.ofType(wire.TypeReplay)
	if len(replays) < 2 { // one from Attach, one from the resync
		t.Fatalf("replays = %d, want a resync redraw", len(replays))
	}
	if !bytes.Contains(replays[len(replays)-1].Payload, []byte("some output")) {
		t.Error("the resync redraw does not show the current screen")
	}
	// And the loss is counted rather than hidden.
	if p.Elided() == 0 {
		t.Error("discarded bytes were not counted")
	}
}

// Even the redraw could not go. There is nothing further to try, and the
// screen stays correct here for the next attach.
func TestAFailedResyncDoesNotEndTheSession(t *testing.T) {
	snd := &fakeSender{failFor: 2}
	p := NewPump(7, NewScreen(40, 10), snd)
	p.Now = func() time.Time { return t0 }
	p.attached = true

	if err := p.Feed([]byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := p.Flush(); err != nil {
		t.Errorf("Flush = %v, want nil — a failed resync must not kill the session", err)
	}
}

// A broken connection and backpressure want opposite responses.
func TestARealSendFailureIsReported(t *testing.T) {
	boom := errors.New("socket gone")
	snd := &fakeSender{err: boom}
	p := NewPump(7, NewScreen(40, 10), snd)
	p.Now = func() time.Time { return t0 }
	p.attached = true

	if err := p.Feed([]byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := p.Flush(); !errors.Is(err, boom) {
		t.Errorf("Flush = %v, want the underlying failure", err)
	}
}

// Stopping would mean the next attach shows whatever was on screen when the
// last viewer left.
func TestDetachKeepsFeedingTheScreen(t *testing.T) {
	p, snd, _ := newTestPump(t)
	if err := p.Attach(); err != nil {
		t.Fatal(err)
	}
	p.Detach()

	if err := p.Feed([]byte("after the viewer left\r\n")); err != nil {
		t.Fatal(err)
	}
	if err := p.Attach(); err != nil {
		t.Fatal(err)
	}

	replays := snd.ofType(wire.TypeReplay)
	last := replays[len(replays)-1]
	if !bytes.Contains(last.Payload, []byte("after the viewer left")) {
		t.Error("the screen stopped being fed while detached")
	}
}

// Sending both would paint the screen and then paint a fragment of its history
// on top of it.
func TestAttachDiscardsWhatWasPending(t *testing.T) {
	p, snd, _ := newTestPump(t)
	p.attached = true
	if err := p.Feed([]byte("pending")); err != nil {
		t.Fatal(err)
	}
	if err := p.Attach(); err != nil {
		t.Fatal(err)
	}
	if got := snd.ofType(wire.TypeOutput); len(got) != 0 {
		t.Errorf("a pending batch was sent after the redraw: %d frames", len(got))
	}
}

func TestRunShipsEverythingBeforeItReturns(t *testing.T) {
	p, snd, _ := newTestPump(t)
	if err := p.Attach(); err != nil {
		t.Fatal(err)
	}
	// A pty read ends with an error rather than EOF when the child exits, so
	// the pending batch has to be flushed on the way out either way.
	if err := p.Run(bytes.NewReader([]byte("final output"))); err != nil {
		t.Fatalf("Run: %v", err)
	}
	outs := snd.ofType(wire.TypeOutput)
	if len(outs) != 1 || string(outs[0].Payload) != "final output" {
		t.Errorf("frames = %+v, want the final batch", outs)
	}
}

// A session outlives the socket its output was going to.
//
// The pump is what carries a terminal across a reconnect: the old connection's
// writes fail, and a pump left pointing at it leaves the agent running and its
// terminal silent for ever.
func TestRebindMovesTheOutputToTheNewConnection(t *testing.T) {
	p, old, _ := newTestPump(t)
	if err := p.Attach(); err != nil {
		t.Fatalf("Attach: %v", err)
	}

	fresh := &fakeSender{}
	p.Rebind(fresh)

	// The viewer is forgotten with the socket, so the caller repaints for one
	// that is still there — a redraw of the screen now, not the fragment of
	// history that was queued for a connection that has gone.
	if err := p.Attach(); err != nil {
		t.Fatalf("Attach after Rebind: %v", err)
	}
	if err := p.Feed([]byte("after the gap\r\n")); err != nil {
		t.Fatalf("Feed: %v", err)
	}
	if err := p.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	if !sentContains(fresh, "after the gap") {
		t.Error("output written after the reconnect never reached the new connection")
	}
	if sentContains(old, "after the gap") {
		t.Error("output was still being written to the connection that had gone")
	}
}

// Rebinding forgets the viewer: whoever was watching was watching through the
// socket that just went.
func TestRebindForgetsTheViewer(t *testing.T) {
	p, _, _ := newTestPump(t)
	if err := p.Attach(); err != nil {
		t.Fatalf("Attach: %v", err)
	}

	fresh := &fakeSender{}
	p.Rebind(fresh)

	if err := p.Feed([]byte("nobody is watching this\r\n")); err != nil {
		t.Fatalf("Feed: %v", err)
	}
	if err := p.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if len(fresh.all()) != 0 {
		t.Error("output was sent over the new connection with nobody attached")
	}
}

// A pump with nowhere to send is not a crash. Rebind takes whatever the caller
// has, and a nil sender is what "not connected" looks like.
func TestAPumpWithNoConnectionKeepsTheScreen(t *testing.T) {
	p, _, _ := newTestPump(t)
	p.Rebind(nil)
	if err := p.Attach(); err != nil {
		t.Fatalf("Attach with no connection: %v", err)
	}
	if err := p.Feed([]byte("still recorded\r\n")); err != nil {
		t.Fatalf("Feed: %v", err)
	}
	if !strings.Contains(string(p.Screen.Redraw()), "still recorded") {
		t.Error("the screen stopped being fed when there was nowhere to send")
	}
}

func sentContains(s *fakeSender, want string) bool {
	for _, f := range s.all() {
		if strings.Contains(string(f.Payload), want) {
			return true
		}
	}
	return false
}
