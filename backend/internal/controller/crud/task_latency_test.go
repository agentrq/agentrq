// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package crud

import (
	"context"
	"errors"
	"testing"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/service/tasklatency"
	"github.com/golang/mock/gomock"
)

func latencyHour(h int) int64 { return time.Date(2026, 9, 27, h, 0, 0, 0, time.UTC).Unix() }

// A custom day in UTC: 24 hourly buckets, whatever the local zone.
func latencyDayRequest(workspaceID int64, aggregate string) entity.GetTaskLatencyStatsRequest {
	return entity.GetTaskLatencyStatsRequest{
		WorkspaceID: workspaceID, UserID: testUserIDStr, Range: "custom",
		From: latencyHour(0), To: latencyHour(23) + 3599, Aggregate: aggregate,
	}
}

func rollupRow(period int64, m model.LatencyMetric, values ...int64) entity.TaskLatencyRollup {
	var s tasklatency.Stats
	for _, v := range values {
		s.Add(v)
	}
	return entity.TaskLatencyRollup{PeriodStart: period, Metric: uint8(m), Count: s.Count, Sum: s.Sum, Min: s.Min, Max: s.Max, Histogram: tasklatency.EncodeHist(s.Hist)}
}

func seconds(v entity.TaskLatencyValue) int64 {
	if v.Seconds == nil {
		return -1
	}
	return *v.Seconds
}

// Buckets up to the newest claimed run come from the rollups, the rest from
// the task rows, and a bucket both touch merges them.
func TestGetTaskLatencyStats_RollupsThenTail(t *testing.T) {
	e := newTestController(t)
	startToClose := int64(1800)

	e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(model.Workspace{ID: 1}, nil)
	e.repo.EXPECT().LatestTelemetryAggregation(gomock.Any(), tasklatency.ClaimHourly).Return("2026-09-27T09", true, nil)
	e.repo.EXPECT().ListTaskLatencyRollups(gomock.Any(), tasklatency.Hour, int64(1), testUserID, latencyHour(0), latencyHour(10)).
		Return([]entity.TaskLatencyRollup{
			rollupRow(latencyHour(9), model.LatencyMetricWorked, 600, 1200),
			rollupRow(latencyHour(9), model.LatencyMetricBlocked, 0, 0),
		}, nil)
	e.repo.EXPECT().ListTaskLatencies(gomock.Any(), int64(1), testUserID, latencyHour(10), latencyHour(24)).
		Return([]model.TaskLatency{
			{TaskID: 5, ClosedAt: latencyHour(10) + 60, StartToCloseSeconds: &startToClose, WorkedSeconds: 60, BlockedSeconds: 30},
		}, nil)

	stats, err := e.controller.GetTaskLatencyStats(context.Background(), latencyDayRequest(1, tasklatency.AggregateMax))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.Granularity != "hour" || stats.Aggregate != "max" || len(stats.Points) != 24 {
		t.Fatalf("got granularity %q, aggregate %q and %d points, want hour, max and 24", stats.Granularity, stats.Aggregate, len(stats.Points))
	}
	nine, ten := stats.Points[9], stats.Points[10]
	if nine.PeriodStart != latencyHour(9) || nine.Closed != 2 || seconds(nine.Worked) != 1200 || seconds(nine.Blocked) != 0 {
		t.Fatalf("the 09:00 bucket is %+v, want 2 closed, 1200s worked and 0s blocked from the rollups", nine)
	}
	// No task in the rollup had a start-to-close: a gap, not zero.
	if nine.StartToClose.Seconds != nil || seconds(ten.StartToClose) != 1800 || seconds(ten.Blocked) != 30 {
		t.Fatalf("the 09:00 bucket is %+v and the 10:00 bucket %+v, want no start-to-close at 09:00, and 1800s start-to-close and 30s blocked at 10:00", nine, ten)
	}
	if stats.Points[0].Worked.Seconds != nil || stats.Points[0].Closed != 0 {
		t.Fatalf("an empty hour must be a gap, got %+v", stats.Points[0])
	}
	if s := stats.Summary; s.Closed != 3 || seconds(s.Worked) != 1200 || seconds(s.Blocked) != 30 || s.StartToClose.Count != 1 {
		t.Fatalf("the summary is %+v, want 3 closed, 1200s worked, 30s blocked and one start-to-close", s)
	}
}

// With no rollup run yet, everything is read from the task rows; account
// scope skips the ownership check.
func TestGetTaskLatencyStats_NoRollupYet(t *testing.T) {
	e := newTestController(t)
	e.repo.EXPECT().LatestTelemetryAggregation(gomock.Any(), tasklatency.ClaimHourly).Return("", false, nil)
	e.repo.EXPECT().ListTaskLatencies(gomock.Any(), int64(0), testUserID, latencyHour(0), latencyHour(24)).
		Return([]model.TaskLatency{
			{ClosedAt: latencyHour(3), WorkedSeconds: 100},
			{ClosedAt: latencyHour(3) + 10, WorkedSeconds: 300},
			{ClosedAt: latencyHour(3) + 20, WorkedSeconds: 50},
		}, nil)

	stats, err := e.controller.GetTaskLatencyStats(context.Background(), latencyDayRequest(0, tasklatency.AggregateMin))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p := stats.Points[3]; p.Closed != 3 || seconds(p.Worked) != 50 {
		t.Fatalf("the 03:00 bucket is %+v, want 3 closed and a minimum of 50s worked", p)
	}
}

// A claim past the period's end reads nothing from the rollups' future: the
// tail is clamped to the period.
func TestGetTaskLatencyStats_ClaimPastThePeriod(t *testing.T) {
	e := newTestController(t)
	e.repo.EXPECT().LatestTelemetryAggregation(gomock.Any(), tasklatency.ClaimHourly).Return("2026-10-05T00", true, nil)
	e.repo.EXPECT().ListTaskLatencyRollups(gomock.Any(), tasklatency.Hour, int64(0), testUserID, latencyHour(0), latencyHour(24)).Return(nil, nil)
	e.repo.EXPECT().ListTaskLatencies(gomock.Any(), int64(0), testUserID, latencyHour(24), latencyHour(24)).Return(nil, nil)

	stats, err := e.controller.GetTaskLatencyStats(context.Background(), latencyDayRequest(0, tasklatency.AggregateP50))
	if err != nil || stats.Summary.Closed != 0 || stats.Summary.Worked.Seconds != nil {
		t.Fatalf("got stats %+v and error %v, want an empty summary and no error", stats, err)
	}
}

func TestGetTaskLatencyStats_Errors(t *testing.T) {
	errDB := errors.New("database unavailable")
	for _, tc := range []struct {
		name  string
		setup func(e *testEnv)
		ws    int64
	}{
		{"a workspace the caller cannot read fails the request", func(e *testEnv) {
			e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(model.Workspace{}, errDB)
		}, 1},
		{"a failed aggregation claim lookup fails the request", func(e *testEnv) {
			e.repo.EXPECT().LatestTelemetryAggregation(gomock.Any(), gomock.Any()).Return("", false, errDB)
		}, 0},
		{"failing to list the rollups fails the request", func(e *testEnv) {
			e.repo.EXPECT().LatestTelemetryAggregation(gomock.Any(), gomock.Any()).Return("2026-09-27T05", true, nil)
			e.repo.EXPECT().ListTaskLatencyRollups(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errDB)
		}, 0},
		{"failing to list the task rows fails the request", func(e *testEnv) {
			e.repo.EXPECT().LatestTelemetryAggregation(gomock.Any(), gomock.Any()).Return("", false, nil)
			e.repo.EXPECT().ListTaskLatencies(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errDB)
		}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newTestController(t)
			tc.setup(e)
			if _, err := e.controller.GetTaskLatencyStats(context.Background(), latencyDayRequest(tc.ws, "p50")); !errors.Is(err, errDB) {
				t.Fatalf("GetTaskLatencyStats returned %v, want the database error %v", err, errDB)
			}
		})
	}
}
