package api

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/gofiber/fiber/v2"
	"github.com/mustafaturan/monoflake"
)

// ── mock event crud controller ────────────────────────────────────────────────

type mockEventCrud struct {
	crud.Controller
	createEventFunc        func(ctx context.Context, req entity.CreateEventRequest) (*entity.CreateEventResponse, error)
	getEventFunc           func(ctx context.Context, req entity.GetEventRequest) (*entity.GetEventResponse, error)
	listEventsFunc         func(ctx context.Context, req entity.ListEventsRequest) (*entity.ListEventsResponse, error)
	deleteEventFunc        func(ctx context.Context, req entity.DeleteEventRequest) error
	createEventTriggerFunc func(ctx context.Context, req entity.CreateEventTriggerRequest) (*entity.CreateEventTriggerResponse, error)
	listEventTriggersFunc  func(ctx context.Context, req entity.ListEventTriggersRequest) (*entity.ListEventTriggersResponse, error)
	deleteEventTriggerFunc func(ctx context.Context, req entity.DeleteEventTriggerRequest) error
	listTasksFromEventFunc func(ctx context.Context, req entity.ListTasksFromEventRequest) (*entity.ListTasksFromEventResponse, error)
	updateEventFunc        func(ctx context.Context, req entity.UpdateEventRequest) (*entity.UpdateEventResponse, error)
}

func (m *mockEventCrud) CreateEvent(ctx context.Context, req entity.CreateEventRequest) (*entity.CreateEventResponse, error) {
	return m.createEventFunc(ctx, req)
}
func (m *mockEventCrud) GetEvent(ctx context.Context, req entity.GetEventRequest) (*entity.GetEventResponse, error) {
	return m.getEventFunc(ctx, req)
}
func (m *mockEventCrud) ListEvents(ctx context.Context, req entity.ListEventsRequest) (*entity.ListEventsResponse, error) {
	return m.listEventsFunc(ctx, req)
}
func (m *mockEventCrud) DeleteEvent(ctx context.Context, req entity.DeleteEventRequest) error {
	return m.deleteEventFunc(ctx, req)
}
func (m *mockEventCrud) CreateEventTrigger(ctx context.Context, req entity.CreateEventTriggerRequest) (*entity.CreateEventTriggerResponse, error) {
	return m.createEventTriggerFunc(ctx, req)
}
func (m *mockEventCrud) ListEventTriggers(ctx context.Context, req entity.ListEventTriggersRequest) (*entity.ListEventTriggersResponse, error) {
	return m.listEventTriggersFunc(ctx, req)
}
func (m *mockEventCrud) DeleteEventTrigger(ctx context.Context, req entity.DeleteEventTriggerRequest) error {
	return m.deleteEventTriggerFunc(ctx, req)
}
func (m *mockEventCrud) ListTasksFromEvent(ctx context.Context, req entity.ListTasksFromEventRequest) (*entity.ListTasksFromEventResponse, error) {
	return m.listTasksFromEventFunc(ctx, req)
}
func (m *mockEventCrud) UpdateEvent(ctx context.Context, req entity.UpdateEventRequest) (*entity.UpdateEventResponse, error) {
	return m.updateEventFunc(ctx, req)
}

func newEventApp(ctrl *mockEventCrud) *fiber.App {
	app := fiber.New()
	h := &handler{crud: ctrl}
	app.Post("/events", func(c *fiber.Ctx) error {
		c.Locals("user_id", "user1")
		return h.createEvent()(c)
	})
	app.Get("/events", func(c *fiber.Ctx) error {
		c.Locals("user_id", "user1")
		return h.listEvents()(c)
	})
	app.Get("/events/:id", func(c *fiber.Ctx) error {
		c.Locals("user_id", "user1")
		return h.getEvent()(c)
	})
	app.Delete("/events/:id", func(c *fiber.Ctx) error {
		c.Locals("user_id", "user1")
		return h.deleteEvent()(c)
	})
	app.Patch("/events/:id", func(c *fiber.Ctx) error {
		c.Locals("user_id", "user1")
		return h.updateEvent()(c)
	})
	app.Post("/events/:id/triggers", func(c *fiber.Ctx) error {
		c.Locals("user_id", "user1")
		return h.createEventTrigger()(c)
	})
	app.Get("/events/:id/triggers", func(c *fiber.Ctx) error {
		c.Locals("user_id", "user1")
		return h.listEventTriggers()(c)
	})
	app.Delete("/events/:id/triggers/:triggerID", func(c *fiber.Ctx) error {
		c.Locals("user_id", "user1")
		return h.deleteEventTrigger()(c)
	})
	app.Get("/events/:id/tasks", func(c *fiber.Ctx) error {
		c.Locals("user_id", "user1")
		return h.listTasksFromEvent()(c)
	})
	return app
}

var testEventID = monoflake.ID(100).String()
var testTriggerID = monoflake.ID(200).String()

// ── createEvent ───────────────────────────────────────────────────────────────

func TestCreateEvent_Created(t *testing.T) {
	ctrl := &mockEventCrud{}
	ctrl.createEventFunc = func(_ context.Context, req entity.CreateEventRequest) (*entity.CreateEventResponse, error) {
		return &entity.CreateEventResponse{Event: entity.Event{ID: 100, Name: req.Name}}, nil
	}
	app := newEventApp(ctrl)

	body := []byte(`{"name":"deploy_done","payloadGuidelines":"what was deployed"}`)
	req := httptest.NewRequest(http.MethodPost, "/events", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)

	if resp.StatusCode != http.StatusCreated {
		t.Errorf("expected 201, got %d", resp.StatusCode)
	}
}

func TestCreateEvent_InvalidPayload(t *testing.T) {
	app := newEventApp(&mockEventCrud{})
	req := httptest.NewRequest(http.MethodPost, "/events", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)

	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("expected 422, got %d", resp.StatusCode)
	}
}

func TestCreateEvent_ControllerError(t *testing.T) {
	ctrl := &mockEventCrud{}
	ctrl.createEventFunc = func(_ context.Context, _ entity.CreateEventRequest) (*entity.CreateEventResponse, error) {
		return nil, errors.New("invalid event name")
	}
	app := newEventApp(ctrl)

	body := []byte(`{"name":"bad name"}`)
	req := httptest.NewRequest(http.MethodPost, "/events", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", resp.StatusCode)
	}
}

// ── listEvents ────────────────────────────────────────────────────────────────

func TestListEvents_OK(t *testing.T) {
	ctrl := &mockEventCrud{}
	ctrl.listEventsFunc = func(_ context.Context, _ entity.ListEventsRequest) (*entity.ListEventsResponse, error) {
		return &entity.ListEventsResponse{Events: []entity.Event{{ID: 1, Name: "ev"}}}, nil
	}
	app := newEventApp(ctrl)

	req := httptest.NewRequest(http.MethodGet, "/events", nil)
	resp, _ := app.Test(req)

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

// ── getEvent ──────────────────────────────────────────────────────────────────

func TestGetEvent_OK(t *testing.T) {
	ctrl := &mockEventCrud{}
	ctrl.getEventFunc = func(_ context.Context, req entity.GetEventRequest) (*entity.GetEventResponse, error) {
		return &entity.GetEventResponse{Event: entity.Event{ID: req.ID, Name: "ev"}}, nil
	}
	app := newEventApp(ctrl)

	req := httptest.NewRequest(http.MethodGet, "/events/"+testEventID, nil)
	resp, _ := app.Test(req)

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestGetEvent_NotFound(t *testing.T) {
	ctrl := &mockEventCrud{}
	ctrl.getEventFunc = func(_ context.Context, _ entity.GetEventRequest) (*entity.GetEventResponse, error) {
		return nil, base.ErrNotFound
	}
	app := newEventApp(ctrl)

	req := httptest.NewRequest(http.MethodGet, "/events/"+testEventID, nil)
	resp, _ := app.Test(req)

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestGetEvent_InvalidID(t *testing.T) {
	app := newEventApp(&mockEventCrud{})
	// "0" encodes to monoflake ID 0 which will fail base62 parse
	req := httptest.NewRequest(http.MethodGet, "/events/!!!", nil)
	resp, _ := app.Test(req)

	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("expected 422, got %d", resp.StatusCode)
	}
}

// ── deleteEvent ───────────────────────────────────────────────────────────────

func TestDeleteEvent_NoContent(t *testing.T) {
	ctrl := &mockEventCrud{}
	ctrl.deleteEventFunc = func(_ context.Context, _ entity.DeleteEventRequest) error {
		return nil
	}
	app := newEventApp(ctrl)

	req := httptest.NewRequest(http.MethodDelete, "/events/"+testEventID, nil)
	resp, _ := app.Test(req)

	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("expected 204, got %d", resp.StatusCode)
	}
}

func TestDeleteEvent_NotFound(t *testing.T) {
	ctrl := &mockEventCrud{}
	ctrl.deleteEventFunc = func(_ context.Context, _ entity.DeleteEventRequest) error {
		return base.ErrNotFound
	}
	app := newEventApp(ctrl)

	req := httptest.NewRequest(http.MethodDelete, "/events/"+testEventID, nil)
	resp, _ := app.Test(req)

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestDeleteEvent_InvalidID(t *testing.T) {
	app := newEventApp(&mockEventCrud{})
	req := httptest.NewRequest(http.MethodDelete, "/events/!!!", nil)
	resp, _ := app.Test(req)

	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("expected 422, got %d", resp.StatusCode)
	}
}

// ── IDOR: getEvent returns 404 for unowned events ─────────────────────────────

func TestGetEvent_IDOR(t *testing.T) {
	ctrl := &mockEventCrud{}
	ctrl.getEventFunc = func(_ context.Context, req entity.GetEventRequest) (*entity.GetEventResponse, error) {
		// Simulate: event exists but belongs to a different user
		return nil, base.ErrNotFound
	}
	app := newEventApp(ctrl)

	req := httptest.NewRequest(http.MethodGet, "/events/"+testEventID, nil)
	resp, _ := app.Test(req)

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for IDOR attempt, got %d", resp.StatusCode)
	}
}

// ── createEventTrigger ────────────────────────────────────────────────────────

func TestCreateEventTrigger_Created(t *testing.T) {
	ctrl := &mockEventCrud{}
	ctrl.createEventTriggerFunc = func(_ context.Context, req entity.CreateEventTriggerRequest) (*entity.CreateEventTriggerResponse, error) {
		return &entity.CreateEventTriggerResponse{
			EventTrigger: entity.EventTrigger{ID: 200, EventID: req.EventID},
		}, nil
	}
	app := newEventApp(ctrl)

	wsID := monoflake.ID(1).String()
	body := []byte(`{"workspaceId":"` + wsID + `","title":"Deploy done","assignee":"agent"}`)
	req := httptest.NewRequest(http.MethodPost, "/events/"+testEventID+"/triggers", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)

	if resp.StatusCode != http.StatusCreated {
		t.Errorf("expected 201, got %d", resp.StatusCode)
	}
}

func TestCreateEventTrigger_InvalidPayload(t *testing.T) {
	app := newEventApp(&mockEventCrud{})
	// Missing title → mapper returns nil
	body := []byte(`{"workspaceId":"` + monoflake.ID(1).String() + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/events/"+testEventID+"/triggers", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)

	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("expected 422, got %d", resp.StatusCode)
	}
}

func TestCreateEventTrigger_EventNotOwned(t *testing.T) {
	ctrl := &mockEventCrud{}
	ctrl.createEventTriggerFunc = func(_ context.Context, _ entity.CreateEventTriggerRequest) (*entity.CreateEventTriggerResponse, error) {
		return nil, base.ErrNotFound
	}
	app := newEventApp(ctrl)

	wsID := monoflake.ID(1).String()
	body := []byte(`{"workspaceId":"` + wsID + `","title":"My trigger","assignee":"agent"}`)
	req := httptest.NewRequest(http.MethodPost, "/events/"+testEventID+"/triggers", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for unowned event, got %d", resp.StatusCode)
	}
}

// ── listEventTriggers ─────────────────────────────────────────────────────────

func TestListEventTriggers_OK(t *testing.T) {
	ctrl := &mockEventCrud{}
	ctrl.getEventFunc = func(_ context.Context, _ entity.GetEventRequest) (*entity.GetEventResponse, error) {
		return &entity.GetEventResponse{}, nil
	}
	ctrl.listEventTriggersFunc = func(_ context.Context, _ entity.ListEventTriggersRequest) (*entity.ListEventTriggersResponse, error) {
		return &entity.ListEventTriggersResponse{EventTriggers: []entity.EventTrigger{{ID: 200}}}, nil
	}
	app := newEventApp(ctrl)

	req := httptest.NewRequest(http.MethodGet, "/events/"+testEventID+"/triggers", nil)
	resp, _ := app.Test(req)

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestListEventTriggers_IDOR(t *testing.T) {
	ctrl := &mockEventCrud{}
	ctrl.getEventFunc = func(_ context.Context, _ entity.GetEventRequest) (*entity.GetEventResponse, error) {
		return nil, base.ErrNotFound
	}
	app := newEventApp(ctrl)

	req := httptest.NewRequest(http.MethodGet, "/events/"+testEventID+"/triggers", nil)
	resp, _ := app.Test(req)

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for IDOR, got %d", resp.StatusCode)
	}
}

// ── deleteEventTrigger ────────────────────────────────────────────────────────

func TestDeleteEventTrigger_NoContent(t *testing.T) {
	ctrl := &mockEventCrud{}
	ctrl.deleteEventTriggerFunc = func(_ context.Context, _ entity.DeleteEventTriggerRequest) error {
		return nil
	}
	app := newEventApp(ctrl)

	req := httptest.NewRequest(http.MethodDelete, "/events/"+testEventID+"/triggers/"+testTriggerID, nil)
	resp, _ := app.Test(req)

	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("expected 204, got %d", resp.StatusCode)
	}
}

func TestDeleteEventTrigger_NotFound(t *testing.T) {
	ctrl := &mockEventCrud{}
	ctrl.deleteEventTriggerFunc = func(_ context.Context, _ entity.DeleteEventTriggerRequest) error {
		return base.ErrNotFound
	}
	app := newEventApp(ctrl)

	req := httptest.NewRequest(http.MethodDelete, "/events/"+testEventID+"/triggers/"+testTriggerID, nil)
	resp, _ := app.Test(req)

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestDeleteEventTrigger_InvalidID(t *testing.T) {
	app := newEventApp(&mockEventCrud{})
	req := httptest.NewRequest(http.MethodDelete, "/events/"+testEventID+"/triggers/!!!", nil)
	resp, _ := app.Test(req)

	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("expected 422, got %d", resp.StatusCode)
	}
}

// ── listTasksFromEvent ────────────────────────────────────────────────────────

func TestListTasksFromEvent_OK(t *testing.T) {
	ctrl := &mockEventCrud{}
	ctrl.getEventFunc = func(_ context.Context, _ entity.GetEventRequest) (*entity.GetEventResponse, error) {
		return &entity.GetEventResponse{}, nil
	}
	ctrl.listTasksFromEventFunc = func(_ context.Context, _ entity.ListTasksFromEventRequest) (*entity.ListTasksFromEventResponse, error) {
		return &entity.ListTasksFromEventResponse{Tasks: []entity.Task{{ID: 99}}}, nil
	}
	app := newEventApp(ctrl)

	req := httptest.NewRequest(http.MethodGet, "/events/"+testEventID+"/tasks", nil)
	resp, _ := app.Test(req)

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestListTasksFromEvent_IDOR(t *testing.T) {
	ctrl := &mockEventCrud{}
	ctrl.getEventFunc = func(_ context.Context, _ entity.GetEventRequest) (*entity.GetEventResponse, error) {
		return nil, base.ErrNotFound
	}
	app := newEventApp(ctrl)

	req := httptest.NewRequest(http.MethodGet, "/events/"+testEventID+"/tasks", nil)
	resp, _ := app.Test(req)

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for IDOR, got %d", resp.StatusCode)
	}
}

func TestListTasksFromEvent_InvalidEventID(t *testing.T) {
	app := newEventApp(&mockEventCrud{})
	req := httptest.NewRequest(http.MethodGet, "/events/!!!/tasks", nil)
	resp, _ := app.Test(req)

	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("expected 422, got %d", resp.StatusCode)
	}
}

// ── updateEvent ───────────────────────────────────────────────────────────────

func TestUpdateEvent_OK(t *testing.T) {
	ctrl := &mockEventCrud{}
	ctrl.updateEventFunc = func(_ context.Context, req entity.UpdateEventRequest) (*entity.UpdateEventResponse, error) {
		return &entity.UpdateEventResponse{Event: entity.Event{ID: req.ID, PayloadGuidelines: req.PayloadGuidelines}}, nil
	}
	app := newEventApp(ctrl)

	body := []byte(`{"payloadGuidelines":"describe what was deployed"}`)
	req := httptest.NewRequest(http.MethodPatch, "/events/"+testEventID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestUpdateEvent_InvalidID(t *testing.T) {
	app := newEventApp(&mockEventCrud{})

	req := httptest.NewRequest(http.MethodPatch, "/events/!!!", bytes.NewReader([]byte(`{"payloadGuidelines":"x"}`)))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)

	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("expected 422, got %d", resp.StatusCode)
	}
}

func TestUpdateEvent_InvalidPayload(t *testing.T) {
	app := newEventApp(&mockEventCrud{})

	req := httptest.NewRequest(http.MethodPatch, "/events/"+testEventID, bytes.NewReader([]byte(`{bad json`)))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)

	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("expected 422, got %d", resp.StatusCode)
	}
}

func TestUpdateEvent_NotFound(t *testing.T) {
	ctrl := &mockEventCrud{}
	ctrl.updateEventFunc = func(_ context.Context, _ entity.UpdateEventRequest) (*entity.UpdateEventResponse, error) {
		return nil, base.ErrNotFound
	}
	app := newEventApp(ctrl)

	body := []byte(`{"payloadGuidelines":"x"}`)
	req := httptest.NewRequest(http.MethodPatch, "/events/"+testEventID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestUpdateEvent_ControllerError(t *testing.T) {
	ctrl := &mockEventCrud{}
	ctrl.updateEventFunc = func(_ context.Context, _ entity.UpdateEventRequest) (*entity.UpdateEventResponse, error) {
		return nil, errors.New("db unavailable")
	}
	app := newEventApp(ctrl)

	body := []byte(`{"payloadGuidelines":"x"}`)
	req := httptest.NewRequest(http.MethodPatch, "/events/"+testEventID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", resp.StatusCode)
	}
}
