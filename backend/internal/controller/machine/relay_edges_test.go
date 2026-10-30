// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package machine

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/agentrq/agentrq/daemon/wire"
)

func TestSessionTerminal(t *testing.T) {
	for status, want := range map[string]bool{
		SessionStarting: false,
		SessionRunning:  false,
		SessionExited:   true,
		SessionKilled:   true,
		SessionFailed:   true,
		"":              false,
	} {
		if got := SessionTerminal(status); got != want {
			t.Errorf("SessionTerminal(%q) = %v, want %v", status, got, want)
		}
	}
}

func TestSessionSendWritesToItsOwnSocket(t *testing.T) {
	r := NewRegistry("pod-a")
	c := &fakeConn{}
	s, err := NewSession(context.Background(), r, &fakeAuth{}, Identity{MachineID: 7}, c)
	if err != nil {
		t.Fatal(err)
	}
	f := outFrame(t, 3, []byte("hi"))
	if err := s.Send(f); err != nil {
		t.Fatal(err)
	}
	if got, ok := c.firstOfType(wire.TypeOutput); !ok || string(got.Payload) != "hi" || got.SessionID != 3 {
		t.Errorf("the socket got %+v, %v; want the frame sent", got, ok)
	}
}

// A request with no op cannot be built, and nothing is left waiting for it.
func TestAskRefusesARequestItCannotBuild(t *testing.T) {
	r := NewRegistry("pod-a")
	c := &fakeConn{}
	r.Add(7, c)
	if _, err := r.Ask(context.Background(), 7, wire.Control{}, time.Minute); err == nil {
		t.Fatal("a request with no op was sent")
	}
	if c.count() != 0 {
		t.Errorf("the daemon was sent %d frames, want none", c.count())
	}
	r.pendingMu.RLock()
	defer r.pendingMu.RUnlock()
	if len(r.pending) != 0 {
		t.Errorf("%d requests left waiting", len(r.pending))
	}
}

// A caller that gives up stops waiting at once, whatever the timeout.
func TestAskEndsWithItsCaller(t *testing.T) {
	r := NewRegistry("pod-a")
	r.Add(7, &fakeConn{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := r.Ask(ctx, 7, wire.Control{Op: wire.OpPresence}, time.Hour); !errors.Is(err, context.Canceled) {
		t.Errorf("Ask with a cancelled caller = %v, want context.Canceled", err)
	}
}

// A second reply to the same request is taken and dropped rather than
// blocking the daemon's frame loop.
func TestASecondReplyToTheSameRequestDoesNotBlock(t *testing.T) {
	r, _, _ := relayWithMachine(t)
	ch := make(chan wire.Control, 1)
	r.registry.pendingMu.Lock()
	r.registry.pending["req-1"] = ch
	r.registry.pendingMu.Unlock()

	done := make(chan [2]bool, 1)
	go func() {
		reply := wire.Control{ID: "req-1", Op: wire.OpPresence}
		done <- [2]bool{r.Deliver(reply), r.Deliver(reply)}
	}()
	select {
	case got := <-done:
		if !got[0] || !got[1] {
			t.Errorf("Deliver = %v, want both taken", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a second reply blocked")
	}
	if len(ch) != 1 {
		t.Errorf("%d replies queued, want the first only", len(ch))
	}
	if r.Deliver(wire.Control{ID: "nobody", Op: wire.OpPresence}) {
		t.Error("a reply nobody asked for was taken")
	}
}

// Input too large for one frame is refused here rather than sent.
func TestSendInputRefusesAnOversizedWrite(t *testing.T) {
	r, _, daemon := relayWithMachine(t)
	if err := r.SendInput(11, 7, make([]byte, wire.MaxPayload+1)); !errors.Is(err, wire.ErrPayloadTooBig) {
		t.Errorf("SendInput = %v, want ErrPayloadTooBig", err)
	}
	if daemon.count() != 0 {
		t.Errorf("the daemon was sent %d frames, want none", daemon.count())
	}
}

// The output cap is per window: a session over it is let through again once
// the window has passed.
func TestTheRateCapResetsWithTheWindow(t *testing.T) {
	r, _, _ := relayWithMachine(t)
	if !r.allow(7, RateLimit) {
		t.Fatal("output up to the cap was refused")
	}
	if r.allow(7, 1) {
		t.Fatal("output over the cap was allowed")
	}
	r.mu.Lock()
	r.rates[7].windowStart = time.Now().Add(-RateWindow)
	r.mu.Unlock()
	if !r.allow(7, 1) {
		t.Error("the cap still held after its window had passed")
	}
}

// Detaching from a session nobody attached to tells the daemon nothing and
// announces to nobody.
func TestDetachingAnUnknownSessionDoesNothing(t *testing.T) {
	r, _, daemon := relayWithMachine(t)
	v := &fakeViewer{}
	r.Detach(99, v)
	v.mu.Lock()
	told := len(v.got)
	v.mu.Unlock()
	if daemon.count() != 0 || told != 0 {
		t.Errorf("the daemon got %d frames and the viewer %d, want none", daemon.count(), told)
	}
}
