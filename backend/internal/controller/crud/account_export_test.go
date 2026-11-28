// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package crud

import (
	"context"
	"errors"
	"testing"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/golang/mock/gomock"
)

func exportRequest() entity.ExportAccountDataRequest {
	return entity.ExportAccountDataRequest{UserID: testUserIDStr, Email: "ada@example.com", Name: "Ada"}
}

// expectExportReads sets every read but the tasks to answer with nothing.
func expectExportReads(e *machineTelemetryEnv) {
	e.repo.EXPECT().ListAccountSettings(gomock.Any(), testUserID).Return([]model.AccountSetting{}, nil)
	e.repo.EXPECT().ExportPresets(gomock.Any(), testUserID).Return([]model.Preset{}, nil)
	e.repo.EXPECT().ListWorkspaces(gomock.Any(), testUserID, true).Return([]model.Workspace{}, nil)
	e.repo.EXPECT().ExportMemories(gomock.Any(), testUserID).Return([]model.Memory{}, nil)
	e.repo.EXPECT().ExportPanoDocs(gomock.Any(), testUserID, gomock.Any()).Return([]model.PanoDoc{}, nil)
}

func TestExportAccountData_GathersEverythingTheAccountOwns(t *testing.T) {
	e := newMachineTelemetryEnv(t)
	at := time.Date(2026, 11, 28, 10, 0, 0, 0, time.UTC)
	archived := at.Add(time.Hour)
	e.repo.EXPECT().ListAccountSettings(gomock.Any(), testUserID).Return([]model.AccountSetting{displayRow(`{"timeFormat":"24h"}`)}, nil)
	e.repo.EXPECT().ExportPresets(gomock.Any(), testUserID).Return([]model.Preset{{WorkspaceID: 0, Name: "review", Body: "Review it", CreatedAt: at}}, nil)
	e.repo.EXPECT().ListWorkspaces(gomock.Any(), testUserID, true).Return([]model.Workspace{{ID: 7, Name: "Site", Description: "d", Icon: "i", WorkingDirectory: "/w", SelfLearningLoopNote: "n", ForkOfID: 3, CreatedAt: at, ArchivedAt: &archived}}, nil)
	e.repo.EXPECT().ExportMemories(gomock.Any(), testUserID).Return([]model.Memory{{WorkspaceID: 7, Name: "memory.md", Content: "# m", UpdatedAt: at}}, nil)
	e.repo.EXPECT().ExportPanoDocs(gomock.Any(), testUserID, gomock.Any()).Return([]model.PanoDoc{{ID: 4, Title: "Plan", Markdown: "# p", CreatedAt: at, UpdatedAt: at}}, nil)
	e.repo.EXPECT().ExportTasks(gomock.Any(), testUserID, int64(0), exportTaskPage).Return([]model.Task{
		{ID: 10, WorkspaceID: 7, ParentID: 2, Title: "Fix", Body: "b", Status: "completed", CreatedBy: "human", Assignee: "agent", CronSchedule: "", CreatedAt: at, UpdatedAt: at},
		{ID: 11, WorkspaceID: 7, Title: "Quiet"},
	}, nil)
	e.repo.EXPECT().ExportMessages(gomock.Any(), []int64{10, 11}).Return([]model.Message{
		{TaskID: 10, Sender: "agent", Text: "done", CreatedAt: at},
	}, nil)

	rs, err := e.controller.ExportAccountData(context.Background(), exportRequest())
	if err != nil {
		t.Fatalf("ExportAccountData returned %v", err)
	}
	if rs.Version != entity.AccountExportVersion || rs.ExportedAt.IsZero() {
		t.Errorf("version %d at %v, want version %d with a time", rs.Version, rs.ExportedAt, entity.AccountExportVersion)
	}
	if rs.Profile != (entity.AccountExportProfile{ID: testUserID, Email: "ada@example.com", Name: "Ada"}) {
		t.Errorf("profile %+v", rs.Profile)
	}
	if rs.Settings[entity.SettingsDisplay][entity.SettingTimeFormat] != "24h" {
		t.Errorf("settings %v, want timeFormat 24h", rs.Settings)
	}
	if len(rs.Presets) != 1 || rs.Presets[0] != (entity.AccountExportPreset{Name: "review", Body: "Review it", CreatedAt: at}) {
		t.Errorf("presets %+v", rs.Presets)
	}
	if len(rs.Workspaces) != 1 || rs.Workspaces[0].ID != 7 || rs.Workspaces[0].ForkOfID != 3 || rs.Workspaces[0].ArchivedAt != &archived || rs.Workspaces[0].SelfLearningLoopNote != "n" {
		t.Errorf("workspaces %+v", rs.Workspaces)
	}
	if len(rs.Memories) != 1 || rs.Memories[0] != (entity.AccountExportMemory{WorkspaceID: 7, Name: "memory.md", Content: "# m", UpdatedAt: at}) {
		t.Errorf("memories %+v", rs.Memories)
	}
	if len(rs.PanoDocs) != 1 || rs.PanoDocs[0] != (entity.AccountExportPanoDoc{ID: 4, Title: "Plan", Markdown: "# p", CreatedAt: at, UpdatedAt: at}) {
		t.Errorf("pano docs %+v", rs.PanoDocs)
	}
	if len(rs.Tasks) != 2 {
		t.Fatalf("tasks %+v, want two", rs.Tasks)
	}
	first := rs.Tasks[0]
	if first.ID != 10 || first.ParentID != 2 || first.Status != "completed" || first.Assignee != "agent" || len(first.Messages) != 1 || first.Messages[0] != (entity.AccountExportMessage{Sender: "agent", Text: "done", CreatedAt: at}) {
		t.Errorf("the first task is %+v", first)
	}
	if rs.Tasks[1].Messages == nil || len(rs.Tasks[1].Messages) != 0 {
		t.Errorf("a task with no messages has %v, want an empty list", rs.Tasks[1].Messages)
	}
}

func TestExportAccountData_ReadsEveryPageOfTasks(t *testing.T) {
	e := newMachineTelemetryEnv(t)
	expectExportReads(e)
	full := make([]model.Task, exportTaskPage)
	for i := range full {
		full[i] = model.Task{ID: int64(i + 1)}
	}
	gomock.InOrder(
		e.repo.EXPECT().ExportTasks(gomock.Any(), testUserID, int64(0), exportTaskPage).Return(full, nil),
		e.repo.EXPECT().ExportTasks(gomock.Any(), testUserID, int64(exportTaskPage), exportTaskPage).Return([]model.Task{}, nil),
	)
	e.repo.EXPECT().ExportMessages(gomock.Any(), gomock.Len(exportTaskPage)).Return([]model.Message{}, nil)

	rs, err := e.controller.ExportAccountData(context.Background(), exportRequest())
	if err != nil || len(rs.Tasks) != exportTaskPage {
		t.Fatalf("ExportAccountData returned %d tasks, %v; want %d", len(rs.Tasks), err, exportTaskPage)
	}
}

func TestExportAccountData_AnAccountWithNothingIsEmptyLists(t *testing.T) {
	e := newMachineTelemetryEnv(t)
	expectExportReads(e)
	e.repo.EXPECT().ExportTasks(gomock.Any(), testUserID, int64(0), exportTaskPage).Return([]model.Task{}, nil)
	rs, err := e.controller.ExportAccountData(context.Background(), exportRequest())
	if err != nil {
		t.Fatalf("ExportAccountData returned %v", err)
	}
	if rs.Presets == nil || rs.Workspaces == nil || rs.Memories == nil || rs.PanoDocs == nil || rs.Tasks == nil {
		t.Errorf("an empty account exported %+v, want empty lists, not none", rs)
	}
}

func TestExportAccountData_RefusesNoAccount(t *testing.T) {
	e := newMachineTelemetryEnv(t)
	if _, err := e.controller.ExportAccountData(context.Background(), entity.ExportAccountDataRequest{}); err == nil {
		t.Fatal("ExportAccountData with no account succeeded")
	}
}

func TestExportAccountData_StopsAtTheFirstFailedRead(t *testing.T) {
	failed := errors.New("database unavailable")
	for _, tc := range []struct {
		name   string
		expect func(e *machineTelemetryEnv)
	}{
		{"settings", func(e *machineTelemetryEnv) {
			e.repo.EXPECT().ListAccountSettings(gomock.Any(), testUserID).Return(nil, failed)
		}},
		{"presets", func(e *machineTelemetryEnv) {
			e.repo.EXPECT().ListAccountSettings(gomock.Any(), testUserID).Return(nil, nil)
			e.repo.EXPECT().ExportPresets(gomock.Any(), testUserID).Return(nil, failed)
		}},
		{"workspaces", func(e *machineTelemetryEnv) {
			e.repo.EXPECT().ListAccountSettings(gomock.Any(), testUserID).Return(nil, nil)
			e.repo.EXPECT().ExportPresets(gomock.Any(), testUserID).Return(nil, nil)
			e.repo.EXPECT().ListWorkspaces(gomock.Any(), testUserID, true).Return(nil, failed)
		}},
		{"memories", func(e *machineTelemetryEnv) {
			e.repo.EXPECT().ListAccountSettings(gomock.Any(), testUserID).Return(nil, nil)
			e.repo.EXPECT().ExportPresets(gomock.Any(), testUserID).Return(nil, nil)
			e.repo.EXPECT().ListWorkspaces(gomock.Any(), testUserID, true).Return(nil, nil)
			e.repo.EXPECT().ExportMemories(gomock.Any(), testUserID).Return(nil, failed)
		}},
		{"pano docs", func(e *machineTelemetryEnv) {
			e.repo.EXPECT().ListAccountSettings(gomock.Any(), testUserID).Return(nil, nil)
			e.repo.EXPECT().ExportPresets(gomock.Any(), testUserID).Return(nil, nil)
			e.repo.EXPECT().ListWorkspaces(gomock.Any(), testUserID, true).Return(nil, nil)
			e.repo.EXPECT().ExportMemories(gomock.Any(), testUserID).Return(nil, nil)
			e.repo.EXPECT().ExportPanoDocs(gomock.Any(), testUserID, gomock.Any()).Return(nil, failed)
		}},
		{"tasks", func(e *machineTelemetryEnv) {
			expectExportReads(e)
			e.repo.EXPECT().ExportTasks(gomock.Any(), testUserID, int64(0), exportTaskPage).Return(nil, failed)
		}},
		{"messages", func(e *machineTelemetryEnv) {
			expectExportReads(e)
			e.repo.EXPECT().ExportTasks(gomock.Any(), testUserID, int64(0), exportTaskPage).Return([]model.Task{{ID: 1}}, nil)
			e.repo.EXPECT().ExportMessages(gomock.Any(), []int64{1}).Return(nil, failed)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newMachineTelemetryEnv(t)
			tc.expect(e)
			if _, err := e.controller.ExportAccountData(context.Background(), exportRequest()); !errors.Is(err, failed) {
				t.Fatalf("ExportAccountData returned %v, want the failed read's error", err)
			}
		})
	}
}
