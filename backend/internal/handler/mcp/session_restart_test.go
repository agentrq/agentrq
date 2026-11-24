// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	mcpctrl "github.com/agentrq/agentrq/backend/internal/controller/mcp"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/service/auth"
	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
	"github.com/mustafaturan/monoflake"
)

// A backend process, as far as MCP sessions go: its own manager and so its
// own workspace servers, with nothing in memory but what it was handed.
type process struct {
	manager *mcpctrl.Manager
	server  *httptest.Server
}

func startProcess(t *testing.T, tokens auth.TokenService) *process {
	t.Helper()
	listTasks := func(context.Context, mcpctrl.ListTasksFilter) ([]model.Task, error) { return nil, nil }
	manager := mcpctrl.NewManager(func(workspaceID int64, userID string) *mcpctrl.WorkspaceServer {
		return mcpctrl.NewWorkspaceServer(
			workspaceID, 0, userID, "http://localhost",
			nil, nil, nil, nil, listTasks, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
			eventbus.New(), nil, nil, "icon", "name", "desc", nil, nil, tokens, nil,
		)
	})
	mux := http.NewServeMux()
	New(Params{MCPManager: manager, TokenSvc: tokens, Crud: &accessCrud{allowed: true}, CIMD: &fakeCIMD{}, BaseURL: "https://agentrq.com", Mux: mux})
	p := &process{manager: manager, server: httptest.NewServer(mux)}
	t.Cleanup(func() {
		p.server.Close()
		manager.Remove(streamWorkspace)
	})
	return p
}

// post sends one JSON-RPC message to the workspace endpoint and returns the
// status, the session id header and the JSON-RPC message the body carried.
func (p *process) post(t *testing.T, token, session, body string) (int, string, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, p.server.URL+"/mcp/"+streamWorkspaceID, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("MCP-Protocol-Version", "2025-06-18")
	if session != "" {
		req.Header.Set("Mcp-Session-Id", session)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var msg map[string]any
	if _, data, ok := strings.Cut(string(raw), "data: "); ok {
		data, _, _ = strings.Cut(data, "\n")
		_ = json.Unmarshal([]byte(data), &msg)
	}
	return resp.StatusCode, resp.Header.Get("Mcp-Session-Id"), msg
}

// get opens the session's push stream, returning its status and response.
func (p *process) get(t *testing.T, token, session string) *http.Response {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, p.server.URL+"/mcp/"+streamWorkspaceID, nil)
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Mcp-Session-Id", session)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

// open holds the session's push stream open and returns the data lines it
// carries.
func (p *process) open(t *testing.T, token, session string) <-chan string {
	t.Helper()
	resp := p.get(t, token, session)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("the push stream answered %d, want 200", resp.StatusCode)
	}
	lines := make(chan string, 16)
	go func() {
		defer resp.Body.Close()
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			if data, ok := strings.CutPrefix(sc.Text(), "data: "); ok {
				lines <- data
			}
		}
	}()
	return lines
}

func initialize(t *testing.T, p *process, token string) string {
	t.Helper()
	status, session, msg := p.post(t, token, "", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"claude-code","version":"2.1.0"}}}`)
	if status != http.StatusOK || session == "" || msg["result"] == nil {
		t.Fatalf("initialize answered %d, session %q, %v", status, session, msg)
	}
	if status, _, _ := p.post(t, token, session, `{"jsonrpc":"2.0","method":"notifications/initialized"}`); status != http.StatusAccepted {
		t.Fatalf("initialized answered %d, want 202", status)
	}
	return session
}

// The reason sessions are signed: a client keeps its session across a
// restart. The process that opened it is gone, and a new one, which has
// never seen it, takes it back when the client reopens its push stream, as
// the client it was, and serves its calls and its pushes. A call before then
// is 404, as it always was, so a client whose push stream has given up starts
// a new session instead of carrying on without one.
func TestSession_SurvivesARestart(t *testing.T) {
	token := mintWorkspaceToken(t, streamWorkspaceID, "access")
	before := startProcess(t, streamTokens)
	session := initialize(t, before, token)
	before.server.Close()
	before.manager.Remove(streamWorkspace)

	after := startProcess(t, streamTokens)
	call := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"workspace","arguments":{"action":"get"}}}`
	if status, _, _ := after.post(t, token, session, call); status != http.StatusNotFound {
		t.Fatalf("a call before the push stream reopened answered %d, want 404", status)
	}

	pushes := after.open(t, token, session)
	ws := after.manager.Get(streamWorkspace, streamUser)
	waitFor(t, "the restored session to stream", func() bool { return ws.IsAgentConnected() })
	if agent := ws.AgentClient(); agent == nil || agent.Name != "claude-code" {
		t.Errorf("the restored session is attached as %+v, want claude-code", agent)
	}

	status, _, msg := after.post(t, token, session, call)
	if status != http.StatusOK || msg["error"] != nil {
		t.Fatalf("after the restart tools/call answered %d: %v", status, msg)
	}
	if text := toolText(msg); !strings.Contains(text, "Workspace: name") {
		t.Errorf("tools/call returned %q, want the workspace", text)
	}

	ws.SendChannelNotification(context.Background(), 42, "hello after the restart")
	select {
	case line := <-pushes:
		if !strings.Contains(line, `"method":"notifications/claude/channel"`) || !strings.Contains(line, "hello after the restart") {
			t.Errorf("pushed %s, want the channel notification", line)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the push never reached the restored session")
	}
}

// A session id is not a credential: one that verifies is still refused for a
// workspace or a user it was not opened for, and so is one that does not
// verify, each as a session the server does not know, even by the push
// stream, which is what rebuilds a session the process does not hold.
func TestSession_RefusesAnIDItDidNotOpen(t *testing.T) {
	token := mintWorkspaceToken(t, streamWorkspaceID, "access")
	p := startProcess(t, streamTokens)
	session := initialize(t, p, token)

	otherWorkspace, _ := streamTokens.CreateMCPSessionToken(context.Background(), streamUser, monoflake.ID(999).String(), auth.MCPSession{ProtocolVersion: "2025-06-18"})
	otherUser, _ := streamTokens.CreateMCPSessionToken(context.Background(), monoflake.ID(778).String(), streamWorkspaceID, auth.MCPSession{ProtocolVersion: "2025-06-18"})
	otherSecret, _ := auth.NewTokenService(auth.TokenConfig{APISecret: "x", MCPSecret: "y"}).CreateMCPSessionToken(context.Background(), streamUser, streamWorkspaceID, auth.MCPSession{})
	accessToken := mintWorkspaceToken(t, streamWorkspaceID, "access")
	forged := session[:len(session)-4] + "AAAA"

	for name, id := range map[string]string{
		"another workspace's": otherWorkspace,
		"another user's":      otherUser,
		"another secret's":    otherSecret,
		"an access token":     accessToken,
		"a forged":            forged,
	} {
		if status, _, _ := p.post(t, token, id, `{"jsonrpc":"2.0","id":3,"method":"ping"}`); status != http.StatusNotFound {
			t.Errorf("%s session id answered %d, want 404", name, status)
		}
		if status := p.get(t, token, id).StatusCode; status != http.StatusNotFound {
			t.Errorf("%s session id opened a push stream with %d, want 404", name, status)
		}
	}
	if status, _, _ := p.post(t, token, session, `{"jsonrpc":"2.0","id":3,"method":"ping"}`); status != http.StatusOK {
		t.Errorf("the session's own id answered %d, want 200", status)
	}
}

// Without the bearer token the session id opens nothing: the token is
// checked first, whatever the session.
func TestSession_IDWithoutTokenIsChallenged(t *testing.T) {
	token := mintWorkspaceToken(t, streamWorkspaceID, "access")
	p := startProcess(t, streamTokens)
	session := initialize(t, p, token)

	if status, _, _ := p.post(t, "", session, `{"jsonrpc":"2.0","id":3,"method":"ping"}`); status != http.StatusUnauthorized {
		t.Errorf("a session id without a token answered %d, want 401", status)
	}
}

// A deleted session stays deleted on the process that deleted it, even to
// its push stream.
func TestSession_DeletedIsGone(t *testing.T) {
	token := mintWorkspaceToken(t, streamWorkspaceID, "access")
	p := startProcess(t, streamTokens)
	session := initialize(t, p, token)

	req, _ := http.NewRequest(http.MethodDelete, p.server.URL+"/mcp/"+streamWorkspaceID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Mcp-Session-Id", session)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE answered %d, want 204", resp.StatusCode)
	}
	if status, _, _ := p.post(t, token, session, `{"jsonrpc":"2.0","id":3,"method":"ping"}`); status != http.StatusNotFound {
		t.Errorf("the deleted session answered %d, want 404", status)
	}
	if status := p.get(t, token, session).StatusCode; status != http.StatusNotFound {
		t.Errorf("the deleted session opened a push stream with %d, want 404", status)
	}
}

func toolText(msg map[string]any) string {
	result, _ := msg["result"].(map[string]any)
	content, _ := result["content"].([]any)
	var sb strings.Builder
	for _, c := range content {
		if block, ok := c.(map[string]any); ok {
			text, _ := block["text"].(string)
			sb.WriteString(text)
		}
	}
	return sb.String()
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
