// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package coremcp

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// What a client is told about each tool before it calls it.
//
// This server carries the destructive end of the product — deleting a task
// takes its messages and attachments with it — and a client deciding whether
// to ask a human first has only these hints to go on. A tool that declares
// none is indistinguishable from a read.
func listedTools(t *testing.T) []*mcp.Tool {
	t.Helper()
	srv := NewServer(&mockEventCrud{}, "https://agentrq.example", nil)

	ctx := context.Background()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := srv.server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("connect server: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })

	clientSession, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).
		Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connect client: %v", err)
	}
	t.Cleanup(func() { _ = clientSession.Close() })

	listed, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	if len(listed.Tools) == 0 {
		t.Fatal("tools/list returned nothing")
	}
	return listed.Tools
}

func TestEveryToolIsAnnotated(t *testing.T) {
	for _, tool := range listedTools(t) {
		if tool.Annotations == nil {
			t.Errorf("tool %q declares no annotations", tool.Name)
			continue
		}
		if tool.Annotations.Title == "" {
			t.Errorf("tool %q has no annotation title", tool.Name)
		}

		// destructiveHint means nothing on a read-only tool, and everything on
		// the rest: without it the protocol's default applies, which is that
		// the tool destroys things.
		declared := tool.Annotations.DestructiveHint != nil
		if tool.Annotations.ReadOnlyHint && declared {
			t.Errorf("tool %q is read-only and still declares destructiveHint", tool.Name)
		}
		if !tool.Annotations.ReadOnlyHint && !declared {
			t.Errorf("tool %q writes but does not say whether it destroys", tool.Name)
		}
	}
}

// Named one by one, because the shape asserted above is only as good as the
// tools it was applied to. These are where getting it wrong costs something:
// task and workflows can destroy what cannot be undone (a delete action
// takes the tool's reads down with it), and memory is a read a client is
// likely to run unattended.
func TestConsequentialToolAnnotations(t *testing.T) {
	want := map[string]struct{ readOnly, destructive bool }{
		"task":      {destructive: true},
		"workflows": {destructive: true},
		"memory":    {readOnly: true},
	}

	seen := map[string]bool{}
	for _, tool := range listedTools(t) {
		expected, interesting := want[tool.Name]
		if !interesting {
			continue
		}
		seen[tool.Name] = true

		if tool.Annotations.ReadOnlyHint != expected.readOnly {
			t.Errorf("tool %q: readOnlyHint = %v, want %v",
				tool.Name, tool.Annotations.ReadOnlyHint, expected.readOnly)
		}
		destructive := tool.Annotations.DestructiveHint != nil && *tool.Annotations.DestructiveHint
		if destructive != expected.destructive {
			t.Errorf("tool %q: destructiveHint = %v, want %v", tool.Name, destructive, expected.destructive)
		}
	}

	for name := range want {
		if !seen[name] {
			t.Errorf("tool %q was not advertised at all", name)
		}
	}
}
