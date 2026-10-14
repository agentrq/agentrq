// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package acpv1

import (
	"crypto/rand"
	"errors"
	"sync"

	"github.com/gorilla/websocket"
)

var errTooManyConnections = errors.New("too many agent connections")

// Hub is the set of logical connections this process holds. Maps keyed by
// id, workspace and user give O(1) admission, lookup and removal; a
// connection is counted from admission until its End, including its grace
// window.
type Hub struct {
	mu      sync.Mutex
	byID    map[string]*connection
	perWS   map[int64]int
	perUser map[string]int
}

func NewHub() *Hub {
	return &Hub{byID: map[string]*connection{}, perWS: map[int64]int{}, perUser: map[string]int{}}
}

// admit creates and registers a connection, or refuses past the limits. The
// check and the registration share one lock so two upgrades cannot both pass.
// token is the bearer token the bridge presented, which the engine hands on to
// the agent for the workspace's MCP server.
func (h *Hub) admit(workspaceID int64, userID, token string, meta bridgeMeta) (*connection, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.perWS[workspaceID] >= maxConnsPerWS || h.perUser[userID] >= maxConnsPerUser {
		return nil, errTooManyConnections
	}
	return h.registerLocked(workspaceID, userID, token, meta), nil
}

// replace swaps a connection that could not be resumed for a fresh one in the
// same place. The resume was let in past the limits, so its replacement is.
func (h *Hub) replace(old *connection, token string, meta bridgeMeta) *connection {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.removeLocked(old)
	return h.registerLocked(old.workspaceID, old.userID, token, meta)
}

func (h *Hub) registerLocked(workspaceID int64, userID, token string, meta bridgeMeta) *connection {
	c := newConnection(rand.Text(), workspaceID, userID, meta)
	c.token = token
	h.byID[c.id] = c
	h.perWS[workspaceID]++
	h.perUser[userID]++
	go func() {
		<-c.Done()
		h.remove(c)
	}()
	return c
}

// lookup finds a connection to resume. Another workspace's or user's id is
// treated as unknown, never as an error that would confirm it exists.
func (h *Hub) lookup(id string, workspaceID int64, userID string) (*connection, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	c, ok := h.byID[id]
	if !ok || c.workspaceID != workspaceID || c.userID != userID {
		return nil, false
	}
	return c, true
}

func (h *Hub) remove(c *connection) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.removeLocked(c)
}

// removeLocked forgets c, once: replace may have done so already.
func (h *Hub) removeLocked(c *connection) {
	if h.byID[c.id] != c {
		return
	}
	delete(h.byID, c.id)
	if h.perWS[c.workspaceID]--; h.perWS[c.workspaceID] <= 0 {
		delete(h.perWS, c.workspaceID)
	}
	if h.perUser[c.userID]--; h.perUser[c.userID] <= 0 {
		delete(h.perUser, c.userID)
	}
}

// CloseAll tells every bridge the server is going away, then ends each
// connection. Called from App.Shutdown.
func (h *Hub) CloseAll(reason string) {
	h.mu.Lock()
	all := make([]*connection, 0, len(h.byID))
	for _, c := range h.byID {
		all = append(all, c)
	}
	h.mu.Unlock()
	for _, c := range all {
		c.Control(control{Kind: "error", Error: "server_shutdown", Message: reason})
		c.endWith(websocket.CloseGoingAway, reason, errors.New(reason))
	}
}
