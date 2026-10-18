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

// seedPlanTask creates the task dtTaskID, which SaveTaskPlan can plan.
func seedPlanTask(t *testing.T, repo Repository) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := repo.CreateTask(context.Background(), model.Task{ID: dtTaskID, CreatedAt: now, UpdatedAt: now, WorkspaceID: dtWorkspaceID, UserID: dtUserID, Status: "ongoing"}); err != nil {
		t.Fatalf("create: %v", err)
	}
}

// A plan is written once and then replaced whole, each in one statement.
func TestSaveTaskPlan(t *testing.T) {
	for _, d := range historyDBs(t) {
		t.Run(d.name, func(t *testing.T) {
			ctx := context.Background()
			repo := New(&mockDB{db: d.db})
			seedPlanTask(t, repo)

			statements := 0
			count := func(*gorm.DB) { statements++ }
			if err := d.db.Callback().Row().Before("gorm:row").Register("test:count_row", count); err != nil {
				t.Fatal(err)
			}
			first := time.Now().UTC().Truncate(time.Second)
			p, err := repo.SaveTaskPlan(ctx, model.TaskPlan{ID: dtTaskID, UpdatedAt: first, Content: "- [ ] Read the code"}, dtWorkspaceID, dtUserID)
			if err != nil {
				t.Fatal(err)
			}
			if p.ID != dtTaskID || p.Version != 1 || p.Content != "- [ ] Read the code" || !p.CreatedAt.Equal(first) {
				t.Fatalf("first save = %+v, want version 1 of the task's plan", p)
			}

			later := first.Add(time.Minute)
			p, err = repo.SaveTaskPlan(ctx, model.TaskPlan{ID: dtTaskID, UpdatedAt: later, Content: "- [x] Read the code"}, dtWorkspaceID, dtUserID)
			if err != nil {
				t.Fatal(err)
			}
			if p.Version != 2 || p.Content != "- [x] Read the code" || !p.CreatedAt.Equal(first) || !p.UpdatedAt.Equal(later) {
				t.Fatalf("second save = %+v, want version 2, replaced, created at the first save", p)
			}
			if statements != 2 {
				t.Errorf("two saves sent %d statements, want 2", statements)
			}

			got, err := repo.GetTaskPlan(ctx, dtTaskID)
			if err != nil || got.Content != p.Content || got.Version != 2 {
				t.Fatalf("GetTaskPlan = %+v, %v; want the saved plan", got, err)
			}
		})
	}
}

// Only the owner's task, in its own workspace, can have a plan saved or removed.
func TestTaskPlan_OtherTasksAreRefused(t *testing.T) {
	for _, d := range historyDBs(t) {
		t.Run(d.name, func(t *testing.T) {
			ctx := context.Background()
			repo := New(&mockDB{db: d.db})
			seedPlanTask(t, repo)
			plan := model.TaskPlan{ID: dtTaskID, UpdatedAt: time.Now().UTC(), Content: "- [ ] step"}

			for name, ids := range map[string][2]int64{
				"another workspace": {dtWorkspaceID + 1, dtUserID},
				"another user":      {dtWorkspaceID, dtUserID + 1},
			} {
				if _, err := repo.SaveTaskPlan(ctx, plan, ids[0], ids[1]); !errors.Is(err, ErrNotFound) {
					t.Errorf("save from %s: err = %v, want ErrNotFound", name, err)
				}
			}
			if _, err := repo.SaveTaskPlan(ctx, model.TaskPlan{ID: dtTaskID + 1, Content: "x"}, dtWorkspaceID, dtUserID); !errors.Is(err, ErrNotFound) {
				t.Errorf("save for a missing task: err = %v, want ErrNotFound", err)
			}
			if _, err := repo.GetTaskPlan(ctx, dtTaskID); !errors.Is(err, ErrNotFound) {
				t.Fatalf("GetTaskPlan after refused saves: err = %v, want ErrNotFound", err)
			}

			if _, err := repo.SaveTaskPlan(ctx, plan, dtWorkspaceID, dtUserID); err != nil {
				t.Fatal(err)
			}
			if err := repo.DeleteTaskPlan(ctx, dtTaskID, dtWorkspaceID, dtUserID+1); err != nil {
				t.Fatal(err)
			}
			if _, err := repo.GetTaskPlan(ctx, dtTaskID); err != nil {
				t.Fatalf("another user's delete removed the plan: %v", err)
			}
			if err := repo.DeleteTaskPlan(ctx, dtTaskID, dtWorkspaceID, dtUserID); err != nil {
				t.Fatal(err)
			}
			if _, err := repo.GetTaskPlan(ctx, dtTaskID); !errors.Is(err, ErrNotFound) {
				t.Fatalf("GetTaskPlan after delete: err = %v, want ErrNotFound", err)
			}
			if err := repo.DeleteTaskPlan(ctx, dtTaskID, dtWorkspaceID, dtUserID); err != nil {
				t.Errorf("deleting no plan: %v, want nil", err)
			}
		})
	}
}

// A plan goes with its task, and with its task's workspace.
func TestTaskPlan_DeletedWithItsTask(t *testing.T) {
	for _, d := range historyDBs(t) {
		t.Run(d.name, func(t *testing.T) {
			ctx := context.Background()
			repo := New(&mockDB{db: d.db})
			seedPlanTask(t, repo)
			if _, err := repo.SaveTaskPlan(ctx, model.TaskPlan{ID: dtTaskID, Content: "- [ ] step"}, dtWorkspaceID, dtUserID); err != nil {
				t.Fatal(err)
			}
			if err := repo.DeleteTask(ctx, dtWorkspaceID, dtTaskID, dtUserID); err != nil {
				t.Fatal(err)
			}
			if _, err := repo.GetTaskPlan(ctx, dtTaskID); !errors.Is(err, ErrNotFound) {
				t.Fatalf("after DeleteTask: err = %v, want ErrNotFound", err)
			}
		})
	}

	db, repo := forkDB(t)
	ctx := context.Background()
	seedForkTask(t, db, wfParent, 200, "ongoing")
	if _, err := repo.SaveTaskPlan(ctx, model.TaskPlan{ID: 200, Content: "- [ ] step"}, wfParent, wfUser); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteWorkspace(ctx, wfParent, wfUser); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetTaskPlan(ctx, 200); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after DeleteWorkspace: err = %v, want ErrNotFound", err)
	}
}

// A plan that cannot be deleted keeps its task, and its workspace.
func TestTaskPlan_DeleteFailureRollsBack(t *testing.T) {
	db, repo := forkDB(t)
	ctx := context.Background()
	seedForkTask(t, db, wfParent, 200, "ongoing")
	if err := db.Callback().Delete().Before("gorm:delete").Register("fail", func(tx *gorm.DB) {
		if tx.Statement.Table == "task_plans" {
			_ = tx.AddError(errors.New("delete failed"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteTask(ctx, wfParent, 200, wfUser); err == nil || err.Error() != "delete failed" {
		t.Errorf("DeleteTask = %v, want \"delete failed\"", err)
	}
	if err := repo.DeleteWorkspace(ctx, wfParent, wfUser); err == nil || err.Error() != "delete failed" {
		t.Errorf("DeleteWorkspace = %v, want \"delete failed\"", err)
	}
	if _, err := repo.GetTaskRow(ctx, wfParent, 200, wfUser); err != nil {
		t.Errorf("the task is gone after a failed delete: %v", err)
	}
}

func TestTaskPlan_Failures(t *testing.T) {
	db := openHistoryDB(t, sqlite.Open("file::memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	ctx := context.Background()
	repo := New(&mockDB{db: db})
	if err := db.Migrator().DropTable(&model.TaskPlan{}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SaveTaskPlan(ctx, model.TaskPlan{ID: dtTaskID}, dtWorkspaceID, dtUserID); err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("SaveTaskPlan with no table: err = %v, want the database's error", err)
	}
	if _, err := repo.GetTaskPlan(ctx, dtTaskID); err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("GetTaskPlan with no table: err = %v, want the database's error", err)
	}
}
