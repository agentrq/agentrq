package crud

import (
	"context"
	"strings"
	"testing"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/golang/mock/gomock"
)

// ── isValidEventName ──────────────────────────────────────────────────────────

func TestIsValidEventName(t *testing.T) {
	valid := []string{"a", "abc", "a1", "a_b", "task_done", "a" + repeat("b", 128)}
	for _, n := range valid {
		if !isValidEventName(n) {
			t.Errorf("expected %q to be valid", n)
		}
	}
	invalid := []string{
		"", "A", "1abc", "_abc", "abc-def", "abc def",
		"a" + repeat("b", 129), // 130 chars total — over limit
	}
	for _, n := range invalid {
		if isValidEventName(n) {
			t.Errorf("expected %q to be invalid", n)
		}
	}
}

func repeat(s string, n int) string {
	out := make([]byte, n)
	for i := range out {
		out[i] = s[0]
	}
	return string(out)
}

// ── CreateEvent ───────────────────────────────────────────────────────────────

func TestCreateEvent_Success(t *testing.T) {
	e := newTestController(t)
	now := time.Now()
	created := model.Event{ID: 100, UserID: testUserID, Name: "deploy_done", CreatedAt: now, UpdatedAt: now}

	e.idgen.EXPECT().NextID().Return(int64(100))
	e.repo.EXPECT().CreateEvent(gomock.Any(), gomock.Any()).Return(created, nil)

	resp, err := e.controller.CreateEvent(context.Background(), entity.CreateEventRequest{
		UserID: testUserIDStr,
		Name:   "deploy_done",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Event.ID != 100 {
		t.Errorf("expected ID 100, got %d", resp.Event.ID)
	}
}

func TestCreateEvent_InvalidName(t *testing.T) {
	e := newTestController(t)
	_, err := e.controller.CreateEvent(context.Background(), entity.CreateEventRequest{
		UserID: testUserIDStr,
		Name:   "InvalidName",
	})
	if err == nil {
		t.Error("expected error for invalid event name")
	}
}

func TestCreateEvent_EmptyName(t *testing.T) {
	e := newTestController(t)
	_, err := e.controller.CreateEvent(context.Background(), entity.CreateEventRequest{
		UserID: testUserIDStr,
		Name:   "",
	})
	if err == nil {
		t.Error("expected error for empty name")
	}
}

func TestCreateEvent_InvalidUserID(t *testing.T) {
	e := newTestController(t)
	_, err := e.controller.CreateEvent(context.Background(), entity.CreateEventRequest{
		UserID: "",
		Name:   "valid_name",
	})
	if err == nil {
		t.Error("expected error for invalid userID")
	}
}

// ── GetEvent ──────────────────────────────────────────────────────────────────

func TestGetEvent_Success(t *testing.T) {
	e := newTestController(t)
	now := time.Now()
	m := model.Event{ID: 200, UserID: testUserID, Name: "code_done", CreatedAt: now}

	e.repo.EXPECT().GetEvent(gomock.Any(), int64(200), testUserID).Return(m, nil)

	resp, err := e.controller.GetEvent(context.Background(), entity.GetEventRequest{
		ID:     200,
		UserID: testUserIDStr,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Event.Name != "code_done" {
		t.Errorf("expected name code_done, got %s", resp.Event.Name)
	}
}

func TestGetEvent_NotFound(t *testing.T) {
	e := newTestController(t)
	e.repo.EXPECT().GetEvent(gomock.Any(), int64(999), testUserID).Return(model.Event{}, base.ErrNotFound)

	_, err := e.controller.GetEvent(context.Background(), entity.GetEventRequest{
		ID:     999,
		UserID: testUserIDStr,
	})
	if err == nil {
		t.Error("expected error for missing event")
	}
}

// ── ListEvents ────────────────────────────────────────────────────────────────

func TestListEvents_Success(t *testing.T) {
	e := newTestController(t)
	models := []model.Event{
		{ID: 1, UserID: testUserID, Name: "event_a"},
		{ID: 2, UserID: testUserID, Name: "event_b"},
	}
	e.repo.EXPECT().ListEventsByUser(gomock.Any(), testUserID).Return(models, nil)

	resp, err := e.controller.ListEvents(context.Background(), entity.ListEventsRequest{UserID: testUserIDStr})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Events) != 2 {
		t.Errorf("expected 2 events, got %d", len(resp.Events))
	}
}

// ── DeleteEvent ───────────────────────────────────────────────────────────────

func TestDeleteEvent_Success(t *testing.T) {
	e := newTestController(t)
	e.repo.EXPECT().DeleteEvent(gomock.Any(), int64(100), testUserID).Return(nil)

	err := e.controller.DeleteEvent(context.Background(), entity.DeleteEventRequest{
		ID:     100,
		UserID: testUserIDStr,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeleteEvent_NotFound(t *testing.T) {
	e := newTestController(t)
	e.repo.EXPECT().DeleteEvent(gomock.Any(), int64(999), testUserID).Return(base.ErrNotFound)

	err := e.controller.DeleteEvent(context.Background(), entity.DeleteEventRequest{
		ID:     999,
		UserID: testUserIDStr,
	})
	if err == nil {
		t.Error("expected error for missing event")
	}
}

// ── CreateEventTrigger ────────────────────────────────────────────────────────

func TestCreateEventTrigger_Success(t *testing.T) {
	e := newTestController(t)
	now := time.Now()
	created := model.EventTrigger{
		ID: 500, EventID: 100, WorkspaceID: 1, UserID: testUserID,
		Title: "Deploy done: {{EVENT_PAYLOAD}}", Assignee: "agent",
		CreatedAt: now,
	}

	e.repo.EXPECT().GetEvent(gomock.Any(), int64(100), testUserID).Return(model.Event{ID: 100}, nil)
	e.repo.EXPECT().CheckWorkspaceAccess(gomock.Any(), int64(1), testUserID).Return(true, nil)
	e.idgen.EXPECT().NextID().Return(int64(500))
	e.repo.EXPECT().CreateEventTrigger(gomock.Any(), gomock.Any()).Return(created, nil)

	resp, err := e.controller.CreateEventTrigger(context.Background(), entity.CreateEventTriggerRequest{
		EventID:     100,
		WorkspaceID: 1,
		Title:       "Deploy done: {{EVENT_PAYLOAD}}",
		Assignee:    "agent",
		UserID:      testUserIDStr,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.EventTrigger.ID != 500 {
		t.Errorf("expected ID 500, got %d", resp.EventTrigger.ID)
	}
}

func TestCreateEventTrigger_EventNotFound(t *testing.T) {
	e := newTestController(t)
	e.repo.EXPECT().GetEvent(gomock.Any(), int64(999), testUserID).Return(model.Event{}, base.ErrNotFound)

	_, err := e.controller.CreateEventTrigger(context.Background(), entity.CreateEventTriggerRequest{
		EventID:     999,
		WorkspaceID: 1,
		Title:       "My trigger",
		UserID:      testUserIDStr,
	})
	if err == nil {
		t.Error("expected error when event not found")
	}
}

func TestCreateEventTrigger_WorkspaceNotOwned(t *testing.T) {
	e := newTestController(t)
	e.repo.EXPECT().GetEvent(gomock.Any(), int64(100), testUserID).Return(model.Event{ID: 100}, nil)
	e.repo.EXPECT().CheckWorkspaceAccess(gomock.Any(), int64(99), testUserID).Return(false, nil)

	_, err := e.controller.CreateEventTrigger(context.Background(), entity.CreateEventTriggerRequest{
		EventID:     100,
		WorkspaceID: 99,
		Title:       "My trigger",
		UserID:      testUserIDStr,
	})
	if err == nil {
		t.Error("expected error for unowned workspace")
	}
}

func TestCreateEventTrigger_InvalidCron(t *testing.T) {
	e := newTestController(t)
	e.repo.EXPECT().GetEvent(gomock.Any(), int64(100), testUserID).Return(model.Event{ID: 100}, nil)
	e.repo.EXPECT().CheckWorkspaceAccess(gomock.Any(), int64(1), testUserID).Return(true, nil)

	_, err := e.controller.CreateEventTrigger(context.Background(), entity.CreateEventTriggerRequest{
		EventID:      100,
		WorkspaceID:  1,
		Title:        "My trigger",
		CronSchedule: "*/5 * * * *", // sub-hourly — rejected
		UserID:       testUserIDStr,
	})
	if err == nil {
		t.Fatal("expected error for invalid cron schedule")
	}
	// The trigger path delegates to the shared schedule.ValidateCronGranularity
	// (same guardrail as the MCP server and task paths); assert we get its
	// granularity error rather than any other validation failure.
	if !strings.Contains(err.Error(), "granularity too fine") {
		t.Errorf("expected granularity error, got %v", err)
	}
}

func TestCreateEventTrigger_EmptyTitle(t *testing.T) {
	e := newTestController(t)
	e.repo.EXPECT().GetEvent(gomock.Any(), int64(100), testUserID).Return(model.Event{ID: 100}, nil)
	e.repo.EXPECT().CheckWorkspaceAccess(gomock.Any(), int64(1), testUserID).Return(true, nil)

	_, err := e.controller.CreateEventTrigger(context.Background(), entity.CreateEventTriggerRequest{
		EventID:     100,
		WorkspaceID: 1,
		Title:       "",
		UserID:      testUserIDStr,
	})
	if err == nil {
		t.Error("expected error for empty title")
	}
}

func TestCreateEventTrigger_InvalidUserID(t *testing.T) {
	e := newTestController(t)
	_, err := e.controller.CreateEventTrigger(context.Background(), entity.CreateEventTriggerRequest{
		EventID:     100,
		WorkspaceID: 1,
		Title:       "My trigger",
		UserID:      "",
	})
	if err == nil {
		t.Error("expected error for invalid userID")
	}
}

func TestCreateEventTrigger_EmitEventNotOwned(t *testing.T) {
	e := newTestController(t)
	e.repo.EXPECT().GetEvent(gomock.Any(), int64(100), testUserID).Return(model.Event{ID: 100}, nil)
	e.repo.EXPECT().CheckWorkspaceAccess(gomock.Any(), int64(1), testUserID).Return(true, nil)
	e.repo.EXPECT().GetEvent(gomock.Any(), int64(999), testUserID).Return(model.Event{}, base.ErrNotFound)

	_, err := e.controller.CreateEventTrigger(context.Background(), entity.CreateEventTriggerRequest{
		EventID:     100,
		WorkspaceID: 1,
		Title:       "My trigger",
		EmitEventID: 999,
		UserID:      testUserIDStr,
	})
	if err == nil {
		t.Error("expected error when emit event not owned by user")
	}
}

// ── ListEventTriggers ─────────────────────────────────────────────────────────

func TestListEventTriggers_Success(t *testing.T) {
	e := newTestController(t)
	models := []model.EventTrigger{
		{ID: 1, EventID: 10, UserID: testUserID},
		{ID: 2, EventID: 10, UserID: testUserID},
	}
	e.repo.EXPECT().ListEventTriggersByEvent(gomock.Any(), int64(10), testUserID).Return(models, nil)

	resp, err := e.controller.ListEventTriggers(context.Background(), entity.ListEventTriggersRequest{
		EventID: 10,
		UserID:  testUserIDStr,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.EventTriggers) != 2 {
		t.Errorf("expected 2 triggers, got %d", len(resp.EventTriggers))
	}
}

// ── DeleteEventTrigger ────────────────────────────────────────────────────────

func TestDeleteEventTrigger_Success(t *testing.T) {
	e := newTestController(t)
	e.repo.EXPECT().DeleteEventTrigger(gomock.Any(), int64(500), testUserID).Return(nil)

	err := e.controller.DeleteEventTrigger(context.Background(), entity.DeleteEventTriggerRequest{
		ID:     500,
		UserID: testUserIDStr,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeleteEventTrigger_NotFound(t *testing.T) {
	e := newTestController(t)
	e.repo.EXPECT().DeleteEventTrigger(gomock.Any(), int64(999), testUserID).Return(base.ErrNotFound)

	err := e.controller.DeleteEventTrigger(context.Background(), entity.DeleteEventTriggerRequest{
		ID:     999,
		UserID: testUserIDStr,
	})
	if err == nil {
		t.Error("expected error for missing trigger")
	}
}

// ── ListTasksFromEvent ────────────────────────────────────────────────────────

func TestListTasksFromEvent_Success(t *testing.T) {
	e := newTestController(t)
	models := []model.Task{
		{ID: 1, TriggerID: 10, UserID: testUserID, Status: "completed"},
		{ID: 2, TriggerID: 10, UserID: testUserID, Status: "notstarted"},
	}
	e.repo.EXPECT().ListTasksByTriggerID(gomock.Any(), int64(10), testUserID).Return(models, nil)

	resp, err := e.controller.ListTasksFromEvent(context.Background(), entity.ListTasksFromEventRequest{
		EventID: 10,
		UserID:  testUserIDStr,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Tasks) != 2 {
		t.Errorf("expected 2 tasks, got %d", len(resp.Tasks))
	}
}

func TestListTasksFromEvent_Empty(t *testing.T) {
	e := newTestController(t)
	e.repo.EXPECT().ListTasksByTriggerID(gomock.Any(), int64(42), testUserID).Return(nil, nil)

	resp, err := e.controller.ListTasksFromEvent(context.Background(), entity.ListTasksFromEventRequest{
		EventID: 42,
		UserID:  testUserIDStr,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Tasks) != 0 {
		t.Errorf("expected 0 tasks, got %d", len(resp.Tasks))
	}
}

// ── GetEventTrigger ───────────────────────────────────────────────────────────

func TestGetEventTrigger_Success(t *testing.T) {
	e := newTestController(t)
	m := model.EventTrigger{ID: 500, EventID: 100, UserID: testUserID, Title: "my trigger"}
	e.repo.EXPECT().GetEventTrigger(gomock.Any(), int64(500), testUserID).Return(m, nil)

	resp, err := e.controller.GetEventTrigger(context.Background(), entity.GetEventTriggerRequest{
		ID:     500,
		UserID: testUserIDStr,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.EventTrigger.Title != "my trigger" {
		t.Errorf("expected title 'my trigger', got %s", resp.EventTrigger.Title)
	}
}
