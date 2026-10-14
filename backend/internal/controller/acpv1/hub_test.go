// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package acpv1

import (
	"errors"
	"fmt"
	"testing"

	"github.com/gorilla/websocket"
)

func (h *Hub) count() (all, inWS100, forUser1 int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.byID), h.perWS[100], h.perUser["user-1"]
}

func TestHub(t *testing.T) {
	t.Run("the fifth connection to one workspace is refused", func(t *testing.T) {
		h := NewHub()
		for i := range maxConnsPerWS {
			if _, err := h.admit(100, fmt.Sprintf("user-%d", i), "t", bridgeMeta{}); err != nil {
				t.Fatalf("connection %d was refused: %v", i+1, err)
			}
		}
		if _, err := h.admit(100, "user-9", "t", bridgeMeta{}); !errors.Is(err, errTooManyConnections) {
			t.Fatalf("the fifth connection returned %v, want %v", err, errTooManyConnections)
		}
		if _, err := h.admit(200, "user-9", "t", bridgeMeta{}); err != nil {
			t.Fatalf("a connection to another workspace was refused: %v", err)
		}
	})

	t.Run("the seventeenth connection for one user is refused across workspaces", func(t *testing.T) {
		h := NewHub()
		for i := range maxConnsPerUser {
			if _, err := h.admit(int64(i/maxConnsPerWS), "user-1", "t", bridgeMeta{}); err != nil {
				t.Fatalf("connection %d was refused: %v", i+1, err)
			}
		}
		if _, err := h.admit(999, "user-1", "t", bridgeMeta{}); !errors.Is(err, errTooManyConnections) {
			t.Fatalf("the seventeenth connection returned %v, want %v", err, errTooManyConnections)
		}
	})

	t.Run("a connection that ends frees its place", func(t *testing.T) {
		h := NewHub()
		var conns []*connection
		for range maxConnsPerWS {
			c, _ := h.admit(100, "user-1", "t", bridgeMeta{})
			conns = append(conns, c)
		}
		conns[0].End(errBridgeBye)
		waitFor(t, func() bool { all, _, _ := h.count(); return all == maxConnsPerWS-1 })
		if _, err := h.admit(100, "user-1", "t", bridgeMeta{}); err != nil {
			t.Fatalf("a connection after one ended was refused: %v", err)
		}
		for _, c := range conns {
			c.End(errBridgeBye)
		}
		waitFor(t, func() bool { all, _, _ := h.count(); return all == 1 })
		if _, ws, user := h.count(); ws != 1 || user != 1 {
			t.Fatalf("the hub counted %d in the workspace and %d for the user, want 1 each", ws, user)
		}
	})

	t.Run("another workspace's connection id resumes nothing", func(t *testing.T) {
		h := NewHub()
		c, _ := h.admit(100, "user-1", "t", bridgeMeta{})
		if _, ok := h.lookup(c.ID(), 200, "user-1"); ok {
			t.Error("lookup from another workspace found the connection, want unknown")
		}
		if _, ok := h.lookup(c.ID(), 100, "user-2"); ok {
			t.Error("lookup by another user found the connection, want unknown")
		}
		if got, ok := h.lookup(c.ID(), 100, "user-1"); !ok || got != c {
			t.Error("lookup by its own workspace and user missed the connection")
		}
	})

	t.Run("a replacement takes its place even at the limit, and the old one is forgotten once", func(t *testing.T) {
		h := NewHub()
		var conns []*connection
		for range maxConnsPerWS {
			c, _ := h.admit(100, "user-1", "t", bridgeMeta{})
			conns = append(conns, c)
		}
		conns[0].End(errReplaced)
		fresh := h.replace(conns[0], "t", bridgeMeta{Agent: "codex"})
		h.remove(conns[0]) // what the old connection's own watcher does
		if all, ws, user := h.count(); all != maxConnsPerWS || ws != maxConnsPerWS || user != maxConnsPerWS {
			t.Fatalf("after a replace the hub held %d (%d in the workspace, %d for the user), want %d each", all, ws, user, maxConnsPerWS)
		}
		if fresh.Meta().Agent != "codex" || fresh.WorkspaceID() != 100 || fresh.UserID() != "user-1" || fresh.ID() == conns[0].ID() {
			t.Fatalf("the replacement was %+v in workspace %d for %s, want a new id with the new headers", fresh.Meta(), fresh.WorkspaceID(), fresh.UserID())
		}
	})

	t.Run("CloseAll sends server_shutdown and ends every connection", func(t *testing.T) {
		srv, engines, h := serveHub(t)
		ws := dial(t, srv, "")
		welcomeOf(t, readFrame(t, ws))
		e := <-engines
		idle, _ := h.Hub.admit(100, "user-1", "t", bridgeMeta{}) // one with no socket

		h.Hub.CloseAll("the server is shutting down")
		got, err := decodeControl([]byte(readFrame(t, ws)))
		if err != nil || got.Kind != "error" || got.Error != "server_shutdown" || got.Message != "the server is shutting down" {
			t.Fatalf("the bridge read %+v, want a server_shutdown error frame", got)
		}
		if code := closeCodeOf(t, ws); code != websocket.CloseGoingAway {
			t.Fatalf("the socket closed with %d, want %d", code, websocket.CloseGoingAway)
		}
		for _, c := range []*connection{e.c, idle} {
			<-c.Done()
		}
		waitFor(t, func() bool { all, _, _ := h.Hub.count(); return all == 0 })
	})
}
