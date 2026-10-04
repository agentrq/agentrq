// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package scheduler

import (
	"bytes"
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
	"github.com/rs/zerolog"
	zlog "github.com/rs/zerolog/log"
)

// spawnAt is the minute a test's run was scheduled for.
var spawnAt = time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)

// captureLogs sends the global logger to a buffer for the rest of the test.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := zlog.Logger
	zlog.Logger = zerolog.New(zerolog.SyncWriter(&buf))
	t.Cleanup(func() { zlog.Logger = prev })
	return &buf
}

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
		mockRepo.EXPECT().SystemCreateCronRun(gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
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
		mockRepo.EXPECT().SystemCreateCronRun(gomock.Any(), gomock.Any()).Return(true, nil)
		mockPubSub.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(&pubsub.PublishResponse{}, nil).AnyTimes()
		// No DeleteTask expectation: gomock fails the test if spawn deletes the parent.

		s.(*scheduler).spawn(context.Background(), task, spawnAt)
	})

	// A true one-time schedule has BOTH day-of-month and month specific (the
	// shape the frontend emits for "run once at <datetime>"). It runs as the
	// template itself: no child is created and nothing is deleted.
	t.Run("SpawnOneTimeStartsTemplate", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		mockRepo := mock_repo.NewMockRepository(ctrl)
		mockIdgen := mock_idgen.NewMockService(ctrl)
		mockPubSub := mock_pubsub.NewMockService(ctrl)
		s := New(mockRepo, mockIdgen, bus, mockPubSub)
		ch := bus.Subscribe(10, "")
		defer bus.Unsubscribe(10, "", ch)

		task := model.Task{ID: 1, WorkspaceID: 10, UserID: 1, Status: "cron", CronSchedule: "0 9 1 1 *", Body: "once"}
		mockRepo.EXPECT().SystemCheckTaskExists(gomock.Any(), int64(10), int64(1), "notstarted").Return(false, nil)
		mockRepo.EXPECT().SystemCheckTaskExists(gomock.Any(), int64(10), int64(1), "ongoing").Return(false, nil)
		// No NextID, CreateTask or DeleteTask expectation: gomock fails the
		// test if the one-time run makes a child or deletes the template.
		mockRepo.EXPECT().SystemStartOneTimeTask(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, run model.Task) (bool, error) {
			if run.ID != 1 || run.Status != "notstarted" || run.CronSchedule != "" || run.Body != "once" {
				t.Errorf("unexpected run: %+v", run)
			}
			if !run.CreatedAt.Equal(spawnAt) {
				t.Errorf("expected the run created at the scheduled minute %v, got %v", spawnAt, run.CreatedAt)
			}
			return true, nil
		})
		mockPubSub.EXPECT().Publish(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, req pubsub.PublishRequest) (*pubsub.PublishResponse, error) {
			ev := req.Event.(entity.CRUDEvent)
			if ev.Action != entity.ActionTaskFromScheduled || ev.ResourceID != 1 {
				t.Errorf("expected ActionTaskFromScheduled for task 1, got %+v", ev)
			}
			return &pubsub.PublishResponse{}, nil
		})

		s.(*scheduler).spawn(context.Background(), task, spawnAt)

		select {
		case line := <-ch:
			if !strings.Contains(string(line), `"type":"task.updated"`) || !strings.Contains(string(line), `"status":"notstarted"`) {
				t.Errorf("expected a task.updated event for the started task, got %s", line)
			}
		default:
			t.Error("expected a task.updated event")
		}
	})

	// Another instance started the one-time run first: this one must say
	// nothing at all.
	t.Run("SpawnOneTimeLostIsSilent", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		mockRepo := mock_repo.NewMockRepository(ctrl)
		mockIdgen := mock_idgen.NewMockService(ctrl)
		mockPubSub := mock_pubsub.NewMockService(ctrl)
		s := New(mockRepo, mockIdgen, bus, mockPubSub)
		ch := bus.Subscribe(10, "")
		defer bus.Unsubscribe(10, "", ch)
		logs := captureLogs(t)

		task := model.Task{ID: 1, WorkspaceID: 10, UserID: 1, Status: "cron", CronSchedule: "0 9 1 1 *"}
		mockRepo.EXPECT().SystemCheckTaskExists(gomock.Any(), int64(10), int64(1), gomock.Any()).Return(false, nil).Times(2)
		mockRepo.EXPECT().SystemStartOneTimeTask(gomock.Any(), gomock.Any()).Return(false, nil)

		s.(*scheduler).spawn(context.Background(), task, spawnAt)

		if len(ch) != 0 {
			t.Errorf("expected no SSE event from the instance that lost, got %d", len(ch))
		}
		if logs.Len() != 0 {
			t.Errorf("expected nothing logged, got %s", logs.String())
		}
	})

	t.Run("SpawnOneTimeDatabaseUnavailable", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		mockRepo := mock_repo.NewMockRepository(ctrl)
		mockIdgen := mock_idgen.NewMockService(ctrl)
		mockPubSub := mock_pubsub.NewMockService(ctrl)
		s := New(mockRepo, mockIdgen, bus, mockPubSub)
		logs := captureLogs(t)

		task := model.Task{ID: 1, WorkspaceID: 10, UserID: 1, Status: "cron", CronSchedule: "0 9 1 1 *"}
		mockRepo.EXPECT().SystemCheckTaskExists(gomock.Any(), int64(10), int64(1), gomock.Any()).Return(false, nil).Times(2)
		mockRepo.EXPECT().SystemStartOneTimeTask(gomock.Any(), gomock.Any()).Return(false, errors.New("database unavailable"))

		s.(*scheduler).spawn(context.Background(), task, spawnAt)

		if !strings.Contains(logs.String(), "failed to start one-time task") {
			t.Errorf("expected the failure logged, got %s", logs.String())
		}
	})

	// A one-time run linked to an event is the template itself, so the
	// publish instruction must name the template's ID.
	t.Run("SpawnOneTimeEventNamesTemplate", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		mockRepo := mock_repo.NewMockRepository(ctrl)
		mockIdgen := mock_idgen.NewMockService(ctrl)
		mockPubSub := mock_pubsub.NewMockService(ctrl)
		s := New(mockRepo, mockIdgen, bus, mockPubSub)

		task := model.Task{ID: 1, WorkspaceID: 10, UserID: 1, Status: "cron", CronSchedule: "0 9 1 1 *", Body: "once", EventID: 77}
		mockRepo.EXPECT().SystemCheckTaskExists(gomock.Any(), int64(10), int64(1), gomock.Any()).Return(false, nil).Times(2)
		mockRepo.EXPECT().GetEvent(gomock.Any(), int64(77), int64(1)).Return(model.Event{ID: 77, UserID: 1, Name: "run_done"}, nil)
		mockRepo.EXPECT().SystemStartOneTimeTask(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, run model.Task) (bool, error) {
			want := "once" + eventinstruction.Build(eventinstruction.Params{EventName: "run_done", TaskID: monoflake.ID(1).String()})
			if run.Body != want {
				t.Errorf("expected the instruction to name the template:\ngot  %q\nwant %q", run.Body, want)
			}
			return true, nil
		})
		mockPubSub.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(&pubsub.PublishResponse{}, nil)

		s.(*scheduler).spawn(context.Background(), task, spawnAt)
	})

	// Another instance created this run first, so idx_tasks_cron_run skipped
	// the insert. That is the expected outcome of running more than one
	// instance, not a failure: no error log, no pubsub, no SSE.
	t.Run("SpawnRecurringLostIsSilent", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		mockRepo := mock_repo.NewMockRepository(ctrl)
		mockIdgen := mock_idgen.NewMockService(ctrl)
		mockPubSub := mock_pubsub.NewMockService(ctrl)
		s := New(mockRepo, mockIdgen, bus, mockPubSub)
		ch := bus.Subscribe(10, "")
		defer bus.Unsubscribe(10, "", ch)
		logs := captureLogs(t)

		task := model.Task{ID: 1, WorkspaceID: 10, UserID: 1, CronSchedule: "0 9 * * *"}
		mockRepo.EXPECT().SystemCheckTaskExists(gomock.Any(), int64(10), int64(1), gomock.Any()).Return(false, nil).Times(2)
		mockIdgen.EXPECT().NextID().Return(int64(2))
		mockRepo.EXPECT().SystemCreateCronRun(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, child model.Task) (bool, error) {
			if !child.CreatedAt.Equal(spawnAt) {
				t.Errorf("expected the child created at the scheduled minute %v, got %v", spawnAt, child.CreatedAt)
			}
			return false, nil
		})

		s.(*scheduler).spawn(context.Background(), task, spawnAt)

		if len(ch) != 0 {
			t.Errorf("expected no SSE event from the instance that lost, got %d", len(ch))
		}
		if logs.Len() != 0 {
			t.Errorf("expected nothing logged, got %s", logs.String())
		}
	})

	t.Run("SpawnRecurringDatabaseUnavailable", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		mockRepo := mock_repo.NewMockRepository(ctrl)
		mockIdgen := mock_idgen.NewMockService(ctrl)
		mockPubSub := mock_pubsub.NewMockService(ctrl)
		s := New(mockRepo, mockIdgen, bus, mockPubSub)
		logs := captureLogs(t)

		task := model.Task{ID: 1, WorkspaceID: 10, UserID: 1, CronSchedule: "0 9 * * *"}
		mockRepo.EXPECT().SystemCheckTaskExists(gomock.Any(), int64(10), int64(1), gomock.Any()).Return(false, nil).Times(2)
		mockIdgen.EXPECT().NextID().Return(int64(2))
		mockRepo.EXPECT().SystemCreateCronRun(gomock.Any(), gomock.Any()).Return(false, errors.New("database unavailable"))

		s.(*scheduler).spawn(context.Background(), task, spawnAt)

		if !strings.Contains(logs.String(), `"level":"error"`) {
			t.Errorf("expected a real failure logged as an error, got %s", logs.String())
		}
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
		mockRepo.EXPECT().SystemCreateCronRun(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, child model.Task) (bool, error) {
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
			return true, nil
		})
		mockPubSub.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(&pubsub.PublishResponse{}, nil).AnyTimes()

		s.(*scheduler).spawn(context.Background(), task, spawnAt)
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
		mockRepo.EXPECT().SystemCreateCronRun(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, child model.Task) (bool, error) {
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
			return true, nil
		})
		mockPubSub.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(&pubsub.PublishResponse{}, nil).AnyTimes()

		s.(*scheduler).spawn(context.Background(), task, spawnAt)
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
		mockRepo.EXPECT().SystemCreateCronRun(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, child model.Task) (bool, error) {
			if child.Body != "do the thing" {
				t.Errorf("expected body untouched when the event cannot be resolved, got %q", child.Body)
			}
			if child.EventID != 77 {
				t.Errorf("expected child EventID 77 even without the instruction, got %d", child.EventID)
			}
			return true, nil
		})
		mockPubSub.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(&pubsub.PublishResponse{}, nil).AnyTimes()

		s.(*scheduler).spawn(context.Background(), task, spawnAt)
	})

	// Without a pubsub the run still starts and still reaches the browser.
	t.Run("SpawnWithoutPubSub", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		mockRepo := mock_repo.NewMockRepository(ctrl)
		mockIdgen := mock_idgen.NewMockService(ctrl)
		s := New(mockRepo, mockIdgen, bus, nil)
		ch := bus.Subscribe(10, "")
		defer bus.Unsubscribe(10, "", ch)

		task := model.Task{ID: 1, WorkspaceID: 10, UserID: 1, CronSchedule: "0 9 * * *"}
		mockRepo.EXPECT().SystemCheckTaskExists(gomock.Any(), int64(10), int64(1), gomock.Any()).Return(false, nil).Times(2)
		mockIdgen.EXPECT().NextID().Return(int64(2))
		mockRepo.EXPECT().SystemCreateCronRun(gomock.Any(), gomock.Any()).Return(true, nil)

		s.(*scheduler).spawn(context.Background(), task, spawnAt)

		if len(ch) != 1 {
			t.Errorf("expected one task.created event, got %d", len(ch))
		}
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
		s.(*scheduler).spawn(context.Background(), task, spawnAt)
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
