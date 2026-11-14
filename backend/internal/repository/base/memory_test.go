// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package base

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// The database is built the way app.go builds the real one, so the unique index
// under test is the one production actually has.
func memoryDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&model.Memory{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

const (
	memUserID      = int64(482467435371298817)
	memWorkspaceID = int64(498041479541817345)
	memOtherWSID   = int64(498041479541817346)
	memOtherUserID = int64(482467435371298818)
)

func memory(id int64, userID, workspaceID int64, name, content string) model.Memory {
	now := time.Now()
	return model.Memory{
		ID: id, CreatedAt: now, UpdatedAt: now,
		UserID: userID, WorkspaceID: workspaceID, Name: name, Content: content,
	}
}

func TestSaveMemory_StoresAndReadsBack(t *testing.T) {
	repo := New(&mockDB{db: memoryDB(t)})
	ctx := context.Background()

	saved, err := repo.SaveMemory(ctx, memory(1, memUserID, memWorkspaceID, "MEMORY.md", "# Index\n"))
	if err != nil {
		t.Fatalf("SaveMemory: %v", err)
	}
	if saved.Content != "# Index\n" || saved.Name != "MEMORY.md" {
		t.Errorf("saved: got %+v", saved)
	}

	got, err := repo.GetMemory(ctx, memUserID, memWorkspaceID, "MEMORY.md")
	if err != nil {
		t.Fatalf("GetMemory: %v", err)
	}
	if got.Content != "# Index\n" {
		t.Errorf("content: got %q", got.Content)
	}
}

// A save on the version it read replaces the row in place and moves it to the
// next version, rather than failing against the unique key or adding a
// duplicate.
func TestSaveMemory_OverwritesInPlace(t *testing.T) {
	db := memoryDB(t)
	repo := New(&mockDB{db: db})
	ctx := context.Background()

	first, err := repo.SaveMemory(ctx, memory(1, memUserID, memWorkspaceID, "MEMORY.md", "before"))
	if err != nil {
		t.Fatalf("first save: %v", err)
	}
	if first.Version != 1 {
		t.Errorf("a new memory is at version %d, want 1", first.Version)
	}

	// A different generated ID, as the caller would supply on any later save.
	update := memory(2, memUserID, memWorkspaceID, "MEMORY.md", "after")
	update.Version = first.Version
	second, err := repo.SaveMemory(ctx, update)
	if err != nil {
		t.Fatalf("second save: %v", err)
	}

	if second.Content != "after" {
		t.Errorf("content: got %q, want the newer one", second.Content)
	}
	if second.Version != 2 {
		t.Errorf("the saved memory is at version %d, want 2", second.Version)
	}
	// The row keeps the identity it already had; only the content moves.
	if second.ID != first.ID {
		t.Errorf("id: got %d, want the existing row's %d", second.ID, first.ID)
	}

	var count int64
	if err := db.Model(&model.Memory{}).Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("expected one row after two saves, got %d", count)
	}
}

// Two agents that read the same version cannot both write it: the second is
// refused, and the first one's content stays.
func TestSaveMemory_RefusesAStaleVersion(t *testing.T) {
	repo := New(&mockDB{db: memoryDB(t)})
	ctx := context.Background()

	if _, err := repo.SaveMemory(ctx, memory(1, memUserID, memWorkspaceID, "memory.md", "v1")); err != nil {
		t.Fatalf("seed: %v", err)
	}
	save := func(id int64, content string) error {
		m := memory(id, memUserID, memWorkspaceID, "memory.md", content)
		m.Version = 1
		_, err := repo.SaveMemory(ctx, m)
		return err
	}
	if err := save(2, "first agent"); err != nil {
		t.Fatalf("the first save on version 1 returned %v, want it stored", err)
	}
	if err := save(3, "second agent"); !errors.Is(err, ErrVersionConflict) {
		t.Errorf("a second save on version 1 returned %v, want ErrVersionConflict", err)
	}
	got, err := repo.GetMemory(ctx, memUserID, memWorkspaceID, "memory.md")
	if err != nil {
		t.Fatalf("GetMemory: %v", err)
	}
	if got.Content != "first agent" || got.Version != 2 {
		t.Errorf("the memory is %q at version %d, want the first agent's at version 2", got.Content, got.Version)
	}
}

// A save with no version only creates: one over a memory that already exists
// would overwrite learnings its writer never read.
func TestSaveMemory_CreateRefusesAMemoryThatExists(t *testing.T) {
	repo := New(&mockDB{db: memoryDB(t)})
	ctx := context.Background()

	if _, err := repo.SaveMemory(ctx, memory(1, memUserID, memWorkspaceID, "memory.md", "kept")); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := repo.SaveMemory(ctx, memory(2, memUserID, memWorkspaceID, "memory.md", "blind")); !errors.Is(err, ErrVersionConflict) {
		t.Errorf("creating a memory that exists returned %v, want ErrVersionConflict", err)
	}
	got, err := repo.GetMemory(ctx, memUserID, memWorkspaceID, "memory.md")
	if err != nil {
		t.Fatalf("GetMemory: %v", err)
	}
	if got.Content != "kept" {
		t.Errorf("the memory is %q, want %q", got.Content, "kept")
	}
}

// Updating a memory deleted since it was read is not found, not a conflict:
// there is nothing to merge with.
func TestSaveMemory_UpdateOfADeletedMemoryIsNotFound(t *testing.T) {
	repo := New(&mockDB{db: memoryDB(t)})

	m := memory(1, memUserID, memWorkspaceID, "gone.md", "x")
	m.Version = 4
	if _, err := repo.SaveMemory(context.Background(), m); !errors.Is(err, ErrNotFound) {
		t.Errorf("updating a memory that does not exist returned %v, want ErrNotFound", err)
	}
}

// An update that cannot land surfaces the failure.
func TestSaveMemory_ReportsAnUpdateThatFailed(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	// Deliberately not migrated: there is no memories table to write to.
	repo := New(&mockDB{db: db})

	m := memory(1, memUserID, memWorkspaceID, "memory.md", "x")
	m.Version = 1
	if _, err := repo.SaveMemory(context.Background(), m); err == nil {
		t.Error("expected an error when the update cannot land")
	}
}

// The unique key spans all three columns. Tagging only Name would make a memory
// name unique across every workspace and every account, so one workspace saving
// MEMORY.md would take the name from everyone else.
func TestSaveMemory_NameIsScopedToOwnerAndWorkspace(t *testing.T) {
	repo := New(&mockDB{db: memoryDB(t)})
	ctx := context.Background()

	if _, err := repo.SaveMemory(ctx, memory(1, memUserID, memWorkspaceID, "MEMORY.md", "workspace one")); err != nil {
		t.Fatalf("first workspace: %v", err)
	}
	if _, err := repo.SaveMemory(ctx, memory(2, memUserID, memOtherWSID, "MEMORY.md", "workspace two")); err != nil {
		t.Fatalf("same name in another workspace must be allowed: %v", err)
	}
	if _, err := repo.SaveMemory(ctx, memory(3, memOtherUserID, memWorkspaceID, "MEMORY.md", "another owner")); err != nil {
		t.Fatalf("same name for another owner must be allowed: %v", err)
	}

	for _, tc := range []struct {
		userID, workspaceID int64
		want                string
	}{
		{memUserID, memWorkspaceID, "workspace one"},
		{memUserID, memOtherWSID, "workspace two"},
		{memOtherUserID, memWorkspaceID, "another owner"},
	} {
		got, err := repo.GetMemory(ctx, tc.userID, tc.workspaceID, "MEMORY.md")
		if err != nil {
			t.Fatalf("GetMemory(%d, %d): %v", tc.userID, tc.workspaceID, err)
		}
		if got.Content != tc.want {
			t.Errorf("GetMemory(%d, %d): got %q, want %q", tc.userID, tc.workspaceID, got.Content, tc.want)
		}
	}
}

func TestGetMemory_MissingIsNotFound(t *testing.T) {
	repo := New(&mockDB{db: memoryDB(t)})

	_, err := repo.GetMemory(context.Background(), memUserID, memWorkspaceID, "never-written.md")

	// A distinguishable error, because a first call on a fresh workspace always
	// misses and the tools answer that differently from a real failure.
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

func TestListMemoriesByWorkspace_OrderedAndScoped(t *testing.T) {
	repo := New(&mockDB{db: memoryDB(t)})
	ctx := context.Background()

	for i, name := range []string{"zeta.md", "MEMORY.md", "alpha.md"} {
		if _, err := repo.SaveMemory(ctx, memory(int64(i+1), memUserID, memWorkspaceID, name, name)); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}
	// Belongs to another workspace, and must not appear.
	if _, err := repo.SaveMemory(ctx, memory(9, memUserID, memOtherWSID, "elsewhere.md", "x")); err != nil {
		t.Fatalf("seed other workspace: %v", err)
	}

	got, err := repo.ListMemoriesByWorkspace(ctx, memUserID, memWorkspaceID)
	if err != nil {
		t.Fatalf("ListMemoriesByWorkspace: %v", err)
	}

	var names []string
	for _, m := range got {
		names = append(names, m.Name)
	}
	if strings.Join(names, ",") != "MEMORY.md,alpha.md,zeta.md" {
		t.Errorf("got %v, want them ordered by name", names)
	}
}

func TestListMemoriesByWorkspace_EmptyWorkspace(t *testing.T) {
	repo := New(&mockDB{db: memoryDB(t)})

	got, err := repo.ListMemoriesByWorkspace(context.Background(), memUserID, memWorkspaceID)

	// A workspace nobody has written to yet is empty, not an error: that is the
	// state every workspace starts in.
	if err != nil {
		t.Fatalf("ListMemoriesByWorkspace: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected no memories, got %d", len(got))
	}
}

// The column is varchar(32); a longer name must not be quietly cut down to fit,
// which would file the memory under a name the agent never chose.
func TestMemoryName_LongerThanTheColumnIsNotSilentlyTruncated(t *testing.T) {
	repo := New(&mockDB{db: memoryDB(t)})
	ctx := context.Background()
	long := strings.Repeat("n", 40) + ".md"

	saved, err := repo.SaveMemory(ctx, memory(1, memUserID, memWorkspaceID, long, "x"))
	if err != nil {
		// Refusing is the other acceptable answer, and is what Postgres does.
		return
	}
	if saved.Name != long {
		t.Errorf("name was truncated to %q; the tools must reject an over-long name before it reaches here", saved.Name)
	}
}

// A write that fails has to surface the failure rather than returning a zero
// memory as though it had been stored: saveMemory reports success to an agent,
// which will then trust that its notes are safe.
func TestSaveMemory_ReportsAWriteThatFailed(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	// Deliberately not migrated: there is no memories table to write to.
	repo := New(&mockDB{db: db})

	if _, err := repo.SaveMemory(context.Background(), memory(1, memUserID, memWorkspaceID, "MEMORY.md", "x")); err == nil {
		t.Error("expected an error when the write cannot land")
	}
}

func TestDeleteMemory_RemovesTheRow(t *testing.T) {
	db := memoryDB(t)
	repo := New(&mockDB{db: db})
	ctx := context.Background()

	if _, err := repo.SaveMemory(ctx, memory(1, memUserID, memWorkspaceID, "MEMORY.md", "x")); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := repo.DeleteMemory(ctx, memUserID, memWorkspaceID, "MEMORY.md"); err != nil {
		t.Fatalf("DeleteMemory: %v", err)
	}

	if _, err := repo.GetMemory(ctx, memUserID, memWorkspaceID, "MEMORY.md"); !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound after delete", err)
	}
}

func TestDeleteMemory_MissingIsNotFound(t *testing.T) {
	repo := New(&mockDB{db: memoryDB(t)})

	err := repo.DeleteMemory(context.Background(), memUserID, memWorkspaceID, "never-written.md")

	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

// The delete is scoped by all three columns, the same as every other memory
// lookup — deleting under one owner/workspace must never remove another's row
// that merely shares a name.
func TestDeleteMemory_ScopedToOwnerAndWorkspace(t *testing.T) {
	repo := New(&mockDB{db: memoryDB(t)})
	ctx := context.Background()

	if _, err := repo.SaveMemory(ctx, memory(1, memUserID, memWorkspaceID, "MEMORY.md", "mine")); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	if _, err := repo.SaveMemory(ctx, memory(2, memUserID, memOtherWSID, "MEMORY.md", "other workspace")); err != nil {
		t.Fatalf("seed other workspace: %v", err)
	}
	if _, err := repo.SaveMemory(ctx, memory(3, memOtherUserID, memWorkspaceID, "MEMORY.md", "other owner")); err != nil {
		t.Fatalf("seed other owner: %v", err)
	}

	if err := repo.DeleteMemory(ctx, memUserID, memWorkspaceID, "MEMORY.md"); err != nil {
		t.Fatalf("DeleteMemory: %v", err)
	}

	for _, tc := range []struct {
		userID, workspaceID int64
		want                string
	}{
		{memUserID, memOtherWSID, "other workspace"},
		{memOtherUserID, memWorkspaceID, "other owner"},
	} {
		got, err := repo.GetMemory(ctx, tc.userID, tc.workspaceID, "MEMORY.md")
		if err != nil {
			t.Fatalf("GetMemory(%d, %d): %v", tc.userID, tc.workspaceID, err)
		}
		if got.Content != tc.want {
			t.Errorf("GetMemory(%d, %d): got %q, want %q", tc.userID, tc.workspaceID, got.Content, tc.want)
		}
	}
}

// The cap holds whatever memory.maxFileSize says, so nothing over it reaches the
// column.
func TestSaveMemory_RefusesContentOverTheCap(t *testing.T) {
	repo := New(&mockDB{db: memoryDB(t)})
	ctx := context.Background()

	if _, err := repo.SaveMemory(ctx, memory(1, memUserID, memWorkspaceID, "big.md", strings.Repeat("a", model.MaxMemoryContentBytes))); err != nil {
		t.Fatalf("at the cap: %v", err)
	}
	_, err := repo.SaveMemory(ctx, memory(2, memUserID, memWorkspaceID, "bigger.md", strings.Repeat("a", model.MaxMemoryContentBytes+1)))
	if !errors.Is(err, ErrMemoryTooLarge) {
		t.Fatalf("over the cap: err = %v, want ErrMemoryTooLarge", err)
	}
	if _, err := repo.GetMemory(ctx, memUserID, memWorkspaceID, "bigger.md"); !errors.Is(err, ErrNotFound) {
		t.Errorf("refused memory was stored: err = %v", err)
	}
}
