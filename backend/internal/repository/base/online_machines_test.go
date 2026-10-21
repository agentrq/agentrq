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

// Online is enabled and heard from since the cut-off; each user's machines are
// counted together, and a user with none online is not listed.
func TestCountOnlineMachinesPerUser(t *testing.T) {
	db := forkFolderDB(t)
	repo := New(&mockDB{db: db})
	now := time.Now()
	recent, old := now.Add(-time.Minute), now.Add(-time.Hour)
	for _, m := range []model.Machine{
		{ID: 1, UserID: memUserID, TokenHash: "a", LastSeenAt: &recent},
		{ID: 2, UserID: memUserID, TokenHash: "b", LastSeenAt: &recent},
		{ID: 3, UserID: memUserID, TokenHash: "c", LastSeenAt: &old},
		{ID: 4, UserID: memOtherUserID, TokenHash: "d", LastSeenAt: &recent},
		{ID: 5, UserID: memOtherUserID, TokenHash: "e"},
		{ID: 6, UserID: 77, TokenHash: "f", LastSeenAt: &old},
	} {
		if err := db.Create(&m).Error; err != nil {
			t.Fatal(err)
		}
	}
	// Disabled after the insert: GORM leaves a false out of the INSERT, and
	// the column's default would turn it back into true.
	if err := db.Model(&model.Machine{}).Where("id = ?", 4).Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}

	got, err := repo.CountOnlineMachinesPerUser(context.Background(), now.Add(-90*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if want := map[int64]int{memUserID: 2}; !reflect.DeepEqual(got, want) {
		t.Errorf("online machines = %v, want %v", got, want)
	}
}

func TestCountOnlineMachinesPerUser_PassesOnAFailure(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	// No machines table: the query fails, and the failure is the answer.
	if got, err := New(&mockDB{db: db}).CountOnlineMachinesPerUser(context.Background(), time.Now()); err == nil || got != nil {
		t.Errorf("got %v, %v; want nil and the error", got, err)
	}
}
