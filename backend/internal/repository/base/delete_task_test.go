// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package base

import (
	"context"
	"testing"
	"time"

	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// Deleting a task used to fail with "violates foreign key constraint": the
// delete cleared the task's messages but not its tool calls, and Task declares
// both as associations, so AutoMigrate gives both a real foreign key back to
// tasks.id. The messages half was handled; the tool_calls half held the row.
//
// The database here is built the way app.go builds the real one — same models,
// same AutoMigrate, foreign keys left enabled — so the constraint under test is
// the one production actually has.
func deleteTaskDB(t *testing.T) *gorm.DB {
	t.Helper()

	// SQLite only enforces foreign keys when asked; Postgres always does. The
	// pragma makes this test see what the reported deployment saw.
	db, err := gorm.Open(sqlite.Open("file::memory:?_pragma=foreign_keys(1)"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&model.Task{}, &model.Message{}, &model.ToolCall{}, &model.SlackTaskThread{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

const (
	dtWorkspaceID = int64(498041479541817345)
	dtUserID      = int64(482467435371298817)
	dtTaskID      = int64(599436328429420545)
)

// seedTask writes a task with one of everything that points at it.
func seedTask(t *testing.T, db *gorm.DB) {
	t.Helper()
	now := time.Now()

	if err := db.Create(&model.Task{
		ID: dtTaskID, CreatedAt: now, UpdatedAt: now,
		WorkspaceID: dtWorkspaceID, UserID: dtUserID,
		Status: "completed", Title: "a task with history",
	}).Error; err != nil {
		t.Fatalf("seed task: %v", err)
	}
	if err := db.Create(&model.Message{
		ID: 1, CreatedAt: now, TaskID: dtTaskID, UserID: dtUserID, Sender: "human", Text: "hello",
	}).Error; err != nil {
		t.Fatalf("seed message: %v", err)
	}
	if err := db.Create(&model.ToolCall{
		ID: 2, CreatedAt: now, TaskID: dtTaskID, ToolName: "Bash", Status: "allowed",
	}).Error; err != nil {
		t.Fatalf("seed tool call: %v", err)
	}
	if err := db.Create(&model.SlackTaskThread{
		TaskID: dtTaskID, WorkspaceID: dtWorkspaceID, SlackChannelID: "C1", ThreadTS: "1.2",
	}).Error; err != nil {
		t.Fatalf("seed slack thread: %v", err)
	}
}

func TestDeleteTask_WithToolCalls(t *testing.T) {
	db := deleteTaskDB(t)
	seedTask(t, db)
	repo := New(&mockDB{db: db})

	if err := repo.DeleteTask(context.Background(), dtWorkspaceID, dtTaskID, dtUserID); err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}

	// The task is gone, and so is everything that pointed at it — an orphaned
	// tool call would keep a deleted task's history addressable.
	for _, c := range []struct {
		what  string
		model any
		where string
	}{
		{"task", &model.Task{}, "id = ?"},
		{"messages", &model.Message{}, "task_id = ?"},
		{"tool calls", &model.ToolCall{}, "task_id = ?"},
		{"slack thread", &model.SlackTaskThread{}, "task_id = ?"},
	} {
		var n int64
		if err := db.Model(c.model).Where(c.where, dtTaskID).Count(&n).Error; err != nil {
			t.Fatalf("count %s: %v", c.what, err)
		}
		if n != 0 {
			t.Errorf("%s: %d row(s) left behind", c.what, n)
		}
	}
}

// Another task's rows must survive: the delete is scoped to one task, and a
// WHERE that forgot its task_id would take the whole table with it.
func TestDeleteTask_LeavesOtherTasksAlone(t *testing.T) {
	db := deleteTaskDB(t)
	seedTask(t, db)

	other := int64(599436328429420546)
	now := time.Now()
	if err := db.Create(&model.Task{
		ID: other, CreatedAt: now, UpdatedAt: now,
		WorkspaceID: dtWorkspaceID, UserID: dtUserID, Status: "ongoing", Title: "keep me",
	}).Error; err != nil {
		t.Fatalf("seed other task: %v", err)
	}
	if err := db.Create(&model.ToolCall{ID: 3, CreatedAt: now, TaskID: other, ToolName: "Read", Status: "allowed"}).Error; err != nil {
		t.Fatalf("seed other tool call: %v", err)
	}
	if err := db.Create(&model.Message{ID: 4, CreatedAt: now, TaskID: other, UserID: dtUserID, Sender: "agent", Text: "hi"}).Error; err != nil {
		t.Fatalf("seed other message: %v", err)
	}

	repo := New(&mockDB{db: db})
	if err := repo.DeleteTask(context.Background(), dtWorkspaceID, dtTaskID, dtUserID); err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}

	for _, c := range []struct {
		what  string
		model any
	}{{"task", &model.Task{}}, {"tool call", &model.ToolCall{}}, {"message", &model.Message{}}} {
		var n int64
		where := "task_id = ?"
		if c.what == "task" {
			where = "id = ?"
		}
		if err := db.Model(c.model).Where(where, other).Count(&n).Error; err != nil {
			t.Fatalf("count other %s: %v", c.what, err)
		}
		if n != 1 {
			t.Errorf("other task's %s: expected 1 row, got %d", c.what, n)
		}
	}
}

// A task nobody owns is not deletable, and the children of the task that was
// named must survive a refusal — the whole thing is one transaction.
func TestDeleteTask_WrongOwnerChangesNothing(t *testing.T) {
	db := deleteTaskDB(t)
	seedTask(t, db)
	repo := New(&mockDB{db: db})

	err := repo.DeleteTask(context.Background(), dtWorkspaceID, dtTaskID, dtUserID+1)
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	for _, c := range []struct {
		what  string
		model any
		where string
	}{
		{"task", &model.Task{}, "id = ?"},
		{"messages", &model.Message{}, "task_id = ?"},
		{"tool calls", &model.ToolCall{}, "task_id = ?"},
	} {
		var n int64
		if err := db.Model(c.model).Where(c.where, dtTaskID).Count(&n).Error; err != nil {
			t.Fatalf("count %s: %v", c.what, err)
		}
		if n != 1 {
			t.Errorf("%s: expected the row kept after a refused delete, got %d", c.what, n)
		}
	}
}

// Deleting a workspace had the same gap as deleting a task, unreported only
// because a workspace is deleted far less often. It clears every task in the
// workspace, so it has to clear every task's tool calls first.
func TestDeleteWorkspace_WithToolCalls(t *testing.T) {
	db := deleteTaskDB(t)
	if err := db.AutoMigrate(&model.Workspace{}); err != nil {
		t.Fatalf("migrate workspace: %v", err)
	}
	now := time.Now()
	if err := db.Create(&model.Workspace{
		ID: dtWorkspaceID, CreatedAt: now, UpdatedAt: now, UserID: dtUserID, Name: "doomed",
	}).Error; err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	seedTask(t, db)

	repo := New(&mockDB{db: db})
	if err := repo.DeleteWorkspace(context.Background(), dtWorkspaceID, dtUserID); err != nil {
		t.Fatalf("DeleteWorkspace: %v", err)
	}

	for _, c := range []struct {
		what  string
		model any
		where string
	}{
		{"workspace", &model.Workspace{}, "id = ?"},
		{"tasks", &model.Task{}, "workspace_id = ?"},
		{"messages", &model.Message{}, "task_id = ?"},
		{"tool calls", &model.ToolCall{}, "task_id = ?"},
		{"slack thread", &model.SlackTaskThread{}, "workspace_id = ?"},
	} {
		id := dtWorkspaceID
		if c.where == "task_id = ?" {
			id = dtTaskID
		}
		var n int64
		if err := db.Model(c.model).Where(c.where, id).Count(&n).Error; err != nil {
			t.Fatalf("count %s: %v", c.what, err)
		}
		if n != 0 {
			t.Errorf("%s: %d row(s) left behind", c.what, n)
		}
	}
}

// A failure part-way through must abort the whole delete rather than leave the
// task stripped of half its history. Dropping one of the child tables makes the
// step that touches it fail for a reason the transaction cannot recover from —
// once per child, so every error return on the way down is exercised.
func TestDeleteTask_RollsBackWhenAChildDeleteFails(t *testing.T) {
	for _, drop := range []struct {
		name  string
		table any
	}{
		{"messages", &model.Message{}},
		{"tool calls", &model.ToolCall{}},
		{"slack thread", &model.SlackTaskThread{}},
	} {
		t.Run(drop.name, func(t *testing.T) {
			db := deleteTaskDB(t)
			seedTask(t, db)
			if err := db.Migrator().DropTable(drop.table); err != nil {
				t.Fatalf("drop %s: %v", drop.name, err)
			}

			repo := New(&mockDB{db: db})
			if err := repo.DeleteTask(context.Background(), dtWorkspaceID, dtTaskID, dtUserID); err == nil {
				t.Fatal("expected an error when a child delete fails")
			}

			// Whatever was deleted before the failure has to come back: a
			// failed delete that still destroyed the conversation would be
			// worse than the bug this all fixes.
			var tasks int64
			if err := db.Model(&model.Task{}).Where("id = ?", dtTaskID).Count(&tasks).Error; err != nil {
				t.Fatalf("count tasks: %v", err)
			}
			if tasks != 1 {
				t.Errorf("rollback failed: task count %d, expected 1", tasks)
			}
		})
	}
}

// The same guarantee for the workspace path, which clears the same children.
func TestDeleteWorkspace_RollsBackWhenAChildDeleteFails(t *testing.T) {
	for _, drop := range []struct {
		name  string
		table any
	}{
		{"messages", &model.Message{}},
		{"tool calls", &model.ToolCall{}},
		{"slack thread", &model.SlackTaskThread{}},
		{"tasks", &model.Task{}},
	} {
		t.Run(drop.name, func(t *testing.T) {
			db := deleteTaskDB(t)
			if err := db.AutoMigrate(&model.Workspace{}); err != nil {
				t.Fatalf("migrate workspace: %v", err)
			}
			now := time.Now()
			if err := db.Create(&model.Workspace{
				ID: dtWorkspaceID, CreatedAt: now, UpdatedAt: now, UserID: dtUserID, Name: "doomed",
			}).Error; err != nil {
				t.Fatalf("seed workspace: %v", err)
			}
			seedTask(t, db)
			if err := db.Migrator().DropTable(drop.table); err != nil {
				t.Fatalf("drop %s: %v", drop.name, err)
			}

			repo := New(&mockDB{db: db})
			if err := repo.DeleteWorkspace(context.Background(), dtWorkspaceID, dtUserID); err == nil {
				t.Fatal("expected an error when a child delete fails")
			}

			var workspaces int64
			if err := db.Model(&model.Workspace{}).Where("id = ?", dtWorkspaceID).Count(&workspaces).Error; err != nil {
				t.Fatalf("count workspaces: %v", err)
			}
			if workspaces != 1 {
				t.Errorf("rollback failed: workspace count %d, expected 1", workspaces)
			}
		})
	}
}

// A workspace that is not yours is not deletable, which is the other branch of
// the row-count check.
func TestDeleteWorkspace_WrongOwnerChangesNothing(t *testing.T) {
	db := deleteTaskDB(t)
	if err := db.AutoMigrate(&model.Workspace{}); err != nil {
		t.Fatalf("migrate workspace: %v", err)
	}
	now := time.Now()
	if err := db.Create(&model.Workspace{
		ID: dtWorkspaceID, CreatedAt: now, UpdatedAt: now, UserID: dtUserID, Name: "not yours",
	}).Error; err != nil {
		t.Fatalf("seed workspace: %v", err)
	}

	repo := New(&mockDB{db: db})
	if err := repo.DeleteWorkspace(context.Background(), dtWorkspaceID, dtUserID+1); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	var workspaces int64
	if err := db.Model(&model.Workspace{}).Where("id = ?", dtWorkspaceID).Count(&workspaces).Error; err != nil {
		t.Fatalf("count workspaces: %v", err)
	}
	if workspaces != 1 {
		t.Errorf("workspace should survive a refused delete, got count %d", workspaces)
	}
}

// Deleting one workspace must not touch another's rows.
//
// This guards the subquery specifically. DeleteWorkspace builds
// `SELECT id FROM tasks WHERE workspace_id = ?` once and feeds it to two
// deletes, and a *gorm.DB carries its conditions with it — so if the second use
// inherited anything from the first, or lost its WHERE, this would take the
// other workspace's tool calls with it and the counts below would drop to zero.
func TestDeleteWorkspace_LeavesOtherWorkspacesAlone(t *testing.T) {
	db := deleteTaskDB(t)
	if err := db.AutoMigrate(&model.Workspace{}); err != nil {
		t.Fatalf("migrate workspace: %v", err)
	}
	now := time.Now()

	otherWorkspace := int64(498041479541817346)
	otherTask := int64(599436328429420547)
	for _, w := range []struct {
		id   int64
		name string
	}{{dtWorkspaceID, "doomed"}, {otherWorkspace, "keep me"}} {
		if err := db.Create(&model.Workspace{
			ID: w.id, CreatedAt: now, UpdatedAt: now, UserID: dtUserID, Name: w.name,
		}).Error; err != nil {
			t.Fatalf("seed workspace %s: %v", w.name, err)
		}
	}
	seedTask(t, db)

	if err := db.Create(&model.Task{
		ID: otherTask, CreatedAt: now, UpdatedAt: now,
		WorkspaceID: otherWorkspace, UserID: dtUserID, Status: "ongoing", Title: "survivor",
	}).Error; err != nil {
		t.Fatalf("seed other task: %v", err)
	}
	if err := db.Create(&model.Message{ID: 10, CreatedAt: now, TaskID: otherTask, UserID: dtUserID, Sender: "human", Text: "still here"}).Error; err != nil {
		t.Fatalf("seed other message: %v", err)
	}
	if err := db.Create(&model.ToolCall{ID: 11, CreatedAt: now, TaskID: otherTask, ToolName: "Grep", Status: "allowed"}).Error; err != nil {
		t.Fatalf("seed other tool call: %v", err)
	}

	repo := New(&mockDB{db: db})
	if err := repo.DeleteWorkspace(context.Background(), dtWorkspaceID, dtUserID); err != nil {
		t.Fatalf("DeleteWorkspace: %v", err)
	}

	for _, c := range []struct {
		what  string
		model any
		where string
		id    int64
	}{
		{"workspace", &model.Workspace{}, "id = ?", otherWorkspace},
		{"task", &model.Task{}, "id = ?", otherTask},
		{"message", &model.Message{}, "task_id = ?", otherTask},
		{"tool call", &model.ToolCall{}, "task_id = ?", otherTask},
	} {
		var n int64
		if err := db.Model(c.model).Where(c.where, c.id).Count(&n).Error; err != nil {
			t.Fatalf("count other %s: %v", c.what, err)
		}
		if n != 1 {
			t.Errorf("other workspace's %s: expected 1 row, got %d", c.what, n)
		}
	}
}
