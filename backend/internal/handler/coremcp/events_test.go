package coremcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/mustafaturan/monoflake"
)

// ── mock crud controller ──────────────────────────────────────────────────────
//
// Embedding the interface keeps the mock to the methods these tools call: any
// other method panics rather than silently returning a zero value.

type mockEventCrud struct {
	crud.Controller

	createEvent        func(ctx context.Context, req entity.CreateEventRequest) (*entity.CreateEventResponse, error)
	getEvent           func(ctx context.Context, req entity.GetEventRequest) (*entity.GetEventResponse, error)
	listEvents         func(ctx context.Context, req entity.ListEventsRequest) (*entity.ListEventsResponse, error)
	updateEvent        func(ctx context.Context, req entity.UpdateEventRequest) (*entity.UpdateEventResponse, error)
	deleteEvent        func(ctx context.Context, req entity.DeleteEventRequest) error
	createTrigger      func(ctx context.Context, req entity.CreateEventTriggerRequest) (*entity.CreateEventTriggerResponse, error)
	getTrigger         func(ctx context.Context, req entity.GetEventTriggerRequest) (*entity.GetEventTriggerResponse, error)
	listTriggers       func(ctx context.Context, req entity.ListEventTriggersRequest) (*entity.ListEventTriggersResponse, error)
	updateTrigger      func(ctx context.Context, req entity.UpdateEventTriggerRequest) (*entity.UpdateEventTriggerResponse, error)
	deleteTrigger      func(ctx context.Context, req entity.DeleteEventTriggerRequest) error
	listTasksFromEvent func(ctx context.Context, req entity.ListTasksFromEventRequest) (*entity.ListTasksFromEventResponse, error)
}

func (m *mockEventCrud) CreateEvent(ctx context.Context, req entity.CreateEventRequest) (*entity.CreateEventResponse, error) {
	return m.createEvent(ctx, req)
}
func (m *mockEventCrud) GetEvent(ctx context.Context, req entity.GetEventRequest) (*entity.GetEventResponse, error) {
	return m.getEvent(ctx, req)
}
func (m *mockEventCrud) ListEvents(ctx context.Context, req entity.ListEventsRequest) (*entity.ListEventsResponse, error) {
	return m.listEvents(ctx, req)
}
func (m *mockEventCrud) UpdateEvent(ctx context.Context, req entity.UpdateEventRequest) (*entity.UpdateEventResponse, error) {
	return m.updateEvent(ctx, req)
}
func (m *mockEventCrud) DeleteEvent(ctx context.Context, req entity.DeleteEventRequest) error {
	return m.deleteEvent(ctx, req)
}
func (m *mockEventCrud) CreateEventTrigger(ctx context.Context, req entity.CreateEventTriggerRequest) (*entity.CreateEventTriggerResponse, error) {
	return m.createTrigger(ctx, req)
}
func (m *mockEventCrud) GetEventTrigger(ctx context.Context, req entity.GetEventTriggerRequest) (*entity.GetEventTriggerResponse, error) {
	return m.getTrigger(ctx, req)
}
func (m *mockEventCrud) ListEventTriggers(ctx context.Context, req entity.ListEventTriggersRequest) (*entity.ListEventTriggersResponse, error) {
	return m.listTriggers(ctx, req)
}
func (m *mockEventCrud) UpdateEventTrigger(ctx context.Context, req entity.UpdateEventTriggerRequest) (*entity.UpdateEventTriggerResponse, error) {
	return m.updateTrigger(ctx, req)
}
func (m *mockEventCrud) DeleteEventTrigger(ctx context.Context, req entity.DeleteEventTriggerRequest) error {
	return m.deleteTrigger(ctx, req)
}
func (m *mockEventCrud) ListTasksFromEvent(ctx context.Context, req entity.ListTasksFromEventRequest) (*entity.ListTasksFromEventResponse, error) {
	return m.listTasksFromEvent(ctx, req)
}

const (
	testUserID    = "user1"
	testEventID   = int64(100)
	testTriggerID = int64(200)
	testWorkspace = int64(300)
	testEmitEvent = int64(400)
)

func eventServer(ctrl *mockEventCrud) *WorkspaceServer {
	return &WorkspaceServer{crud: ctrl}
}

// The tools are reached with the user the transport authenticated, which the
// core MCP handler puts on the context — every request is scoped to it.
func authedContext() context.Context {
	return context.WithValue(context.Background(), "user_id", testUserID) //nolint:staticcheck // the handler uses this key
}

func base62(id int64) string {
	return monoflake.ID(id).String()
}

// callResult is what a tool answered, flattened: the SDK's result type carries
// the text in a slice of content parts, and every assertion here is about that
// text and whether the tool reported a failure.
type callResult struct {
	text    string
	isError bool
}

// toolResult adapts a handler's three return values. Written to take them
// positionally so a call reads `toolResult(srv.handleX(...))`.
func toolResult(res *mcp.CallToolResult, _ any, err error) callResult {
	// Handlers answer with an error *result* rather than a Go error, so this
	// only fires if one starts doing otherwise.
	if err != nil {
		return callResult{text: err.Error(), isError: true}
	}
	if len(res.Content) == 0 {
		return callResult{isError: res.IsError}
	}
	text, _ := res.Content[0].(*mcp.TextContent)
	return callResult{text: text.Text, isError: res.IsError}
}

func textOf(t *testing.T, result callResult) string {
	t.Helper()
	if result.isError {
		t.Fatalf("tool reported an error: %s", result.text)
	}
	return result.text
}

// ── event tools ───────────────────────────────────────────────────────────────

func TestListEvents_ScopesToTheAuthenticatedUser(t *testing.T) {
	var got entity.ListEventsRequest
	ctrl := &mockEventCrud{listEvents: func(_ context.Context, req entity.ListEventsRequest) (*entity.ListEventsResponse, error) {
		got = req
		return &entity.ListEventsResponse{Events: []entity.Event{{ID: testEventID, Name: "deploy_done"}}}, nil
	}}

	body := textOf(t, toolResult(eventServer(ctrl).handleListEvents(authedContext(), nil, struct{}{})))

	if got.UserID != testUserID {
		t.Errorf("UserID = %q, want %q", got.UserID, testUserID)
	}
	var payload struct {
		Events []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"events"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Events) != 1 || payload.Events[0].Name != "deploy_done" {
		t.Fatalf("events = %+v", payload.Events)
	}
	// IDs cross the wire as base62, the way every other tool answers.
	if payload.Events[0].ID != base62(testEventID) {
		t.Errorf("id = %q, want %q", payload.Events[0].ID, base62(testEventID))
	}
}

func TestListEvents_ReportsAFailure(t *testing.T) {
	ctrl := &mockEventCrud{listEvents: func(context.Context, entity.ListEventsRequest) (*entity.ListEventsResponse, error) {
		return nil, errors.New("database unavailable")
	}}

	result := toolResult(eventServer(ctrl).handleListEvents(authedContext(), nil, struct{}{}))

	if !result.isError || result.text != "database unavailable" {
		t.Fatalf("result = %+v", result)
	}
}

func TestCreateEvent_PassesTheNameAndGuidelines(t *testing.T) {
	var got entity.CreateEventRequest
	ctrl := &mockEventCrud{createEvent: func(_ context.Context, req entity.CreateEventRequest) (*entity.CreateEventResponse, error) {
		got = req
		return &entity.CreateEventResponse{Event: entity.Event{ID: testEventID, Name: req.Name}}, nil
	}}

	textOf(t, toolResult(eventServer(ctrl).handleCreateEvent(authedContext(), nil, CreateEventParams{
		Name:              "deploy_done",
		PayloadGuidelines: "what shipped, and where",
	})))

	if got.Name != "deploy_done" || got.PayloadGuidelines != "what shipped, and where" || got.UserID != testUserID {
		t.Fatalf("request = %+v", got)
	}
}

// A name that is already taken, or malformed, is the controller's call — the
// tool reports what it said rather than inventing its own answer.
func TestCreateEvent_ReportsTheControllersRefusal(t *testing.T) {
	ctrl := &mockEventCrud{createEvent: func(context.Context, entity.CreateEventRequest) (*entity.CreateEventResponse, error) {
		return nil, errors.New(`duplicate name: "deploy_done"`)
	}}

	result := toolResult(eventServer(ctrl).handleCreateEvent(authedContext(), nil, CreateEventParams{Name: "deploy_done"}))

	if !result.isError {
		t.Fatal("expected an error result")
	}
	if result.text != `duplicate name: "deploy_done"` {
		t.Errorf("text = %q", result.text)
	}
}

func TestGetEvent_ReadsTheBase62ID(t *testing.T) {
	var got entity.GetEventRequest
	ctrl := &mockEventCrud{getEvent: func(_ context.Context, req entity.GetEventRequest) (*entity.GetEventResponse, error) {
		got = req
		return &entity.GetEventResponse{Event: entity.Event{ID: req.ID, Name: "deploy_done"}}, nil
	}}

	textOf(t, toolResult(eventServer(ctrl).handleGetEvent(authedContext(), nil, GetEventParams{EventID: base62(testEventID)})))

	if got.ID != testEventID {
		t.Errorf("ID = %d, want %d", got.ID, testEventID)
	}
}

func TestGetEvent_ReportsAMissingEvent(t *testing.T) {
	ctrl := &mockEventCrud{getEvent: func(context.Context, entity.GetEventRequest) (*entity.GetEventResponse, error) {
		return nil, errors.New("record not found")
	}}

	result := toolResult(eventServer(ctrl).handleGetEvent(authedContext(), nil, GetEventParams{EventID: base62(testEventID)}))

	if !result.isError || result.text != "record not found" {
		t.Fatalf("result = %+v", result)
	}
}

func TestUpdateEvent_RevisesTheGuidelines(t *testing.T) {
	var got entity.UpdateEventRequest
	ctrl := &mockEventCrud{updateEvent: func(_ context.Context, req entity.UpdateEventRequest) (*entity.UpdateEventResponse, error) {
		got = req
		return &entity.UpdateEventResponse{Event: entity.Event{ID: req.ID, PayloadGuidelines: req.PayloadGuidelines}}, nil
	}}

	body := textOf(t, toolResult(eventServer(ctrl).handleUpdateEvent(authedContext(), nil, UpdateEventParams{
		EventID:           base62(testEventID),
		PayloadGuidelines: "name the service and the version",
	})))

	if got.ID != testEventID || got.PayloadGuidelines != "name the service and the version" {
		t.Fatalf("request = %+v", got)
	}
	if !strings.Contains(body, "name the service and the version") {
		t.Errorf("body = %s", body)
	}
}

func TestUpdateEvent_ReportsAFailure(t *testing.T) {
	ctrl := &mockEventCrud{updateEvent: func(context.Context, entity.UpdateEventRequest) (*entity.UpdateEventResponse, error) {
		return nil, errors.New("invalid userID")
	}}

	result := toolResult(eventServer(ctrl).handleUpdateEvent(authedContext(), nil, UpdateEventParams{EventID: base62(testEventID)}))

	if !result.isError {
		t.Fatal("expected an error result")
	}
}

func TestDeleteEvent_SaysSo(t *testing.T) {
	var got entity.DeleteEventRequest
	ctrl := &mockEventCrud{deleteEvent: func(_ context.Context, req entity.DeleteEventRequest) error {
		got = req
		return nil
	}}

	body := textOf(t, toolResult(eventServer(ctrl).handleDeleteEvent(authedContext(), nil, DeleteEventParams{EventID: base62(testEventID)})))

	if got.ID != testEventID || got.UserID != testUserID {
		t.Fatalf("request = %+v", got)
	}
	if body != "event deleted" {
		t.Errorf("text = %q", body)
	}
}

func TestDeleteEvent_ReportsAFailure(t *testing.T) {
	ctrl := &mockEventCrud{deleteEvent: func(context.Context, entity.DeleteEventRequest) error {
		return errors.New("record not found")
	}}

	result := toolResult(eventServer(ctrl).handleDeleteEvent(authedContext(), nil, DeleteEventParams{EventID: base62(testEventID)}))

	if !result.isError || result.text != "record not found" {
		t.Fatalf("result = %+v", result)
	}
}

// ── trigger tools ─────────────────────────────────────────────────────────────

func TestCreateEventTrigger_CarriesTheWholeWiring(t *testing.T) {
	var got entity.CreateEventTriggerRequest
	ctrl := &mockEventCrud{createTrigger: func(_ context.Context, req entity.CreateEventTriggerRequest) (*entity.CreateEventTriggerResponse, error) {
		got = req
		return &entity.CreateEventTriggerResponse{EventTrigger: entity.EventTrigger{
			ID: testTriggerID, EventID: req.EventID, WorkspaceID: req.WorkspaceID, Title: req.Title, EmitEventID: req.EmitEventID,
		}}, nil
	}}

	body := textOf(t, toolResult(eventServer(ctrl).handleCreateEventTrigger(authedContext(), nil, CreateEventTriggerParams{
		EventID:          base62(testEventID),
		WorkspaceID:      base62(testWorkspace),
		Title:            "Review the deploy",
		Body:             "It shipped: {{EVENT_PAYLOAD}}",
		Assignee:         "human",
		CronSchedule:     "30 * * * *",
		AllowAllCommands: true,
		EmitEventID:      base62(testEmitEvent),
	})))

	want := entity.CreateEventTriggerRequest{
		EventID:          testEventID,
		WorkspaceID:      testWorkspace,
		Title:            "Review the deploy",
		Body:             "It shipped: {{EVENT_PAYLOAD}}",
		Assignee:         "human",
		CronSchedule:     "30 * * * *",
		AllowAllCommands: true,
		EmitEventID:      testEmitEvent,
		UserID:           testUserID,
	}
	if got != want {
		t.Fatalf("request = %+v, want %+v", got, want)
	}
	if !strings.Contains(body, base62(testEmitEvent)) {
		t.Errorf("the chained event is missing from the answer: %s", body)
	}
}

// A trigger exists to make something happen without being asked, so one with
// no assignee is for an agent — the same default the REST API applies.
func TestCreateEventTrigger_DefaultsToTheAgent(t *testing.T) {
	var got entity.CreateEventTriggerRequest
	ctrl := &mockEventCrud{createTrigger: func(_ context.Context, req entity.CreateEventTriggerRequest) (*entity.CreateEventTriggerResponse, error) {
		got = req
		return &entity.CreateEventTriggerResponse{EventTrigger: entity.EventTrigger{ID: testTriggerID}}, nil
	}}

	textOf(t, toolResult(eventServer(ctrl).handleCreateEventTrigger(authedContext(), nil, CreateEventTriggerParams{
		EventID:     base62(testEventID),
		WorkspaceID: base62(testWorkspace),
		Title:       "Review the deploy",
	})))

	if got.Assignee != "agent" {
		t.Errorf("Assignee = %q, want agent", got.Assignee)
	}
	// An omitted chain is no chain, not event zero.
	if got.EmitEventID != 0 {
		t.Errorf("EmitEventID = %d, want 0", got.EmitEventID)
	}
}

func TestCreateEventTrigger_ReportsARejectedSchedule(t *testing.T) {
	ctrl := &mockEventCrud{createTrigger: func(context.Context, entity.CreateEventTriggerRequest) (*entity.CreateEventTriggerResponse, error) {
		return nil, errors.New("cron schedule must be hourly or less frequent")
	}}

	result := toolResult(eventServer(ctrl).handleCreateEventTrigger(authedContext(), nil, CreateEventTriggerParams{
		EventID: base62(testEventID), WorkspaceID: base62(testWorkspace), Title: "t", CronSchedule: "* * * * *",
	}))

	if !result.isError || !strings.Contains(result.text, "hourly") {
		t.Fatalf("result = %+v", result)
	}
}

func TestListEventTriggers_ListsWhatAnEventDoes(t *testing.T) {
	var got entity.ListEventTriggersRequest
	ctrl := &mockEventCrud{listTriggers: func(_ context.Context, req entity.ListEventTriggersRequest) (*entity.ListEventTriggersResponse, error) {
		got = req
		return &entity.ListEventTriggersResponse{EventTriggers: []entity.EventTrigger{
			{ID: testTriggerID, EventID: req.EventID, WorkspaceID: testWorkspace, Title: "Review the deploy"},
		}}, nil
	}}

	body := textOf(t, toolResult(eventServer(ctrl).handleListEventTriggers(authedContext(), nil, ListEventTriggersParams{EventID: base62(testEventID)})))

	if got.EventID != testEventID || got.UserID != testUserID {
		t.Fatalf("request = %+v", got)
	}
	if !strings.Contains(body, "Review the deploy") {
		t.Errorf("body = %s", body)
	}
}

func TestListEventTriggers_ReportsAFailure(t *testing.T) {
	ctrl := &mockEventCrud{listTriggers: func(context.Context, entity.ListEventTriggersRequest) (*entity.ListEventTriggersResponse, error) {
		return nil, errors.New("record not found")
	}}

	result := toolResult(eventServer(ctrl).handleListEventTriggers(authedContext(), nil, ListEventTriggersParams{EventID: base62(testEventID)}))

	if !result.isError {
		t.Fatal("expected an error result")
	}
}

// The REST API reads triggers an event at a time; an agent holding a trigger
// ID has nothing to list, which is what this tool is for.
func TestGetEventTrigger_ReadsOneByID(t *testing.T) {
	var got entity.GetEventTriggerRequest
	ctrl := &mockEventCrud{getTrigger: func(_ context.Context, req entity.GetEventTriggerRequest) (*entity.GetEventTriggerResponse, error) {
		got = req
		return &entity.GetEventTriggerResponse{EventTrigger: entity.EventTrigger{
			ID: req.ID, EventID: testEventID, WorkspaceID: testWorkspace, Title: "Review the deploy",
		}}, nil
	}}

	body := textOf(t, toolResult(eventServer(ctrl).handleGetEventTrigger(authedContext(), nil, GetEventTriggerParams{TriggerID: base62(testTriggerID)})))

	if got.ID != testTriggerID || got.UserID != testUserID {
		t.Fatalf("request = %+v", got)
	}
	var payload struct {
		EventTrigger struct {
			ID          string `json:"id"`
			WorkspaceID string `json:"workspaceId"`
			Title       string `json:"title"`
		} `json:"eventTrigger"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.EventTrigger.ID != base62(testTriggerID) || payload.EventTrigger.WorkspaceID != base62(testWorkspace) {
		t.Fatalf("trigger = %+v", payload.EventTrigger)
	}
}

func TestGetEventTrigger_ReportsAMissingTrigger(t *testing.T) {
	ctrl := &mockEventCrud{getTrigger: func(context.Context, entity.GetEventTriggerRequest) (*entity.GetEventTriggerResponse, error) {
		return nil, errors.New("record not found")
	}}

	result := toolResult(eventServer(ctrl).handleGetEventTrigger(authedContext(), nil, GetEventTriggerParams{TriggerID: base62(testTriggerID)}))

	if !result.isError {
		t.Fatal("expected an error result")
	}
}

func TestUpdateEventTrigger_RewritesEveryField(t *testing.T) {
	var got entity.UpdateEventTriggerRequest
	ctrl := &mockEventCrud{updateTrigger: func(_ context.Context, req entity.UpdateEventTriggerRequest) (*entity.UpdateEventTriggerResponse, error) {
		got = req
		return &entity.UpdateEventTriggerResponse{EventTrigger: entity.EventTrigger{ID: req.ID, Title: req.Title}}, nil
	}}

	textOf(t, toolResult(eventServer(ctrl).handleUpdateEventTrigger(authedContext(), nil, UpdateEventTriggerParams{
		TriggerID:   base62(testTriggerID),
		WorkspaceID: base62(testWorkspace),
		Title:       "Review the deploy, carefully",
		Body:        "{{EVENT_FAQ}}",
		Assignee:    "human",
		EmitEventID: base62(testEmitEvent),
	})))

	want := entity.UpdateEventTriggerRequest{
		ID:          testTriggerID,
		UserID:      testUserID,
		WorkspaceID: testWorkspace,
		Title:       "Review the deploy, carefully",
		Body:        "{{EVENT_FAQ}}",
		Assignee:    "human",
		EmitEventID: testEmitEvent,
	}
	if got != want {
		t.Fatalf("request = %+v, want %+v", got, want)
	}
}

func TestUpdateEventTrigger_DefaultsToTheAgentAndReportsFailure(t *testing.T) {
	var got entity.UpdateEventTriggerRequest
	ctrl := &mockEventCrud{updateTrigger: func(_ context.Context, req entity.UpdateEventTriggerRequest) (*entity.UpdateEventTriggerResponse, error) {
		got = req
		return nil, errors.New("title is required")
	}}

	result := toolResult(eventServer(ctrl).handleUpdateEventTrigger(authedContext(), nil, UpdateEventTriggerParams{
		TriggerID: base62(testTriggerID), WorkspaceID: base62(testWorkspace),
	}))

	if got.Assignee != "agent" {
		t.Errorf("Assignee = %q, want agent", got.Assignee)
	}
	if !result.isError || result.text != "title is required" {
		t.Fatalf("result = %+v", result)
	}
}

func TestDeleteEventTrigger_LeavesTheEventInPlace(t *testing.T) {
	var got entity.DeleteEventTriggerRequest
	ctrl := &mockEventCrud{deleteTrigger: func(_ context.Context, req entity.DeleteEventTriggerRequest) error {
		got = req
		return nil
	}}

	body := textOf(t, toolResult(eventServer(ctrl).handleDeleteEventTrigger(authedContext(), nil, DeleteEventTriggerParams{TriggerID: base62(testTriggerID)})))

	if got.ID != testTriggerID || got.UserID != testUserID {
		t.Fatalf("request = %+v", got)
	}
	if body != "event trigger deleted" {
		t.Errorf("text = %q", body)
	}
}

func TestDeleteEventTrigger_ReportsAFailure(t *testing.T) {
	ctrl := &mockEventCrud{deleteTrigger: func(context.Context, entity.DeleteEventTriggerRequest) error {
		return errors.New("record not found")
	}}

	result := toolResult(eventServer(ctrl).handleDeleteEventTrigger(authedContext(), nil, DeleteEventTriggerParams{TriggerID: base62(testTriggerID)}))

	if !result.isError {
		t.Fatal("expected an error result")
	}
}

func TestListEventTasks_ShowsWhatTheEventHasSpawned(t *testing.T) {
	var got entity.ListTasksFromEventRequest
	ctrl := &mockEventCrud{listTasksFromEvent: func(_ context.Context, req entity.ListTasksFromEventRequest) (*entity.ListTasksFromEventResponse, error) {
		got = req
		return &entity.ListTasksFromEventResponse{Tasks: []entity.Task{{ID: 500, Title: "Review the deploy", Status: "notstarted"}}}, nil
	}}

	body := textOf(t, toolResult(eventServer(ctrl).handleListEventTasks(authedContext(), nil, ListEventTasksParams{EventID: base62(testEventID)})))

	if got.EventID != testEventID || got.UserID != testUserID {
		t.Fatalf("request = %+v", got)
	}
	if !strings.Contains(body, "Review the deploy") {
		t.Errorf("body = %s", body)
	}
}

func TestListEventTasks_ReportsAFailure(t *testing.T) {
	ctrl := &mockEventCrud{listTasksFromEvent: func(context.Context, entity.ListTasksFromEventRequest) (*entity.ListTasksFromEventResponse, error) {
		return nil, errors.New("record not found")
	}}

	result := toolResult(eventServer(ctrl).handleListEventTasks(authedContext(), nil, ListEventTasksParams{EventID: base62(testEventID)}))

	if !result.isError {
		t.Fatal("expected an error result")
	}
}

// ── registration ──────────────────────────────────────────────────────────────

// A handler nothing registers is a handler no agent can reach, which no test
// above would notice.
func TestEventToolsAreRegistered(t *testing.T) {
	// NewServer is what an agent actually talks to, and it is also where the
	// SDK builds each tool's input schema from its params struct — a malformed
	// one fails here rather than at the first call.
	srv := NewServer(&mockEventCrud{}, "https://agentrq.example")

	ctx := context.Background()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := srv.server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("connect server: %v", err)
	}
	defer serverSession.Close()

	clientSession, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).
		Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connect client: %v", err)
	}
	defer clientSession.Close()

	listed, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	registered := make(map[string]string, len(listed.Tools))
	for _, tool := range listed.Tools {
		registered[tool.Name] = tool.Description
	}

	want := []string{
		"listEvents", "createEvent", "getEvent", "updateEvent", "deleteEvent",
		"createEventTrigger", "listEventTriggers", "getEventTrigger",
		"updateEventTrigger", "deleteEventTrigger", "listEventTasks",
	}
	for _, name := range want {
		description, ok := registered[name]
		if !ok {
			t.Errorf("tool %q is not registered", name)
			continue
		}
		// The description is the only place an agent learns what an event is
		// for, so an empty one makes the tool unusable in practice.
		if description == "" {
			t.Errorf("tool %q has no description", name)
		}
	}

	// The tools an agent already had are untouched.
	if _, ok := registered["createTask"]; !ok {
		t.Error("createTask went missing")
	}
}
