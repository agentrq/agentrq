package event

import (
	"context"
	"testing"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
	mock_idgen "github.com/agentrq/agentrq/backend/internal/service/mocks/idgen"
	mock_pubsub "github.com/agentrq/agentrq/backend/internal/service/mocks/pubsub"
	mock_repo "github.com/agentrq/agentrq/backend/internal/service/mocks/repository"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
	"github.com/golang/mock/gomock"
)

func newTestController(t *testing.T) (*controller, *mock_repo.MockRepository, *mock_pubsub.MockService, *mock_idgen.MockService) {
	t.Helper()
	ctrl := gomock.NewController(t)
	mockRepo := mock_repo.NewMockRepository(ctrl)
	mockPubSub := mock_pubsub.NewMockService(ctrl)
	mockIDGen := mock_idgen.NewMockService(ctrl)
	c := &controller{
		repo:   mockRepo,
		pubsub: mockPubSub,
		ids:    mockIDGen,
		bus:    eventbus.New(),
	}
	return c, mockRepo, mockPubSub, mockIDGen
}

// ── renderTemplate ────────────────────────────────────────────────────────────

func TestRenderTemplate_PayloadSubstitution(t *testing.T) {
	result := renderTemplate("Deploy: {{EVENT_PAYLOAD}}", "v1.2.3", "")
	if result != "Deploy: v1.2.3" {
		t.Errorf("unexpected result: %q", result)
	}
}

func TestRenderTemplate_FAQSubstitution(t *testing.T) {
	result := renderTemplate("Context:\n{{EVENT_FAQ}}", "", "Q: What?\nA: Nothing.")
	if result != "Context:\nQ: What?\nA: Nothing." {
		t.Errorf("unexpected result: %q", result)
	}
}

func TestRenderTemplate_BothVars(t *testing.T) {
	result := renderTemplate("{{EVENT_PAYLOAD}}\n{{EVENT_FAQ}}", "deployed", "Q: Why?\nA: Because.")
	want := "deployed\nQ: Why?\nA: Because."
	if result != want {
		t.Errorf("got %q, want %q", result, want)
	}
}

func TestRenderTemplate_NoVars(t *testing.T) {
	result := renderTemplate("Fixed title", "payload", "faq")
	if result != "Fixed title" {
		t.Errorf("unexpected result: %q", result)
	}
}

func TestRenderTemplate_EmptyAfterSubstitution(t *testing.T) {
	result := renderTemplate("  {{EVENT_PAYLOAD}}  ", "", "")
	if result != "" {
		t.Errorf("expected empty after trim, got %q", result)
	}
}

func TestRenderTemplate_MissingVarSafe(t *testing.T) {
	// Template with only one var — the other substitution is a no-op
	result := renderTemplate("Payload: {{EVENT_PAYLOAD}}", "hello", "")
	if result != "Payload: hello" {
		t.Errorf("unexpected result: %q", result)
	}
}

// ── renderFAQ ─────────────────────────────────────────────────────────────────

func TestRenderFAQ_Empty(t *testing.T) {
	result := renderFAQ(nil)
	if result != "" {
		t.Errorf("expected empty, got %q", result)
	}
}

func TestRenderFAQ_SingleItem(t *testing.T) {
	faq := []entity.EventFAQ{{Q: "What happened?", A: "Deploy succeeded."}}
	result := renderFAQ(faq)
	want := "Q: What happened?\nA: Deploy succeeded."
	if result != want {
		t.Errorf("got %q, want %q", result, want)
	}
}

func TestRenderFAQ_MultipleItems(t *testing.T) {
	faq := []entity.EventFAQ{
		{Q: "What?", A: "Deployment."},
		{Q: "Where?", A: "Production."},
	}
	result := renderFAQ(faq)
	want := "Q: What?\nA: Deployment.\n\nQ: Where?\nA: Production."
	if result != want {
		t.Errorf("got %q, want %q", result, want)
	}
}

// ── processEvent ──────────────────────────────────────────────────────────────

func TestProcessEvent_NoTriggers(t *testing.T) {
	c, mockRepo, _, _ := newTestController(t)

	mockRepo.EXPECT().
		SystemListEventTriggersByEventID(gomock.Any(), int64(42)).
		Return(nil, nil)

	// No other repo calls expected
	c.processEvent(context.Background(), entity.EventPublishedPayload{
		EventID: 42,
		Name:    "deploy_done",
		Payload: "v1",
	})
}

func TestProcessEvent_SingleTrigger(t *testing.T) {
	c, mockRepo, mockPubSub, mockIDGen := newTestController(t)

	trigger := model.EventTrigger{
		ID:          1,
		EventID:     42,
		WorkspaceID: 10,
		Title:       "Deploy: {{EVENT_PAYLOAD}}",
		Body:        "Details",
		Assignee:    "agent",
	}

	mockRepo.EXPECT().
		SystemListEventTriggersByEventID(gomock.Any(), int64(42)).
		Return([]model.EventTrigger{trigger}, nil)

	mockRepo.EXPECT().
		SystemGetWorkspace(gomock.Any(), int64(10)).
		Return(model.Workspace{ID: 10, UserID: 100}, nil)

	mockIDGen.EXPECT().NextID().Return(int64(999))

	mockRepo.EXPECT().
		CreateTask(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, t model.Task) (model.Task, error) {
			if t.Title != "Deploy: v1.2.3" {
				return model.Task{}, nil
			}
			if t.TriggerID != 42 {
				return model.Task{}, nil
			}
			if t.WorkspaceID != 10 {
				return model.Task{}, nil
			}
			t.CreatedAt = time.Now()
			return t, nil
		})

	mockPubSub.EXPECT().
		Publish(gomock.Any(), gomock.Any()).
		Return(&pubsub.PublishResponse{}, nil)

	c.processEvent(context.Background(), entity.EventPublishedPayload{
		EventID: 42,
		Name:    "deploy_done",
		Payload: "v1.2.3",
	})
}

func TestProcessEvent_MultipleWorkspaces(t *testing.T) {
	c, mockRepo, mockPubSub, mockIDGen := newTestController(t)

	triggers := []model.EventTrigger{
		{ID: 1, EventID: 42, WorkspaceID: 10, Title: "Task in ws10", Assignee: "agent"},
		{ID: 2, EventID: 42, WorkspaceID: 20, Title: "Task in ws20", Assignee: "human"},
	}

	mockRepo.EXPECT().
		SystemListEventTriggersByEventID(gomock.Any(), int64(42)).
		Return(triggers, nil)

	mockRepo.EXPECT().
		SystemGetWorkspace(gomock.Any(), int64(10)).
		Return(model.Workspace{ID: 10, UserID: 100}, nil)
	mockRepo.EXPECT().
		SystemGetWorkspace(gomock.Any(), int64(20)).
		Return(model.Workspace{ID: 20, UserID: 200}, nil)

	mockIDGen.EXPECT().NextID().Return(int64(111)).Times(1)
	mockIDGen.EXPECT().NextID().Return(int64(222)).Times(1)

	mockRepo.EXPECT().CreateTask(gomock.Any(), gomock.Any()).Return(model.Task{ID: 111}, nil).Times(1)
	mockRepo.EXPECT().CreateTask(gomock.Any(), gomock.Any()).Return(model.Task{ID: 222}, nil).Times(1)

	mockPubSub.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(&pubsub.PublishResponse{}, nil).Times(2)

	c.processEvent(context.Background(), entity.EventPublishedPayload{
		EventID: 42,
		Name:    "deploy_done",
		Payload: "v1.2.3",
	})
}

func TestProcessEvent_EmptyTitleSkipped(t *testing.T) {
	c, mockRepo, _, _ := newTestController(t)

	trigger := model.EventTrigger{
		ID:          1,
		EventID:     42,
		WorkspaceID: 10,
		Title:       "  ", // whitespace-only title is treated as empty
		Assignee:    "agent",
	}

	mockRepo.EXPECT().
		SystemListEventTriggersByEventID(gomock.Any(), int64(42)).
		Return([]model.EventTrigger{trigger}, nil)

	// SystemGetWorkspace and CreateTask must NOT be called — empty title guard fires first
	c.processEvent(context.Background(), entity.EventPublishedPayload{
		EventID: 42,
		Name:    "deploy_done",
		Payload: "some payload",
	})
}

func TestProcessEvent_WorkspaceNotFound(t *testing.T) {
	c, mockRepo, _, _ := newTestController(t)

	trigger := model.EventTrigger{
		ID: 1, EventID: 42, WorkspaceID: 99, Title: "My task", Assignee: "agent",
	}

	mockRepo.EXPECT().
		SystemListEventTriggersByEventID(gomock.Any(), int64(42)).
		Return([]model.EventTrigger{trigger}, nil)

	mockRepo.EXPECT().
		SystemGetWorkspace(gomock.Any(), int64(99)).
		Return(model.Workspace{}, base.ErrNotFound)

	// No CreateTask call expected
	c.processEvent(context.Background(), entity.EventPublishedPayload{
		EventID: 42,
		Name:    "deploy_done",
	})
}

func TestProcessEvent_WithCronSchedule(t *testing.T) {
	c, mockRepo, mockPubSub, mockIDGen := newTestController(t)

	trigger := model.EventTrigger{
		ID:           1,
		EventID:      42,
		WorkspaceID:  10,
		Title:        "Recurring check",
		Assignee:     "agent",
		CronSchedule: "0 * * * *",
	}

	mockRepo.EXPECT().
		SystemListEventTriggersByEventID(gomock.Any(), int64(42)).
		Return([]model.EventTrigger{trigger}, nil)

	mockRepo.EXPECT().
		SystemGetWorkspace(gomock.Any(), int64(10)).
		Return(model.Workspace{ID: 10, UserID: 100}, nil)

	mockIDGen.EXPECT().NextID().Return(int64(500))

	mockRepo.EXPECT().
		CreateTask(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, t model.Task) (model.Task, error) {
			if t.Status != "cron" {
				return model.Task{}, nil
			}
			return t, nil
		})

	mockPubSub.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(&pubsub.PublishResponse{}, nil)

	c.processEvent(context.Background(), entity.EventPublishedPayload{
		EventID: 42,
		Name:    "deploy_done",
	})
}
