package base

import (
	"context"
	"testing"
	"time"

	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

type mockDB struct {
	db *gorm.DB
}

func (m *mockDB) Conn(ctx context.Context) *gorm.DB {
	return m.db
}

func (m *mockDB) Close(ctx context.Context) {}

func TestRepository_GetNextTask(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to connect database: %v", err)
	}

	_ = db.AutoMigrate(&model.Task{})
	repo := New(&mockDB{db: db})

	ctx := context.Background()
	workspaceID := int64(100)
	userID := int64(1)

	// Case 1: No tasks
	_, err = repo.GetNextTask(ctx, workspaceID, userID)
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}

	// Case 2: Tasks exist but none match filters
	db.Create(&model.Task{
		ID:          1,
		WorkspaceID: workspaceID,
		UserID:      userID,
		Status:      "ongoing", // wrong status
		Assignee:    "agent",
	})
	db.Create(&model.Task{
		ID:          2,
		WorkspaceID: workspaceID,
		UserID:      userID,
		Status:      "notstarted",
		Assignee:    "human", // wrong assignee
	})
	db.Create(&model.Task{
		ID:          3,
		WorkspaceID: 200, // wrong workspace
		UserID:      userID,
		Status:      "notstarted",
		Assignee:    "agent",
	})

	_, err = repo.GetNextTask(ctx, workspaceID, userID)
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound for non-matching tasks, got %v", err)
	}

	// Case 3: Proper match and sorting
	now := time.Now()
	db.Create(&model.Task{
		ID:          10,
		WorkspaceID: workspaceID,
		UserID:      userID,
		Status:      "notstarted",
		Assignee:    "agent",
		SortOrder:   0, // fallback to CreatedAt
		CreatedAt:   now.Add(time.Hour),
	})
	db.Create(&model.Task{
		ID:          11,
		WorkspaceID: workspaceID,
		UserID:      userID,
		Status:      "notstarted",
		Assignee:    "agent",
		SortOrder:   5, // explicit sort order (prioritized)
		CreatedAt:   now.Add(2 * time.Hour),
	})
	db.Create(&model.Task{
		ID:          12,
		WorkspaceID: workspaceID,
		UserID:      userID,
		Status:      "notstarted",
		Assignee:    "agent",
		SortOrder:   10,
		CreatedAt:   now.Add(-time.Hour),
	})

	// Expected order:
	// 1. ID 11 (SortOrder 5)
	// 2. ID 12 (SortOrder 10)
	// 3. ID 10 (SortOrder 0 -> CreatedAt)

	task, err := repo.GetNextTask(ctx, workspaceID, userID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if task.ID != 11 {
		t.Errorf("expected task 11, got %d", task.ID)
	}

	// Case 4: Tie-break by ID
	db.Create(&model.Task{
		ID:          13,
		WorkspaceID: workspaceID,
		UserID:      userID,
		Status:      "notstarted",
		Assignee:    "agent",
		SortOrder:   5,
		CreatedAt:   now.Add(3 * time.Hour),
	})
	// Now 11 and 13 have same SortOrder 5. ID 11 should come first.
	task, _ = repo.GetNextTask(ctx, workspaceID, userID)
	if task.ID != 11 {
		t.Errorf("expected task 11 (tie-break by ID), got %d", task.ID)
	}
}
