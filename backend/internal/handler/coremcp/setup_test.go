// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package coremcp

import (
	"context"
	"encoding/json"
	"html/template"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/agentrq/agentrq/backend/internal/service/config"
)

const setupEndpoint = "https://mcp.agentrq.com/mcp"

// newSetupMux serves the supervisor as agentrq.com does, with its own host.
func newSetupMux(t *testing.T) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()
	if _, err := New(Params{Mux: mux, TokenSvc: codeTokenSvc{}, BaseURL: "https://agentrq.com", Domain: "agentrq.com", MCPURL: setupEndpoint}); err != nil {
		t.Fatal(err)
	}
	return mux
}

func setupRequest(method, host, target string) *http.Request {
	r := httptest.NewRequest(method, target, nil)
	r.Host = host
	r.Header.Set("X-Forwarded-Proto", "https")
	return r
}

func TestSetupPage_Browsers(t *testing.T) {
	mux := newSetupMux(t)
	for _, tc := range []struct {
		name, host, path, accept, endpoint string
		cookie                             bool
	}{
		{"no accept", "mcp.agentrq.com", "/mcp", "", "https://mcp.agentrq.com/mcp", false},
		{"browser", "mcp.agentrq.com", "/mcp", "text/html,application/xhtml+xml,*/*;q=0.8", "https://mcp.agentrq.com/mcp", false},
		{"host root", "mcp.agentrq.com", "/", "*/*", "https://mcp.agentrq.com/mcp", false},
		// A signed-in browser sends its session cookie, which is not a token here.
		{"app host, signed in", "agentrq.com", "/mcp", "text/html", "https://agentrq.com/mcp", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := setupRequest(http.MethodGet, tc.host, tc.path)
			if tc.accept != "" {
				r.Header.Set("Accept", tc.accept)
			}
			if tc.cookie {
				r.AddCookie(&http.Cookie{Name: "at", Value: "session"})
			}
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
			}
			if got := w.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
				t.Errorf("Content-Type = %q", got)
			}
			if got, want := w.Header().Get("Content-Security-Policy"), setupPageCSP("https://agentrq.com"); got != want {
				t.Errorf("Content-Security-Policy = %q, want %q", got, want)
			}
			if got := w.Header().Get("X-Frame-Options"); got != "DENY" {
				t.Errorf("X-Frame-Options = %q, want DENY", got)
			}
			page := w.Body.String()
			for _, want := range []string{
				"<title>AgentRQ MCP server</title>",
				`<img src="https://agentrq.com/agentrq.png" alt="">`,
				`<p class="url"><code>` + tc.endpoint + "</code></p>",
				"claude mcp add --transport http agentrq " + tc.endpoint,
				"codex mcp add agentrq --url " + tc.endpoint,
				"gemini mcp add --transport http agentrq " + tc.endpoint,
			} {
				if !strings.Contains(page, want) {
					t.Errorf("the page lacks %q", want)
				}
			}
			for _, tool := range setupTools {
				if want := "<code>" + tool.Name + "</code>: " + template.HTMLEscapeString(tool.Summary); !strings.Contains(page, want) {
					t.Errorf("the page lacks %q", want)
				}
			}
			if strings.Contains(page, "<script") {
				t.Error("the page carries a script")
			}
		})
	}

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, setupRequest(http.MethodHead, "mcp.agentrq.com", "/mcp"))
	if w.Code != http.StatusOK {
		t.Errorf("HEAD status = %d, want 200", w.Code)
	}
}

// TestSetupPage_MCPClients checks that MCP clients still get the 401 that
// starts their sign-in.
func TestSetupPage_MCPClients(t *testing.T) {
	mux := newSetupMux(t)
	for _, tc := range []struct {
		name, method, path, accept, auth string
	}{
		{"post", http.MethodPost, "/mcp", "*/*", ""},
		{"event stream", http.MethodGet, "/mcp", "text/event-stream", ""},
		{"json", http.MethodGet, "/mcp", "Application/JSON", ""},
		{"bearer token", http.MethodGet, "/mcp", "", "Bearer bad"},
		{"query token", http.MethodGet, "/mcp?token=bad", "", ""},
		{"another path", http.MethodGet, "/favicon.ico", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := setupRequest(tc.method, "mcp.agentrq.com", tc.path)
			if tc.accept != "" {
				r.Header.Set("Accept", tc.accept)
			}
			if tc.auth != "" {
				r.Header.Set("Authorization", tc.auth)
			}
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)

			if w.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", w.Code)
			}
			if challenge := w.Header().Get("WWW-Authenticate"); !strings.Contains(challenge, "resource_metadata=") {
				t.Errorf("WWW-Authenticate = %q, want a resource_metadata challenge", challenge)
			}
		})
	}
}

// TestSetupPage_NoBaseURL checks a server with no app URL shows no logo and
// allows no images.
func TestSetupPage_NoBaseURL(t *testing.T) {
	w := httptest.NewRecorder()
	(&handler{}).streamableHandler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/mcp", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if got, want := w.Header().Get("Content-Security-Policy"), setupPageCSP("'none'"); got != want {
		t.Errorf("Content-Security-Policy = %q, want %q", got, want)
	}
	if strings.Contains(w.Body.String(), "<img") {
		t.Error("the page shows a logo with no app URL to load it from")
	}
	if !strings.Contains(w.Body.String(), "claude mcp add --transport http agentrq http://example.com/mcp") {
		t.Error("the page lacks the endpoint the browser asked for")
	}
}

// TestSetupPage_ListsEveryTool keeps the page's tool list in step with the
// registered tools.
func TestSetupPage_ListsEveryTool(t *testing.T) {
	ctx := context.Background()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	ss, err := NewServer(&mockEventCrud{}, "https://agentrq.example", nil).server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })

	listed, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	var registered, shown []string
	for _, tool := range listed.Tools {
		registered = append(registered, tool.Name)
	}
	for _, tool := range setupTools {
		shown = append(shown, tool.Name)
	}
	slices.Sort(registered)
	slices.Sort(shown)
	if !slices.Equal(registered, shown) {
		t.Errorf("the setup page lists %v, the server registers %v", shown, registered)
	}
}

// TestManifest keeps the served manifest equal to the server.json published
// to the MCP Registry, except for the version, which is this release, and
// the remote, which is this server's.
func TestManifest(t *testing.T) {
	raw, err := os.ReadFile("../../../../server.json")
	if err != nil {
		t.Fatal(err)
	}
	var want map[string]any
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	want["version"] = config.Release()
	want["remotes"] = []any{map[string]any{"type": "streamable-http", "url": "https://mcp.example.com/mcp"}}

	var got map[string]any
	if err := json.Unmarshal(serverManifest("https://mcp.example.com/mcp"), &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("manifest = %v, want %v", got, want)
	}
}

// TestManifest_EveryHost checks the app and MCP hosts both publish the
// manifest, naming the MCP host's endpoint.
func TestManifest_EveryHost(t *testing.T) {
	mux := newSetupMux(t)
	for _, host := range []string{"agentrq.com", "app.agentrq.com", "mcp.agentrq.com"} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, setupRequest(http.MethodGet, host, manifestPath))

		if w.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200", host, w.Code)
		}
		if got := w.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
			t.Errorf("%s: Content-Type = %q", host, got)
		}
		if got := w.Header().Get("Access-Control-Allow-Origin"); got != "*" {
			t.Errorf("%s: Access-Control-Allow-Origin = %q, want *", host, got)
		}
		if got, want := w.Body.String(), string(serverManifest(setupEndpoint)); got != want {
			t.Errorf("%s: body = %s, want %s", host, got, want)
		}
	}
}

// OpenAI verifies a plugin submission's domain by reading this token from
// the MCP host, or a parent domain, exactly as it issued it.
func TestOpenAIChallenge_EveryHost(t *testing.T) {
	mux := http.NewServeMux()
	if _, err := New(Params{Mux: mux, TokenSvc: codeTokenSvc{}, BaseURL: "https://agentrq.com", Domain: "agentrq.com", MCPURL: setupEndpoint, OpenAIAppsChallenge: "challenge-token"}); err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{"agentrq.com", "mcp.agentrq.com"} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, setupRequest(http.MethodGet, host, openAIChallengePath))

		if w.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200", host, w.Code)
		}
		if got := w.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
			t.Errorf("%s: Content-Type = %q", host, got)
		}
		if got := w.Body.String(); got != "challenge-token" {
			t.Errorf("%s: body = %q, want the token alone", host, got)
		}
	}
}

func TestOpenAIChallenge_NoneSet(t *testing.T) {
	mux := newSetupMux(t)
	for _, host := range []string{"agentrq.com", "mcp.agentrq.com"} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, setupRequest(http.MethodGet, host, openAIChallengePath))
		if w.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", host, w.Code)
		}
	}
}
