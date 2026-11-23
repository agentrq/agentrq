// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package sitetools

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"
)

// Conn is one browser socket, narrowed for tests (like machine.Conn).
type Conn interface {
	Send(Frame) error
	Close() error
}

var (
	ErrOffline = errors.New("sitetools: browser not connected to this instance")
	ErrTimeout = errors.New("sitetools: no result from the browser before the deadline")
)

type browserKey struct {
	userID    int64
	browserID string
}

type pendingCall struct {
	key browserKey
	ch  chan reply
}

type reply struct {
	frame Frame
	err   error
}

// Hub is the browsers whose sockets this process holds, and the calls waiting
// on them. Per-process, like machine.Registry: a call is never relayed to
// another instance.
type Hub struct {
	instanceID string
	ids        func() string
	deadline   time.Duration

	// One lock for both maps, so a Call cannot find a conn and then miss the
	// Remove that fails its pending entry.
	mu      sync.Mutex
	conns   map[browserKey]Conn
	pending map[string]pendingCall
}

// NewHub makes a hub for one backend instance. ids makes call ids; they must
// be unguessable, since a result is matched to its call by id alone.
func NewHub(instanceID string, ids func() string) *Hub {
	return &Hub{
		instanceID: instanceID,
		ids:        ids,
		deadline:   CallDeadline,
		conns:      map[browserKey]Conn{},
		pending:    map[string]pendingCall{},
	}
}

// InstanceID names this process.
func (h *Hub) InstanceID() string { return h.instanceID }

// Add records a browser's socket, returning any connection it displaced for
// the caller to close outside the lock.
func (h *Hub) Add(userID int64, browserID string, c Conn) (displaced Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	k := browserKey{userID, browserID}
	displaced = h.conns[k]
	h.conns[k] = c
	return displaced
}

// Remove drops a browser's socket only if c is still the registered one — a
// displaced socket's late disconnect must not unroute its replacement — and
// fails that browser's pending calls with ErrOffline.
func (h *Hub) Remove(userID int64, browserID string, c Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	k := browserKey{userID, browserID}
	if h.conns[k] != c {
		return
	}
	delete(h.conns, k)
	for id, p := range h.pending {
		if p.key == k {
			p.ch <- reply{err: ErrOffline}
			delete(h.pending, id)
		}
	}
}

// Online reports whether this instance holds the browser's socket.
func (h *Hub) Online(userID int64, browserID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	_, ok := h.conns[browserKey{userID, browserID}]
	return ok
}

// Call sends a call frame and waits for the matching result, the deadline, or
// ctx. A result carrying Error is returned as a frame, not an error.
func (h *Hub) Call(ctx context.Context, userID int64, browserID, origin, tool string, args json.RawMessage) (Frame, error) {
	k := browserKey{userID, browserID}
	id := h.ids()
	ch := make(chan reply, 1)

	h.mu.Lock()
	c, ok := h.conns[k]
	if !ok {
		h.mu.Unlock()
		return Frame{}, ErrOffline
	}
	h.pending[id] = pendingCall{key: k, ch: ch}
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.pending, id)
		h.mu.Unlock()
	}()

	if err := c.Send(Frame{Type: FrameCall, CallID: id, Origin: origin, Tool: tool, Arguments: args}); err != nil {
		return Frame{}, err
	}

	timer := time.NewTimer(h.deadline)
	defer timer.Stop()
	select {
	case r := <-ch:
		return r.frame, r.err
	case <-timer.C:
		return Frame{}, ErrTimeout
	case <-ctx.Done():
		return Frame{}, ctx.Err()
	}
}

// Deliver routes a result frame to its waiting Call; false if nobody waits.
func (h *Hub) Deliver(f Frame) bool {
	if f.CallID == "" {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	p, ok := h.pending[f.CallID]
	if !ok {
		return false
	}
	delete(h.pending, f.CallID)
	p.ch <- reply{frame: f}
	return true
}
