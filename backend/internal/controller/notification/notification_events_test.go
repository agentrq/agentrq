// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package notification

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/service/memq"
	mock_memq "github.com/agentrq/agentrq/backend/internal/service/mocks/memq"
	mock_pubsub "github.com/agentrq/agentrq/backend/internal/service/mocks/pubsub"
	mock_repository "github.com/agentrq/agentrq/backend/internal/service/mocks/repository"
	mock_smtp "github.com/agentrq/agentrq/backend/internal/service/mocks/smtp"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
	"github.com/agentrq/agentrq/backend/internal/service/smtp"
	"github.com/golang/mock/gomock"
	"github.com/rs/zerolog"
	zlog "github.com/rs/zerolog/log"
)

// logLines sends every line the global logger writes to a channel, so a test
// can wait for the controller's goroutine to reach a branch that only logs.
type logLines chan string

func (l logLines) Write(p []byte) (int, error) {
	select {
	case l <- string(p):
	default:
	}
	return len(p), nil
}

func captureLogs(t *testing.T) logLines {
	t.Helper()
	lines := make(logLines, 100)
	prev := zlog.Logger
	zlog.Logger = zerolog.New(lines)
	t.Cleanup(func() { zlog.Logger = prev })
	return lines
}

func (l logLines) waitFor(t *testing.T, substr string) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case line := <-l:
			if strings.Contains(line, substr) {
				return
			}
		case <-deadline:
			t.Fatalf("no log line containing %q", substr)
		}
	}
}

func TestNew_Errors(t *testing.T) {
	t.Run("queue cannot be created", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mq := mock_memq.NewMockService(ctrl)
		mq.EXPECT().Create(gomock.Any(), memq.CreateRequest{Name: "email_notifications", Size: 1000}).Return(nil, errors.New("full"))

		c, err := New(Params{MemQ: mq})
		if err == nil || !strings.Contains(err.Error(), "failed to create email queue") {
			t.Fatalf("err = %v, want the queue error", err)
		}
		if c != nil {
			t.Error("a controller was returned with the error")
		}
	})

	t.Run("workers cannot be added", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mq := mock_memq.NewMockService(ctrl)
		mq.EXPECT().Create(gomock.Any(), gomock.Any()).Return(&memq.CreateResponse{ID: 9}, nil)
		mq.EXPECT().AddWorkers(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, req memq.AddWorkersRequest) error {
			if req.QueueID != 9 || req.Count != 2 {
				t.Errorf("AddWorkers(%d, %d), want 2 workers on queue 9", req.QueueID, req.Count)
			}
			return errors.New("no")
		})

		c, err := New(Params{MemQ: mq})
		if err == nil || !strings.Contains(err.Error(), "failed to add email workers") {
			t.Fatalf("err = %v, want the workers error", err)
		}
		if c != nil {
			t.Error("a controller was returned with the error")
		}
	})
}

func TestStart_SubscribeFails(t *testing.T) {
	ctrl := gomock.NewController(t)
	ps := mock_pubsub.NewMockService(ctrl)
	ps.EXPECT().Subscribe(gomock.Any(), gomock.Any()).Return(nil, errors.New("down"))

	c := &controller{pubsub: ps}
	if err := c.Start(context.Background()); err == nil || !strings.Contains(err.Error(), "failed to subscribe") {
		t.Fatalf("err = %v, want the subscribe error", err)
	}
}

func TestStart_SkipsForeignEventsAndStopsWhenTheTopicCloses(t *testing.T) {
	logs := captureLogs(t)
	ctrl := gomock.NewController(t)
	ps := mock_pubsub.NewMockService(ctrl)
	events := make(chan any)
	ps.EXPECT().Subscribe(gomock.Any(), pubsub.SubscribeRequest{PubSubID: entity.PubSubTopicCRUD}).Return(&pubsub.SubscribeResponse{Events: events}, nil)

	// No repository: anything that is not a CRUDEvent must be dropped before
	// it is looked at.
	c := &controller{pubsub: ps}
	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	logs.waitFor(t, "started controller")

	select {
	case events <- "not an event":
	case <-time.After(5 * time.Second):
		t.Fatal("the controller did not take the event")
	}
	logs.waitFor(t, "received invalid event type")

	close(events)
	logs.waitFor(t, "pubsub channel closed")
}

// harness wires a controller to mocks and records every email it queues.
type harness struct {
	c      *controller
	repo   *mock_repository.MockRepository
	queued []emailTask
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	ctrl := gomock.NewController(t)
	h := &harness{repo: mock_repository.NewMockRepository(ctrl)}
	mq := mock_memq.NewMockService(ctrl)
	mq.EXPECT().AddTask(gomock.Any(), gomock.Any()).AnyTimes().DoAndReturn(func(_ context.Context, req memq.AddTaskRequest) error {
		if req.QueueID != 3 {
			t.Errorf("queued on %d, want queue 3", req.QueueID)
		}
		h.queued = append(h.queued, req.Task.Val.(emailTask))
		return nil
	})
	h.repo.EXPECT().SystemGetUser(gomock.Any(), int64(100)).AnyTimes().Return(model.User{ID: 100, Email: "owner@example.com"}, nil)
	h.c = &controller{repo: h.repo, memq: mq, queueID: 3, baseURL: "https://app.example.com"}
	return h
}

const allEmail = `{"TaskCreated":true,"TaskStatusUpdated":true,"TaskReceivedMessage":true,"WorkspaceArchived":true,"WorkspaceUnarchived":true,"Channels":["email"]}`

func workspaceModel(archived bool) model.Workspace {
	w := model.Workspace{ID: 1, UserID: 100, Name: "Ops", NotificationSettings: []byte(allEmail)}
	if archived {
		now := time.Now()
		w.ArchivedAt = &now
	}
	return w
}

func (h *harness) onlySubject(t *testing.T, prefix string) {
	t.Helper()
	if len(h.queued) != 1 {
		t.Fatalf("queued %d emails, want 1: %+v", len(h.queued), h.queued)
	}
	if got := h.queued[0]; !strings.HasPrefix(got.Subject, "[AgentRQ] "+prefix) || got.To != "owner@example.com" {
		t.Errorf("queued %+v, want subject %q to owner@example.com", got, prefix)
	}
}

func TestProcessEvent_TaskEvents(t *testing.T) {
	cases := []struct {
		action  entity.Action
		allow   bool
		subject string
		body    string
	}{
		{entity.ActionTaskUpdate, false, "Task Status Updated: Deploy [Ops]", "updated to: ongoing"},
		{entity.ActionTaskComplete, false, "Task Status Updated: Deploy [Ops]", "updated to: ongoing"},
		{entity.ActionTaskAllowAllCommandsToggle, true, "Auto-Allow Commands Toggled: Deploy [Ops]", "has been turned ON"},
		{entity.ActionTaskAllowAllCommandsToggle, false, "Auto-Allow Commands Toggled: Deploy [Ops]", "has been turned OFF"},
	}
	for _, tc := range cases {
		t.Run(tc.subject+" "+tc.body, func(t *testing.T) {
			h := newHarness(t)
			h.repo.EXPECT().SystemGetTask(gomock.Any(), int64(42)).Return(model.Task{ID: 42, WorkspaceID: 1, Title: "Deploy", Status: "ongoing", AllowAllCommands: tc.allow}, nil)
			h.repo.EXPECT().SystemGetWorkspace(gomock.Any(), int64(1)).Return(workspaceModel(false), nil)

			h.c.processEvent(context.Background(), entity.CRUDEvent{Action: tc.action, ResourceType: entity.ResourceTask, ResourceID: 42})

			h.onlySubject(t, tc.subject)
			if !strings.Contains(h.queued[0].Body, tc.body) {
				t.Errorf("body %q lacks %q", h.queued[0].Body, tc.body)
			}
		})
	}

	t.Run("an action without an email sends nothing", func(t *testing.T) {
		h := newHarness(t)
		h.repo.EXPECT().SystemGetTask(gomock.Any(), int64(42)).Return(model.Task{ID: 42, WorkspaceID: 1}, nil)
		h.repo.EXPECT().SystemGetWorkspace(gomock.Any(), int64(1)).Return(workspaceModel(false), nil)
		h.c.processEvent(context.Background(), entity.CRUDEvent{Action: entity.ActionTaskDelete, ResourceType: entity.ResourceTask, ResourceID: 42})
		if len(h.queued) != 0 {
			t.Errorf("queued %+v", h.queued)
		}
	})
}

func TestProcessEvent_WorkspaceEvents(t *testing.T) {
	t.Run("archived", func(t *testing.T) {
		h := newHarness(t)
		h.repo.EXPECT().SystemGetWorkspace(gomock.Any(), int64(1)).Return(workspaceModel(true), nil)
		h.c.processEvent(context.Background(), entity.CRUDEvent{Action: entity.ActionWorkspaceUpdate, ResourceType: entity.ResourceWorkspace, ResourceID: 1})
		h.onlySubject(t, "Mission Vaulted [Ops]")
	})

	t.Run("unarchived", func(t *testing.T) {
		h := newHarness(t)
		h.repo.EXPECT().SystemGetWorkspace(gomock.Any(), int64(1)).Return(workspaceModel(false), nil)
		h.c.processEvent(context.Background(), entity.CRUDEvent{Action: entity.ActionWorkspaceUpdate, ResourceType: entity.ResourceWorkspace, ResourceID: 1})
		h.onlySubject(t, "Mission Restored [Ops]")
	})

	t.Run("created sends nothing", func(t *testing.T) {
		h := newHarness(t)
		h.repo.EXPECT().SystemGetWorkspace(gomock.Any(), int64(1)).Return(workspaceModel(false), nil)
		h.c.processEvent(context.Background(), entity.CRUDEvent{Action: entity.ActionWorkspaceCreate, ResourceType: entity.ResourceWorkspace, ResourceID: 1})
		if len(h.queued) != 0 {
			t.Errorf("queued %+v", h.queued)
		}
	})
}

func TestProcessEvent_MessageEvents(t *testing.T) {
	t.Run("agent message is emailed", func(t *testing.T) {
		h := newHarness(t)
		h.repo.EXPECT().SystemGetMessage(gomock.Any(), int64(7)).Return(model.Message{ID: 7, TaskID: 42, Sender: "agent", Text: "done, please check"}, nil)
		h.repo.EXPECT().SystemGetTask(gomock.Any(), int64(42)).Return(model.Task{ID: 42, WorkspaceID: 1, Title: "Deploy"}, nil)
		h.repo.EXPECT().SystemGetWorkspace(gomock.Any(), int64(1)).Return(workspaceModel(false), nil)

		h.c.processEvent(context.Background(), entity.CRUDEvent{Action: entity.ActionMessageCreate, ResourceType: entity.ResourceMessage, ResourceID: 7})

		h.onlySubject(t, "New Message in Task: Deploy [Ops]")
		if !strings.Contains(h.queued[0].Body, "done, please check") {
			t.Errorf("body %q lacks the message text", h.queued[0].Body)
		}
	})

	t.Run("an edited message sends nothing", func(t *testing.T) {
		h := newHarness(t)
		h.repo.EXPECT().SystemGetMessage(gomock.Any(), int64(7)).Return(model.Message{ID: 7, TaskID: 42, Sender: "agent"}, nil)
		h.repo.EXPECT().SystemGetTask(gomock.Any(), int64(42)).Return(model.Task{ID: 42, WorkspaceID: 1}, nil)
		h.repo.EXPECT().SystemGetWorkspace(gomock.Any(), int64(1)).Return(workspaceModel(false), nil)
		h.c.processEvent(context.Background(), entity.CRUDEvent{Action: entity.ActionMessageUpdate, ResourceType: entity.ResourceMessage, ResourceID: 7})
		if len(h.queued) != 0 {
			t.Errorf("queued %+v", h.queued)
		}
	})
}

func TestProcessEvent_LookupFailuresSendNothing(t *testing.T) {
	boom := errors.New("gone")
	cases := []struct {
		name  string
		event entity.CRUDEvent
		setup func(r *mock_repository.MockRepository)
	}{
		{"task missing", entity.CRUDEvent{Action: entity.ActionTaskCreate, ResourceType: entity.ResourceTask, ResourceID: 42}, func(r *mock_repository.MockRepository) {
			r.EXPECT().SystemGetTask(gomock.Any(), int64(42)).Return(model.Task{}, boom)
		}},
		{"task's workspace missing", entity.CRUDEvent{Action: entity.ActionTaskCreate, ResourceType: entity.ResourceTask, ResourceID: 42}, func(r *mock_repository.MockRepository) {
			r.EXPECT().SystemGetTask(gomock.Any(), int64(42)).Return(model.Task{ID: 42, WorkspaceID: 1}, nil)
			r.EXPECT().SystemGetWorkspace(gomock.Any(), int64(1)).Return(model.Workspace{}, boom)
		}},
		{"workspace missing", entity.CRUDEvent{Action: entity.ActionWorkspaceUpdate, ResourceType: entity.ResourceWorkspace, ResourceID: 1}, func(r *mock_repository.MockRepository) {
			r.EXPECT().SystemGetWorkspace(gomock.Any(), int64(1)).Return(model.Workspace{}, boom)
		}},
		{"message missing", entity.CRUDEvent{Action: entity.ActionMessageCreate, ResourceType: entity.ResourceMessage, ResourceID: 7}, func(r *mock_repository.MockRepository) {
			r.EXPECT().SystemGetMessage(gomock.Any(), int64(7)).Return(model.Message{}, boom)
		}},
		{"message's task missing", entity.CRUDEvent{Action: entity.ActionMessageCreate, ResourceType: entity.ResourceMessage, ResourceID: 7}, func(r *mock_repository.MockRepository) {
			r.EXPECT().SystemGetMessage(gomock.Any(), int64(7)).Return(model.Message{ID: 7, TaskID: 42}, nil)
			r.EXPECT().SystemGetTask(gomock.Any(), int64(42)).Return(model.Task{}, boom)
		}},
		{"message's workspace missing", entity.CRUDEvent{Action: entity.ActionMessageCreate, ResourceType: entity.ResourceMessage, ResourceID: 7}, func(r *mock_repository.MockRepository) {
			r.EXPECT().SystemGetMessage(gomock.Any(), int64(7)).Return(model.Message{ID: 7, TaskID: 42}, nil)
			r.EXPECT().SystemGetTask(gomock.Any(), int64(42)).Return(model.Task{ID: 42, WorkspaceID: 1}, nil)
			r.EXPECT().SystemGetWorkspace(gomock.Any(), int64(1)).Return(model.Workspace{}, boom)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			tc.setup(h.repo)
			h.c.processEvent(context.Background(), tc.event)
			if len(h.queued) != 0 {
				t.Errorf("queued %+v after a failed lookup", h.queued)
			}
		})
	}
}

func TestNotify_RespectsEachSetting(t *testing.T) {
	ws := entity.Workspace{ID: 1, UserID: 100, Name: "Ops"}
	task := entity.Task{ID: 42, Title: "Deploy"}
	cases := map[string]*entity.NotificationSettings{
		"no settings": nil,
		"all off":     {Channels: []string{"email"}},
		"email not a channel": {
			TaskCreated: true, TaskStatusUpdated: true, TaskReceivedMessage: true,
			WorkspaceArchived: true, WorkspaceUnarchived: true, Channels: []string{"push"},
		},
	}
	for name, settings := range cases {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			w := ws
			w.NotificationSettings = settings
			h.c.NotifyTaskCreated(w, task)
			h.c.NotifyTaskStatusUpdated(w, task)
			h.c.NotifyTaskAllowAllCommandsToggled(w, task)
			h.c.NotifyTaskReceivedMessage(w, task, entity.Message{Sender: "agent"})
			h.c.NotifyWorkspaceArchived(w)
			h.c.NotifyWorkspaceUnarchived(w)
			if len(h.queued) != 0 {
				t.Errorf("queued %d emails, want none: %+v", len(h.queued), h.queued)
			}
		})
	}

	if (&controller{}).hasChannel(nil, "email") {
		t.Error("nil settings reported the email channel")
	}
}

func TestEnqueueEmail_QueueFailureIsLogged(t *testing.T) {
	logs := captureLogs(t)
	ctrl := gomock.NewController(t)
	mq := mock_memq.NewMockService(ctrl)
	repo := mock_repository.NewMockRepository(ctrl)
	// The recipient decodes to some ID; when no user has it, the recipient is
	// used as given.
	repo.EXPECT().SystemGetUser(gomock.Any(), gomock.Any()).Return(model.User{}, errors.New("not found"))
	mq.EXPECT().AddTask(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, req memq.AddTaskRequest) error {
		if got := req.Task.Val.(emailTask).To; got != "someone@example.com" {
			t.Errorf("To = %q, want the recipient as given", got)
		}
		return errors.New("queue full")
	})

	c := &controller{repo: repo, memq: mq}
	c.enqueueEmail("someone@example.com", "s", "b")
	logs.waitFor(t, "failed to enqueue email")
}

func TestHandleEmailTask(t *testing.T) {
	t.Run("rejects a payload that is not an email", func(t *testing.T) {
		c := &controller{}
		if err := c.handleEmailTask(context.Background(), memq.Task{Val: "nope"}); err == nil {
			t.Fatal("a non-email payload was accepted")
		}
	})

	t.Run("passes the email to SMTP and returns its error", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		s := mock_smtp.NewMockService(ctrl)
		s.EXPECT().Send(gomock.Any(), smtp.SendRequest{To: []string{"a@example.com"}, Subject: "S", Body: "B"}).Return(errors.New("relay down"))
		c := &controller{smtp: s}
		err := c.handleEmailTask(context.Background(), memq.Task{Val: emailTask{To: "a@example.com", Subject: "S", Body: "B"}})
		if err == nil || err.Error() != "relay down" {
			t.Fatalf("err = %v, want the SMTP error", err)
		}
	})
}

func TestModelMappers(t *testing.T) {
	c := &controller{}

	t.Run("workspace", func(t *testing.T) {
		w := c.fromModelWorkspaceToEntity(model.Workspace{
			AutoAllowedTools:     []byte(`["Bash","Edit"]`),
			NotificationSettings: []byte(`not json`),
		})
		if strings.Join(w.AutoAllowedTools, ",") != "Bash,Edit" {
			t.Errorf("AutoAllowedTools = %v", w.AutoAllowedTools)
		}
		if w.NotificationSettings != nil {
			t.Errorf("unreadable settings mapped to %+v, want nil", w.NotificationSettings)
		}
	})

	t.Run("task", func(t *testing.T) {
		task := c.fromModelTaskToEntity(model.Task{Attachments: []byte(`[{"id":"a1","filename":"f.txt"}]`)})
		if len(task.Attachments) != 1 || task.Attachments[0].Filename != "f.txt" {
			t.Errorf("Attachments = %+v", task.Attachments)
		}
	})

	t.Run("message", func(t *testing.T) {
		m := c.fromModelMessageToEntity(model.Message{
			ID:          7,
			Attachments: []byte(`[{"id":"a1","mimeType":"image/png"}]`),
			Metadata:    []byte(`{"kind":"permission"}`),
		})
		if len(m.Attachments) != 1 || m.Attachments[0].MimeType != "image/png" {
			t.Errorf("Attachments = %+v", m.Attachments)
		}
		meta, ok := m.Metadata.(map[string]any)
		if !ok || meta["kind"] != "permission" {
			t.Errorf("Metadata = %#v", m.Metadata)
		}
		if empty := c.fromModelMessageToEntity(model.Message{}); empty.Metadata != nil || empty.Attachments != nil {
			t.Errorf("empty message mapped to %+v", empty)
		}
	})
}
