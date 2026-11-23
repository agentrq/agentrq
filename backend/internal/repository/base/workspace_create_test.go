// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package base

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/agentrq/agentrq/backend/internal/data/model"
)

// A workspace is stored as given, false included: the column's default of
// true must not stand in for a false, as it does for a fork of a parent that
// had clear-context turned off.
func TestCreateWorkspace_KeepsAFalseClearContextDefault(t *testing.T) {
	db := workspaceDB(t)
	repo := New(&mockDB{db: db})
	for id, want := range map[int64]bool{1: false, 2: true} {
		created, err := repo.CreateWorkspace(context.Background(), model.Workspace{
			ID: id, UserID: memUserID, Name: "api", CreatedAt: time.Now(), ClearContextDefault: want,
		})
		if err != nil || created.ClearContextDefault != want {
			t.Fatalf("CreateWorkspace returned %+v and error %v, want clearContextDefault %v and no error", created, err, want)
		}
		got, err := repo.GetWorkspace(context.Background(), id, memUserID)
		if err != nil || got.ClearContextDefault != want {
			t.Errorf("the stored clearContextDefault is %v (read error %v), want %v", got.ClearContextDefault, err, want)
		}
	}
}

func TestCreateWorkspace_ReturnsAFailedWriteAndKeepsNothing(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	repo := New(&mockDB{db: db})
	if _, err := repo.CreateWorkspace(context.Background(), model.Workspace{ID: 1}); err == nil {
		t.Error("no workspaces table, and no error")
	}

	db = workspaceDB(t)
	errUpdate := errors.New("updating clear_context_default failed")
	if err := db.Callback().Update().Before("gorm:update").Register("fail", func(tx *gorm.DB) { _ = tx.AddError(errUpdate) }); err != nil {
		t.Fatal(err)
	}
	repo = New(&mockDB{db: db})
	if _, err := repo.CreateWorkspace(context.Background(), model.Workspace{ID: 1, UserID: memUserID}); !errors.Is(err, errUpdate) {
		t.Errorf("CreateWorkspace returned %v, want the failed update's error %v", err, errUpdate)
	}
	var kept int64
	db.Model(&model.Workspace{}).Where("id = ?", 1).Count(&kept)
	if kept != 0 {
		t.Error("the workspace was kept when writing its setting back failed")
	}
}

// Recording a fork's folder writes the folder and nothing else, so a parent
// save that lands meanwhile keeps the settings it copied to the fork.
func TestSetWorkingDirectory_WritesOnlyTheFolder(t *testing.T) {
	db := workspaceDB(t)
	repo := New(&mockDB{db: db})
	ctx := context.Background()
	stale, err := repo.GetWorkspace(ctx, 7, memUserID)
	if err != nil {
		t.Fatal(err)
	}
	// The parent's save reaches the fork after it was read.
	if err := db.Model(&model.Workspace{}).Where("id = ?", 7).Update("allow_all_commands", true).Error; err != nil {
		t.Fatal(err)
	}

	if err := repo.SetWorkingDirectory(ctx, stale.ID, memUserID, "/home/u/.agentrq/forks/7"); err != nil {
		t.Fatal(err)
	}
	got, _ := repo.GetWorkspace(ctx, 7, memUserID)
	if got.WorkingDirectory != "/home/u/.agentrq/forks/7" || !got.AllowAllCommands || !got.UpdatedAt.After(stale.UpdatedAt) {
		t.Errorf("the stored workspace is %+v, want the folder set and the parent's setting kept", got)
	}
	if err := repo.SetWorkingDirectory(ctx, 9, memUserID, "/elsewhere"); err != nil {
		t.Fatal(err)
	}
	var other model.Workspace
	db.First(&other, 9)
	if other.WorkingDirectory != "" {
		t.Error("another account's workspace was written")
	}
}
