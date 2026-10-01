// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package base

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func openScheduledTaskDB(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)", filepath.Join(t.TempDir(), "scheduled.db"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := db.AutoMigrate(&model.Task{}, &model.TaskStateTransition{}, &model.TaskLatency{}); err != nil {
		t.Fatalf("migrate database: %v", err)
	}
	return db
}

func TestScheduledTaskActiveChildUniqueIndex(t *testing.T) {
	db := openScheduledTaskDB(t)
	now := time.Now()

	first := model.Task{
		ID: 1, CreatedAt: now, UpdatedAt: now, UserID: 1, WorkspaceID: 10,
		ParentID: 100, Status: "notstarted",
	}
	if err := db.Create(&first).Error; err != nil {
		t.Fatalf("create first active child: %v", err)
	}

	second := model.Task{
		ID: 2, CreatedAt: now, UpdatedAt: now, UserID: 1, WorkspaceID: 10,
		ParentID: 100, Status: "ongoing",
	}
	if err := db.Create(&second).Error; !errors.Is(err, gorm.ErrDuplicatedKey) {
		t.Fatalf("second active child error = %v, want %v", err, gorm.ErrDuplicatedKey)
	}

	if err := db.Model(&model.Task{}).Where("id = ?", first.ID).Update("status", "completed").Error; err != nil {
		t.Fatalf("complete first child: %v", err)
	}
	third := model.Task{
		ID: 3, CreatedAt: now, UpdatedAt: now, UserID: 1, WorkspaceID: 10,
		ParentID: 100, Status: "notstarted",
	}
	if err := db.Create(&third).Error; err != nil {
		t.Fatalf("create active child after completion: %v", err)
	}
}

func TestScheduledTaskConcurrentCreateOnlyOneActiveChild(t *testing.T) {
	db := openScheduledTaskDB(t)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("database handle: %v", err)
	}
	sqlDB.SetMaxOpenConns(2)

	repo := New(&mockDB{db: db})
	now := time.Now()
	start := make(chan struct{})
	results := make(chan error, 2)

	for i := int64(1); i <= 2; i++ {
		go func(id int64) {
			<-start
			_, err := repo.CreateTask(context.Background(), model.Task{
				ID: id, CreatedAt: now, UpdatedAt: now, UserID: 1, WorkspaceID: 10,
				ParentID: 100, Status: "notstarted",
			})
			results <- err
		}(i)
	}
	close(start)

	var created, duplicates int
	for i := 0; i < 2; i++ {
		switch err := <-results; {
		case err == nil:
			created++
		case errors.Is(err, gorm.ErrDuplicatedKey):
			duplicates++
		default:
			t.Fatalf("unexpected create error: %v", err)
		}
	}
	if created != 1 || duplicates != 1 {
		t.Fatalf("created = %d, duplicates = %d; want exactly one of each", created, duplicates)
	}
}
