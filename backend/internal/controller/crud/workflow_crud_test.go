// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package crud

import (
	"context"
	"errors"
	"testing"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/golang/mock/gomock"
	"gorm.io/gorm"
)

var errWorkflowRepo = errors.New("db down")

func i64Ptr(v int64) *int64 { return &v }

// ── CreateWorkflow ────────────────────────────────────────────────────────────

func TestCreateWorkflow_Success(t *testing.T) {
	e := newTestController(t)
	e.repo.EXPECT().GetEvent(gomock.Any(), int64(7), testUserID).Return(model.Event{ID: 7}, nil)
	e.idgen.EXPECT().NextID().Return(int64(100))
	e.repo.EXPECT().CreateWorkflow(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, w model.Workflow) (model.Workflow, error) {
			if w.ID != 100 || w.UserID != testUserID || w.Name != "release" || w.Description != "d" || w.StartEventID != 7 {
				t.Errorf("unexpected workflow row: %+v", w)
			}
			return w, nil
		})

	rs, err := e.controller.CreateWorkflow(context.Background(), entity.CreateWorkflowRequest{
		UserID: testUserIDStr, Name: "release", Description: "d", StartEventID: 7,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rs.Workflow.ID != 100 || rs.Workflow.StartEventID != 7 {
		t.Errorf("unexpected response: %+v", rs.Workflow)
	}
}

func TestCreateWorkflow_WithoutStartEventSkipsTheLookup(t *testing.T) {
	e := newTestController(t)
	e.idgen.EXPECT().NextID().Return(int64(100))
	e.repo.EXPECT().CreateWorkflow(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, w model.Workflow) (model.Workflow, error) { return w, nil })

	if _, err := e.controller.CreateWorkflow(context.Background(), entity.CreateWorkflowRequest{
		UserID: testUserIDStr, Name: "release",
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreateWorkflow_InvalidUser(t *testing.T) {
	e := newTestController(t)
	if _, err := e.controller.CreateWorkflow(context.Background(), entity.CreateWorkflowRequest{Name: "release"}); err == nil {
		t.Error("expected an error for a missing user")
	}
}

func TestCreateWorkflow_InvalidName(t *testing.T) {
	e := newTestController(t)
	_, err := e.controller.CreateWorkflow(context.Background(), entity.CreateWorkflowRequest{
		UserID: testUserIDStr, Name: "Bad Name",
	})
	if !errors.Is(err, ErrInvalidWorkflowName) {
		t.Errorf("expected ErrInvalidWorkflowName, got %v", err)
	}
}

func TestCreateWorkflow_StartEventNotFound(t *testing.T) {
	e := newTestController(t)
	e.repo.EXPECT().GetEvent(gomock.Any(), int64(7), testUserID).Return(model.Event{}, base.ErrNotFound)
	_, err := e.controller.CreateWorkflow(context.Background(), entity.CreateWorkflowRequest{
		UserID: testUserIDStr, Name: "release", StartEventID: 7,
	})
	if err == nil || err.Error() != "start event not found" {
		t.Errorf("expected start event not found, got %v", err)
	}
}

func TestCreateWorkflow_DuplicateName(t *testing.T) {
	e := newTestController(t)
	e.idgen.EXPECT().NextID().Return(int64(100))
	e.repo.EXPECT().CreateWorkflow(gomock.Any(), gomock.Any()).Return(model.Workflow{}, gorm.ErrDuplicatedKey)
	_, err := e.controller.CreateWorkflow(context.Background(), entity.CreateWorkflowRequest{
		UserID: testUserIDStr, Name: "release",
	})
	if !errors.Is(err, ErrDuplicateName) {
		t.Errorf("expected ErrDuplicateName, got %v", err)
	}
}

func TestCreateWorkflow_RepositoryError(t *testing.T) {
	e := newTestController(t)
	e.idgen.EXPECT().NextID().Return(int64(100))
	e.repo.EXPECT().CreateWorkflow(gomock.Any(), gomock.Any()).Return(model.Workflow{}, errWorkflowRepo)
	_, err := e.controller.CreateWorkflow(context.Background(), entity.CreateWorkflowRequest{
		UserID: testUserIDStr, Name: "release",
	})
	if !errors.Is(err, errWorkflowRepo) {
		t.Errorf("expected the repository error, got %v", err)
	}
}

// ── GetWorkflow / ListWorkflows / DeleteWorkflow ──────────────────────────────

func TestGetWorkflow(t *testing.T) {
	e := newTestController(t)
	e.repo.EXPECT().GetWorkflow(gomock.Any(), int64(5), testUserID).Return(model.Workflow{ID: 5, Name: "release"}, nil)
	rs, err := e.controller.GetWorkflow(context.Background(), entity.GetWorkflowRequest{ID: 5, UserID: testUserIDStr})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rs.Workflow.ID != 5 || rs.Workflow.Name != "release" {
		t.Errorf("unexpected workflow: %+v", rs.Workflow)
	}
}

func TestGetWorkflow_NotFound(t *testing.T) {
	e := newTestController(t)
	e.repo.EXPECT().GetWorkflow(gomock.Any(), int64(5), testUserID).Return(model.Workflow{}, base.ErrNotFound)
	if _, err := e.controller.GetWorkflow(context.Background(), entity.GetWorkflowRequest{ID: 5, UserID: testUserIDStr}); !errors.Is(err, base.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestListWorkflows(t *testing.T) {
	e := newTestController(t)
	e.repo.EXPECT().ListWorkflowsByUser(gomock.Any(), testUserID).Return([]model.Workflow{{ID: 1}, {ID: 2}}, nil)
	rs, err := e.controller.ListWorkflows(context.Background(), entity.ListWorkflowsRequest{UserID: testUserIDStr})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rs.Workflows) != 2 || rs.Workflows[1].ID != 2 {
		t.Errorf("unexpected workflows: %+v", rs.Workflows)
	}
}

func TestListWorkflows_RepositoryError(t *testing.T) {
	e := newTestController(t)
	e.repo.EXPECT().ListWorkflowsByUser(gomock.Any(), testUserID).Return(nil, errWorkflowRepo)
	if _, err := e.controller.ListWorkflows(context.Background(), entity.ListWorkflowsRequest{UserID: testUserIDStr}); !errors.Is(err, errWorkflowRepo) {
		t.Errorf("expected the repository error, got %v", err)
	}
}

func TestDeleteWorkflow(t *testing.T) {
	e := newTestController(t)
	e.repo.EXPECT().DeleteWorkflow(gomock.Any(), int64(5), testUserID).Return(errWorkflowRepo)
	if err := e.controller.DeleteWorkflow(context.Background(), entity.DeleteWorkflowRequest{ID: 5, UserID: testUserIDStr}); !errors.Is(err, errWorkflowRepo) {
		t.Errorf("expected the repository error to pass through, got %v", err)
	}
}

// ── UpdateWorkflow ────────────────────────────────────────────────────────────

func TestUpdateWorkflow_AllFields(t *testing.T) {
	e := newTestController(t)
	existing := model.Workflow{ID: 5, UserID: testUserID, Name: "old", Description: "old", StartEventID: 1}
	e.repo.EXPECT().GetWorkflow(gomock.Any(), int64(5), testUserID).Return(existing, nil)
	e.repo.EXPECT().GetEvent(gomock.Any(), int64(9), testUserID).Return(model.Event{ID: 9}, nil)
	e.repo.EXPECT().UpdateWorkflow(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, w model.Workflow) (model.Workflow, error) {
			if w.Name != "renamed" || w.Description != "new" || w.StartEventID != 9 || string(w.Layout) != `{"x":1}` {
				t.Errorf("unexpected update: %+v", w)
			}
			return w, nil
		})

	rs, err := e.controller.UpdateWorkflow(context.Background(), entity.UpdateWorkflowRequest{
		ID: 5, UserID: testUserIDStr,
		Name: strPtr("renamed"), Description: strPtr("new"), StartEventID: i64Ptr(9), Layout: strPtr(`{"x":1}`),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rs.Workflow.Layout != `{"x":1}` {
		t.Errorf("layout = %q", rs.Workflow.Layout)
	}
}

// Omitted fields must be left alone: the editor PATCHes only the layout on
// every drag, and clearing the start event to 0 needs no lookup.
func TestUpdateWorkflow_ClearsStartEventAndLayoutWithoutTouchingTheName(t *testing.T) {
	e := newTestController(t)
	existing := model.Workflow{ID: 5, UserID: testUserID, Name: "keep", StartEventID: 1, Layout: []byte(`{"x":1}`)}
	e.repo.EXPECT().GetWorkflow(gomock.Any(), int64(5), testUserID).Return(existing, nil)
	e.repo.EXPECT().UpdateWorkflow(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, w model.Workflow) (model.Workflow, error) {
			if w.Name != "keep" || w.StartEventID != 0 || len(w.Layout) != 0 {
				t.Errorf("unexpected update: %+v", w)
			}
			return w, nil
		})

	if _, err := e.controller.UpdateWorkflow(context.Background(), entity.UpdateWorkflowRequest{
		ID: 5, UserID: testUserIDStr, StartEventID: i64Ptr(0), Layout: strPtr(""),
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUpdateWorkflow_InvalidUser(t *testing.T) {
	e := newTestController(t)
	if _, err := e.controller.UpdateWorkflow(context.Background(), entity.UpdateWorkflowRequest{ID: 5}); err == nil {
		t.Error("expected an error for a missing user")
	}
}

func TestUpdateWorkflow_NotFound(t *testing.T) {
	e := newTestController(t)
	e.repo.EXPECT().GetWorkflow(gomock.Any(), int64(5), testUserID).Return(model.Workflow{}, base.ErrNotFound)
	_, err := e.controller.UpdateWorkflow(context.Background(), entity.UpdateWorkflowRequest{ID: 5, UserID: testUserIDStr})
	if !errors.Is(err, base.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestUpdateWorkflow_InvalidName(t *testing.T) {
	e := newTestController(t)
	e.repo.EXPECT().GetWorkflow(gomock.Any(), int64(5), testUserID).Return(model.Workflow{ID: 5}, nil)
	_, err := e.controller.UpdateWorkflow(context.Background(), entity.UpdateWorkflowRequest{
		ID: 5, UserID: testUserIDStr, Name: strPtr("Not Valid"),
	})
	if !errors.Is(err, ErrInvalidWorkflowName) {
		t.Errorf("expected ErrInvalidWorkflowName, got %v", err)
	}
}

func TestUpdateWorkflow_StartEventNotFound(t *testing.T) {
	e := newTestController(t)
	e.repo.EXPECT().GetWorkflow(gomock.Any(), int64(5), testUserID).Return(model.Workflow{ID: 5}, nil)
	e.repo.EXPECT().GetEvent(gomock.Any(), int64(9), testUserID).Return(model.Event{}, base.ErrNotFound)
	_, err := e.controller.UpdateWorkflow(context.Background(), entity.UpdateWorkflowRequest{
		ID: 5, UserID: testUserIDStr, StartEventID: i64Ptr(9),
	})
	if err == nil || err.Error() != "start event not found" {
		t.Errorf("expected start event not found, got %v", err)
	}
}

func TestUpdateWorkflow_InvalidLayout(t *testing.T) {
	e := newTestController(t)
	e.repo.EXPECT().GetWorkflow(gomock.Any(), int64(5), testUserID).Return(model.Workflow{ID: 5}, nil)
	_, err := e.controller.UpdateWorkflow(context.Background(), entity.UpdateWorkflowRequest{
		ID: 5, UserID: testUserIDStr, Layout: strPtr("{not json"),
	})
	if err == nil || err.Error() != "layout must be valid JSON" {
		t.Errorf("expected a layout error, got %v", err)
	}
}

func TestUpdateWorkflow_RepositoryError(t *testing.T) {
	e := newTestController(t)
	e.repo.EXPECT().GetWorkflow(gomock.Any(), int64(5), testUserID).Return(model.Workflow{ID: 5}, nil)
	e.repo.EXPECT().UpdateWorkflow(gomock.Any(), gomock.Any()).Return(model.Workflow{}, errWorkflowRepo)
	_, err := e.controller.UpdateWorkflow(context.Background(), entity.UpdateWorkflowRequest{
		ID: 5, UserID: testUserIDStr, Description: strPtr("x"),
	})
	if !errors.Is(err, errWorkflowRepo) {
		t.Errorf("expected the repository error, got %v", err)
	}
}

// ── CreateWorkflowStep ────────────────────────────────────────────────────────

func validStepRequest() entity.CreateWorkflowStepRequest {
	return entity.CreateWorkflowStepRequest{
		UserID:           testUserIDStr,
		WorkflowID:       5,
		EventID:          10,
		WorkspaceID:      20,
		EmitEventID:      11,
		Title:            "do it",
		Body:             "{{EVENT_PAYLOAD}}",
		Assignee:         "agent",
		AllowAllCommands: true,
	}
}

// expectStepLookups sets up the lookups that precede the title check.
func expectStepLookups(e *testEnv) {
	e.repo.EXPECT().GetWorkflow(gomock.Any(), int64(5), testUserID).Return(model.Workflow{ID: 5}, nil)
	e.repo.EXPECT().GetEvent(gomock.Any(), int64(10), testUserID).Return(model.Event{ID: 10}, nil)
	e.repo.EXPECT().CheckWorkspaceAccess(gomock.Any(), int64(20), testUserID).Return(true, nil)
}

func TestCreateWorkflowStep_Success(t *testing.T) {
	e := newTestController(t)
	expectStepLookups(e)
	e.repo.EXPECT().GetEvent(gomock.Any(), int64(11), testUserID).Return(model.Event{ID: 11}, nil)
	e.repo.EXPECT().ListWorkflowStepsByWorkflow(gomock.Any(), int64(5), testUserID).Return([]model.WorkflowStep{step(1, 10)}, nil)
	e.idgen.EXPECT().NextID().Return(int64(300))
	e.repo.EXPECT().CreateWorkflowStep(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, s model.WorkflowStep) (model.WorkflowStep, error) {
			if s.ID != 300 || s.WorkflowID != 5 || s.UserID != testUserID || s.EventID != 10 ||
				s.WorkspaceID != 20 || s.EmitEventID != 11 || s.Title != "do it" ||
				s.Body != "{{EVENT_PAYLOAD}}" || s.Assignee != "agent" || !s.AllowAllCommands {
				t.Errorf("unexpected step row: %+v", s)
			}
			return s, nil
		})

	rs, err := e.controller.CreateWorkflowStep(context.Background(), validStepRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rs.WorkflowStep.ID != 300 || rs.WorkflowStep.EmitEventID != 11 {
		t.Errorf("unexpected response: %+v", rs.WorkflowStep)
	}
}

func TestCreateWorkflowStep_LeafStepSkipsTheEmitLookup(t *testing.T) {
	e := newTestController(t)
	expectStepLookups(e)
	e.repo.EXPECT().ListWorkflowStepsByWorkflow(gomock.Any(), int64(5), testUserID).Return(nil, nil)
	e.idgen.EXPECT().NextID().Return(int64(300))
	e.repo.EXPECT().CreateWorkflowStep(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, s model.WorkflowStep) (model.WorkflowStep, error) { return s, nil })

	rq := validStepRequest()
	rq.EmitEventID = 0
	if _, err := e.controller.CreateWorkflowStep(context.Background(), rq); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreateWorkflowStep_Errors(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*entity.CreateWorkflowStepRequest)
		setup   func(e *testEnv)
		wantMsg string
		wantErr error
	}{
		{
			name:    "invalid user",
			mutate:  func(rq *entity.CreateWorkflowStepRequest) { rq.UserID = "" },
			setup:   func(*testEnv) {},
			wantMsg: "invalid userID",
		},
		{
			name: "workflow not found",
			setup: func(e *testEnv) {
				e.repo.EXPECT().GetWorkflow(gomock.Any(), int64(5), testUserID).Return(model.Workflow{}, base.ErrNotFound)
			},
			wantMsg: "workflow not found",
		},
		{
			name: "event not found",
			setup: func(e *testEnv) {
				e.repo.EXPECT().GetWorkflow(gomock.Any(), int64(5), testUserID).Return(model.Workflow{ID: 5}, nil)
				e.repo.EXPECT().GetEvent(gomock.Any(), int64(10), testUserID).Return(model.Event{}, base.ErrNotFound)
			},
			wantMsg: "event not found",
		},
		{
			name: "workspace access check fails",
			setup: func(e *testEnv) {
				e.repo.EXPECT().GetWorkflow(gomock.Any(), int64(5), testUserID).Return(model.Workflow{ID: 5}, nil)
				e.repo.EXPECT().GetEvent(gomock.Any(), int64(10), testUserID).Return(model.Event{ID: 10}, nil)
				e.repo.EXPECT().CheckWorkspaceAccess(gomock.Any(), int64(20), testUserID).Return(false, errWorkflowRepo)
			},
			wantErr: errWorkflowRepo,
		},
		{
			name: "workspace belongs to someone else",
			setup: func(e *testEnv) {
				e.repo.EXPECT().GetWorkflow(gomock.Any(), int64(5), testUserID).Return(model.Workflow{ID: 5}, nil)
				e.repo.EXPECT().GetEvent(gomock.Any(), int64(10), testUserID).Return(model.Event{ID: 10}, nil)
				e.repo.EXPECT().CheckWorkspaceAccess(gomock.Any(), int64(20), testUserID).Return(false, nil)
			},
			wantMsg: "workspace not found",
		},
		{
			name:    "missing title",
			mutate:  func(rq *entity.CreateWorkflowStepRequest) { rq.Title = "" },
			setup:   expectStepLookups,
			wantMsg: "title is required",
		},
		{
			name: "emit event not found",
			setup: func(e *testEnv) {
				expectStepLookups(e)
				e.repo.EXPECT().GetEvent(gomock.Any(), int64(11), testUserID).Return(model.Event{}, base.ErrNotFound)
			},
			wantMsg: "emit event not found",
		},
		{
			name: "listing existing steps fails",
			setup: func(e *testEnv) {
				expectStepLookups(e)
				e.repo.EXPECT().GetEvent(gomock.Any(), int64(11), testUserID).Return(model.Event{ID: 11}, nil)
				e.repo.EXPECT().ListWorkflowStepsByWorkflow(gomock.Any(), int64(5), testUserID).Return(nil, errWorkflowRepo)
			},
			wantErr: errWorkflowRepo,
		},
		{
			name: "step closes a cycle",
			setup: func(e *testEnv) {
				expectStepLookups(e)
				e.repo.EXPECT().GetEvent(gomock.Any(), int64(11), testUserID).Return(model.Event{ID: 11}, nil)
				e.repo.EXPECT().ListWorkflowStepsByWorkflow(gomock.Any(), int64(5), testUserID).Return([]model.WorkflowStep{step(11, 10)}, nil)
			},
			wantErr: ErrWorkflowCycle,
		},
		{
			name: "create fails",
			setup: func(e *testEnv) {
				expectStepLookups(e)
				e.repo.EXPECT().GetEvent(gomock.Any(), int64(11), testUserID).Return(model.Event{ID: 11}, nil)
				e.repo.EXPECT().ListWorkflowStepsByWorkflow(gomock.Any(), int64(5), testUserID).Return(nil, nil)
				e.idgen.EXPECT().NextID().Return(int64(300))
				e.repo.EXPECT().CreateWorkflowStep(gomock.Any(), gomock.Any()).Return(model.WorkflowStep{}, errWorkflowRepo)
			},
			wantErr: errWorkflowRepo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newTestController(t)
			tt.setup(e)
			rq := validStepRequest()
			if tt.mutate != nil {
				tt.mutate(&rq)
			}
			rs, err := e.controller.CreateWorkflowStep(context.Background(), rq)
			if rs != nil {
				t.Errorf("expected no response, got %+v", rs)
			}
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Errorf("expected %v, got %v", tt.wantErr, err)
			}
			if tt.wantMsg != "" && (err == nil || err.Error() != tt.wantMsg) {
				t.Errorf("expected %q, got %v", tt.wantMsg, err)
			}
		})
	}
}

// ── ListWorkflowSteps / DeleteWorkflowStep / ListTasksFromWorkflow ────────────

func TestListWorkflowSteps(t *testing.T) {
	e := newTestController(t)
	e.repo.EXPECT().ListWorkflowStepsByWorkflow(gomock.Any(), int64(5), testUserID).Return([]model.WorkflowStep{{ID: 1, EventID: 10}}, nil)
	rs, err := e.controller.ListWorkflowSteps(context.Background(), entity.ListWorkflowStepsRequest{WorkflowID: 5, UserID: testUserIDStr})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rs.WorkflowSteps) != 1 || rs.WorkflowSteps[0].EventID != 10 {
		t.Errorf("unexpected steps: %+v", rs.WorkflowSteps)
	}
}

func TestListWorkflowSteps_RepositoryError(t *testing.T) {
	e := newTestController(t)
	e.repo.EXPECT().ListWorkflowStepsByWorkflow(gomock.Any(), int64(5), testUserID).Return(nil, errWorkflowRepo)
	if _, err := e.controller.ListWorkflowSteps(context.Background(), entity.ListWorkflowStepsRequest{WorkflowID: 5, UserID: testUserIDStr}); !errors.Is(err, errWorkflowRepo) {
		t.Errorf("expected the repository error, got %v", err)
	}
}

func TestDeleteWorkflowStep(t *testing.T) {
	e := newTestController(t)
	e.repo.EXPECT().DeleteWorkflowStep(gomock.Any(), int64(3), testUserID).Return(nil)
	if err := e.controller.DeleteWorkflowStep(context.Background(), entity.DeleteWorkflowStepRequest{ID: 3, WorkflowID: 5, UserID: testUserIDStr}); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestListTasksFromWorkflow(t *testing.T) {
	e := newTestController(t)
	e.repo.EXPECT().ListTasksByWorkflowID(gomock.Any(), int64(5), testUserID).Return([]model.Task{{ID: 1, Title: "a"}, {ID: 2, Title: "b"}}, nil)
	rs, err := e.controller.ListTasksFromWorkflow(context.Background(), entity.ListTasksFromWorkflowRequest{WorkflowID: 5, UserID: testUserIDStr})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rs.Tasks) != 2 || rs.Tasks[1].ID != 2 || rs.Tasks[1].Title != "b" {
		t.Errorf("unexpected tasks: %+v", rs.Tasks)
	}
}

func TestListTasksFromWorkflow_RepositoryError(t *testing.T) {
	e := newTestController(t)
	e.repo.EXPECT().ListTasksByWorkflowID(gomock.Any(), int64(5), testUserID).Return(nil, errWorkflowRepo)
	if _, err := e.controller.ListTasksFromWorkflow(context.Background(), entity.ListTasksFromWorkflowRequest{WorkflowID: 5, UserID: testUserIDStr}); !errors.Is(err, errWorkflowRepo) {
		t.Errorf("expected the repository error, got %v", err)
	}
}
