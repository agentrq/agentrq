// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package coremcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/service/auth"
)

// supervisorTokens signs for real, so a token is judged by its audience
// exactly as it is in production.
var supervisorTokens = auth.NewTokenService(auth.TokenConfig{JWTSecret: "supervisor-streamable-secret"})

func mintSupervisorToken(t *testing.T, userID, resource, tokenType string) string {
	t.Helper()
	tok, err := supervisorTokens.CreateMCPToken(userID, resource, tokenType)
	if err != nil {
		t.Fatalf("minting a %s %s token: %v", resource, tokenType, err)
	}
	return tok
}

func supervisorStreamHandler() *handler {
	return &handler{
		coremcpServer: NewServer(&mockEventCrud{}, "https://agentrq.example", nil),
		tokenSvc:      supervisorTokens,
		baseURL:       "https://agentrq.example",
	}
}

const supervisorInitialize = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"0"}}}`

func supervisorStreamRequest(token string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(supervisorInitialize))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req
}

// Only a supervisor access token with a subject opens the server: a workspace's
// token, a supervisor refresh token or code, and one naming nobody are all
// challenged.
func TestSupervisorStreamable_ChallengesEverythingButASupervisorAccessToken(t *testing.T) {
	h := supervisorStreamHandler()

	cases := []struct {
		name  string
		token string
	}{
		{"a workspace's access token", mintSupervisorToken(t, "user-1", "000000003D7", "access")},
		{"a supervisor refresh token", mintSupervisorToken(t, "user-1", "coremcp", "refresh")},
		{"a supervisor authorization code", mintSupervisorToken(t, "user-1", "coremcp", "authorization_code")},
		{"a supervisor token naming nobody", mintSupervisorToken(t, "", "coremcp", "access")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			h.streamableHandler().ServeHTTP(w, supervisorStreamRequest(tc.token))

			if w.Code != http.StatusUnauthorized {
				t.Fatalf("answered %d, want 401", w.Code)
			}
			if !strings.Contains(w.Header().Get("WWW-Authenticate"), `scope="supervisor-mcp"`) {
				t.Errorf("challenge %q names no supervisor scope", w.Header().Get("WWW-Authenticate"))
			}
		})
	}
}

// A supervisor access token reaches the server, which answers the handshake.
func TestSupervisorStreamable_ASupervisorTokenReachesTheServer(t *testing.T) {
	h := supervisorStreamHandler()

	w := httptest.NewRecorder()
	h.streamableHandler().ServeHTTP(w, supervisorStreamRequest(mintSupervisorToken(t, "user-1", "coremcp", "access")))

	if w.Code != http.StatusOK {
		t.Fatalf("initialize answered %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"serverInfo"`) {
		t.Errorf("initialize answered %q, want the server's info", w.Body.String())
	}
}

// A browser on this machine may send credentials, and is told so by name; any
// other origin gets the wildcard, which a browser never sends credentials to.
func TestSupervisorCORS_CredentialsOnlyForALocalOrigin(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTeapot) })

	for _, origin := range []string{"http://localhost:5173", "http://127.0.0.1:3000", "https://localhost", "https://127.0.0.1:8443"} {
		req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
		req.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		corsWrapper(next).ServeHTTP(w, req)

		if got := w.Header().Get("Access-Control-Allow-Origin"); got != origin {
			t.Errorf("origin %s was allowed as %q", origin, got)
		}
		if w.Header().Get("Access-Control-Allow-Credentials") != "true" {
			t.Errorf("origin %s was not allowed credentials", origin)
		}
		if w.Code != http.StatusTeapot {
			t.Errorf("origin %s: the request did not reach the endpoint (%d)", origin, w.Code)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	req.Header.Set("Origin", "https://evil.example")
	w := httptest.NewRecorder()
	corsWrapper(next).ServeHTTP(w, req)
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("a remote origin was allowed as %q, want *", got)
	}
	if w.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Error("a remote origin was allowed credentials")
	}
}

// A preflight is answered by the wrapper and never reaches the endpoint.
func TestSupervisorCORS_PreflightIsAnsweredHere(t *testing.T) {
	reached := false
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true })

	w := httptest.NewRecorder()
	corsWrapper(next).ServeHTTP(w, httptest.NewRequest(http.MethodOptions, "/mcp", nil))

	if w.Code != http.StatusOK || reached {
		t.Fatalf("preflight answered %d and reached the endpoint: %v", w.Code, reached)
	}
}

// The OAuth endpoints are named under /mcp when the server shares the app's
// host, and at the root on the server's own host or a workspace subdomain.
func TestSupervisorMetadata_EndpointsFollowTheHost(t *testing.T) {
	h := &handler{baseURL: "https://agentrq.com"}

	for host, want := range map[string]string{
		"agentrq.com":        "https://agentrq.com/mcp/oauth2/token",
		"localhost:8080":     "http://localhost:8080/mcp/oauth2/token",
		"mcp.agentrq.com":    "https://mcp.agentrq.com/oauth2/token",
		"ws.mcp.agentrq.com": "https://ws.mcp.agentrq.com/oauth2/token",
	} {
		req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server", nil)
		req.Host = host
		if host == "agentrq.com" {
			req.Header.Set("X-Forwarded-Proto", "https")
		}
		w := httptest.NewRecorder()
		h.oauthMetadataHandler().ServeHTTP(w, req)

		var meta map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &meta); err != nil {
			t.Fatalf("%s: decoding %q: %v", host, w.Body.String(), err)
		}
		if meta["token_endpoint"] != want {
			t.Errorf("%s: token_endpoint %v, want %s", host, meta["token_endpoint"], want)
		}
	}
}

// On a workspace subdomain the protected resource is the origin itself.
func TestSupervisorProtectedResource_OnAWorkspaceSubdomain(t *testing.T) {
	h := &handler{baseURL: "https://agentrq.com"}
	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil)
	req.Host = "ws.mcp.agentrq.com"

	w := httptest.NewRecorder()
	h.oauthProtectedResourceHandler().ServeHTTP(w, req)

	var meta map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &meta); err != nil {
		t.Fatalf("decoding %q: %v", w.Body.String(), err)
	}
	if meta["resource"] != "https://ws.mcp.agentrq.com" {
		t.Errorf("resource %v, want the subdomain's origin", meta["resource"])
	}
}
