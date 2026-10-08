// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package coremcp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// /auth.md is served on the app's host and on the mcp. host, where every
// other path is this server's MCP endpoint.
func TestAuthMDIsServedOnBothHosts(t *testing.T) {
	mux := http.NewServeMux()
	if _, err := New(Params{Mux: mux, BaseURL: "https://app.agentrq.com", Domain: "agentrq.com"}); err != nil {
		t.Fatalf("New: %v", err)
	}

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
