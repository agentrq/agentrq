package scheduler

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
	"github.com/agentrq/agentrq/backend/internal/service/eventinstruction"
	mock_idgen "github.com/agentrq/agentrq/backend/internal/service/mocks/idgen"
	mock_pubsub "github.com/agentrq/agentrq/backend/internal/service/mocks/pubsub"
	mock_repo "github.com/agentrq/agentrq/backend/internal/service/mocks/repository"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
	"github.com/golang/mock/gomock"
	"github.com/mustafaturan/monoflake"
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

	// A cron template linked to an event must pass its EventID on to spawned
	// children and carry the publishEvent instruction in the body — otherwise
	// the event chain silently never fires on scheduled runs.
	//
	// The instruction has to name the child, not the template: taskId is what
	// tells the server which run a publish continues, so asserting on the
	// child's own ID is the part that would catch a regression to naming the
	// parent (or naming nothing at all).
	t.Run("SpawnCopiesEventLink", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		mockRepo := mock_repo.NewMockRepository(ctrl)
		mockIdgen := mock_idgen.NewMockService(ctrl)
		mockPubSub := mock_pubsub.NewMockService(ctrl)
		s := New(mockRepo, mockIdgen, bus, mockPubSub)

		task := model.Task{ID: 1, WorkspaceID: 10, UserID: 1, CronSchedule: "0 9 * * *", Body: "do the thing", EventID: 77}
		mockRepo.EXPECT().SystemCheckTaskExists(gomock.Any(), int64(10), int64(1), "notstarted").Return(false, nil)
		mockRepo.EXPECT().SystemCheckTaskExists(gomock.Any(), int64(10), int64(1), "ongoing").Return(false, nil)
		mockRepo.EXPECT().GetEvent(gomock.Any(), int64(77), int64(1)).
			Return(model.Event{ID: 77, UserID: 1, Name: "run_done", PayloadGuidelines: "say what ran"}, nil)
		mockIdgen.EXPECT().NextID().Return(int64(2))
		mockRepo.EXPECT().CreateTask(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, child model.Task) (model.Task, error) {
			if child.EventID != 77 {
				t.Errorf("expected child EventID 77, got %d", child.EventID)
			}
			want := eventinstruction.Build(eventinstruction.Params{
				EventName:         "run_done",
				TaskID:            monoflake.ID(2).String(),
				PayloadGuidelines: "say what ran",
			})
			if child.Body != "do the thing"+want {
				t.Errorf("child body does not match the shared instruction wording:\ngot  %q\nwant %q", child.Body, "do the thing"+want)
			}
			if !strings.Contains(child.Body, `taskId: "`+monoflake.ID(2).String()+`"`) {
				t.Errorf("expected the child's own ID in the instruction, got %q", child.Body)
			}
			return model.Task{ID: 2}, nil
		})
		mockPubSub.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(&pubsub.PublishResponse{}, nil).AnyTimes()

		s.(*scheduler).spawn(context.Background(), task)
	})

	// Picking a workflow on a cron template sets EventID (the workflow's start
	// event) *and* WorkflowID, and records the choice in CompletionTriggerType.
	// Carrying only the first leaves the spawned run fanning out through the
	// global triggers instead of the workflow's steps, and the UI labelling it
	// as a bare event.
	t.Run("SpawnCopiesWorkflowLink", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		mockRepo := mock_repo.NewMockRepository(ctrl)
		mockIdgen := mock_idgen.NewMockService(ctrl)
		mockPubSub := mock_pubsub.NewMockService(ctrl)
		s := New(mockRepo, mockIdgen, bus, mockPubSub)

		task := model.Task{
			ID: 1, WorkspaceID: 10, UserID: 1, CronSchedule: "0 9 * * *", Body: "nightly build",
			EventID: 77, WorkflowID: 88, CompletionTriggerType: entity.CompletionTriggerWorkflow,
		}
		mockRepo.EXPECT().SystemCheckTaskExists(gomock.Any(), int64(10), int64(1), "notstarted").Return(false, nil)
		mockRepo.EXPECT().SystemCheckTaskExists(gomock.Any(), int64(10), int64(1), "ongoing").Return(false, nil)
		mockRepo.EXPECT().GetEvent(gomock.Any(), int64(77), int64(1)).Return(model.Event{ID: 77, UserID: 1, Name: "build_done"}, nil)
		mockIdgen.EXPECT().NextID().Return(int64(2))
		mockRepo.EXPECT().CreateTask(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, child model.Task) (model.Task, error) {
			if child.WorkflowID != 88 {
				t.Errorf("expected child WorkflowID 88, got %d", child.WorkflowID)
			}
			if child.CompletionTriggerType != entity.CompletionTriggerWorkflow {
				t.Errorf("expected child CompletionTriggerType %d, got %d", entity.CompletionTriggerWorkflow, child.CompletionTriggerType)
			}
			// Every scheduled run is hop zero of its own run, so the runaway
			// guard must start counting from scratch rather than inherit.
			if child.WorkflowDepth != 0 {
				t.Errorf("expected child WorkflowDepth 0, got %d", child.WorkflowDepth)
			}
			return model.Task{ID: 2}, nil
		})
		mockPubSub.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(&pubsub.PublishResponse{}, nil).AnyTimes()

		s.(*scheduler).spawn(context.Background(), task)
	})

	// An event row that has since been deleted must not cost the run: the child
	// is still spawned and still carries the link, it just goes out without an
	// instruction it could not build.
	t.Run("SpawnMissingEventStillSpawns", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		mockRepo := mock_repo.NewMockRepository(ctrl)
		mockIdgen := mock_idgen.NewMockService(ctrl)
		mockPubSub := mock_pubsub.NewMockService(ctrl)
		s := New(mockRepo, mockIdgen, bus, mockPubSub)

		task := model.Task{ID: 1, WorkspaceID: 10, UserID: 1, CronSchedule: "0 9 * * *", Body: "do the thing", EventID: 77}
		mockRepo.EXPECT().SystemCheckTaskExists(gomock.Any(), int64(10), int64(1), "notstarted").Return(false, nil)
		mockRepo.EXPECT().SystemCheckTaskExists(gomock.Any(), int64(10), int64(1), "ongoing").Return(false, nil)
		mockRepo.EXPECT().GetEvent(gomock.Any(), int64(77), int64(1)).Return(model.Event{}, errors.New("not found"))
		mockIdgen.EXPECT().NextID().Return(int64(2))
		mockRepo.EXPECT().CreateTask(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, child model.Task) (model.Task, error) {
			if child.Body != "do the thing" {
				t.Errorf("expected body untouched when the event cannot be resolved, got %q", child.Body)
			}
			if child.EventID != 77 {
				t.Errorf("expected child EventID 77 even without the instruction, got %d", child.EventID)
			}
			return model.Task{ID: 2}, nil
		})
		mockPubSub.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(&pubsub.PublishResponse{}, nil).AnyTimes()

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
