// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package coremcp

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var errStoreDown = errors.New("store unavailable")

// failingCrud fails every call the workspace and task tools make, so each
// tool's report of a failure is exercised.
type failingCrud struct {
	crud.Controller
}

func (failingCrud) ListWorkspaces(context.Context, entity.ListWorkspacesRequest) (*entity.ListWorkspacesResponse, error) {
	return nil, errStoreDown
}

func (failingCrud) CreateWorkspace(context.Context, entity.CreateWorkspaceRequest) (*entity.CreateWorkspaceResponse, error) {
	return nil, errStoreDown
}

func (failingCrud) GetWorkspace(context.Context, entity.GetWorkspaceRequest) (*entity.GetWorkspaceResponse, error) {
	return nil, errStoreDown
}

func (failingCrud) GetDetailedWorkspaceStats(context.Context, entity.GetWorkspaceStatsRequest) (*entity.GetDetailedWorkspaceStatsResponse, error) {
	return nil, errStoreDown
}

func (failingCrud) ListTasks(context.Context, entity.ListTasksRequest) (*entity.ListTasksResponse, error) {
	return nil, errStoreDown
}

func (failingCrud) CreateTask(context.Context, entity.CreateTaskRequest) (*entity.CreateTaskResponse, error) {
	return nil, errStoreDown
}

func (failingCrud) GetTask(context.Context, entity.GetTaskRequest) (*entity.GetTaskResponse, error) {
	return nil, errStoreDown
}

func (failingCrud) RespondToTask(context.Context, entity.RespondToTaskRequest) (*entity.RespondToTaskResponse, error) {
	return nil, errStoreDown
}

func (failingCrud) ReplyToTask(context.Context, entity.ReplyToTaskRequest) (*entity.ReplyToTaskResponse, error) {
	return nil, errStoreDown
}

func (failingCrud) UpdateTaskStatus(context.Context, entity.UpdateTaskStatusRequest) (*entity.UpdateTaskStatusResponse, error) {
	return nil, errStoreDown
}

func (failingCrud) UpdateTaskOrder(context.Context, entity.UpdateTaskOrderRequest) (*entity.UpdateTaskOrderResponse, error) {
	return nil, errStoreDown
}

func (failingCrud) UpdateTaskAssignee(context.Context, entity.UpdateTaskAssigneeRequest) (*entity.UpdateTaskAssigneeResponse, error) {
	return nil, errStoreDown
}

func (failingCrud) UpdateTaskAllowAllCommands(context.Context, entity.UpdateTaskAllowAllCommandsRequest) (*entity.UpdateTaskAllowAllCommandsResponse, error) {
	return nil, errStoreDown
}

func (failingCrud) UpdateScheduledTask(context.Context, entity.UpdateScheduledTaskRequest) (*entity.UpdateScheduledTaskResponse, error) {
	return nil, errStoreDown
}

// updateFailingCrud finds the workspace and fails only to save it.
type updateFailingCrud struct {
	mockServerCrud
}

func (*updateFailingCrud) UpdateWorkspace(context.Context, entity.UpdateWorkspaceRequest) (*entity.UpdateWorkspaceResponse, error) {
	return nil, errStoreDown
}

// Every tool says what failed, as a tool error the agent can read, rather than
// failing the call.
func TestServerHandlers_ReportAFailingStore(t *testing.T) {
	ws, task := base62(testWorkspace), base62(testTriggerID)
	cases := []struct {
		name string
		crud crud.Controller
		call func(ctx context.Context, srv *WorkspaceServer) (*mcp.CallToolResult, any, error)
	}{
		{"listWorkspaces", failingCrud{}, func(ctx context.Context, srv *WorkspaceServer) (*mcp.CallToolResult, any, error) {
			return srv.handleListWorkspaces(ctx, nil, ListWorkspacesParams{})
		}},
		{"createWorkspace", failingCrud{}, func(ctx context.Context, srv *WorkspaceServer) (*mcp.CallToolResult, any, error) {
			return srv.handleCreateWorkspace(ctx, nil, CreateWorkspaceParams{Name: "n"})
		}},
		{"getWorkspace", failingCrud{}, func(ctx context.Context, srv *WorkspaceServer) (*mcp.CallToolResult, any, error) {
			return srv.handleGetWorkspace(ctx, nil, GetWorkspaceParams{ID: ws})
		}},
		{"updateWorkspace, finding it", failingCrud{}, func(ctx context.Context, srv *WorkspaceServer) (*mcp.CallToolResult, any, error) {
			return srv.handleUpdateWorkspace(ctx, nil, UpdateWorkspaceParams{ID: ws})
		}},
		{"updateWorkspace, saving it", &updateFailingCrud{}, func(ctx context.Context, srv *WorkspaceServer) (*mcp.CallToolResult, any, error) {
			return srv.handleUpdateWorkspace(ctx, nil, UpdateWorkspaceParams{ID: ws})
		}},
		{"getWorkspaceStats", failingCrud{}, func(ctx context.Context, srv *WorkspaceServer) (*mcp.CallToolResult, any, error) {
			return srv.handleGetWorkspaceStats(ctx, nil, GetWorkspaceStatsParams{ID: ws})
		}},
		{"listTasks", failingCrud{}, func(ctx context.Context, srv *WorkspaceServer) (*mcp.CallToolResult, any, error) {
			return srv.handleListTasks(ctx, nil, ListTasksParams{WorkspaceID: ws})
		}},
		{"listAllTasks", failingCrud{}, func(ctx context.Context, srv *WorkspaceServer) (*mcp.CallToolResult, any, error) {
			return srv.handleListAllTasks(ctx, nil, ListAllTasksParams{})
		}},
		{"createTask", failingCrud{}, func(ctx context.Context, srv *WorkspaceServer) (*mcp.CallToolResult, any, error) {
			return srv.handleCreateTask(ctx, nil, CreateTaskParams{WorkspaceID: ws, Title: "t"})
		}},
		{"getTask", failingCrud{}, func(ctx context.Context, srv *WorkspaceServer) (*mcp.CallToolResult, any, error) {
			return srv.handleGetTask(ctx, nil, GetTaskParams{WorkspaceID: ws, TaskID: task})
		}},
		{"respondToTask", failingCrud{}, func(ctx context.Context, srv *WorkspaceServer) (*mcp.CallToolResult, any, error) {
			return srv.handleRespondToTask(ctx, nil, RespondToTaskParams{WorkspaceID: ws, TaskID: task, Action: "allow"})
		}},
		{"replyToTask", failingCrud{}, func(ctx context.Context, srv *WorkspaceServer) (*mcp.CallToolResult, any, error) {
			return srv.handleReplyToTask(ctx, nil, ReplyToTaskParams{WorkspaceID: ws, TaskID: task, Text: "hi"})
		}},
		{"updateTaskStatus", failingCrud{}, func(ctx context.Context, srv *WorkspaceServer) (*mcp.CallToolResult, any, error) {
			return srv.handleUpdateTaskStatus(ctx, nil, UpdateTaskStatusParams{WorkspaceID: ws, TaskID: task, Status: "ongoing"})
		}},
		{"updateTaskOrder", failingCrud{}, func(ctx context.Context, srv *WorkspaceServer) (*mcp.CallToolResult, any, error) {
			return srv.handleUpdateTaskOrder(ctx, nil, UpdateTaskOrderParams{WorkspaceID: ws, TaskID: task, SortOrder: 1})
		}},
		{"updateTaskAssignee", failingCrud{}, func(ctx context.Context, srv *WorkspaceServer) (*mcp.CallToolResult, any, error) {
			return srv.handleUpdateTaskAssignee(ctx, nil, UpdateTaskAssigneeParams{WorkspaceID: ws, TaskID: task, Assignee: "agent"})
		}},
		{"updateTaskAllowAll", failingCrud{}, func(ctx context.Context, srv *WorkspaceServer) (*mcp.CallToolResult, any, error) {
			return srv.handleUpdateTaskAllowAll(ctx, nil, UpdateTaskAllowAllParams{WorkspaceID: ws, TaskID: task, AllowAll: true})
		}},
		{"updateScheduledTask", failingCrud{}, func(ctx context.Context, srv *WorkspaceServer) (*mcp.CallToolResult, any, error) {
			return srv.handleUpdateScheduledTask(ctx, nil, UpdateScheduledTaskParams{WorkspaceID: ws, TaskID: task, Title: "t"})
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := &WorkspaceServer{crud: tc.crud}
			res, _, err := tc.call(authedContext(), srv)
			if err != nil {
				t.Fatalf("the call itself failed: %v", err)
			}
			if !res.IsError || toolText(res) != errStoreDown.Error() {
				t.Errorf("answered %q (error %v), want the tool error %q", toolText(res), res.IsError, errStoreDown)
			}
		})
	}
}

func toolText(res *mcp.CallToolResult) string {
	var sb strings.Builder
	for _, c := range res.Content {
		if text, ok := c.(*mcp.TextContent); ok {
			sb.WriteString(text.Text)
		}
	}
	return sb.String()
}

// workspaceRecorder keeps the workspace each write was asked to store.
type workspaceRecorder struct {
	mockServerCrud
	created, updated entity.Workspace
}

func (r *workspaceRecorder) CreateWorkspace(ctx context.Context, req entity.CreateWorkspaceRequest) (*entity.CreateWorkspaceResponse, error) {
	r.created = req.Workspace
	return r.mockServerCrud.CreateWorkspace(ctx, req)
}

func (r *workspaceRecorder) UpdateWorkspace(ctx context.Context, req entity.UpdateWorkspaceRequest) (*entity.UpdateWorkspaceResponse, error) {
	r.updated = req.Workspace
	return r.mockServerCrud.UpdateWorkspace(ctx, req)
}

// Every optional field a supervisor sets on a workspace is the one stored.
func TestWorkspaceTools_StoreEveryOptionalField(t *testing.T) {
	name, desc, note := "renamed", "a new mission", "keep notes short"
	settings := map[string]any{"taskCreated": true, "channels": []any{"email"}}

	check := func(t *testing.T, got entity.Workspace, wantName string) {
		t.Helper()
		if got.Name != wantName || got.Description != desc || got.SelfLearningLoopNote != note {
			t.Errorf("stored %q / %q / %q, want %q / %q / %q", got.Name, got.Description, got.SelfLearningLoopNote, wantName, desc, note)
		}
		ns := got.NotificationSettings
		if ns == nil || !ns.TaskCreated || len(ns.Channels) != 1 || ns.Channels[0] != "email" {
			t.Errorf("stored notification settings %+v, want task-created by email", ns)
		}
	}

	t.Run("createWorkspace", func(t *testing.T) {
		rec := &workspaceRecorder{}
		srv := &WorkspaceServer{crud: rec}
		res, _, _ := srv.handleCreateWorkspace(authedContext(), nil, CreateWorkspaceParams{
			Name: "made", Description: &desc, SelfLearningLoopNote: &note, NotificationSettings: settings,
		})
		if res.IsError {
			t.Fatalf("create failed: %s", toolText(res))
		}
		check(t, rec.created, "made")
	})

	t.Run("updateWorkspace", func(t *testing.T) {
		rec := &workspaceRecorder{}
		srv := &WorkspaceServer{crud: rec}
		res, _, _ := srv.handleUpdateWorkspace(authedContext(), nil, UpdateWorkspaceParams{
			ID: base62(testWorkspace), Name: &name, Description: &desc, SelfLearningLoopNote: &note, NotificationSettings: settings,
		})
		if res.IsError {
			t.Fatalf("update failed: %s", toolText(res))
		}
		check(t, rec.updated, name)
	})
}

// listLimitRecorder keeps the page size each listing asked for.
type listLimitRecorder struct {
	mockServerCrud
	limits []int
}

func (r *listLimitRecorder) ListTasks(ctx context.Context, req entity.ListTasksRequest) (*entity.ListTasksResponse, error) {
	r.limits = append(r.limits, req.Limit)
	return r.mockServerCrud.ListTasks(ctx, req)
}

// The account-wide listing defaults to five and never pages more than fifty.
func TestListAllTasks_PageSizeIsBounded(t *testing.T) {
	rec := &listLimitRecorder{}
	srv := &WorkspaceServer{crud: rec}
	for _, limit := range []int{0, 7, 500} {
		if res, _, _ := srv.handleListAllTasks(authedContext(), nil, ListAllTasksParams{Limit: limit}); res.IsError {
			t.Fatalf("limit %d failed: %s", limit, toolText(res))
		}
	}
	if want := []int{5, 7, 50}; len(rec.limits) != 3 || rec.limits[0] != want[0] || rec.limits[1] != want[1] || rec.limits[2] != want[2] {
		t.Errorf("listed with limits %v, want %v", rec.limits, want)
	}
}

func TestJSONResponse_ReportsAValueThatCannotBeEncoded(t *testing.T) {
	res := jsonResponse(map[string]any{"c": make(chan int)})
	if !res.IsError || !strings.Contains(toolText(res), "unsupported type") {
		t.Errorf("answered %q (error %v), want the encoding error", toolText(res), res.IsError)
	}
}

func TestParseID_AnythingButABase62StringIsZero(t *testing.T) {
	for _, in := range []any{nil, "", 42} {
		if got := parseID(in); got != 0 {
			t.Errorf("parseID(%#v) = %d, want 0", in, got)
		}
	}
	if got := parseID(base62(testWorkspace)); got != testWorkspace {
		t.Errorf("parseID(%q) = %d, want %d", base62(testWorkspace), got, testWorkspace)
	}
}

// MCPServer is the server the handler serves, for the parity tests that list
// what is really registered.
func TestMCPServer_IsTheServedServer(t *testing.T) {
	srv := NewServer(&mockEventCrud{}, "https://agentrq.example", nil)
	if srv.MCPServer() == nil || srv.MCPServer() != srv.server {
		t.Fatal("MCPServer is not the server being served")
	}
}
