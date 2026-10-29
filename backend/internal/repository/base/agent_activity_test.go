// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package base

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
)

func agentActivityDB(t *testing.T, models ...any) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(models...); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func seed(t *testing.T, db *gorm.DB, rows ...any) {
	t.Helper()
	for _, r := range rows {
		if err := db.Create(r).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
}

// A heartbeat writes only the sessions of the machine that sent it.
func TestRecordSessionActivity(t *testing.T) {
	db := agentActivityDB(t, &model.Session{})
	now := time.Now().UTC().Truncate(time.Second)
	seed(t, db,
		&model.Session{ID: 1, MachineID: 3, UserID: memUserID, WorkspaceID: 7, Status: "running", CreatedAt: now},
		&model.Session{ID: 2, MachineID: 4, UserID: memUserID, WorkspaceID: 8, Status: "running", CreatedAt: now},
	)
	repo := New(&mockDB{db: db})

	resets := now.Add(time.Hour)
	err := repo.RecordSessionActivity(context.Background(), 3, []entity.SessionActivity{
		{SessionID: 1, LastOutputAt: now, UsageLimited: true, UsageResetsAt: &resets},
		{SessionID: 2, LastOutputAt: now},
	})
	if err != nil {
		t.Fatal(err)
	}
	var mine, theirs model.Session
	db.First(&mine, 1)
	db.First(&theirs, 2)
	if mine.LastOutputAt == nil || !mine.LastOutputAt.Equal(now) || !mine.UsageLimited || mine.UsageResetsAt == nil || !mine.UsageResetsAt.Equal(resets) {
		t.Errorf("session 1 = %+v", mine)
	}
	if theirs.LastOutputAt != nil {
		t.Errorf("another machine's session was written: %+v", theirs)
	}

	// The limit lifting clears it.
	if err := repo.RecordSessionActivity(context.Background(), 3, []entity.SessionActivity{{SessionID: 1, LastOutputAt: now}}); err != nil {
		t.Fatal(err)
	}
	var cleared model.Session
	db.First(&cleared, 1)
	if cleared.UsageLimited || cleared.UsageResetsAt != nil {
		t.Errorf("session 1 kept its limit: %+v", cleared)
	}
}

func TestRecordSessionActivity_Fails(t *testing.T) {
	repo := New(&mockDB{db: agentActivityDB(t)})
	if err := repo.RecordSessionActivity(context.Background(), 3, []entity.SessionActivity{{SessionID: 1}}); err == nil {
		t.Error("no error with no sessions table")
	}
}

func TestListLiveSessionsByUser(t *testing.T) {
	db := agentActivityDB(t, &model.Session{})
	now := time.Now()
	seed(t, db,
		&model.Session{ID: 1, MachineID: 3, UserID: memUserID, WorkspaceID: 7, Status: "running", CreatedAt: now.Add(-time.Hour)},
		&model.Session{ID: 2, MachineID: 3, UserID: memUserID, WorkspaceID: 8, Status: "starting", CreatedAt: now},
		&model.Session{ID: 3, MachineID: 3, UserID: memUserID, WorkspaceID: 9, Status: "exited", CreatedAt: now},
		&model.Session{ID: 4, MachineID: 5, UserID: memOtherUserID, WorkspaceID: 10, Status: "running", CreatedAt: now},
	)
	got, err := New(&mockDB{db: db}).ListLiveSessionsByUser(context.Background(), memUserID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != 2 || got[1].ID != 1 {
		t.Errorf("sessions = %+v, want 2 then 1", got)
	}
}

func TestLatestOngoingAgentTasks(t *testing.T) {
	db := agentActivityDB(t, &model.Task{})
	now := time.Now()
	seed(t, db,
		&model.Task{ID: 1, WorkspaceID: 7, UserID: memUserID, Status: "ongoing", Assignee: "agent", UpdatedAt: now.Add(-time.Hour)},
		&model.Task{ID: 2, WorkspaceID: 7, UserID: memUserID, Status: "ongoing", Assignee: "agent", UpdatedAt: now},
		&model.Task{ID: 3, WorkspaceID: 8, UserID: memUserID, Status: "ongoing", Assignee: "human", UpdatedAt: now},
		&model.Task{ID: 4, WorkspaceID: 8, UserID: memUserID, Status: "completed", Assignee: "agent", UpdatedAt: now},
		&model.Task{ID: 5, WorkspaceID: 9, UserID: memOtherUserID, Status: "ongoing", Assignee: "agent", UpdatedAt: now},
	)
	got, err := New(&mockDB{db: db}).LatestOngoingAgentTasks(context.Background(), memUserID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[7] != 2 {
		t.Errorf("tasks = %v, want workspace 7's latest, task 2", got)
	}
}

func TestLatestOngoingAgentTasks_Fails(t *testing.T) {
	if _, err := New(&mockDB{db: agentActivityDB(t)}).LatestOngoingAgentTasks(context.Background(), memUserID); err == nil {
		t.Error("no error with no tasks table")
	}
}
