// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package app

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// The mux would answer a public file path holding '..' or '//' with a
// redirect to its cleaned form; the file route has to see it as it came.
func TestPublicFilesFirst(t *testing.T) {
	mux := http.NewServeMux()
	var got string
	files := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got = "files " + r.URL.EscapedPath() })
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got = "mux " + r.URL.Path }))
	h := publicFilesFirst(mux, files)

	for path, want := range map[string]string{
		"/storage/artifacts/../agentrq.db": "files /storage/artifacts/../agentrq.db",
		"/storage//artifacts/x":            "files /storage//artifacts/x",
		"/api/v1/workspaces":               "mux /api/v1/workspaces",
		"/storagex":                        "mux /storagex",
	} {
		got = ""
		req := httptest.NewRequest(http.MethodGet, "http://agentrq.example/", nil)
		req.URL.Path, req.URL.RawPath = path, path
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if got != want {
			t.Errorf("%s: got %q (status %d), want %q", path, got, rec.Code, want)
		}
	}
}
