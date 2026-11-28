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

// noExportCap turns the daily cap off for a test about what is read.
func noExportCap(t *testing.T) {
	t.Helper()
	before := AccountMaxExportsPerDay
	AccountMaxExportsPerDay = 0
	t.Cleanup(func() { AccountMaxExportsPerDay = before })
}

func exportCap(t *testing.T, n int) {
	t.Helper()
	before := AccountMaxExportsPerDay
	AccountMaxExportsPerDay = n
	t.Cleanup(func() { AccountMaxExportsPerDay = before })
}

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
	noExportCap(t)
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
	if ev := e.only(t); ev.Action != entity.ActionAccountExport || ev.UserID != testUserID || ev.ResourceType != entity.ResourceUser || ev.ResourceID != testUserID || ev.WorkspaceID != 0 || ev.Actor != entity.ActorHuman {
		t.Errorf("published %+v, want an account_export of the account by the human, with no workspace", ev)
	}
}

func TestExportAccountData_ReadsEveryPageOfTasks(t *testing.T) {
	noExportCap(t)
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
	noExportCap(t)
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
	noExportCap(t)
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
			noExportCap(t)
			e := newMachineTelemetryEnv(t)
			tc.expect(e)
			if _, err := e.controller.ExportAccountData(context.Background(), exportRequest()); !errors.Is(err, failed) {
				t.Fatalf("ExportAccountData returned %v, want the failed read's error", err)
			}
		})
	}
}

func TestExportAccountData_CountsEachDownloadAgainstTheCap(t *testing.T) {
	exportCap(t, 2)
	e := newMachineTelemetryEnv(t)
	e.idgen.EXPECT().NextID().Return(int64(77))
	var kept model.AccountExport
	e.repo.EXPECT().RecordAccountExport(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, x model.AccountExport) error {
		kept = x
		return nil
	})
	var since time.Time
	e.repo.EXPECT().ListAccountExportsSince(gomock.Any(), testUserID, gomock.Any()).DoAndReturn(func(_ context.Context, _ int64, s time.Time) ([]time.Time, error) {
		since = s
		return []time.Time{kept.CreatedAt.Add(-time.Hour), kept.CreatedAt}, nil
	})
	expectExportReads(e)
	e.repo.EXPECT().ExportTasks(gomock.Any(), testUserID, int64(0), exportTaskPage).Return([]model.Task{}, nil)

	if _, err := e.controller.ExportAccountData(context.Background(), exportRequest()); err != nil {
		t.Fatalf("the second download of the day returned %v", err)
	}
	if kept.ID != 77 || kept.UserID != testUserID || kept.CreatedAt.IsZero() {
		t.Errorf("kept %+v", kept)
	}
	if want := kept.CreatedAt.Add(-24 * time.Hour); !since.Equal(want) {
		t.Errorf("counted since %v, want 24 hours before the download, %v", since, want)
	}
}

func TestExportAccountData_RefusesADownloadPastTheCap(t *testing.T) {
	for _, tc := range []struct {
		name    string
		limit   int
		earlier []time.Duration // before this download, oldest first
		retry   time.Duration   // after this download
		message string
	}{
		{"twice a day, the third", 2, []time.Duration{20 * time.Hour, time.Hour}, 4 * time.Hour, "Your data can be exported 2 times a day"},
		{"once a day, the second", 1, []time.Duration{3 * time.Hour}, 21 * time.Hour, "Your data can be exported once a day"},
		{"two at once past the cap: the slot the newer earlier one frees", 1, []time.Duration{3 * time.Hour, 2 * time.Hour}, 22 * time.Hour, "Your data can be exported once a day"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			exportCap(t, tc.limit)
			e := newMachineTelemetryEnv(t)
			e.idgen.EXPECT().NextID().Return(int64(77))
			var now time.Time
			e.repo.EXPECT().RecordAccountExport(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, x model.AccountExport) error {
				now = x.CreatedAt
				return nil
			})
			e.repo.EXPECT().ListAccountExportsSince(gomock.Any(), testUserID, gomock.Any()).DoAndReturn(func(context.Context, int64, time.Time) ([]time.Time, error) {
				times := []time.Time{}
				for _, d := range tc.earlier {
					times = append(times, now.Add(-d))
				}
				return append(times, now), nil
			})
			e.repo.EXPECT().ForgetAccountExport(gomock.Any(), int64(77)).Return(nil)

			_, err := e.controller.ExportAccountData(context.Background(), exportRequest())
			var throttled *entity.AccountExportThrottled
			if !errors.As(err, &throttled) {
				t.Fatalf("returned %v, want AccountExportThrottled", err)
			}
			if throttled.Limit != tc.limit || !throttled.RetryAt.Equal(now.Add(tc.retry)) || err.Error() != tc.message {
				t.Errorf("refused with %+v %q, want limit %d, retry %v after, %q", throttled, err, tc.limit, tc.retry, tc.message)
			}
			if len(*e.events) != 0 {
				t.Errorf("a refused download published %+v", *e.events)
			}
		})
	}
}

func TestExportAccountData_ACountThatFailsIsNoDownload(t *testing.T) {
	failed := errors.New("database unavailable")
	for _, tc := range []struct {
		name   string
		expect func(e *machineTelemetryEnv)
	}{
		{"keeping it", func(e *machineTelemetryEnv) {
			e.repo.EXPECT().RecordAccountExport(gomock.Any(), gomock.Any()).Return(failed)
		}},
		{"counting", func(e *machineTelemetryEnv) {
			e.repo.EXPECT().RecordAccountExport(gomock.Any(), gomock.Any()).Return(nil)
			e.repo.EXPECT().ListAccountExportsSince(gomock.Any(), testUserID, gomock.Any()).Return(nil, failed)
		}},
		{"forgetting a refused one", func(e *machineTelemetryEnv) {
			e.repo.EXPECT().RecordAccountExport(gomock.Any(), gomock.Any()).Return(nil)
			e.repo.EXPECT().ListAccountExportsSince(gomock.Any(), testUserID, gomock.Any()).Return([]time.Time{{}, {}, {}}, nil)
			e.repo.EXPECT().ForgetAccountExport(gomock.Any(), int64(77)).Return(failed)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			exportCap(t, 2)
			e := newMachineTelemetryEnv(t)
			e.idgen.EXPECT().NextID().Return(int64(77))
			tc.expect(e)
			if _, err := e.controller.ExportAccountData(context.Background(), exportRequest()); !errors.Is(err, failed) {
				t.Fatalf("returned %v, want the failed write's error", err)
			}
		})
	}
}
