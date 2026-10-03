// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
	"github.com/gofiber/fiber/v2"
	"github.com/mustafaturan/monoflake"
)

// mockCrudUpdateTaskTitle answers exactly UpdateTaskTitle.
type mockCrudUpdateTaskTitle struct {
	crud.Controller
	updateTaskTitleFunc func(ctx context.Context, req entity.UpdateTaskTitleRequest) (*entity.UpdateTaskTitleResponse, error)
}

func (m *mockCrudUpdateTaskTitle) UpdateTaskTitle(ctx context.Context, req entity.UpdateTaskTitleRequest) (*entity.UpdateTaskTitleResponse, error) {
	return m.updateTaskTitleFunc(ctx, req)
}

// patchTaskTitle sends body to task 44 of workspace 1 and returns the status,
// the response body and what the workspace's event stream received.
func patchTaskTitle(t *testing.T, body string, rename func(ctx context.Context, req entity.UpdateTaskTitleRequest) (*entity.UpdateTaskTitleResponse, error)) (int, string, []byte) {
	t.Helper()
	userID := monoflake.ID(100).String()
	bus := eventbus.New()
	events := bus.Subscribe(1, userID)
	h := &handler{crud: &mockCrudUpdateTaskTitle{updateTaskTitleFunc: rename}, bus: bus}

	app := fiber.New()
	app.Patch("/api/v1/workspaces/:id/tasks/:taskID/title", func(c *fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.updateTaskTitle()(c)
	})
	req := httptest.NewRequest(http.MethodPatch,
		"/api/v1/workspaces/"+monoflake.ID(1).String()+"/tasks/"+monoflake.ID(44).String()+"/title",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("the request failed: %v", err)
	}
	b, _ := io.ReadAll(resp.Body)

	var published []byte
	select {
	case published = <-events:
	default:
	}
	return resp.StatusCode, string(b), published
}

func TestUpdateTaskTitle_RenamesAndTellsTheWorkspace(t *testing.T) {
	var got entity.UpdateTaskTitleRequest
	status, body, published := patchTaskTitle(t, `{"title":{"value":"  Fix the login page "}}`,
		func(ctx context.Context, req entity.UpdateTaskTitleRequest) (*entity.UpdateTaskTitleResponse, error) {
			got = req
			return &entity.UpdateTaskTitleResponse{Task: entity.Task{ID: 44, WorkspaceID: 1, Title: req.Title}}, nil
		})

	if status != http.StatusOK {
		t.Fatalf("the rename answered %d (%s), want 200", status, body)
	}
	want := entity.UpdateTaskTitleRequest{WorkspaceID: 1, TaskID: 44, Title: "Fix the login page", UserID: monoflake.ID(100).String()}
	if got != want {
		t.Errorf("the controller was asked %+v, want %+v", got, want)
	}
	var rs struct {
		Task struct {
			Title string `json:"title"`
		} `json:"task"`
	}
	if err := json.Unmarshal([]byte(body), &rs); err != nil || rs.Task.Title != "Fix the login page" {
		t.Errorf("the rename answered %s, want the renamed task", body)
	}
	if !strings.Contains(string(published), `"task.updated"`) || !strings.Contains(string(published), "Fix the login page") {
		t.Errorf("the workspace's event stream received %q, want a task.updated carrying the new title", published)
	}
}

func TestUpdateTaskTitle_RefusesABlankOrUnreadableTitle(t *testing.T) {
	for _, body := range []string{`{"title":{"value":"   "}}`, `{}`, `not json`} {
		t.Run(body, func(t *testing.T) {
			status, _, published := patchTaskTitle(t, body,
				func(ctx context.Context, req entity.UpdateTaskTitleRequest) (*entity.UpdateTaskTitleResponse, error) {
					t.Fatalf("the controller was asked to rename to %q, want the request refused first", req.Title)
					return nil, nil
				})
			if status != http.StatusUnprocessableEntity {
				t.Errorf("the rename answered %d, want 422", status)
			}
			if published != nil {
				t.Errorf("the workspace's event stream received %q, want nothing", published)
			}
		})
	}
}

func TestUpdateTaskTitle_SaysWhyAnOldTaskCannotBeRenamed(t *testing.T) {
	status, body, published := patchTaskTitle(t, `{"title":{"value":"New"}}`,
		func(ctx context.Context, req entity.UpdateTaskTitleRequest) (*entity.UpdateTaskTitleResponse, error) {
			return nil, entity.ErrTaskTitleLocked
		})
	if status != http.StatusConflict {
		t.Errorf("the rename answered %d, want 409", status)
	}
	if !strings.Contains(body, entity.ErrTaskTitleLocked.Error()) {
		t.Errorf("the rename answered %s, want it to say %q", body, entity.ErrTaskTitleLocked.Error())
	}
	if published != nil {
		t.Errorf("the workspace's event stream received %q, want nothing", published)
	}
}

// The web form may leave the title out; the controller is what names it.
func TestCreateTask_PassesAMissingTitleOnToTheController(t *testing.T) {
	var got entity.CreateTaskRequest
	crudCtrl := &mockCrudCreateTask{
		createTaskFunc: func(ctx context.Context, req entity.CreateTaskRequest) (*entity.CreateTaskResponse, error) {
			got = req
			return &entity.CreateTaskResponse{Task: entity.Task{ID: 45, WorkspaceID: 1, Assignee: "human", Title: entity.UntitledTaskTitle}}, nil
		},
	}
	h := &handler{crud: crudCtrl, mcpManager: &fakeMCPManager{server: &fakeWorkspaceServer{}}, bus: eventbus.New()}
	app := fiber.New()
	app.Post("/api/v1/workspaces/:id/tasks", func(c *fiber.Ctx) error {
		c.Locals("user_id", monoflake.ID(100).String())
		return h.createTask()(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/"+monoflake.ID(1).String()+"/tasks",
		strings.NewReader(`{"task":{"body":"Fix the login page","createdBy":"human","assignee":"human"}}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("the request failed: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("creating a task without a title answered %d (%s), want 201", resp.StatusCode, b)
	}
	if got.Task.Title != "" || got.Task.Body != "Fix the login page" {
		t.Errorf("the controller was asked for title %q and body %q, want no title and the body as sent", got.Task.Title, got.Task.Body)
	}
}
