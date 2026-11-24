// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package mcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/agentrq/agentrq/backend/internal/service/mcp"
	"github.com/agentrq/agentrq/backend/internal/service/mcp/jsonrpc"
)

// pingPeer is a client that sees each of the server's pings and answers it
// the way answer says: with a result, with an error, or (nil) not at all.
func pingPeer(t *testing.T, answer func(jsonrpc.ID) *jsonrpc.Response) (*WorkspaceServer, chan struct{}) {
	t.Helper()
	ws := &WorkspaceServer{done: make(chan struct{}), mcpServer: mcp.NewServer(&mcp.Implementation{Name: "ws", Version: "0"}, nil)}
	t.Cleanup(ws.Close)
	st, ct := mcp.NewInMemoryTransports()
	ss, err := ws.mcpServer.Connect(context.Background(), st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	conn, err := ct.Connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	pinged := make(chan struct{}, 64)
	go func() {
		for {
			msg, err := conn.Read(context.Background())
			if err != nil {
				return
			}
			req, ok := msg.(*jsonrpc.Request)
			if !ok || req.Method != "ping" {
				continue
			}
			if resp := answer(req.ID); resp != nil {
				_ = conn.Write(context.Background(), resp)
			}
			pinged <- struct{}{}
		}
	}()
	return ws, pinged
}

func setPingTiming(t *testing.T, every, deadline time.Duration) {
	t.Helper()
	oldEvery, oldDeadline := pingEvery, pingDeadline
	pingEvery, pingDeadline = every, deadline
	t.Cleanup(func() { pingEvery, pingDeadline = oldEvery, oldDeadline })
}

func sessionCount(ws *WorkspaceServer) int {
	n := 0
	for range ws.mcpServer.Sessions() {
		n++
	}
	return n
}

// waitPings waits for n pings to reach the peer, and for the last one's
// handling to finish on the server.
func waitPings(t *testing.T, pinged chan struct{}, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		select {
		case <-pinged:
		case <-time.After(5 * time.Second):
			t.Fatalf("only %d of %d pings arrived", i, n)
		}
	}
	time.Sleep(50 * time.Millisecond)
}

// The deadline is 10s: 3s evicted clients that were alive but slow.
func TestPingDeadlineIsTenSeconds(t *testing.T) {
	if pingDeadline != 10*time.Second {
		t.Fatalf("pingDeadline = %v, want 10s", pingDeadline)
	}
	if pingEvery != 60*time.Second {
		t.Fatalf("pingEvery = %v, want 60s: it keeps idle push streams under proxy timeouts", pingEvery)
	}
}

// A client that answers its pings keeps its session.
func TestPingKeepsAnsweringSession(t *testing.T) {
	setPingTiming(t, 10*time.Millisecond, time.Second)
	ws, pinged := pingPeer(t, func(id jsonrpc.ID) *jsonrpc.Response {
		return &jsonrpc.Response{ID: id, Result: json.RawMessage(`{}`)}
	})
	ws.StartPing()
	waitPings(t, pinged, 3)
	if n := sessionCount(ws); n != 1 {
		t.Fatalf("an answering client was evicted: %d sessions left", n)
	}
}

// A client that answers with an error is still there, so it keeps its session.
func TestPingKeepsSessionAnsweringWithError(t *testing.T) {
	setPingTiming(t, 10*time.Millisecond, time.Second)
	ws, pinged := pingPeer(t, func(id jsonrpc.ID) *jsonrpc.Response {
		return &jsonrpc.Response{ID: id, Error: &jsonrpc.Error{Code: jsonrpc.CodeInternalError, Message: "busy"}}
	})
	ws.StartPing()
	waitPings(t, pinged, 2)
	if n := sessionCount(ws); n != 1 {
		t.Fatalf("a client that answered with an error was evicted: %d sessions left", n)
	}
}

// A client silent past the deadline is evicted.
func TestPingEvictsSilentSession(t *testing.T) {
	setPingTiming(t, 10*time.Millisecond, 20*time.Millisecond)
	ws, pinged := pingPeer(t, func(jsonrpc.ID) *jsonrpc.Response { return nil })
	ws.StartPing()
	waitPings(t, pinged, 1)
	deadline := time.Now().Add(5 * time.Second)
	for sessionCount(ws) != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := sessionCount(ws); n != 0 {
		t.Fatalf("a silent client was not evicted: %d sessions left", n)
	}
}
