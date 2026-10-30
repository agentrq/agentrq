// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
)

func eventOwned(_ context.Context, _ entity.GetEventRequest) (*entity.GetEventResponse, error) {
	return &entity.GetEventResponse{}, nil
}

// A name the user already uses is their input, so it is a 409 that says so,
// not a 500.
func TestCreateEvent_DuplicateNameConflict(t *testing.T) {
	ctrl := &mockEventCrud{}
	ctrl.createEventFunc = func(_ context.Context, _ entity.CreateEventRequest) (*entity.CreateEventResponse, error) {
		return nil, fmt.Errorf("event %q: %w", "deploy_done", crud.ErrDuplicateName)
	}
	app := newEventApp(ctrl)

	req := httptest.NewRequest(http.MethodPost, "/events", bytes.NewReader([]byte(`{"name":"deploy_done"}`)))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "deploy_done") {
		t.Errorf("the conflict should name the taken event, got %s", body)
	}
}

func TestListEvents_ControllerError(t *testing.T) {
	ctrl := &mockEventCrud{}
	ctrl.listEventsFunc = func(_ context.Context, _ entity.ListEventsRequest) (*entity.ListEventsResponse, error) {
		return nil, errors.New("database unavailable")
	}
	resp, _ := newEventApp(ctrl).Test(httptest.NewRequest(http.MethodGet, "/events", nil))

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", resp.StatusCode)
	}
}

func TestListEventTriggers_InvalidEventID(t *testing.T) {
	resp, _ := newEventApp(&mockEventCrud{}).Test(httptest.NewRequest(http.MethodGet, "/events/!!!/triggers", nil))

	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("expected 422, got %d", resp.StatusCode)
	}
}

func TestListEventTriggers_ControllerError(t *testing.T) {
	ctrl := &mockEventCrud{getEventFunc: eventOwned}
	ctrl.listEventTriggersFunc = func(_ context.Context, _ entity.ListEventTriggersRequest) (*entity.ListEventTriggersResponse, error) {
		return nil, errors.New("database unavailable")
	}
	resp, _ := newEventApp(ctrl).Test(httptest.NewRequest(http.MethodGet, "/events/"+testEventID+"/triggers", nil))

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", resp.StatusCode)
	}
}

// An ownership check that fails for any reason other than "not found" is
// reported as the failure it is, not as a missing event.
func TestListTasksFromEvent_OwnershipCheckFails(t *testing.T) {
	ctrl := &mockEventCrud{}
	ctrl.getEventFunc = func(_ context.Context, _ entity.GetEventRequest) (*entity.GetEventResponse, error) {
		return nil, errors.New("database unavailable")
	}
	resp, _ := newEventApp(ctrl).Test(httptest.NewRequest(http.MethodGet, "/events/"+testEventID+"/tasks", nil))

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", resp.StatusCode)
	}
}

func TestListTasksFromEvent_ControllerError(t *testing.T) {
	ctrl := &mockEventCrud{getEventFunc: eventOwned}
	ctrl.listTasksFromEventFunc = func(_ context.Context, _ entity.ListTasksFromEventRequest) (*entity.ListTasksFromEventResponse, error) {
		return nil, errors.New("database unavailable")
	}
	resp, _ := newEventApp(ctrl).Test(httptest.NewRequest(http.MethodGet, "/events/"+testEventID+"/tasks", nil))

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", resp.StatusCode)
	}
}
