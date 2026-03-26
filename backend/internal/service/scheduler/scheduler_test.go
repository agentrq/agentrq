package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/hasmcp/agentrq/backend/internal/data/model"
	"github.com/hasmcp/agentrq/backend/internal/service/eventbus"
	"github.com/hasmcp/agentrq/backend/internal/service/mocks/idgen"
	"github.com/hasmcp/agentrq/backend/internal/service/mocks/repository"
	"github.com/hasmcp/agentrq/backend/internal/service/mocks/telemetry"
)

func TestScheduler(t *testing.T) {
	bus := eventbus.New()

	t.Run("StartStop", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		mockRepo := repository.NewMockRepository(ctrl)
		mockIdgen := idgen.NewMockService(ctrl)
		mockTelemetry := telemetry.NewMockService(ctrl)
		s := New(mockRepo, mockIdgen, bus, mockTelemetry)

		ctx, cancel := context.WithCancel(context.Background())
		s.Start(ctx)
		cancel()
		time.Sleep(10 * time.Millisecond)
	})

	t.Run("TickNoCrons", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		mockRepo := repository.NewMockRepository(ctrl)
		mockIdgen := idgen.NewMockService(ctrl)
		mockTelemetry := telemetry.NewMockService(ctrl)
		s := New(mockRepo, mockIdgen, bus, mockTelemetry)

		mockRepo.EXPECT().SystemListTasksByStatus(gomock.Any(), "cron").Return([]model.Task{}, nil)
		s.(*scheduler).tick(context.Background())
	})

	t.Run("TickWithValidCron", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		mockRepo := repository.NewMockRepository(ctrl)
		mockIdgen := idgen.NewMockService(ctrl)
		mockTelemetry := telemetry.NewMockService(ctrl)
		s := New(mockRepo, mockIdgen, bus, mockTelemetry)

		task := model.Task{
			ID:           1,
			CronSchedule: "* * * * *",
			WorkspaceID:  10,
			UserID:       "u1",
		}
		mockRepo.EXPECT().SystemListTasksByStatus(gomock.Any(), "cron").Return([]model.Task{task}, nil)
		
		// Match whatever the current time is
		mockRepo.EXPECT().SystemCheckTaskExists(gomock.Any(), int64(10), int64(1), "notstarted").Return(false, nil).AnyTimes()
		mockRepo.EXPECT().SystemCheckTaskExists(gomock.Any(), int64(10), int64(1), "ongoing").Return(false, nil).AnyTimes()
		mockIdgen.EXPECT().NextID().Return(int64(2)).AnyTimes()
		mockRepo.EXPECT().CreateTask(gomock.Any(), gomock.Any()).Return(model.Task{ID: 2}, nil).AnyTimes()
		mockTelemetry.EXPECT().Record(gomock.Any(), "u1", int64(10), model.ActionIDTaskFromScheduled).AnyTimes()

		s.(*scheduler).tick(context.Background())
	})

	t.Run("TickWithInvalidCron", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		mockRepo := repository.NewMockRepository(ctrl)
		mockIdgen := idgen.NewMockService(ctrl)
		mockTelemetry := telemetry.NewMockService(ctrl)
		s := New(mockRepo, mockIdgen, bus, mockTelemetry)

		task := model.Task{ID: 1, CronSchedule: "invalid"}
		mockRepo.EXPECT().SystemListTasksByStatus(gomock.Any(), "cron").Return([]model.Task{task}, nil)
		s.(*scheduler).tick(context.Background())
	})

	t.Run("SpawnExists", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		mockRepo := repository.NewMockRepository(ctrl)
		mockIdgen := idgen.NewMockService(ctrl)
		mockTelemetry := telemetry.NewMockService(ctrl)
		s := New(mockRepo, mockIdgen, bus, mockTelemetry)

		task := model.Task{ID: 1, WorkspaceID: 10}
		mockRepo.EXPECT().SystemCheckTaskExists(gomock.Any(), int64(10), int64(1), "notstarted").Return(true, nil)
		s.(*scheduler).spawn(context.Background(), task)
	})

	t.Run("ListError", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		mockRepo := repository.NewMockRepository(ctrl)
		mockIdgen := idgen.NewMockService(ctrl)
		mockTelemetry := telemetry.NewMockService(ctrl)
		s := New(mockRepo, mockIdgen, bus, mockTelemetry)

		mockRepo.EXPECT().SystemListTasksByStatus(gomock.Any(), "cron").Return(nil, context.DeadlineExceeded)
		s.(*scheduler).tick(context.Background())
	})
}
