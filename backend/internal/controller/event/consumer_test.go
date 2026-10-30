// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package event

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
	"github.com/golang/mock/gomock"
	"github.com/mustafaturan/monoflake"
)

// waitConsumerDone waits until no goroutine of this controller is left, so a
// test proves the consumer stops rather than leaks.
func waitConsumerDone(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	buf := make([]byte, 1<<20)
	for {
		n := runtime.Stack(buf, true)
		if !strings.Contains(string(buf[:n]), "event.(*controller)") {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("event consumer goroutines still running after 5s:\n%s", buf[:n])
		}
		runtime.Gosched()
	}
}

func TestNew_WiresDependencies(t *testing.T) {
	c, mockRepo, mockPubSub, mockIDGen := newTestController(t)
	got := New(Params{Repository: mockRepo, PubSub: mockPubSub, IDGen: mockIDGen}).(*controller)
	if got.repo != c.repo || got.pubsub != c.pubsub || got.ids != c.ids {
		t.Errorf("New did not keep its dependencies: %+v", got)
	}
}

func TestStart_SubscribeFails(t *testing.T) {
	c, _, mockPubSub, _ := newTestController(t)
	mockPubSub.EXPECT().
		Subscribe(gomock.Any(), pubsub.SubscribeRequest{PubSubID: entity.PubSubTopicEvents}).
		Return(nil, errors.New("no events topic"))

	err := c.Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "no events topic") {
		t.Fatalf("Start error = %v, want the subscribe failure wrapped", err)
	}
}

// The consumer hands each published event to processEvent, ignores anything
// else on the topic, and stops when its context ends.
func TestStart_ConsumesPublishedEvents(t *testing.T) {
	c, mockRepo, mockPubSub, _ := newTestController(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := make(chan any)
	mockPubSub.EXPECT().
		Subscribe(gomock.Any(), pubsub.SubscribeRequest{PubSubID: entity.PubSubTopicEvents}).
		Return(&pubsub.SubscribeResponse{Events: events}, nil)
	mockRepo.EXPECT().SystemDeleteOrphanedEventRouting(gomock.Any()).Return(int64(0), nil)

	processed := make(chan int64, 1)
	mockRepo.EXPECT().
		SystemListEventTriggersByEventID(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, eventID int64) ([]model.EventTrigger, error) {
			processed <- eventID
			return nil, nil
		})

	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	for _, msg := range []any{"not an event", entity.EventPublishedPayload{EventID: 42}} {
		select {
		case events <- msg:
		case <-time.After(5 * time.Second):
			t.Fatalf("the consumer did not take %v", msg)
		}
	}

	select {
	case id := <-processed:
		if id != 42 {
			t.Errorf("processed event %d, want 42", id)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the published event was never processed")
	}

	cancel()
	waitConsumerDone(t)
}

func TestStart_StopsWhenTopicCloses(t *testing.T) {
	c, mockRepo, mockPubSub, _ := newTestController(t)

	events := make(chan any)
	mockPubSub.EXPECT().
		Subscribe(gomock.Any(), pubsub.SubscribeRequest{PubSubID: entity.PubSubTopicEvents}).
		Return(&pubsub.SubscribeResponse{Events: events}, nil)
	mockRepo.EXPECT().SystemDeleteOrphanedEventRouting(gomock.Any()).Return(int64(0), nil)

	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	close(events)
	waitConsumerDone(t)
}

func TestProcessEvent_WorkflowWithoutSteps(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"no steps", nil},
		{"lookup fails", errors.New("database unavailable")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, mockRepo, _, _ := newTestController(t)
			mockRepo.EXPECT().
				SystemListWorkflowStepsByEvent(gomock.Any(), int64(7), int64(42)).
				Return(nil, tc.err)
			mockRepo.EXPECT().SystemListEventTriggersByEventID(gomock.Any(), int64(42)).Return(nil, nil)

			// No workspace lookup or CreateTask expected.
			c.processEvent(context.Background(), entity.EventPublishedPayload{EventID: 42, WorkflowID: 7})
		})
	}
}

// A trigger that chains to another event tells the agent, in the task body, to
// publish it under the task's own id; when that event cannot be resolved the
// task is still created, just without the instruction.
func TestProcessEvent_TriggerChainsToAnotherEvent(t *testing.T) {
	trigger := model.EventTrigger{
		ID: 1, EventID: 42, WorkspaceID: 10, EmitEventID: 77,
		Title: "Review", Body: "Look at {{EVENT_PAYLOAD}}", Assignee: "agent",
	}

	t.Run("resolved", func(t *testing.T) {
		c, mockRepo, mockPubSub, mockIDGen := newTestController(t)
		mockRepo.EXPECT().SystemListEventTriggersByEventID(gomock.Any(), int64(42)).Return([]model.EventTrigger{trigger}, nil)
		mockRepo.EXPECT().SystemGetWorkspace(gomock.Any(), int64(10)).Return(model.Workspace{ID: 10, UserID: 100}, nil)
		mockIDGen.EXPECT().NextID().Return(int64(999))
		mockRepo.EXPECT().GetEvent(gomock.Any(), int64(77), int64(100)).
			Return(model.Event{ID: 77, Name: "review_done", PayloadGuidelines: "say what changed"}, nil)

		var got model.Task
		mockRepo.EXPECT().CreateTask(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, task model.Task) (model.Task, error) {
				got = task
				return task, nil
			})
		mockPubSub.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(&pubsub.PublishResponse{}, nil)

		c.processEvent(context.Background(), entity.EventPublishedPayload{EventID: 42, Payload: "PR 12"})

		if !strings.HasPrefix(got.Body, "Look at PR 12") {
			t.Errorf("body = %q, want the rendered template first", got.Body)
		}
		for _, want := range []string{`"publishEvent"`, `name: "review_done"`, `taskId: "` + monoflake.ID(999).String() + `"`, "say what changed"} {
			if !strings.Contains(got.Body, want) {
				t.Errorf("body is missing %q:\n%s", want, got.Body)
			}
		}
		if got.EventID != 77 {
			t.Errorf("task EventID = %d, want 77", got.EventID)
		}
	})

	t.Run("unresolved", func(t *testing.T) {
		c, mockRepo, mockPubSub, mockIDGen := newTestController(t)
		mockRepo.EXPECT().SystemListEventTriggersByEventID(gomock.Any(), int64(42)).Return([]model.EventTrigger{trigger}, nil)
		mockRepo.EXPECT().SystemGetWorkspace(gomock.Any(), int64(10)).Return(model.Workspace{ID: 10, UserID: 100}, nil)
		mockIDGen.EXPECT().NextID().Return(int64(999))
		mockRepo.EXPECT().GetEvent(gomock.Any(), int64(77), int64(100)).Return(model.Event{}, base.ErrNotFound)

		var got model.Task
		mockRepo.EXPECT().CreateTask(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, task model.Task) (model.Task, error) {
				got = task
				return task, nil
			})
		mockPubSub.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(&pubsub.PublishResponse{}, nil)

		c.processEvent(context.Background(), entity.EventPublishedPayload{EventID: 42, Payload: "PR 12"})

		if got.Body != "Look at PR 12" {
			t.Errorf("body = %q, want only the rendered template", got.Body)
		}
	})
}

// A task that fails to save is not announced on the CRUD topic.
func TestProcessEvent_TriggerTaskCreateFails(t *testing.T) {
	c, mockRepo, _, mockIDGen := newTestController(t)
	mockRepo.EXPECT().SystemListEventTriggersByEventID(gomock.Any(), int64(42)).
		Return([]model.EventTrigger{{ID: 1, EventID: 42, WorkspaceID: 10, Title: "t"}}, nil)
	mockRepo.EXPECT().SystemGetWorkspace(gomock.Any(), int64(10)).Return(model.Workspace{ID: 10, UserID: 100}, nil)
	mockIDGen.EXPECT().NextID().Return(int64(999))
	mockRepo.EXPECT().CreateTask(gomock.Any(), gomock.Any()).Return(model.Task{}, errors.New("disk full"))

	// No Publish expected.
	c.processEvent(context.Background(), entity.EventPublishedPayload{EventID: 42})
}

func TestProcessEvent_WorkflowStepEmptyTitleSkipped(t *testing.T) {
	c, mockRepo, _, _ := newTestController(t)
	mockRepo.EXPECT().SystemListWorkflowStepsByEvent(gomock.Any(), int64(7), int64(42)).
		Return([]model.WorkflowStep{{ID: 1, WorkflowID: 7, EventID: 42, WorkspaceID: 10, Title: "   "}}, nil)
	mockRepo.EXPECT().SystemListEventTriggersByEventID(gomock.Any(), int64(42)).Return(nil, nil)

	// No workspace lookup or CreateTask expected.
	c.processEvent(context.Background(), entity.EventPublishedPayload{EventID: 42, WorkflowID: 7})
}

// A workflow step that chains onward arms its task to publish the next event,
// named for the task so the run stays identifiable.
func TestProcessEvent_WorkflowStepChainsToNextEvent(t *testing.T) {
	step := model.WorkflowStep{
		ID: 1, WorkflowID: 7, EventID: 42, WorkspaceID: 10, EmitEventID: 88,
		Title: "Build", Body: "Build {{EVENT_PAYLOAD}}", Assignee: "agent",
	}

	t.Run("resolved", func(t *testing.T) {
		c, mockRepo, mockPubSub, mockIDGen := newTestController(t)
		mockRepo.EXPECT().SystemListWorkflowStepsByEvent(gomock.Any(), int64(7), int64(42)).Return([]model.WorkflowStep{step}, nil)
		mockRepo.EXPECT().SystemListEventTriggersByEventID(gomock.Any(), int64(42)).Return(nil, nil)
		mockRepo.EXPECT().SystemGetWorkspace(gomock.Any(), int64(10)).Return(model.Workspace{ID: 10, UserID: 100}, nil)
		mockIDGen.EXPECT().NextID().Return(int64(555))
		mockRepo.EXPECT().GetEvent(gomock.Any(), int64(88), int64(100)).Return(model.Event{ID: 88, Name: "built"}, nil)

		var got model.Task
		mockRepo.EXPECT().CreateTask(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, task model.Task) (model.Task, error) {
				got = task
				return task, nil
			})
		mockPubSub.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(&pubsub.PublishResponse{}, nil)

		c.processEvent(context.Background(), entity.EventPublishedPayload{EventID: 42, WorkflowID: 7, Depth: 3, Payload: "main"})

		for _, want := range []string{"Build main", `name: "built"`, `taskId: "` + monoflake.ID(555).String() + `"`} {
			if !strings.Contains(got.Body, want) {
				t.Errorf("body is missing %q:\n%s", want, got.Body)
			}
		}
		if got.EventID != 88 || got.WorkflowID != 7 || got.WorkflowDepth != 4 {
			t.Errorf("task EventID/WorkflowID/WorkflowDepth = %d/%d/%d, want 88/7/4", got.EventID, got.WorkflowID, got.WorkflowDepth)
		}
	})

	t.Run("unresolved", func(t *testing.T) {
		c, mockRepo, mockPubSub, mockIDGen := newTestController(t)
		mockRepo.EXPECT().SystemListWorkflowStepsByEvent(gomock.Any(), int64(7), int64(42)).Return([]model.WorkflowStep{step}, nil)
		mockRepo.EXPECT().SystemListEventTriggersByEventID(gomock.Any(), int64(42)).Return(nil, nil)
		mockRepo.EXPECT().SystemGetWorkspace(gomock.Any(), int64(10)).Return(model.Workspace{ID: 10, UserID: 100}, nil)
		mockIDGen.EXPECT().NextID().Return(int64(555))
		mockRepo.EXPECT().GetEvent(gomock.Any(), int64(88), int64(100)).Return(model.Event{}, base.ErrNotFound)

		var got model.Task
		mockRepo.EXPECT().CreateTask(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, task model.Task) (model.Task, error) {
				got = task
				return task, nil
			})
		mockPubSub.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(&pubsub.PublishResponse{}, nil)

		c.processEvent(context.Background(), entity.EventPublishedPayload{EventID: 42, WorkflowID: 7, Payload: "main"})

		if got.Body != "Build main" {
			t.Errorf("body = %q, want only the rendered template", got.Body)
		}
	})
}

func TestProcessEvent_WorkflowStepTaskCreateFails(t *testing.T) {
	c, mockRepo, _, mockIDGen := newTestController(t)
	mockRepo.EXPECT().SystemListWorkflowStepsByEvent(gomock.Any(), int64(7), int64(42)).
		Return([]model.WorkflowStep{{ID: 1, WorkflowID: 7, EventID: 42, WorkspaceID: 10, Title: "step"}}, nil)
	mockRepo.EXPECT().SystemListEventTriggersByEventID(gomock.Any(), int64(42)).Return(nil, nil)
	mockRepo.EXPECT().SystemGetWorkspace(gomock.Any(), int64(10)).Return(model.Workspace{ID: 10, UserID: 100}, nil)
	mockIDGen.EXPECT().NextID().Return(int64(555))
	mockRepo.EXPECT().CreateTask(gomock.Any(), gomock.Any()).Return(model.Task{}, errors.New("disk full"))

	// No Publish expected.
	c.processEvent(context.Background(), entity.EventPublishedPayload{EventID: 42, WorkflowID: 7})
}
