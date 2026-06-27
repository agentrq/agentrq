package backfill

import (
	"context"
	"errors"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// testDB implements dbconn.DBConn over an in-memory SQLite database.
type testDB struct{ db *gorm.DB }

func (m *testDB) Conn(ctx context.Context) *gorm.DB { return m.db }
func (m *testDB) Close(ctx context.Context)         {}

func newTestRepo(t *testing.T) (base.Repository, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&model.Backfill{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return base.New(&testDB{db: db}), db
}

func TestApply_RunsAndRecords(t *testing.T) {
	repo, db := newTestRepo(t)
	s := New(repo).(*service)

	ran := 0
	s.apply(context.Background(), []Backfill{
		{Name: "260101_ok", Run: func(context.Context) error { ran++; return nil }},
	})

	if ran != 1 {
		t.Errorf("expected backfill to run once, ran=%d", ran)
	}
	var count int64
	db.Model(&model.Backfill{}).Where("name = ?", "260101_ok").Count(&count)
	if count != 1 {
		t.Errorf("expected backfill recorded once, got %d", count)
	}
}

func TestApply_FailureNotRecorded(t *testing.T) {
	repo, db := newTestRepo(t)
	s := New(repo).(*service)

	s.apply(context.Background(), []Backfill{
		{Name: "260101_boom", Run: func(context.Context) error { return errors.New("boom") }},
	})

	var count int64
	db.Model(&model.Backfill{}).Where("name = ?", "260101_boom").Count(&count)
	if count != 0 {
		t.Errorf("failed backfill must not be recorded, got %d", count)
	}
}

// TestRun_SkipsApplied verifies Run's synchronous skip path: an already-recorded
// backfill is not executed again.
func TestRun_SkipsApplied(t *testing.T) {
	repo, _ := newTestRepo(t)
	s := New(repo).(*service)
	ctx := context.Background()

	if err := repo.SystemRecordBackfill(ctx, "260101_x"); err != nil {
		t.Fatalf("record backfill: %v", err)
	}

	ran := false
	err := s.Run(ctx, []Backfill{
		{Name: "260101_x", Run: func(context.Context) error { ran = true; return nil }},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if ran {
		t.Errorf("Run should not execute an already-applied backfill")
	}
}
