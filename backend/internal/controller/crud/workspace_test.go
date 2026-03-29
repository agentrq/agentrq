package crud

import (
	"context"
	"fmt"
	"testing"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/golang/mock/gomock"
)

func TestCreateWorkspace_WithOptions(t *testing.T) {
	e := newTestController(t)

	e.idgen.EXPECT().NextID().Return(int64(100))
	e.image.EXPECT().ResizeBase64("base64icon", 32, 32).Return("resized icon", nil)
	e.repo.EXPECT().CreateWorkspace(gomock.Any(), gomock.Any()).Return(model.Workspace{ID: 100}, nil)
	e.telemetry.EXPECT().Record(gomock.Any(), testUserID, int64(100), model.ActionIDWorkspaceCreate)

	resp, err := e.controller.CreateWorkspace(context.Background(), entity.CreateWorkspaceRequest{
		UserID: testUserIDStr,
		Workspace: entity.Workspace{
			Name:                 "W",
			Icon:                 "base64icon",
			NotificationSettings: &entity.NotificationSettings{TaskCreated: true},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Workspace.ID != 100 {
		t.Errorf("expected ID 100")
	}
}

func TestDeleteWorkspace_Complex(t *testing.T) {
	e := newTestController(t)

	// Mocking tasks with attachments
	task := model.Task{
		ID:          10,
		Attachments: []byte(`[{"id":"att-1"}]`),
		Messages: []model.Message{
			{ID: 101, Attachments: []byte(`[{"id":"att-2"}]`)},
		},
	}
	e.repo.EXPECT().ListTasks(gomock.Any(), gomock.Any(), testUserID).Return([]model.Task{task}, nil)
	e.repo.EXPECT().DeleteWorkspace(gomock.Any(), int64(1), testUserID).Return(nil)
	e.storage.EXPECT().Delete("att-1").Return(nil)
	e.storage.EXPECT().Delete("att-2").Return(nil)
	e.telemetry.EXPECT().Record(gomock.Any(), testUserID, int64(1), model.ActionIDWorkspaceDelete)

	err := e.controller.DeleteWorkspace(context.Background(), entity.DeleteWorkspaceRequest{ID: 1, UserID: testUserIDStr})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGetWorkspace_Success(t *testing.T) {
	e := newTestController(t)

	ws := model.Workspace{ID: 1, UserID: testUserID, Name: "Found"}
	e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(ws, nil)

	resp, err := e.controller.GetWorkspace(context.Background(), entity.GetWorkspaceRequest{ID: 1, UserID: testUserIDStr})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Workspace.Name != "Found" {
		t.Errorf("expected Name Found, got %s", resp.Workspace.Name)
	}
}

func TestListWorkspaces_Success(t *testing.T) {
	e := newTestController(t)

	ms := []model.Workspace{
		{ID: 1, UserID: testUserID, Name: "W1"},
		{ID: 2, UserID: testUserID, Name: "W2"},
	}
	e.repo.EXPECT().ListWorkspaces(gomock.Any(), testUserID, false).Return(ms, nil)

	resp, err := e.controller.ListWorkspaces(context.Background(), entity.ListWorkspacesRequest{UserID: testUserIDStr, IncludeArchived: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Workspaces) != 2 {
		t.Errorf("expected 2 workspaces, got %d", len(resp.Workspaces))
	}
}

func TestArchiveWorkspace_Success(t *testing.T) {
	e := newTestController(t)

	ws := activeWorkspace()
	e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(ws, nil)
	e.repo.EXPECT().UpdateWorkspace(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, w model.Workspace) (model.Workspace, error) {
		if w.ArchivedAt == nil {
			return model.Workspace{}, fmt.Errorf("expected ArchivedAt to be set")
		}
		return w, nil
	})
	e.notif.EXPECT().NotifyWorkspaceArchived(gomock.Any())
	e.telemetry.EXPECT().Record(gomock.Any(), testUserID, int64(1), model.ActionIDWorkspaceUpdate)

	err := e.controller.ArchiveWorkspace(context.Background(), entity.ArchiveWorkspaceRequest{ID: 1, UserID: testUserIDStr})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUnarchiveWorkspace_Success(t *testing.T) {
	e := newTestController(t)

	now := time.Now()
	ws := activeWorkspace()
	ws.ArchivedAt = &now

	e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(ws, nil)
	e.repo.EXPECT().UpdateWorkspace(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, w model.Workspace) (model.Workspace, error) {
		if w.ArchivedAt != nil {
			return model.Workspace{}, fmt.Errorf("expected ArchivedAt to be nil")
		}
		return w, nil
	})
	e.notif.EXPECT().NotifyWorkspaceUnarchived(gomock.Any())
	e.telemetry.EXPECT().Record(gomock.Any(), testUserID, int64(1), model.ActionIDWorkspaceUpdate)

	err := e.controller.UnarchiveWorkspace(context.Background(), entity.UnarchiveWorkspaceRequest{ID: 1, UserID: testUserIDStr})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUpdateWorkspace_Full(t *testing.T) {
	e := newTestController(t)

	ws := activeWorkspace()
	e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(ws, nil)
	e.repo.EXPECT().UpdateWorkspace(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, w model.Workspace) (model.Workspace, error) {
		w.Description = "desc"
		w.Icon = "icon"
		return w, nil
	})
	e.telemetry.EXPECT().Record(gomock.Any(), testUserID, int64(1), model.ActionIDWorkspaceUpdate)

	resp, err := e.controller.UpdateWorkspace(context.Background(), entity.UpdateWorkspaceRequest{
		UserID:    testUserIDStr,
		Workspace: entity.Workspace{ID: 1, Name: "updated", Description: "desc", AutoAllowedTools: []string{"*"}, NotificationSettings: &entity.NotificationSettings{TaskCreated: true}},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Description != "desc" {
		t.Errorf("expected description updated")
	}
}

func TestUpdateWorkspaceAutoAllowedTools_Success(t *testing.T) {
	e := newTestController(t)

	ws := activeWorkspace()
	e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(ws, nil)
	e.repo.EXPECT().UpdateWorkspace(gomock.Any(), gomock.Any()).Return(ws, nil)
	e.telemetry.EXPECT().Record(gomock.Any(), testUserID, int64(1), model.ActionIDWorkspaceUpdate)

	err := e.controller.UpdateWorkspaceAutoAllowedTools(context.Background(), entity.UpdateWorkspaceAutoAllowedToolsRequest{
		WorkspaceID: 1,
		UserID:      testUserIDStr,
		Tools:       []string{"git *"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGetWorkspaceStats_Success(t *testing.T) {
	e := newTestController(t)

	e.telemetry.EXPECT().GetWorkspaceStats(gomock.Any(), int64(1)).Return(&entity.GetWorkspaceStatsResponse{Total: 10}, nil)

	resp, err := e.controller.GetWorkspaceStats(context.Background(), entity.GetWorkspaceRequest{ID: 1, UserID: testUserIDStr})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Total != 10 {
		t.Errorf("expected total 10")
	}
}

func TestUpdateWorkspace_Archived_Fails(t *testing.T) {
	e := newTestController(t)

	ws := activeWorkspace()
	now := time.Now()
	ws.ArchivedAt = &now

	e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(ws, nil)

	_, err := e.controller.UpdateWorkspace(context.Background(), entity.UpdateWorkspaceRequest{
		UserID:    testUserIDStr,
		Workspace: entity.Workspace{ID: 1, Name: "new"},
	})

	if err == nil || err.Error() != "cannot update archived workspace" {
		t.Fatalf("expected archived error, got %v", err)
	}
}
