// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package coremcp

import (
	"encoding/json"
	"net/http"
	"reflect"
	"testing"
)

type advertisedIdentity struct {
	Instructions string `json:"instructions"`
	ServerInfo   struct {
		Name        string `json:"name"`
		Title       string `json:"title"`
		Description string `json:"description"`
		WebsiteURL  string `json:"websiteUrl"`
		Icons       []struct {
			Src      string   `json:"src"`
			MIMEType string   `json:"mimeType"`
			Sizes    []string `json:"sizes"`
		} `json:"icons"`
	} `json:"serverInfo"`
}

func checkIdentity(t *testing.T, method string, got advertisedIdentity) {
	t.Helper()
	info := got.ServerInfo
	if info.Name != "agentrq" || info.Title != serverTitle || info.Description != serverDescription || info.WebsiteURL != serverWebsiteURL {
		t.Errorf("%s: unexpected serverInfo %+v", method, info)
	}
	wantIcon := []string{"512x512"}
	if len(info.Icons) != 1 || info.Icons[0].Src != "https://agentrq.example/agentrq.png" ||
		info.Icons[0].MIMEType != "image/png" || !reflect.DeepEqual(info.Icons[0].Sizes, wantIcon) {
		t.Errorf("%s: unexpected icons %+v", method, info.Icons)
	}
	if got.Instructions != serverInstructions {
		t.Errorf("%s: instructions = %q", method, got.Instructions)
	}
}

// Directories score a server on what initialize and server/discover advertise,
// so the identity must reach the wire, not only the constructor.
func TestServerIdentity_Initialize(t *testing.T) {
	srv := newStatelessTestServer(t)
	status, out, _ := corePost(t, srv.URL, nil,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`)
	if status != http.StatusOK {
		t.Fatalf("initialize: expected 200, got %d: %s", status, out)
	}
	var env struct {
		Result advertisedIdentity `json:"result"`
	}
	if err := json.Unmarshal(out, &env); err != nil {
		t.Fatalf("decode %s: %v", out, err)
	}
	checkIdentity(t, "initialize", env.Result)
}

func TestServerIdentity_Discover(t *testing.T) {
	srv := newStatelessTestServer(t)
	status, out, _ := corePost(t, srv.URL, map[string]string{
		"MCP-Protocol-Version": "2026-07-28",
		"Mcp-Method":           "server/discover",
	}, `{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{"_meta":{`+
		`"io.modelcontextprotocol/protocolVersion":"2026-07-28",`+
		`"io.modelcontextprotocol/clientCapabilities":{}}}}`)
	if status != http.StatusOK {
		t.Fatalf("server/discover: expected 200, got %d: %s", status, out)
	}
	var env struct {
		Result struct {
			Instructions string `json:"instructions"`
			Meta         struct {
				ServerInfo json.RawMessage `json:"io.modelcontextprotocol/serverInfo"`
			} `json:"_meta"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out, &env); err != nil {
		t.Fatalf("decode %s: %v", out, err)
	}
	got := advertisedIdentity{Instructions: env.Result.Instructions}
	if err := json.Unmarshal(env.Result.Meta.ServerInfo, &got.ServerInfo); err != nil {
		t.Fatalf("decode serverInfo %s: %v", env.Result.Meta.ServerInfo, err)
	}
	checkIdentity(t, "server/discover", got)
}

// Claude Code keeps only the first 2048 characters of a server's
// instructions; this server keeps well under it, like the workspace server.
func TestServerInstructionsAreShort(t *testing.T) {
	if n := len(serverInstructions); n >= 1024 {
		t.Errorf("instructions are %d characters; keep them under 1024", n)
	}
}
