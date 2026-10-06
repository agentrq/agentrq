// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

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
	if err := db.AutoMigrate(&model.Task{}, &model.Message{}, &model.ToolCall{}, &model.SlackTaskThread{}, &model.EventTrigger{}, &model.WorkflowStep{}, &model.TaskStateTransition{}, &model.TaskLatency{}, &model.Agent{}, &model.AgentModel{}); err != nil {
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
	if err := db.AutoMigrate(&model.Workspace{}, &model.Skill{}, &model.SkillFile{}, &model.WorkspaceSkill{}, &model.SiteShare{}, &model.ForkFolder{}, &model.TaskStateTransition{}, &model.TaskLatency{}); err != nil {
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
		{"workspace skills", &model.WorkspaceSkill{}},
		{"site shares", &model.SiteShare{}},
		{"fork folders", &model.ForkFolder{}},
		{"event triggers", &model.EventTrigger{}},
		{"workflow steps", &model.WorkflowStep{}},
		{"tasks", &model.Task{}},
	} {
		t.Run(drop.name, func(t *testing.T) {
			db := deleteTaskDB(t)
			if err := db.AutoMigrate(&model.Workspace{}, &model.Skill{}, &model.SkillFile{}, &model.WorkspaceSkill{}, &model.SiteShare{}, &model.ForkFolder{}, &model.TaskStateTransition{}, &model.TaskLatency{}); err != nil {
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
	if err := db.AutoMigrate(&model.Workspace{}, &model.Skill{}, &model.SkillFile{}, &model.WorkspaceSkill{}, &model.SiteShare{}, &model.ForkFolder{}, &model.TaskStateTransition{}, &model.TaskLatency{}); err != nil {
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
	if err := db.AutoMigrate(&model.Workspace{}, &model.Skill{}, &model.SkillFile{}, &model.WorkspaceSkill{}, &model.SiteShare{}, &model.ForkFolder{}, &model.TaskStateTransition{}, &model.TaskLatency{}); err != nil {
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

// A trigger or workflow step creating tasks in a deleted workspace has nowhere
// left to create them. Left behind, the canvas drew it as "(deleted workspace)"
// forever and every publish logged "workspace not found".
func TestDeleteWorkspace_DeletesItsTriggersAndSteps(t *testing.T) {
	db := deleteTaskDB(t)
	if err := db.AutoMigrate(&model.Workspace{}, &model.Skill{}, &model.SkillFile{}, &model.WorkspaceSkill{}, &model.SiteShare{}, &model.ForkFolder{}, &model.TaskStateTransition{}, &model.TaskLatency{}); err != nil {
		t.Fatalf("migrate workspace: %v", err)
	}
	now := time.Now()
	otherWorkspace := int64(498041479541817346)
	for _, id := range []int64{dtWorkspaceID, otherWorkspace} {
		if err := db.Create(&model.Workspace{ID: id, CreatedAt: now, UpdatedAt: now, UserID: dtUserID, Name: "w"}).Error; err != nil {
			t.Fatalf("seed workspace: %v", err)
		}
	}
	for i, ws := range []int64{dtWorkspaceID, otherWorkspace} {
		id := int64(i + 1)
		if err := db.Create(&model.EventTrigger{ID: id, EventID: 42, WorkspaceID: ws, UserID: dtUserID, Title: "t"}).Error; err != nil {
			t.Fatalf("seed trigger: %v", err)
		}
		if err := db.Create(&model.WorkflowStep{ID: id, WorkflowID: 7, EventID: 42, WorkspaceID: ws, UserID: dtUserID, Title: "s"}).Error; err != nil {
			t.Fatalf("seed step: %v", err)
		}
	}

	repo := New(&mockDB{db: db})
	if err := repo.DeleteWorkspace(context.Background(), dtWorkspaceID, dtUserID); err != nil {
		t.Fatalf("DeleteWorkspace: %v", err)
	}

	for _, m := range []any{&model.EventTrigger{}, &model.WorkflowStep{}} {
		var gone, kept int64
		db.Model(m).Where("workspace_id = ?", dtWorkspaceID).Count(&gone)
		db.Model(m).Where("workspace_id = ?", otherWorkspace).Count(&kept)
		if gone != 0 || kept != 1 {
			t.Errorf("%T: %d row(s) left in the deleted workspace, %d in the other (want 0 and 1)", m, gone, kept)
		}
	}
}

// Rows left behind by workspaces deleted or archived before either removed them
// are swept separately; only a live workspace keeps its rows.
func TestSystemDeleteOrphanedEventRouting(t *testing.T) {
	db := deleteTaskDB(t)
	if err := db.AutoMigrate(&model.Workspace{}, &model.TaskStateTransition{}, &model.TaskLatency{}); err != nil {
		t.Fatalf("migrate workspace: %v", err)
	}
	now := time.Now()
	live, archived, gone := int64(10), int64(20), int64(30)
	for _, ws := range []model.Workspace{
		{ID: live, CreatedAt: now, UpdatedAt: now, UserID: dtUserID, Name: "live"},
		{ID: archived, CreatedAt: now, UpdatedAt: now, UserID: dtUserID, Name: "archived", ArchivedAt: &now},
	} {
		if err := db.Create(&ws).Error; err != nil {
			t.Fatalf("seed workspace: %v", err)
		}
	}
	for i, ws := range []int64{live, archived, gone} {
		id := int64(i + 1)
		if err := db.Create(&model.EventTrigger{ID: id, EventID: 42, WorkspaceID: ws, UserID: dtUserID, Title: "t"}).Error; err != nil {
			t.Fatalf("seed trigger: %v", err)
		}
		if err := db.Create(&model.WorkflowStep{ID: id, WorkflowID: 7, EventID: 42, WorkspaceID: ws, UserID: dtUserID, Title: "s"}).Error; err != nil {
			t.Fatalf("seed step: %v", err)
		}
	}

	repo := New(&mockDB{db: db})
	n, err := repo.SystemDeleteOrphanedEventRouting(context.Background())
	if err != nil {
		t.Fatalf("SystemDeleteOrphanedEventRouting: %v", err)
	}
	if n != 4 {
		t.Errorf("deleted %d rows, want 4 (a trigger and a step for each of two workspaces)", n)
	}
	for _, m := range []any{&model.EventTrigger{}, &model.WorkflowStep{}} {
		var orphans, kept int64
		db.Model(m).Where("workspace_id IN ?", []int64{archived, gone}).Count(&orphans)
		db.Model(m).Where("workspace_id = ?", live).Count(&kept)
		if orphans != 0 || kept != 1 {
			t.Errorf("%T: %d orphan(s) left, %d kept (want 0 and 1)", m, orphans, kept)
		}
	}

	// Running it again finds nothing.
	if n, err := repo.SystemDeleteOrphanedEventRouting(context.Background()); err != nil || n != 0 {
		t.Errorf("second sweep: n=%d err=%v, want 0 and nil", n, err)
	}
}

// A failure in either delete rolls the sweep back and reports it.
func TestSystemDeleteOrphanedEventRouting_Fails(t *testing.T) {
	for _, drop := range []struct {
		name  string
		table any
	}{
		{"event triggers", &model.EventTrigger{}},
		{"workflow steps", &model.WorkflowStep{}},
	} {
		t.Run(drop.name, func(t *testing.T) {
			db := deleteTaskDB(t)
			if err := db.AutoMigrate(&model.Workspace{}, &model.TaskStateTransition{}, &model.TaskLatency{}); err != nil {
				t.Fatalf("migrate workspace: %v", err)
			}
			if err := db.Create(&model.EventTrigger{ID: 1, EventID: 42, WorkspaceID: 30, UserID: dtUserID, Title: "t"}).Error; err != nil {
				t.Fatalf("seed trigger: %v", err)
			}
			if err := db.Migrator().DropTable(drop.table); err != nil {
				t.Fatalf("drop %s: %v", drop.name, err)
			}

			repo := New(&mockDB{db: db})
			if n, err := repo.SystemDeleteOrphanedEventRouting(context.Background()); err == nil || n != 0 {
				t.Fatalf("expected an error and 0 rows, got n=%d err=%v", n, err)
			}
			if db.Migrator().HasTable(&model.EventTrigger{}) {
				var left int64
				db.Model(&model.EventTrigger{}).Count(&left)
				if left != 1 {
					t.Errorf("rollback failed: %d trigger(s) left, want 1", left)
				}
			}
		})
	}
}

// Archiving a workspace deletes the triggers and steps that create tasks in it,
// since nobody would see those tasks; unarchiving does not bring them back.
func TestArchiveWorkspace_DeletesItsTriggersAndSteps(t *testing.T) {
	db := deleteTaskDB(t)
	if err := db.AutoMigrate(&model.Workspace{}, &model.TaskStateTransition{}, &model.TaskLatency{}); err != nil {
		t.Fatalf("migrate workspace: %v", err)
	}
	now := time.Now()
	otherWorkspace := int64(498041479541817346)
	for _, id := range []int64{dtWorkspaceID, otherWorkspace} {
		if err := db.Create(&model.Workspace{ID: id, CreatedAt: now, UpdatedAt: now, UserID: dtUserID, Name: "w"}).Error; err != nil {
			t.Fatalf("seed workspace: %v", err)
		}
	}
	for i, ws := range []int64{dtWorkspaceID, otherWorkspace} {
		id := int64(i + 1)
		if err := db.Create(&model.EventTrigger{ID: id, EventID: 42, WorkspaceID: ws, UserID: dtUserID, Title: "t"}).Error; err != nil {
			t.Fatalf("seed trigger: %v", err)
		}
		if err := db.Create(&model.WorkflowStep{ID: id, WorkflowID: 7, EventID: 42, WorkspaceID: ws, UserID: dtUserID, Title: "s"}).Error; err != nil {
			t.Fatalf("seed step: %v", err)
		}
	}

	repo := New(&mockDB{db: db})
	updated, err := repo.ArchiveWorkspace(context.Background(), model.Workspace{
		ID: dtWorkspaceID, CreatedAt: now, UpdatedAt: now, UserID: dtUserID, Name: "w", ArchivedAt: &now,
	})
	if err != nil {
		t.Fatalf("ArchiveWorkspace: %v", err)
	}
	if updated.ArchivedAt == nil {
		t.Error("returned workspace has no ArchivedAt")
	}
	var stored model.Workspace
	if err := db.First(&stored, dtWorkspaceID).Error; err != nil || stored.ArchivedAt == nil {
		t.Errorf("workspace not stored as archived: %+v, %v", stored, err)
	}
	for _, m := range []any{&model.EventTrigger{}, &model.WorkflowStep{}} {
		var gone, kept int64
		db.Model(m).Where("workspace_id = ?", dtWorkspaceID).Count(&gone)
		db.Model(m).Where("workspace_id = ?", otherWorkspace).Count(&kept)
		if gone != 0 || kept != 1 {
			t.Errorf("%T: %d row(s) left in the archived workspace, %d in the other (want 0 and 1)", m, gone, kept)
		}
	}
}

// A failure deleting either leaves the workspace unarchived, so it never ends
// up archived with its triggers still firing into it.
func TestArchiveWorkspace_RollsBackWhenADeleteFails(t *testing.T) {
	for _, drop := range []struct {
		name  string
		table any
	}{
		{"workspaces", &model.Workspace{}},
		{"event triggers", &model.EventTrigger{}},
		{"workflow steps", &model.WorkflowStep{}},
	} {
		t.Run(drop.name, func(t *testing.T) {
			db := deleteTaskDB(t)
			if err := db.AutoMigrate(&model.Workspace{}, &model.TaskStateTransition{}, &model.TaskLatency{}); err != nil {
				t.Fatalf("migrate workspace: %v", err)
			}
			now := time.Now()
			if err := db.Create(&model.Workspace{ID: dtWorkspaceID, CreatedAt: now, UpdatedAt: now, UserID: dtUserID, Name: "w"}).Error; err != nil {
				t.Fatalf("seed workspace: %v", err)
			}
			if err := db.Migrator().DropTable(drop.table); err != nil {
				t.Fatalf("drop %s: %v", drop.name, err)
			}

			repo := New(&mockDB{db: db})
			if _, err := repo.ArchiveWorkspace(context.Background(), model.Workspace{
				ID: dtWorkspaceID, CreatedAt: now, UpdatedAt: now, UserID: dtUserID, Name: "w", ArchivedAt: &now,
			}); err == nil {
				t.Fatal("expected an error when a delete fails")
			}
			if db.Migrator().HasTable(&model.Workspace{}) {
				var stored model.Workspace
				if err := db.First(&stored, dtWorkspaceID).Error; err != nil || stored.ArchivedAt != nil {
					t.Errorf("rollback failed: %+v, %v", stored, err)
				}
			}
		})
	}
}
