// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package base

import (
	"context"
	"testing"
	"time"

	"github.com/agentrq/agentrq/backend/internal/data/model"
	"gorm.io/gorm"
)

// A task read with GetTask carries every message and tool call it has. Saving
// it used to upsert all of them in one statement each, so past 4,095 tool calls
// (8 columns a row) SQLite refused it with "too many SQL variables": the task
// could take no reply and no status change, and held the workspace's queue.
func TestUpdateTask_DoesNotRewriteHistory(t *testing.T) {
	db := deleteTaskDB(t)
	repo := New(&mockDB{db: db})
	ctx := context.Background()
	now := time.Now()

	if _, err := repo.CreateTask(ctx, model.Task{ID: dtTaskID, CreatedAt: now, UpdatedAt: now, WorkspaceID: dtWorkspaceID, UserID: dtUserID, Status: "ongoing"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	const n = 4100
	msgs := make([]model.Message, n)
	calls := make([]model.ToolCall, n)
	for i := range n {
		msgs[i] = model.Message{ID: int64(i + 1), CreatedAt: now, TaskID: dtTaskID, UserID: dtUserID, Sender: "agent", Text: "working"}
		calls[i] = model.ToolCall{ID: int64(i + 1), CreatedAt: now, TaskID: dtTaskID, WorkspaceID: dtWorkspaceID, ToolName: "Bash", Status: "auto_allowed"}
	}
	if err := db.CreateInBatches(msgs, 500).Error; err != nil {
		t.Fatalf("seed messages: %v", err)
	}
	if err := db.CreateInBatches(calls, 500).Error; err != nil {
		t.Fatalf("seed tool calls: %v", err)
	}

	task, err := repo.GetTask(ctx, dtWorkspaceID, dtTaskID, dtUserID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(task.Messages) != n || len(task.ToolCalls) != n {
		t.Fatalf("loaded %d messages and %d tool calls, want %d of each", len(task.Messages), len(task.ToolCalls), n)
	}

	// Count every write that reaches a child table during the save.
	var childWrites int
	countChildWrite := func(tx *gorm.DB) {
		if tx.Statement.Table == "messages" || tx.Statement.Table == "tool_calls" {
			childWrites++
		}
	}
	if err := db.Callback().Create().After("gorm:create").Register("test:count_child_creates", countChildWrite); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := db.Callback().Update().After("gorm:update").Register("test:count_child_updates", countChildWrite); err != nil {
		t.Fatalf("register: %v", err)
	}

	task.Status = "completed"
	if _, err := repo.UpdateTask(ctx, task); err != nil {
		t.Fatalf("update a task with %d tool calls: %v", n, err)
	}
	if childWrites != 0 {
		t.Errorf("saving the task wrote its messages or tool calls %d times, want 0", childWrites)
	}

	got, err := repo.GetTask(ctx, dtWorkspaceID, dtTaskID, dtUserID)
	if err != nil {
		t.Fatalf("get after update: %v", err)
	}
	if got.Status != "completed" {
		t.Errorf("status = %q, want completed", got.Status)
	}
	if len(got.Messages) != n || len(got.ToolCalls) != n {
		t.Errorf("after the save: %d messages and %d tool calls, want %d of each", len(got.Messages), len(got.ToolCalls), n)
	}
}
