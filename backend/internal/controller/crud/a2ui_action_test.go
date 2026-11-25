// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package crud

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	mock_idgen "github.com/agentrq/agentrq/backend/internal/service/mocks/idgen"
	mock_pubsub "github.com/agentrq/agentrq/backend/internal/service/mocks/pubsub"
	mock_repo "github.com/agentrq/agentrq/backend/internal/service/mocks/repository"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
	"github.com/golang/mock/gomock"
	"github.com/mustafaturan/monoflake"
	"gorm.io/datatypes"
)

// a2uiCardMetadata is a stored card with two Buttons; ok is redefined by the
// second updateComponents, as the page draws it.
const a2uiCardMetadata = `{"type":"a2ui","version":"1.0","surfaceId":"deploy","messages":[
 {"version":"1.0","createSurface":{"surfaceId":"deploy","catalogId":"x"}},
 {"version":"1.0","updateComponents":{"surfaceId":"deploy","components":[
  {"id":"root","component":"Row","children":["ok","no","info"]},
  {"id":"ok","component":"Button","child":"okLabel","action":{"event":{"name":"old"}}},
  {"id":"okLabel","component":"Text","text":"Approve"},
  {"id":"no","component":"Button","child":"noLabel","action":{"event":{"name":"reject","userMessage":" Not now "}}},
  {"id":"noLabel","component":"Text","text":"Reject"},
  {"id":"info","component":"Text","text":"Deploy v2?"}
 ]}},
 {"version":"1.0","updateComponents":{"surfaceId":"deploy","components":[
  {"id":"ok","component":"Button","child":"okLabel","action":{"event":{"name":"approve"}}}
 ]}}
]}`

type a2uiActionEnv struct {
	c      Controller
	repo   *mock_repo.MockRepository
	idgen  *mock_idgen.MockService
	events []entity.CRUDEvent
	// afterAnswer is what the store returns once answer has run.
	afterAnswer error
}

func newA2uiActionEnv(t *testing.T) *a2uiActionEnv {
	t.Helper()
	ctrl := gomock.NewController(t)
	e := &a2uiActionEnv{repo: mock_repo.NewMockRepository(ctrl), idgen: mock_idgen.NewMockService(ctrl)}
	bus := mock_pubsub.NewMockService(ctrl)
	bus.EXPECT().Publish(gomock.Any(), gomock.Any()).AnyTimes().DoAndReturn(func(_ context.Context, req pubsub.PublishRequest) (*pubsub.PublishResponse, error) {
		if ev, ok := req.Event.(entity.CRUDEvent); ok {
			e.events = append(e.events, ev)
		}
		return &pubsub.PublishResponse{}, nil
	})
	e.c = New(Params{IDGen: e.idgen, Repository: e.repo, PubSub: bus})
	return e
}

func a2uiActionRequest(component, name string) entity.SendA2uiActionRequest {
	return entity.SendA2uiActionRequest{WorkspaceID: 1, TaskID: 10, UserID: testUserIDStr, MessageID: 500,
		SurfaceID: "deploy", Name: name, SourceComponentID: component, Context: map[string]any{"env": "prod"}}
}

// expectCard expects the workspace and task reads, then the answer to
// message 500 of task 10 holding metadata, which answer is run against
// unless the store fails with storeErr first. It returns what was answered.
func (e *a2uiActionEnv) expectCard(metadata string, storeErr error) *a2uiAnswer {
	e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(activeWorkspace(), nil)
	e.repo.EXPECT().GetTaskRow(gomock.Any(), int64(1), int64(10), testUserID).Return(model.Task{ID: 10, WorkspaceID: 1, Status: "blocked"}, nil)
	got := &a2uiAnswer{}
	e.repo.EXPECT().AnswerCard(gomock.Any(), int64(10), int64(500), gomock.Any()).DoAndReturn(
		func(_ context.Context, _, _ int64, answer func(model.Message) (model.Message, []byte, error)) error {
			if storeErr != nil {
				return storeErr
			}
			reply, answered, err := answer(model.Message{ID: 500, TaskID: 10, Sender: "agent", Metadata: datatypes.JSON(metadata)})
			if err != nil {
				return err
			}
			got.reply, got.answered = reply, answered
			return e.afterAnswer
		})
	return got
}

type a2uiAnswer struct {
	reply    model.Message
	answered []byte
}

func TestSendA2uiAction_PostsTheClickAsThePersonsReply(t *testing.T) {
	e := newA2uiActionEnv(t)
	got := e.expectCard(a2uiCardMetadata, nil)
	e.idgen.EXPECT().NextID().Return(int64(600))
	e.repo.EXPECT().UpdateTask(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, m model.Task, _ ...string) (model.Task, error) {
		if m.Status != "ongoing" {
			t.Errorf("status = %q, want the blocked task resumed", m.Status)
		}
		return m, nil
	})
	e.repo.EXPECT().GetTask(gomock.Any(), int64(1), int64(10), testUserID).Return(model.Task{ID: 10, WorkspaceID: 1}, nil)

	rs, err := e.c.SendA2uiAction(context.Background(), a2uiActionRequest("ok", "approve"))
	if err != nil {
		t.Fatal(err)
	}
	if rs.Text != "Approve" || rs.MessageID != 600 || rs.Task.ID != 10 {
		t.Errorf("response = %+v, want the label posted as message 600", rs)
	}
	posted := got.reply
	if posted.ID != 600 || posted.Sender != "human" || posted.Text != "Approve" || posted.TaskID != 10 || posted.UserID != testUserID {
		t.Errorf("posted = %+v, want the person's reply", posted)
	}
	var meta map[string]any
	_ = json.Unmarshal(posted.Metadata, &meta)
	if _, typed := meta["type"]; typed {
		t.Errorf("metadata %s has a type, which reads as no conversation", posted.Metadata)
	}
	action, _ := meta["a2uiAction"].(map[string]any)
	if action["messageId"] != monoflake.ID(500).String() || action["surfaceId"] != "deploy" || action["name"] != "approve" ||
		action["context"].(map[string]any)["env"] != "prod" {
		t.Errorf("metadata = %s", posted.Metadata)
	}
	var mark map[string]any
	_ = json.Unmarshal(got.answered, &mark)
	if len(mark) != 2 || mark["name"] != "approve" || mark["at"] == nil {
		t.Errorf("answered = %s, want approve and its time", got.answered)
	}
	var actions []entity.Action
	for _, ev := range e.events {
		actions = append(actions, ev.Action)
	}
	want := []entity.Action{entity.ActionMessageUpdate, entity.ActionMessageCreate, entity.ActionTaskUpdate, entity.ActionA2UIAction}
	if len(actions) != len(want) {
		t.Fatalf("events = %v, want %v", actions, want)
	}
	for i := range want {
		if actions[i] != want[i] {
			t.Fatalf("events = %v, want %v", actions, want)
		}
	}
	if last := e.events[3]; last.ResourceID != 600 || last.WorkspaceID != 1 || last.UserID != testUserID || last.Actor != entity.ActorHuman {
		t.Errorf("a2ui_action = %+v, want message 600 by the person", last)
	}
}

func TestSendA2uiAction_TheReplysText(t *testing.T) {
	noLabel := strings.Replace(a2uiCardMetadata, `{"id":"okLabel","component":"Text","text":"Approve"}`, `{"id":"okLabel","component":"Icon","name":"check"}`, 1)
	for _, tc := range []struct {
		name, component, action, userMessage, card, want string
	}{
		{"what the page resolved", "ok", "approve", " Ship it ", a2uiCardMetadata, "Ship it"},
		{"the Button's own userMessage", "no", "reject", "", a2uiCardMetadata, "Not now"},
		{"its label", "ok", "approve", "", a2uiCardMetadata, "Approve"},
		{"the action's name, with no Text label", "ok", "approve", "", noLabel, "approve"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newA2uiActionEnv(t)
			e.expectCard(tc.card, nil)
			e.idgen.EXPECT().NextID().Return(int64(600))
			e.repo.EXPECT().UpdateTask(gomock.Any(), gomock.Any(), gomock.Any()).Return(model.Task{ID: 10, WorkspaceID: 1}, nil)
			e.repo.EXPECT().GetTask(gomock.Any(), int64(1), int64(10), testUserID).Return(model.Task{}, errDBDown)
			req := a2uiActionRequest(tc.component, tc.action)
			req.UserMessage = tc.userMessage
			rs, err := e.c.SendA2uiAction(context.Background(), req)
			if err != nil {
				t.Fatal(err)
			}
			if rs.Text != tc.want {
				t.Errorf("text = %q, want %q", rs.Text, tc.want)
			}
			if rs.Task.ID != 10 {
				t.Errorf("task = %+v, want the updated task when the reread fails", rs.Task)
			}
		})
	}
}

func TestSendA2uiAction_Refused(t *testing.T) {
	answered := strings.Replace(a2uiCardMetadata, `"surfaceId":"deploy","messages"`, `"surfaceId":"deploy","answered":{"name":"reject"},"messages"`, 1)
	for _, tc := range []struct {
		name      string
		card      string
		req       entity.SendA2uiActionRequest
		wantError string
		conflict  bool
	}{
		{"a message with no card", `{"type":"usage"}`, a2uiActionRequest("ok", "approve"), "carries no card", false},
		{"a message with no metadata", ``, a2uiActionRequest("ok", "approve"), "carries no card", false},
		{"another surface", strings.Replace(a2uiCardMetadata, `"surfaceId":"deploy","messages"`, `"surfaceId":"other","messages"`, 1), a2uiActionRequest("ok", "approve"), "is not surface", false},
		{"no such component", a2uiCardMetadata, a2uiActionRequest("nope", "approve"), "has no Button", false},
		{"a component that is no Button", a2uiCardMetadata, a2uiActionRequest("info", "approve"), "has no Button", false},
		{"an action the Button no longer has", a2uiCardMetadata, a2uiActionRequest("ok", "old"), "has no Button", false},
		{"already answered", answered, a2uiActionRequest("ok", "approve"), "already answered", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newA2uiActionEnv(t)
			e.expectCard(tc.card, nil)
			_, err := e.c.SendA2uiAction(context.Background(), tc.req)
			var refused *entity.A2uiActionError
			if !errors.As(err, &refused) || !strings.Contains(refused.Message, tc.wantError) || refused.Conflict != tc.conflict {
				t.Fatalf("err = %#v, want %q (conflict %v)", err, tc.wantError, tc.conflict)
			}
			if len(e.events) != 0 {
				t.Errorf("events = %+v, want none", e.events)
			}
		})
	}
}

// Another task's message, or one since deleted, is not found by the store.
func TestSendA2uiAction_AMessageTheTaskDoesNotHave(t *testing.T) {
	e := newA2uiActionEnv(t)
	e.expectCard(a2uiCardMetadata, base.ErrNotFound)
	_, err := e.c.SendA2uiAction(context.Background(), a2uiActionRequest("ok", "approve"))
	wantErrIs(t, "SendA2uiAction", err, base.ErrNotFound)
	if len(e.events) != 0 {
		t.Errorf("events = %+v, want none", e.events)
	}
}

func TestSendA2uiAction_Failures(t *testing.T) {
	req := a2uiActionRequest("ok", "approve")
	t.Run("rate limited", func(t *testing.T) {
		e := newA2uiActionEnv(t)
		e.c.(*controller).limiter = &denyMessageLimiter{}
		_, err := e.c.SendA2uiAction(context.Background(), req)
		wantErrText(t, "SendA2uiAction", err, "rate limit exceeded")
	})
	t.Run("workspace not found", func(t *testing.T) {
		e := newA2uiActionEnv(t)
		e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(model.Workspace{}, base.ErrNotFound)
		_, err := e.c.SendA2uiAction(context.Background(), req)
		wantErrIs(t, "SendA2uiAction", err, base.ErrNotFound)
	})
	t.Run("task not found", func(t *testing.T) {
		e := newA2uiActionEnv(t)
		e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(activeWorkspace(), nil)
		e.repo.EXPECT().GetTaskRow(gomock.Any(), int64(1), int64(10), testUserID).Return(model.Task{}, base.ErrNotFound)
		_, err := e.c.SendA2uiAction(context.Background(), req)
		wantErrIs(t, "SendA2uiAction", err, base.ErrNotFound)
	})
	t.Run("the card cannot be read", func(t *testing.T) {
		e := newA2uiActionEnv(t)
		e.expectCard(a2uiCardMetadata, errDBDown)
		_, err := e.c.SendA2uiAction(context.Background(), req)
		wantErrIs(t, "SendA2uiAction", err, errDBDown)
	})
	t.Run("the answer and its reply are not stored", func(t *testing.T) {
		e := newA2uiActionEnv(t)
		e.afterAnswer = errDBDown
		e.expectCard(a2uiCardMetadata, nil)
		e.idgen.EXPECT().NextID().Return(int64(600))
		_, err := e.c.SendA2uiAction(context.Background(), req)
		wantErrIs(t, "SendA2uiAction", err, errDBDown)
		if len(e.events) != 0 {
			t.Errorf("events = %+v, want none for a click that was not stored", e.events)
		}
	})
	t.Run("the task is not resumed", func(t *testing.T) {
		e := newA2uiActionEnv(t)
		e.expectCard(a2uiCardMetadata, nil)
		e.idgen.EXPECT().NextID().Return(int64(600))
		e.repo.EXPECT().UpdateTask(gomock.Any(), gomock.Any(), gomock.Any()).Return(model.Task{}, errDBDown)
		_, err := e.c.SendA2uiAction(context.Background(), req)
		wantErrIs(t, "SendA2uiAction", err, errDBDown)
	})
}
