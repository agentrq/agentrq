package crud

import (
	"context"
	"errors"
	"testing"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	mock_idgen "github.com/agentrq/agentrq/backend/internal/service/mocks/idgen"
	mock_pubsub "github.com/agentrq/agentrq/backend/internal/service/mocks/pubsub"
	mock_repo "github.com/agentrq/agentrq/backend/internal/service/mocks/repository"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
	"github.com/golang/mock/gomock"
)

const testWorkspaceID int64 = 42

func TestClientReportableActionAllowsOnlyTheNamedTwo(t *testing.T) {
	for name, want := range map[string]entity.Action{
		"local_ai_title_generate": entity.ActionLocalAITitleGenerate,
		"local_ai_recording_end":  entity.ActionLocalAIRecordingEnd,
	} {
		got, ok := entity.ClientReportableAction(name)
		if !ok || got != want {
			t.Errorf("%s: got (%v, %v), want (%v, true)", name, got, ok, want)
		}
	}

	// A client that could name any action could mint the events the backend
	// emits for real work, so everything outside the allowlist must be refused
	// — including actions that genuinely exist.
	for _, name := range []string{
		"task_create",
		"message_create",
		"mcp_tool_call",
		"event_published",
		"user_create",
		"",
		"unknown",
		"LOCAL_AI_TITLE_GENERATE",
	} {
		if _, ok := entity.ClientReportableAction(name); ok {
			t.Errorf("%q must not be client-reportable", name)
		}
	}
}

// Both new actions have to stringify, or they land in telemetry as "unknown"
// and cannot be told apart when the counts are read back.
func TestLocalAIActionsStringify(t *testing.T) {
	if got := entity.ActionLocalAITitleGenerate.String(); got != "local_ai_title_generate" {
		t.Errorf("got %q", got)
	}
	if got := entity.ActionLocalAIRecordingEnd.String(); got != "local_ai_recording_end" {
		t.Errorf("got %q", got)
	}
}

type stubLimiter struct{ allowed int }

func (s *stubLimiter) AllowWorkspace(int64) bool { return true }
func (s *stubLimiter) AllowTask(int64) bool      { return true }
func (s *stubLimiter) AllowMessage(int64) bool   { return true }
func (s *stubLimiter) AllowTelemetry(int64) bool {
	if s.allowed <= 0 {
		return false
	}
	s.allowed--
	return true
}

type telemetryEnv struct {
	controller *controller
	repo       *mock_repo.MockRepository
	pubsub     *mock_pubsub.MockService
	limiter    *stubLimiter
}

func newTelemetryController(t *testing.T, allowed int) *telemetryEnv {
	t.Helper()
	ctrl := gomock.NewController(t)
	repo := mock_repo.NewMockRepository(ctrl)
	psSvc := mock_pubsub.NewMockService(ctrl)
	lim := &stubLimiter{allowed: allowed}

	return &telemetryEnv{
		controller: &controller{
			repository: repo,
			pubsub:     psSvc,
			idgen:      mock_idgen.NewMockService(ctrl),
			limiter:    lim,
		},
		repo:    repo,
		pubsub:  psSvc,
		limiter: lim,
	}
}

func telemetryRequest(action entity.Action) entity.RecordTelemetryRequest {
	return entity.RecordTelemetryRequest{
		Action:      action,
		WorkspaceID: testWorkspaceID,
		UserID:      testUserIDStr,
	}
}

func TestRecordTelemetryPublishesTheActionScopedToUserAndWorkspace(t *testing.T) {
	env := newTelemetryController(t, 1)
	env.repo.EXPECT().CheckWorkspaceAccess(gomock.Any(), testWorkspaceID, testUserID).Return(true, nil)

	var published entity.CRUDEvent
	env.pubsub.EXPECT().Publish(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, req pubsub.PublishRequest) (*pubsub.PublishResponse, error) {
			if req.PubSubID != entity.PubSubTopicCRUD {
				t.Errorf("expected the CRUD topic, got %d", req.PubSubID)
			}
			published, _ = req.Event.(entity.CRUDEvent)
			return &pubsub.PublishResponse{}, nil
		})

	if err := env.controller.RecordTelemetry(context.Background(), telemetryRequest(entity.ActionLocalAIRecordingEnd)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if published.Action != entity.ActionLocalAIRecordingEnd {
		t.Errorf("action: got %v", published.Action)
	}
	// The whole point of the metric is that it can be attributed, so both
	// scopes have to survive to the event.
	if published.WorkspaceID != testWorkspaceID {
		t.Errorf("workspaceID: got %d, want %d", published.WorkspaceID, testWorkspaceID)
	}
	if published.UserID != testUserID {
		t.Errorf("userID: got %d, want %d", published.UserID, testUserID)
	}
	// A person clicked a button; recording it as agent activity would put it
	// in the wrong half of every actor breakdown.
	if published.Actor != entity.ActorHuman {
		t.Errorf("actor: got %v, want human", published.Actor)
	}
}

// Without this a caller could attribute its own clicks to a workspace it does
// not own, corrupting that workspace's metrics.
func TestRecordTelemetryRefusesAWorkspaceTheUserDoesNotOwn(t *testing.T) {
	env := newTelemetryController(t, 1)
	env.repo.EXPECT().CheckWorkspaceAccess(gomock.Any(), testWorkspaceID, testUserID).Return(false, nil)
	// No Publish expectation: reaching pubsub would fail the mock controller.

	err := env.controller.RecordTelemetry(context.Background(), telemetryRequest(entity.ActionLocalAITitleGenerate))
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
}

// The limiter is consulted before the workspace lookup, so a flood costs no
// database work and cannot be used to probe which workspaces exist.
func TestRecordTelemetryRateLimitsBeforeTouchingTheWorkspace(t *testing.T) {
	env := newTelemetryController(t, 0)
	// No CheckWorkspaceAccess or Publish expectations: either call would fail.

	err := env.controller.RecordTelemetry(context.Background(), telemetryRequest(entity.ActionLocalAITitleGenerate))
	if err == nil || err.Error() != "rate limit exceeded" {
		t.Fatalf("expected a rate limit error, got %v", err)
	}
}

func TestRecordTelemetryRejectsMissingIdentifiers(t *testing.T) {
	// Both cases return before the limiter, so no repo or pubsub calls happen.
	env := newTelemetryController(t, 5)

	if err := env.controller.RecordTelemetry(context.Background(), entity.RecordTelemetryRequest{
		Action: entity.ActionLocalAITitleGenerate,
		UserID: testUserIDStr,
	}); err == nil {
		t.Error("expected an error when workspaceID is zero")
	}

	if err := env.controller.RecordTelemetry(context.Background(), entity.RecordTelemetryRequest{
		Action:      entity.ActionLocalAITitleGenerate,
		WorkspaceID: testWorkspaceID,
	}); err == nil {
		t.Error("expected an error when userID is empty")
	}
}
