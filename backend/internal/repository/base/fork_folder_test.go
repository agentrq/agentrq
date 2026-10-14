// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package base

import (
	"context"
	"reflect"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/agentrq/agentrq/backend/internal/data/model"
)

func forkFolderDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Session{}, &model.ForkFolder{}, &model.PanoDocTask{}, &model.Machine{}); err != nil {
		t.Fatal(err)
	}
	return db
}

// The machines holding a fork's folder are the ones recorded, and any with a
// session of it still running: a session reports its folder once it is up.
func TestForkFolderMachines(t *testing.T) {
	db := forkFolderDB(t)
	for _, s := range []model.Session{
		{ID: 2, WorkspaceID: 7, UserID: memUserID, MachineID: 4, Status: "running"},
		{ID: 3, WorkspaceID: 7, UserID: memUserID, MachineID: 5, Status: "starting"},
		{ID: 4, WorkspaceID: 8, UserID: memUserID, MachineID: 9, Status: "running"},
		{ID: 5, WorkspaceID: 7, UserID: memOtherUserID, MachineID: 6, Status: "running"},
	} {
		if err := db.Create(&s).Error; err != nil {
			t.Fatal(err)
		}
	}
	repo := New(&mockDB{db: db})
	ctx := context.Background()
	for _, m := range []int64{3, 4, 3} {
		if err := repo.RecordForkFolder(ctx, 7, m, memUserID); err != nil {
			t.Fatalf("recording machine %d: %v", m, err)
		}
	}
	if err := repo.RecordForkFolder(ctx, 7, 8, memOtherUserID); err != nil {
		t.Fatal(err)
	}

	got, err := repo.ForkFolderMachines(ctx, 7, memUserID)
	if err != nil {
		t.Fatal(err)
	}
	if want := []int64{3, 4, 5}; !reflect.DeepEqual(got, want) {
		t.Errorf("machines = %v, want %v (each once, this workspace and user only)", got, want)
	}
	if got, _ := repo.ForkFolderMachines(ctx, 99, memUserID); len(got) != 0 {
		t.Errorf("a workspace that never ran = %v", got)
	}
}

// A machine that is deleted takes the record of its fork folders with it:
// nothing could reach them to delete them, and a merge would wait on it.
func TestDeleteMachineForgetsItsForkFolders(t *testing.T) {
	db := forkFolderDB(t)
	repo := New(&mockDB{db: db})
	ctx := context.Background()
	if err := db.Create(&model.Machine{ID: 3, UserID: memUserID, Name: "laptop"}).Error; err != nil {
		t.Fatal(err)
	}
	for _, m := range []int64{3, 4} {
		if err := repo.RecordForkFolder(ctx, 7, m, memUserID); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.DeleteMachine(ctx, 3, memUserID); err != nil {
		t.Fatal(err)
	}
	if got, _ := repo.ForkFolderMachines(ctx, 7, memUserID); !reflect.DeepEqual(got, []int64{4}) {
		t.Errorf("machines = %v, want only the one left", got)
	}
}

func TestForkFolderErrors(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	repo := New(&mockDB{db: db})
	ctx := context.Background()
	if _, err := repo.ForkFolderMachines(ctx, 7, memUserID); err == nil {
		t.Error("no fork_folders table, and no error")
	}
	if err := repo.DeleteMachine(ctx, 3, memUserID); err == nil {
		t.Error("no fork_folders table, and no error")
	}
	if err := db.AutoMigrate(&model.ForkFolder{}, &model.PanoDocTask{}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ForkFolderMachines(ctx, 7, memUserID); err == nil {
		t.Error("no sessions table, and no error")
	}
	if err := repo.DeleteMachine(ctx, 3, memUserID); err == nil {
		t.Error("no sessions table, and no error")
	}
}
