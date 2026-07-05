package push

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	webpush "github.com/SherClockHolmes/webpush-go"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	mock_idgen "github.com/agentrq/agentrq/backend/internal/service/mocks/idgen"
	mock_repo "github.com/agentrq/agentrq/backend/internal/service/mocks/repository"
	"github.com/golang/mock/gomock"
)

func newTestController(t *testing.T, cfg Config) (*controller, *mock_repo.MockRepository, *mock_idgen.MockService) {
	t.Helper()
	ctrl := gomock.NewController(t)
	mockRepo := mock_repo.NewMockRepository(ctrl)
	mockIDGen := mock_idgen.NewMockService(ctrl)
	c := &controller{
		cfg:  cfg,
		repo: mockRepo,
		ids:  mockIDGen,
	}
	return c, mockRepo, mockIDGen
}

// ── IsEnabled ────────────────────────────────────────────────────────────────

func TestIsEnabled(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		enabled bool
	}{
		{"both keys set", Config{VAPIDPublicKey: "pub", VAPIDPrivateKey: "priv"}, true},
		{"no public key", Config{VAPIDPrivateKey: "priv"}, false},
		{"no private key", Config{VAPIDPublicKey: "pub"}, false},
		{"no keys", Config{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _, _ := newTestController(t, tt.cfg)
			if got := c.IsEnabled(); got != tt.enabled {
				t.Errorf("IsEnabled() = %v, want %v", got, tt.enabled)
			}
		})
	}
}

// ── VAPIDPublicKey ───────────────────────────────────────────────────────────

func TestVAPIDPublicKey(t *testing.T) {
	c, _, _ := newTestController(t, Config{VAPIDPublicKey: "mypubkey"})
	if got := c.VAPIDPublicKey(); got != "mypubkey" {
		t.Errorf("VAPIDPublicKey() = %q, want %q", got, "mypubkey")
	}
}

// ── SaveSubscription ─────────────────────────────────────────────────────────

func TestSaveSubscription_NoTypes(t *testing.T) {
	c, mockRepo, mockIDGen := newTestController(t, Config{})
	mockIDGen.EXPECT().NextID().Return(int64(42))
	mockRepo.EXPECT().SavePushSubscription(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, sub model.PushSubscription) error {
			if sub.ID != 42 {
				t.Errorf("expected ID 42, got %d", sub.ID)
			}
			if sub.UserID != 1 || sub.WorkspaceID != 10 {
				t.Errorf("unexpected userID/workspaceID: %d/%d", sub.UserID, sub.WorkspaceID)
			}
			if sub.Endpoint != "https://push.example.com/sub" {
				t.Errorf("unexpected endpoint: %s", sub.Endpoint)
			}
			if sub.Types != "" {
				t.Errorf("expected empty types, got %q", sub.Types)
			}
			return nil
		},
	)

	err := c.SaveSubscription(context.Background(), entity.SavePushSubscriptionRequest{
		UserID:      1,
		WorkspaceID: 10,
		Endpoint:    "https://push.example.com/sub",
		P256dh:      "key",
		Auth:        "auth",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSaveSubscription_WithTypes(t *testing.T) {
	c, mockRepo, mockIDGen := newTestController(t, Config{})
	mockIDGen.EXPECT().NextID().Return(int64(1))
	mockRepo.EXPECT().SavePushSubscription(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, sub model.PushSubscription) error {
			if sub.Types != "task_create,message_create" {
				t.Errorf("unexpected types: %q", sub.Types)
			}
			return nil
		},
	)

	err := c.SaveSubscription(context.Background(), entity.SavePushSubscriptionRequest{
		UserID:   1,
		Endpoint: "https://push.example.com/sub",
		Types:    []string{"task_create", "message_create"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSaveSubscription_RepoError(t *testing.T) {
	c, mockRepo, mockIDGen := newTestController(t, Config{})
	mockIDGen.EXPECT().NextID().Return(int64(1))
	mockRepo.EXPECT().SavePushSubscription(gomock.Any(), gomock.Any()).Return(errors.New("db error"))

	err := c.SaveSubscription(context.Background(), entity.SavePushSubscriptionRequest{Endpoint: "x"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ── DeleteSubscription ───────────────────────────────────────────────────────

func TestDeleteSubscription(t *testing.T) {
	c, mockRepo, _ := newTestController(t, Config{})
	mockRepo.EXPECT().DeletePushSubscription(gomock.Any(), int64(5), "https://push.example.com/sub").Return(nil)

	err := c.DeleteSubscription(context.Background(), entity.DeletePushSubscriptionRequest{
		UserID:   5,
		Endpoint: "https://push.example.com/sub",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ── DeleteSubscriptionByWorkspace ────────────────────────────────────────────

func TestDeleteSubscriptionByWorkspace(t *testing.T) {
	c, mockRepo, _ := newTestController(t, Config{})
	mockRepo.EXPECT().DeletePushSubscriptionByWorkspace(gomock.Any(), int64(5), int64(10), "https://push.example.com/sub").Return(nil)

	err := c.DeleteSubscriptionByWorkspace(context.Background(), entity.DeletePushSubscriptionByWorkspaceRequest{
		UserID:      5,
		WorkspaceID: 10,
		Endpoint:    "https://push.example.com/sub",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ── CheckSubscription ────────────────────────────────────────────────────────

func TestCheckSubscription_Subscribed(t *testing.T) {
	c, mockRepo, _ := newTestController(t, Config{})
	mockRepo.EXPECT().GetPushSubscriptionForWorkspace(gomock.Any(), int64(1), int64(10), "https://push.example.com/sub").Return(true, nil)

	subscribed, err := c.CheckSubscription(context.Background(), entity.CheckPushSubscriptionRequest{
		UserID:      1,
		WorkspaceID: 10,
		Endpoint:    "https://push.example.com/sub",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !subscribed {
		t.Error("expected subscribed=true")
	}
}

func TestCheckSubscription_NotSubscribed(t *testing.T) {
	c, mockRepo, _ := newTestController(t, Config{})
	mockRepo.EXPECT().GetPushSubscriptionForWorkspace(gomock.Any(), int64(1), int64(10), "https://push.example.com/sub").Return(false, nil)

	subscribed, err := c.CheckSubscription(context.Background(), entity.CheckPushSubscriptionRequest{
		UserID:      1,
		WorkspaceID: 10,
		Endpoint:    "https://push.example.com/sub",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if subscribed {
		t.Error("expected subscribed=false")
	}
}

// ── subscriptionAllowsType ───────────────────────────────────────────────────

func TestSubscriptionAllowsType(t *testing.T) {
	tests := []struct {
		storedTypes string
		eventType   string
		want        bool
	}{
		{"", PushTypeTaskCreate, true},
		{"", PushTypeMessageCreate, true},
		{PushTypeTaskCreate, PushTypeTaskCreate, true},
		{PushTypeTaskCreate, PushTypeTaskUpdate, false},
		{"task_create,message_create", PushTypeTaskCreate, true},
		{"task_create,message_create", PushTypeMessageCreate, true},
		{"task_create,message_create", PushTypeTaskUpdate, false},
		{" task_create ", PushTypeTaskCreate, true},
	}
	for _, tt := range tests {
		got := subscriptionAllowsType(tt.storedTypes, tt.eventType)
		if got != tt.want {
			t.Errorf("subscriptionAllowsType(%q, %q) = %v, want %v",
				tt.storedTypes, tt.eventType, got, tt.want)
		}
	}
}

// ── truncate ─────────────────────────────────────────────────────────────────

func TestTruncate(t *testing.T) {
	tests := []struct {
		input string
		n     int
		want  string
	}{
		{"hello", 10, "hello"},
		{"hello", 5, "hello"},
		{"hello world", 5, "hello…"},
		{"", 5, ""},
	}
	for _, tt := range tests {
		got := truncate(tt.input, tt.n)
		if got != tt.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.n, got, tt.want)
		}
	}
}

// ── sendToUser (stale-subscription pruning) ──────────────────────────────────

// newTestSubscriptionKeys returns a valid P-256 ECDH public key (p256dh) and a
// 16-byte auth secret, base64url-encoded exactly as a browser PushSubscription
// provides them. Real keys are required because webpush.SendNotification encrypts
// the payload before sending, so bogus keys would fail before any HTTP round-trip.
func newTestSubscriptionKeys(t *testing.T) (p256dh, auth string) {
	t.Helper()
	priv, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate ECDH key: %v", err)
	}
	authBytes := make([]byte, 16)
	if _, err := rand.Read(authBytes); err != nil {
		t.Fatalf("generate auth secret: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(priv.PublicKey().Bytes()),
		base64.RawURLEncoding.EncodeToString(authBytes)
}

func newPushTestController(t *testing.T) (*controller, *mock_repo.MockRepository) {
	t.Helper()
	vapidPriv, vapidPub, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		t.Fatalf("generate VAPID keys: %v", err)
	}
	c, mockRepo, _ := newTestController(t, Config{
		VAPIDPublicKey:  vapidPub,
		VAPIDPrivateKey: vapidPriv,
		Subscriber:      "mailto:test@example.com",
	})
	return c, mockRepo
}

// A push service returns 404 Not Found or 410 Gone once a subscription no longer
// exists (RFC 8030 §7.3). Those responses arrive with a nil error, so sendToUser
// must inspect the status code and delete the dead subscription.
func TestSendToUser_PrunesExpiredSubscription(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusGone} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
			}))
			defer srv.Close()

			c, mockRepo := newPushTestController(t)
			p256dh, auth := newTestSubscriptionKeys(t)

			mockRepo.EXPECT().
				ListPushSubscriptionsByUserAndWorkspace(gomock.Any(), int64(7), int64(3)).
				Return([]model.PushSubscription{
					{Endpoint: srv.URL, P256dh: p256dh, Auth: auth, Types: PushTypeTaskCreate},
				}, nil)
			mockRepo.EXPECT().
				DeletePushSubscription(gomock.Any(), int64(7), srv.URL).
				Return(nil)

			c.sendToUser(context.Background(), 7, 3, PushTypeTaskCreate, pushPayload{Title: "hi", Body: "body"})
		})
	}
}

// A successful delivery must leave the subscription in place. gomock fails the
// test if DeletePushSubscription is called, since no such call is expected.
func TestSendToUser_KeepsSubscriptionOnSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	c, mockRepo := newPushTestController(t)
	p256dh, auth := newTestSubscriptionKeys(t)

	mockRepo.EXPECT().
		ListPushSubscriptionsByUserAndWorkspace(gomock.Any(), int64(7), int64(3)).
		Return([]model.PushSubscription{
			{Endpoint: srv.URL, P256dh: p256dh, Auth: auth, Types: PushTypeTaskCreate},
		}, nil)

	c.sendToUser(context.Background(), 7, 3, PushTypeTaskCreate, pushPayload{Title: "hi", Body: "body"})
}

// ── processEvent ─────────────────────────────────────────────────────────────

func TestProcessEvent_WorkspaceNotFound(t *testing.T) {
	c, mockRepo, _ := newTestController(t, Config{VAPIDPublicKey: "pub", VAPIDPrivateKey: "priv"})
	mockRepo.EXPECT().
		SystemGetWorkspace(gomock.Any(), int64(99)).
		Return(model.Workspace{}, errors.New("not found"))

	// Should return silently — no panic, no further repo calls.
	c.processEvent(context.Background(), entity.CRUDEvent{
		WorkspaceID:  99,
		ResourceType: entity.ResourceTask,
		ResourceID:   1,
		Action:       entity.ActionTaskCreate,
		Actor:        entity.ActorAgent,
	})
}

func TestProcessEvent_TaskCreate_HumanActor_NoNotification(t *testing.T) {
	c, mockRepo, _ := newTestController(t, Config{VAPIDPublicKey: "pub", VAPIDPrivateKey: "priv"})
	mockRepo.EXPECT().
		SystemGetWorkspace(gomock.Any(), int64(10)).
		Return(model.Workspace{ID: 10, UserID: 1, Name: "ws"}, nil)
	mockRepo.EXPECT().
		SystemGetTask(gomock.Any(), int64(5)).
		Return(model.Task{ID: 5, Title: "Fix bug"}, nil)
	// ListPushSubscriptions must NOT be called for human-actor events

	c.processEvent(context.Background(), entity.CRUDEvent{
		WorkspaceID:  10,
		ResourceType: entity.ResourceTask,
		ResourceID:   5,
		Action:       entity.ActionTaskCreate,
		Actor:        entity.ActorHuman,
	})
}

func TestProcessEvent_TaskCreate_AgentActor(t *testing.T) {
	c, mockRepo, _ := newTestController(t, Config{VAPIDPublicKey: "pub", VAPIDPrivateKey: "priv"})
	mockRepo.EXPECT().
		SystemGetWorkspace(gomock.Any(), int64(10)).
		Return(model.Workspace{ID: 10, UserID: 1, Name: "ws"}, nil)
	mockRepo.EXPECT().
		SystemGetTask(gomock.Any(), int64(5)).
		Return(model.Task{ID: 5, Title: "Fix bug"}, nil)
	mockRepo.EXPECT().
		ListPushSubscriptionsByUserAndWorkspace(gomock.Any(), int64(1), int64(10)).
		Return([]model.PushSubscription{}, nil)

	c.processEvent(context.Background(), entity.CRUDEvent{
		WorkspaceID:  10,
		ResourceType: entity.ResourceTask,
		ResourceID:   5,
		Action:       entity.ActionTaskCreate,
		Actor:        entity.ActorAgent,
	})
}

func TestProcessEvent_TaskUpdate_AgentActor(t *testing.T) {
	c, mockRepo, _ := newTestController(t, Config{VAPIDPublicKey: "pub", VAPIDPrivateKey: "priv"})
	mockRepo.EXPECT().
		SystemGetWorkspace(gomock.Any(), int64(10)).
		Return(model.Workspace{ID: 10, UserID: 1, Name: "ws"}, nil)
	mockRepo.EXPECT().
		SystemGetTask(gomock.Any(), int64(5)).
		Return(model.Task{ID: 5, Title: "Fix bug", Status: "completed"}, nil)
	mockRepo.EXPECT().
		ListPushSubscriptionsByUserAndWorkspace(gomock.Any(), int64(1), int64(10)).
		Return([]model.PushSubscription{}, nil)

	c.processEvent(context.Background(), entity.CRUDEvent{
		WorkspaceID:  10,
		ResourceType: entity.ResourceTask,
		ResourceID:   5,
		Action:       entity.ActionTaskUpdate,
		Actor:        entity.ActorAgent,
	})
}

func TestProcessEvent_MessageCreate_AgentSender(t *testing.T) {
	c, mockRepo, _ := newTestController(t, Config{VAPIDPublicKey: "pub", VAPIDPrivateKey: "priv"})
	mockRepo.EXPECT().
		SystemGetWorkspace(gomock.Any(), int64(10)).
		Return(model.Workspace{ID: 10, UserID: 1, Name: "ws"}, nil)
	mockRepo.EXPECT().
		SystemGetMessage(gomock.Any(), int64(20)).
		Return(model.Message{ID: 20, TaskID: 5, Sender: "agent", Text: "Done!"}, nil)
	mockRepo.EXPECT().
		SystemGetTask(gomock.Any(), int64(5)).
		Return(model.Task{ID: 5, Title: "Fix bug"}, nil)
	mockRepo.EXPECT().
		ListPushSubscriptionsByUserAndWorkspace(gomock.Any(), int64(1), int64(10)).
		Return([]model.PushSubscription{}, nil)

	c.processEvent(context.Background(), entity.CRUDEvent{
		WorkspaceID:  10,
		ResourceType: entity.ResourceMessage,
		ResourceID:   20,
		Action:       entity.ActionMessageCreate,
		Actor:        entity.ActorAgent,
	})
}

func TestProcessEvent_MessageCreate_HumanSender_NoNotification(t *testing.T) {
	c, mockRepo, _ := newTestController(t, Config{VAPIDPublicKey: "pub", VAPIDPrivateKey: "priv"})
	mockRepo.EXPECT().
		SystemGetWorkspace(gomock.Any(), int64(10)).
		Return(model.Workspace{ID: 10, UserID: 1, Name: "ws"}, nil)
	mockRepo.EXPECT().
		SystemGetMessage(gomock.Any(), int64(20)).
		Return(model.Message{ID: 20, TaskID: 5, Sender: "human", Text: "Please do X"}, nil)
	// ListPushSubscriptions must NOT be called for human messages

	c.processEvent(context.Background(), entity.CRUDEvent{
		WorkspaceID:  10,
		ResourceType: entity.ResourceMessage,
		ResourceID:   20,
		Action:       entity.ActionMessageCreate,
		Actor:        entity.ActorHuman,
	})
}

func TestProcessEvent_TypeFiltering_SkipsNonMatchingSub(t *testing.T) {
	c, mockRepo, _ := newTestController(t, Config{VAPIDPublicKey: "pub", VAPIDPrivateKey: "priv"})
	mockRepo.EXPECT().
		SystemGetWorkspace(gomock.Any(), int64(10)).
		Return(model.Workspace{ID: 10, UserID: 1, Name: "ws"}, nil)
	mockRepo.EXPECT().
		SystemGetTask(gomock.Any(), int64(5)).
		Return(model.Task{ID: 5, Title: "Fix bug"}, nil)
	// Subscription only allows message_create — task_create should be filtered out,
	// so no HTTP push is attempted (no crash expected with this fake endpoint).
	mockRepo.EXPECT().
		ListPushSubscriptionsByUserAndWorkspace(gomock.Any(), int64(1), int64(10)).
		Return([]model.PushSubscription{
			{Endpoint: "https://push.example.com/sub", P256dh: "key", Auth: "auth", Types: PushTypeMessageCreate},
		}, nil)

	c.processEvent(context.Background(), entity.CRUDEvent{
		WorkspaceID:  10,
		ResourceType: entity.ResourceTask,
		ResourceID:   5,
		Action:       entity.ActionTaskCreate,
		Actor:        entity.ActorAgent,
	})
}
