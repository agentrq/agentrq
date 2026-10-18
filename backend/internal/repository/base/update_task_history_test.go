// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package base

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/glebarez/sqlite"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// A task's writes must never move its history (#921). One statement for all of
// a task's messages or tool calls binds 8 values a row: past 4,095 rows that is
// over SQLite's limit of 32,766, and past 8,191 over Postgres's 65,535. The
// tests seed 8,200 of each, which breaks both if any write takes them along.
//
// They run on SQLite, and on Postgres too when AGENTRQ_TEST_POSTGRES_DSN names
// a throwaway database: its tables are dropped and recreated.
const historyRows = 8200

type historyDB struct {
	name string
	db   *gorm.DB
}

var historyTables = []any{&model.Task{}, &model.Message{}, &model.ToolCall{}, &model.SlackTaskThread{}, &model.EventTrigger{}, &model.WorkflowStep{}, &model.TaskStateTransition{}, &model.TaskLatency{}, &model.Agent{}, &model.AgentModel{}, &model.PanoDoc{}, &model.PanoDocTask{}, &model.TaskPlan{}}

func historyDBs(t *testing.T) []historyDB {
	t.Helper()
	quiet := &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)}
	dbs := []historyDB{{name: "sqlite", db: openHistoryDB(t, sqlite.Open("file::memory:?_pragma=foreign_keys(1)"), quiet)}}
	if dsn := os.Getenv("AGENTRQ_TEST_POSTGRES_DSN"); dsn != "" {
		pg := openHistoryDB(t, postgres.Open(dsn), quiet)
		dbs = append(dbs, historyDB{name: "postgres", db: pg})
	} else {
		t.Log("AGENTRQ_TEST_POSTGRES_DSN is not set; running on SQLite only")
	}
	return dbs
}

func openHistoryDB(t *testing.T, d gorm.Dialector, cfg *gorm.Config) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(d, cfg)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	if err := db.Migrator().DropTable(historyTables...); err != nil {
		t.Fatalf("drop tables: %v", err)
	}
	if err := db.AutoMigrate(historyTables...); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

// seedLongTask creates the task dtTaskID with historyRows messages and tool
// calls, and returns it with all of them, though GetTask loads only the latest
// tool calls: the saves under test must not write any history they are handed.
func seedLongTask(t *testing.T, db *gorm.DB) model.Task {
	t.Helper()
	ctx := context.Background()
	repo := New(&mockDB{db: db})
	now := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := repo.CreateTask(ctx, model.Task{ID: dtTaskID, CreatedAt: now, UpdatedAt: now, WorkspaceID: dtWorkspaceID, UserID: dtUserID, Status: "ongoing"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	msgs := make([]model.Message, historyRows)
	calls := make([]model.ToolCall, historyRows)
	for i := range historyRows {
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
	if len(task.Messages) != historyRows || len(task.ToolCalls) != TaskToolCallsPage || task.ToolCallCount != historyRows {
		t.Fatalf("loaded %d messages and %d of %d tool calls, want %d, and %d of %d", len(task.Messages), len(task.ToolCalls), task.ToolCallCount, historyRows, TaskToolCallsPage, historyRows)
	}
	if first := task.ToolCalls[0].ID; first != historyRows-TaskToolCallsPage+1 {
		t.Fatalf("first loaded tool call = %d, want the latest page", first)
	}
	task.ToolCalls = calls
	return task
}

// countChildWrites counts every insert, update or delete that reaches the
// messages or tool_calls table from now on.
func countChildWrites(t *testing.T, db *gorm.DB) *int {
	t.Helper()
	var n int
	count := func(tx *gorm.DB) {
		if tx.Statement.Table == "messages" || tx.Statement.Table == "tool_calls" {
			n++
		}
	}
	cb := db.Callback()
	if err := cb.Create().After("gorm:create").Register("test:count_child_creates", count); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := cb.Update().After("gorm:update").Register("test:count_child_updates", count); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := cb.Delete().After("gorm:delete").Register("test:count_child_deletes", count); err != nil {
		t.Fatalf("register: %v", err)
	}
	return &n
}

// historyOf counts the messages and tool calls stored for a task.
func historyOf(t *testing.T, db *gorm.DB, taskID int64) (msgs, calls int64) {
	t.Helper()
	if err := db.Model(&model.Message{}).Where("task_id = ?", taskID).Count(&msgs).Error; err != nil {
		t.Fatalf("count messages: %v", err)
	}
	if err := db.Model(&model.ToolCall{}).Where("task_id = ?", taskID).Count(&calls).Error; err != nil {
		t.Fatalf("count tool calls: %v", err)
	}
	return msgs, calls
}

// Saving a task must write only its row, never the history GetTask loaded.
func TestUpdateTask_DoesNotRewriteHistory(t *testing.T) {
	for _, d := range historyDBs(t) {
		t.Run(d.name, func(t *testing.T) {
			repo := New(&mockDB{db: d.db})
			task := seedLongTask(t, d.db)
			writes := countChildWrites(t, d.db)

			task.Status = "completed"
			if _, err := repo.UpdateTask(context.Background(), task, "Status"); err != nil {
				t.Fatalf("update a task with %d tool calls: %v", historyRows, err)
			}
			if *writes != 0 {
				t.Errorf("saving the task wrote its messages or tool calls %d times, want 0", *writes)
			}
			got, err := repo.GetTask(context.Background(), dtWorkspaceID, dtTaskID, dtUserID)
			if err != nil {
				t.Fatalf("get after update: %v", err)
			}
			if got.Status != "completed" {
				t.Errorf("status = %q, want completed", got.Status)
			}
			if len(got.Messages) != historyRows || got.ToolCallCount != historyRows {
				t.Errorf("after the save: %d messages and %d tool calls, want %d of each", len(got.Messages), got.ToolCallCount, historyRows)
			}
		})
	}
}

// An update of a task that is not stored refuses it and writes nothing: no
// row, and none of the history it carries moves off the task it was loaded from.
func TestUpdateTask_MissingTaskWritesNothing(t *testing.T) {
	const copyID = dtTaskID + 1
	for _, d := range historyDBs(t) {
		t.Run(d.name, func(t *testing.T) {
			repo := New(&mockDB{db: d.db})
			task := seedLongTask(t, d.db)
			writes := countChildWrites(t, d.db)

			task.ID = copyID
			if _, err := repo.UpdateTask(context.Background(), task, "Status"); !errors.Is(err, ErrNotFound) {
				t.Fatalf("update a task that is not stored: %v, want ErrNotFound", err)
			}
			if *writes != 0 {
				t.Errorf("the update wrote messages or tool calls %d times, want 0", *writes)
			}
			if _, err := repo.GetTask(context.Background(), dtWorkspaceID, copyID, dtUserID); !errors.Is(err, ErrNotFound) {
				t.Fatalf("the missing task: %v, want ErrNotFound", err)
			}
			if m, c := historyOf(t, d.db, dtTaskID); m != historyRows || c != historyRows {
				t.Errorf("the original kept %d messages and %d tool calls, want %d of each", m, c, historyRows)
			}
		})
	}
}

// A task saved with fewer messages or tool calls than it has loses none: a
// save never deletes history.
func TestUpdateTask_TrimmedHistoryDeletesNothing(t *testing.T) {
	for _, d := range historyDBs(t) {
		t.Run(d.name, func(t *testing.T) {
			repo := New(&mockDB{db: d.db})
			task := seedLongTask(t, d.db)
			writes := countChildWrites(t, d.db)

			task.Messages = task.Messages[:1]
			task.ToolCalls = nil
			task.Status = "blocked"
			if _, err := repo.UpdateTask(context.Background(), task, "Status"); err != nil {
				t.Fatalf("update: %v", err)
			}
			if *writes != 0 {
				t.Errorf("saving the task wrote its messages or tool calls %d times, want 0", *writes)
			}
			if m, c := historyOf(t, d.db, dtTaskID); m != historyRows || c != historyRows {
				t.Errorf("after the save: %d messages and %d tool calls, want %d of each", m, c, historyRows)
			}
		})
	}
}

// Deleting a task removes all of its history however long it is: the child
// deletes go by task id, so they bind one value whatever the count.
func TestDeleteTask_LongHistory(t *testing.T) {
	for _, d := range historyDBs(t) {
		t.Run(d.name, func(t *testing.T) {
			repo := New(&mockDB{db: d.db})
			seedLongTask(t, d.db)

			if err := repo.DeleteTask(context.Background(), dtWorkspaceID, dtTaskID, dtUserID); err != nil {
				t.Fatalf("delete a task with %d tool calls: %v", historyRows, err)
			}
			if m, c := historyOf(t, d.db, dtTaskID); m != 0 || c != 0 {
				t.Errorf("after the delete: %d messages and %d tool calls, want none", m, c)
			}
		})
	}
}

// A task created from a copy of a loaded one starts with no history, and the
// task it was copied from keeps all of its own, on every create path.
func TestCreateTasks_NeverMoveAnotherTasksHistory(t *testing.T) {
	const copyID = dtTaskID + 1
	creates := map[string]func(r Repository, db *gorm.DB, task model.Task) error{
		"CreateTask": func(r Repository, _ *gorm.DB, task model.Task) error {
			_, err := r.CreateTask(context.Background(), task)
			return err
		},
		"CreateTaskWithMessages": func(r Repository, _ *gorm.DB, task model.Task) error {
			_, err := r.CreateTaskWithMessages(context.Background(), task, nil)
			return err
		},
		"CreateTaskLinkingPanoDoc": func(r Repository, db *gorm.DB, task model.Task) error {
			now := time.Now().UTC()
			if err := db.Create(&model.PanoDoc{ID: 1, UserID: dtUserID, CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(time.Hour)}).Error; err != nil {
				return err
			}
			link := model.PanoDocTask{ID: 1, UserID: dtUserID, DocID: 1, TaskID: task.ID, WorkspaceID: task.WorkspaceID}
			_, err := r.CreateTaskLinkingPanoDoc(context.Background(), task, link, now)
			return err
		},
	}
	for name, create := range creates {
		t.Run(name, func(t *testing.T) {
			for _, d := range historyDBs(t) {
				t.Run(d.name, func(t *testing.T) {
					repo := New(&mockDB{db: d.db})
					task := seedLongTask(t, d.db)
					writes := countChildWrites(t, d.db)

					task.ID = copyID
					task.Status = "notstarted"
					if err := create(repo, d.db, task); err != nil {
						t.Fatalf("create a task copied from one with %d tool calls: %v", historyRows, err)
					}
					if *writes != 0 {
						t.Errorf("creating the task wrote messages or tool calls %d times, want 0", *writes)
					}
					if m, c := historyOf(t, d.db, copyID); m != 0 || c != 0 {
						t.Errorf("the new task has %d messages and %d tool calls, want none", m, c)
					}
					if m, c := historyOf(t, d.db, dtTaskID); m != historyRows || c != historyRows {
						t.Errorf("the original kept %d messages and %d tool calls, want %d of each", m, c, historyRows)
					}
				})
			}
		})
	}
}

// A save reads only the task row: none of its history is loaded, and a task in
// another workspace or of another user is not found.
func TestGetTaskRow_ReadsOnlyTheRow(t *testing.T) {
	for _, d := range historyDBs(t) {
		t.Run(d.name, func(t *testing.T) {
			ctx := context.Background()
			repo := New(&mockDB{db: d.db})
			seedLongTask(t, d.db)

			var childReads int
			if err := d.db.Callback().Query().After("gorm:query").Register("test:count_child_reads", func(tx *gorm.DB) {
				if tx.Statement.Table == "messages" || tx.Statement.Table == "tool_calls" {
					childReads++
				}
			}); err != nil {
				t.Fatalf("register: %v", err)
			}

			task, err := repo.GetTaskRow(ctx, dtWorkspaceID, dtTaskID, dtUserID)
			if err != nil {
				t.Fatalf("get the row of a task with %d tool calls: %v", historyRows, err)
			}
			if task.ID != dtTaskID || task.Status != "ongoing" {
				t.Errorf("got task %d with status %q, want %d ongoing", task.ID, task.Status, dtTaskID)
			}
			if childReads != 0 || len(task.Messages) != 0 || len(task.ToolCalls) != 0 {
				t.Errorf("read %d child tables, got %d messages and %d tool calls; want the row only", childReads, len(task.Messages), len(task.ToolCalls))
			}

			if _, err := repo.GetTaskRow(ctx, dtWorkspaceID+1, dtTaskID, dtUserID); err != ErrNotFound {
				t.Errorf("another workspace: got %v, want ErrNotFound", err)
			}
			if _, err := repo.GetTaskRow(ctx, dtWorkspaceID, dtTaskID, dtUserID+1); err != ErrNotFound {
				t.Errorf("another user: got %v, want ErrNotFound", err)
			}
		})
	}
}
