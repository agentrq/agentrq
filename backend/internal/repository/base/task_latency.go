// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package base

import (
	"context"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/service/tasklatency"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Latency rollup tables, by the granularity they hold.
const (
	hourlyTaskLatencies  = "hourly_task_latencies"
	dailyTaskLatencies   = "daily_task_latencies"
	monthlyTaskLatencies = "monthly_task_latencies"
)

var taskLatencyRollupTable = map[tasklatency.Granularity]string{
	tasklatency.Hour:  hourlyTaskLatencies,
	tasklatency.Day:   dailyTaskLatencies,
	tasklatency.Month: monthlyTaskLatencies,
}

// recordTaskLatency writes the closed task's timing from its whole history,
// replacing the row an earlier close left.
func recordTaskLatency(tx *gorm.DB, t model.Task) error {
	var rows []model.TaskStateTransition
	if err := tx.Where("task_id = ?", t.ID).Order("created_at asc, id asc").Find(&rows).Error; err != nil {
		return err
	}
	l := latencyOfHistory(rows)
	l.TaskID, l.UserID, l.WorkspaceID = t.ID, t.UserID, t.WorkspaceID
	return tx.Clauses(clause.OnConflict{UpdateAll: true}).Create(&l).Error
}

// latencyOfHistory is the latency of a task whose history, oldest first,
// ends in the close; the owner is the last transition's.
func latencyOfHistory(rows []model.TaskStateTransition) model.TaskLatency {
	last := rows[len(rows)-1]
	timing := model.TaskTimingOf(rows, last.CreatedAt)
	return model.TaskLatency{
		TaskID:              last.TaskID,
		UserID:              last.UserID,
		WorkspaceID:         last.WorkspaceID,
		ClosedAt:            last.CreatedAt.Unix(),
		StartToCloseSeconds: timing.StartToCloseSeconds,
		WorkedSeconds:       timing.WorkedSeconds,
		BlockedSeconds:      timing.BlockedSeconds,
		NeedsInputSeconds:   timing.NeedsInputSeconds,
	}
}

// latencyScope narrows a latency query to one workspace, or to every
// workspace of the user when workspaceID is 0.
func latencyScope(db *gorm.DB, workspaceID, userID int64) *gorm.DB {
	if workspaceID != 0 {
		return db.Where("workspace_id = ?", workspaceID)
	}
	return db.Where("user_id = ?", userID)
}

// ListTaskLatencies returns the tasks closed in [start, end), in scope.
func (r *repository) ListTaskLatencies(ctx context.Context, workspaceID, userID, start, end int64) ([]model.TaskLatency, error) {
	var rows []model.TaskLatency
	err := latencyScope(r.conn(ctx), workspaceID, userID).
		Where("closed_at >= ? AND closed_at < ?", start, end).
		Find(&rows).Error
	return rows, err
}

// ListTaskLatencyRollups returns the rollup rows of granularity g whose period
// starts in [start, end), in scope.
func (r *repository) ListTaskLatencyRollups(ctx context.Context, g tasklatency.Granularity, workspaceID, userID, start, end int64) ([]entity.TaskLatencyRollup, error) {
	var rows []entity.TaskLatencyRollup
	err := latencyScope(r.conn(ctx).Table(taskLatencyRollupTable[g]), workspaceID, userID).
		Where("period_start >= ? AND period_start < ?", start, end).
		Scan(&rows).Error
	return rows, err
}

// LatestTelemetryAggregation is the newest period key claimed for an
// aggregation type; ok is false when none has run yet.
func (r *repository) LatestTelemetryAggregation(ctx context.Context, aggregationType string) (string, bool, error) {
	var keys []string
	err := r.conn(ctx).Model(&model.TelemetryAggregation{}).
		Where("aggregation_type = ?", aggregationType).
		Order("period_key desc").Limit(1).
		Pluck("period_key", &keys).Error
	if err != nil || len(keys) == 0 {
		return "", false, err
	}
	return keys[0], true, nil
}

type latencyKey struct {
	userID, workspaceID int64
	metric              model.LatencyMetric
}

func latencyRollupRows(stats map[latencyKey]*tasklatency.Stats, periodStart int64) []entity.TaskLatencyRollup {
	rows := make([]entity.TaskLatencyRollup, 0, len(stats))
	for k, s := range stats {
		rows = append(rows, entity.TaskLatencyRollup{
			PeriodStart: periodStart,
			UserID:      k.userID,
			WorkspaceID: k.workspaceID,
			Metric:      uint8(k.metric),
			Count:       s.Count,
			Sum:         s.Sum,
			Min:         s.Min,
			Max:         s.Max,
			Histogram:   tasklatency.EncodeHist(s.Hist),
		})
	}
	return rows
}

// AggregateHourlyTaskLatency rolls the tasks closed in [periodStart,
// periodEnd) — normally one hour — up into hourly_task_latencies. A re-run
// over the same hour is a no-op.
func (r *repository) AggregateHourlyTaskLatency(ctx context.Context, periodStart, periodEnd int64) error {
	var facts []model.TaskLatency
	if err := r.conn(ctx).Where("closed_at >= ? AND closed_at < ?", periodStart, periodEnd).Find(&facts).Error; err != nil {
		return err
	}
	stats := map[latencyKey]*tasklatency.Stats{}
	for _, f := range facts {
		for _, m := range model.LatencyMetrics {
			v, ok := f.Value(m)
			if !ok {
				continue
			}
			k := latencyKey{f.UserID, f.WorkspaceID, m}
			if stats[k] == nil {
				stats[k] = &tasklatency.Stats{}
			}
			stats[k].Add(v)
		}
	}
	return writeTaskLatencyRollups(r.conn(ctx), hourlyTaskLatencies, latencyRollupRows(stats, periodStart), false)
}

// AggregateDailyTaskLatency merges the hourly rows of [periodStart, periodEnd)
// — normally one day — into daily_task_latencies.
func (r *repository) AggregateDailyTaskLatency(ctx context.Context, periodStart, periodEnd int64) error {
	return r.mergeTaskLatencyRollups(ctx, hourlyTaskLatencies, dailyTaskLatencies, periodStart, periodEnd)
}

// AggregateMonthlyTaskLatency merges the daily rows of [periodStart,
// periodEnd) into monthly_task_latencies. It runs every day the month is
// open, so each run replaces the month's rows with the fresh total.
func (r *repository) AggregateMonthlyTaskLatency(ctx context.Context, periodStart, periodEnd int64) error {
	return r.mergeTaskLatencyRollups(ctx, dailyTaskLatencies, monthlyTaskLatencies, periodStart, periodEnd)
}

func (r *repository) mergeTaskLatencyRollups(ctx context.Context, from, to string, periodStart, periodEnd int64) error {
	var src []entity.TaskLatencyRollup
	if err := r.conn(ctx).Table(from).
		Where("period_start >= ? AND period_start < ?", periodStart, periodEnd).
		Scan(&src).Error; err != nil {
		return err
	}
	stats := map[latencyKey]*tasklatency.Stats{}
	for _, row := range src {
		k := latencyKey{row.UserID, row.WorkspaceID, model.LatencyMetric(row.Metric)}
		if stats[k] == nil {
			stats[k] = &tasklatency.Stats{}
		}
		stats[k].Merge(tasklatency.Decode(row.Count, row.Sum, row.Min, row.Max, row.Histogram))
	}
	return writeTaskLatencyRollups(r.conn(ctx), to, latencyRollupRows(stats, periodStart), to == monthlyTaskLatencies)
}

var taskLatencyRollupColumns = []clause.Column{
	{Name: "period_start"}, {Name: "user_id"}, {Name: "workspace_id"}, {Name: "metric"},
}

// writeTaskLatencyRollups inserts rollup rows. replace upserts them instead,
// for the monthly re-run and the backfill; otherwise a row already there wins.
func writeTaskLatencyRollups(db *gorm.DB, table string, rows []entity.TaskLatencyRollup, replace bool) error {
	if len(rows) == 0 {
		return nil
	}
	conflict := clause.OnConflict{DoNothing: true}
	if replace {
		conflict = clause.OnConflict{
			Columns:   taskLatencyRollupColumns,
			DoUpdates: clause.AssignmentColumns([]string{"count", "sum", "min", "max", "histogram"}),
		}
	}
	return db.Table(table).Clauses(conflict).CreateInBatches(&rows, 500).Error
}

// moveTaskLatencyRollups gives a workspace's latency rollups to another,
// inside tx. Each row is merged into the other's row for the same period,
// user and metric, since the workspace is part of the unique key and a
// histogram cannot be added up in SQL.
func moveTaskLatencyRollups(tx *gorm.DB, fromID, toID int64) error {
	for _, table := range []string{hourlyTaskLatencies, dailyTaskLatencies, monthlyTaskLatencies} {
		var src []entity.TaskLatencyRollup
		if err := tx.Table(table).Where("workspace_id = ?", fromID).Scan(&src).Error; err != nil {
			return err
		}
		if len(src) == 0 {
			continue
		}
		type key struct {
			periodStart, userID int64
			metric              uint8
		}
		stats := make(map[key]*tasklatency.Stats, len(src))
		first, last := src[0].PeriodStart, src[0].PeriodStart
		for _, row := range src {
			s := tasklatency.Decode(row.Count, row.Sum, row.Min, row.Max, row.Histogram)
			stats[key{row.PeriodStart, row.UserID, row.Metric}] = &s
			first, last = min(first, row.PeriodStart), max(last, row.PeriodStart)
		}
		var dst []entity.TaskLatencyRollup
		if err := tx.Table(table).
			Where("workspace_id = ? AND period_start >= ? AND period_start <= ?", toID, first, last).
			Scan(&dst).Error; err != nil {
			return err
		}
		for _, row := range dst {
			if s := stats[key{row.PeriodStart, row.UserID, row.Metric}]; s != nil {
				s.Merge(tasklatency.Decode(row.Count, row.Sum, row.Min, row.Max, row.Histogram))
			}
		}
		rows := make([]entity.TaskLatencyRollup, 0, len(stats))
		for k, s := range stats {
			rows = append(rows, entity.TaskLatencyRollup{
				PeriodStart: k.periodStart, UserID: k.userID, WorkspaceID: toID, Metric: k.metric,
				Count: s.Count, Sum: s.Sum, Min: s.Min, Max: s.Max, Histogram: tasklatency.EncodeHist(s.Hist),
			})
		}
		if err := tx.Exec("DELETE FROM "+table+" WHERE workspace_id = ?", fromID).Error; err != nil {
			return err
		}
		if err := writeTaskLatencyRollups(tx, table, rows, true); err != nil {
			return err
		}
	}
	return nil
}

// backfillBatch is how many tasks the backfill reads at a time.
var backfillBatch = 500

// BackfillTaskLatency builds the latency of every task closed before this
// version recorded one, then recomputes the rollups from all of them: hourly
// for closes before hourCut, daily and monthly before dayCut. A task that
// already has a row keeps it, and rollup rows are replaced, so a re-run is
// harmless. It returns how many tasks it added.
func (r *repository) BackfillTaskLatency(ctx context.Context, hourCut, dayCut int64) (int, error) {
	db := r.conn(ctx)
	added := 0
	for after := int64(0); ; {
		var ids []int64
		err := db.Model(&model.TaskStateTransition{}).
			Select("DISTINCT task_id").
			Where("task_id > ? AND task_id NOT IN (?)", after, db.Model(&model.TaskLatency{}).Select("task_id")).
			Order("task_id").Limit(backfillBatch).
			Scan(&ids).Error
		if err != nil {
			return added, err
		}
		if len(ids) == 0 {
			break
		}
		after = ids[len(ids)-1]

		var rows []model.TaskStateTransition
		if err := db.Where("task_id IN ?", ids).Order("task_id, created_at, id").Find(&rows).Error; err != nil {
			return added, err
		}
		var latencies []model.TaskLatency
		for start := 0; start < len(rows); {
			end := start
			for end < len(rows) && rows[end].TaskID == rows[start].TaskID {
				end++
			}
			if isClosedTaskState(rows[end-1].ToState) {
				latencies = append(latencies, latencyOfHistory(rows[start:end]))
			}
			start = end
		}
		if len(latencies) > 0 {
			if err := db.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(&latencies, backfillBatch).Error; err != nil {
				return added, err
			}
			added += len(latencies)
		}
	}

	type grain struct {
		table string
		g     tasklatency.Granularity
		cut   int64
	}
	grains := []grain{
		{hourlyTaskLatencies, tasklatency.Hour, hourCut},
		{dailyTaskLatencies, tasklatency.Day, dayCut},
		{monthlyTaskLatencies, tasklatency.Month, dayCut},
	}
	type periodKey struct {
		period int64
		key    latencyKey
	}
	stats := make([]map[periodKey]*tasklatency.Stats, len(grains))
	for i := range stats {
		stats[i] = map[periodKey]*tasklatency.Stats{}
	}
	var facts []model.TaskLatency
	err := db.Where("closed_at < ?", max(hourCut, dayCut)).
		FindInBatches(&facts, backfillBatch, func(*gorm.DB, int) error {
			for _, f := range facts {
				for i, gr := range grains {
					if f.ClosedAt >= gr.cut {
						continue
					}
					for _, m := range model.LatencyMetrics {
						v, ok := f.Value(m)
						if !ok {
							continue
						}
						k := periodKey{tasklatency.Floor(f.ClosedAt, gr.g), latencyKey{f.UserID, f.WorkspaceID, m}}
						if stats[i][k] == nil {
							stats[i][k] = &tasklatency.Stats{}
						}
						stats[i][k].Add(v)
					}
				}
			}
			return nil
		}).Error
	if err != nil {
		return added, err
	}
	for i, gr := range grains {
		rows := make([]entity.TaskLatencyRollup, 0, len(stats[i]))
		for k, s := range stats[i] {
			rows = append(rows, latencyRollupRows(map[latencyKey]*tasklatency.Stats{k.key: s}, k.period)...)
		}
		if err := writeTaskLatencyRollups(r.conn(ctx), gr.table, rows, true); err != nil {
			return added, err
		}
	}
	return added, nil
}
