package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
	mock_idgen "github.com/agentrq/agentrq/backend/internal/service/mocks/idgen"
	mock_pubsub "github.com/agentrq/agentrq/backend/internal/service/mocks/pubsub"
	mock_repo "github.com/agentrq/agentrq/backend/internal/service/mocks/repository"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
	"github.com/golang/mock/gomock"
)

func eqMinutes(a, b []time.Time) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !a[i].Equal(b[i]) {
			return false
		}
	}
	return true
}

func TestMinutesToProcess(t *testing.T) {
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	min := func(offset int) time.Time { return base.Add(time.Duration(offset) * time.Minute) }

	t.Run("first run returns only current minute", func(t *testing.T) {
		if got := minutesToProcess(time.Time{}, base); !eqMinutes(got, []time.Time{base}) {
			t.Fatalf("got %v", got)
		}
	})
	t.Run("single elapsed minute", func(t *testing.T) {
		if got := minutesToProcess(min(-1), base); !eqMinutes(got, []time.Time{base}) {
			t.Fatalf("got %v", got)
		}
	})
	t.Run("catches up a multi-minute gap in order", func(t *testing.T) {
		want := []time.Time{min(-2), min(-1), base}
		if got := minutesToProcess(min(-3), base); !eqMinutes(got, want) {
			t.Fatalf("got %v, want %v", got, want)
		}
	})
	t.Run("same minute returns nil", func(t *testing.T) {
		if got := minutesToProcess(base, base); got != nil {
			t.Fatalf("got %v, want nil", got)
		}
	})
	t.Run("sub-minute advance returns nil (now truncates to same minute)", func(t *testing.T) {
		if got := minutesToProcess(base, base.Add(30*time.Second)); got != nil {
			t.Fatalf("got %v, want nil", got)
		}
	})
	t.Run("clock moving backward returns nil", func(t *testing.T) {
		if got := minutesToProcess(base, min(-5)); got != nil {
			t.Fatalf("got %v, want nil", got)
		}
	})
	t.Run("huge gap is capped to the most recent window", func(t *testing.T) {
		got := minutesToProcess(min(-500), base)
		if len(got) != maxCatchUpMinutes {
			t.Fatalf("len = %d, want %d", len(got), maxCatchUpMinutes)
		}
		if !got[len(got)-1].Equal(base) {
			t.Fatalf("last = %v, want %v", got[len(got)-1], base)
		}
		if want := base.Add(-time.Duration(maxCatchUpMinutes-1) * time.Minute); !got[0].Equal(want) {
			t.Fatalf("first = %v, want %v", got[0], want)
		}
	})
}

// A gap of several minutes must fire an every-minute schedule once per missed minute,
// rather than only once for "now".
func TestTickCatchesUpMissedMinutes(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockRepo := mock_repo.NewMockRepository(ctrl)
	mockIdgen := mock_idgen.NewMockService(ctrl)
	mockPubSub := mock_pubsub.NewMockService(ctrl)
	s := New(mockRepo, mockIdgen, eventbus.New(), mockPubSub).(*scheduler)

	// Pretend the poller last ran two minutes ago (e.g. it drifted / stalled).
	s.lastProcessed = time.Now().UTC().Truncate(time.Minute).Add(-2 * time.Minute)

	task := model.Task{ID: 1, WorkspaceID: 10, UserID: 1, CronSchedule: "* * * * *"}
	mockRepo.EXPECT().SystemListTasksByStatus(gomock.Any(), "cron").Return([]model.Task{task}, nil)
	mockRepo.EXPECT().SystemCheckTaskExists(gomock.Any(), int64(10), int64(1), "notstarted").Return(false, nil).AnyTimes()
	mockRepo.EXPECT().SystemCheckTaskExists(gomock.Any(), int64(10), int64(1), "ongoing").Return(false, nil).AnyTimes()
	mockIdgen.EXPECT().NextID().Return(int64(2)).AnyTimes()
	mockPubSub.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(&pubsub.PublishResponse{}, nil).AnyTimes()

	// The gap spans at least two prior minutes plus the current one, so an every-minute
	// schedule must spawn more than once (the old single-"now" logic spawned exactly once).
	spawns := 0
	mockRepo.EXPECT().CreateTask(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, task model.Task) (model.Task, error) {
			spawns++
			return model.Task{ID: 2}, nil
		}).MinTimes(2)

	s.tick(context.Background())

	if spawns < 2 {
		t.Fatalf("catch-up spawned %d times, want >= 2", spawns)
	}
}
