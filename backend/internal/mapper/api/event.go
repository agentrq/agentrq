package api

import (
	"encoding/json"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	view "github.com/agentrq/agentrq/backend/internal/data/view/api"
	"github.com/gofiber/fiber/v2"
	"github.com/mustafaturan/monoflake"
)

// ── Event HTTP mappers ────────────────────────────────────────────────────────

func FromHTTPRequestToCreateEventRequestEntity(c *fiber.Ctx) *entity.CreateEventRequest {
	var payload view.CreateEventRequest
	if err := json.Unmarshal(c.BodyRaw(), &payload); err != nil {
		return nil
	}
	if payload.Name == "" {
		return nil
	}
	return &entity.CreateEventRequest{
		Name:              payload.Name,
		PayloadGuidelines: payload.PayloadGuidelines,
	}
}

func FromCreateEventResponseEntityToHTTPResponse(rs *entity.CreateEventResponse) []byte {
	payload, _ := json.Marshal(view.CreateEventResponse{Event: fromEntityEventToView(rs.Event)})
	return payload
}

func FromHTTPRequestToGetEventRequestEntity(c *fiber.Ctx) *entity.GetEventRequest {
	id := monoflake.IDFromBase62(c.Params("id")).Int64()
	if id == 0 {
		return nil
	}
	return &entity.GetEventRequest{ID: id}
}

func FromGetEventResponseEntityToHTTPResponse(rs *entity.GetEventResponse) []byte {
	payload, _ := json.Marshal(view.GetEventResponse{Event: fromEntityEventToView(rs.Event)})
	return payload
}

func FromListEventsResponseEntityToHTTPResponse(rs *entity.ListEventsResponse) []byte {
	events := make([]view.Event, len(rs.Events))
	for i, e := range rs.Events {
		events[i] = fromEntityEventToView(e)
	}
	payload, _ := json.Marshal(view.ListEventsResponse{Events: events})
	return payload
}

func FromHTTPRequestToUpdateEventRequestEntity(c *fiber.Ctx) *entity.UpdateEventRequest {
	id := monoflake.IDFromBase62(c.Params("id")).Int64()
	if id == 0 {
		return nil
	}
	var payload view.UpdateEventRequest
	if err := json.Unmarshal(c.BodyRaw(), &payload); err != nil {
		return nil
	}
	return &entity.UpdateEventRequest{ID: id, PayloadGuidelines: payload.PayloadGuidelines}
}

func FromUpdateEventResponseEntityToHTTPResponse(rs *entity.UpdateEventResponse) []byte {
	payload, _ := json.Marshal(view.UpdateEventResponse{Event: fromEntityEventToView(rs.Event)})
	return payload
}

func FromHTTPRequestToDeleteEventRequestEntity(c *fiber.Ctx) *entity.DeleteEventRequest {
	id := monoflake.IDFromBase62(c.Params("id")).Int64()
	if id == 0 {
		return nil
	}
	return &entity.DeleteEventRequest{ID: id}
}

// ── EventTrigger HTTP mappers ─────────────────────────────────────────────────

func FromHTTPRequestToCreateEventTriggerRequestEntity(c *fiber.Ctx) *entity.CreateEventTriggerRequest {
	eventID := monoflake.IDFromBase62(c.Params("id")).Int64()
	if eventID == 0 {
		return nil
	}
	var payload view.CreateEventTriggerRequest
	if err := json.Unmarshal(c.BodyRaw(), &payload); err != nil {
		return nil
	}
	workspaceID := monoflake.IDFromBase62(payload.WorkspaceID).Int64()
	if payload.Title == "" || workspaceID == 0 {
		return nil
	}
	assignee := payload.Assignee
	if assignee == "" {
		assignee = "agent"
	}
	return &entity.CreateEventTriggerRequest{
		EventID:          eventID,
		WorkspaceID:      workspaceID,
		Title:            payload.Title,
		Body:             payload.Body,
		Assignee:         assignee,
		CronSchedule:     payload.CronSchedule,
		AllowAllCommands: payload.AllowAllCommands,
		EmitEventID:      monoflake.IDFromBase62(payload.EmitEventID).Int64(),
	}
}

func FromCreateEventTriggerResponseEntityToHTTPResponse(rs *entity.CreateEventTriggerResponse) []byte {
	payload, _ := json.Marshal(view.CreateEventTriggerResponse{EventTrigger: fromEntityEventTriggerToView(rs.EventTrigger)})
	return payload
}

func FromListEventTriggersResponseEntityToHTTPResponse(rs *entity.ListEventTriggersResponse) []byte {
	triggers := make([]view.EventTrigger, len(rs.EventTriggers))
	for i, t := range rs.EventTriggers {
		triggers[i] = fromEntityEventTriggerToView(t)
	}
	payload, _ := json.Marshal(view.ListEventTriggersResponse{EventTriggers: triggers})
	return payload
}

func FromHTTPRequestToDeleteEventTriggerRequestEntity(c *fiber.Ctx) *entity.DeleteEventTriggerRequest {
	id := monoflake.IDFromBase62(c.Params("triggerID")).Int64()
	if id == 0 {
		return nil
	}
	return &entity.DeleteEventTriggerRequest{ID: id}
}

func FromListTasksFromEventResponseEntityToHTTPResponse(rs *entity.ListTasksFromEventResponse) []byte {
	tasks := make([]view.Task, len(rs.Tasks))
	for i, t := range rs.Tasks {
		tasks[i] = FromEntityTaskToView(t)
	}
	payload, _ := json.Marshal(view.ListTasksResponse{Tasks: tasks})
	return payload
}

// ── Internal model↔entity mappers ─────────────────────────────────────────────

func FromModelEventToEntity(m model.Event) entity.Event {
	return entity.Event{
		ID:                m.ID,
		CreatedAt:         m.CreatedAt,
		UpdatedAt:         m.UpdatedAt,
		UserID:            m.UserID,
		Name:              m.Name,
		PayloadGuidelines: m.PayloadGuidelines,
	}
}

func FromModelEventTriggerToEntity(m model.EventTrigger) entity.EventTrigger {
	return entity.EventTrigger{
		ID:               m.ID,
		CreatedAt:        m.CreatedAt,
		UpdatedAt:        m.UpdatedAt,
		EventID:          m.EventID,
		WorkspaceID:      m.WorkspaceID,
		UserID:           m.UserID,
		Title:            m.Title,
		Body:             m.Body,
		Assignee:         m.Assignee,
		CronSchedule:     m.CronSchedule,
		AllowAllCommands: m.AllowAllCommands,
		EmitEventID:      m.EmitEventID,
	}
}

// ── Internal entity↔view mappers ──────────────────────────────────────────────

func fromEntityEventToView(e entity.Event) view.Event {
	return view.Event{
		ID:                monoflake.ID(e.ID).String(),
		CreatedAt:         e.CreatedAt,
		UpdatedAt:         e.UpdatedAt,
		Name:              e.Name,
		PayloadGuidelines: e.PayloadGuidelines,
	}
}

func fromEntityEventTriggerToView(t entity.EventTrigger) view.EventTrigger {
	v := view.EventTrigger{
		ID:               monoflake.ID(t.ID).String(),
		CreatedAt:        t.CreatedAt,
		EventID:          monoflake.ID(t.EventID).String(),
		WorkspaceID:      monoflake.ID(t.WorkspaceID).String(),
		Title:            t.Title,
		Body:             t.Body,
		Assignee:         t.Assignee,
		CronSchedule:     t.CronSchedule,
		AllowAllCommands: t.AllowAllCommands,
	}
	if t.EmitEventID != 0 {
		v.EmitEventID = monoflake.ID(t.EmitEventID).String()
	}
	return v
}
