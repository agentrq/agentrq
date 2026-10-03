// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"reflect"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestWorkspaceIcons(t *testing.T) {
	logo := mcp.Icon{Source: "https://agentrq.com/agentrq.png", MIMEType: "image/png", Sizes: []string{"512x512"}}
	tests := []struct {
		name, icon, baseURL string
		want                []mcp.Icon
	}{
		{"no icon of its own still advertises the logo", "", "https://agentrq.com", []mcp.Icon{logo}},
		{"png data URI carries its MIME type", "data:image/png;base64,AAAA", "https://agentrq.com", []mcp.Icon{
			{Source: "data:image/png;base64,AAAA", MIMEType: "image/png", Sizes: []string{"32x32"}}, logo,
		}},
		{"data URI without parameters", "data:image/jpeg,AAAA", "https://agentrq.com", []mcp.Icon{
			{Source: "data:image/jpeg,AAAA", MIMEType: "image/jpeg", Sizes: []string{"32x32"}}, logo,
		}},
		{"a URL gets no guessed MIME type", "https://example.com/i.png", "", []mcp.Icon{
			{Source: "https://example.com/i.png", Sizes: []string{"32x32"}},
		}},
		{"nothing to advertise", "", "", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := workspaceIcons(tt.icon, tt.baseURL); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("workspaceIcons(%q, %q) = %+v, want %+v", tt.icon, tt.baseURL, got, tt.want)
			}
		})
	}
}

// A client learns the icon from the server's identity, so the logo must reach
// it there and not only exist in the server's construction.
func TestServerDiscover_AdvertisesLogo(t *testing.T) {
	srv := newProtocolTestServer(t)
	_, result := discoverResult(t, srv)

	meta, _ := result["_meta"].(map[string]any)
	info, _ := meta["io.modelcontextprotocol/serverInfo"].(map[string]any)
	icons, _ := info["icons"].([]any)
	for _, ic := range icons {
		if m, _ := ic.(map[string]any); m["src"] == "http://localhost/agentrq.png" && m["mimeType"] == "image/png" {
			return
		}
	}
	t.Errorf("expected the AgentRQ logo among the server's icons, got %v", info["icons"])
}
