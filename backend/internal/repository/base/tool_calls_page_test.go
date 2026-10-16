// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package base

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// seedToolCalls creates a task with n tool calls, numbered 1 to n.
func seedToolCalls(t *testing.T, db *gorm.DB, taskID int64, n int) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Microsecond)
	repo := New(&mockDB{db: db})
	if _, err := repo.CreateTask(context.Background(), model.Task{ID: taskID, CreatedAt: now, UpdatedAt: now, WorkspaceID: dtWorkspaceID, UserID: dtUserID, Status: "ongoing"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	calls := make([]model.ToolCall, n)
	for i := range n {
		calls[i] = model.ToolCall{ID: taskID*1000 + int64(i+1), CreatedAt: now, TaskID: taskID, WorkspaceID: dtWorkspaceID, ToolName: "Bash", Status: "auto_allowed"}
	}
	if err := db.CreateInBatches(calls, 100).Error; err != nil {
		t.Fatalf("seed tool calls: %v", err)
	}
}

// GetTask reads a task with only its latest tool calls, and counts them all.
func TestGetTask_LatestToolCalls(t *testing.T) {
	for _, d := range historyDBs(t) {
		t.Run(d.name, func(t *testing.T) {
			ctx := context.Background()
			repo := New(&mockDB{db: d.db})
			seedToolCalls(t, d.db, 1, TaskToolCallsPage+1)
			seedToolCalls(t, d.db, 2, 3)

			long, err := repo.GetTask(ctx, dtWorkspaceID, 1, dtUserID)
			if err != nil {
				t.Fatal(err)
			}
			if len(long.ToolCalls) != TaskToolCallsPage || long.ToolCallCount != TaskToolCallsPage+1 {
				t.Fatalf("loaded %d of %d tool calls, want %d of %d", len(long.ToolCalls), long.ToolCallCount, TaskToolCallsPage, TaskToolCallsPage+1)
			}
			if first, last := long.ToolCalls[0].ID, long.ToolCalls[TaskToolCallsPage-1].ID; first != 1002 || last != 1000+TaskToolCallsPage+1 {
				t.Errorf("loaded %d to %d, want the latest, oldest first", first, last)
			}

			short, err := repo.GetTask(ctx, dtWorkspaceID, 2, dtUserID)
			if err != nil {
				t.Fatal(err)
			}
			if len(short.ToolCalls) != 3 || short.ToolCallCount != 3 {
				t.Errorf("loaded %d of %d tool calls, want 3 of 3", len(short.ToolCalls), short.ToolCallCount)
			}
		})
	}
}

func TestGetTask_ToolCallFailures(t *testing.T) {
	db := openHistoryDB(t, sqlite.Open("file::memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	ctx := context.Background()
	repo := New(&mockDB{db: db})
	seedToolCalls(t, db, 1, TaskToolCallsPage)

	errCount := errors.New("count failed")
	failCount := func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*int64); ok {
			_ = tx.AddError(errCount)
		}
	}
	if err := db.Callback().Query().Before("gorm:query").Register("test:fail_count", failCount); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetTask(ctx, dtWorkspaceID, 1, dtUserID); !errors.Is(err, errCount) {
		t.Errorf("err = %v, want the count's", err)
	}
	if err := db.Callback().Query().Remove("test:fail_count"); err != nil {
		t.Fatal(err)
	}

	if err := db.Exec("DROP TABLE tool_calls").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetTask(ctx, dtWorkspaceID, 1, dtUserID); err == nil {
		t.Error("GetTask without its tool calls returned no error")
	}
}

// UpdateToolCallStatus changes the status alone, in one statement, and
// returns the whole row for the stream.
func TestUpdateToolCallStatus(t *testing.T) {
	for _, d := range historyDBs(t) {
		t.Run(d.name, func(t *testing.T) {
			ctx := context.Background()
			repo := New(&mockDB{db: d.db})
			seedToolCalls(t, d.db, 1, 2)

			var statements int
			count := func(*gorm.DB) { statements++ }
			for _, err := range []error{
				d.db.Callback().Query().Before("gorm:query").Register("test:count_query", count),
				d.db.Callback().Update().Before("gorm:update").Register("test:count_update", count),
			} {
				if err != nil {
					t.Fatal(err)
				}
			}
			got, err := repo.UpdateToolCallStatus(ctx, 1001, "completed")
			if err != nil {
				t.Fatal(err)
			}
			if statements != 1 {
				t.Errorf("the update took %d statements, want 1", statements)
			}
			if got.ID != 1001 || got.TaskID != 1 || got.WorkspaceID != dtWorkspaceID || got.ToolName != "Bash" || got.Status != "completed" || got.CreatedAt.IsZero() {
				t.Errorf("returned %+v, want the whole row of 1001, completed", got)
			}

			var rows []model.ToolCall
			if err := d.db.Order("id").Find(&rows).Error; err != nil {
				t.Fatal(err)
			}
			if rows[0].Status != "completed" || rows[1].Status != "auto_allowed" {
				t.Errorf("stored statuses %q and %q, want only 1001 completed", rows[0].Status, rows[1].Status)
			}

			if _, err := repo.UpdateToolCallStatus(ctx, 9999, "failed"); !errors.Is(err, ErrNotFound) {
				t.Errorf("err for a missing row = %v, want ErrNotFound", err)
			}
			if err := d.db.Exec("DROP TABLE tool_calls").Error; err != nil {
				t.Fatal(err)
			}
			if _, err := repo.UpdateToolCallStatus(ctx, 1001, "failed"); err == nil || errors.Is(err, ErrNotFound) {
				t.Errorf("err without the table = %v, want the database's", err)
			}
		})
	}
}

// CreateToolCall checks the task and inserts in one statement, and records
// nothing on a task of another workspace or user.
func TestCreateToolCall(t *testing.T) {
	for _, d := range historyDBs(t) {
		t.Run(d.name, func(t *testing.T) {
			ctx := context.Background()
			repo := New(&mockDB{db: d.db})
			seedToolCalls(t, d.db, 1, 0)

			var statements int
			if err := d.db.Callback().Raw().Before("gorm:raw").Register("test:count_raw", func(*gorm.DB) { statements++ }); err != nil {
				t.Fatal(err)
			}
			now := time.Now().UTC().Truncate(time.Microsecond)
			tc := model.ToolCall{ID: 7, CreatedAt: now, TaskID: 1, WorkspaceID: dtWorkspaceID, ToolName: "Read", Description: "Read a.go", InputPreview: "{}", Status: "running"}
			got, err := repo.CreateToolCall(ctx, tc, dtUserID)
			if err != nil || got != tc {
				t.Fatalf("CreateToolCall = %+v, %v, want %+v", got, err, tc)
			}
			if statements != 1 {
				t.Errorf("the insert took %d statements, want 1", statements)
			}
			var stored model.ToolCall
			if err := d.db.First(&stored, 7).Error; err != nil {
				t.Fatal(err)
			}
			if !stored.CreatedAt.Equal(now) || stored.TaskID != 1 || stored.WorkspaceID != dtWorkspaceID || stored.ToolName != "Read" ||
				stored.Description != "Read a.go" || stored.InputPreview != "{}" || stored.Status != "running" {
				t.Errorf("stored %+v, want %+v", stored, tc)
			}

			for name, c := range map[string]struct {
				tc   model.ToolCall
				user int64
			}{
				"another user":      {model.ToolCall{ID: 8, TaskID: 1, WorkspaceID: dtWorkspaceID}, dtUserID + 1},
				"another workspace": {model.ToolCall{ID: 9, TaskID: 1, WorkspaceID: dtWorkspaceID + 1}, dtUserID},
				"no such task":      {model.ToolCall{ID: 10, TaskID: 2, WorkspaceID: dtWorkspaceID}, dtUserID},
			} {
				if _, err := repo.CreateToolCall(ctx, c.tc, c.user); !errors.Is(err, ErrNotFound) {
					t.Errorf("%s: err = %v, want ErrNotFound", name, err)
				}
			}
			var n int64
			if err := d.db.Model(&model.ToolCall{}).Count(&n).Error; err != nil || n != 1 {
				t.Errorf("%d rows stored (%v), want only the first", n, err)
			}
			if _, err := repo.CreateToolCall(ctx, tc, dtUserID); err == nil {
				t.Error("a repeated id was stored without an error")
			}
		})
	}
}
