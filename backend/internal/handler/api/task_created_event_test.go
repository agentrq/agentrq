// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
	"github.com/gofiber/fiber/v2"
	"github.com/mustafaturan/monoflake"
)

// The controller's CRUD event is what the central forwarder turns into
// task.created. A handler publishing one as well is the same task arriving
// twice on the stream, and the browser toasts each one.
func assertNoLiveEvent(t *testing.T, events chan []byte) {
	t.Helper()
	select {
	case line := <-events:
		t.Errorf("the handler published %s itself, want it left to the CRUD forwarder", line)
	default:
	}
}

func TestCreateTask_LeavesTaskCreatedToTheForwarder(t *testing.T) {
	userID := monoflake.ID(100).String()
	bus := eventbus.New()
	events := bus.Subscribe(1, userID)
	defer bus.Unsubscribe(1, userID, events)

	created := entity.Task{ID: 61, WorkspaceID: 1, CreatedBy: "human", Assignee: "human", Status: "notstarted", Title: "Review the release notes"}
	crudCtrl := &mockCrudCreateTask{
		createTaskFunc: func(ctx context.Context, req entity.CreateTaskRequest) (*entity.CreateTaskResponse, error) {
			return &entity.CreateTaskResponse{Task: created}, nil
		},
	}
	h := &handler{crud: crudCtrl, mcpManager: &fakeMCPManager{server: &fakeWorkspaceServer{}}, bus: bus}
	app := fiber.New()
	app.Post("/api/v1/workspaces/:id/tasks", func(c *fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.createTask()(c)
	})

	body := `{"task":{"title":"Review the release notes","createdBy":"human","assignee":"human"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/"+monoflake.ID(1).String()+"/tasks", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("the request failed: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("the create answered %d, want 201", resp.StatusCode)
	}
	assertNoLiveEvent(t, events)
}

func TestForkTask_LeavesTaskCreatedToTheForwarder(t *testing.T) {
	userID := monoflake.ID(100).String()
	bus := eventbus.New()
	events := bus.Subscribe(1, userID)
	defer bus.Unsubscribe(1, userID, events)

	h := &handler{crud: &mockCrudForkTask{}, mcpManager: &fakeMCPManager{server: &fakeWorkspaceServer{}}, bus: bus}
	app := fiber.New()
	app.Post(_routePathFork, func(c *fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.forkTask()(c)
	})
	url := "/workspaces/" + monoflake.ID(1).String() + "/tasks/" + monoflake.ID(9).String() + "/fork"
	req := httptest.NewRequest(http.MethodPost, url, strings.NewReader(`{"messageId":"`+monoflake.ID(5).String()+`"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("the request failed: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("the fork answered %d, want 201", resp.StatusCode)
	}
	assertNoLiveEvent(t, events)
}
