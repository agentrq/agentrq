// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package crud

import (
	"context"
	"errors"
	"testing"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/golang/mock/gomock"
)

var errDBDown = errors.New("database unavailable")

// A failure that is not a taken name is passed on as it is, not as a conflict.
func TestCreateEvent_RepositoryError(t *testing.T) {
	e := newTestController(t)
	e.idgen.EXPECT().NextID().Return(int64(100))
	e.repo.EXPECT().CreateEvent(gomock.Any(), gomock.Any()).Return(model.Event{}, errDBDown)

	_, err := e.controller.CreateEvent(context.Background(), entity.CreateEventRequest{UserID: testUserIDStr, Name: "deploy_done"})
	if !errors.Is(err, errDBDown) || errors.Is(err, ErrDuplicateName) {
		t.Fatalf("expected the repository error unchanged, got %v", err)
	}
}

func TestListEvents_RepositoryError(t *testing.T) {
	e := newTestController(t)
	e.repo.EXPECT().ListEventsByUser(gomock.Any(), testUserID).Return(nil, errDBDown)

	if _, err := e.controller.ListEvents(context.Background(), entity.ListEventsRequest{UserID: testUserIDStr}); !errors.Is(err, errDBDown) {
		t.Fatalf("expected the repository error, got %v", err)
	}
}

func TestUpdateEvent(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		e := newTestController(t)
		e.repo.EXPECT().UpdateEvent(gomock.Any(), int64(100), testUserID, "say what changed").
			Return(model.Event{ID: 100, UserID: testUserID, Name: "deploy_done", PayloadGuidelines: "say what changed"}, nil)

		resp, err := e.controller.UpdateEvent(context.Background(), entity.UpdateEventRequest{
			ID: 100, UserID: testUserIDStr, PayloadGuidelines: "say what changed",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Event.ID != 100 || resp.Event.PayloadGuidelines != "say what changed" {
			t.Errorf("unexpected event: %+v", resp.Event)
		}
	})

	t.Run("invalid user", func(t *testing.T) {
		e := newTestController(t)
		if _, err := e.controller.UpdateEvent(context.Background(), entity.UpdateEventRequest{ID: 100, UserID: ""}); err == nil {
			t.Fatal("expected an error for an empty user")
		}
	})

	t.Run("not found", func(t *testing.T) {
		e := newTestController(t)
		e.repo.EXPECT().UpdateEvent(gomock.Any(), int64(100), testUserID, "").Return(model.Event{}, base.ErrNotFound)

		if _, err := e.controller.UpdateEvent(context.Background(), entity.UpdateEventRequest{ID: 100, UserID: testUserIDStr}); !errors.Is(err, base.ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
	})
}

func TestCreateEventTrigger_RepositoryErrors(t *testing.T) {
	req := entity.CreateEventTriggerRequest{UserID: testUserIDStr, EventID: 100, WorkspaceID: 10, Title: "Review"}

	t.Run("workspace access check fails", func(t *testing.T) {
		e := newTestController(t)
		e.repo.EXPECT().GetEvent(gomock.Any(), int64(100), testUserID).Return(model.Event{ID: 100}, nil)
		e.repo.EXPECT().CheckWorkspaceAccess(gomock.Any(), int64(10), testUserID).Return(false, errDBDown)

		if _, err := e.controller.CreateEventTrigger(context.Background(), req); !errors.Is(err, errDBDown) {
			t.Fatalf("expected the access check error, got %v", err)
		}
	})

	t.Run("create fails", func(t *testing.T) {
		e := newTestController(t)
		e.repo.EXPECT().GetEvent(gomock.Any(), int64(100), testUserID).Return(model.Event{ID: 100}, nil)
		e.repo.EXPECT().CheckWorkspaceAccess(gomock.Any(), int64(10), testUserID).Return(true, nil)
		e.idgen.EXPECT().NextID().Return(int64(200))
		e.repo.EXPECT().CreateEventTrigger(gomock.Any(), gomock.Any()).Return(model.EventTrigger{}, errDBDown)

		if _, err := e.controller.CreateEventTrigger(context.Background(), req); !errors.Is(err, errDBDown) {
			t.Fatalf("expected the create error, got %v", err)
		}
	})
}

func TestGetEventTrigger_NotFound(t *testing.T) {
	e := newTestController(t)
	e.repo.EXPECT().GetEventTrigger(gomock.Any(), int64(200), testUserID).Return(model.EventTrigger{}, base.ErrNotFound)

	if _, err := e.controller.GetEventTrigger(context.Background(), entity.GetEventTriggerRequest{ID: 200, UserID: testUserIDStr}); !errors.Is(err, base.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestListEventTriggers_RepositoryError(t *testing.T) {
	e := newTestController(t)
	e.repo.EXPECT().ListEventTriggersByEvent(gomock.Any(), int64(100), testUserID).Return(nil, errDBDown)

	if _, err := e.controller.ListEventTriggers(context.Background(), entity.ListEventTriggersRequest{EventID: 100, UserID: testUserIDStr}); !errors.Is(err, errDBDown) {
		t.Fatalf("expected the repository error, got %v", err)
	}
}

func TestUpdateEventTrigger_WorkspaceAccessCheckFails(t *testing.T) {
	e := newTestController(t)
	e.repo.EXPECT().CheckWorkspaceAccess(gomock.Any(), int64(10), testUserID).Return(false, errDBDown)

	_, err := e.controller.UpdateEventTrigger(context.Background(), entity.UpdateEventTriggerRequest{
		ID: 200, UserID: testUserIDStr, WorkspaceID: 10, Title: "Review",
	})
	if !errors.Is(err, errDBDown) {
		t.Fatalf("expected the access check error, got %v", err)
	}
}

func TestListTasksFromEvent_RepositoryError(t *testing.T) {
	e := newTestController(t)
	e.repo.EXPECT().ListTasksByTriggerID(gomock.Any(), int64(100), testUserID).Return(nil, errDBDown)

	if _, err := e.controller.ListTasksFromEvent(context.Background(), entity.ListTasksFromEventRequest{EventID: 100, UserID: testUserIDStr}); !errors.Is(err, errDBDown) {
		t.Fatalf("expected the repository error, got %v", err)
	}
}

func TestIsUniqueConstraintErr_Nil(t *testing.T) {
	if isUniqueConstraintErr(nil) {
		t.Error("no error is not a unique constraint violation")
	}
}
