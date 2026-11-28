// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

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

func sessionStatuses(t *testing.T, db *gorm.DB) map[int64]string {
	t.Helper()
	var rows []model.Session
	if err := db.Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	out := map[int64]string{}
	for _, r := range rows {
		out[r.ID] = r.Status
	}
	return out
}

// A machine away too long, or deleted, has its live sessions marked away; a
// machine still heard from, and a finished session, keep theirs as they are.
func TestMarkStaleSessionsAway(t *testing.T) {
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

	n, err := repo.MarkStaleSessionsAway(ctx, now.Add(-30*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if n != 4 {
		t.Errorf("marked %d sessions, want 4", n)
	}
	want := map[int64]string{10: "running", 11: "away", 12: "away", 13: "exited", 14: "away", 15: "away"}
	if got := sessionStatuses(t, db); !reflect.DeepEqual(got, want) {
		t.Errorf("sessions = %v, want %v", got, want)
	}
}

// An away session goes once it is older than the cut-off, or its machine is
// gone; a newer one, and any other state, stays.
func TestDeleteExpiredAwaySessions(t *testing.T) {
	db := forkFolderDB(t)
	repo := New(&mockDB{db: db})
	now := time.Now()
	if err := db.Create(&model.Machine{ID: 1, UserID: memUserID, TokenHash: "a"}).Error; err != nil {
		t.Fatal(err)
	}
	for _, s := range []model.Session{
		{ID: 10, MachineID: 1, UserID: memUserID, Status: "away", UpdatedAt: now.Add(-8 * 24 * time.Hour)},
		{ID: 11, MachineID: 1, UserID: memUserID, Status: "away", UpdatedAt: now.Add(-time.Hour)},
		{ID: 12, MachineID: 4, UserID: memUserID, Status: "away", UpdatedAt: now.Add(-time.Hour)}, // its machine was deleted
		{ID: 13, MachineID: 1, UserID: memUserID, Status: "running", UpdatedAt: now.Add(-8 * 24 * time.Hour)},
	} {
		if err := db.Create(&s).Error; err != nil {
			t.Fatal(err)
		}
	}
	n, err := repo.DeleteExpiredAwaySessions(context.Background(), now.Add(-7*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("deleted %d sessions, want 2", n)
	}
	if got, want := sessionIDs(t, db), []int64{11, 13}; !reflect.DeepEqual(got, want) {
		t.Errorf("sessions left = %v, want %v", got, want)
	}
}

// Every ended session goes, whatever its machine; a live one stays.
func TestDeleteFinishedSessions(t *testing.T) {
	db := forkFolderDB(t)
	repo := New(&mockDB{db: db})
	for _, s := range []model.Session{
		{ID: 10, MachineID: 1, UserID: memUserID, Status: "running"},
		{ID: 11, MachineID: 1, UserID: memUserID, Status: "starting"},
		{ID: 12, MachineID: 1, UserID: memUserID, Status: "exited"},
		{ID: 13, MachineID: 2, UserID: memUserID, Status: "killed"},
		{ID: 14, MachineID: 3, UserID: memOtherUserID, Status: "failed"},
	} {
		if err := db.Create(&s).Error; err != nil {
			t.Fatal(err)
		}
	}

	n, err := repo.DeleteFinishedSessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Errorf("deleted %d sessions, want 3", n)
	}
	if got, want := sessionIDs(t, db), []int64{10, 11}; !reflect.DeepEqual(got, want) {
		t.Errorf("sessions left = %v, want %v", got, want)
	}
}

func TestDeleteFinishedSessionsDatabaseUnavailable(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := New(&mockDB{db: db}).DeleteFinishedSessions(context.Background()); err == nil {
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
