// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package coremcp

import (
	"context"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	mcpevent "github.com/agentrq/agentrq/backend/internal/controller/mcp"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// mockServerCrud covers the workspace and task tools in server.go that had no
// test at all before telemetry was added to them — each just returns a
// minimal success response so the handler's own logic (and the emitTelemetry
// call now sitting at its top) actually runs.
type mockServerCrud struct {
	crud.Controller
}

func (m *mockServerCrud) ListWorkspaces(ctx context.Context, req entity.ListWorkspacesRequest) (*entity.ListWorkspacesResponse, error) {
	return &entity.ListWorkspacesResponse{}, nil
}

func (m *mockServerCrud) CreateWorkspace(ctx context.Context, req entity.CreateWorkspaceRequest) (*entity.CreateWorkspaceResponse, error) {
	return &entity.CreateWorkspaceResponse{Workspace: entity.Workspace{ID: testWorkspace}}, nil
}

func (m *mockServerCrud) GetWorkspace(ctx context.Context, req entity.GetWorkspaceRequest) (*entity.GetWorkspaceResponse, error) {
	return &entity.GetWorkspaceResponse{Workspace: entity.Workspace{ID: req.ID}}, nil
}

func (m *mockServerCrud) UpdateWorkspace(ctx context.Context, req entity.UpdateWorkspaceRequest) (*entity.UpdateWorkspaceResponse, error) {
	return &entity.UpdateWorkspaceResponse{Workspace: req.Workspace}, nil
}

func (m *mockServerCrud) GetDetailedWorkspaceStats(ctx context.Context, req entity.GetWorkspaceStatsRequest) (*entity.GetDetailedWorkspaceStatsResponse, error) {
	return &entity.GetDetailedWorkspaceStatsResponse{}, nil
}

func (m *mockServerCrud) ListTasks(ctx context.Context, req entity.ListTasksRequest) (*entity.ListTasksResponse, error) {
	return &entity.ListTasksResponse{}, nil
}

func (m *mockServerCrud) GetTask(ctx context.Context, req entity.GetTaskRequest) (*entity.GetTaskResponse, error) {
	return &entity.GetTaskResponse{Task: entity.Task{ID: req.TaskID, WorkspaceID: req.WorkspaceID}}, nil
}

func (m *mockServerCrud) RespondToTask(ctx context.Context, req entity.RespondToTaskRequest) (*entity.RespondToTaskResponse, error) {
	return &entity.RespondToTaskResponse{Task: entity.Task{ID: req.TaskID}}, nil
}

func (m *mockServerCrud) ReplyToTask(ctx context.Context, req entity.ReplyToTaskRequest) (*entity.ReplyToTaskResponse, error) {
	return &entity.ReplyToTaskResponse{Task: entity.Task{ID: req.TaskID}}, nil
}

func (m *mockServerCrud) UpdateTaskStatus(ctx context.Context, req entity.UpdateTaskStatusRequest) (*entity.UpdateTaskStatusResponse, error) {
	return &entity.UpdateTaskStatusResponse{Task: entity.Task{ID: req.TaskID, Status: req.Status}}, nil
}

func (m *mockServerCrud) UpdateTaskOrder(ctx context.Context, req entity.UpdateTaskOrderRequest) (*entity.UpdateTaskOrderResponse, error) {
	return &entity.UpdateTaskOrderResponse{Task: entity.Task{ID: req.TaskID}}, nil
}

func (m *mockServerCrud) UpdateTaskAssignee(ctx context.Context, req entity.UpdateTaskAssigneeRequest) (*entity.UpdateTaskAssigneeResponse, error) {
	return &entity.UpdateTaskAssigneeResponse{Task: entity.Task{ID: req.TaskID}}, nil
}

func (m *mockServerCrud) UpdateTaskAllowAllCommands(ctx context.Context, req entity.UpdateTaskAllowAllCommandsRequest) (*entity.UpdateTaskAllowAllCommandsResponse, error) {
	return &entity.UpdateTaskAllowAllCommandsResponse{Task: entity.Task{ID: req.TaskID}}, nil
}

func (m *mockServerCrud) UpdateScheduledTask(ctx context.Context, req entity.UpdateScheduledTaskRequest) (*entity.UpdateScheduledTaskResponse, error) {
	return &entity.UpdateScheduledTaskResponse{Task: entity.Task{ID: req.TaskID}}, nil
}

func (m *mockServerCrud) GetAttachment(ctx context.Context, req entity.GetAttachmentRequest) (*entity.GetAttachmentResponse, error) {
	return &entity.GetAttachmentResponse{Filename: "f"}, nil
}

// TestServerHandlers_EmitToolCallTelemetry drives every server.go handler that
// no other test file reaches, and checks each one reports exactly the tool
// call telemetry it should: the right tool name, and the workspace the call
// was actually scoped to (0 for the account-wide ones, same convention as the
// machine actions in controller/telemetry).
func TestServerHandlers_EmitToolCallTelemetry(t *testing.T) {
	cases := []struct {
		name        string
		workspaceID int64
		call        func(ctx context.Context, srv *WorkspaceServer) (*mcp.CallToolResult, any, error)
	}{
		{"listWorkspaces", 0, func(ctx context.Context, srv *WorkspaceServer) (*mcp.CallToolResult, any, error) {
			return srv.handleListWorkspaces(ctx, nil, ListWorkspacesParams{})
		}},
		{"createWorkspace", 0, func(ctx context.Context, srv *WorkspaceServer) (*mcp.CallToolResult, any, error) {
			return srv.handleCreateWorkspace(ctx, nil, CreateWorkspaceParams{Name: "n"})
		}},
		{"getWorkspace", testWorkspace, func(ctx context.Context, srv *WorkspaceServer) (*mcp.CallToolResult, any, error) {
			return srv.handleGetWorkspace(ctx, nil, GetWorkspaceParams{ID: base62(testWorkspace)})
		}},
		{"updateWorkspace", testWorkspace, func(ctx context.Context, srv *WorkspaceServer) (*mcp.CallToolResult, any, error) {
			return srv.handleUpdateWorkspace(ctx, nil, UpdateWorkspaceParams{ID: base62(testWorkspace)})
		}},
		{"getWorkspaceStats", testWorkspace, func(ctx context.Context, srv *WorkspaceServer) (*mcp.CallToolResult, any, error) {
			return srv.handleGetWorkspaceStats(ctx, nil, GetWorkspaceStatsParams{ID: base62(testWorkspace)})
		}},
		{"listTasks", testWorkspace, func(ctx context.Context, srv *WorkspaceServer) (*mcp.CallToolResult, any, error) {
			return srv.handleListTasks(ctx, nil, ListTasksParams{WorkspaceID: base62(testWorkspace)})
		}},
		{"listAllTasks", 0, func(ctx context.Context, srv *WorkspaceServer) (*mcp.CallToolResult, any, error) {
			return srv.handleListAllTasks(ctx, nil, ListAllTasksParams{})
		}},
		{"getTask", testWorkspace, func(ctx context.Context, srv *WorkspaceServer) (*mcp.CallToolResult, any, error) {
			return srv.handleGetTask(ctx, nil, GetTaskParams{WorkspaceID: base62(testWorkspace), TaskID: base62(testTriggerID)})
		}},
		{"getTaskMessages", testWorkspace, func(ctx context.Context, srv *WorkspaceServer) (*mcp.CallToolResult, any, error) {
			return srv.handleTaskMessages(ctx, nil, TaskMessagesParams{WorkspaceID: base62(testWorkspace), TaskID: base62(testTriggerID)})
		}},
		{"respondToTask", testWorkspace, func(ctx context.Context, srv *WorkspaceServer) (*mcp.CallToolResult, any, error) {
			return srv.handleRespondToTask(ctx, nil, RespondToTaskParams{WorkspaceID: base62(testWorkspace), TaskID: base62(testTriggerID), Action: "allow"})
		}},
		{"replyToTask", testWorkspace, func(ctx context.Context, srv *WorkspaceServer) (*mcp.CallToolResult, any, error) {
			return srv.handleReplyToTask(ctx, nil, ReplyToTaskParams{WorkspaceID: base62(testWorkspace), TaskID: base62(testTriggerID), Text: "hi"})
		}},
		{"updateTaskStatus", testWorkspace, func(ctx context.Context, srv *WorkspaceServer) (*mcp.CallToolResult, any, error) {
			return srv.handleUpdateTaskStatus(ctx, nil, UpdateTaskStatusParams{WorkspaceID: base62(testWorkspace), TaskID: base62(testTriggerID), Status: "ongoing"})
		}},
		{"updateTaskOrder", testWorkspace, func(ctx context.Context, srv *WorkspaceServer) (*mcp.CallToolResult, any, error) {
			return srv.handleUpdateTaskOrder(ctx, nil, UpdateTaskOrderParams{WorkspaceID: base62(testWorkspace), TaskID: base62(testTriggerID), SortOrder: 1})
		}},
		{"updateTaskAssignee", testWorkspace, func(ctx context.Context, srv *WorkspaceServer) (*mcp.CallToolResult, any, error) {
			return srv.handleUpdateTaskAssignee(ctx, nil, UpdateTaskAssigneeParams{WorkspaceID: base62(testWorkspace), TaskID: base62(testTriggerID), Assignee: "agent"})
		}},
		{"updateTaskAllowAll", testWorkspace, func(ctx context.Context, srv *WorkspaceServer) (*mcp.CallToolResult, any, error) {
			return srv.handleUpdateTaskAllowAll(ctx, nil, UpdateTaskAllowAllParams{WorkspaceID: base62(testWorkspace), TaskID: base62(testTriggerID), AllowAll: true})
		}},
		{"updateScheduledTask", testWorkspace, func(ctx context.Context, srv *WorkspaceServer) (*mcp.CallToolResult, any, error) {
			return srv.handleUpdateScheduledTask(ctx, nil, UpdateScheduledTaskParams{WorkspaceID: base62(testWorkspace), TaskID: base62(testTriggerID), Title: "t"})
		}},
		{"getAttachment", testWorkspace, func(ctx context.Context, srv *WorkspaceServer) (*mcp.CallToolResult, any, error) {
			return srv.handleGetAttachment(ctx, nil, GetAttachmentParams{WorkspaceID: base62(testWorkspace), AttachmentID: "att1"})
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got mcpevent.MCPEvent
			fps := &fakePubSub{onPublish: func(req pubsub.PublishRequest) {
				got = req.Event.(mcpevent.MCPEvent)
			}}

			srv := &WorkspaceServer{crud: &mockServerCrud{}, pubsub: fps}
			if _, _, err := tc.call(authedContext(), srv); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got.Action != mcpevent.ActionMCPToolCall {
				t.Errorf("Action = %v, want ActionMCPToolCall", got.Action)
			}
			if got.ToolName != tc.name {
				t.Errorf("ToolName = %q, want %q", got.ToolName, tc.name)
			}
			if got.WorkspaceID != tc.workspaceID {
				t.Errorf("WorkspaceID = %d, want %d", got.WorkspaceID, tc.workspaceID)
			}
		})
	}
}
