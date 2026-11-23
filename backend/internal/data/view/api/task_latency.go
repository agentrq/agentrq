// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package api

type (
	// TaskLatencyValue is one metric's aggregate in seconds, over the tasks
	// that closed in the bucket. seconds is null when none did: a gap, not 0.
	TaskLatencyValue struct {
		Seconds *int64 `json:"seconds"`
		Count   int64  `json:"count"`
	}

	TaskLatencyPoint struct {
		PeriodStart  int64            `json:"periodStart"`
		Closed       int64            `json:"closed"`
		StartToClose TaskLatencyValue `json:"startToClose"`
		Worked       TaskLatencyValue `json:"worked"`
		Blocked      TaskLatencyValue `json:"blocked"`
		NeedsInput   TaskLatencyValue `json:"needsInput"`
	}

	TaskLatencySummary struct {
		Closed       int64            `json:"closed"`
		StartToClose TaskLatencyValue `json:"startToClose"`
		Worked       TaskLatencyValue `json:"worked"`
		Blocked      TaskLatencyValue `json:"blocked"`
		NeedsInput   TaskLatencyValue `json:"needsInput"`
	}

	TaskLatencyStats struct {
		Granularity string             `json:"granularity"`
		Aggregate   string             `json:"aggregate"`
		RangeStart  int64              `json:"rangeStart"`
		RangeEnd    int64              `json:"rangeEnd"`
		Points      []TaskLatencyPoint `json:"points"`
		Summary     TaskLatencySummary `json:"summary"`
	}
)
