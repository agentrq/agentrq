// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package coremcp

import (
	"context"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
)

// mockTaskCrud isolates the createTask tool from the rest of crud.Controller,
// the same way mockEventCrud does for the event tools.
type mockTaskCrud struct {
	crud.Controller

	createTask func(ctx context.Context, req entity.CreateTaskRequest) (*entity.CreateTaskResponse, error)
	listTasks  func(ctx context.Context, req entity.ListTasksRequest) (*entity.ListTasksResponse, error)
	getTask    func(ctx context.Context, req entity.GetTaskRequest) (*entity.GetTaskResponse, error)
}

func (m *mockTaskCrud) GetTask(ctx context.Context, req entity.GetTaskRequest) (*entity.GetTaskResponse, error) {
	return m.getTask(ctx, req)
}

func (m *mockTaskCrud) CreateTask(ctx context.Context, req entity.CreateTaskRequest) (*entity.CreateTaskResponse, error) {
	return m.createTask(ctx, req)
}

func (m *mockTaskCrud) ListTasks(ctx context.Context, req entity.ListTasksRequest) (*entity.ListTasksResponse, error) {
	return m.listTasks(ctx, req)
}

// delegatingWorkspace is the workspace that hands a task to testWorkspace.
const delegatingWorkspace = int64(301)

func taskServer(ctrl *mockTaskCrud) *WorkspaceServer {
	return &WorkspaceServer{crud: ctrl}
}

// The supervisor writes full context into a task's body before handing it to
// another workspace, so the receiving agent should be able to ask for a clean
// slate the same way a workspace's own createTask tool already can.
func TestCreateTask_PassesClearContextThrough(t *testing.T) {
	var got entity.CreateTaskRequest
	ctrl := &mockTaskCrud{createTask: func(_ context.Context, req entity.CreateTaskRequest) (*entity.CreateTaskResponse, error) {
		got = req
		return &entity.CreateTaskResponse{Task: entity.Task{ID: testWorkspace, ClearContext: req.Task.ClearContext}}, nil
	}}

	textOf(t, toolResult(taskServer(ctrl).handleCreateTask(authedContext(), nil, CreateTaskParams{
		WorkspaceID:  base62(testWorkspace),
		Title:        "Wire up the new gateway",
		ClearContext: true,
	})))

	if !got.Task.ClearContext {
		t.Errorf("expected ClearContext to be carried through to the create request")
	}
}

// Omitting the field must not be silently coerced to true: a caller that
// says nothing gets the target workspace's own default, applied downstream
// in the crud controller, not a value forced here.
func TestCreateTask_ClearContextDefaultsToFalseWhenOmitted(t *testing.T) {
	var got entity.CreateTaskRequest
	ctrl := &mockTaskCrud{createTask: func(_ context.Context, req entity.CreateTaskRequest) (*entity.CreateTaskResponse, error) {
		got = req
		return &entity.CreateTaskResponse{Task: entity.Task{ID: testWorkspace}}, nil
	}}

	textOf(t, toolResult(taskServer(ctrl).handleCreateTask(authedContext(), nil, CreateTaskParams{
		WorkspaceID: base62(testWorkspace),
		Title:       "Wire up the new gateway",
	})))

	if got.Task.ClearContext {
		t.Errorf("expected ClearContext to stay false when the caller did not ask for it")
	}
	if got.Task.CreatedBy != "agent" {
		t.Errorf("CreatedBy = %q, want %q so the crud layer applies the workspace's clearContextDefault", got.Task.CreatedBy, "agent")
	}
	if !got.BySupervisor {
		t.Errorf("BySupervisor = false, want true so the crud layer records the supervisor workspace as the delegator")
	}
}

// The interface may leave a title out and have the browser name the task, so
// the crud controller now calls it Untitled. An agent has no such helper, and
// must still say what it is asking for.
func TestCreateTask_StillRequiresATitle(t *testing.T) {
	for _, title := range []string{"", "   "} {
		ctrl := &mockTaskCrud{createTask: func(_ context.Context, req entity.CreateTaskRequest) (*entity.CreateTaskResponse, error) {
			t.Fatalf("a task titled %q was created, want it refused", req.Task.Title)
			return nil, nil
		}}

		res := toolResult(taskServer(ctrl).handleCreateTask(authedContext(), nil, CreateTaskParams{
			WorkspaceID: base62(testWorkspace),
			Title:       title,
			Body:        "Wire up the new gateway",
		}))
		if !res.isError || res.text != "title is required" {
			t.Errorf("createTask titled %q answered %+v, want the error \"title is required\"", title, res)
		}
	}
}

// filter is text to find in a title or body. The crud layer's own Filter is a
// named view ("pending_approval"), which drops any other value, so the
// supervisor's text must arrive as Search.
func TestListTasks_FilterSearchesTitleAndBody(t *testing.T) {
	var got []entity.ListTasksRequest
	ctrl := &mockTaskCrud{listTasks: func(_ context.Context, req entity.ListTasksRequest) (*entity.ListTasksResponse, error) {
		got = append(got, req)
		return &entity.ListTasksResponse{}, nil
	}}
	srv := taskServer(ctrl)

	textOf(t, toolResult(srv.handleListTasks(authedContext(), nil, ListTasksParams{WorkspaceID: base62(testWorkspace), Filter: "gateway"})))
	textOf(t, toolResult(srv.handleListAllTasks(authedContext(), nil, ListAllTasksParams{Filter: "gateway"})))

	if len(got) != 2 {
		t.Fatalf("ListTasks called %d times, want 2", len(got))
	}
	for i, req := range got {
		if req.Search != "gateway" || req.Filter != "" {
			t.Errorf("call %d: Search = %q, Filter = %q, want the text in Search and no Filter", i, req.Search, req.Filter)
		}
	}
}

// createdByWorkspaceId lists what one workspace delegated, in one query.
func TestListTasks_FiltersByDelegatingWorkspace(t *testing.T) {
	var got []entity.ListTasksRequest
	ctrl := &mockTaskCrud{listTasks: func(_ context.Context, req entity.ListTasksRequest) (*entity.ListTasksResponse, error) {
		got = append(got, req)
		return &entity.ListTasksResponse{}, nil
	}}
	srv := taskServer(ctrl)

	textOf(t, toolResult(srv.handleListTasks(authedContext(), nil, ListTasksParams{WorkspaceID: base62(testWorkspace), CreatedByWorkspaceID: base62(delegatingWorkspace)})))
	textOf(t, toolResult(srv.handleListAllTasks(authedContext(), nil, ListAllTasksParams{CreatedByWorkspaceID: base62(delegatingWorkspace)})))

	if len(got) != 2 {
		t.Fatalf("ListTasks called %d times, want 2", len(got))
	}
	for i, req := range got {
		if req.CreatedByWorkspaceID != delegatingWorkspace {
			t.Errorf("call %d: CreatedByWorkspaceID = %d, want %d", i, req.CreatedByWorkspaceID, delegatingWorkspace)
		}
	}
}
