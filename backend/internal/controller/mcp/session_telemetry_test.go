// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package mcp

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agentrq/agentrq/backend/internal/service/auth"
	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
	mock_pubsub "github.com/agentrq/agentrq/backend/internal/service/mocks/pubsub"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
	"github.com/golang/mock/gomock"
)

// sessionProcess is a workspace server as one backend process holds it,
// with the MCP events it published.
type sessionProcess struct {
	server *httptest.Server
	mu     sync.Mutex
	events []MCPEvent
}

func startSessionProcess(t *testing.T, tokens auth.TokenService) *sessionProcess {
	t.Helper()
	p := &sessionProcess{}
	bus := mock_pubsub.NewMockService(gomock.NewController(t))
	bus.EXPECT().Publish(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, req pubsub.PublishRequest) (*pubsub.PublishResponse, error) {
		if e, ok := req.Event.(MCPEvent); ok {
			p.mu.Lock()
			p.events = append(p.events, e)
			p.mu.Unlock()
		}
		return &pubsub.PublishResponse{}, nil
	}).AnyTimes()
	ps := NewWorkspaceServer(
		1, 0, "user-1", "http://localhost",
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		eventbus.New(), nil, nil, "icon", "name", "desc", nil, nil, tokens, bus,
	)
	t.Cleanup(ps.Close)
	p.server = httptest.NewServer(ps.Handler())
	t.Cleanup(p.server.Close)
	return p
}

// count is how many events of action were published, and the client the
// last one named.
func (p *sessionProcess) count(action Action) (int, string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	n, client := 0, ""
	for _, e := range p.events {
		if e.Action == action {
			n++
			client = e.ClientName
		}
	}
	return n, client
}

func (p *sessionProcess) post(t *testing.T, session, body string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, p.server.URL, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", "2025-06-18")
	req.Header.Set("User-Agent", "claude-code/2.1.331")
	if session != "" {
		req.Header.Set("Mcp-Session-Id", session)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, res.Body)
	_ = res.Body.Close()
	return res
}

const telemetryInitialize = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"claude-code","version":"2.1.331"}}}`

// A new session is counted as an initialize, once, and a session another
// process opened is counted as a reconnect when its client reopens its push
// stream here: two actions of their own, apart from connect.
func TestTelemetry_InitializeAndReconnect(t *testing.T) {
	tokens := auth.NewTokenService(auth.TokenConfig{APISecret: "api", MCPSecret: "mcp"})

	before := startSessionProcess(t, tokens)
	opened := before.post(t, "", telemetryInitialize)
	session := opened.Header.Get("Mcp-Session-Id")
	if opened.StatusCode != http.StatusOK || session == "" {
		t.Fatalf("initialize answered %d with session %q", opened.StatusCode, session)
	}
	before.post(t, session, telemetryInitialize) // a second handshake is refused
	if n, client := before.count(ActionMCPInitialize); n != 1 || client != "claude-code/2.1.331" {
		t.Fatalf("counted %d initializes for %q, want one for the client", n, client)
	}
	if n, _ := before.count(ActionMCPReconnect); n != 0 {
		t.Fatalf("a new session was counted as %d reconnects", n)
	}

	after := startSessionProcess(t, tokens)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, after.server.URL, nil)
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Mcp-Session-Id", session)
	req.Header.Set("User-Agent", "claude-code/2.1.331")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = res.Body.Close() })
	if line, _ := bufio.NewReader(res.Body).ReadString('\n'); res.StatusCode != http.StatusOK || line != ": ok\n" {
		t.Fatalf("reopening the push stream answered %d %q", res.StatusCode, line)
	}
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		if n, _ := after.count(ActionMCPConnect); n == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the reopened stream was not counted as a connect")
		}
	}
	if n, client := after.count(ActionMCPReconnect); n != 1 || client != "claude-code/2.1.331" {
		t.Fatalf("counted %d reconnects for %q, want one for the client", n, client)
	}
	if n, _ := after.count(ActionMCPInitialize); n != 0 {
		t.Fatalf("a carried-over session was counted as %d initializes", n)
	}
}
