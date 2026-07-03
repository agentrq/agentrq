package crud

import (
	"context"
	"fmt"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	mapper "github.com/agentrq/agentrq/backend/internal/mapper/api"
	"github.com/agentrq/agentrq/backend/internal/service/schedule"
	"github.com/mustafaturan/monoflake"
)

// EventController defines event operations.
type EventController interface {
	CreateEvent(ctx context.Context, req entity.CreateEventRequest) (*entity.CreateEventResponse, error)
	GetEvent(ctx context.Context, req entity.GetEventRequest) (*entity.GetEventResponse, error)
	ListEvents(ctx context.Context, req entity.ListEventsRequest) (*entity.ListEventsResponse, error)
	UpdateEvent(ctx context.Context, req entity.UpdateEventRequest) (*entity.UpdateEventResponse, error)
	DeleteEvent(ctx context.Context, req entity.DeleteEventRequest) error
}

// EventTriggerController defines event trigger operations.
type EventTriggerController interface {
	CreateEventTrigger(ctx context.Context, req entity.CreateEventTriggerRequest) (*entity.CreateEventTriggerResponse, error)
	GetEventTrigger(ctx context.Context, req entity.GetEventTriggerRequest) (*entity.GetEventTriggerResponse, error)
	ListEventTriggers(ctx context.Context, req entity.ListEventTriggersRequest) (*entity.ListEventTriggersResponse, error)
	DeleteEventTrigger(ctx context.Context, req entity.DeleteEventTriggerRequest) error
	ListTasksFromEvent(ctx context.Context, req entity.ListTasksFromEventRequest) (*entity.ListTasksFromEventResponse, error)
}

// ── Events ────────────────────────────────────────────────────────────────────

func (c *controller) CreateEvent(ctx context.Context, req entity.CreateEventRequest) (*entity.CreateEventResponse, error) {
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	if uid == 0 {
		return nil, fmt.Errorf("invalid userID")
	}
	if !isValidEventName(req.Name) {
		return nil, fmt.Errorf("invalid event name: must match ^[a-z][a-z0-9_]{0,128}$")
	}

	now := time.Now()
	m := model.Event{
		ID:                c.idgen.NextID(),
		CreatedAt:         now,
		UpdatedAt:         now,
		UserID:            uid,
		Name:              req.Name,
		PayloadGuidelines: req.PayloadGuidelines,
	}

	created, err := c.repository.CreateEvent(ctx, m)
	if err != nil {
		return nil, err
	}
	return &entity.CreateEventResponse{Event: mapper.FromModelEventToEntity(created)}, nil
}

func (c *controller) GetEvent(ctx context.Context, req entity.GetEventRequest) (*entity.GetEventResponse, error) {
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	e, err := c.repository.GetEvent(ctx, req.ID, uid)
	if err != nil {
		return nil, err
	}
	return &entity.GetEventResponse{Event: mapper.FromModelEventToEntity(e)}, nil
}

func (c *controller) ListEvents(ctx context.Context, req entity.ListEventsRequest) (*entity.ListEventsResponse, error) {
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	models, err := c.repository.ListEventsByUser(ctx, uid)
	if err != nil {
		return nil, err
	}
	events := make([]entity.Event, len(models))
	for i, m := range models {
		events[i] = mapper.FromModelEventToEntity(m)
	}
	return &entity.ListEventsResponse{Events: events}, nil
}

func (c *controller) UpdateEvent(ctx context.Context, req entity.UpdateEventRequest) (*entity.UpdateEventResponse, error) {
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	if uid == 0 {
		return nil, fmt.Errorf("invalid userID")
	}
	updated, err := c.repository.UpdateEvent(ctx, req.ID, uid, req.PayloadGuidelines)
	if err != nil {
		return nil, err
	}
	return &entity.UpdateEventResponse{Event: mapper.FromModelEventToEntity(updated)}, nil
}

func (c *controller) DeleteEvent(ctx context.Context, req entity.DeleteEventRequest) error {
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	return c.repository.DeleteEvent(ctx, req.ID, uid)
}

// ── EventTriggers ─────────────────────────────────────────────────────────────

func (c *controller) CreateEventTrigger(ctx context.Context, req entity.CreateEventTriggerRequest) (*entity.CreateEventTriggerResponse, error) {
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	if uid == 0 {
		return nil, fmt.Errorf("invalid userID")
	}

	// Verify the event exists and belongs to this user.
	if _, err := c.repository.GetEvent(ctx, req.EventID, uid); err != nil {
		return nil, fmt.Errorf("event not found")
	}

	// Verify the target workspace belongs to this user.
	ok, err := c.repository.CheckWorkspaceAccess(ctx, req.WorkspaceID, uid)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("workspace not found")
	}

	if req.Title == "" {
		return nil, fmt.Errorf("title is required")
	}

	if req.CronSchedule != "" {
		if err := schedule.ValidateCronGranularity(req.CronSchedule); err != nil {
			return nil, err
		}
	}

	if req.EmitEventID != 0 {
		if _, err := c.repository.GetEvent(ctx, req.EmitEventID, uid); err != nil {
			return nil, fmt.Errorf("emit event not found")
		}
	}

	now := time.Now()
	m := model.EventTrigger{
		ID:               c.idgen.NextID(),
		CreatedAt:        now,
		UpdatedAt:        now,
		EventID:          req.EventID,
		WorkspaceID:      req.WorkspaceID,
		UserID:           uid,
		Title:            req.Title,
		Body:             req.Body,
		Assignee:         req.Assignee,
		CronSchedule:     req.CronSchedule,
		AllowAllCommands: req.AllowAllCommands,
		EmitEventID:      req.EmitEventID,
	}

	created, err := c.repository.CreateEventTrigger(ctx, m)
	if err != nil {
		return nil, err
	}
	return &entity.CreateEventTriggerResponse{EventTrigger: mapper.FromModelEventTriggerToEntity(created)}, nil
}

func (c *controller) GetEventTrigger(ctx context.Context, req entity.GetEventTriggerRequest) (*entity.GetEventTriggerResponse, error) {
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	t, err := c.repository.GetEventTrigger(ctx, req.ID, uid)
	if err != nil {
		return nil, err
	}
	return &entity.GetEventTriggerResponse{EventTrigger: mapper.FromModelEventTriggerToEntity(t)}, nil
}

func (c *controller) ListEventTriggers(ctx context.Context, req entity.ListEventTriggersRequest) (*entity.ListEventTriggersResponse, error) {
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	models, err := c.repository.ListEventTriggersByEvent(ctx, req.EventID, uid)
	if err != nil {
		return nil, err
	}
	triggers := make([]entity.EventTrigger, len(models))
	for i, m := range models {
		triggers[i] = mapper.FromModelEventTriggerToEntity(m)
	}
	return &entity.ListEventTriggersResponse{EventTriggers: triggers}, nil
}

func (c *controller) DeleteEventTrigger(ctx context.Context, req entity.DeleteEventTriggerRequest) error {
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	return c.repository.DeleteEventTrigger(ctx, req.ID, uid)
}

func (c *controller) ListTasksFromEvent(ctx context.Context, req entity.ListTasksFromEventRequest) (*entity.ListTasksFromEventResponse, error) {
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	models, err := c.repository.ListTasksByTriggerID(ctx, req.EventID, uid)
	if err != nil {
		return nil, err
	}
	tasks := make([]entity.Task, len(models))
	for i, m := range models {
		tasks[i] = c.fromModelTaskToEntity(m)
	}
	return &entity.ListTasksFromEventResponse{Tasks: tasks}, nil
}

// ── Validation helpers ────────────────────────────────────────────────────────

func isValidEventName(name string) bool {
	if len(name) == 0 || len(name) > 129 {
		return false
	}
	for i, ch := range name {
		if i == 0 {
			if ch < 'a' || ch > 'z' {
				return false
			}
		} else {
			if !((ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '_') {
				return false
			}
		}
	}
	return true
}
