// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package telemetryaggregator

import (
	"context"
	"testing"
	"time"

	mock_repo "github.com/agentrq/agentrq/backend/internal/service/mocks/repository"
	"github.com/golang/mock/gomock"
)

func TestTelemetryAggregator_StartStop(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockRepo := mock_repo.NewMockRepository(ctrl)
	s := New(mockRepo)

	ctx, cancel := context.WithCancel(context.Background())
	s.Start(ctx)
	cancel()
	time.Sleep(10 * time.Millisecond)
}

// tick must fire nothing outside the three fixed run times.
func TestTelemetryAggregator_Tick_Quiet(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockRepo := mock_repo.NewMockRepository(ctrl)
	s := New(mockRepo).(*aggregator)

	// No EXPECT() calls set up: any repo call fails the test.
	s.tick(context.Background(), time.Date(2026, 9, 20, 13, 37, 0, 0, time.UTC))
}

func TestTelemetryAggregator_Tick_Hourly(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockRepo := mock_repo.NewMockRepository(ctrl)
	s := New(mockRepo).(*aggregator)

	now := time.Date(2026, 9, 20, 14, 0, 0, 0, time.UTC)
	periodStart := now.Add(-time.Hour)

	mockRepo.EXPECT().ClaimTelemetryAggregation(gomock.Any(), aggregationTypeHourly, periodStart.Format(hourKeyFormat)).Return(true, nil)
	mockRepo.EXPECT().AggregateHourlyTelemetry(gomock.Any(), periodStart.Unix(), now.Unix()).Return(nil)

	s.tick(context.Background(), now)
}

// A lost claim (another instance got there first) must not call the
// aggregation at all.
func TestTelemetryAggregator_Tick_Hourly_LostClaim(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockRepo := mock_repo.NewMockRepository(ctrl)
	s := New(mockRepo).(*aggregator)

	mockRepo.EXPECT().ClaimTelemetryAggregation(gomock.Any(), aggregationTypeHourly, gomock.Any()).Return(false, nil)
	// No AggregateHourlyTelemetry expectation: calling it fails the test.

	s.tick(context.Background(), time.Date(2026, 9, 20, 14, 0, 0, 0, time.UTC))
}

func TestTelemetryAggregator_Tick_Daily(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockRepo := mock_repo.NewMockRepository(ctrl)
	s := New(mockRepo).(*aggregator)

	now := time.Date(2026, 9, 20, 0, 15, 0, 0, time.UTC)
	todayStart := time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)
	periodStart := todayStart.AddDate(0, 0, -1)

	// Minute 15 is not an hourly boundary (minute 0), so only the daily run
	// fires on this tick.
	mockRepo.EXPECT().ClaimTelemetryAggregation(gomock.Any(), aggregationTypeDaily, periodStart.Format(dayKeyFormat)).Return(true, nil)
	mockRepo.EXPECT().AggregateDailyTelemetry(gomock.Any(), periodStart.Unix(), todayStart.Unix()).Return(nil)

	s.tick(context.Background(), now)
}

func TestTelemetryAggregator_Tick_Monthly(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockRepo := mock_repo.NewMockRepository(ctrl)
	s := New(mockRepo).(*aggregator)

	now := time.Date(2026, 9, 20, 0, 30, 0, 0, time.UTC)
	monthStart := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	todayStart := time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)

	mockRepo.EXPECT().ClaimTelemetryAggregation(gomock.Any(), aggregationTypeMonthly, now.Format(dayKeyFormat)).Return(true, nil)
	mockRepo.EXPECT().AggregateMonthlyTelemetry(gomock.Any(), monthStart.Unix(), todayStart.Unix()).Return(nil)

	s.tick(context.Background(), now)
}

// On the 1st the run must close the previous month, its last day included —
// taking the current month would sum [1st, 1st), nothing at all.
func TestTelemetryAggregator_Tick_MonthlyOnTheFirst(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockRepo := mock_repo.NewMockRepository(ctrl)
	s := New(mockRepo).(*aggregator)

	now := time.Date(2026, 10, 1, 0, 30, 0, 0, time.UTC)
	prevMonthStart := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	todayStart := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

	mockRepo.EXPECT().ClaimTelemetryAggregation(gomock.Any(), aggregationTypeMonthly, now.Format(dayKeyFormat)).Return(true, nil)
	mockRepo.EXPECT().AggregateMonthlyTelemetry(gomock.Any(), prevMonthStart.Unix(), todayStart.Unix()).Return(nil)

	s.tick(context.Background(), now)
}

// A repository error on aggregation must not panic and must not be treated
// as success; there is nothing further to assert without a mock logger, but
// the call must return.
func TestTelemetryAggregator_Tick_Hourly_AggregationError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockRepo := mock_repo.NewMockRepository(ctrl)
	s := New(mockRepo).(*aggregator)

	mockRepo.EXPECT().ClaimTelemetryAggregation(gomock.Any(), aggregationTypeHourly, gomock.Any()).Return(true, nil)
	mockRepo.EXPECT().AggregateHourlyTelemetry(gomock.Any(), gomock.Any(), gomock.Any()).Return(context.DeadlineExceeded)

	s.tick(context.Background(), time.Date(2026, 9, 20, 14, 0, 0, 0, time.UTC))
}

// A claim error (e.g. the DB is down) must also not call the aggregation.
func TestTelemetryAggregator_Tick_Hourly_ClaimError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockRepo := mock_repo.NewMockRepository(ctrl)
	s := New(mockRepo).(*aggregator)

	mockRepo.EXPECT().ClaimTelemetryAggregation(gomock.Any(), aggregationTypeHourly, gomock.Any()).Return(false, context.DeadlineExceeded)

	s.tick(context.Background(), time.Date(2026, 9, 20, 14, 0, 0, 0, time.UTC))
}

func TestTelemetryAggregator_Tick_Daily_LostClaim(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockRepo := mock_repo.NewMockRepository(ctrl)
	s := New(mockRepo).(*aggregator)

	mockRepo.EXPECT().ClaimTelemetryAggregation(gomock.Any(), aggregationTypeDaily, gomock.Any()).Return(false, nil)
	// No AggregateDailyTelemetry expectation: calling it fails the test.

	s.tick(context.Background(), time.Date(2026, 9, 20, 0, 15, 0, 0, time.UTC))
}

func TestTelemetryAggregator_Tick_Monthly_LostClaim(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockRepo := mock_repo.NewMockRepository(ctrl)
	s := New(mockRepo).(*aggregator)

	mockRepo.EXPECT().ClaimTelemetryAggregation(gomock.Any(), aggregationTypeMonthly, gomock.Any()).Return(false, nil)
	// No AggregateMonthlyTelemetry expectation: calling it fails the test.

	s.tick(context.Background(), time.Date(2026, 9, 20, 0, 30, 0, 0, time.UTC))
}

func TestTelemetryAggregator_Tick_Daily_ClaimError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockRepo := mock_repo.NewMockRepository(ctrl)
	s := New(mockRepo).(*aggregator)

	mockRepo.EXPECT().ClaimTelemetryAggregation(gomock.Any(), aggregationTypeDaily, gomock.Any()).Return(false, context.DeadlineExceeded)

	s.tick(context.Background(), time.Date(2026, 9, 20, 0, 15, 0, 0, time.UTC))
}

func TestTelemetryAggregator_Tick_Daily_AggregationError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockRepo := mock_repo.NewMockRepository(ctrl)
	s := New(mockRepo).(*aggregator)

	mockRepo.EXPECT().ClaimTelemetryAggregation(gomock.Any(), aggregationTypeDaily, gomock.Any()).Return(true, nil)
	mockRepo.EXPECT().AggregateDailyTelemetry(gomock.Any(), gomock.Any(), gomock.Any()).Return(context.DeadlineExceeded)

	s.tick(context.Background(), time.Date(2026, 9, 20, 0, 15, 0, 0, time.UTC))
}

func TestTelemetryAggregator_Tick_Monthly_ClaimError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockRepo := mock_repo.NewMockRepository(ctrl)
	s := New(mockRepo).(*aggregator)

	mockRepo.EXPECT().ClaimTelemetryAggregation(gomock.Any(), aggregationTypeMonthly, gomock.Any()).Return(false, context.DeadlineExceeded)

	s.tick(context.Background(), time.Date(2026, 9, 20, 0, 30, 0, 0, time.UTC))
}

func TestTelemetryAggregator_Tick_Monthly_AggregationError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockRepo := mock_repo.NewMockRepository(ctrl)
	s := New(mockRepo).(*aggregator)

	mockRepo.EXPECT().ClaimTelemetryAggregation(gomock.Any(), aggregationTypeMonthly, gomock.Any()).Return(true, nil)
	mockRepo.EXPECT().AggregateMonthlyTelemetry(gomock.Any(), gomock.Any(), gomock.Any()).Return(context.DeadlineExceeded)

	s.tick(context.Background(), time.Date(2026, 9, 20, 0, 30, 0, 0, time.UTC))
}
