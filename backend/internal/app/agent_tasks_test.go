// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/mustafaturan/monoflake"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
	mock_idgen "github.com/agentrq/agentrq/backend/internal/service/mocks/idgen"
	mock_pubsub "github.com/agentrq/agentrq/backend/internal/service/mocks/pubsub"
	mock_repository "github.com/agentrq/agentrq/backend/internal/service/mocks/repository"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
	"github.com/agentrq/agentrq/backend/internal/service/storage"
)

type agentTasksEnv struct {
	a      agentTasks
	repo   *mock_repository.MockRepository
	ids    *mock_idgen.MockService
	stream chan []byte
	// actions are the CRUD events published, in order.
	actions *[]entity.Action
}

func newAgentTasks(t *testing.T) agentTasksEnv {
	t.Helper()
	ctrl := gomock.NewController(t)
	repo := mock_repository.NewMockRepository(ctrl)
	ids := mock_idgen.NewMockService(ctrl)
	ps := mock_pubsub.NewMockService(ctrl)
	actions := &[]entity.Action{}
	ps.EXPECT().Publish(gomock.Any(), gomock.Any()).AnyTimes().DoAndReturn(func(_ context.Context, req pubsub.PublishRequest) (*pubsub.PublishResponse, error) {
		*actions = append(*actions, req.Event.(entity.CRUDEvent).Action)
		return &pubsub.PublishResponse{}, nil
	})
	store, err := storage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	bus := eventbus.New()
	owner := monoflake.ID(memUser).String()
	return agentTasksEnv{
		a:       agentTasks{repo: repo, pubsub: ps, bus: bus, storage: store, ids: ids, workspaceID: memParent, contentID: memParent, owner: owner},
		repo:    repo,
		ids:     ids,
		stream:  bus.Subscribe(memParent, owner),
		actions: actions,
	}
}

func TestAgentTasksReply(t *testing.T) {
	ctx := context.Background()
	chatID := monoflake.ID(101).String()
	row := func(status string) model.Task {
		return model.Task{ID: 101, WorkspaceID: memParent, UserID: memUser, Status: status}
	}

	t.Run("the first reply starts the task, naming only its status", func(t *testing.T) {
		e := newAgentTasks(t)
		e.repo.EXPECT().SystemGetTask(ctx, int64(101)).Return(row("notstarted"), nil)
		e.ids.EXPECT().NextID().Return(int64(7))
		e.ids.EXPECT().NextID().Return(int64(8))
		e.repo.EXPECT().UpdateTask(ctx, gomock.Any(), "Status").DoAndReturn(func(_ context.Context, m model.Task, _ ...string) (model.Task, error) {
			if m.Status != "ongoing" {
				t.Errorf("status = %q, want ongoing", m.Status)
			}
			return m, nil
		})
		e.repo.EXPECT().CreateMessage(ctx, gomock.Any()).DoAndReturn(func(_ context.Context, msg model.Message) error {
			if msg.ID != 8 || msg.Sender != "agent" || msg.Text != "done" || string(msg.Metadata) != `{"k":"v"}` {
				t.Errorf("message = %+v", msg)
			}
			var atts []entity.Attachment
			if err := json.Unmarshal(msg.Attachments, &atts); err != nil || len(atts) != 1 || atts[0].Data != "" {
				t.Errorf("attachments = %s, %v; want one, stored and without its data", msg.Attachments, err)
			}
			return nil
		})
		e.repo.EXPECT().GetTask(ctx, memParent, int64(101), memUser).Return(row("ongoing"), nil)

		id, err := e.a.reply(ctx, chatID, "done", []entity.Attachment{{Filename: "a.txt", MimeType: "text/plain", Data: "aGk="}}, map[string]string{"k": "v"})
		if err != nil || id != 8 {
			t.Fatalf("reply = %d, %v", id, err)
		}
		if want := []entity.Action{entity.ActionMessageCreate}; !equalActions(*e.actions, want) {
			t.Errorf("published %v, want %v", *e.actions, want)
		}
		select {
		case <-e.stream:
		default:
			t.Error("the task was not put on the stream")
		}
	})

	t.Run("a started task keeps its status", func(t *testing.T) {
		e := newAgentTasks(t)
		e.repo.EXPECT().SystemGetTask(ctx, int64(101)).Return(row("ongoing"), nil)
		e.ids.EXPECT().NextID().Return(int64(8))
		e.repo.EXPECT().CreateMessage(ctx, gomock.Any()).DoAndReturn(func(_ context.Context, msg model.Message) error {
			if msg.Metadata != nil {
				t.Errorf("metadata = %s, want none for a value JSON cannot hold", msg.Metadata)
			}
			return nil
		})
		e.repo.EXPECT().GetTask(ctx, memParent, int64(101), memUser).Return(model.Task{}, errRepo)

		if _, err := e.a.reply(ctx, chatID, "still going", nil, make(chan int)); err != nil {
			t.Fatal(err)
		}
		select {
		case line := <-e.stream:
			t.Errorf("unexpected event without a task to send: %s", line)
		default:
		}
	})

	t.Run("refusals and storage failures", func(t *testing.T) {
		e := newAgentTasks(t)
		if _, err := e.a.reply(ctx, "", "x", nil, nil); err == nil {
			t.Error("an empty chat id was taken")
		}

		e.repo.EXPECT().SystemGetTask(ctx, int64(101)).Return(model.Task{ID: 101, WorkspaceID: memFork, UserID: memUser}, nil)
		if _, err := e.a.reply(ctx, chatID, "x", nil, nil); err == nil {
			t.Error("a task in another workspace took a reply")
		}

		e.repo.EXPECT().SystemGetTask(ctx, int64(101)).Return(row("ongoing"), nil)
		e.ids.EXPECT().NextID().Return(int64(8))
		e.repo.EXPECT().CreateMessage(ctx, gomock.Any()).Return(errRepo)
		if _, err := e.a.reply(ctx, chatID, "x", nil, nil); err != errRepo {
			t.Errorf("err = %v, want %v", err, errRepo)
		}
		if len(*e.actions) != 0 {
			t.Errorf("published %v, want nothing", *e.actions)
		}
	})
}

func equalActions(got, want []entity.Action) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
