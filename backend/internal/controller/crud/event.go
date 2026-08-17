package crud

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	mapper "github.com/agentrq/agentrq/backend/internal/mapper/api"
	"github.com/agentrq/agentrq/backend/internal/service/schedule"
	"github.com/mustafaturan/monoflake"
	"gorm.io/gorm"
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
	UpdateEventTrigger(ctx context.Context, req entity.UpdateEventTriggerRequest) (*entity.UpdateEventTriggerResponse, error)
	DeleteEventTrigger(ctx context.Context, req entity.DeleteEventTriggerRequest) error
	ListTasksFromEvent(ctx context.Context, req entity.ListTasksFromEventRequest) (*entity.ListTasksFromEventResponse, error)
}

// ── Events ────────────────────────────────────────────────────────────────────

func (c *controller) CreateEvent(ctx context.Context, req entity.CreateEventRequest) (*entity.CreateEventResponse, error) {
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	if uid == 0 {
		return nil, fmt.Errorf("invalid userID")
	}
	if !isValidResourceName(req.Name) {
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
		// A duplicate name is the user picking one that is taken, not a server
		// fault. Without this it surfaced as a bare 500, which made a name
		// collision impossible to tell apart from a real failure.
		if isUniqueConstraintErr(err) {
			return nil, fmt.Errorf("%w: %q", ErrDuplicateName, req.Name)
		}
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

func (c *controller) UpdateEventTrigger(ctx context.Context, req entity.UpdateEventTriggerRequest) (*entity.UpdateEventTriggerResponse, error) {
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	if uid == 0 {
		return nil, fmt.Errorf("invalid userID")
	}

	if req.Title == "" {
		return nil, fmt.Errorf("title is required")
	}

	// Verify the target workspace belongs to this user.
	ok, err := c.repository.CheckWorkspaceAccess(ctx, req.WorkspaceID, uid)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("workspace not found")
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

	updated, err := c.repository.UpdateEventTrigger(ctx, req.ID, uid, model.EventTrigger{
		WorkspaceID:      req.WorkspaceID,
		Title:            req.Title,
		Body:             req.Body,
		Assignee:         req.Assignee,
		CronSchedule:     req.CronSchedule,
		AllowAllCommands: req.AllowAllCommands,
		EmitEventID:      req.EmitEventID,
	})
	if err != nil {
		return nil, err
	}
	return &entity.UpdateEventTriggerResponse{EventTrigger: mapper.FromModelEventTriggerToEntity(updated)}, nil
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

// ErrDuplicateName is returned when a name is already taken by another record
// of the same kind for this user. Exported so handlers can answer 409 with the
// name itself rather than a generic failure.
var ErrDuplicateName = fmt.Errorf("name already exists")

// isUniqueConstraintErr reports whether err is a unique-index violation.
//
// gorm.ErrDuplicatedKey is the real check: both drivers here open with
// TranslateError, which converts the driver's own error into that sentinel.
// The text matching below is only a fallback for a connection opened without
// translation — which is exactly how an early version of this function's test
// was written, so it passed while production still returned 500.
func isUniqueConstraintErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique constraint") ||
		strings.Contains(msg, "duplicate key") ||
		strings.Contains(msg, "unique_violation") ||
		(strings.Contains(msg, "unique") && strings.Contains(msg, "constraint failed"))
}

// isValidResourceName enforces the shared identifier convention for names that
// are referenced textually rather than by id — event names in publishEvent,
// and workflow and agent names in the workflow text format. Anything with a
// space or a colon would be ambiguous to parse there, so the rule is one rule.
func isValidResourceName(name string) bool {
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
