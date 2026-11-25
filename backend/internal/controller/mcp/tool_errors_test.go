// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package mcp

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/service/mcp"
	"github.com/golang/mock/gomock"

	"github.com/agentrq/agentrq/backend/internal/controller/agentlink"
	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
	mock_pubsub "github.com/agentrq/agentrq/backend/internal/service/mocks/pubsub"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
)

// connectTestServer connects a client to srv over in-memory transports.
func connectTestServer(t *testing.T, srv *mcp.Server) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// A call that fails is counted once, under the name it was counted as when
// it ran, or the tool it named when it never got that far; one that succeeds,
// and anything that is not a tool call, is not.
func TestToolErrorsMiddleware(t *testing.T) {
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	mcp.AddTool(srv, &mcp.Tool{Name: "ok"}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		NoteToolCall(ctx, "getTask", 7)
		return &mcp.CallToolResult{}, nil, nil
	})
	mcp.AddTool(srv, &mcp.Tool{Name: "task"}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		NoteToolCall(ctx, "getTask", 7)
		return nil, nil, errors.New("database unavailable")
	})
	mcp.AddTool(srv, &mcp.Tool{Name: "refused"}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, any, error) {
		return toolError("read-only"), nil, nil
	})
	var got []ToolCall
	srv.AddReceivingMiddleware(ToolErrorsMiddleware(func(_ context.Context, req *mcp.CallToolRequest, call ToolCall) {
		if req == nil {
			t.Error("emitted without the request")
		}
		got = append(got, call)
	}))
	cs := connectTestServer(t, srv)
	ctx := context.Background()

	if _, err := cs.ListTools(ctx, nil); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"ok", "task", "refused", "noSuchTool"} {
		_, _ = cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: map[string]any{}})
	}
	want := []ToolCall{{Name: "getTask", WorkspaceID: 7}, {Name: "refused"}, {Name: "noSuchTool"}}
	if !slices.Equal(got, want) {
		t.Errorf("counted %+v, want %+v", got, want)
	}
}

// A call with no parameters is no tool call to count; nor is noting a call
// outside the middleware anything but a no-op.
func TestToolErrorsMiddleware_NoParams(t *testing.T) {
	NoteToolCall(context.Background(), "getTask", 1)
	next := func(context.Context, string, mcp.Request) (mcp.Result, error) { return nil, errors.New("bad request") }
	h := ToolErrorsMiddleware(func(context.Context, *mcp.CallToolRequest, ToolCall) {
		t.Error("counted a call with no parameters")
	})(next)
	if _, err := h(context.Background(), "tools/call", &mcp.CallToolRequest{}); err == nil {
		t.Error("the handler's error was lost")
	}
}

// The workspace server counts a failed call of its tools under the action's
// name, with the calling client, and a call that succeeds not at all.
func TestWorkspaceServer_CountsToolErrors(t *testing.T) {
	ctrl := gomock.NewController(t)
	pub := mock_pubsub.NewMockService(ctrl)
	var events []MCPEvent
	pub.EXPECT().Publish(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, req pubsub.PublishRequest) (*pubsub.PublishResponse, error) {
		events = append(events, req.Event.(MCPEvent))
		return nil, nil
	}).AnyTimes()
	ps := &WorkspaceServer{workspaceID: 100, userID: "1", pubsub: pub}
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	mcp.AddTool(srv, &mcp.Tool{Name: "task"}, func(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		ps.emitTelemetry(ctx, ActionMCPToolCall, "getTask", clientIdentityFromRequest(req))
		return toolError("task not found"), nil, nil
	})
	srv.AddReceivingMiddleware(ToolErrorsMiddleware(ps.emitToolError))
	cs := connectTestServer(t, srv)

	if _, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "task", Arguments: map[string]any{}}); err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Action != ActionMCPToolCall {
		t.Fatalf("published %+v, want a tool call and then its error", events)
	}
	if e := events[1]; e.Action != ActionMCPToolCallError || e.ToolName != "getTask" || e.WorkspaceID != 100 || e.ClientName != "test" {
		t.Errorf("published %+v, want test's getTask error in workspace 100", e)
	}
}

// The workspace server registers the middleware outermost, so a call its
// action tools refuse is counted.
func TestWorkspaceServer_RegistersToolErrors(t *testing.T) {
	ctrl := gomock.NewController(t)
	pub := mock_pubsub.NewMockService(ctrl)
	var errs []string
	pub.EXPECT().Publish(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, req pubsub.PublishRequest) (*pubsub.PublishResponse, error) {
		if ev, ok := req.Event.(MCPEvent); ok && ev.Action == ActionMCPToolCallError {
			errs = append(errs, ev.ToolName)
		}
		return nil, nil
	}).AnyTimes()
	ps := NewWorkspaceServer(
		1, 0, "user", "http://localhost",
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		eventbus.New(), nil, nil, "icon", "name", "desc", nil, nil, nil, nil,
	)
	ps.pubsub = pub
	cs := connectTestServer(t, ps.MCPServer())

	if _, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "task", Arguments: map[string]any{"action": "nope"}}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(errs, []string{"task"}) {
		t.Errorf("counted errors %v, want the task tool's", errs)
	}
}

// A call of the agent's own tools is counted when it fails, whether it was
// recorded failed or moved to it, and not for any other status.
func TestWorkspaceServer_CountsAgentToolFailures(t *testing.T) {
	ctrl := gomock.NewController(t)
	pub := mock_pubsub.NewMockService(ctrl)
	var fails int
	pub.EXPECT().Publish(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, req pubsub.PublishRequest) (*pubsub.PublishResponse, error) {
		ev := req.Event.(MCPEvent)
		if ev.Action != ActionMCPAgentToolCallFail || ev.WorkspaceID != 100 || ev.ToolName != "" || ev.Actor != 2 {
			t.Errorf("published %+v, want an unnamed agent tool failure in workspace 100", ev)
		}
		fails++
		return nil, nil
	}).AnyTimes()
	ps := &WorkspaceServer{workspaceID: 100, userID: "1", pubsub: pub}
	ctx := context.Background()

	ps.RecordToolCall(ctx, 42, agentlink.ToolCall{ToolName: "Bash", Status: "running"})
	ps.RecordToolCall(ctx, 42, agentlink.ToolCall{ToolName: "Bash", Status: "failed"})
	ps.SetToolCallStatus(ctx, 1, "completed", "")
	ps.SetToolCallStatus(ctx, 2, "failed", "exit status 1")
	if fails != 2 {
		t.Errorf("counted %d failures, want 2", fails)
	}
}
