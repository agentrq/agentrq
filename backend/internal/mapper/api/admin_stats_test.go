// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import "testing"

func TestFromAdminStatsToHTTPResponse(t *testing.T) {
	for name, tc := range map[string]struct {
		mcp, machines map[string]int
		want          string
	}{
		"one row per user, busiest first": {
			mcp:      map[string]int{"bob": 1, "alice": 3, "aaron": 1},
			machines: map[string]int{"bob": 2, "carol": 1, "alice": 1},
			want: `{"mcpConnections":5,"machines":4,"users":[` +
				`{"userId":"alice","mcpConnections":3,"machines":1},` +
				`{"userId":"bob","mcpConnections":1,"machines":2},` +
				`{"userId":"aaron","mcpConnections":1,"machines":0},` +
				`{"userId":"carol","mcpConnections":0,"machines":1}]}`,
		},
		"nothing is an empty list, not null": {
			want: `{"mcpConnections":0,"machines":0,"users":[]}`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			if got := string(FromAdminStatsToHTTPResponse(tc.mcp, tc.machines)); got != tc.want {
				t.Errorf("got %s, want %s", got, tc.want)
			}
		})
	}
}
