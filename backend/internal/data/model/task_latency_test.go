// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package model

import (
	"testing"
	"time"
)

func TestTaskTimingOf(t *testing.T) {
	t0 := time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)
	at := func(minutes int) time.Time { return t0.Add(time.Duration(minutes) * time.Minute) }
	tr := func(from, to TaskState, minutes int) TaskStateTransition {
		return TaskStateTransition{FromState: from, ToState: to, CreatedAt: at(minutes)}
	}
	now := at(100)

	for _, tc := range []struct {
		name                          string
		transitions                   []TaskStateTransition
		started, closed               *time.Time
		startToClose                  *int64
		blockedSeconds, workedSeconds int64
		needsInputSeconds             int64
	}{
		{name: "no history"},
		{
			name:        "not started yet",
			transitions: []TaskStateTransition{tr(TaskStateNone, TaskStateNotStarted, 0)},
		},
		{
			name: "worked, blocked, worked, completed",
			transitions: []TaskStateTransition{
				tr(TaskStateNone, TaskStateNotStarted, 0),
				tr(TaskStateNotStarted, TaskStateOngoing, 5),
				tr(TaskStateOngoing, TaskStateBlocked, 15),
				tr(TaskStateBlocked, TaskStateOngoing, 45),
				tr(TaskStateOngoing, TaskStateCompleted, 50),
			},
			started: ptr(at(5)), closed: ptr(at(50)), startToClose: ptr(int64(45 * 60)),
			blockedSeconds: 30 * 60, workedSeconds: 15 * 60,
		},
		{
			name: "still blocked counts up to now",
			transitions: []TaskStateTransition{
				tr(TaskStateNone, TaskStateOngoing, 0),
				tr(TaskStateOngoing, TaskStateBlocked, 40),
			},
			started:        ptr(at(0)),
			blockedSeconds: 60 * 60, workedSeconds: 40 * 60,
		},
		{
			name: "reopened is not closed",
			transitions: []TaskStateTransition{
				tr(TaskStateNone, TaskStateOngoing, 0),
				tr(TaskStateOngoing, TaskStateRejected, 10),
				tr(TaskStateRejected, TaskStateOngoing, 90),
			},
			started:       ptr(at(0)),
			workedSeconds: 10*60 + 10*60,
		},
		{
			name: "needing input is its own total",
			transitions: []TaskStateTransition{
				tr(TaskStateNone, TaskStateOngoing, 0),
				tr(TaskStateOngoing, TaskStateNeedsInput, 10),
				tr(TaskStateNeedsInput, TaskStateOngoing, 25),
				tr(TaskStateOngoing, TaskStateCompleted, 30),
			},
			started: ptr(at(0)), closed: ptr(at(30)), startToClose: ptr(int64(30 * 60)),
			workedSeconds: 15 * 60, needsInputSeconds: 15 * 60,
		},
		{
			name: "rejected without being started",
			transitions: []TaskStateTransition{
				tr(TaskStateNone, TaskStateNotStarted, 0),
				tr(TaskStateNotStarted, TaskStateRejected, 10),
			},
			closed: ptr(at(10)),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := TaskTimingOf(tc.transitions, now)
			if !sameTime(got.StartedAt, tc.started) || !sameTime(got.ClosedAt, tc.closed) {
				t.Errorf("started, closed = %v, %v; want %v, %v", got.StartedAt, got.ClosedAt, tc.started, tc.closed)
			}
			if (got.StartToCloseSeconds == nil) != (tc.startToClose == nil) ||
				(got.StartToCloseSeconds != nil && *got.StartToCloseSeconds != *tc.startToClose) {
				t.Errorf("startToClose = %v, want %v", got.StartToCloseSeconds, tc.startToClose)
			}
			if got.NeedsInputSeconds != tc.needsInputSeconds {
				t.Errorf("needs input = %d, want %d", got.NeedsInputSeconds, tc.needsInputSeconds)
			}
			if got.BlockedSeconds != tc.blockedSeconds || got.WorkedSeconds != tc.workedSeconds {
				t.Errorf("blocked, worked = %d, %d; want %d, %d", got.BlockedSeconds, got.WorkedSeconds, tc.blockedSeconds, tc.workedSeconds)
			}
		})
	}
}

func TestTaskLatency_Value(t *testing.T) {
	l := TaskLatency{StartToCloseSeconds: ptr(int64(9)), WorkedSeconds: 1, BlockedSeconds: 2, NeedsInputSeconds: 3}
	for m, want := range map[LatencyMetric]int64{
		LatencyMetricStartToClose: 9, LatencyMetricWorked: 1, LatencyMetricBlocked: 2, LatencyMetricNeedsInput: 3,
	} {
		if v, ok := l.Value(m); !ok || v != want {
			t.Errorf("Value(%d) = %d, %v; want %d", m, v, ok, want)
		}
	}
	// Never started: no start-to-close, but the rest still count, zeros too.
	if _, ok := (TaskLatency{}).Value(LatencyMetricStartToClose); ok {
		t.Error("a task never started has no start-to-close")
	}
	if v, ok := (TaskLatency{}).Value(LatencyMetricBlocked); !ok || v != 0 {
		t.Errorf("blocked = %d, %v; want 0, true", v, ok)
	}
	if _, ok := l.Value(LatencyMetricUnknown); ok {
		t.Error("an unknown metric has no value")
	}
}

func ptr[T any](v T) *T { return &v }

func sameTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(*b)
}
