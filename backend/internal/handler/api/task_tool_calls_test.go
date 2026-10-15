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

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/gofiber/fiber/v2"
	"github.com/mustafaturan/monoflake"
)

// mockCrudListTaskToolCalls answers exactly ListTaskToolCalls.
type mockCrudListTaskToolCalls struct {
	crud.Controller
	list func(ctx context.Context, req entity.ListTaskToolCallsRequest) (*entity.ListTaskToolCallsResponse, error)
}

func (m *mockCrudListTaskToolCalls) ListTaskToolCalls(ctx context.Context, req entity.ListTaskToolCallsRequest) (*entity.ListTaskToolCallsResponse, error) {
	return m.list(ctx, req)
}

var toolCallsPath = "/workspaces/" + monoflake.ID(1).String() + "/tasks/" + monoflake.ID(44).String() + "/tool-calls"

// getToolCalls asks for the tool calls at path, under /api/v1.
func getToolCalls(t *testing.T, path string, list func(ctx context.Context, req entity.ListTaskToolCallsRequest) (*entity.ListTaskToolCallsResponse, error)) (int, string) {
	t.Helper()
	h := &handler{crud: &mockCrudListTaskToolCalls{list: list}}
	app := fiber.New()
	app.Get("/api/v1"+_routePathToolCalls, func(c *fiber.Ctx) error {
		c.Locals("user_id", monoflake.ID(100).String())
		return h.listTaskToolCalls()(c)
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1"+path, nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("the request failed: %v", err)
	}
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestListTaskToolCalls_PagesBefore(t *testing.T) {
	var got entity.ListTaskToolCallsRequest
	status, body := getToolCalls(t, toolCallsPath+"?before="+monoflake.ID(9).String()+"&limit=2",
		func(ctx context.Context, req entity.ListTaskToolCallsRequest) (*entity.ListTaskToolCallsResponse, error) {
			got = req
			return &entity.ListTaskToolCallsResponse{ToolCalls: []entity.ToolCall{{ID: 7, TaskID: 44, ToolName: "Bash"}}}, nil
		})
	if status != http.StatusOK {
		t.Fatalf("answered %d (%s), want 200", status, body)
	}
	want := entity.ListTaskToolCallsRequest{WorkspaceID: 1, TaskID: 44, UserID: monoflake.ID(100).String(), Before: 9, Limit: 2}
	if got != want {
		t.Errorf("the controller was asked %+v, want %+v", got, want)
	}
	if wantBody := `{"toolCalls":[{"id":"` + monoflake.ID(7).String() + `","createdAt":"0001-01-01T00:00:00Z","taskId":"` + monoflake.ID(44).String() + `","toolName":"Bash","status":""}]}`; body != wantBody {
		t.Errorf("body = %s\nwant %s", body, wantBody)
	}
}

func TestListTaskToolCalls_Refusals(t *testing.T) {
	never := func(ctx context.Context, req entity.ListTaskToolCallsRequest) (*entity.ListTaskToolCallsResponse, error) {
		t.Error("the controller was called")
		return nil, nil
	}
	if status, _ := getToolCalls(t, toolCallsPath+"?before=%21", never); status != http.StatusUnprocessableEntity {
		t.Errorf("an unreadable cursor answered %d, want 422", status)
	}
	if status, _ := getToolCalls(t, "/workspaces/0/tasks/"+monoflake.ID(44).String()+"/tool-calls", never); status != http.StatusUnprocessableEntity {
		t.Errorf("no workspace answered %d, want 422", status)
	}
	notFound := func(ctx context.Context, req entity.ListTaskToolCallsRequest) (*entity.ListTaskToolCallsResponse, error) {
		return nil, base.ErrNotFound
	}
	if status, _ := getToolCalls(t, toolCallsPath, notFound); status != http.StatusNotFound {
		t.Errorf("a task elsewhere answered %d, want 404", status)
	}
}
