// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package machine

import (
	"bytes"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/agentrq/agentrq/daemon/wire"
)

type fakeViewer struct {
	who    string
	mu     sync.Mutex
	got    []wire.Frame
	err    error
	closed bool
}

func (v *fakeViewer) Name() string { return v.who }

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

// frames is what reached the terminal. Control frames are presence
// announcements rather than session traffic, and are asked for separately.
func (v *fakeViewer) frames() []wire.Frame {
	v.mu.Lock()
	defer v.mu.Unlock()
	out := make([]wire.Frame, 0, len(v.got))
	for _, f := range v.got {
		if f.Type != wire.TypeControl {
			out = append(out, f)
		}
	}
	return out
}

// presence is the most recent list of viewers this one was told about, and
// how many times it has been told.
func (v *fakeViewer) presence(t *testing.T) ([]string, int) {
	names, _, n := v.presenceWithSelf(t)
	return names, n
}

// presenceWithSelf also returns where the viewer was told it sits in the list.
func (v *fakeViewer) presenceWithSelf(t *testing.T) ([]string, int, int) {
	t.Helper()
	v.mu.Lock()
	defer v.mu.Unlock()
	var names []string
	you, n := 0, 0
	for _, f := range v.got {
		if f.Type != wire.TypeControl {
			continue
		}
		c, err := wire.ParseControl(f)
		if err != nil || c.Op != wire.OpPresence {
			continue
		}
		var p wire.Presence
		if err := json.Unmarshal(c.Body, &p); err != nil {
			t.Fatalf("presence body: %v", err)
		}
		names = p.Viewers
		you = p.You
		n++
	}
	return names, you, n
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

// Two people on one terminal is allowed. Being surprised by it is not: a
// keystroke arriving from nowhere is indistinguishable from a machine that has
// gone wrong, so every viewer is told who else is here.
func TestViewersAreToldWhoElseIsWatching(t *testing.T) {
	r, _, _ := relayWithMachine(t)
	const session = 9

	ada := &fakeViewer{who: "Ada"}
	if err := r.Attach(session, 11, ada); err != nil {
		t.Fatalf("attach: %v", err)
	}
	if names, n := ada.presence(t); n != 1 || len(names) != 1 || names[0] != "Ada" {
		t.Fatalf("first viewer was told %v after %d announcements", names, n)
	}

	grace := &fakeViewer{who: "Grace"}
	if err := r.Attach(session, 11, grace); err != nil {
		t.Fatalf("second attach: %v", err)
	}

	// Both of them, and sorted, so the UI does not reshuffle the names on
	// every announcement.
	want := []string{"Ada", "Grace"}
	for _, v := range []*fakeViewer{ada, grace} {
		names, _ := v.presence(t)
		if len(names) != 2 || names[0] != want[0] || names[1] != want[1] {
			t.Errorf("%s was told %v, want %v", v.who, names, want)
		}
	}

	// And when one leaves, the other stops being told the terminal is shared.
	r.Detach(session, grace)
	if names, _ := ada.presence(t); len(names) != 1 || names[0] != "Ada" {
		t.Errorf("after a viewer left, the remaining one was told %v", names)
	}
}

// The same person in two tabs is two entries. That is the honest answer: it is
// two terminals, and both of them can type.
func TestTheSamePersonTwiceIsTwoViewers(t *testing.T) {
	r, _, _ := relayWithMachine(t)
	const session = 9

	first := &fakeViewer{who: "Ada"}
	second := &fakeViewer{who: "Ada"}
	_ = r.Attach(session, 11, first)
	_ = r.Attach(session, 11, second)

	// Two identical names, and each tab is told which of the two it is —
	// there is no way to work that out from the list alone.
	firstNames, firstSelf, _ := first.presenceWithSelf(t)
	_, secondSelf, _ := second.presenceWithSelf(t)
	if len(firstNames) != 2 {
		t.Errorf("two tabs were reported as %v", firstNames)
	}
	if firstSelf == secondSelf {
		t.Errorf("both tabs were told they are viewer %d", firstSelf)
	}
}

// The last viewer leaving is a detach, not an announcement to nobody.
func TestTheLastViewerLeavingAnnouncesNothing(t *testing.T) {
	r, _, daemon := relayWithMachine(t)
	const session = 9

	v := &fakeViewer{who: "Ada"}
	_ = r.Attach(session, 11, v)
	before, _ := v.presence(t)
	r.Detach(session, v)

	if _, n := v.presence(t); n != len(before) {
		t.Errorf("a departed viewer was sent a presence update")
	}
	if got := daemon.lastFrame(); got.Type != wire.TypeControl {
		t.Fatalf("the daemon was not told to detach")
	}
}

// A viewer whose socket has already gone must not be reaped from inside an
// announcement it provoked — that re-enters Detach from within Detach.
func TestAnnouncingToADeadViewerDoesNotReapIt(t *testing.T) {
	r, _, _ := relayWithMachine(t)
	const session = 9

	dead := &fakeViewer{who: "Gone", err: errors.New("socket closed")}
	live := &fakeViewer{who: "Ada"}
	_ = r.Attach(session, 11, dead)
	_ = r.Attach(session, 11, live)

	if r.Viewers(session) != 2 {
		t.Fatalf("a failed announcement changed the viewer count")
	}
	if dead.isClosed() {
		t.Errorf("a failed announcement closed the viewer")
	}
}
