// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// What a client is told about each tool before it calls it.
//
// The hints are read by the thing deciding whether to ask a human first, so an
// unannotated tool is not a cosmetic gap: a client that auto-approves reads on
// sight has no way to tell `loadMemory` from `deleteMemory`. This asserts on
// the JSON rather than the Go structs, because the wire is what the client
// sees — `readOnlyHint` is a non-pointer bool and has been omitted from the
// wire by an SDK debug flag before now.
func toolsOverTheWire(t *testing.T) []map[string]any {
	t.Helper()
	srv := newProtocolTestServer(t)

	const version = "2025-06-18"
	status, out, hdr := mcpPost(t, srv.URL, map[string]string{"MCP-Protocol-Version": version},
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"`+version+
			`","capabilities":{},"clientInfo":{"name":"probe","version":"1"}}}`)
	if status != http.StatusOK {
		t.Fatalf("initialize: expected 200, got %d: %s", status, out)
	}
	session := hdr.Get("Mcp-Session-Id")
	if session == "" {
		t.Fatal("expected a session id from initialize")
	}

	status, out, _ = mcpPost(t, srv.URL,
		map[string]string{"MCP-Protocol-Version": version, "Mcp-Session-Id": session},
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`)
	if status != http.StatusOK {
		t.Fatalf("tools/list: expected 200, got %d: %s", status, out)
	}

	var env struct {
		Error  *struct{ Message string } `json:"error"`
		Result struct {
			Tools []map[string]any `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out, &env); err != nil {
		t.Fatalf("decode %s: %v", out, err)
	}
	if env.Error != nil {
		t.Fatalf("tools/list failed: %s", env.Error.Message)
	}
	if len(env.Result.Tools) == 0 {
		t.Fatal("tools/list returned nothing")
	}
	return env.Result.Tools
}

func TestEveryToolIsAnnotated(t *testing.T) {
	for _, tool := range toolsOverTheWire(t) {
		name, _ := tool["name"].(string)
		annotations, ok := tool["annotations"].(map[string]any)
		if !ok {
			t.Errorf("tool %q declares no annotations", name)
			continue
		}

		if title, _ := annotations["title"].(string); title == "" {
			t.Errorf("tool %q has no annotation title", name)
		}

		readOnly, ok := annotations["readOnlyHint"].(bool)
		if !ok {
			t.Errorf("tool %q does not say whether it is read-only", name)
			continue
		}

		// OpenAI's plugin review wants destructiveHint and openWorldHint
		// stated on every tool, a read-only one included, where it is false.
		destructive, ok := annotations["destructiveHint"].(bool)
		if !ok {
			t.Errorf("tool %q does not say whether it destroys", name)
		} else if readOnly && destructive {
			t.Errorf("tool %q is read-only and still claims to destroy", name)
		}
		if _, ok := annotations["openWorldHint"].(bool); !ok {
			t.Errorf("tool %q does not say whether its world is open", name)
		}
	}
}

// The ones that decide whether a client may run a tool unattended. A shape
// asserted in the abstract above is worth nothing if these got it wrong:
// memory and skills replace and delete, so each is destructive as a whole,
// and help only reads.
func TestMemoryToolAnnotations(t *testing.T) {
	want := map[string]struct{ readOnly, destructive bool }{
		"memory": {destructive: true},
		"skills": {destructive: true},
		"help":   {readOnly: true},
	}

	seen := map[string]bool{}
	for _, tool := range toolsOverTheWire(t) {
		name, _ := tool["name"].(string)
		expected, interesting := want[name]
		if !interesting {
			continue
		}
		seen[name] = true

		annotations, _ := tool["annotations"].(map[string]any)
		if readOnly, _ := annotations["readOnlyHint"].(bool); readOnly != expected.readOnly {
			t.Errorf("tool %q: readOnlyHint = %v, want %v", name, readOnly, expected.readOnly)
		}
		destructive, _ := annotations["destructiveHint"].(bool)
		if destructive != expected.destructive {
			t.Errorf("tool %q: destructiveHint = %v, want %v", name, destructive, expected.destructive)
		}
	}

	for name := range want {
		if !seen[name] {
			t.Errorf("tool %q was not advertised at all", name)
		}
	}
}

// The workspace's pano tool sends its agent to agentrq-ws, which keeps a doc's
// markdown and files out of the agent's context.
func TestPanoToolPointsAtTheCLI(t *testing.T) {
	for _, tool := range toolsOverTheWire(t) {
		if tool["name"] != "pano" {
			continue
		}
		if d, _ := tool["description"].(string); !strings.HasSuffix(d, PanoCLIHint) {
			t.Errorf("the pano tool's description %q does not end with the agentrq-ws hint", d)
		}
		return
	}
	t.Error("the workspace server offers no pano tool")
}

// The workspace's task tool sends its agent to agentrq-ws for attachments,
// which keeps their base64 out of the agent's context.
func TestTaskToolPointsAtTheCLI(t *testing.T) {
	for _, tool := range toolsOverTheWire(t) {
		if tool["name"] != "task" {
			continue
		}
		if d, _ := tool["description"].(string); !strings.HasSuffix(d, TaskCLIHint) {
			t.Errorf("the task tool's description %q does not end with the agentrq-ws hint", d)
		}
		return
	}
	t.Error("the workspace server offers no task tool")
}
