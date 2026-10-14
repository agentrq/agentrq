// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package base

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/agentrq/agentrq/backend/internal/data/model"
)

func sessionIDs(t *testing.T, db *gorm.DB) []int64 {
	t.Helper()
	var ids []int64
	if err := db.Model(&model.Session{}).Order("id").Pluck("id", &ids).Error; err != nil {
		t.Fatal(err)
	}
	return ids
}

// A machine away too long, or deleted, loses its live sessions; a machine
// still heard from, and a finished session, keep theirs.
func TestDeleteStaleSessions(t *testing.T) {
	db := forkFolderDB(t)
	repo := New(&mockDB{db: db})
	ctx := context.Background()
	now := time.Now()
	recent, old := now.Add(-time.Minute), now.Add(-time.Hour)
	for _, m := range []model.Machine{
		{ID: 1, UserID: memUserID, TokenHash: "a", LastSeenAt: &recent},
		{ID: 2, UserID: memUserID, TokenHash: "b", LastSeenAt: &old},
		{ID: 3, UserID: memOtherUserID, TokenHash: "c"},
	} {
		if err := db.Create(&m).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, s := range []model.Session{
		{ID: 10, MachineID: 1, UserID: memUserID, Status: "running"},
		{ID: 11, MachineID: 2, UserID: memUserID, Status: "running"},
		{ID: 12, MachineID: 2, UserID: memUserID, Status: "starting"},
		{ID: 13, MachineID: 2, UserID: memUserID, Status: "exited"},
		{ID: 14, MachineID: 3, UserID: memOtherUserID, Status: "running"},
		{ID: 15, MachineID: 4, UserID: memUserID, Status: "running"}, // its machine was deleted
	} {
		if err := db.Create(&s).Error; err != nil {
			t.Fatal(err)
		}
	}

	n, err := repo.DeleteStaleSessions(ctx, now.Add(-30*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if n != 4 {
		t.Errorf("deleted %d sessions, want 4", n)
	}
	if got, want := sessionIDs(t, db), []int64{10, 13}; !reflect.DeepEqual(got, want) {
		t.Errorf("sessions left = %v, want %v", got, want)
	}
}

func TestDeleteStaleSessionsDatabaseUnavailable(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := New(&mockDB{db: db}).DeleteStaleSessions(context.Background(), time.Now()); err == nil {
		t.Error("no sessions table, and no error")
	}
}

// Deleting a machine deletes its sessions, so none is left to block a merge,
// and leaves another machine's alone.
func TestDeleteMachineDeletesItsSessions(t *testing.T) {
	db := forkFolderDB(t)
	repo := New(&mockDB{db: db})
	for _, s := range []model.Session{
		{ID: 10, MachineID: 3, UserID: memUserID, Status: "running"},
		{ID: 11, MachineID: 4, UserID: memUserID, Status: "running"},
	} {
		if err := db.Create(&s).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.DeleteMachine(context.Background(), 3, memUserID); err != nil {
		t.Fatal(err)
	}
	if got, want := sessionIDs(t, db), []int64{11}; !reflect.DeepEqual(got, want) {
		t.Errorf("sessions left = %v, want %v", got, want)
	}
}
