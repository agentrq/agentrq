// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package machine

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/agentrq/agentrq/daemon/wire"
)

// fakeConn records what was sent to it, so the routing can be checked without
// a network.
type fakeConn struct {
	name    string
	mu      sync.Mutex
	sent    []wire.Frame
	closed  bool
	sendErr error
}

func (c *fakeConn) Send(f wire.Frame) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sendErr != nil {
		return c.sendErr
	}
	c.sent = append(c.sent, f)
	return nil
}

func (c *fakeConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	return nil
}

func (c *fakeConn) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.sent)
}

// firstOfType is the first frame of a kind the daemon was sent.
//
// Not lastFrame: a viewer attaching makes the relay send a control frame
// first, so a test that waits for "any frame" and then reads the last one is
// reading the attach and calling it the keystroke.
func (c *fakeConn) firstOfType(t wire.Type) (wire.Frame, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, f := range c.sent {
		if f.Type == t {
			return f, true
		}
	}
	return wire.Frame{}, false
}

// countOfType and lastOfType are firstOfType's answer for a test that sends
// more than one frame.
//
// The trap firstOfType names has a second half: counting *all* frames to
// decide that a keystroke has arrived counts the attach as well, so the wait
// can finish before the keystroke is sent at all — and then read the attach.
// A test that only ever counts and reads the kind it sent cannot be disturbed
// by a control frame arriving at any moment.
func (c *fakeConn) countOfType(t wire.Type) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, f := range c.sent {
		if f.Type == t {
			n++
		}
	}
	return n
}

func (c *fakeConn) lastOfType(t wire.Type) (wire.Frame, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := len(c.sent) - 1; i >= 0; i-- {
		if c.sent[i].Type == t {
			return c.sent[i], true
		}
	}
	return wire.Frame{}, false
}

func (c *fakeConn) isClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

func frame(t *testing.T, session uint64) wire.Frame {
	t.Helper()
	f, err := wire.SessionFrame(wire.TypeInput, session, []byte{0x1b})
	if err != nil {
		t.Fatalf("SessionFrame: %v", err)
	}
	return f
}

func TestRegistryRoutesToTheRightMachine(t *testing.T) {
	r := NewRegistry("pod-a")
	if r.InstanceID() != "pod-a" {
		t.Errorf("InstanceID returned %q, want \"pod-a\"", r.InstanceID())
	}

	a, b := &fakeConn{name: "a"}, &fakeConn{name: "b"}
	r.Add(1, a)
	r.Add(2, b)

	if err := r.Send(1, frame(t, 9)); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if a.count() != 1 || b.count() != 0 {
		t.Errorf("frame went to the wrong machine: a=%d b=%d", a.count(), b.count())
	}
	if r.Count() != 2 {
		t.Errorf("Count returned %d, want 2", r.Count())
	}
}

// Not connected *here* is not the same as offline: another instance may hold
// the socket, and the caller checks the stored pairing before deciding.
func TestSendToAnUnheldMachineSaysSo(t *testing.T) {
	r := NewRegistry("pod-a")
	if _, err := r.Get(42); !errors.Is(err, ErrNotConnected) {
		t.Errorf("Get returned %v, want ErrNotConnected", err)
	}
	if err := r.Send(42, frame(t, 1)); !errors.Is(err, ErrNotConnected) {
		t.Errorf("Send returned %v, want ErrNotConnected", err)
	}
}

// A reconnect must displace rather than be refused: the old socket is usually
// a dead connection nobody has noticed, and refusing the new one leaves the
// machine unreachable until it times out.
func TestReconnectDisplacesTheOldSocket(t *testing.T) {
	r := NewRegistry("pod-a")
	old := &fakeConn{name: "old"}
	r.Add(1, old)

	fresh := &fakeConn{name: "fresh"}
	displaced := r.Add(1, fresh)

	if displaced != Conn(old) {
		t.Fatalf("Add returned %v, want the displaced connection", displaced)
	}
	// Returned rather than closed inside the lock: closing there would block
	// every other machine's traffic on one socket's shutdown.
	if old.isClosed() {
		t.Error("the registry closed the displaced socket while holding the lock")
	}
	if err := r.Send(1, frame(t, 1)); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if fresh.count() != 1 || old.count() != 0 {
		t.Errorf("traffic went to the displaced socket: fresh=%d old=%d", fresh.count(), old.count())
	}
}

// The guard that matters. A slow disconnect on an old connection arrives after
// the daemon has already reconnected; an unconditional delete would unroute a
// machine that is connected and healthy.
func TestALateDisconnectCannotUnrouteALiveMachine(t *testing.T) {
	r := NewRegistry("pod-a")
	old := &fakeConn{name: "old"}
	fresh := &fakeConn{name: "fresh"}

	r.Add(1, old)
	r.Add(1, fresh) // the daemon reconnected

	// Now the old connection finally notices it is dead and cleans up.
	if r.Remove(1, old) {
		t.Error("a stale connection was allowed to remove the live one")
	}
	if err := r.Send(1, frame(t, 1)); err != nil {
		t.Fatalf("the live machine became unreachable: %v", err)
	}

	// The holder can remove itself.
	if !r.Remove(1, fresh) {
		t.Error("the current holder could not remove itself")
	}
	if _, err := r.Get(1); !errors.Is(err, ErrNotConnected) {
		t.Errorf("Get after Remove returned %v, want ErrNotConnected", err)
	}
	// And removing again reports that somebody else already did.
	if r.Remove(1, fresh) {
		t.Error("Remove twice reported success")
	}
}

// Revocation has to take effect without the daemon's cooperation.
func TestDropClosesTheSocketFromThisEnd(t *testing.T) {
	r := NewRegistry("pod-a")
	conn := &fakeConn{name: "conn"}
	r.Add(1, conn)

	if !r.Drop(1) {
		t.Fatal("Drop reported nothing to drop")
	}
	if !conn.isClosed() {
		t.Error("Drop did not close the socket")
	}
	if _, err := r.Get(1); !errors.Is(err, ErrNotConnected) {
		t.Errorf("Get after Drop returned %v, want ErrNotConnected", err)
	}
	if r.Drop(1) {
		t.Error("Drop of an absent machine reported success")
	}
}

func TestMachinesListsWhatThisInstanceHolds(t *testing.T) {
	r := NewRegistry("pod-a")
	r.Add(7, &fakeConn{})
	r.Add(9, &fakeConn{})

	got := r.Machines()
	if len(got) != 2 {
		t.Fatalf("Machines returned %v, want two entries", got)
	}
	seen := map[int64]bool{got[0]: true, got[1]: true}
	if !seen[7] || !seen[9] {
		t.Errorf("Machines returned %v, want 7 and 9", got)
	}
}

func TestSendPropagatesAWriteFailure(t *testing.T) {
	r := NewRegistry("pod-a")
	errSocket := errors.New("socket gone")
	r.Add(1, &fakeConn{sendErr: errSocket})

	if err := r.Send(1, frame(t, 1)); !errors.Is(err, errSocket) {
		t.Errorf("Send returned %v, want the socket's write error %v", err, errSocket)
	}
}

// Daemons connect and drop constantly; the registry is touched from every one
// of those goroutines at once.
func TestRegistryIsSafeUnderConcurrentUse(t *testing.T) {
	r := NewRegistry("pod-a")
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		id := int64(i % 10)
		wg.Add(3)
		go func() { defer wg.Done(); c := &fakeConn{}; r.Add(id, c); r.Remove(id, c) }()
		go func() { defer wg.Done(); _, _ = r.Get(id) }()
		go func() { defer wg.Done(); _ = r.Count(); _ = r.Machines() }()
	}
	wg.Wait()
}

// lastFrame is what the daemon most recently received.
func (c *fakeConn) lastFrame() wire.Frame {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.sent) == 0 {
		return wire.Frame{}
	}
	return c.sent[len(c.sent)-1]
}

// Ask has no daemon on the other end here, so the reply is delivered by hand
// — the point of the test is that Ask wakes up and returns it, not that a
// real socket round-trips it.
func TestAskReturnsTheCorrelatedReply(t *testing.T) {
	r := NewRegistry("pod-a")
	conn := &fakeConn{}
	r.Add(1, conn)

	done := make(chan struct{})
	var reply wire.Control
	var askErr error
	go func() {
		reply, askErr = r.Ask(context.Background(), 1, wire.Control{Op: wire.OpListAcpAgents}, time.Second)
		close(done)
	}()

	// The frame Ask actually sent names the id it is waiting on — a fixed id
	// in the test would pass even if Ask ignored what it generated.
	waitFor(t, func() bool { return conn.count() > 0 }, "Ask never sent a request")
	sent, err := wire.ParseControl(conn.lastFrame())
	if err != nil {
		t.Fatalf("ParseControl: %v", err)
	}
	if sent.ID == "" {
		t.Fatal("Ask sent a request with no id to correlate a reply against")
	}

	if !r.Deliver(wire.Control{ID: sent.ID, Op: wire.OpAcpAgents, Body: []byte(`{"agents":[]}`)}) {
		t.Fatal("Deliver reported nothing was waiting")
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Ask did not return once its reply was delivered")
	}
	if askErr != nil {
		t.Fatalf("Ask: %v", askErr)
	}
	if reply.ID != sent.ID || reply.Op != wire.OpAcpAgents {
		t.Errorf("Ask returned %+v, want the delivered reply", reply)
	}
}

// The wait is bounded: a daemon that never answers must not hold the caller
// open forever.
func TestAskTimesOutWhenNothingReplies(t *testing.T) {
	r := NewRegistry("pod-a")
	r.Add(1, &fakeConn{})

	_, err := r.Ask(context.Background(), 1, wire.Control{Op: wire.OpListAcpAgents}, 10*time.Millisecond)
	if !errors.Is(err, ErrRequestTimeout) {
		t.Errorf("Ask returned %v, want ErrRequestTimeout", err)
	}
}

// A send failure is reported immediately rather than waiting out the timeout
// for a reply that was never going to arrive.
func TestAskPropagatesASendFailure(t *testing.T) {
	r := NewRegistry("pod-a")
	errSocket := errors.New("socket gone")
	r.Add(1, &fakeConn{sendErr: errSocket})

	_, err := r.Ask(context.Background(), 1, wire.Control{Op: wire.OpListAcpAgents}, time.Second)
	if !errors.Is(err, errSocket) {
		t.Errorf("Ask returned %v, want the socket's send error %v", err, errSocket)
	}
}

// Asking a machine nothing here holds is the same ErrNotConnected as Send's,
// not a timeout — there is nobody to ever answer.
func TestAskRefusesAnUnconnectedMachine(t *testing.T) {
	r := NewRegistry("pod-a")
	_, err := r.Ask(context.Background(), 42, wire.Control{Op: wire.OpListAcpAgents}, time.Second)
	if !errors.Is(err, ErrNotConnected) {
		t.Errorf("Ask returned %v, want ErrNotConnected", err)
	}
}

// A reply with nobody waiting on its id — arriving late, after Ask's own
// timeout already gave up and stopped listening — is a stray, not a bug.
func TestDeliverReportsWhetherAnythingWasWaiting(t *testing.T) {
	r := NewRegistry("pod-a")
	if r.Deliver(wire.Control{ID: "nobody-asked", Op: wire.OpAcpAgents}) {
		t.Error("Deliver reported success for an id nothing was waiting on")
	}
	if r.Deliver(wire.Control{Op: wire.OpAcpAgents}) {
		t.Error("Deliver reported success for a reply with no id at all")
	}
}

// waitFor polls until cond holds, for the tests that drive real sockets.
func waitFor(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal(msg)
}

// A fork launch is refused unless the daemon said it can run one, and what it
// said belongs to that connection: a reconnect may be an older daemon.
func TestCapabilitiesBelongToTheConnectionThatSaidThem(t *testing.T) {
	r := NewRegistry("pod-a")
	old, fresh := &fakeConn{name: "old"}, &fakeConn{name: "fresh"}
	if r.HasCapability(1, wire.CapabilityFork) {
		t.Fatal("an unheld machine has a capability")
	}

	r.Add(1, old)
	if r.HasCapability(1, wire.CapabilityFork) {
		t.Fatal("a daemon that has not said hello has a capability")
	}
	r.SetCapabilities(1, old, []string{wire.CapabilityFork})
	if !r.HasCapability(1, wire.CapabilityFork) || r.HasCapability(1, "other") {
		t.Fatal("the hello's capabilities were not kept as said")
	}

	r.Add(1, fresh)
	if r.HasCapability(1, wire.CapabilityFork) {
		t.Error("a reconnect inherited the previous connection's capabilities")
	}
	r.SetCapabilities(1, old, []string{wire.CapabilityFork}) // a late hello on the displaced socket
	if r.HasCapability(1, wire.CapabilityFork) {
		t.Error("a displaced connection's hello was credited to the new one")
	}

	r.SetCapabilities(1, fresh, []string{wire.CapabilityFork})
	r.Remove(1, fresh)
	if r.HasCapability(1, wire.CapabilityFork) {
		t.Error("capabilities outlived their connection's removal")
	}
	r.Add(1, fresh)
	r.SetCapabilities(1, fresh, []string{wire.CapabilityFork})
	r.Drop(1)
	if r.HasCapability(1, wire.CapabilityFork) {
		t.Error("capabilities outlived a drop")
	}
}

func TestASessionRecordsItsDaemonsCapabilities(t *testing.T) {
	r := NewRegistry("pod-a")
	session, err := NewSession(t.Context(), r, &fakeAuth{}, Identity{MachineID: 7}, &fakeConn{})
	if err != nil {
		t.Fatal(err)
	}
	session.SetCapabilities([]string{wire.CapabilityFork})
	if !r.HasCapability(7, wire.CapabilityFork) {
		t.Error("the session's hello was not recorded")
	}
	(&Session{}).SetCapabilities([]string{wire.CapabilityFork}) // no registry: nothing to record
}
