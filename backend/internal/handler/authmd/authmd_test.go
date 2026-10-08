// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package authmd

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func serve(method string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	Handler("https://selfhosted.example").ServeHTTP(w, httptest.NewRequest(method, "/auth.md", nil))
	return w
}

func TestAuthMDDescribesThisServer(t *testing.T) {
	w := serve(http.MethodGet)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /auth.md answered %d", w.Code)
	}
	if got := w.Header().Get("Content-Type"); got != "text/markdown; charset=utf-8" {
		t.Errorf("Content-Type = %q, want markdown", got)
	}
	body := w.Body.String()
	// Scanners recognise the document by an H1 that names it.
	if first := strings.SplitN(body, "\n", 2)[0]; !strings.HasPrefix(first, "# ") || !strings.Contains(first, "auth.md") {
		t.Errorf("first line = %q, want an H1 containing auth.md", first)
	}
	if strings.Contains(body, "{{BASE}}") {
		t.Error("the document still has an unfilled {{BASE}}")
	}
	for _, want := range []string{
		"`https://selfhosted.example/mcp/{workspaceId}`",
		"`https://selfhosted.example/mcp`",
		"`https://selfhosted.example/.well-known/oauth-authorization-server`",
		"`mcp`",
		"`supervisor-mcp`",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the document does not mention %s", want)
		}
	}
}

func TestAuthMDAnswersHeadButNotPost(t *testing.T) {
	if w := serve(http.MethodHead); w.Code != http.StatusOK {
		t.Errorf("HEAD /auth.md answered %d, want 200", w.Code)
	}
	w := serve(http.MethodPost)
	if w.Code != http.StatusMethodNotAllowed || w.Header().Get("Allow") != "GET, HEAD" {
		t.Errorf("POST /auth.md answered %d with Allow %q, want 405 with GET, HEAD", w.Code, w.Header().Get("Allow"))
	}
}
