// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	mcpctrl "github.com/agentrq/agentrq/backend/internal/controller/mcp"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/service/auth"
	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
	"github.com/mustafaturan/monoflake"
)

// streamWorkspace is the workspace every request in this file is addressed to.
const streamWorkspace = int64(12345)

var streamWorkspaceID = monoflake.ID(streamWorkspace).String()

// streamUser owns streamWorkspace.
var streamUser = monoflake.ID(777).String()

// streamTokens signs for real, so a workspace token is judged by its audience
// exactly as it is in production.
var streamTokens = auth.NewTokenService(auth.TokenConfig{JWTSecret: "streamable-test-secret"})

func mintWorkspaceToken(t *testing.T, workspaceID, tokenType string) string {
	t.Helper()
	tok, err := streamTokens.CreateMCPToken(streamUser, workspaceID, tokenType)
	if err != nil {
		t.Fatalf("minting a %s token: %v", tokenType, err)
	}
	return tok
}

// accessCrud answers the workspace access check the way it is told to.
type accessCrud struct {
	mockCrud
	allowed bool
	err     error
}

func (c *accessCrud) CheckWorkspaceAccess(ctx context.Context, id int64, userID string) (bool, error) {
	return c.allowed, c.err
}

// streamRouter mounts the handler over a real workspace server, and reports how
// many times that server looked for the task a permission request belongs to —
// which is what a relayed channel notification does first.
func streamRouter(t *testing.T, tokens auth.TokenService, crud *accessCrud) (*http.ServeMux, *atomic.Int32) {
	t.Helper()
	var lookups atomic.Int32
	listTasks := func(context.Context, mcpctrl.ListTasksFilter) ([]model.Task, error) {
		lookups.Add(1)
		return nil, nil
	}
	manager := mcpctrl.NewManager(func(workspaceID int64, userID string) *mcpctrl.WorkspaceServer {
		return mcpctrl.NewWorkspaceServer(
			workspaceID, 0, userID, "http://localhost",
			nil, nil, nil, nil, listTasks, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
			eventbus.New(), nil, nil, "icon", "name", "desc", nil, nil, nil, nil,
		)
	})
	t.Cleanup(func() { manager.Remove(streamWorkspace) })

	mux := http.NewServeMux()
	New(Params{
		MCPManager: manager,
		TokenSvc:   tokens,
		Crud:       crud,
		CIMD:       &fakeCIMD{},
		BaseURL:    "https://agentrq.com",
		Mux:        mux,
	})
	return mux, &lookups
}

const initializeBody = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"0"}}}`

func streamRequest(method, target, body string) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	return req
}

// A preflight is answered by the CORS wrapper itself and never reaches the
// endpoint, which would refuse it for carrying no token.
func TestStreamable_PreflightIsAnsweredByTheCORSWrapper(t *testing.T) {
	mux, _ := streamRouter(t, streamTokens, &accessCrud{allowed: true})

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodOptions, "/mcp/"+streamWorkspaceID, nil))

	if w.Code != http.StatusOK {
		t.Fatalf("preflight answered %d, want 200", w.Code)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Access-Control-Allow-Origin = %q, want *", got)
	}
	if w.Body.Len() != 0 {
		t.Errorf("preflight carried a body: %q", w.Body.String())
	}
}

// Neither the path nor the host names a workspace, so there is nothing to
// authorise against.
func TestWorkspaceIDFromParam_NoWorkspaceIsZero(t *testing.T) {
	for _, host := range []string{"agentrq.com", "app.agentrq.com", "zz!.mcp.agentrq.com"} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Host = host
		if got := workspaceIDFromParam(req); got != 0 {
			t.Errorf("host %q: workspace %d, want 0", host, got)
		}
	}
}

func TestStreamable_RefusesAnUnnamedWorkspace(t *testing.T) {
	h := &handler{tokenSvc: streamTokens, crud: &accessCrud{allowed: true}}
	req := streamRequest(http.MethodPost, "/", initializeBody)
	req.Host = "agentrq.com"

	w := httptest.NewRecorder()
	h.streamableHandler().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "invalid workspace id") {
		t.Fatalf("answered %d %q, want 400 invalid workspace id", w.Code, w.Body.String())
	}
}

// Only an access token for this very workspace opens it: another workspace's,
// a refresh token and one that does not verify are all challenged, wherever
// the token is carried.
func TestStreamable_ChallengesATokenNotMintedForThisWorkspace(t *testing.T) {
	mux, _ := streamRouter(t, streamTokens, &accessCrud{allowed: true})
	other := monoflake.ID(999).String()

	cases := []struct {
		name  string
		token string
	}{
		{"another workspace's access token", mintWorkspaceToken(t, other, "access")},
		{"this workspace's refresh token", mintWorkspaceToken(t, streamWorkspaceID, "refresh")},
		{"a token that does not verify", "not-a-token"},
	}
	for _, tc := range cases {
		for _, carrier := range []string{"query", "header"} {
			t.Run(tc.name+" in the "+carrier, func(t *testing.T) {
				target := "/mcp/" + streamWorkspaceID
				if carrier == "query" {
					target += "?token=" + tc.token
				}
				req := streamRequest(http.MethodPost, target, initializeBody)
				if carrier == "header" {
					req.Header.Set("Authorization", "Bearer "+tc.token)
				}

				w := httptest.NewRecorder()
				mux.ServeHTTP(w, req)

				if w.Code != http.StatusUnauthorized {
					t.Fatalf("answered %d, want 401", w.Code)
				}
				if !strings.Contains(w.Body.String(), "situational security: unauthorized") {
					t.Errorf("body %q does not say unauthorized", w.Body.String())
				}
				if !strings.Contains(w.Header().Get("WWW-Authenticate"), "resource_metadata=") {
					t.Errorf("challenge %q names no resource metadata", w.Header().Get("WWW-Authenticate"))
				}
			})
		}
	}
}

// A valid token for a workspace the person can no longer reach is forbidden,
// and so is one whose access cannot be checked at all.
func TestStreamable_ForbidsAWorkspaceTheUserCannotReach(t *testing.T) {
	for _, tc := range []struct {
		name string
		crud *accessCrud
	}{
		{"access denied", &accessCrud{allowed: false}},
		{"access check failed", &accessCrud{allowed: true, err: errors.New("database unavailable")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mux, _ := streamRouter(t, streamTokens, tc.crud)
			req := streamRequest(http.MethodPost, "/mcp/"+streamWorkspaceID, initializeBody)
			req.Header.Set("Authorization", "Bearer "+mintWorkspaceToken(t, streamWorkspaceID, "access"))

			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "situational security: forbidden") {
				t.Fatalf("answered %d %q, want 403 forbidden", w.Code, w.Body.String())
			}
		})
	}
}

// The workspace's own access token reaches its MCP server, which opens a
// session for it.
func TestStreamable_AWorkspaceTokenReachesItsServer(t *testing.T) {
	mux, _ := streamRouter(t, streamTokens, &accessCrud{allowed: true})
	req := streamRequest(http.MethodPost, "/mcp/"+streamWorkspaceID, initializeBody)
	req.Header.Set("Authorization", "Bearer "+mintWorkspaceToken(t, streamWorkspaceID, "access"))

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("initialize answered %d: %s", w.Code, w.Body.String())
	}
	if w.Header().Get("Mcp-Session-Id") == "" {
		t.Error("initialize opened no session")
	}
	if !strings.Contains(w.Body.String(), `"serverInfo"`) {
		t.Errorf("initialize answered %q, want the server's info", w.Body.String())
	}
}

// onceTokens verifies a token the first time it is asked and refuses it after,
// as happens when it expires between the two checks the handler makes.
type onceTokens struct {
	auth.TokenService
	calls atomic.Int32
}

func (o *onceTokens) ValidateToken(tokenStr string) (*auth.Claims, error) {
	if o.calls.Add(1) > 1 {
		return nil, errors.New("token has expired")
	}
	return o.TokenService.ValidateToken(tokenStr)
}

// A token that stops verifying after the user was identified still goes
// through, carrying that user, rather than reaching the server unidentified.
func TestStreamable_ATokenThatLapsesMidRequestStillCarriesItsUser(t *testing.T) {
	tokens := &onceTokens{TokenService: streamTokens}
	mux, _ := streamRouter(t, tokens, &accessCrud{allowed: true})
	req := streamRequest(http.MethodPost, "/mcp/"+streamWorkspaceID+"?token="+mintWorkspaceToken(t, streamWorkspaceID, "access"), initializeBody)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("initialize answered %d: %s", w.Code, w.Body.String())
	}
	if got := tokens.calls.Load(); got != 2 {
		t.Errorf("the token was checked %d times, want 2", got)
	}
}

// A channel notification the SDK would refuse is handed to the workspace
// server directly and answered with an empty 200.
func TestStreamable_ACustomChannelNotificationIsHandedToTheServer(t *testing.T) {
	mux, lookups := streamRouter(t, streamTokens, &accessCrud{allowed: true})
	body := `{"jsonrpc":"2.0","method":"notifications/claude/channel/permission_request","params":{"request_id":"r1","tool_name":"Bash"}}`
	req := streamRequest(http.MethodPost, "/mcp/"+streamWorkspaceID, body)
	req.Header.Set("Authorization", "Bearer "+mintWorkspaceToken(t, streamWorkspaceID, "access"))
	req.Header.Set("Mcp-Session-Id", "session-1")

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("notification answered %d: %s", w.Code, w.Body.String())
	}
	if w.Body.Len() != 0 {
		t.Errorf("notification answered with a body: %q", w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	if lookups.Load() == 0 {
		t.Error("the workspace server never looked for the request's task; the notification did not reach it")
	}
}
