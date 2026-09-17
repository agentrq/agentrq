// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package base

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/agentrq/agentrq/backend/internal/data/model"
)

func workspaceDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&model.Workspace{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	now := time.Now()
	rows := []model.Workspace{
		{ID: 7, CreatedAt: now, UpdatedAt: now, UserID: memUserID, Name: "Q3 migration"},
		{ID: 8, CreatedAt: now, UpdatedAt: now, UserID: memUserID, Name: "Terminal probe"},
		{ID: 9, CreatedAt: now, UpdatedAt: now, UserID: memOtherUserID, Name: "Somebody else's"},
	}
	for _, w := range rows {
		if err := db.Create(&w).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	return db
}

func TestWorkspaceNamesByID(t *testing.T) {
	repo := New(&mockDB{db: workspaceDB(t)})

	got, err := repo.WorkspaceNamesByID(context.Background(), []int64{7, 8}, memUserID)
	if err != nil {
		t.Fatalf("WorkspaceNamesByID: %v", err)
	}
	if got[7] != "Q3 migration" || got[8] != "Terminal probe" {
		t.Errorf("names = %v", got)
	}
}

// Scoped to the user like every other read here. A name is not worth much on
// its own, but a query that would return somebody else's is one that will be
// reused where it matters.
func TestWorkspaceNamesAreScopedToTheirOwner(t *testing.T) {
	repo := New(&mockDB{db: workspaceDB(t)})

	got, err := repo.WorkspaceNamesByID(context.Background(), []int64{7, 9}, memUserID)
	if err != nil {
		t.Fatalf("WorkspaceNamesByID: %v", err)
	}
	if _, leaked := got[9]; leaked {
		t.Error("a workspace belonging to another account was named")
	}
	if got[7] != "Q3 migration" {
		t.Errorf("names = %v", got)
	}
}

// Asking for nothing is the ordinary case — a machine whose sessions predate
// the field — and must not become a query with an empty IN clause.
func TestWorkspaceNamesForNothing(t *testing.T) {
	repo := New(&mockDB{db: workspaceDB(t)})

	got, err := repo.WorkspaceNamesByID(context.Background(), nil, memUserID)
	if err != nil {
		t.Fatalf("WorkspaceNamesByID: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("names = %v, want none", got)
	}
}

// An id nobody owns simply is not in the answer, rather than an error: a
// workspace can be deleted while its session row is still being read.
func TestAMissingWorkspaceIsNotAnError(t *testing.T) {
	repo := New(&mockDB{db: workspaceDB(t)})

	got, err := repo.WorkspaceNamesByID(context.Background(), []int64{404}, memUserID)
	if err != nil {
		t.Fatalf("WorkspaceNamesByID: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("names = %v, want none", got)
	}
}

// A failing query has to reach the caller, which answers it by leaving the
// sessions unnamed rather than by guessing at names.
func TestWorkspaceNamesReportsAFailure(t *testing.T) {
	// A database with no workspaces table at all: the query cannot run.
	repo := New(&mockDB{db: memoryDB(t)})

	if _, err := repo.WorkspaceNamesByID(context.Background(), []int64{7}, memUserID); err == nil {
		t.Error("a query that could not run reported success")
	}
}
