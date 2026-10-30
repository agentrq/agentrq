// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package crud

import (
	"context"
	"errors"
	"strings"
	"testing"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/golang/mock/gomock"
)

// The account the text tests resolve names against: events 1..3 and
// workspaces 20/21, keyed by name in the document and by ID in storage.
var (
	textEvents = []model.Event{
		{ID: 1, Name: "code_changed"},
		{ID: 2, Name: "doc_updated"},
		{ID: 3, Name: "published"},
	}
	textWorkspaces = []model.Workspace{
		{ID: 20, Name: "doc"},
		{ID: 21, Name: "blog"},
	}
)

// ── GetWorkflowText ───────────────────────────────────────────────────────────

func TestGetWorkflowText_RendersStepsAndSkipsDanglingOnes(t *testing.T) {
	e := newTestController(t)
	e.repo.EXPECT().GetWorkflow(gomock.Any(), int64(5), testUserID).Return(model.Workflow{ID: 5, Name: "release", StartEventID: 1}, nil)
	e.repo.EXPECT().ListWorkflowStepsByWorkflow(gomock.Any(), int64(5), testUserID).Return([]model.WorkflowStep{
		{EventID: 1, WorkspaceID: 20, EmitEventID: 2},
		{EventID: 2, WorkspaceID: 21},
		// A deleted source event and a deleted workspace: neither renders.
		{EventID: 99, WorkspaceID: 20},
		{EventID: 1, WorkspaceID: 98},
	}, nil)
	e.repo.EXPECT().ListEventsByUser(gomock.Any(), testUserID).Return(textEvents, nil)
	e.repo.EXPECT().ListWorkspaces(gomock.Any(), testUserID, false).Return(textWorkspaces, nil)

	rs, err := e.controller.GetWorkflowText(context.Background(), entity.GetWorkflowTextRequest{ID: 5, UserID: testUserIDStr})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "workflow: release\nevent: code_changed\n- agent:doc\n  - event:doc_updated\n    - agent:blog\n"
	if rs.Text != want {
		t.Errorf("text =\n%s\nwant\n%s", rs.Text, want)
	}
}

func TestGetWorkflowText_Errors(t *testing.T) {
	tests := []struct {
		name  string
		setup func(e *testEnv)
		want  error
	}{
		{
			name: "workflow not found",
			setup: func(e *testEnv) {
				e.repo.EXPECT().GetWorkflow(gomock.Any(), int64(5), testUserID).Return(model.Workflow{}, base.ErrNotFound)
			},
			want: base.ErrNotFound,
		},
		{
			name: "listing steps fails",
			setup: func(e *testEnv) {
				e.repo.EXPECT().GetWorkflow(gomock.Any(), int64(5), testUserID).Return(model.Workflow{ID: 5}, nil)
				e.repo.EXPECT().ListWorkflowStepsByWorkflow(gomock.Any(), int64(5), testUserID).Return(nil, errWorkflowRepo)
			},
			want: errWorkflowRepo,
		},
		{
			name: "listing events fails",
			setup: func(e *testEnv) {
				e.repo.EXPECT().GetWorkflow(gomock.Any(), int64(5), testUserID).Return(model.Workflow{ID: 5}, nil)
				e.repo.EXPECT().ListWorkflowStepsByWorkflow(gomock.Any(), int64(5), testUserID).Return(nil, nil)
				e.repo.EXPECT().ListEventsByUser(gomock.Any(), testUserID).Return(nil, errWorkflowRepo)
			},
			want: errWorkflowRepo,
		},
		{
			name: "listing workspaces fails",
			setup: func(e *testEnv) {
				e.repo.EXPECT().GetWorkflow(gomock.Any(), int64(5), testUserID).Return(model.Workflow{ID: 5}, nil)
				e.repo.EXPECT().ListWorkflowStepsByWorkflow(gomock.Any(), int64(5), testUserID).Return(nil, nil)
				e.repo.EXPECT().ListEventsByUser(gomock.Any(), testUserID).Return(textEvents, nil)
				e.repo.EXPECT().ListWorkspaces(gomock.Any(), testUserID, false).Return(nil, errWorkflowRepo)
			},
			want: errWorkflowRepo,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newTestController(t)
			tt.setup(e)
			rs, err := e.controller.GetWorkflowText(context.Background(), entity.GetWorkflowTextRequest{ID: 5, UserID: testUserIDStr})
			if rs != nil || !errors.Is(err, tt.want) {
				t.Errorf("got (%+v, %v), want error %v", rs, err, tt.want)
			}
		})
	}
}

// ── ReplaceWorkflowFromText ───────────────────────────────────────────────────

const replaceDoc = `workflow: renamed
event: code_changed
- agent:doc
  - event:doc_updated
    - agent:blog
- agent:blog
`

// expectTextLookups sets up the workflow and the name tables ahead of
// validation.
func expectTextLookups(e *testEnv) {
	e.repo.EXPECT().GetWorkflow(gomock.Any(), int64(5), testUserID).Return(model.Workflow{ID: 5, UserID: testUserID, Name: "release"}, nil)
	e.repo.EXPECT().ListEventsByUser(gomock.Any(), testUserID).Return(textEvents, nil)
	e.repo.EXPECT().ListWorkspaces(gomock.Any(), testUserID, false).Return(textWorkspaces, nil)
}

func TestReplaceWorkflowFromText_ReplacesStepsAndRenames(t *testing.T) {
	e := newTestController(t)
	expectTextLookups(e)
	next := int64(400)
	e.idgen.EXPECT().NextID().DoAndReturn(func() int64 { next++; return next }).Times(3)
	e.repo.EXPECT().ReplaceWorkflowSteps(gomock.Any(), int64(5), testUserID, gomock.Any()).DoAndReturn(
		func(_ context.Context, _ int64, _ int64, steps []model.WorkflowStep) error {
			want := []model.WorkflowStep{
				{EventID: 1, WorkspaceID: 20, EmitEventID: 2, Title: "renamed: code_changed"},
				{EventID: 2, WorkspaceID: 21, Title: "renamed: doc_updated"},
				{EventID: 1, WorkspaceID: 21, Title: "renamed: code_changed"},
			}
			if len(steps) != len(want) {
				t.Fatalf("got %d steps, want %d", len(steps), len(want))
			}
			for i, s := range steps {
				w := want[i]
				if s.EventID != w.EventID || s.WorkspaceID != w.WorkspaceID || s.EmitEventID != w.EmitEventID || s.Title != w.Title {
					t.Errorf("step %d = %+v, want %+v", i, s, w)
				}
				if s.WorkflowID != 5 || s.UserID != testUserID || s.Body != defaultStepBody || s.Assignee != "agent" || s.ID == 0 {
					t.Errorf("step %d has the wrong defaults: %+v", i, s)
				}
			}
			return nil
		})
	e.repo.EXPECT().UpdateWorkflow(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, w model.Workflow) (model.Workflow, error) {
			if w.Name != "renamed" || w.StartEventID != 1 {
				t.Errorf("unexpected workflow update: %+v", w)
			}
			return w, nil
		})

	rs, err := e.controller.ReplaceWorkflowFromText(context.Background(), entity.ReplaceWorkflowFromTextRequest{
		ID: 5, UserID: testUserIDStr, Text: replaceDoc,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rs.StepCount != 3 || rs.Workflow.Name != "renamed" || rs.Workflow.StartEventID != 1 {
		t.Errorf("unexpected response: %+v", rs)
	}
}

func TestReplaceWorkflowFromText_InvalidUser(t *testing.T) {
	e := newTestController(t)
	if _, err := e.controller.ReplaceWorkflowFromText(context.Background(), entity.ReplaceWorkflowFromTextRequest{ID: 5, Text: replaceDoc}); err == nil {
		t.Error("expected an error for a missing user")
	}
}

func TestReplaceWorkflowFromText_RepositoryErrors(t *testing.T) {
	tests := []struct {
		name  string
		setup func(e *testEnv)
		want  error
	}{
		{
			name: "workflow not found",
			setup: func(e *testEnv) {
				e.repo.EXPECT().GetWorkflow(gomock.Any(), int64(5), testUserID).Return(model.Workflow{}, base.ErrNotFound)
			},
			want: base.ErrNotFound,
		},
		{
			name: "listing events fails",
			setup: func(e *testEnv) {
				e.repo.EXPECT().GetWorkflow(gomock.Any(), int64(5), testUserID).Return(model.Workflow{ID: 5}, nil)
				e.repo.EXPECT().ListEventsByUser(gomock.Any(), testUserID).Return(nil, errWorkflowRepo)
			},
			want: errWorkflowRepo,
		},
		{
			name: "listing workspaces fails",
			setup: func(e *testEnv) {
				e.repo.EXPECT().GetWorkflow(gomock.Any(), int64(5), testUserID).Return(model.Workflow{ID: 5}, nil)
				e.repo.EXPECT().ListEventsByUser(gomock.Any(), testUserID).Return(textEvents, nil)
				e.repo.EXPECT().ListWorkspaces(gomock.Any(), testUserID, false).Return(nil, errWorkflowRepo)
			},
			want: errWorkflowRepo,
		},
		{
			name: "replacing steps fails",
			setup: func(e *testEnv) {
				expectTextLookups(e)
				e.idgen.EXPECT().NextID().Return(int64(400)).Times(3)
				e.repo.EXPECT().ReplaceWorkflowSteps(gomock.Any(), int64(5), testUserID, gomock.Any()).Return(errWorkflowRepo)
			},
			want: errWorkflowRepo,
		},
		{
			name: "updating the workflow fails",
			setup: func(e *testEnv) {
				expectTextLookups(e)
				e.idgen.EXPECT().NextID().Return(int64(400)).Times(3)
				e.repo.EXPECT().ReplaceWorkflowSteps(gomock.Any(), int64(5), testUserID, gomock.Any()).Return(nil)
				e.repo.EXPECT().UpdateWorkflow(gomock.Any(), gomock.Any()).Return(model.Workflow{}, errWorkflowRepo)
			},
			want: errWorkflowRepo,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newTestController(t)
			tt.setup(e)
			rs, err := e.controller.ReplaceWorkflowFromText(context.Background(), entity.ReplaceWorkflowFromTextRequest{
				ID: 5, UserID: testUserIDStr, Text: replaceDoc,
			})
			if rs != nil || !errors.Is(err, tt.want) {
				t.Errorf("got (%+v, %v), want error %v", rs, err, tt.want)
			}
		})
	}
}

// Every document error must reach the handler as a *WorkflowTextError, with
// the line that caused it, and nothing may be written before it is found.
func TestReplaceWorkflowFromText_DocumentErrors(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		lookups  bool
		wantLine int
		wantMsg  string
	}{
		{
			name:     "parse error",
			text:     "workflow: release\n- agent:doc\n",
			wantLine: 2,
			wantMsg:  "list items must come after",
		},
		{
			name:    "unknown start event",
			text:    "workflow: release\nevent: nope\n- agent:doc\n",
			lookups: true,
			wantMsg: `unknown event "nope"`,
		},
		{
			name:     "unknown workspace",
			text:     "workflow: release\nevent: code_changed\n- agent:ghost\n",
			lookups:  true,
			wantLine: 3,
			wantMsg:  `unknown workspace "ghost"`,
		},
		{
			name:     "unknown emitted event",
			text:     "workflow: release\nevent: code_changed\n- agent:doc\n  - event:nope\n",
			lookups:  true,
			wantLine: 4,
			wantMsg:  `unknown event "nope"`,
		},
		{
			name:     "unknown workspace further down the tree",
			text:     "workflow: release\nevent: code_changed\n- agent:doc\n  - event:doc_updated\n    - agent:ghost\n",
			lookups:  true,
			wantLine: 5,
			wantMsg:  `unknown workspace "ghost"`,
		},
		{
			name:    "branches that re-join into a loop",
			text:    "workflow: release\nevent: code_changed\n- agent:doc\n  - event:doc_updated\n    - agent:blog\n      - event:code_changed\n",
			lookups: true,
			wantMsg: "doc_updated -> code_changed closes a loop",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newTestController(t)
			if tt.lookups {
				expectTextLookups(e)
			} else {
				e.repo.EXPECT().GetWorkflow(gomock.Any(), int64(5), testUserID).Return(model.Workflow{ID: 5}, nil)
			}
			_, err := e.controller.ReplaceWorkflowFromText(context.Background(), entity.ReplaceWorkflowFromTextRequest{
				ID: 5, UserID: testUserIDStr, Text: tt.text,
			})
			var te *WorkflowTextError
			if !errors.As(err, &te) {
				t.Fatalf("expected a *WorkflowTextError, got %v", err)
			}
			if te.Line != tt.wantLine {
				t.Errorf("line = %d, want %d", te.Line, tt.wantLine)
			}
			if !strings.Contains(te.Msg, tt.wantMsg) {
				t.Errorf("message %q does not contain %q", te.Msg, tt.wantMsg)
			}
		})
	}
}
