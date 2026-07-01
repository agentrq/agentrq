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

func TestScheduler(t *testing.T) {
	bus := eventbus.New()

	t.Run("StartStop", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		mockRepo := mock_repo.NewMockRepository(ctrl)
		mockIdgen := mock_idgen.NewMockService(ctrl)
		mockPubSub := mock_pubsub.NewMockService(ctrl)
		s := New(mockRepo, mockIdgen, bus, mockPubSub)

		ctx, cancel := context.WithCancel(context.Background())
		s.Start(ctx)
		cancel()
		time.Sleep(10 * time.Millisecond)
	})

	t.Run("TickNoCrons", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		mockRepo := mock_repo.NewMockRepository(ctrl)
		mockIdgen := mock_idgen.NewMockService(ctrl)
		mockPubSub := mock_pubsub.NewMockService(ctrl)
		s := New(mockRepo, mockIdgen, bus, mockPubSub)

		mockRepo.EXPECT().SystemListTasksByStatus(gomock.Any(), "cron").Return([]model.Task{}, nil)
		s.(*scheduler).tick(context.Background())
	})

	t.Run("TickWithValidCron", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		mockRepo := mock_repo.NewMockRepository(ctrl)
		mockIdgen := mock_idgen.NewMockService(ctrl)
		mockPubSub := mock_pubsub.NewMockService(ctrl)
		s := New(mockRepo, mockIdgen, bus, mockPubSub)

		task := model.Task{
			ID:           1,
			CronSchedule: "* * * * *",
			WorkspaceID:  10,
			UserID:       1,
		}
		mockRepo.EXPECT().SystemListTasksByStatus(gomock.Any(), "cron").Return([]model.Task{task}, nil)

		mockRepo.EXPECT().SystemCheckTaskExists(gomock.Any(), int64(10), int64(1), "notstarted").Return(false, nil).AnyTimes()
		mockRepo.EXPECT().SystemCheckTaskExists(gomock.Any(), int64(10), int64(1), "ongoing").Return(false, nil).AnyTimes()
		mockIdgen.EXPECT().NextID().Return(int64(2)).AnyTimes()
		mockRepo.EXPECT().CreateTask(gomock.Any(), gomock.Any()).Return(model.Task{ID: 2}, nil).AnyTimes()
		mockPubSub.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(&pubsub.PublishResponse{}, nil).AnyTimes()

		s.(*scheduler).tick(context.Background())
	})

	t.Run("TickWithInvalidCron", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		mockRepo := mock_repo.NewMockRepository(ctrl)
		mockIdgen := mock_idgen.NewMockService(ctrl)
		mockPubSub := mock_pubsub.NewMockService(ctrl)
		s := New(mockRepo, mockIdgen, bus, mockPubSub)

		task := model.Task{ID: 1, CronSchedule: "invalid"}
		mockRepo.EXPECT().SystemListTasksByStatus(gomock.Any(), "cron").Return([]model.Task{task}, nil)
		s.(*scheduler).tick(context.Background())
	})

	// A fixed-month schedule whose day-of-month is a wildcard (e.g. "9:00 every
	// day in June") is a RECURRING schedule per the frontend contract
	// (useCron.js / the task-form generators treat a cron as one-time only when
	// BOTH day-of-month and month are specific). The scheduler must not delete
	// the parent template after the first spawn.
	t.Run("SpawnRecurringFixedMonthKeepsParent", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		mockRepo := mock_repo.NewMockRepository(ctrl)
		mockIdgen := mock_idgen.NewMockService(ctrl)
		mockPubSub := mock_pubsub.NewMockService(ctrl)
		s := New(mockRepo, mockIdgen, bus, mockPubSub)

		task := model.Task{ID: 1, WorkspaceID: 10, UserID: 1, CronSchedule: "0 9 * 6 *"}
		mockRepo.EXPECT().SystemCheckTaskExists(gomock.Any(), int64(10), int64(1), "notstarted").Return(false, nil)
		mockRepo.EXPECT().SystemCheckTaskExists(gomock.Any(), int64(10), int64(1), "ongoing").Return(false, nil)
		mockIdgen.EXPECT().NextID().Return(int64(2))
		mockRepo.EXPECT().CreateTask(gomock.Any(), gomock.Any()).Return(model.Task{ID: 2}, nil)
		mockPubSub.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(&pubsub.PublishResponse{}, nil).AnyTimes()
		// No DeleteTask expectation: gomock fails the test if spawn deletes the parent.

		s.(*scheduler).spawn(context.Background(), task)
	})

	// A true one-time schedule has BOTH day-of-month and month specific (the
	// shape the frontend emits for "run once at <datetime>"). The parent
	// template must be deleted after it spawns its single run.
	t.Run("SpawnOneTimeDeletesParent", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		mockRepo := mock_repo.NewMockRepository(ctrl)
		mockIdgen := mock_idgen.NewMockService(ctrl)
		mockPubSub := mock_pubsub.NewMockService(ctrl)
		s := New(mockRepo, mockIdgen, bus, mockPubSub)

		task := model.Task{ID: 1, WorkspaceID: 10, UserID: 1, CronSchedule: "0 9 1 1 *"}
		mockRepo.EXPECT().SystemCheckTaskExists(gomock.Any(), int64(10), int64(1), "notstarted").Return(false, nil)
		mockRepo.EXPECT().SystemCheckTaskExists(gomock.Any(), int64(10), int64(1), "ongoing").Return(false, nil)
		mockIdgen.EXPECT().NextID().Return(int64(2))
		mockRepo.EXPECT().CreateTask(gomock.Any(), gomock.Any()).Return(model.Task{ID: 2}, nil)
		mockPubSub.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(&pubsub.PublishResponse{}, nil).AnyTimes()
		mockRepo.EXPECT().DeleteTask(gomock.Any(), int64(10), int64(1), int64(1)).Return(nil)

		s.(*scheduler).spawn(context.Background(), task)
	})

	t.Run("SpawnExists", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		mockRepo := mock_repo.NewMockRepository(ctrl)
		mockIdgen := mock_idgen.NewMockService(ctrl)
		mockPubSub := mock_pubsub.NewMockService(ctrl)
		s := New(mockRepo, mockIdgen, bus, mockPubSub)

		task := model.Task{ID: 1, WorkspaceID: 10}
		mockRepo.EXPECT().SystemCheckTaskExists(gomock.Any(), int64(10), int64(1), "notstarted").Return(true, nil)
		s.(*scheduler).spawn(context.Background(), task)
	})

	t.Run("ListError", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		mockRepo := mock_repo.NewMockRepository(ctrl)
		mockIdgen := mock_idgen.NewMockService(ctrl)
		mockPubSub := mock_pubsub.NewMockService(ctrl)
		s := New(mockRepo, mockIdgen, bus, mockPubSub)

		mockRepo.EXPECT().SystemListTasksByStatus(gomock.Any(), "cron").Return(nil, context.DeadlineExceeded)
		s.(*scheduler).tick(context.Background())
	})
}
