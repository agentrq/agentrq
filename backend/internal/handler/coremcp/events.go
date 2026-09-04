package coremcp

import (
	"context"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	apiMapper "github.com/agentrq/agentrq/backend/internal/mapper/api"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Events and their triggers, over MCP.
//
// A supervisor could already create workspaces and put tasks in them, but not
// the thing that turns those into a system: an event is a named signal one
// workspace publishes, and a trigger is a standing instruction to create a
// task somewhere else when it fires. Without these tools every hand-off had to
// be arranged by the supervisor itself, task by task.
//
// Each handler is a thin wrapper over the same controller method the REST API
// calls, so an agent gets no path the UI does not have and no validation the
// UI does not enforce: the event-name format, cron granularity, ownership of
// the target workspace, and the existence of a chained event are all checked
// in the controller.

// ── Params ────────────────────────────────────────────────────────────────────

type CreateEventParams struct {
	Name              string `json:"name" jsonschema:"Event name, lowercase and unique for this account: ^[a-z][a-z0-9_]{0,128}$"`
	PayloadGuidelines string `json:"payloadGuidelines,omitempty" jsonschema:"What a publisher should put in the payload. Shown to the agent that publishes this event"`
}

type GetEventParams struct {
	EventID string `json:"eventId" jsonschema:"Event ID (base62)"`
}

type UpdateEventParams struct {
	EventID           string `json:"eventId"`
	PayloadGuidelines string `json:"payloadGuidelines" jsonschema:"Replaces the current guidelines. The event's name cannot be changed"`
}

type DeleteEventParams struct {
	EventID string `json:"eventId"`
}

type CreateEventTriggerParams struct {
	EventID          string `json:"eventId" jsonschema:"The event this trigger listens to"`
	WorkspaceID      string `json:"workspaceId" jsonschema:"The workspace the task is created in"`
	Title            string `json:"title" jsonschema:"Task title, used exactly as written: placeholders are not substituted here"`
	Body             string `json:"body,omitempty" jsonschema:"Task body. {{EVENT_PAYLOAD}} and {{EVENT_FAQ}} are replaced with what the publisher sent"`
	Assignee         string `json:"assignee,omitempty" jsonschema:"enum: agent, human. Defaults to agent"`
	CronSchedule     string `json:"cronSchedule,omitempty" jsonschema:"Optional cron schedule for the spawned task. Hourly granularity at most: the minute field must be a single fixed number"`
	AllowAllCommands bool   `json:"allowAllCommands,omitempty" jsonschema:"Let the spawned task run commands without asking for permission"`
	EmitEventID      string `json:"emitEventId,omitempty" jsonschema:"Event to publish when the spawned task completes. This is how one system hands off to the next"`
}

type ListEventTriggersParams struct {
	EventID string `json:"eventId"`
}

type GetEventTriggerParams struct {
	TriggerID string `json:"triggerId" jsonschema:"Event trigger ID (base62)"`
}

type UpdateEventTriggerParams struct {
	TriggerID        string `json:"triggerId"`
	WorkspaceID      string `json:"workspaceId"`
	Title            string `json:"title"`
	Body             string `json:"body,omitempty"`
	Assignee         string `json:"assignee,omitempty" jsonschema:"enum: agent, human. Defaults to agent"`
	CronSchedule     string `json:"cronSchedule,omitempty"`
	AllowAllCommands bool   `json:"allowAllCommands,omitempty"`
	EmitEventID      string `json:"emitEventId,omitempty"`
}

type DeleteEventTriggerParams struct {
	TriggerID string `json:"triggerId"`
}

type ListEventTasksParams struct {
	EventID string `json:"eventId"`
}

// ── Tool definitions ──────────────────────────────────────────────────────────

func (s *WorkspaceServer) registerEventTools() {
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "listEvents",
		Description: "List the events defined for this account. An event is a named signal a workspace publishes when something happens",
	}, s.handleListEvents)
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "createEvent",
		Description: "Define a named signal that workspaces can publish and triggers can react to",
	}, s.handleCreateEvent)
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "getEvent",
		Description: "Get an event by ID",
	}, s.handleGetEvent)
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "updateEvent",
		Description: "Revise an event's payload guidelines. Its name is fixed once created",
	}, s.handleUpdateEvent)
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "deleteEvent",
		Description: "Delete an event. Its triggers stop firing",
	}, s.handleDeleteEvent)
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "createEventTrigger",
		Description: "React to an event: when it fires, create a task in a workspace. The body may carry {{EVENT_PAYLOAD}} and {{EVENT_FAQ}}, and emitEventId chains a second event to the task's completion",
	}, s.handleCreateEventTrigger)
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "listEventTriggers",
		Description: "List the triggers attached to an event — everything that happens when it fires",
	}, s.handleListEventTriggers)
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "getEventTrigger",
		Description: "Get an event trigger by ID",
	}, s.handleGetEventTrigger)
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "updateEventTrigger",
		Description: "Rewrite an event trigger. Every field is written as given, so send the ones to keep as well",
	}, s.handleUpdateEventTrigger)
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "deleteEventTrigger",
		Description: "Delete an event trigger, leaving its event in place",
	}, s.handleDeleteEventTrigger)
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "listEventTasks",
		Description: "List the tasks an event has spawned, to see whether a system that was wired up is running",
	}, s.handleListEventTasks)
}

// ── Handlers ──────────────────────────────────────────────────────────────────

func (s *WorkspaceServer) handleListEvents(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error) {
	res, err := s.crud.ListEvents(ctx, entity.ListEventsRequest{UserID: getUserID(ctx)})
	if err != nil {
		return errorResponse(err), nil, nil
	}
	return textResponse(string(apiMapper.FromListEventsResponseEntityToHTTPResponse(res))), nil, nil
}

func (s *WorkspaceServer) handleCreateEvent(ctx context.Context, req *mcp.CallToolRequest, args CreateEventParams) (*mcp.CallToolResult, any, error) {
	res, err := s.crud.CreateEvent(ctx, entity.CreateEventRequest{
		UserID:            getUserID(ctx),
		Name:              args.Name,
		PayloadGuidelines: args.PayloadGuidelines,
	})
	if err != nil {
		return errorResponse(err), nil, nil
	}
	return textResponse(string(apiMapper.FromCreateEventResponseEntityToHTTPResponse(res))), nil, nil
}

func (s *WorkspaceServer) handleGetEvent(ctx context.Context, req *mcp.CallToolRequest, args GetEventParams) (*mcp.CallToolResult, any, error) {
	res, err := s.crud.GetEvent(ctx, entity.GetEventRequest{
		UserID: getUserID(ctx),
		ID:     parseID(args.EventID),
	})
	if err != nil {
		return errorResponse(err), nil, nil
	}
	return textResponse(string(apiMapper.FromGetEventResponseEntityToHTTPResponse(res))), nil, nil
}

func (s *WorkspaceServer) handleUpdateEvent(ctx context.Context, req *mcp.CallToolRequest, args UpdateEventParams) (*mcp.CallToolResult, any, error) {
	res, err := s.crud.UpdateEvent(ctx, entity.UpdateEventRequest{
		UserID:            getUserID(ctx),
		ID:                parseID(args.EventID),
		PayloadGuidelines: args.PayloadGuidelines,
	})
	if err != nil {
		return errorResponse(err), nil, nil
	}
	return textResponse(string(apiMapper.FromUpdateEventResponseEntityToHTTPResponse(res))), nil, nil
}

func (s *WorkspaceServer) handleDeleteEvent(ctx context.Context, req *mcp.CallToolRequest, args DeleteEventParams) (*mcp.CallToolResult, any, error) {
	if err := s.crud.DeleteEvent(ctx, entity.DeleteEventRequest{
		UserID: getUserID(ctx),
		ID:     parseID(args.EventID),
	}); err != nil {
		return errorResponse(err), nil, nil
	}
	return textResponse("event deleted"), nil, nil
}

func (s *WorkspaceServer) handleCreateEventTrigger(ctx context.Context, req *mcp.CallToolRequest, args CreateEventTriggerParams) (*mcp.CallToolResult, any, error) {
	res, err := s.crud.CreateEventTrigger(ctx, entity.CreateEventTriggerRequest{
		UserID:           getUserID(ctx),
		EventID:          parseID(args.EventID),
		WorkspaceID:      parseID(args.WorkspaceID),
		Title:            args.Title,
		Body:             args.Body,
		Assignee:         triggerAssignee(args.Assignee),
		CronSchedule:     args.CronSchedule,
		AllowAllCommands: args.AllowAllCommands,
		EmitEventID:      parseID(args.EmitEventID),
	})
	if err != nil {
		return errorResponse(err), nil, nil
	}
	return textResponse(string(apiMapper.FromCreateEventTriggerResponseEntityToHTTPResponse(res))), nil, nil
}

func (s *WorkspaceServer) handleListEventTriggers(ctx context.Context, req *mcp.CallToolRequest, args ListEventTriggersParams) (*mcp.CallToolResult, any, error) {
	res, err := s.crud.ListEventTriggers(ctx, entity.ListEventTriggersRequest{
		UserID:  getUserID(ctx),
		EventID: parseID(args.EventID),
	})
	if err != nil {
		return errorResponse(err), nil, nil
	}
	return textResponse(string(apiMapper.FromListEventTriggersResponseEntityToHTTPResponse(res))), nil, nil
}

func (s *WorkspaceServer) handleGetEventTrigger(ctx context.Context, req *mcp.CallToolRequest, args GetEventTriggerParams) (*mcp.CallToolResult, any, error) {
	res, err := s.crud.GetEventTrigger(ctx, entity.GetEventTriggerRequest{
		UserID: getUserID(ctx),
		ID:     parseID(args.TriggerID),
	})
	if err != nil {
		return errorResponse(err), nil, nil
	}
	return textResponse(string(apiMapper.FromGetEventTriggerResponseEntityToHTTPResponse(res))), nil, nil
}

func (s *WorkspaceServer) handleUpdateEventTrigger(ctx context.Context, req *mcp.CallToolRequest, args UpdateEventTriggerParams) (*mcp.CallToolResult, any, error) {
	res, err := s.crud.UpdateEventTrigger(ctx, entity.UpdateEventTriggerRequest{
		UserID:           getUserID(ctx),
		ID:               parseID(args.TriggerID),
		WorkspaceID:      parseID(args.WorkspaceID),
		Title:            args.Title,
		Body:             args.Body,
		Assignee:         triggerAssignee(args.Assignee),
		CronSchedule:     args.CronSchedule,
		AllowAllCommands: args.AllowAllCommands,
		EmitEventID:      parseID(args.EmitEventID),
	})
	if err != nil {
		return errorResponse(err), nil, nil
	}
	return textResponse(string(apiMapper.FromUpdateEventTriggerResponseEntityToHTTPResponse(res))), nil, nil
}

func (s *WorkspaceServer) handleDeleteEventTrigger(ctx context.Context, req *mcp.CallToolRequest, args DeleteEventTriggerParams) (*mcp.CallToolResult, any, error) {
	if err := s.crud.DeleteEventTrigger(ctx, entity.DeleteEventTriggerRequest{
		UserID: getUserID(ctx),
		ID:     parseID(args.TriggerID),
	}); err != nil {
		return errorResponse(err), nil, nil
	}
	return textResponse("event trigger deleted"), nil, nil
}

func (s *WorkspaceServer) handleListEventTasks(ctx context.Context, req *mcp.CallToolRequest, args ListEventTasksParams) (*mcp.CallToolResult, any, error) {
	res, err := s.crud.ListTasksFromEvent(ctx, entity.ListTasksFromEventRequest{
		UserID:  getUserID(ctx),
		EventID: parseID(args.EventID),
	})
	if err != nil {
		return errorResponse(err), nil, nil
	}
	return textResponse(string(apiMapper.FromListTasksFromEventResponseEntityToHTTPResponse(res))), nil, nil
}

// triggerAssignee mirrors the REST mapper: a trigger with no assignee is for an
// agent, since a trigger exists to make something happen without being asked.
func triggerAssignee(assignee string) string {
	if assignee == "" {
		return "agent"
	}
	return assignee
}
