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
	"gorm.io/gorm"
)

// A human drags a task from a copy read before the agent completed it. The drag
// names only the order, so the save must leave the agent's status alone.
func TestUpdateTask_LeavesAConcurrentChangeAlone(t *testing.T) {
	for _, d := range historyDBs(t) {
		t.Run(d.name, func(t *testing.T) { leavesAConcurrentChangeAlone(t, d.db) })
	}
}

func leavesAConcurrentChangeAlone(t *testing.T, db *gorm.DB) {
	repo := New(&mockDB{db: db})
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)

	if _, err := repo.CreateTask(ctx, model.Task{ID: dtTaskID, CreatedAt: now, UpdatedAt: now, WorkspaceID: dtWorkspaceID, UserID: dtUserID, Status: "ongoing", Title: "Run the migration", SortOrder: 1}); err != nil {
		t.Fatalf("create: %v", err)
	}
	stale, err := repo.GetTask(ctx, dtWorkspaceID, dtTaskID, dtUserID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	// The agent's path never sets UpdatedAt; the save stamps it, or a board that
	// drops events older than what it holds would drop this one.
	agents := stale
	agents.Status = "completed"
	completed, err := repo.UpdateTask(ctx, agents, "Status")
	if err != nil {
		t.Fatalf("agent completes: %v", err)
	}
	if !completed.UpdatedAt.After(now) {
		t.Errorf("updatedAt = %v, want later than %v", completed.UpdatedAt, now)
	}

	stale.UpdatedAt = time.Now()
	stale.SortOrder = 2
	stale.Messages = []model.Message{{ID: 1, Text: "kept for the caller"}}
	got, err := repo.UpdateTask(ctx, stale, "SortOrder")
	if err != nil {
		t.Fatalf("human saves: %v", err)
	}

	if got.Status != "completed" || got.SortOrder != 2 || got.Title != "Run the migration" {
		t.Errorf("returned %q, order %v, title %q; want the row as it now stands", got.Status, got.SortOrder, got.Title)
	}
	if len(got.Messages) != 1 {
		t.Errorf("returned %d messages, want the caller's", len(got.Messages))
	}
	var row model.Task
	if err := db.First(&row, dtTaskID).Error; err != nil || row.Status != "completed" || row.SortOrder != 2 {
		t.Fatalf("row = %q, order %v, %v; want completed with the new order", row.Status, row.SortOrder, err)
	}
	rows := transitionsOf(t, repo, dtTaskID)
	if len(rows) != 2 || rows[1].FromState != model.TaskStateOngoing || rows[1].ToState != model.TaskStateCompleted {
		t.Fatalf("transitions = %+v, want none→ongoing, ongoing→completed and nothing after", rows)
	}
}

// A name outside the list is refused before anything is written, so a typo
// fails a test instead of silently dropping the change.
func TestUpdateTask_RefusesAnUnknownColumn(t *testing.T) {
	db := deleteTaskDB(t)
	repo := New(&mockDB{db: db})
	ctx := context.Background()

	task, err := repo.CreateTask(ctx, model.Task{ID: dtTaskID, WorkspaceID: dtWorkspaceID, UserID: dtUserID, Status: "ongoing"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	task.Status = "completed"
	if _, err := repo.UpdateTask(ctx, task, "Statsu"); err == nil {
		t.Fatal("update: want an unknown column refused")
	}
	var row model.Task
	if err := db.First(&row, dtTaskID).Error; err != nil || row.Status != "ongoing" {
		t.Fatalf("status = %q, %v; want nothing written", row.Status, err)
	}
}

func TestUpdateTask_WriteFailures(t *testing.T) {
	for _, tc := range []struct {
		name   string
		inject func(t *testing.T, db *gorm.DB)
	}{
		{"the update", func(t *testing.T, db *gorm.DB) { failOn(t, db, "update", "tasks") }},
		{"reading the row back", func(t *testing.T, db *gorm.DB) {
			// The second read of tasks inside the update: the first is the lock.
			reads := 0
			if err := db.Callback().Query().Before("gorm:query").Register("test:fail-reread", func(tx *gorm.DB) {
				if tx.Statement.Table == "tasks" {
					if reads++; reads == 2 {
						_ = tx.AddError(errInjected)
					}
				}
			}); err != nil {
				t.Fatalf("register: %v", err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := deleteTaskDB(t)
			repo := New(&mockDB{db: db})
			task, err := repo.CreateTask(context.Background(), model.Task{ID: dtTaskID, WorkspaceID: dtWorkspaceID, UserID: dtUserID, Status: "ongoing"})
			if err != nil {
				t.Fatalf("create: %v", err)
			}
			tc.inject(t, db)
			task.Status = "completed"
			if _, err := repo.UpdateTask(context.Background(), task, "Status"); !errors.Is(err, errInjected) {
				t.Fatalf("got %v, want the injected failure", err)
			}
		})
	}
}
