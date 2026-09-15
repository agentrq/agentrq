// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package machine

import (
	"bytes"
	"errors"
	"sync"
	"testing"

	"github.com/agentrq/agentrq/daemon/wire"
)

type fakeViewer struct {
	mu     sync.Mutex
	got    []wire.Frame
	err    error
	closed bool
}

func (v *fakeViewer) Send(f wire.Frame) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.err != nil {
		return v.err
	}
	v.got = append(v.got, f)
	return nil
}

func (v *fakeViewer) Close() error {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.closed = true
	return nil
}

func (v *fakeViewer) frames() []wire.Frame {
	v.mu.Lock()
	defer v.mu.Unlock()
	return append([]wire.Frame(nil), v.got...)
}

func (v *fakeViewer) isClosed() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.closed
}

func relayWithMachine(t *testing.T) (*Relay, *Registry, *fakeConn) {
	t.Helper()
	reg := NewRegistry("pod-a")
	c := &fakeConn{}
	reg.Add(11, c)
	return NewRelay(reg), reg, c
}

func outFrame(t *testing.T, session uint64, payload []byte) wire.Frame {
	t.Helper()
	f, err := wire.SessionFrame(wire.TypeOutput, session, payload)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestOutputReachesEveryViewer(t *testing.T) {
	r, _, _ := relayWithMachine(t)
	a, b := &fakeViewer{}, &fakeViewer{}

	if err := r.Attach(7, 11, a); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if err := r.Attach(7, 11, b); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if r.Viewers(7) != 2 {
		t.Fatalf("viewers = %d", r.Viewers(7))
	}

	payload := []byte{0x1b, '[', '2', 'K', 0xff, 0x00}
	r.FromDaemon(outFrame(t, 7, payload))

	for name, v := range map[string]*fakeViewer{"a": a, "b": b} {
		fs := v.frames()
		if len(fs) != 1 {
			t.Fatalf("viewer %s got %d frames", name, len(fs))
		}
		if !bytes.Equal(fs[0].Payload, payload) {
			t.Errorf("viewer %s payload changed: %#v", name, fs[0].Payload)
		}
	}
}

// Asking again would make the daemon repaint for everyone every time somebody
// opened a second tab.
func TestOnlyTheFirstViewerAsksTheDaemonToAttach(t *testing.T) {
	r, _, conn := relayWithMachine(t)
	if err := r.Attach(7, 11, &fakeViewer{}); err != nil {
		t.Fatal(err)
	}
	after := conn.count()
	if after != 1 {
		t.Fatalf("daemon received %d frames for the first attach, want 1", after)
	}
	if err := r.Attach(7, 11, &fakeViewer{}); err != nil {
		t.Fatal(err)
	}
	if conn.count() != after {
		t.Error("a second viewer made the daemon repaint for everyone")
	}
}

func TestTheDaemonIsToldWhenTheLastViewerLeaves(t *testing.T) {
	r, _, conn := relayWithMachine(t)
	a, b := &fakeViewer{}, &fakeViewer{}
	if err := r.Attach(7, 11, a); err != nil {
		t.Fatal(err)
	}
	if err := r.Attach(7, 11, b); err != nil {
		t.Fatal(err)
	}
	before := conn.count()

	r.Detach(7, a)
	if conn.count() != before {
		t.Error("the daemon was told to detach while somebody was still watching")
	}
	r.Detach(7, b)
	if conn.count() != before+1 {
		t.Error("the daemon was not told when the last viewer left")
	}
	if r.Viewers(7) != 0 {
		t.Errorf("viewers = %d after everyone left", r.Viewers(7))
	}
}

// One slow browser must not stall a session for everyone else, nor back up
// into the daemon.
func TestASlowViewerIsDroppedRatherThanBlockingTheOthers(t *testing.T) {
	r, _, _ := relayWithMachine(t)
	good := &fakeViewer{}
	bad := &fakeViewer{err: errors.New("socket full")}

	if err := r.Attach(7, 11, good); err != nil {
		t.Fatal(err)
	}
	if err := r.Attach(7, 11, bad); err != nil {
		t.Fatal(err)
	}

	r.FromDaemon(outFrame(t, 7, []byte("hello")))

	if len(good.frames()) != 1 {
		t.Error("the working viewer missed a frame because of the broken one")
	}
	if !bad.isClosed() {
		t.Error("the broken viewer was not closed")
	}
	if r.Viewers(7) != 1 {
		t.Errorf("viewers = %d, want the broken one removed", r.Viewers(7))
	}
}

// A terminal panel that opens, shows nothing and gives no reason is worse than
// an error.
func TestAttachingToAMachineHeldElsewhereIsRefused(t *testing.T) {
	r := NewRelay(NewRegistry("pod-a")) // holds nothing
	if err := r.Attach(7, 11, &fakeViewer{}); !errors.Is(err, ErrSessionNotHere) {
		t.Errorf("error = %v, want ErrSessionNotHere", err)
	}
}

// A viewer must not be able to synthesise output or an exit, which would let
// one browser lie to every other viewer of the same session.
func TestAViewerMaySendOnlyInputAndResize(t *testing.T) {
	r, _, conn := relayWithMachine(t)
	if err := r.Attach(7, 11, &fakeViewer{}); err != nil {
		t.Fatal(err)
	}
	before := conn.count()

	for _, typ := range []wire.Type{wire.TypeInput, wire.TypeResize} {
		f, err := wire.SessionFrame(typ, 7, []byte(`{"cols":80,"rows":24}`))
		if err != nil {
			t.Fatal(err)
		}
		if err := r.FromViewer(7, f); err != nil {
			t.Errorf("FromViewer(%s) = %v, want it allowed", typ, err)
		}
	}
	if conn.count() != before+2 {
		t.Errorf("input and resize did not reach the daemon")
	}

	for _, typ := range []wire.Type{wire.TypeOutput, wire.TypeExit, wire.TypeReplay} {
		f, err := wire.SessionFrame(typ, 7, []byte("x"))
		if err != nil {
			t.Fatal(err)
		}
		if err := r.FromViewer(7, f); err == nil {
			t.Errorf("a viewer was allowed to send %s", typ)
		}
	}
}

func TestInputToAnUnattachedSessionIsRefused(t *testing.T) {
	r, _, _ := relayWithMachine(t)
	f, err := wire.SessionFrame(wire.TypeInput, 99, []byte("x"))
	if err != nil {
		t.Fatal(err)
	}
	if err := r.FromViewer(99, f); !errors.Is(err, ErrSessionNotHere) {
		t.Errorf("error = %v, want ErrSessionNotHere", err)
	}
}

// Megabytes a second is not somebody working, it is a process that has gone
// wrong. The daemon's own resync is what repairs the viewer's screen.
func TestTheRateCapStopsARunawaySession(t *testing.T) {
	r, _, _ := relayWithMachine(t)
	v := &fakeViewer{}
	if err := r.Attach(7, 11, v); err != nil {
		t.Fatal(err)
	}

	chunk := bytes.Repeat([]byte("x"), 64*1024)
	sent := 0
	for i := 0; i < 64; i++ { // 4 MiB in one window
		r.FromDaemon(outFrame(t, 7, chunk))
		sent++
	}

	got := len(v.frames())
	if got == sent {
		t.Error("the rate cap let everything through")
	}
	if got == 0 {
		t.Error("the rate cap blocked everything, including the start of the window")
	}
	// Roughly the cap's worth got through, not four times it.
	if got*len(chunk) > RateLimit*2 {
		t.Errorf("forwarded %d bytes against a %d cap", got*len(chunk), RateLimit)
	}
}

// Separate sessions have separate budgets: one noisy session must not silence
// a quiet one.
func TestTheRateCapIsPerSession(t *testing.T) {
	r, _, _ := relayWithMachine(t)
	noisy, quiet := &fakeViewer{}, &fakeViewer{}
	if err := r.Attach(7, 11, noisy); err != nil {
		t.Fatal(err)
	}
	if err := r.Attach(8, 11, quiet); err != nil {
		t.Fatal(err)
	}

	chunk := bytes.Repeat([]byte("x"), 64*1024)
	for i := 0; i < 64; i++ {
		r.FromDaemon(outFrame(t, 7, chunk))
	}
	r.FromDaemon(outFrame(t, 8, []byte("just a little")))

	if len(quiet.frames()) != 1 {
		t.Error("a noisy session silenced a quiet one")
	}
}
