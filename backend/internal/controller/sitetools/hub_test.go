// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package sitetools

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"
)

// fakeConn records frames; onSend, when set, runs after each one is recorded.
type fakeConn struct {
	mu     sync.Mutex
	frames []Frame
	err    error
	onSend func(Frame)
}

func (c *fakeConn) Send(f Frame) error {
	c.mu.Lock()
	c.frames = append(c.frames, f)
	c.mu.Unlock()
	if c.onSend != nil {
		c.onSend(f)
	}
	return c.err
}

func (c *fakeConn) Close() error { return nil }

func counter() func() string {
	n := 0
	return func() string { n++; return "call-" + string(rune('0'+n)) }
}

func TestHubInstanceID(t *testing.T) {
	if got := NewHub("pod-1", counter()).InstanceID(); got != "pod-1" {
		t.Fatalf("InstanceID returned %q, want \"pod-1\"", got)
	}
}

func TestHubCallDeliver(t *testing.T) {
	h := NewHub("i", counter())
	browser := &fakeConn{}
	browser.onSend = func(f Frame) {
		go h.Deliver(Frame{Type: FrameResult, CallID: f.CallID, Text: "done"})
	}
	h.Add(1, "b", browser)

	got, err := h.Call(context.Background(), 1, "b", "https://a.b", "search", json.RawMessage(`{"q":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "done" {
		t.Fatalf("Call returned %+v, want the delivered result with text \"done\"", got)
	}
	sent := browser.frames[0]
	if sent.Type != FrameCall || sent.CallID != "call-1" || sent.Origin != "https://a.b" ||
		sent.Tool != "search" || string(sent.Arguments) != `{"q":1}` {
		t.Fatalf("the browser was sent %+v, want a call frame call-1 for search on https://a.b with arguments {\"q\":1}", sent)
	}
	if h.Deliver(Frame{Type: FrameResult, CallID: "call-1"}) {
		t.Fatal("a finished call must not accept a second result")
	}
}

func TestHubDeliverUnknown(t *testing.T) {
	h := NewHub("i", counter())
	if h.Deliver(Frame{Type: FrameResult, CallID: "call-nobody-made"}) {
		t.Fatal("a result for a callId no call is waiting on was accepted")
	}
	if h.Deliver(Frame{Type: FrameResult}) {
		t.Fatal("a result with no callId was accepted")
	}
}

func TestHubDeliverTwiceBeforeRead(t *testing.T) {
	h := NewHub("i", counter())
	browser := &fakeConn{}
	browser.onSend = func(f Frame) {
		h.Deliver(Frame{Type: FrameResult, CallID: f.CallID, Text: "first"})
		if h.Deliver(Frame{Type: FrameResult, CallID: f.CallID, Text: "second"}) {
			t.Error("a second result to the same call was accepted")
		}
	}
	h.Add(1, "b", browser)
	got, err := h.Call(context.Background(), 1, "b", "o", "t", nil)
	if err != nil || got.Text != "first" {
		t.Fatalf("Call returned %+v and error %v, want the first result and no error", got, err)
	}
}

func TestHubCallOffline(t *testing.T) {
	h := NewHub("i", counter())
	h.Add(1, "other", &fakeConn{})
	if _, err := h.Call(context.Background(), 1, "b", "o", "t", nil); !errors.Is(err, ErrOffline) {
		t.Fatalf("a call to a browser the user has not connected returned %v, want ErrOffline", err)
	}
	if _, err := h.Call(context.Background(), 2, "b", "o", "t", nil); !errors.Is(err, ErrOffline) {
		t.Fatalf("a call for a user with no browser connected returned %v, want ErrOffline", err)
	}
}

func TestHubCallTimeout(t *testing.T) {
	h := NewHub("i", counter())
	h.deadline = 50 * time.Millisecond
	h.Add(1, "b", &fakeConn{})
	if _, err := h.Call(context.Background(), 1, "b", "o", "t", nil); !errors.Is(err, ErrTimeout) {
		t.Fatalf("a call nobody answered returned %v, want ErrTimeout", err)
	}
	if n := len(h.pending); n != 0 {
		t.Fatalf("%d pending calls were left behind, want none", n)
	}
}

func TestHubCallContextCancelled(t *testing.T) {
	h := NewHub("i", counter())
	ctx, cancel := context.WithCancel(context.Background())
	browser := &fakeConn{onSend: func(Frame) { cancel() }}
	h.Add(1, "b", browser)
	if _, err := h.Call(ctx, 1, "b", "o", "t", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("a call whose context was cancelled returned %v, want context.Canceled", err)
	}
}

func TestHubCallSendError(t *testing.T) {
	h := NewHub("i", counter())
	errSocket := errors.New("browser socket closed")
	h.Add(1, "b", &fakeConn{err: errSocket})
	if _, err := h.Call(context.Background(), 1, "b", "o", "t", nil); !errors.Is(err, errSocket) {
		t.Fatalf("Call returned %v, want the socket's send error %v", err, errSocket)
	}
	if n := len(h.pending); n != 0 {
		t.Fatalf("%d pending calls were left behind, want none", n)
	}
}

func TestHubRemoveDuringCall(t *testing.T) {
	h := NewHub("i", counter())
	other := &fakeConn{}
	otherSent := make(chan string, 1)
	other.onSend = func(f Frame) { otherSent <- f.CallID }
	h.Add(1, "other", other)
	otherDone := make(chan Frame, 1)
	go func() {
		result, _ := h.Call(context.Background(), 1, "other", "o", "t", nil)
		otherDone <- result
	}()
	otherID := <-otherSent

	browser := &fakeConn{}
	browser.onSend = func(Frame) { go h.Remove(1, "b", browser) }
	h.Add(1, "b", browser)
	start := time.Now()
	if _, err := h.Call(context.Background(), 1, "b", "o", "t", nil); !errors.Is(err, ErrOffline) {
		t.Fatalf("a call to a browser removed mid-call returned %v, want ErrOffline", err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("Remove did not fail the call immediately")
	}
	if h.Online(1, "b") {
		t.Fatal("removed browser still online")
	}

	// Another browser's pending call is untouched by that Remove.
	if !h.Deliver(Frame{Type: FrameResult, CallID: otherID, Text: "ok"}) {
		t.Fatal("the other browser's call was failed too")
	}
	if result := <-otherDone; result.Text != "ok" {
		t.Fatalf("the other browser's call returned %+v, want its delivered result with text \"ok\"", result)
	}
}

func TestHubDisplaced(t *testing.T) {
	h := NewHub("i", counter())
	old, fresh := &fakeConn{}, &fakeConn{}
	if displaced := h.Add(1, "b", old); displaced != nil {
		t.Fatalf("the first Add displaced %v, want nothing", displaced)
	}
	if displaced := h.Add(1, "b", fresh); displaced != old {
		t.Fatalf("the second Add displaced %v, want the old conn", displaced)
	}
	h.Remove(1, "b", old)
	if !h.Online(1, "b") {
		t.Fatal("the displaced conn's Remove took the new one offline")
	}
	h.Remove(2, "b", old) // unknown user: a no-op
	h.Remove(1, "b", fresh)
	if h.Online(1, "b") {
		t.Fatal("the browser is still online after its current conn was removed")
	}
}

func TestNewHubDefaults(t *testing.T) {
	if deadline := NewHub("i", counter()).deadline; deadline != CallDeadline {
		t.Fatalf("a new hub's deadline is %v, want CallDeadline (%v)", deadline, CallDeadline)
	}
}
