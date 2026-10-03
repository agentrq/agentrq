// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
	"github.com/gofiber/fiber/v2"
	"github.com/mustafaturan/monoflake"
)

type mockCrudElicitation struct {
	mockCrudWorkspaceAccess
	closeFunc func(ctx context.Context, req entity.CloseExpiredElicitationRequest) (*entity.CloseExpiredElicitationResponse, error)
}

func (m *mockCrudElicitation) CloseExpiredElicitation(ctx context.Context, req entity.CloseExpiredElicitationRequest) (*entity.CloseExpiredElicitationResponse, error) {
	return m.closeFunc(ctx, req)
}

// postElicitationAnswer answers request "req-1" on task 2 of workspace 1 to a
// server nobody is waiting on, and returns the status, the error text and
// what the workspace's event stream received.
func postElicitationAnswer(t *testing.T, closeFunc func(ctx context.Context, req entity.CloseExpiredElicitationRequest) (*entity.CloseExpiredElicitationResponse, error)) (int, elicitationError, []byte) {
	t.Helper()
	userID := monoflake.ID(100).String()
	crudCtrl := &mockCrudElicitation{closeFunc: closeFunc}
	crudCtrl.checkWorkspaceAccessFunc = func(ctx context.Context, id int64, userID string) (bool, error) { return true, nil }
	bus := eventbus.New()
	events := bus.Subscribe(1, userID)
	h := &handler{
		crud:       crudCtrl,
		mcpManager: &fakeMCPManager{server: &fakeWorkspaceServer{elicitErr: errors.New("elicitation request not found (expired)")}},
		bus:        bus,
	}
	app := fiber.New()
	app.Post("/api/v1/workspaces/:id/tasks/:taskID/elicitation", func(c *fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.respondToElicitation()(c)
	})

	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/workspaces/"+monoflake.ID(1).String()+"/tasks/"+monoflake.ID(2).String()+"/elicitation",
		bytes.NewBufferString(`{"requestId":"req-1","action":"decline"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var body elicitationError
	_ = json.NewDecoder(resp.Body).Decode(&body)

	var published []byte
	select {
	case published = <-events:
	default:
	}
	return resp.StatusCode, body, published
}

type elicitationError struct {
	Error  string `json:"error"`
	Closed bool   `json:"closed"`
}

// An answer nobody waits for, to a question past its deadline, closes the
// question, so the human is not left with buttons that can only fail.
func TestRespondToElicitation_ClosesAnExpiredQuestion(t *testing.T) {
	var got entity.CloseExpiredElicitationRequest
	status, body, published := postElicitationAnswer(t, func(ctx context.Context, req entity.CloseExpiredElicitationRequest) (*entity.CloseExpiredElicitationResponse, error) {
		got = req
		return &entity.CloseExpiredElicitationResponse{Closed: true, Task: entity.Task{ID: 2, WorkspaceID: 1}}, nil
	})
	if status != http.StatusGone {
		t.Fatalf("expected 410, got %d", status)
	}
	if got.WorkspaceID != 1 || got.TaskID != 2 || got.UserID != monoflake.ID(100).String() || got.RequestID != "req-1" {
		t.Errorf("unexpected close request: %+v", got)
	}
	if !body.Closed || !strings.Contains(body.Error, "closed now") {
		t.Errorf("expected the error to say the question is closed, got %+v", body)
	}
	if !strings.Contains(string(published), "task.updated") {
		t.Errorf("expected a task.updated event, got %q", published)
	}
}

// Before the deadline the agent may be waiting on another instance, so the
// question stays as it is.
func TestRespondToElicitation_LeavesAQuestionBeforeItsDeadline(t *testing.T) {
	for name, closeFunc := range map[string]func(ctx context.Context, req entity.CloseExpiredElicitationRequest) (*entity.CloseExpiredElicitationResponse, error){
		"not past its deadline": func(ctx context.Context, req entity.CloseExpiredElicitationRequest) (*entity.CloseExpiredElicitationResponse, error) {
			return &entity.CloseExpiredElicitationResponse{}, nil
		},
		"database unavailable": func(ctx context.Context, req entity.CloseExpiredElicitationRequest) (*entity.CloseExpiredElicitationResponse, error) {
			return nil, errors.New("database unavailable")
		},
	} {
		t.Run(name, func(t *testing.T) {
			status, body, published := postElicitationAnswer(t, closeFunc)
			if status != http.StatusGone {
				t.Fatalf("expected 410, got %d", status)
			}
			if body.Closed || !strings.Contains(body.Error, "has expired") {
				t.Errorf("unexpected error %+v", body)
			}
			if published != nil {
				t.Errorf("expected no event, got %q", published)
			}
		})
	}
}
