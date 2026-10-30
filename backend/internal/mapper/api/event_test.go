// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"reflect"
	"testing"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	view "github.com/agentrq/agentrq/backend/internal/data/view/api"
)

func TestFromHTTPRequestToCreateEventRequestEntity(t *testing.T) {
	checkRequest(t, "/events", FromHTTPRequestToCreateEventRequestEntity, []requestCase[entity.CreateEventRequest]{
		{"name and guidelines", "/events", `{"name":"release.done","payloadGuidelines":"the tag"}`,
			&entity.CreateEventRequest{Name: "release.done", PayloadGuidelines: "the tag"}},
		{"guidelines are optional", "/events", `{"name":"ping"}`, &entity.CreateEventRequest{Name: "ping"}},
		{"no name", "/events", `{"payloadGuidelines":"x"}`, nil},
		{"not JSON", "/events", `{`, nil},
	})
}

func TestFromHTTPRequestToEventIDRequestEntities(t *testing.T) {
	route := "/events/:id"
	checkRequest(t, route, FromHTTPRequestToGetEventRequestEntity, []requestCase[entity.GetEventRequest]{
		{"an id", "/events/" + b62(10), ``, &entity.GetEventRequest{ID: 10}},
		{"id 0", "/events/0", ``, nil},
	})
	checkRequest(t, route, FromHTTPRequestToDeleteEventRequestEntity, []requestCase[entity.DeleteEventRequest]{
		{"an id", "/events/" + b62(10), ``, &entity.DeleteEventRequest{ID: 10}},
		{"id 0", "/events/0", ``, nil},
	})
	checkRequest(t, route, FromHTTPRequestToUpdateEventRequestEntity, []requestCase[entity.UpdateEventRequest]{
		{"new guidelines", "/events/" + b62(10), `{"payloadGuidelines":"the tag and notes"}`,
			&entity.UpdateEventRequest{ID: 10, PayloadGuidelines: "the tag and notes"}},
		{"guidelines cleared", "/events/" + b62(10), `{}`, &entity.UpdateEventRequest{ID: 10}},
		{"id 0", "/events/0", `{}`, nil},
		{"not JSON", "/events/" + b62(10), `[`, nil},
	})
}

func TestFromHTTPRequestToCreateEventTriggerRequestEntity(t *testing.T) {
	route := "/events/:id/triggers"
	target := "/events/" + b62(10) + "/triggers"
	checkRequest(t, route, FromHTTPRequestToCreateEventTriggerRequestEntity, []requestCase[entity.CreateEventTriggerRequest]{
		{"every field", target,
			`{"workspaceId":"` + b62(20) + `","title":"Ship","body":"{{EVENT_PAYLOAD}}","assignee":"human","cronSchedule":"0 9 * * *","allowAllCommands":true,"emitEventId":"` + b62(11) + `"}`,
			&entity.CreateEventTriggerRequest{EventID: 10, WorkspaceID: 20, Title: "Ship", Body: "{{EVENT_PAYLOAD}}", Assignee: "human",
				CronSchedule: "0 9 * * *", AllowAllCommands: true, EmitEventID: 11}},
		{"the agent is the default assignee and no event is emitted", target,
			`{"workspaceId":"` + b62(20) + `","title":"Ship"}`,
			&entity.CreateEventTriggerRequest{EventID: 10, WorkspaceID: 20, Title: "Ship", Assignee: "agent"}},
		{"no title", target, `{"workspaceId":"` + b62(20) + `"}`, nil},
		{"no workspace", target, `{"title":"Ship"}`, nil},
		{"event 0", "/events/0/triggers", `{"workspaceId":"` + b62(20) + `","title":"Ship"}`, nil},
		{"not JSON", target, `{`, nil},
	})
}

func TestFromHTTPRequestToUpdateEventTriggerRequestEntity(t *testing.T) {
	route := "/events/:id/triggers/:triggerID"
	target := "/events/" + b62(10) + "/triggers/" + b62(30)
	checkRequest(t, route, FromHTTPRequestToUpdateEventTriggerRequestEntity, []requestCase[entity.UpdateEventTriggerRequest]{
		{"every field", target,
			`{"workspaceId":"` + b62(20) + `","title":"Ship","body":"b","assignee":"human","cronSchedule":"@daily","allowAllCommands":true,"emitEventId":"` + b62(11) + `"}`,
			&entity.UpdateEventTriggerRequest{ID: 30, WorkspaceID: 20, Title: "Ship", Body: "b", Assignee: "human",
				CronSchedule: "@daily", AllowAllCommands: true, EmitEventID: 11}},
		{"the agent is the default assignee", target, `{"workspaceId":"` + b62(20) + `","title":"Ship"}`,
			&entity.UpdateEventTriggerRequest{ID: 30, WorkspaceID: 20, Title: "Ship", Assignee: "agent"}},
		{"no title", target, `{"workspaceId":"` + b62(20) + `"}`, nil},
		{"workspace 0", target, `{"workspaceId":"0","title":"Ship"}`, nil},
		{"trigger 0", "/events/" + b62(10) + "/triggers/0", `{"workspaceId":"` + b62(20) + `","title":"Ship"}`, nil},
		{"not JSON", target, `nope`, nil},
	})
	checkRequest(t, route, FromHTTPRequestToDeleteEventTriggerRequestEntity, []requestCase[entity.DeleteEventTriggerRequest]{
		{"a trigger", target, ``, &entity.DeleteEventTriggerRequest{ID: 30}},
		{"trigger 0", "/events/" + b62(10) + "/triggers/0", ``, nil},
	})
}

func TestEventResponses(t *testing.T) {
	ev := entity.Event{ID: 10, CreatedAt: at, UpdatedAt: at.Add(1), UserID: 9, Name: "release.done", PayloadGuidelines: "the tag"}
	// The user is never in the view.
	want := view.Event{ID: b62(10), CreatedAt: at, UpdatedAt: at.Add(1), Name: "release.done", PayloadGuidelines: "the tag"}

	checkResponse(t, FromCreateEventResponseEntityToHTTPResponse(&entity.CreateEventResponse{Event: ev}), view.CreateEventResponse{Event: want})
	checkResponse(t, FromGetEventResponseEntityToHTTPResponse(&entity.GetEventResponse{Event: ev}), view.GetEventResponse{Event: want})
	checkResponse(t, FromUpdateEventResponseEntityToHTTPResponse(&entity.UpdateEventResponse{Event: ev}), view.UpdateEventResponse{Event: want})
	checkResponse(t, FromListEventsResponseEntityToHTTPResponse(&entity.ListEventsResponse{Events: []entity.Event{ev, ev}}),
		view.ListEventsResponse{Events: []view.Event{want, want}})

	if got := string(FromListEventsResponseEntityToHTTPResponse(&entity.ListEventsResponse{})); got != `{"events":[]}` {
		t.Errorf("no events = %s, want an empty list", got)
	}
}

func TestEventTriggerResponses(t *testing.T) {
	leaf := entity.EventTrigger{ID: 30, CreatedAt: at, UpdatedAt: at.Add(1), EventID: 10, WorkspaceID: 20, UserID: 9,
		Title: "Ship", Body: "b", Assignee: "agent", CronSchedule: "@daily", AllowAllCommands: true}
	chained := leaf
	chained.EmitEventID = 11

	wantLeaf := view.EventTrigger{ID: b62(30), CreatedAt: at, EventID: b62(10), WorkspaceID: b62(20),
		Title: "Ship", Body: "b", Assignee: "agent", CronSchedule: "@daily", AllowAllCommands: true}
	wantChained := wantLeaf
	wantChained.EmitEventID = b62(11)

	checkResponse(t, FromCreateEventTriggerResponseEntityToHTTPResponse(&entity.CreateEventTriggerResponse{EventTrigger: chained}),
		view.CreateEventTriggerResponse{EventTrigger: wantChained})
	checkResponse(t, FromUpdateEventTriggerResponseEntityToHTTPResponse(&entity.UpdateEventTriggerResponse{EventTrigger: chained}),
		view.UpdateEventTriggerResponse{EventTrigger: wantChained})
	checkResponse(t, FromGetEventTriggerResponseEntityToHTTPResponse(&entity.GetEventTriggerResponse{EventTrigger: leaf}),
		view.GetEventTriggerResponse{EventTrigger: wantLeaf})
	raw := FromListEventTriggersResponseEntityToHTTPResponse(&entity.ListEventTriggersResponse{EventTriggers: []entity.EventTrigger{leaf, chained}})
	checkResponse(t, raw, view.ListEventTriggersResponse{EventTriggers: []view.EventTrigger{wantLeaf, wantChained}})
	if got := decodeResponse[map[string][]map[string]any](t, raw)["eventTriggers"][0]; got["emitEventId"] != nil {
		t.Errorf("a trigger that emits nothing carries emitEventId %v", got["emitEventId"])
	}
}

func TestFromListTasksFromEventResponseEntityToHTTPResponse(t *testing.T) {
	raw := FromListTasksFromEventResponseEntityToHTTPResponse(&entity.ListTasksFromEventResponse{
		Tasks: []entity.Task{{ID: 1, WorkspaceID: 20, EventID: 10, Title: "Ship", Status: "notstarted"}},
	})
	checkResponse(t, raw, view.ListTasksResponse{Tasks: []view.Task{{
		ID: b62(1), WorkspaceID: b62(20), EventID: b62(10), Title: "Ship", Status: "notstarted",
	}}})
}

func TestFromModelEventToEntity(t *testing.T) {
	got := FromModelEventToEntity(model.Event{ID: 10, CreatedAt: at, UpdatedAt: at.Add(1), UserID: 9, Name: "n", PayloadGuidelines: "g"})
	want := entity.Event{ID: 10, CreatedAt: at, UpdatedAt: at.Add(1), UserID: 9, Name: "n", PayloadGuidelines: "g"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestFromModelEventTriggerToEntity(t *testing.T) {
	got := FromModelEventTriggerToEntity(model.EventTrigger{ID: 30, CreatedAt: at, UpdatedAt: at.Add(1), EventID: 10, WorkspaceID: 20,
		UserID: 9, Title: "t", Body: "b", Assignee: "human", CronSchedule: "@daily", AllowAllCommands: true, EmitEventID: 11})
	want := entity.EventTrigger{ID: 30, CreatedAt: at, UpdatedAt: at.Add(1), EventID: 10, WorkspaceID: 20,
		UserID: 9, Title: "t", Body: "b", Assignee: "human", CronSchedule: "@daily", AllowAllCommands: true, EmitEventID: 11}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}
