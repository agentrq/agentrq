// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package base

import (
	"context"
	"errors"
	"testing"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/service/tasklatency"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func latencyDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&model.Task{}, &model.Message{}, &model.TaskStateTransition{}, &model.TaskLatency{},
		&model.HourlyTaskLatency{}, &model.DailyTaskLatency{}, &model.MonthlyTaskLatency{}, &model.TelemetryAggregation{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func latencyOf(t *testing.T, db *gorm.DB, taskID int64) (model.TaskLatency, bool) {
	t.Helper()
	var rows []model.TaskLatency
	if err := db.Where("task_id = ?", taskID).Find(&rows).Error; err != nil {
		t.Fatalf("read latency: %v", err)
	}
	if len(rows) > 1 {
		t.Fatalf("%d latency rows for one task", len(rows))
	}
	if len(rows) == 0 {
		return model.TaskLatency{}, false
	}
	return rows[0], true
}

// Closing a task records its timing from the whole history, with the same
// arithmetic the task page shows; a reclose replaces the row.
func TestTaskLatency_RecordedOnClose(t *testing.T) {
	db := latencyDB(t)
	repo := New(&mockDB{db: db})
	ctx := context.Background()
	now := time.Now()

	task, err := repo.CreateTask(ctx, model.Task{ID: dtTaskID, CreatedAt: now, UpdatedAt: now, WorkspaceID: dtWorkspaceID, UserID: dtUserID, Status: "ongoing"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// Backdate the start an hour, then block for none of it.
	db.Model(&model.TaskStateTransition{}).Where("task_id = ?", dtTaskID).Update("created_at", now.Add(-time.Hour))
	if _, ok := latencyOf(t, db, dtTaskID); ok {
		t.Fatal("an open task has no latency")
	}

	task.Status = "completed"
	if task, err = repo.UpdateTask(ctx, task, "Status"); err != nil {
		t.Fatalf("complete: %v", err)
	}
	l, ok := latencyOf(t, db, dtTaskID)
	if !ok || l.UserID != dtUserID || l.WorkspaceID != dtWorkspaceID {
		t.Fatalf("latency = %+v, %v", l, ok)
	}
	if l.WorkedSeconds < 3599 || l.WorkedSeconds > 3601 || l.StartToCloseSeconds == nil || *l.StartToCloseSeconds != l.WorkedSeconds {
		t.Fatalf("worked, start to close = %d, %v", l.WorkedSeconds, l.StartToCloseSeconds)
	}
	if l.BlockedSeconds != 0 || l.NeedsInputSeconds != 0 {
		t.Fatalf("blocked, needs input = %d, %d", l.BlockedSeconds, l.NeedsInputSeconds)
	}
	firstClose := l.ClosedAt

	// Reopen: the row stays until the task closes again, then is replaced.
	task.Status = "ongoing"
	if task, err = repo.UpdateTask(ctx, task, "Status"); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	db.Model(&model.TaskStateTransition{}).Where("task_id = ? AND to_state = ?", dtTaskID, model.TaskStateOngoing).
		Where("from_state = ?", model.TaskStateCompleted).Update("created_at", now.Add(time.Hour))
	task.Status = "rejected"
	if _, err = repo.UpdateTask(ctx, task, "Status"); err != nil {
		t.Fatalf("reject: %v", err)
	}
	l, _ = latencyOf(t, db, dtTaskID)
	if l.ClosedAt < firstClose {
		t.Fatalf("closed at %d, before the first close %d", l.ClosedAt, firstClose)
	}
}

// A task closed without ever going ongoing has no start-to-close, but its
// other metrics are recorded, zero included.
func TestTaskLatency_ClosedWithoutStarting(t *testing.T) {
	db := latencyDB(t)
	repo := New(&mockDB{db: db})
	if _, err := repo.CreateTask(context.Background(), model.Task{ID: dtTaskID, WorkspaceID: dtWorkspaceID, UserID: dtUserID, Status: "completed"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	l, ok := latencyOf(t, db, dtTaskID)
	if !ok || l.StartToCloseSeconds != nil || l.WorkedSeconds != 0 {
		t.Fatalf("latency = %+v, %v", l, ok)
	}
}

func TestTaskLatency_MovesWithTheTask(t *testing.T) {
	db := latencyDB(t)
	repo := New(&mockDB{db: db})
	ctx := context.Background()
	task, err := repo.CreateTask(ctx, model.Task{ID: dtTaskID, WorkspaceID: dtWorkspaceID, UserID: dtUserID, Status: "completed"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	task.WorkspaceID = dtWorkspaceID + 1
	if _, err := repo.UpdateTask(ctx, task, "WorkspaceID"); err != nil {
		t.Fatalf("move: %v", err)
	}
	if l, _ := latencyOf(t, db, dtTaskID); l.WorkspaceID != dtWorkspaceID+1 {
		t.Fatalf("latency workspace = %d", l.WorkspaceID)
	}
}

// Created already closed, the first query of the history is the latency's
// own read of it.
func TestTaskLatency_HistoryReadFailure(t *testing.T) {
	db := latencyDB(t)
	repo := New(&mockDB{db: db})
	failOn(t, db, "query", "task_state_transitions")
	if _, err := repo.CreateTask(context.Background(), model.Task{ID: dtTaskID, WorkspaceID: dtWorkspaceID, UserID: dtUserID, Status: "completed"}); !errors.Is(err, errInjected) {
		t.Fatalf("got %v, want the injected failure", err)
	}
}

func TestTaskLatency_WriteFailures(t *testing.T) {
	for _, tc := range []struct {
		name, kind, table string
		move              bool
	}{
		{"latency write", "create", "task_latencies", false},
		{"latency move", "update", "task_latencies", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := latencyDB(t)
			repo := New(&mockDB{db: db})
			ctx := context.Background()
			task, err := repo.CreateTask(ctx, model.Task{ID: dtTaskID, WorkspaceID: dtWorkspaceID, UserID: dtUserID, Status: "ongoing"})
			if err != nil {
				t.Fatalf("create: %v", err)
			}
			failOn(t, db, tc.kind, tc.table)
			if tc.move {
				task.WorkspaceID++
			} else {
				task.Status = "completed"
			}
			if _, err := repo.UpdateTask(ctx, task, "Status", "WorkspaceID"); !errors.Is(err, errInjected) {
				t.Fatalf("got %v, want the injected failure", err)
			}
		})
	}
}

func hourAt(h int) int64 { return time.Date(2026, 9, 27, h, 0, 0, 0, time.UTC).Unix() }

func seedLatencies(t *testing.T, db *gorm.DB, rows ...model.TaskLatency) {
	t.Helper()
	if err := db.Create(&rows).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
}

func rollupsOf(t *testing.T, db *gorm.DB, table string) map[latencyKey]entity.TaskLatencyRollup {
	t.Helper()
	var rows []entity.TaskLatencyRollup
	if err := db.Table(table).Scan(&rows).Error; err != nil {
		t.Fatalf("read %s: %v", table, err)
	}
	out := map[latencyKey]entity.TaskLatencyRollup{}
	for _, r := range rows {
		out[latencyKey{r.UserID, r.WorkspaceID, model.LatencyMetric(r.Metric)}] = r
	}
	return out
}

func TestTaskLatency_Rollups(t *testing.T) {
	db := latencyDB(t)
	repo := New(&mockDB{db: db})
	ctx := context.Background()
	s2c := int64(1200)
	seedLatencies(t, db,
		model.TaskLatency{TaskID: 1, UserID: 1, WorkspaceID: 10, ClosedAt: hourAt(9) + 5, StartToCloseSeconds: &s2c, WorkedSeconds: 600, BlockedSeconds: 60},
		model.TaskLatency{TaskID: 2, UserID: 1, WorkspaceID: 10, ClosedAt: hourAt(9) + 50, WorkedSeconds: 100},
		model.TaskLatency{TaskID: 3, UserID: 1, WorkspaceID: 10, ClosedAt: hourAt(10) + 1, StartToCloseSeconds: &s2c, WorkedSeconds: 3000},
		// Outside every hour rolled up below.
		model.TaskLatency{TaskID: 4, UserID: 1, WorkspaceID: 10, ClosedAt: hourAt(12), WorkedSeconds: 1},
	)

	for _, h := range []int{9, 10, 11} {
		if err := repo.AggregateHourlyTaskLatency(ctx, hourAt(h), hourAt(h+1)); err != nil {
			t.Fatalf("hourly %d: %v", h, err)
		}
	}
	// A re-run of an hour is a no-op, not a doubling.
	if err := repo.AggregateHourlyTaskLatency(ctx, hourAt(9), hourAt(10)); err != nil {
		t.Fatalf("hourly re-run: %v", err)
	}
	var hourly []model.HourlyTaskLatency
	db.Where("period_start = ?", hourAt(9)).Find(&hourly)
	byMetric := map[model.LatencyMetric]model.HourlyTaskLatency{}
	for _, r := range hourly {
		byMetric[r.Metric] = r
	}
	if w := byMetric[model.LatencyMetricWorked]; w.Count != 2 || w.Sum != 700 || w.Min != 100 || w.Max != 600 {
		t.Fatalf("hour 9 worked = %+v", w)
	}
	// Only the started task has a start-to-close.
	if s := byMetric[model.LatencyMetricStartToClose]; s.Count != 1 {
		t.Fatalf("hour 9 start to close = %+v", s)
	}
	if b := byMetric[model.LatencyMetricBlocked]; b.Count != 2 || b.Min != 0 || b.Max != 60 {
		t.Fatalf("hour 9 blocked = %+v", b)
	}

	day := time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC).Unix()
	if err := repo.AggregateDailyTaskLatency(ctx, day, day+86400); err != nil {
		t.Fatalf("daily: %v", err)
	}
	daily := rollupsOf(t, db, dailyTaskLatencies)
	w := daily[latencyKey{1, 10, model.LatencyMetricWorked}]
	if w.PeriodStart != day || w.Count != 3 || w.Sum != 3700 || w.Min != 100 || w.Max != 3000 {
		t.Fatalf("daily worked = %+v", w)
	}
	if got := tasklatency.Decode(w.Count, w.Sum, w.Min, w.Max, w.Histogram); got.Hist[1] != 1 || got.Hist[4] != 1 || got.Hist[6] != 1 {
		t.Fatalf("daily histogram = %v", got.Hist)
	}

	month := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC).Unix()
	if err := repo.AggregateMonthlyTaskLatency(ctx, month, day+86400); err != nil {
		t.Fatalf("monthly: %v", err)
	}
	// The next day's run replaces the month's rows rather than adding a set.
	db.Table(dailyTaskLatencies).Where("metric = ?", model.LatencyMetricWorked).Update("count", 4)
	if err := repo.AggregateMonthlyTaskLatency(ctx, month, day+86400); err != nil {
		t.Fatalf("monthly re-run: %v", err)
	}
	monthly := rollupsOf(t, db, monthlyTaskLatencies)
	if m := monthly[latencyKey{1, 10, model.LatencyMetricWorked}]; m.PeriodStart != month || m.Count != 4 {
		t.Fatalf("monthly worked = %+v", m)
	}
	var n int64
	db.Table(monthlyTaskLatencies).Count(&n)
	if n != 4 {
		t.Fatalf("%d monthly rows, want one per metric", n)
	}

	// Empty periods write nothing.
	if err := repo.AggregateHourlyTaskLatency(ctx, hourAt(20), hourAt(21)); err != nil {
		t.Fatalf("empty hourly: %v", err)
	}
	if err := repo.AggregateDailyTaskLatency(ctx, day+86400, day+2*86400); err != nil {
		t.Fatalf("empty daily: %v", err)
	}
}

func TestTaskLatency_Reads(t *testing.T) {
	db := latencyDB(t)
	repo := New(&mockDB{db: db})
	ctx := context.Background()
	seedLatencies(t, db,
		model.TaskLatency{TaskID: 1, UserID: 1, WorkspaceID: 10, ClosedAt: hourAt(9)},
		model.TaskLatency{TaskID: 2, UserID: 1, WorkspaceID: 11, ClosedAt: hourAt(9)},
		model.TaskLatency{TaskID: 3, UserID: 2, WorkspaceID: 12, ClosedAt: hourAt(9)},
		model.TaskLatency{TaskID: 4, UserID: 1, WorkspaceID: 10, ClosedAt: hourAt(11)},
	)
	if rows, _ := repo.ListTaskLatencies(ctx, 10, 1, hourAt(9), hourAt(10)); len(rows) != 1 || rows[0].TaskID != 1 {
		t.Fatalf("workspace scope = %+v", rows)
	}
	if rows, _ := repo.ListTaskLatencies(ctx, 0, 1, hourAt(0), hourAt(23)); len(rows) != 3 {
		t.Fatalf("account scope = %+v", rows)
	}

	if err := repo.AggregateHourlyTaskLatency(ctx, hourAt(9), hourAt(10)); err != nil {
		t.Fatalf("hourly: %v", err)
	}
	rows, err := repo.ListTaskLatencyRollups(ctx, tasklatency.Hour, 0, 1, hourAt(9), hourAt(10))
	if err != nil || len(rows) != 6 { // two workspaces, three metrics each (never started)
		t.Fatalf("rollups = %+v, %v", rows, err)
	}
	if rows, _ := repo.ListTaskLatencyRollups(ctx, tasklatency.Hour, 12, 2, hourAt(10), hourAt(11)); len(rows) != 0 {
		t.Fatalf("rollups outside the period = %+v", rows)
	}

	if _, ok, err := repo.LatestTelemetryAggregation(ctx, tasklatency.ClaimHourly); ok || err != nil {
		t.Fatalf("no claim yet: %v, %v", ok, err)
	}
	for _, k := range []string{"2026-09-27T08", "2026-09-27T10", "2026-09-27T09"} {
		if _, err := repo.ClaimTelemetryAggregation(ctx, tasklatency.ClaimHourly, k); err != nil {
			t.Fatalf("claim: %v", err)
		}
	}
	if _, err := repo.ClaimTelemetryAggregation(ctx, tasklatency.ClaimDaily, "2026-09-30"); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if key, ok, err := repo.LatestTelemetryAggregation(ctx, tasklatency.ClaimHourly); !ok || err != nil || key != "2026-09-27T10" {
		t.Fatalf("latest = %q, %v, %v", key, ok, err)
	}
}

func TestTaskLatency_ReadFailures(t *testing.T) {
	db := latencyDB(t)
	repo := New(&mockDB{db: db})
	ctx := context.Background()
	failOn(t, db, "query", "task_latencies")
	failOn(t, db, "row", hourlyTaskLatencies)
	failOn(t, db, "row", dailyTaskLatencies)
	failOn(t, db, "query", "telemetry_aggregations")

	for name, err := range map[string]error{
		"hourly":  repo.AggregateHourlyTaskLatency(ctx, 0, 1),
		"daily":   repo.AggregateDailyTaskLatency(ctx, 0, 1),
		"monthly": repo.AggregateMonthlyTaskLatency(ctx, 0, 1),
	} {
		if !errors.Is(err, errInjected) {
			t.Errorf("%s: got %v, want the injected failure", name, err)
		}
	}
	if _, err := repo.ListTaskLatencies(ctx, 1, 1, 0, 1); !errors.Is(err, errInjected) {
		t.Errorf("list: got %v", err)
	}
	if _, err := repo.ListTaskLatencyRollups(ctx, tasklatency.Hour, 1, 1, 0, 1); !errors.Is(err, errInjected) {
		t.Errorf("rollups: got %v", err)
	}
	if _, _, err := repo.LatestTelemetryAggregation(ctx, tasklatency.ClaimHourly); !errors.Is(err, errInjected) {
		t.Errorf("latest: got %v", err)
	}
}

func seedHistory(t *testing.T, db *gorm.DB, taskID int64, start time.Time, steps ...any) {
	t.Helper()
	at, prev := start, model.TaskStateNone
	for i := 0; i < len(steps); i += 2 {
		at = at.Add(steps[i].(time.Duration))
		to := steps[i+1].(model.TaskState)
		if err := db.Create(&model.TaskStateTransition{UserID: 1, WorkspaceID: 10 + taskID%2, TaskID: taskID, FromState: prev, ToState: to, CreatedAt: at}).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
		prev = to
	}
}

func dumpRollups(t *testing.T, db *gorm.DB) map[string][]entity.TaskLatencyRollup {
	t.Helper()
	out := map[string][]entity.TaskLatencyRollup{}
	for _, table := range []string{hourlyTaskLatencies, dailyTaskLatencies, monthlyTaskLatencies} {
		var rows []entity.TaskLatencyRollup
		if err := db.Table(table).Order("period_start, user_id, workspace_id, metric").Scan(&rows).Error; err != nil {
			t.Fatalf("dump %s: %v", table, err)
		}
		out[table] = rows
	}
	return out
}

// The backfill adds the tasks closed before latency was recorded, keeps the
// ones already recorded, and leaves the rollups exactly as the aggregator
// would have built them.
func TestBackfillTaskLatency(t *testing.T) {
	db := latencyDB(t)
	repo := New(&mockDB{db: db})
	ctx := context.Background()
	defer func(n int) { backfillBatch = n }(backfillBatch)
	backfillBatch = 2 // several batches of tasks and of rows

	day := time.Date(2026, 8, 30, 9, 0, 0, 0, time.UTC)
	seedHistory(t, db, 1, day, time.Duration(0), model.TaskStateNotStarted, time.Minute, model.TaskStateOngoing,
		time.Hour, model.TaskStateBlocked, 10*time.Minute, model.TaskStateOngoing, 5*time.Minute, model.TaskStateCompleted)
	seedHistory(t, db, 2, day, time.Duration(0), model.TaskStateOngoing) // still open
	seedHistory(t, db, 3, day.AddDate(0, 0, 3), time.Duration(0), model.TaskStateNotStarted, 2*time.Hour, model.TaskStateRejected)
	seedHistory(t, db, 4, day.AddDate(0, 0, 5), time.Duration(0), model.TaskStateOngoing, 3*time.Minute, model.TaskStateNeedsInput,
		7*time.Minute, model.TaskStateOngoing, time.Minute, model.TaskStateCompleted)
	seedHistory(t, db, 5, day.AddDate(0, 0, 6), time.Duration(0), model.TaskStateOngoing, time.Minute, model.TaskStateCompleted)
	// Recorded by the server already: kept as it is, not recomputed.
	kept := model.TaskLatency{TaskID: 5, UserID: 1, WorkspaceID: 11, ClosedAt: day.AddDate(0, 0, 6).Unix() + 60, WorkedSeconds: 999}
	seedLatencies(t, db, kept)
	// Past the day cut but before the hour cut: in the hourly rollup only.
	seedHistory(t, db, 7, day.AddDate(0, 0, 15), time.Duration(0), model.TaskStateOngoing, time.Minute, model.TaskStateCompleted)
	// Closed after the cut: recorded, but in no rollup yet.
	seedHistory(t, db, 6, day.AddDate(0, 0, 30), time.Duration(0), model.TaskStateOngoing, time.Minute, model.TaskStateCompleted)

	hourCut := day.AddDate(0, 0, 20).Unix()
	dayCut := day.AddDate(0, 0, 10).Unix()
	added, err := repo.BackfillTaskLatency(ctx, hourCut, dayCut)
	if err != nil || added != 5 {
		t.Fatalf("added %d, %v; want 5 (tasks 1, 3, 4, 6, 7)", added, err)
	}
	if l, _ := latencyOf(t, db, 5); l != kept {
		t.Fatalf("a recorded task was rewritten: %+v", l)
	}
	if _, ok := latencyOf(t, db, 2); ok {
		t.Fatal("an open task has no latency")
	}
	l1, _ := latencyOf(t, db, 1)
	if l1.WorkedSeconds != 65*60 || l1.BlockedSeconds != 10*60 || *l1.StartToCloseSeconds != 75*60 || l1.WorkspaceID != 11 {
		t.Fatalf("task 1 = %+v", l1)
	}
	if l3, _ := latencyOf(t, db, 3); l3.StartToCloseSeconds != nil {
		t.Fatalf("task 3 never started: %+v", l3)
	}
	if l4, _ := latencyOf(t, db, 4); l4.NeedsInputSeconds != 7*60 {
		t.Fatalf("task 4 = %+v", l4)
	}
	backfilled := dumpRollups(t, db)
	if len(backfilled[hourlyTaskLatencies]) == 0 || len(backfilled[monthlyTaskLatencies]) == 0 {
		t.Fatalf("no rollups: %+v", backfilled)
	}

	// The same rollups, built the aggregator's way.
	for _, table := range []string{hourlyTaskLatencies, dailyTaskLatencies, monthlyTaskLatencies} {
		db.Exec("DELETE FROM " + table)
	}
	for h := day.Truncate(24 * time.Hour); h.Unix() < hourCut; h = h.Add(time.Hour) {
		_ = repo.AggregateHourlyTaskLatency(ctx, h.Unix(), h.Add(time.Hour).Unix())
	}
	for d := day.Truncate(24 * time.Hour); d.Unix() < dayCut; d = d.AddDate(0, 0, 1) {
		_ = repo.AggregateDailyTaskLatency(ctx, d.Unix(), d.AddDate(0, 0, 1).Unix())
	}
	for _, m := range []time.Time{time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)} {
		end := min(m.AddDate(0, 1, 0).Unix(), dayCut)
		_ = repo.AggregateMonthlyTaskLatency(ctx, m.Unix(), end)
	}
	aggregated := dumpRollups(t, db)
	for table, rows := range aggregated {
		if len(rows) != len(backfilled[table]) {
			t.Fatalf("%s: %d rows backfilled, %d aggregated", table, len(backfilled[table]), len(rows))
		}
		for i := range rows {
			if rows[i] != backfilled[table][i] {
				t.Fatalf("%s row %d: backfilled %+v, aggregated %+v", table, i, backfilled[table][i], rows[i])
			}
		}
	}

	// A second run adds nothing and changes nothing.
	if added, err := repo.BackfillTaskLatency(ctx, hourCut, dayCut); err != nil || added != 0 {
		t.Fatalf("second run added %d, %v", added, err)
	}
}

func TestBackfillTaskLatency_Failures(t *testing.T) {
	for _, tc := range []struct{ name, kind, table string }{
		{"list tasks", "row", "task_state_transitions"},
		{"read histories", "query", "task_state_transitions"},
		{"write latencies", "create", "task_latencies"},
		{"read latencies", "query", "task_latencies"},
		{"write rollups", "create", hourlyTaskLatencies},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := latencyDB(t)
			seedHistory(t, db, 1, time.Date(2026, 8, 30, 9, 0, 0, 0, time.UTC), time.Duration(0), model.TaskStateOngoing, time.Minute, model.TaskStateCompleted)
			failOn(t, db, tc.kind, tc.table)
			if _, err := New(&mockDB{db: db}).BackfillTaskLatency(context.Background(), time.Now().Unix(), time.Now().Unix()); !errors.Is(err, errInjected) {
				t.Fatalf("got %v, want the injected failure", err)
			}
		})
	}
}
