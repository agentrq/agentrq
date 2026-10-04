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
)

func cronRunRepo(t *testing.T, tables ...any) (*gorm.DB, Repository) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := db.AutoMigrate(tables...); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db, New(&mockDB{db: db})
}

// failHistoryWrites makes every insert into the status history fail, as a
// database that refuses the write would.
func failHistoryWrites(t *testing.T, db *gorm.DB) {
	t.Helper()
	err := db.Callback().Create().Before("gorm:create").Register("test:fail_history", func(tx *gorm.DB) {
		if tx.Statement.Table == "task_state_transitions" {
			_ = tx.AddError(errors.New("database unavailable"))
		}
	})
	if err != nil {
		t.Fatalf("register callback: %v", err)
	}
}

var cronRunAt = time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)

func TestSystemCreateCronRun(t *testing.T) {
	ctx := context.Background()
	run := func(id int64) model.Task {
		return model.Task{ID: id, CreatedAt: cronRunAt, UserID: 1, WorkspaceID: 10, ParentID: 5, Status: "notstarted"}
	}

	t.Run("OneRunPerMinute", func(t *testing.T) {
		db, repo := cronRunRepo(t, &model.Task{}, &model.TaskStateTransition{}, &model.TaskLatency{})
		created, err := repo.SystemCreateCronRun(ctx, run(1))
		if err != nil || !created {
			t.Fatalf("expected the first run created, got %v, %v", created, err)
		}
		created, err = repo.SystemCreateCronRun(ctx, run(2))
		if err != nil || created {
			t.Fatalf("expected the second run of the minute skipped quietly, got %v, %v", created, err)
		}
		var history int64
		db.Model(&model.TaskStateTransition{}).Count(&history)
		if history != 1 {
			t.Errorf("expected history for the created run only, got %d rows", history)
		}
	})

	t.Run("DatabaseUnavailable", func(t *testing.T) {
		_, repo := cronRunRepo(t)
		if created, err := repo.SystemCreateCronRun(ctx, run(1)); err == nil || created {
			t.Fatalf("expected an error with no tasks table, got %v, %v", created, err)
		}
	})

	t.Run("HistoryWriteFails", func(t *testing.T) {
		db, repo := cronRunRepo(t, &model.Task{}, &model.TaskStateTransition{}, &model.TaskLatency{})
		failHistoryWrites(t, db)
		if created, err := repo.SystemCreateCronRun(ctx, run(1)); err == nil || created {
			t.Fatalf("expected the history failure returned, got %v, %v", created, err)
		}
		var tasks int64
		db.Model(&model.Task{}).Count(&tasks)
		if tasks != 0 {
			t.Errorf("expected the run rolled back with its history, got %d tasks", tasks)
		}
	})
}

func TestSystemStartOneTimeTask(t *testing.T) {
	ctx := context.Background()
	seed := func(t *testing.T, repo Repository) model.Task {
		t.Helper()
		tmpl, err := repo.CreateTask(ctx, model.Task{ID: 1, UserID: 1, WorkspaceID: 10, Status: "cron", CronSchedule: "0 9 1 1 *", Body: "once"})
		if err != nil {
			t.Fatalf("seed: %v", err)
		}
		tmpl.Body = "once, now"
		tmpl.CreatedAt = cronRunAt
		tmpl.UpdatedAt = cronRunAt.Add(time.Second)
		return tmpl
	}

	t.Run("StartsOnce", func(t *testing.T) {
		db, repo := cronRunRepo(t, &model.Task{}, &model.TaskStateTransition{}, &model.TaskLatency{})
		tmpl := seed(t, repo)
		started, err := repo.SystemStartOneTimeTask(ctx, tmpl)
		if err != nil || !started {
			t.Fatalf("expected the template started, got %v, %v", started, err)
		}
		started, err = repo.SystemStartOneTimeTask(ctx, tmpl)
		if err != nil || started {
			t.Fatalf("expected a second start to change nothing, got %v, %v", started, err)
		}

		var got model.Task
		db.First(&got, 1)
		if got.Status != "notstarted" || got.CronSchedule != "" || got.Body != "once, now" || !got.CreatedAt.Equal(cronRunAt) {
			t.Errorf("unexpected task after start: %+v", got)
		}
		history, _ := repo.ListTaskStateTransitions(ctx, 1)
		if len(history) != 2 || history[1].ToState != model.TaskStateFromStatus("notstarted") {
			t.Errorf("expected cron then notstarted in the history, got %+v", history)
		}
	})

	t.Run("DatabaseUnavailable", func(t *testing.T) {
		_, repo := cronRunRepo(t)
		if started, err := repo.SystemStartOneTimeTask(ctx, model.Task{ID: 1}); err == nil || started {
			t.Fatalf("expected an error with no tasks table, got %v, %v", started, err)
		}
	})

	t.Run("HistoryReadFails", func(t *testing.T) {
		db, repo := cronRunRepo(t, &model.Task{})
		if err := db.Create(&model.Task{ID: 1, Status: "cron"}).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
		if started, err := repo.SystemStartOneTimeTask(ctx, model.Task{ID: 1}); err == nil || started {
			t.Fatalf("expected an error with no history table, got %v, %v", started, err)
		}
	})

	t.Run("HistoryWriteFails", func(t *testing.T) {
		db, repo := cronRunRepo(t, &model.Task{}, &model.TaskStateTransition{}, &model.TaskLatency{})
		tmpl := seed(t, repo)
		failHistoryWrites(t, db)
		if started, err := repo.SystemStartOneTimeTask(ctx, tmpl); err == nil || started {
			t.Fatalf("expected the history failure returned, got %v, %v", started, err)
		}
		var got model.Task
		db.First(&got, 1)
		if got.Status != "cron" {
			t.Errorf("expected the start rolled back, got status %q", got.Status)
		}
	})
}
