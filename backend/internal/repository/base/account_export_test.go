// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package base

import (
	"context"
	"testing"
	"time"

	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func exportDB(t *testing.T) (*gorm.DB, Repository) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&model.Preset{}, &model.Memory{}, &model.PanoDoc{}, &model.Task{}, &model.Message{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db, New(&mockDB{db: db})
}

func create(t *testing.T, db *gorm.DB, rows ...any) {
	t.Helper()
	for _, r := range rows {
		if err := db.Create(r).Error; err != nil {
			t.Fatalf("create %T: %v", r, err)
		}
	}
}

func TestExportPresetsAndMemories_OnlyTheAccountsInOrder(t *testing.T) {
	db, repo := exportDB(t)
	ctx := context.Background()
	create(t, db,
		&model.Preset{ID: 1, UserID: memUserID, WorkspaceID: 7, Name: "b"},
		&model.Preset{ID: 2, UserID: memUserID, WorkspaceID: 0, Name: "z"},
		&model.Preset{ID: 3, UserID: memUserID, WorkspaceID: 7, Name: "a"},
		&model.Preset{ID: 4, UserID: memOtherUserID, WorkspaceID: 7, Name: "c"},
		&model.Memory{ID: 1, UserID: memUserID, WorkspaceID: 9, Name: "b.md"},
		&model.Memory{ID: 2, UserID: memUserID, WorkspaceID: 8, Name: "z.md"},
		&model.Memory{ID: 3, UserID: memOtherUserID, WorkspaceID: 8, Name: "a.md"},
	)

	presets, err := repo.ExportPresets(ctx, memUserID)
	if err != nil || len(presets) != 3 || presets[0].ID != 2 || presets[1].ID != 3 || presets[2].ID != 1 {
		t.Fatalf("ExportPresets returned %+v, %v; want 2, 3, 1", presets, err)
	}
	memories, err := repo.ExportMemories(ctx, memUserID)
	if err != nil || len(memories) != 2 || memories[0].ID != 2 || memories[1].ID != 1 {
		t.Fatalf("ExportMemories returned %+v, %v; want 2, 1", memories, err)
	}
}

func TestExportPanoDocs_LeavesOutTheExpiredAndKeepsTheMarkdown(t *testing.T) {
	db, repo := exportDB(t)
	now := time.Now()
	create(t, db,
		&model.PanoDoc{ID: 2, UserID: memUserID, Markdown: "# two", ExpiresAt: now.Add(time.Hour)},
		&model.PanoDoc{ID: 1, UserID: memUserID, Markdown: "# one", ExpiresAt: now.Add(time.Hour)},
		&model.PanoDoc{ID: 3, UserID: memUserID, ExpiresAt: now.Add(-time.Hour)},
		&model.PanoDoc{ID: 4, UserID: memOtherUserID, ExpiresAt: now.Add(time.Hour)},
	)
	docs, err := repo.ExportPanoDocs(context.Background(), memUserID, now)
	if err != nil || len(docs) != 2 || docs[0].ID != 1 || docs[0].Markdown != "# one" || docs[1].ID != 2 {
		t.Fatalf("ExportPanoDocs returned %+v, %v; want 1 then 2 with markdown", docs, err)
	}
}

func TestExportTasks_PagesByIDWithoutRepeats(t *testing.T) {
	db, repo := exportDB(t)
	ctx := context.Background()
	at := time.Now()
	create(t, db,
		&model.Task{ID: 30, UserID: memUserID, CreatedAt: at},
		&model.Task{ID: 10, UserID: memUserID, CreatedAt: at},
		&model.Task{ID: 20, UserID: memUserID, CreatedAt: at},
		&model.Task{ID: 15, UserID: memOtherUserID, CreatedAt: at},
	)
	first, err := repo.ExportTasks(ctx, memUserID, 0, 2)
	if err != nil || len(first) != 2 || first[0].ID != 10 || first[1].ID != 20 {
		t.Fatalf("the first page is %+v, %v; want 10, 20", first, err)
	}
	second, err := repo.ExportTasks(ctx, memUserID, 20, 2)
	if err != nil || len(second) != 1 || second[0].ID != 30 {
		t.Fatalf("the second page is %+v, %v; want 30", second, err)
	}
}

func TestExportMessages_OldestFirstWithinEachTask(t *testing.T) {
	db, repo := exportDB(t)
	ctx := context.Background()
	at := time.Now()
	create(t, db,
		&model.Message{ID: 1, TaskID: 20, CreatedAt: at},
		&model.Message{ID: 2, TaskID: 10, CreatedAt: at.Add(time.Minute)},
		&model.Message{ID: 3, TaskID: 10, CreatedAt: at},
		&model.Message{ID: 4, TaskID: 99, CreatedAt: at},
	)
	got, err := repo.ExportMessages(ctx, []int64{10, 20})
	if err != nil || len(got) != 3 || got[0].ID != 3 || got[1].ID != 2 || got[2].ID != 1 {
		t.Fatalf("ExportMessages returned %+v, %v; want 3, 2, 1", got, err)
	}
	none, err := repo.ExportMessages(ctx, nil)
	if err != nil || none == nil || len(none) != 0 {
		t.Fatalf("ExportMessages of no tasks returned %+v, %v; want an empty list", none, err)
	}
}
