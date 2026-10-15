// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package base

import (
	"context"
	"testing"
	"time"

	"github.com/agentrq/agentrq/backend/internal/data/model"
	"gorm.io/datatypes"
)

func TestCreateTaskWithMessages_WritesBoth(t *testing.T) {
	db := deleteTaskDB(t)
	repo := New(&mockDB{db: db})
	now := time.Now()

	task := model.Task{ID: dtTaskID, CreatedAt: now, UpdatedAt: now, WorkspaceID: dtWorkspaceID, UserID: dtUserID, Status: "ongoing", Title: "Fork: x"}
	msgs := []model.Message{
		{ID: 1, CreatedAt: now, TaskID: dtTaskID, UserID: dtUserID, Sender: "human", Text: "a"},
		{ID: 2, CreatedAt: now, TaskID: dtTaskID, UserID: dtUserID, Sender: "agent", Text: "b"},
	}
	if _, err := repo.CreateTaskWithMessages(context.Background(), task, msgs); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := repo.ListMessages(context.Background(), dtTaskID)
	if err != nil || len(got) != 2 {
		t.Fatalf("messages = %d, %v; want 2", len(got), err)
	}
}

// A conversation past SQLite's variable limit in one INSERT still forks: with
// attachments and metadata set, 4,096 messages used to fail (#921).
func TestCreateTaskWithMessages_LongConversation(t *testing.T) {
	db := deleteTaskDB(t)
	repo := New(&mockDB{db: db})
	now := time.Now()

	task := model.Task{ID: dtTaskID, CreatedAt: now, UpdatedAt: now, WorkspaceID: dtWorkspaceID, UserID: dtUserID, Status: "ongoing", Title: "Fork: x"}
	const n = 4100
	msgs := make([]model.Message, n)
	for i := range msgs {
		msgs[i] = model.Message{
			ID: int64(i + 1), CreatedAt: now, TaskID: dtTaskID, UserID: dtUserID, Sender: "agent", Text: "working",
			Attachments: datatypes.JSON(`[{"id":"a1","filename":"shot.png"}]`),
			Metadata:    datatypes.JSON(`{"type":"elicitation_request"}`),
		}
	}
	if _, err := repo.CreateTaskWithMessages(context.Background(), task, msgs); err != nil {
		t.Fatalf("create: %v", err)
	}
	var got int64
	db.Model(&model.Message{}).Where("task_id = ?", dtTaskID).Count(&got)
	if got != n {
		t.Fatalf("messages = %d, want %d", got, n)
	}
}

// A batch that fails takes the batches already written down with it.
func TestCreateTaskWithMessages_LaterBatchFails(t *testing.T) {
	db := deleteTaskDB(t)
	repo := New(&mockDB{db: db})
	now := time.Now()

	task := model.Task{ID: dtTaskID, CreatedAt: now, UpdatedAt: now, WorkspaceID: dtWorkspaceID, UserID: dtUserID, Status: "ongoing", Title: "Fork: x"}
	msgs := make([]model.Message, 600)
	for i := range msgs {
		msgs[i] = model.Message{ID: int64(i + 1), CreatedAt: now, TaskID: dtTaskID, UserID: dtUserID, Sender: "agent", Text: "working"}
	}
	msgs[599].ID = 1 // the second batch repeats a message of the first
	if _, err := repo.CreateTaskWithMessages(context.Background(), task, msgs); err == nil {
		t.Fatal("expected the duplicate message ID to fail the write")
	}
	var n int64
	db.Model(&model.Message{}).Count(&n)
	if n != 0 {
		t.Fatalf("first batch survived a failed fork (%d messages)", n)
	}
	db.Model(&model.Task{}).Where("id = ?", dtTaskID).Count(&n)
	if n != 0 {
		t.Fatalf("task row survived a failed fork (%d rows)", n)
	}
}

func TestCreateTaskWithMessages_NoMessages(t *testing.T) {
	db := deleteTaskDB(t)
	repo := New(&mockDB{db: db})
	now := time.Now()

	task := model.Task{ID: dtTaskID, CreatedAt: now, UpdatedAt: now, WorkspaceID: dtWorkspaceID, UserID: dtUserID, Status: "ongoing", Title: "Fork: x"}
	if _, err := repo.CreateTaskWithMessages(context.Background(), task, nil); err != nil {
		t.Fatalf("create: %v", err)
	}
}

// A message that cannot be written takes the task down with it: a fork is
// either the whole conversation or nothing.
func TestCreateTaskWithMessages_AllOrNothing(t *testing.T) {
	db := deleteTaskDB(t)
	repo := New(&mockDB{db: db})
	now := time.Now()

	task := model.Task{ID: dtTaskID, CreatedAt: now, UpdatedAt: now, WorkspaceID: dtWorkspaceID, UserID: dtUserID, Status: "ongoing", Title: "Fork: x"}
	dup := []model.Message{
		{ID: 1, CreatedAt: now, TaskID: dtTaskID, UserID: dtUserID, Sender: "human", Text: "a"},
		{ID: 1, CreatedAt: now, TaskID: dtTaskID, UserID: dtUserID, Sender: "human", Text: "same id"},
	}
	if _, err := repo.CreateTaskWithMessages(context.Background(), task, dup); err == nil {
		t.Fatal("expected the duplicate message ID to fail the write")
	}
	var n int64
	db.Model(&model.Task{}).Where("id = ?", dtTaskID).Count(&n)
	if n != 0 {
		t.Fatalf("task row survived a failed fork (%d rows)", n)
	}

	// And a task that cannot be written stops before any message.
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateTaskWithMessages(context.Background(), task, dup[:1]); err == nil {
		t.Fatal("expected the duplicate task ID to fail the write")
	}
	db.Model(&model.Message{}).Count(&n)
	if n != 0 {
		t.Fatalf("messages written for a task that failed (%d rows)", n)
	}
}
