// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package push

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	mock_idgen "github.com/agentrq/agentrq/backend/internal/service/mocks/idgen"
	mock_pubsub "github.com/agentrq/agentrq/backend/internal/service/mocks/pubsub"
	mock_repo "github.com/agentrq/agentrq/backend/internal/service/mocks/repository"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
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

func TestNew_WiresParams(t *testing.T) {
	ctrl := gomock.NewController(t)
	repo := mock_repo.NewMockRepository(ctrl)
	ps := mock_pubsub.NewMockService(ctrl)
	ids := mock_idgen.NewMockService(ctrl)
	cfg := Config{VAPIDPublicKey: "pub", VAPIDPrivateKey: "priv", Subscriber: "mailto:a@example.com"}

	c, ok := New(Params{Config: cfg, Repository: repo, PubSub: ps, IDGen: ids, BasePath: "/base"}).(*controller)
	if !ok {
		t.Fatal("New did not return the controller")
	}
	if c.cfg != cfg || c.repo != repo || c.pubsub != ps || c.ids != ids || c.basePath != "/base" {
		t.Errorf("controller = %+v, want every param wired", c)
	}
}

func TestStart_DisabledWithoutVAPIDKeys(t *testing.T) {
	// No pubsub: a disabled controller must not subscribe at all.
	c := &controller{cfg: Config{VAPIDPublicKey: "pub"}}
	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start = %v, want nil when push is disabled", err)
	}
}

func TestStart_SubscribeFails(t *testing.T) {
	ctrl := gomock.NewController(t)
	ps := mock_pubsub.NewMockService(ctrl)
	ps.EXPECT().Subscribe(gomock.Any(), gomock.Any()).Return(nil, errors.New("down"))

	c := &controller{cfg: Config{VAPIDPublicKey: "pub", VAPIDPrivateKey: "priv"}, pubsub: ps}
	if err := c.Start(context.Background()); err == nil || !strings.Contains(err.Error(), "failed to subscribe to CRUD topic") {
		t.Fatalf("err = %v, want the subscribe error", err)
	}
}

func TestStart_ProcessesEventsUntilTheTopicCloses(t *testing.T) {
	logs := captureLogs(t)
	ctrl := gomock.NewController(t)
	ps := mock_pubsub.NewMockService(ctrl)
	repo := mock_repo.NewMockRepository(ctrl)
	events := make(chan any)
	ps.EXPECT().Subscribe(gomock.Any(), pubsub.SubscribeRequest{PubSubID: entity.PubSubTopicCRUD}).Return(&pubsub.SubscribeResponse{Events: events}, nil)

	looked := make(chan int64, 1)
	repo.EXPECT().SystemGetWorkspace(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, id int64) (model.Workspace, error) {
		looked <- id
		return model.Workspace{}, errors.New("gone")
	})

	c := &controller{cfg: Config{VAPIDPublicKey: "pub", VAPIDPrivateKey: "priv"}, pubsub: ps, repo: repo}
	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	send := func(v any) {
		t.Helper()
		select {
		case events <- v:
		case <-time.After(5 * time.Second):
			t.Fatal("the controller did not take the event")
		}
	}
	// Something that is not a CRUD event is skipped; the next one is handled.
	send("not an event")
	send(entity.CRUDEvent{WorkspaceID: 12, ResourceType: entity.ResourceTask})
	select {
	case id := <-looked:
		if id != 12 {
			t.Errorf("looked up workspace %d, want 12", id)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the event was not processed")
	}

	close(events)
	logs.waitFor(t, "pubsub channel closed")
}

func TestStart_StopsWithItsContext(t *testing.T) {
	ctrl := gomock.NewController(t)
	ps := mock_pubsub.NewMockService(ctrl)
	// A nil events channel is never ready, so the only way out of the loop is
	// the context, which is already done. No repository: no event is handled.
	ps.EXPECT().Subscribe(gomock.Any(), gomock.Any()).Return(&pubsub.SubscribeResponse{}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c := &controller{cfg: Config{VAPIDPublicKey: "pub", VAPIDPrivateKey: "priv"}, pubsub: ps}
	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
}

func TestProcessEvent_StopsWhenALookupFails(t *testing.T) {
	ws := model.Workspace{ID: 10, UserID: 1, Name: "ws"}
	boom := errors.New("gone")
	cases := []struct {
		name  string
		event entity.CRUDEvent
		setup func(r *mock_repo.MockRepository)
	}{
		{"task missing", entity.CRUDEvent{ResourceType: entity.ResourceTask, ResourceID: 5, Action: entity.ActionTaskCreate, Actor: entity.ActorAgent}, func(r *mock_repo.MockRepository) {
			r.EXPECT().SystemGetTask(gomock.Any(), int64(5)).Return(model.Task{}, boom)
		}},
		{"message edited, not created", entity.CRUDEvent{ResourceType: entity.ResourceMessage, ResourceID: 8, Action: entity.ActionMessageUpdate}, func(*mock_repo.MockRepository) {}},
		{"message missing", entity.CRUDEvent{ResourceType: entity.ResourceMessage, ResourceID: 8, Action: entity.ActionMessageCreate}, func(r *mock_repo.MockRepository) {
			r.EXPECT().SystemGetMessage(gomock.Any(), int64(8)).Return(model.Message{}, boom)
		}},
		{"message's task missing", entity.CRUDEvent{ResourceType: entity.ResourceMessage, ResourceID: 8, Action: entity.ActionMessageCreate}, func(r *mock_repo.MockRepository) {
			r.EXPECT().SystemGetMessage(gomock.Any(), int64(8)).Return(model.Message{ID: 8, TaskID: 5, Sender: "agent"}, nil)
			r.EXPECT().SystemGetTask(gomock.Any(), int64(5)).Return(model.Task{}, boom)
		}},
		{"workspace event", entity.CRUDEvent{ResourceType: entity.ResourceWorkspace, ResourceID: 10, Action: entity.ActionWorkspaceUpdate}, func(*mock_repo.MockRepository) {}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// No ListPushSubscriptions expectation: nothing may be sent.
			c, repo, _ := newTestController(t, Config{VAPIDPublicKey: "pub", VAPIDPrivateKey: "priv"})
			repo.EXPECT().SystemGetWorkspace(gomock.Any(), int64(10)).Return(ws, nil)
			tc.setup(repo)
			tc.event.WorkspaceID = 10
			c.processEvent(context.Background(), tc.event)
		})
	}
}

func TestSendToUser_ListErrorSendsNothing(t *testing.T) {
	c, repo, _ := newTestController(t, Config{VAPIDPublicKey: "pub", VAPIDPrivateKey: "priv"})
	repo.EXPECT().ListPushSubscriptionsByUserAndWorkspace(gomock.Any(), int64(1), int64(10)).Return(nil, errors.New("db down"))
	// The fallback is not consulted when the first lookup fails.
	c.sendToUser(context.Background(), 1, 10, 4, PushTypeTaskCreate, pushPayload{Title: "hi"})
}

func TestSendToUser_ABrokenSubscriptionDoesNotStopTheRest(t *testing.T) {
	logs := captureLogs(t)
	delivered := make(chan []byte, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		delivered <- body
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	c, repo := newPushTestController(t)
	p256dh, auth := newTestSubscriptionKeys(t)
	repo.EXPECT().ListPushSubscriptionsByUserAndWorkspace(gomock.Any(), int64(7), int64(3)).Return([]model.PushSubscription{
		// Keys a browser could never have produced: encryption fails before
		// anything is sent.
		{Endpoint: "https://push.example.com/broken", P256dh: "not-a-key", Auth: "x"},
		{Endpoint: srv.URL, P256dh: p256dh, Auth: auth},
	}, nil)

	c.sendToUser(context.Background(), 7, 3, 0, PushTypeTaskUpdate, pushPayload{Title: "hi"})

	logs.waitFor(t, "failed to send notification")
	select {
	case body := <-delivered:
		if len(body) == 0 {
			t.Error("the working subscription got an empty push")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the working subscription got nothing")
	}
}

func TestPushPayload_JSONShape(t *testing.T) {
	data, err := json.Marshal(pushPayload{Title: "t", Body: "b", URL: "/u", Tag: "g"})
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"title":"t","body":"b","url":"/u","tag":"g"}` {
		t.Errorf("payload = %s", data)
	}
}
