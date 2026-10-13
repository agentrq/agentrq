// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package telemetry

import (
	"context"
	"testing"

	mcpctrl "github.com/agentrq/agentrq/backend/internal/controller/mcp"
	"github.com/agentrq/agentrq/backend/internal/handler/coremcp"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// listedNames connects an in-process client to srv and returns every name
// MCPEvent.ToolName can carry for it: each tool by its own name, and each
// resource/prompt with the "resource:"/"prompt:" prefix emitTelemetry gives
// it (see server.go's emitTelemetry in both controller/mcp and
// handler/coremcp).
func listedNames(t *testing.T, srv *sdkmcp.Server) []string {
	t.Helper()
	ctx := context.Background()

	serverTransport, clientTransport := sdkmcp.NewInMemoryTransports()
	serverSession, err := srv.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("connect server: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })

	client, err := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test", Version: "0"}, nil).
		Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connect client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	var names []string

	tools, err := client.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	for _, tool := range tools.Tools {
		names = append(names, tool.Name)
	}

	resources, err := client.ListResources(ctx, nil)
	if err != nil {
		t.Fatalf("list resources: %v", err)
	}
	for _, r := range resources.Resources {
		names = append(names, "resource:"+r.Name)
	}

	prompts, err := client.ListPrompts(ctx, nil)
	if err != nil {
		t.Fatalf("list prompts: %v", err)
	}
	for _, p := range prompts.Prompts {
		names = append(names, "prompt:"+p.Name)
	}

	return names
}

// TestSubActionIDCoversEveryRegisteredToolResourceAndPrompt is the test that
// actually compares the map against the servers: a name added to either live
// server without a matching entry here would otherwise just record
// SubActionIDUnknown forever, silently, the same trap ActionMCPMethodCall sat
// in before this column existed.
func TestSubActionIDCoversEveryRegisteredToolResourceAndPrompt(t *testing.T) {
	// Nil-heavy on purpose: listing tools/resources/prompts never calls a
	// single handler, so nothing here is ever invoked.
	workspaceSrv := mcpctrl.NewWorkspaceServer(
		1, 0, "1", "https://agentrq.example",
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		nil, nil, nil, "", "", "", nil, nil, nil, nil,
	)
	coreSrv := coremcp.NewServer(nil, "https://agentrq.example", nil)

	seen := map[string]bool{}
	for _, name := range listedNames(t, workspaceSrv.MCPServer()) {
		seen[name] = true
	}
	for _, name := range listedNames(t, coreSrv.MCPServer()) {
		seen[name] = true
	}

	if len(seen) == 0 {
		t.Fatal("no tools, resources or prompts were listed at all")
	}

	for name := range seen {
		if _, ok := subActionIDByToolName[name]; !ok {
			t.Errorf("%q is registered on a live server but has no subActionIDByToolName entry", name)
		}
	}

	// Every constant should mean exactly one name — a duplicate value is
	// almost always a copy-paste of the wrong constant.
	byValue := map[uint8]string{}
	for name, id := range subActionIDByToolName {
		if other, dup := byValue[id]; dup {
			t.Errorf("%q and %q share the same SubActionID %d", name, other, id)
			continue
		}
		byValue[id] = name
	}
}
