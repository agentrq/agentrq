// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/gofiber/fiber/v2"
	"github.com/mustafaturan/monoflake"
)

// mockCrudGetTaskPlan answers exactly GetTaskPlan.
type mockCrudGetTaskPlan struct {
	crud.Controller
	get func(ctx context.Context, req entity.GetTaskPlanRequest) (*entity.GetTaskPlanResponse, error)
}

func (m *mockCrudGetTaskPlan) GetTaskPlan(ctx context.Context, req entity.GetTaskPlanRequest) (*entity.GetTaskPlanResponse, error) {
	return m.get(ctx, req)
}

var taskPlanPath = "/workspaces/" + monoflake.ID(1).String() + "/tasks/" + monoflake.ID(44).String() + "/plan"

// getTaskPlan asks for the plan at path, under /api/v1.
func getTaskPlan(t *testing.T, path string, get func(ctx context.Context, req entity.GetTaskPlanRequest) (*entity.GetTaskPlanResponse, error)) (int, string) {
	t.Helper()
	h := &handler{crud: &mockCrudGetTaskPlan{get: get}}
	app := fiber.New()
	app.Get("/api/v1"+_routePathTaskPlan, func(c *fiber.Ctx) error {
		c.Locals("user_id", monoflake.ID(100).String())
		return h.getTaskPlan()(c)
	})
	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/api/v1"+path, nil))
	if err != nil {
		t.Fatalf("the request failed: %v", err)
	}
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestGetTaskPlan_ReadsThePlan(t *testing.T) {
	at := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)
	var got entity.GetTaskPlanRequest
	status, body := getTaskPlan(t, taskPlanPath, func(ctx context.Context, req entity.GetTaskPlanRequest) (*entity.GetTaskPlanResponse, error) {
		got = req
		return &entity.GetTaskPlanResponse{Plan: entity.TaskPlan{ID: 44, Content: "- [x] Read", Version: 2, CreatedAt: at, UpdatedAt: at}}, nil
	})
	if status != http.StatusOK {
		t.Fatalf("answered %d (%s), want 200", status, body)
	}
	if want := (entity.GetTaskPlanRequest{WorkspaceID: 1, TaskID: 44, UserID: monoflake.ID(100).String()}); got != want {
		t.Errorf("the controller was asked %+v, want %+v", got, want)
	}
	if want := `{"id":"` + monoflake.ID(44).String() + `","content":"- [x] Read","version":2,"createdAt":"2026-10-18T00:00:00Z","updatedAt":"2026-10-18T00:00:00Z"}`; body != want {
		t.Errorf("body = %s\nwant %s", body, want)
	}
}

func TestGetTaskPlan_Refusals(t *testing.T) {
	never := func(ctx context.Context, req entity.GetTaskPlanRequest) (*entity.GetTaskPlanResponse, error) {
		t.Error("the controller was called")
		return nil, nil
	}
	if status, _ := getTaskPlan(t, "/workspaces/0/tasks/"+monoflake.ID(44).String()+"/plan", never); status != http.StatusUnprocessableEntity {
		t.Errorf("no workspace answered %d, want 422", status)
	}
	notFound := func(ctx context.Context, req entity.GetTaskPlanRequest) (*entity.GetTaskPlanResponse, error) {
		return nil, base.ErrNotFound
	}
	if status, _ := getTaskPlan(t, taskPlanPath, notFound); status != http.StatusNotFound {
		t.Errorf("no plan answered %d, want 404", status)
	}
}
