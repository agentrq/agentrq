// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package telemetryaggregator

import (
	"context"
	"time"

	zlog "github.com/rs/zerolog/log"

	"github.com/agentrq/agentrq/backend/internal/repository/base"
)

const (
	aggregationTypeHourly  = "hourly"
	aggregationTypeDaily   = "daily"
	aggregationTypeMonthly = "monthly"

	hourKeyFormat = "2006-01-02T15"
	dayKeyFormat  = "2006-01-02"
)

type Service interface {
	Start(ctx context.Context)
}

type aggregator struct {
	repo base.Repository
}

// New builds the telemetry aggregator. It rolls the raw telemetries table up
// into hourly_telemetries at the top of every hour, hourly_telemetries into
// daily_telemetries once a day, and daily_telemetries into
// monthly_telemetries once a day for as long as the month stays open — see
// tick for exactly when each one fires.
func New(repo base.Repository) Service {
	return &aggregator{repo: repo}
}

func (a *aggregator) Start(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	go func() {
		zlog.Info().Msg("telemetryaggregator: background poller started (interval: 1m)")
		for {
			select {
			case <-ctx.Done():
				zlog.Info().Msg("telemetryaggregator: background poller stopped")
				return
			case <-ticker.C:
				a.tick(ctx, time.Now().UTC())
			}
		}
	}()
}

// tick checks the current UTC minute against the three fixed run times.
// Ticks are 1 minute apart and the checks are exact-match, so a tick that is
// skipped (a slow previous run, a paused process) skips that run entirely
// rather than firing late — matching the scheduler's tick semantics in
// internal/service/scheduler.
func (a *aggregator) tick(ctx context.Context, now time.Time) {
	now = now.Truncate(time.Minute)

	if now.Minute() == 0 {
		a.runHourly(ctx, now)
	}
	if now.Hour() == 0 && now.Minute() == 15 {
		a.runDaily(ctx, now)
	}
	if now.Hour() == 0 && now.Minute() == 30 {
		a.runMonthly(ctx, now)
	}
}

// runHourly aggregates the hour that just ended: [now-1h, now).
func (a *aggregator) runHourly(ctx context.Context, now time.Time) {
	periodStart := now.Add(-time.Hour)
	periodEnd := now

	claimed, err := a.repo.ClaimTelemetryAggregation(ctx, aggregationTypeHourly, periodStart.Format(hourKeyFormat))
	if err != nil {
		zlog.Error().Err(err).Msg("telemetryaggregator: failed to claim hourly aggregation")
		return
	}
	if !claimed {
		return
	}

	if err := a.repo.AggregateHourlyTelemetry(ctx, periodStart.Unix(), periodEnd.Unix()); err != nil {
		zlog.Error().Err(err).Time("period_start", periodStart).Msg("telemetryaggregator: hourly aggregation failed")
		return
	}
	zlog.Info().Time("period_start", periodStart).Msg("telemetryaggregator: hourly aggregation complete")
}

// runDaily aggregates the day that just ended: [today's midnight - 24h, today's midnight).
func (a *aggregator) runDaily(ctx context.Context, now time.Time) {
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	periodStart := todayStart.AddDate(0, 0, -1)
	periodEnd := todayStart

	claimed, err := a.repo.ClaimTelemetryAggregation(ctx, aggregationTypeDaily, periodStart.Format(dayKeyFormat))
	if err != nil {
		zlog.Error().Err(err).Msg("telemetryaggregator: failed to claim daily aggregation")
		return
	}
	if !claimed {
		return
	}

	if err := a.repo.AggregateDailyTelemetry(ctx, periodStart.Unix(), periodEnd.Unix()); err != nil {
		zlog.Error().Err(err).Time("period_start", periodStart).Msg("telemetryaggregator: daily aggregation failed")
		return
	}
	zlog.Info().Time("period_start", periodStart).Msg("telemetryaggregator: daily aggregation complete")
}

// runMonthly re-aggregates the month yesterday was in from
// daily_telemetries: [that month's start, today's midnight). It runs every day
// rather than once at month end, so the row for the month is always current
// through yesterday — and the run on the 1st closes the previous month with
// its last day, where taking today's month would sum nothing. The claim key is
// the day it ran, not the month, so this daily re-run is never blocked by its
// own earlier claims.
func (a *aggregator) runMonthly(ctx context.Context, now time.Time) {
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	yesterday := todayStart.AddDate(0, 0, -1)
	periodStart := time.Date(yesterday.Year(), yesterday.Month(), 1, 0, 0, 0, 0, time.UTC)
	periodEnd := todayStart

	claimed, err := a.repo.ClaimTelemetryAggregation(ctx, aggregationTypeMonthly, now.Format(dayKeyFormat))
	if err != nil {
		zlog.Error().Err(err).Msg("telemetryaggregator: failed to claim monthly aggregation")
		return
	}
	if !claimed {
		return
	}

	if err := a.repo.AggregateMonthlyTelemetry(ctx, periodStart.Unix(), periodEnd.Unix()); err != nil {
		zlog.Error().Err(err).Time("period_start", periodStart).Msg("telemetryaggregator: monthly aggregation failed")
		return
	}
	zlog.Info().Time("period_start", periodStart).Msg("telemetryaggregator: monthly aggregation complete")
}
