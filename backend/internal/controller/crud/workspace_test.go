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

	resp, err := e.controller.CreateWorkspace(context.Background(), entity.CreateWorkspaceRequest{
		UserID: testUserIDStr,
		Workspace: entity.Workspace{
			Name:                 "W",
			Icon:                 "base64icon",
			NotificationSettings: &entity.NotificationSettings{TaskCreated: true},
			SelfLearningLoopNote: "always think before acting",
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

	e.repo.EXPECT().GetWorkspaceAttachmentIDs(gomock.Any(), int64(1)).Return([]string{"att-1", "att-2"}, nil)
	e.repo.EXPECT().DeleteWorkspace(gomock.Any(), int64(1), testUserID).Return(nil)
	e.storage.EXPECT().Delete("att-1").Return(nil)
	e.storage.EXPECT().Delete("att-2").Return(nil)

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
		w.SelfLearningLoopNote = "Be mindful."
		return w, nil
	})

	resp, err := e.controller.UpdateWorkspace(context.Background(), entity.UpdateWorkspaceRequest{
		UserID:    testUserIDStr,
		Workspace: entity.Workspace{ID: 1, Name: "updated", Description: "desc", AutoAllowedTools: []string{"*"}, NotificationSettings: &entity.NotificationSettings{TaskCreated: true}, AllowAllCommands: true, SelfLearningLoopNote: "Be mindful.", InputSendDelaySeconds: 10},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Workspace.Description != "desc" {
		t.Errorf("expected description updated")
	}
	if !resp.Workspace.AllowAllCommands {
		t.Errorf("expected AllowAllCommands to be true")
	}
	if resp.Workspace.SelfLearningLoopNote != "Be mindful." {
		t.Errorf("expected SelfLearningLoopNote to be updated")
	}
	if resp.Workspace.InputSendDelaySeconds != 10 {
		t.Errorf("expected InputSendDelaySeconds to be 10, got %d", resp.Workspace.InputSendDelaySeconds)
	}
}

func TestUpdateWorkspace_InvalidInputSendDelaySeconds_Fails(t *testing.T) {
	e := newTestController(t)

	_, err := e.controller.UpdateWorkspace(context.Background(), entity.UpdateWorkspaceRequest{
		UserID:    testUserIDStr,
		Workspace: entity.Workspace{ID: 1, Name: "updated", InputSendDelaySeconds: 7},
	})

	if err == nil {
		t.Fatalf("expected an error for invalid InputSendDelaySeconds")
	}
}

func TestUpdateWorkspaceAutoAllowedTools_Success(t *testing.T) {
	e := newTestController(t)

	ws := activeWorkspace()
	e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(ws, nil)
	e.repo.EXPECT().UpdateWorkspace(gomock.Any(), gomock.Any()).Return(ws, nil)

	err := e.controller.UpdateWorkspaceAutoAllowedTools(context.Background(), entity.UpdateWorkspaceAutoAllowedToolsRequest{
		WorkspaceID: 1,
		UserID:      testUserIDStr,
		Tools:       []string{"git *"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGetDetailedWorkspaceStats_Success(t *testing.T) {
	e := newTestController(t)

	e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(model.Workspace{ID: 1, UserID: testUserID}, nil)
	e.repo.EXPECT().GetDetailedWorkspaceStats(gomock.Any(), int64(1), gomock.Any(), gomock.Any()).Return(entity.GetDetailedWorkspaceStatsResponse{
		Summary: entity.WorkspaceStatsSummary{TasksCompleted: 10},
	}, nil)

	resp, err := e.controller.GetDetailedWorkspaceStats(context.Background(), entity.GetWorkspaceStatsRequest{ID: 1, UserID: testUserIDStr, Range: "7d"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Summary.TasksCompleted != 10 {
		t.Errorf("expected 10 tasks completed")
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

func TestNormalizeWorkingDirectory_OptionalAndBlank(t *testing.T) {
	// The field is optional, and whitespace someone left behind is not a value.
	for _, in := range []string{"", "   ", "\t\n "} {
		got, err := normalizeWorkingDirectory(in)
		if err != nil {
			t.Errorf("normalizeWorkingDirectory(%q) errored: %v", in, err)
		}
		if got != "" {
			t.Errorf("normalizeWorkingDirectory(%q) = %q, want empty", in, got)
		}
	}
}

func TestNormalizeWorkingDirectory_TrimsSurroundingSpace(t *testing.T) {
	// Paths pasted from a terminal or a file manager routinely arrive padded.
	got, err := normalizeWorkingDirectory("  /Users/mt/Code/agentrq  ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := "/Users/mt/Code/agentrq"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestNormalizeWorkingDirectory_AcceptsAbsoluteShapes(t *testing.T) {
	// The agent may run on a different platform from this server, so all three
	// shapes have to be accepted here rather than only the local one.
	for _, in := range []string{
		"/",
		"/Users/mt/Code/agentrq",
		"~",
		"~/Code/agentrq",
		`C:\Users\mt\Code`,
		"C:/Users/mt/Code",
		`\\server\share\code`,
	} {
		got, err := normalizeWorkingDirectory(in)
		if err != nil {
			t.Errorf("normalizeWorkingDirectory(%q) errored: %v", in, err)
		}
		if got != in {
			t.Errorf("normalizeWorkingDirectory(%q) = %q, want it unchanged", in, got)
		}
	}
}

func TestNormalizeWorkingDirectory_RejectsRelativePaths(t *testing.T) {
	// A relative path means whatever directory the agent's shell started in,
	// which looks like it works and then quietly does something else.
	for _, in := range []string{"Code/agentrq", "./agentrq", "../agentrq", "agentrq", "~agentrq", "C:", "C:x"} {
		if _, err := normalizeWorkingDirectory(in); err == nil {
			t.Errorf("normalizeWorkingDirectory(%q) was accepted, want rejected", in)
		}
	}
}

func TestNormalizeWorkingDirectory_RejectsControlCharacters(t *testing.T) {
	// A newline is an injection anywhere this value is echoed into a line-based
	// format, and no filesystem accepts one in a path.
	for _, in := range []string{"/tmp/a\nb", "/tmp/a\rb", "/tmp/a\x00b", "/tmp/a\x7fb", "/tmp/a\tb"} {
		if _, err := normalizeWorkingDirectory(in); err == nil {
			t.Errorf("normalizeWorkingDirectory(%q) was accepted, want rejected", in)
		}
	}
}

func TestNormalizeWorkingDirectory_AllowsNonASCIINames(t *testing.T) {
	// Rejecting control characters must not also reject perfectly ordinary
	// directory names in other scripts.
	in := "/Users/mt/Documents/プロジェクト"
	got, err := normalizeWorkingDirectory(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != in {
		t.Errorf("got %q, want %q", got, in)
	}
}

func TestIsDriveLetter(t *testing.T) {
	for _, b := range []byte{'a', 'z', 'A', 'Z'} {
		if !isDriveLetter(b) {
			t.Errorf("isDriveLetter(%q) = false, want true", b)
		}
	}
	for _, b := range []byte{'0', '/', ':', '-'} {
		if isDriveLetter(b) {
			t.Errorf("isDriveLetter(%q) = true, want false", b)
		}
	}
}
