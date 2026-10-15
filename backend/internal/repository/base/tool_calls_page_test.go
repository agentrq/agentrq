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
