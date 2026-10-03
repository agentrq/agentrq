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
}

func (m *mockTaskCrud) CreateTask(ctx context.Context, req entity.CreateTaskRequest) (*entity.CreateTaskResponse, error) {
	return m.createTask(ctx, req)
}

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
