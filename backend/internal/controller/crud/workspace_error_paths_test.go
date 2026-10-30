// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package crud

import (
	"context"
	"testing"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/golang/mock/gomock"
)

func TestCreateWorkspace_Refusals(t *testing.T) {
	t.Run("rate limited", func(t *testing.T) {
		e := newTestController(t)
		e.controller.(*controller).limiter = &denyWorkspaceLimiter{}
		_, err := e.controller.CreateWorkspace(context.Background(), entity.CreateWorkspaceRequest{UserID: testUserIDStr, Workspace: entity.Workspace{Name: "ws"}})
		wantErrText(t, "CreateWorkspace", err, "rate limit exceeded")
	})
	t.Run("a relative folder", func(t *testing.T) {
		e := newTestController(t)
		_, err := e.controller.CreateWorkspace(context.Background(), entity.CreateWorkspaceRequest{UserID: testUserIDStr, Workspace: entity.Workspace{Name: "ws", WorkingDirectory: "src/api"}})
		wantErrText(t, "CreateWorkspace", err, "must be an absolute path")
	})
	t.Run("the write fails", func(t *testing.T) {
		e := newTestController(t)
		e.idgen.EXPECT().NextID().Return(int64(5))
		e.repo.EXPECT().CreateWorkspace(gomock.Any(), gomock.Any()).Return(model.Workspace{}, errDBDown)
		_, err := e.controller.CreateWorkspace(context.Background(), entity.CreateWorkspaceRequest{UserID: testUserIDStr, Workspace: entity.Workspace{Name: "ws"}})
		wantErrIs(t, "CreateWorkspace", err, errDBDown)
		wantErrText(t, "CreateWorkspace", err, "create workspace: ")
	})
}

func TestWorkspaceReads_PassOnFailures(t *testing.T) {
	ctx := context.Background()
	t.Run("get", func(t *testing.T) {
		e := newTestController(t)
		e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(model.Workspace{}, base.ErrNotFound)
		_, err := e.controller.GetWorkspace(ctx, entity.GetWorkspaceRequest{ID: 1, UserID: testUserIDStr})
		wantErrIs(t, "GetWorkspace", err, base.ErrNotFound)
	})
	t.Run("list", func(t *testing.T) {
		e := newTestController(t)
		e.repo.EXPECT().ListWorkspaces(gomock.Any(), testUserID, true).Return(nil, errDBDown)
		_, err := e.controller.ListWorkspaces(ctx, entity.ListWorkspacesRequest{UserID: testUserIDStr, IncludeArchived: true})
		wantErrIs(t, "ListWorkspaces", err, errDBDown)
	})
	t.Run("list: the forks' unfinished tasks", func(t *testing.T) {
		e := newTestController(t)
		e.repo.EXPECT().ListWorkspaces(gomock.Any(), testUserID, false).Return([]model.Workspace{{ID: 1, Name: "api"}, {ID: 2, Name: "api fork", ForkOfID: 1}}, nil)
		e.repo.EXPECT().CountUnfinishedTasks(gomock.Any(), []int64{2}).Return(nil, errDBDown)
		_, err := e.controller.ListWorkspaces(ctx, entity.ListWorkspacesRequest{UserID: testUserIDStr})
		wantErrIs(t, "ListWorkspaces", err, errDBDown)
	})
	t.Run("stats", func(t *testing.T) {
		e := newTestController(t)
		e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(activeWorkspace(), nil)
		e.repo.EXPECT().GetDetailedWorkspaceStats(gomock.Any(), int64(1), gomock.Any(), gomock.Any()).Return(entity.GetDetailedWorkspaceStatsResponse{}, errDBDown)
		_, err := e.controller.GetDetailedWorkspaceStats(ctx, entity.GetWorkspaceStatsRequest{ID: 1, UserID: testUserIDStr})
		wantErrIs(t, "GetDetailedWorkspaceStats", err, errDBDown)
	})
	t.Run("system get", func(t *testing.T) {
		e := newTestController(t)
		e.repo.EXPECT().SystemGetWorkspace(gomock.Any(), int64(1)).Return(model.Workspace{ID: 1, Name: "ws"}, nil)
		if w, err := e.controller.SystemGetWorkspace(ctx, 1); err != nil || w.ID != 1 || w.Name != "ws" {
			t.Errorf("SystemGetWorkspace = %+v, %v", w, err)
		}
		e.repo.EXPECT().SystemGetWorkspace(gomock.Any(), int64(2)).Return(model.Workspace{}, base.ErrNotFound)
		_, err := e.controller.SystemGetWorkspace(ctx, 2)
		wantErrIs(t, "SystemGetWorkspace", err, base.ErrNotFound)
	})
}

func TestWorkspaceWrites_PassOnFailures(t *testing.T) {
	ctx := context.Background()
	notFound := func(e *testEnv) {
		e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(model.Workspace{}, base.ErrNotFound)
	}
	t.Run("delete", func(t *testing.T) {
		e := newTestController(t)
		e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(model.Workspace{ID: 1, UserID: testUserID}, nil)
		e.repo.EXPECT().CountForks(gomock.Any(), int64(1), testUserID).Return(int64(0), nil)
		e.repo.EXPECT().GetWorkspaceAttachments(gomock.Any(), int64(1)).Return([]entity.TaskAttachment{{TaskID: 3, ID: "a"}}, nil)
		e.repo.EXPECT().DeleteWorkspace(gomock.Any(), int64(1), testUserID).Return(errDBDown)
		// No storage expectation: a workspace still in the database keeps its files.
		wantErrIs(t, "DeleteWorkspace", e.controller.DeleteWorkspace(ctx, entity.DeleteWorkspaceRequest{ID: 1, UserID: testUserIDStr}), errDBDown)
	})
	t.Run("archive", func(t *testing.T) {
		e := newTestController(t)
		notFound(e)
		wantErrIs(t, "ArchiveWorkspace", e.controller.ArchiveWorkspace(ctx, entity.ArchiveWorkspaceRequest{ID: 1, UserID: testUserIDStr}), base.ErrNotFound)
	})
	t.Run("unarchive", func(t *testing.T) {
		e := newTestController(t)
		notFound(e)
		wantErrIs(t, "UnarchiveWorkspace", e.controller.UnarchiveWorkspace(ctx, entity.UnarchiveWorkspaceRequest{ID: 1, UserID: testUserIDStr}), base.ErrNotFound)
	})
	t.Run("auto-allowed tools", func(t *testing.T) {
		e := newTestController(t)
		notFound(e)
		err := e.controller.UpdateWorkspaceAutoAllowedTools(ctx, entity.UpdateWorkspaceAutoAllowedToolsRequest{WorkspaceID: 1, Tools: []string{"x"}, UserID: testUserIDStr})
		wantErrIs(t, "UpdateWorkspaceAutoAllowedTools", err, base.ErrNotFound)
	})
	t.Run("update: a relative folder", func(t *testing.T) {
		e := newTestController(t)
		_, err := e.controller.UpdateWorkspace(ctx, entity.UpdateWorkspaceRequest{UserID: testUserIDStr, Workspace: entity.Workspace{ID: 1, WorkingDirectory: "api"}})
		wantErrText(t, "UpdateWorkspace", err, "must be an absolute path")
	})
	t.Run("update: the read", func(t *testing.T) {
		e := newTestController(t)
		notFound(e)
		_, err := e.controller.UpdateWorkspace(ctx, entity.UpdateWorkspaceRequest{UserID: testUserIDStr, Workspace: entity.Workspace{ID: 1}})
		wantErrIs(t, "UpdateWorkspace", err, base.ErrNotFound)
	})
	t.Run("update: the write", func(t *testing.T) {
		e := newTestController(t)
		e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(activeWorkspace(), nil)
		e.repo.EXPECT().UpdateWorkspace(gomock.Any(), gomock.Any()).Return(model.Workspace{}, errDBDown)
		_, err := e.controller.UpdateWorkspace(ctx, entity.UpdateWorkspaceRequest{UserID: testUserIDStr, Workspace: entity.Workspace{ID: 1, Name: "ws"}})
		wantErrIs(t, "UpdateWorkspace", err, errDBDown)
	})
}

// An icon is stored resized; one that cannot be resized is dropped and the
// rest of the update still goes through.
func TestUpdateWorkspace_Icon(t *testing.T) {
	for _, tc := range []struct {
		name      string
		resizeErr error
		want      string
	}{
		{"resized", nil, "small"},
		{"unreadable", errDBDown, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newTestController(t)
			e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(activeWorkspace(), nil)
			e.image.EXPECT().ResizeBase64("big", 32, 32).Return("small", tc.resizeErr)
			var saved model.Workspace
			e.repo.EXPECT().UpdateWorkspace(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, m model.Workspace) (model.Workspace, error) { saved = m; return m, nil })
			e.repo.EXPECT().ListForks(gomock.Any(), int64(1), testUserID).Return(nil, nil)
			if _, err := e.controller.UpdateWorkspace(context.Background(), entity.UpdateWorkspaceRequest{UserID: testUserIDStr, Workspace: entity.Workspace{ID: 1, Name: "renamed", Icon: "big"}}); err != nil {
				t.Fatalf("UpdateWorkspace: %v", err)
			}
			if saved.Icon != tc.want || saved.Name != "renamed" {
				t.Errorf("saved icon %q, name %q; want %q, renamed", saved.Icon, saved.Name, tc.want)
			}
		})
	}
}
