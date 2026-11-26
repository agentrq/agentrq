// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package model

import "time"

// TaskTiming is what a task's state transitions add up to.
type TaskTiming struct {
	StartedAt           *time.Time // first time it went ongoing
	ClosedAt            *time.Time // when it became completed or rejected, if it still is
	StartToCloseSeconds *int64
	BlockedSeconds      int64
	NeedsInputSeconds   int64
	WorkedSeconds       int64
}

// TaskTimingOf adds up a task's transitions, oldest first. Each state lasts
// until the next transition, and the current one until now. A task counts as
// closed only while it is still completed or rejected, so a reopened task has
// no close time.
//
// Both the task page and the latency statistics are built from this, so the
// two can never disagree about how long a task took.
func TaskTimingOf(transitions []TaskStateTransition, now time.Time) TaskTiming {
	var timing TaskTiming
	for i, tr := range transitions {
		end := now
		if i+1 < len(transitions) {
			end = transitions[i+1].CreatedAt
		}
		seconds := int64(end.Sub(tr.CreatedAt) / time.Second)
		switch tr.ToState {
		case TaskStateOngoing:
			timing.WorkedSeconds += seconds
			if timing.StartedAt == nil {
				at := tr.CreatedAt
				timing.StartedAt = &at
			}
		case TaskStateBlocked:
			timing.BlockedSeconds += seconds
		case TaskStateNeedsInput:
			timing.NeedsInputSeconds += seconds
		}
	}
	if n := len(transitions); n > 0 {
		last := transitions[n-1]
		if last.ToState == TaskStateCompleted || last.ToState == TaskStateRejected {
			at := last.CreatedAt
			timing.ClosedAt = &at
		}
	}
	if timing.StartedAt != nil && timing.ClosedAt != nil {
		seconds := int64(timing.ClosedAt.Sub(*timing.StartedAt) / time.Second)
		timing.StartToCloseSeconds = &seconds
	}
	return timing
}

// TaskListTiming is what a task card shows. WorkedSeconds stops at the
// latest transition, so a card adds now − WorkingSince itself and counts up
// live without knowing when the server answered.
type TaskListTiming struct {
	WorkedSeconds int64
	WorkingSince  *time.Time // the latest transition, while it is to ongoing
	StatusSince   *time.Time // when the task entered status; a pending question is no change
}

// TaskListTimingOf is a card's timing from a task's transitions, oldest
// first; nil when there are none. StatusSince is nil when the history does
// not end in status.
func TaskListTimingOf(transitions []TaskStateTransition, status TaskState) *TaskListTiming {
	n := len(transitions)
	if n == 0 {
		return nil
	}
	last := transitions[n-1]
	timing := &TaskListTiming{WorkedSeconds: TaskTimingOf(transitions, last.CreatedAt).WorkedSeconds}
	if last.ToState == TaskStateOngoing {
		at := last.CreatedAt
		timing.WorkingSince = &at
	}
	current, since := TaskStateNone, time.Time{}
	for _, tr := range transitions {
		s := tr.ToState
		if s == TaskStateNeedsInput {
			s = tr.FromState
		}
		if s != current {
			current, since = s, tr.CreatedAt
		}
	}
	if current == status {
		timing.StatusSince = &since
	}
	return timing
}

// TaskLatency is one closed task's timing, written by the repository in the
// transaction that closes it. There is one row per task: closing it again
// after a reopen replaces the row with the totals over its whole history.
// A period already rolled up keeps the earlier close, so each close is
// counted once, in the period it happened in.
//
// StartToCloseSeconds is null for a task that was closed without ever going
// ongoing. The other three are always set, zero included.
type TaskLatency struct {
	TaskID              int64 `gorm:"primaryKey;autoIncrement:false"`
	UserID              int64 `gorm:"index:idx_task_latencies_user_id"`
	WorkspaceID         int64 `gorm:"index:idx_task_latencies_workspace_id"`
	ClosedAt            int64 `gorm:"index:idx_task_latencies_closed_at"` // unix seconds
	StartToCloseSeconds *int64
	WorkedSeconds       int64
	BlockedSeconds      int64
	NeedsInputSeconds   int64
}

// LatencyMetric names the value a latency rollup row summarises. Stored, so
// new ones only ever go on the end.
type LatencyMetric uint8

const (
	LatencyMetricUnknown LatencyMetric = iota
	LatencyMetricStartToClose
	LatencyMetricWorked
	LatencyMetricBlocked
	LatencyMetricNeedsInput
)

// LatencyMetrics lists every metric, in the order the API reports them.
var LatencyMetrics = []LatencyMetric{
	LatencyMetricStartToClose, LatencyMetricWorked, LatencyMetricBlocked, LatencyMetricNeedsInput,
}

// Value returns the row's value for each metric; ok is false where the task
// has none (start-to-close of a task never started).
func (l TaskLatency) Value(m LatencyMetric) (v int64, ok bool) {
	switch m {
	case LatencyMetricStartToClose:
		if l.StartToCloseSeconds == nil {
			return 0, false
		}
		return *l.StartToCloseSeconds, true
	case LatencyMetricWorked:
		return l.WorkedSeconds, true
	case LatencyMetricBlocked:
		return l.BlockedSeconds, true
	case LatencyMetricNeedsInput:
		return l.NeedsInputSeconds, true
	}
	return 0, false
}

// HourlyTaskLatency, DailyTaskLatency and MonthlyTaskLatency roll TaskLatency
// up, one row per (period, user, workspace, metric), the way the telemetry
// rollups roll up telemetries: hourly from the rows, daily from hourly,
// monthly from daily (internal/service/latencyaggregator). Histogram is the
// counts of tasklatency.Bounds, comma separated; it is what p50 is read from
// once rows are merged, while Min and Max merge exactly.

type HourlyTaskLatency struct {
	PeriodStart int64         `gorm:"uniqueIndex:uk_hourly_task_latencies_dims,priority:1"`
	UserID      int64         `gorm:"uniqueIndex:uk_hourly_task_latencies_dims,priority:2"`
	WorkspaceID int64         `gorm:"uniqueIndex:uk_hourly_task_latencies_dims,priority:3;index:idx_hourly_task_latencies_workspace_id"`
	Metric      LatencyMetric `gorm:"uniqueIndex:uk_hourly_task_latencies_dims,priority:4"`
	Count       int64
	Sum         int64
	Min         int64
	Max         int64
	Histogram   string `gorm:"type:varchar(255)"`
}

type DailyTaskLatency struct {
	PeriodStart int64         `gorm:"uniqueIndex:uk_daily_task_latencies_dims,priority:1"`
	UserID      int64         `gorm:"uniqueIndex:uk_daily_task_latencies_dims,priority:2"`
	WorkspaceID int64         `gorm:"uniqueIndex:uk_daily_task_latencies_dims,priority:3;index:idx_daily_task_latencies_workspace_id"`
	Metric      LatencyMetric `gorm:"uniqueIndex:uk_daily_task_latencies_dims,priority:4"`
	Count       int64
	Sum         int64
	Min         int64
	Max         int64
	Histogram   string `gorm:"type:varchar(255)"`
}

// MonthlyTaskLatency is recomputed every day for the month still open, so its
// rows are upserted on the unique index rather than inserted once.
type MonthlyTaskLatency struct {
	PeriodStart int64         `gorm:"uniqueIndex:uk_monthly_task_latencies_dims,priority:1"`
	UserID      int64         `gorm:"uniqueIndex:uk_monthly_task_latencies_dims,priority:2"`
	WorkspaceID int64         `gorm:"uniqueIndex:uk_monthly_task_latencies_dims,priority:3;index:idx_monthly_task_latencies_workspace_id"`
	Metric      LatencyMetric `gorm:"uniqueIndex:uk_monthly_task_latencies_dims,priority:4"`
	Count       int64
	Sum         int64
	Min         int64
	Max         int64
	Histogram   string `gorm:"type:varchar(255)"`
}
