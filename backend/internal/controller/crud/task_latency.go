// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package crud

import (
	"context"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/service/tasklatency"
	"github.com/mustafaturan/monoflake"
)

// GetTaskLatencyStats is how long the tasks closed in a period took, per
// bucket and over the whole period, as the chosen aggregate.
//
// The period is statsWindow's, widened to whole buckets. Buckets the rollups
// already hold are read from them; the stretch after the newest rollup run is
// computed from the task rows, so the current hour or day is not missing.
func (c *controller) GetTaskLatencyStats(ctx context.Context, req entity.GetTaskLatencyStatsRequest) (*entity.GetTaskLatencyStatsResponse, error) {
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	if req.WorkspaceID != 0 {
		if _, err := c.repository.GetWorkspace(ctx, req.WorkspaceID, uid); err != nil {
			return nil, err
		}
	}

	start, end := statsWindow(req.Range, req.From, req.To, time.Now())
	g := tasklatency.GranularityFor(start, end)
	periods := tasklatency.Periods(start, end, g)
	first, last := periods[0], tasklatency.Next(periods[len(periods)-1], g)

	key, ok, err := c.repository.LatestTelemetryAggregation(ctx, tasklatency.ClaimType(g))
	if err != nil {
		return nil, err
	}
	tail := first
	if ok {
		tail = min(max(tasklatency.TailStart(g, key), first), last)
	}

	buckets := map[int64]map[model.LatencyMetric]*tasklatency.Stats{}
	at := func(period int64, m model.LatencyMetric) *tasklatency.Stats {
		if buckets[period] == nil {
			buckets[period] = map[model.LatencyMetric]*tasklatency.Stats{}
		}
		if buckets[period][m] == nil {
			buckets[period][m] = &tasklatency.Stats{}
		}
		return buckets[period][m]
	}

	if tail > first {
		rows, err := c.repository.ListTaskLatencyRollups(ctx, g, req.WorkspaceID, uid, first, tail)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			at(r.PeriodStart, model.LatencyMetric(r.Metric)).
				Merge(tasklatency.Decode(r.Count, r.Sum, r.Min, r.Max, r.Histogram))
		}
	}
	facts, err := c.repository.ListTaskLatencies(ctx, req.WorkspaceID, uid, tail, last)
	if err != nil {
		return nil, err
	}
	for _, f := range facts {
		period := tasklatency.Floor(f.ClosedAt, g)
		for _, m := range model.LatencyMetrics {
			if v, ok := f.Value(m); ok {
				at(period, m).Add(v)
			}
		}
	}

	res := &entity.GetTaskLatencyStatsResponse{
		Granularity: string(g),
		Aggregate:   req.Aggregate,
		RangeStart:  start,
		RangeEnd:    end,
		Points:      make([]entity.TaskLatencyPoint, 0, len(periods)),
	}
	total := map[model.LatencyMetric]*tasklatency.Stats{}
	for _, p := range periods {
		stats := map[model.LatencyMetric]tasklatency.Stats{}
		for m, s := range buckets[p] {
			stats[m] = *s
			if total[m] == nil {
				total[m] = &tasklatency.Stats{}
			}
			total[m].Merge(*s)
		}
		point := latencyPoint(stats, req.Aggregate)
		point.PeriodStart = p
		res.Points = append(res.Points, point)
	}
	totals := map[model.LatencyMetric]tasklatency.Stats{}
	for m, s := range total {
		totals[m] = *s
	}
	res.Summary = latencyPoint(totals, req.Aggregate)
	res.Summary.PeriodStart = first
	return res, nil
}

// latencyPoint reads each metric's aggregate. Every closed task has a worked
// time, zero included, so its count is the number of tasks closed.
func latencyPoint(stats map[model.LatencyMetric]tasklatency.Stats, aggregate string) entity.TaskLatencyPoint {
	value := func(m model.LatencyMetric) entity.TaskLatencyValue {
		s := stats[m]
		v, ok := s.Value(aggregate)
		if !ok {
			return entity.TaskLatencyValue{}
		}
		return entity.TaskLatencyValue{Seconds: &v, Count: s.Count}
	}
	return entity.TaskLatencyPoint{
		Closed:       stats[model.LatencyMetricWorked].Count,
		StartToClose: value(model.LatencyMetricStartToClose),
		Worked:       value(model.LatencyMetricWorked),
		Blocked:      value(model.LatencyMetricBlocked),
		NeedsInput:   value(model.LatencyMetricNeedsInput),
	}
}
