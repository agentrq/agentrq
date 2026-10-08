// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Package authmd serves /auth.md, which tells agents how to get access to
// both MCP servers (https://github.com/workos/auth.md).
package authmd

import (
	_ "embed"
	"net/http"
	"strings"
)

//go:embed auth.md
var document string

// Handler serves the document with its links on baseURL, so a self-hosted
// server describes itself rather than the hosted one.
func Handler(baseURL string) http.Handler {
	body := []byte(strings.ReplaceAll(document, "{{BASE}}", baseURL))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Write(body)
	})
}
