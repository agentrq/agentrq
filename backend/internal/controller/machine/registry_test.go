// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package machine

import (
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
		t.Errorf("InstanceID() = %q", r.InstanceID())
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
		t.Errorf("Count() = %d, want 2", r.Count())
	}
}

// Not connected *here* is not the same as offline: another instance may hold
// the socket, and the caller checks the stored pairing before deciding.
func TestSendToAnUnheldMachineSaysSo(t *testing.T) {
	r := NewRegistry("pod-a")
	if _, err := r.Get(42); !errors.Is(err, ErrNotConnected) {
		t.Errorf("Get error = %v, want ErrNotConnected", err)
	}
	if err := r.Send(42, frame(t, 1)); !errors.Is(err, ErrNotConnected) {
		t.Errorf("Send error = %v, want ErrNotConnected", err)
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
		t.Errorf("Get after Remove = %v, want ErrNotConnected", err)
	}
	// And removing again reports that somebody else already did.
	if r.Remove(1, fresh) {
		t.Error("Remove twice reported success")
	}
}

// Revocation has to take effect without the daemon's cooperation.
func TestDropClosesTheSocketFromThisEnd(t *testing.T) {
	r := NewRegistry("pod-a")
	c := &fakeConn{name: "c"}
	r.Add(1, c)

	if !r.Drop(1) {
		t.Fatal("Drop reported nothing to drop")
	}
	if !c.isClosed() {
		t.Error("Drop did not close the socket")
	}
	if _, err := r.Get(1); !errors.Is(err, ErrNotConnected) {
		t.Errorf("machine still registered after Drop: %v", err)
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
		t.Fatalf("Machines() = %v, want two entries", got)
	}
	seen := map[int64]bool{got[0]: true, got[1]: true}
	if !seen[7] || !seen[9] {
		t.Errorf("Machines() = %v, want 7 and 9", got)
	}
}

func TestSendPropagatesAWriteFailure(t *testing.T) {
	r := NewRegistry("pod-a")
	boom := errors.New("socket gone")
	r.Add(1, &fakeConn{sendErr: boom})

	if err := r.Send(1, frame(t, 1)); !errors.Is(err, boom) {
		t.Errorf("Send error = %v, want the underlying failure", err)
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
