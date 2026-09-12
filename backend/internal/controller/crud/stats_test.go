// Copyright 2026 Contextual, Inc. https://agentrq.com

package crud

import (
	"context"
	"errors"
	"testing"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/golang/mock/gomock"
	"github.com/mustafaturan/monoflake"
)

// statsWindow is shared by the per-workspace and account-wide endpoints, so the
// calendar cases are pinned here at a fixed date. Wednesday 2026-09-09 12:00
// local is deliberately mid-week and mid-month: a Monday or a 1st would let an
// off-by-one in either direction pass unnoticed.
func TestStatsWindow(t *testing.T) {
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.Local)
	if now.Weekday() != time.Wednesday {
		t.Fatalf("fixture is meant to be a Wednesday, got %s", now.Weekday())
	}

	midnight := func(y int, m time.Month, d int) time.Time {
		return time.Date(y, m, d, 0, 0, 0, 0, time.Local)
	}

	tests := []struct {
		name      string
		rng       string
		from, to  int64
		wantStart int64
		wantEnd   int64
	}{
		{
			name:      "1d looks back exactly a day",
			rng:       "1d",
			wantStart: now.AddDate(0, 0, -1).Unix(),
			wantEnd:   now.Unix(),
		},
		{
			name:      "7d looks back exactly a week",
			rng:       "7d",
			wantStart: now.AddDate(0, 0, -7).Unix(),
			wantEnd:   now.Unix(),
		},
		{
			name:      "30d looks back exactly 30 days",
			rng:       "30d",
			wantStart: now.AddDate(0, 0, -30).Unix(),
			wantEnd:   now.Unix(),
		},
		{
			// Monday the 7th through the end of Sunday the 13th, not the
			// trailing seven days from Wednesday.
			name:      "week is the whole calendar week from Monday",
			rng:       "week",
			wantStart: midnight(2026, 9, 7).Unix(),
			wantEnd:   midnight(2026, 9, 14).Unix() - 1,
		},
		{
			name:      "month is the whole calendar month",
			rng:       "month",
			wantStart: midnight(2026, 9, 1).Unix(),
			wantEnd:   midnight(2026, 10, 1).Unix() - 1,
		},
		{
			name:      "custom uses the timestamps given",
			rng:       "custom",
			from:      1000,
			to:        2000,
			wantStart: 1000,
			wantEnd:   2000,
		},
		{
			// An open-ended custom range runs up to now rather than to zero,
			// which would otherwise produce an empty window.
			name:      "custom without an end runs to now",
			rng:       "custom",
			from:      1000,
			wantStart: 1000,
			wantEnd:   now.Unix(),
		},
		{
			name:      "an unknown range falls back to 7d",
			rng:       "not-a-range",
			wantStart: now.AddDate(0, 0, -7).Unix(),
			wantEnd:   now.Unix(),
		},
		{
			name:      "an empty range falls back to 7d",
			rng:       "",
			wantStart: now.AddDate(0, 0, -7).Unix(),
			wantEnd:   now.Unix(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end := statsWindow(tt.rng, tt.from, tt.to, now)
			if start != tt.wantStart {
				t.Errorf("start: want %d, got %d", tt.wantStart, start)
			}
			if end != tt.wantEnd {
				t.Errorf("end: want %d, got %d", tt.wantEnd, end)
			}
		})
	}
}

// The Monday case is the one that varies by which day the fixture lands on, so
// it is walked across a whole week rather than trusted at one date.
func TestStatsWindow_WeekStartsMondayWhicheverDayItIs(t *testing.T) {
	// 2026-09-07 is a Monday; step through to the following Sunday.
	monday := time.Date(2026, 9, 7, 9, 30, 0, 0, time.Local)
	wantStart := time.Date(2026, 9, 7, 0, 0, 0, 0, time.Local).Unix()
	wantEnd := time.Date(2026, 9, 14, 0, 0, 0, 0, time.Local).Unix() - 1

	for i := 0; i < 7; i++ {
		day := monday.AddDate(0, 0, i)
		start, end := statsWindow("week", 0, 0, day)
		if start != wantStart || end != wantEnd {
			t.Errorf("%s: want [%d,%d], got [%d,%d]", day.Weekday(), wantStart, wantEnd, start, end)
		}
	}
}

func TestGetDetailedUserStats(t *testing.T) {
	e := newTestController(t)

	const wsID = int64(4242)
	e.repo.EXPECT().
		GetDetailedUserStats(gomock.Any(), testUserID, gomock.Any(), gomock.Any()).
		Return(entity.GetDetailedUserStatsRows{
			Summary: entity.WorkspaceStatsSummary{TasksCompleted: 9, Messages: 4},
			Workspaces: []entity.WorkspaceStatsBreakdownRow{
				{WorkspaceID: wsID, Name: "busy", TasksCompleted: 9, Messages: 4},
			},
		}, nil)

	res, err := e.controller.GetDetailedUserStats(context.Background(), entity.GetUserStatsRequest{
		UserID: testUserIDStr,
		Range:  "7d",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Summary.TasksCompleted != 9 || res.Summary.Messages != 4 {
		t.Errorf("summary should pass through unchanged, got %+v", res.Summary)
	}
	if len(res.Workspaces) != 1 {
		t.Fatalf("expected 1 workspace in the breakdown, got %d", len(res.Workspaces))
	}
	// The raw ID must be re-encoded: the API speaks base62 everywhere else, and
	// a numeric ID here would not resolve against any route.
	if want := monoflake.ID(wsID).String(); res.Workspaces[0].WorkspaceID != want {
		t.Errorf("expected base62 workspace ID %q, got %q", want, res.Workspaces[0].WorkspaceID)
	}
	if res.Workspaces[0].Name != "busy" {
		t.Errorf("expected the name to pass through, got %q", res.Workspaces[0].Name)
	}
}

// The window the controller hands the repository has to match the range asked
// for; a 7d request that quietly queried all of time would look like working
// code and wrong numbers.
func TestGetDetailedUserStats_PassesTheRequestedWindow(t *testing.T) {
	e := newTestController(t)

	var gotStart, gotEnd int64
	e.repo.EXPECT().
		GetDetailedUserStats(gomock.Any(), testUserID, gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ int64, start, end int64) (entity.GetDetailedUserStatsRows, error) {
			gotStart, gotEnd = start, end
			return entity.GetDetailedUserStatsRows{}, nil
		})

	before := time.Now()
	if _, err := e.controller.GetDetailedUserStats(context.Background(), entity.GetUserStatsRequest{
		UserID: testUserIDStr,
		Range:  "custom",
		From:   1000,
		To:     2000,
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotStart != 1000 || gotEnd != 2000 {
		t.Errorf("expected the custom window [1000,2000], got [%d,%d]", gotStart, gotEnd)
	}

	// And a relative range resolves against the clock rather than being passed
	// through as zero.
	e.repo.EXPECT().
		GetDetailedUserStats(gomock.Any(), testUserID, gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ int64, start, end int64) (entity.GetDetailedUserStatsRows, error) {
			gotStart, gotEnd = start, end
			return entity.GetDetailedUserStatsRows{}, nil
		})
	if _, err := e.controller.GetDetailedUserStats(context.Background(), entity.GetUserStatsRequest{
		UserID: testUserIDStr,
		Range:  "7d",
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotEnd < before.Unix() {
		t.Errorf("expected the window to end at or after now (%d), got %d", before.Unix(), gotEnd)
	}
	if want := gotEnd - 7*24*3600; gotStart > want+5 || gotStart < want-5 {
		t.Errorf("expected a ~7 day window ending now, got [%d,%d]", gotStart, gotEnd)
	}
}

func TestGetDetailedUserStats_RepositoryError(t *testing.T) {
	e := newTestController(t)

	boom := errors.New("boom")
	e.repo.EXPECT().
		GetDetailedUserStats(gomock.Any(), testUserID, gomock.Any(), gomock.Any()).
		Return(entity.GetDetailedUserStatsRows{}, boom)

	res, err := e.controller.GetDetailedUserStats(context.Background(), entity.GetUserStatsRequest{
		UserID: testUserIDStr,
		Range:  "7d",
	})
	if !errors.Is(err, boom) {
		t.Errorf("expected the repository error to surface, got %v", err)
	}
	if res != nil {
		t.Errorf("expected no response alongside an error, got %+v", res)
	}
}

// An empty breakdown must serialise as [] rather than null: the frontend
// iterates it without a guard, and null would be a runtime error rather than an
// empty panel.
func TestGetDetailedUserStats_EmptyBreakdownIsNotNil(t *testing.T) {
	e := newTestController(t)

	e.repo.EXPECT().
		GetDetailedUserStats(gomock.Any(), testUserID, gomock.Any(), gomock.Any()).
		Return(entity.GetDetailedUserStatsRows{}, nil)

	res, err := e.controller.GetDetailedUserStats(context.Background(), entity.GetUserStatsRequest{
		UserID: testUserIDStr,
		Range:  "7d",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Workspaces == nil {
		t.Error("expected an empty slice, got nil")
	}
	if len(res.Workspaces) != 0 {
		t.Errorf("expected an empty breakdown, got %+v", res.Workspaces)
	}
}
