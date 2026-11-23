// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package base

import (
	"context"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func newTelemetryTestRepo(t *testing.T) (Repository, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to connect database: %v", err)
	}
	if err := db.AutoMigrate(
		&model.Telemetry{},
		&model.HourlyTelemetry{},
		&model.DailyTelemetry{},
		&model.MonthlyTelemetry{},
		&model.TelemetryAggregation{},
	); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	return New(&mockDB{db: db}), db
}

// The claim row is the only thing standing between several backend instances
// and aggregating the same period twice, so the second claim for an
// already-claimed (type, period) must fail even though nothing about the
// call looks different from the first.
func TestRepository_ClaimTelemetryAggregation(t *testing.T) {
	repo, _ := newTelemetryTestRepo(t)
	ctx := context.Background()

	claimed, err := repo.ClaimTelemetryAggregation(ctx, "hourly", "2026-09-20T14")
	if err != nil {
		t.Fatalf("ClaimTelemetryAggregation: %v", err)
	}
	if !claimed {
		t.Fatal("expected the first claim to succeed")
	}

	claimed, err = repo.ClaimTelemetryAggregation(ctx, "hourly", "2026-09-20T14")
	if err != nil {
		t.Fatalf("ClaimTelemetryAggregation (repeat): %v", err)
	}
	if claimed {
		t.Fatal("expected a repeat claim for the same period to fail")
	}

	// A different aggregation type may reuse the same period key: monthly's
	// claim key is the day it ran, which collides in spelling (but not in
	// type) with a daily claim key for the same day.
	claimed, err = repo.ClaimTelemetryAggregation(ctx, "monthly", "2026-09-20T14")
	if err != nil {
		t.Fatalf("ClaimTelemetryAggregation (different type): %v", err)
	}
	if !claimed {
		t.Fatal("expected a claim with a different aggregation type to succeed")
	}
}

func TestRepository_AggregateHourlyTelemetry(t *testing.T) {
	repo, db := newTelemetryTestRepo(t)
	ctx := context.Background()

	const hourStart, hourEnd = int64(1000), int64(1000 + 3600)
	seed := []model.Telemetry{
		{UserID: 1, WorkspaceID: 10, OccurredAt: hourStart, Action: 5, Actor: 1},
		{UserID: 1, WorkspaceID: 10, OccurredAt: hourStart + 100, Action: 5, Actor: 1},
		{UserID: 1, WorkspaceID: 10, OccurredAt: hourStart + 200, Action: 6, Actor: 1},
		// Outside the target hour: must not be counted.
		{UserID: 1, WorkspaceID: 10, OccurredAt: hourEnd, Action: 5, Actor: 1},
		{UserID: 1, WorkspaceID: 10, OccurredAt: hourStart - 1, Action: 5, Actor: 1},
	}
	if err := db.Create(&seed).Error; err != nil {
		t.Fatalf("seed telemetries: %v", err)
	}

	if err := repo.AggregateHourlyTelemetry(ctx, hourStart, hourEnd); err != nil {
		t.Fatalf("AggregateHourlyTelemetry: %v", err)
	}

	var rows []model.HourlyTelemetry
	if err := db.Order("action").Find(&rows).Error; err != nil {
		t.Fatalf("read hourly_telemetries: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows (one per action), got %d: %+v", len(rows), rows)
	}
	if rows[0].Action != 5 || rows[0].Count != 2 || rows[0].PeriodStart != hourStart {
		t.Errorf("unexpected action-5 row: %+v", rows[0])
	}
	if rows[1].Action != 6 || rows[1].Count != 1 {
		t.Errorf("unexpected action-6 row: %+v", rows[1])
	}

	// Re-running the same period must not double the counts: the unique
	// index on the dimensions makes the second insert a no-op.
	if err := repo.AggregateHourlyTelemetry(ctx, hourStart, hourEnd); err != nil {
		t.Fatalf("AggregateHourlyTelemetry (repeat): %v", err)
	}
	var count int64
	db.Model(&model.HourlyTelemetry{}).Count(&count)
	if count != 2 {
		t.Errorf("expected re-running the aggregation to stay at 2 rows, got %d", count)
	}
}

func TestRepository_AggregateDailyTelemetry(t *testing.T) {
	repo, db := newTelemetryTestRepo(t)
	ctx := context.Background()

	const dayStart, dayEnd = int64(86400), int64(86400 * 2)
	seed := []model.HourlyTelemetry{
		{PeriodStart: dayStart, UserID: 1, WorkspaceID: 10, Action: 5, Actor: 1, Count: 3},
		{PeriodStart: dayStart + 3600, UserID: 1, WorkspaceID: 10, Action: 5, Actor: 1, Count: 4},
		// Outside the target day: must not be counted.
		{PeriodStart: dayEnd, UserID: 1, WorkspaceID: 10, Action: 5, Actor: 1, Count: 100},
	}
	if err := db.Create(&seed).Error; err != nil {
		t.Fatalf("seed hourly_telemetries: %v", err)
	}

	if err := repo.AggregateDailyTelemetry(ctx, dayStart, dayEnd); err != nil {
		t.Fatalf("AggregateDailyTelemetry: %v", err)
	}

	var row model.DailyTelemetry
	if err := db.First(&row).Error; err != nil {
		t.Fatalf("read daily_telemetries: %v", err)
	}
	if row.Count != 7 || row.PeriodStart != dayStart {
		t.Errorf("unexpected daily row: %+v", row)
	}
}

// Monthly aggregation re-runs every day for the still-open month, so it must
// upsert: a second run covering more days replaces the count for the same
// dimension set rather than adding a second row.
func TestRepository_AggregateMonthlyTelemetry_Upserts(t *testing.T) {
	repo, db := newTelemetryTestRepo(t)
	ctx := context.Background()

	const monthStart = int64(0)
	seed := []model.DailyTelemetry{
		{PeriodStart: monthStart, UserID: 1, WorkspaceID: 10, Action: 5, Actor: 1, Count: 2},
	}
	if err := db.Create(&seed).Error; err != nil {
		t.Fatalf("seed daily_telemetries: %v", err)
	}

	if err := repo.AggregateMonthlyTelemetry(ctx, monthStart, monthStart+86400); err != nil {
		t.Fatalf("AggregateMonthlyTelemetry: %v", err)
	}
	var row model.MonthlyTelemetry
	if err := db.First(&row).Error; err != nil {
		t.Fatalf("read monthly_telemetries: %v", err)
	}
	if row.Count != 2 {
		t.Fatalf("expected count 2 after first run, got %d", row.Count)
	}

	// A second day's worth of data arrives; the next day's run covers a
	// wider window and must replace, not add to, the existing row.
	if err := db.Create(&model.DailyTelemetry{
		PeriodStart: monthStart + 86400, UserID: 1, WorkspaceID: 10, Action: 5, Actor: 1, Count: 5,
	}).Error; err != nil {
		t.Fatalf("seed second day: %v", err)
	}
	if err := repo.AggregateMonthlyTelemetry(ctx, monthStart, monthStart+2*86400); err != nil {
		t.Fatalf("AggregateMonthlyTelemetry (second run): %v", err)
	}

	var rows []model.MonthlyTelemetry
	if err := db.Find(&rows).Error; err != nil {
		t.Fatalf("read monthly_telemetries: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected the upsert to keep a single row, got %d: %+v", len(rows), rows)
	}
	if rows[0].Count != 7 {
		t.Errorf("expected the upserted count to be 7 (2+5), got %d", rows[0].Count)
	}
}

// A failure reading the source table must surface rather than being reported
// as a period with no activity — same reasoning as
// TestRepository_GetDetailedUserStats_AggregationError.
func TestRepository_AggregateDailyTelemetry_SourceTableMissing(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to connect database: %v", err)
	}
	// Deliberately not migrated: there is no hourly_telemetries table.
	repo := New(&mockDB{db: db})

	if err := repo.AggregateDailyTelemetry(context.Background(), 0, 86400); err == nil {
		t.Error("expected an error when hourly_telemetries is missing, got nil")
	}
}

func TestRepository_AggregateMonthlyTelemetry_SourceTableMissing(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to connect database: %v", err)
	}
	// Deliberately not migrated: there is no daily_telemetries table.
	repo := New(&mockDB{db: db})

	if err := repo.AggregateMonthlyTelemetry(context.Background(), 0, 86400); err == nil {
		t.Error("expected an error when daily_telemetries is missing, got nil")
	}
}

func TestRepository_AggregateHourlyTelemetry_NoRows(t *testing.T) {
	repo, db := newTelemetryTestRepo(t)
	ctx := context.Background()

	if err := repo.AggregateHourlyTelemetry(ctx, 0, 3600); err != nil {
		t.Fatalf("expected no error aggregating an empty period, got %v", err)
	}
	var count int64
	db.Model(&model.HourlyTelemetry{}).Count(&count)
	if count != 0 {
		t.Errorf("expected no rows written for an empty period, got %d", count)
	}
}
