// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package coremcp

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	mcpevent "github.com/agentrq/agentrq/backend/internal/controller/mcp"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
	"github.com/mustafaturan/monoflake"
)

// fakePubSub is a hand-written pubsub.Service, not the generated gomock: this
// package's test files must compile without `make mocks` having run, since
// the "Documented tools match the servers" CI job runs `go test
// ./internal/handler/coremcp/ -run TestPluginDocs` directly, with no mock
// generation step first.
type fakePubSub struct {
	pubsub.Service
	onPublish func(req pubsub.PublishRequest)
}

func (f *fakePubSub) Publish(_ context.Context, req pubsub.PublishRequest) (*pubsub.PublishResponse, error) {
	if f.onPublish != nil {
		f.onPublish(req)
	}
	return nil, nil
}

// A WorkspaceServer built without a pubsub (the shape every hand-built test
// fixture in this package takes, e.g. eventServer) must not panic when a
// handler still calls emitTelemetry.
func TestEmitTelemetry_NilPubSubIsANoop(t *testing.T) {
	srv := &WorkspaceServer{}
	srv.emitTelemetry(authedContext(), mcpevent.ActionMCPToolCall, "getWorkspace", testWorkspace)
}

func TestEmitTelemetry_PublishesOnMCPTopic(t *testing.T) {
	var got mcpevent.MCPEvent
	var gotTopic int64
	fps := &fakePubSub{onPublish: func(req pubsub.PublishRequest) {
		gotTopic = req.PubSubID
		got = req.Event.(mcpevent.MCPEvent)
	}}

	srv := &WorkspaceServer{pubsub: fps}
	srv.emitTelemetry(authedContext(), mcpevent.ActionMCPToolCall, "getWorkspace", testWorkspace)

	if gotTopic != entity.PubSubTopicMCP {
		t.Errorf("PubSubID = %d, want PubSubTopicMCP", gotTopic)
	}
	if got.Action != mcpevent.ActionMCPToolCall {
		t.Errorf("Action = %v, want ActionMCPToolCall", got.Action)
	}
	if got.WorkspaceID != testWorkspace {
		t.Errorf("WorkspaceID = %d, want %d", got.WorkspaceID, testWorkspace)
	}
	if got.ToolName != "getWorkspace" || got.Method != "getWorkspace" {
		t.Errorf("ToolName/Method = %q/%q, want %q", got.ToolName, got.Method, "getWorkspace")
	}
	if got.Actor != 2 {
		t.Errorf("Actor = %d, want 2 (agent)", got.Actor)
	}
	if want := monoflake.IDFromBase62(testUserID).Int64(); got.UserID != want {
		t.Errorf("UserID = %d, want %d", got.UserID, want)
	}
}

// Resource and prompt reads are reported as method calls, not tool calls, so
// they don't inflate the same count a supervisor's tool use does.
func TestEmitTelemetry_MethodCallAction(t *testing.T) {
	var got mcpevent.MCPEvent
	fps := &fakePubSub{onPublish: func(req pubsub.PublishRequest) {
		got = req.Event.(mcpevent.MCPEvent)
	}}

	srv := &WorkspaceServer{pubsub: fps}
	srv.emitTelemetry(authedContext(), mcpevent.ActionMCPMethodCall, "resource:new-workspace-guide", 0)

	if got.Action != mcpevent.ActionMCPMethodCall {
		t.Errorf("Action = %v, want ActionMCPMethodCall", got.Action)
	}
	if got.WorkspaceID != 0 {
		t.Errorf("WorkspaceID = %d, want 0 (resources/prompts have no single workspace)", got.WorkspaceID)
	}
}

// A failed call is counted under the name its call was counted as, with its
// workspace; one refused before it was counted, and a tool that does not
// exist, under the name it was called by.
func TestToolErrors_CountedThroughTheServer(t *testing.T) {
	var errs []mcpevent.MCPEvent
	fps := &fakePubSub{onPublish: func(req pubsub.PublishRequest) {
		if ev := req.Event.(mcpevent.MCPEvent); ev.Action == mcpevent.ActionMCPToolCallError {
			errs = append(errs, ev)
		}
	}}
	srv := NewServer(&taskCrud{}, "https://agentrq.example", fps)
	ctx := context.Background()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	ss, err := srv.server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })

	// No user in the context, so each call that reaches its handler fails.
	_, _ = cs.CallTool(ctx, &mcp.CallToolParams{Name: "workspace", Arguments: map[string]any{"action": "list"}})
	_, _ = cs.CallTool(ctx, &mcp.CallToolParams{Name: "launchAgent", Arguments: map[string]any{"workspaceId": base62(testWorkspace), "machineId": base62(1), "kind": "bogus"}})
	// A payload refused before its action runs counts under the tool.
	_, _ = cs.CallTool(ctx, &mcp.CallToolParams{Name: "launchAgent", Arguments: map[string]any{"workspaceId": base62(testWorkspace)}})
	_, _ = cs.CallTool(ctx, &mcp.CallToolParams{Name: "help", Arguments: map[string]any{}})
	_, _ = cs.CallTool(ctx, &mcp.CallToolParams{Name: "noSuchTool", Arguments: map[string]any{}})

	var got []string
	for _, e := range errs {
		got = append(got, fmt.Sprintf("%s@%d", e.ToolName, e.WorkspaceID))
	}
	if want := []string{"listWorkspaces@0", fmt.Sprintf("launchAgent@%d", testWorkspace), "machines@0", "noSuchTool@0"}; !slices.Equal(got, want) {
		t.Errorf("counted errors %v, want %v", got, want)
	}
}
