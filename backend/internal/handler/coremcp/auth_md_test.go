// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package coremcp

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// /auth.md and /robots.txt are public on the app's host and on the mcp.
// host, where every other path is this server's MCP endpoint and needs a
// token.
func newPublicMux(t *testing.T) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "robots.txt"), []byte("User-agent: *"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := New(Params{Mux: mux, BaseURL: "https://app.agentrq.com", Domain: "agentrq.com", PublicDir: dir}); err != nil {
		t.Fatalf("New: %v", err)
	}
	return mux
}

func TestAuthMDIsServedOnBothHosts(t *testing.T) {
	mux := newPublicMux(t)

	for _, host := range []string{"app.agentrq.com", "mcp.agentrq.com"} {
		req := httptest.NewRequest(http.MethodGet, "/auth.md", nil)
		req.Host = host
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK || !strings.HasPrefix(w.Body.String(), "# AgentRQ auth.md") {
			t.Errorf("GET %s/auth.md answered %d: %.60q", host, w.Code, w.Body.String())
		}
	}
}

func TestRobotsIsPublicOnTheMCPHost(t *testing.T) {
	mux := newPublicMux(t)

	req := httptest.NewRequest(http.MethodGet, "/robots.txt", nil)
	req.Host = "mcp.agentrq.com"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK || w.Body.String() != "User-agent: *" {
		t.Errorf("GET mcp.agentrq.com/robots.txt answered %d: %.60q", w.Code, w.Body.String())
	}
}

// Without a PublicDir it reads the app's own ./public, which a test run has
// none of: the request reaches the file server rather than the MCP endpoint.
func TestRobotsDefaultsToTheAppsPublicDir(t *testing.T) {
	mux := http.NewServeMux()
	if _, err := New(Params{Mux: mux, BaseURL: "https://app.agentrq.com", Domain: "agentrq.com"}); err != nil {
		t.Fatalf("New: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/robots.txt", nil)
	req.Host = "mcp.agentrq.com"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("GET mcp.agentrq.com/robots.txt answered %d, want the file server's 404", w.Code)
	}
}
