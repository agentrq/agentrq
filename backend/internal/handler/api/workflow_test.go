// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
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
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/gofiber/fiber/v2"
	"github.com/mustafaturan/monoflake"
)

// ── mock workflow crud controller ─────────────────────────────────────────────

// mockWorkflowCrud records the last request each method saw, so a test can
// check what the mapper parsed and that the caller's user ID was attached.
type mockWorkflowCrud struct {
	crud.Controller
	err error
	got any
}

func (m *mockWorkflowCrud) CreateWorkflow(_ context.Context, rq entity.CreateWorkflowRequest) (*entity.CreateWorkflowResponse, error) {
	m.got = rq
	if m.err != nil {
		return nil, m.err
	}
	return &entity.CreateWorkflowResponse{Workflow: entity.Workflow{ID: 5, Name: rq.Name, StartEventID: rq.StartEventID}}, nil
}

func (m *mockWorkflowCrud) ListWorkflows(_ context.Context, rq entity.ListWorkflowsRequest) (*entity.ListWorkflowsResponse, error) {
	m.got = rq
	if m.err != nil {
		return nil, m.err
	}
	return &entity.ListWorkflowsResponse{Workflows: []entity.Workflow{{ID: 5, Name: "release"}, {ID: 6, Name: "deploy"}}}, nil
}

func (m *mockWorkflowCrud) GetWorkflow(_ context.Context, rq entity.GetWorkflowRequest) (*entity.GetWorkflowResponse, error) {
	m.got = rq
	if m.err != nil {
		return nil, m.err
	}
	return &entity.GetWorkflowResponse{Workflow: entity.Workflow{ID: rq.ID, Name: "release", Layout: `{"x":1}`}}, nil
}

func (m *mockWorkflowCrud) UpdateWorkflow(_ context.Context, rq entity.UpdateWorkflowRequest) (*entity.UpdateWorkflowResponse, error) {
	m.got = rq
	if m.err != nil {
		return nil, m.err
	}
	return &entity.UpdateWorkflowResponse{Workflow: entity.Workflow{ID: rq.ID, Name: "renamed"}}, nil
}

func (m *mockWorkflowCrud) DeleteWorkflow(_ context.Context, rq entity.DeleteWorkflowRequest) error {
	m.got = rq
	return m.err
}

func (m *mockWorkflowCrud) CreateWorkflowStep(_ context.Context, rq entity.CreateWorkflowStepRequest) (*entity.CreateWorkflowStepResponse, error) {
	m.got = rq
	if m.err != nil {
		return nil, m.err
	}
	return &entity.CreateWorkflowStepResponse{WorkflowStep: entity.WorkflowStep{
		ID: 300, WorkflowID: rq.WorkflowID, EventID: rq.EventID, WorkspaceID: rq.WorkspaceID,
		EmitEventID: rq.EmitEventID, Title: rq.Title, Assignee: rq.Assignee,
	}}, nil
}

func (m *mockWorkflowCrud) ListWorkflowSteps(_ context.Context, rq entity.ListWorkflowStepsRequest) (*entity.ListWorkflowStepsResponse, error) {
	m.got = rq
	if m.err != nil {
		return nil, m.err
	}
	return &entity.ListWorkflowStepsResponse{WorkflowSteps: []entity.WorkflowStep{{ID: 300, WorkflowID: rq.WorkflowID, EventID: 10, WorkspaceID: 20}}}, nil
}

func (m *mockWorkflowCrud) DeleteWorkflowStep(_ context.Context, rq entity.DeleteWorkflowStepRequest) error {
	m.got = rq
	return m.err
}

func (m *mockWorkflowCrud) ListTasksFromWorkflow(_ context.Context, rq entity.ListTasksFromWorkflowRequest) (*entity.ListTasksFromWorkflowResponse, error) {
	m.got = rq
	if m.err != nil {
		return nil, m.err
	}
	return &entity.ListTasksFromWorkflowResponse{Tasks: []entity.Task{{ID: 77, Title: "spawned"}}}, nil
}

func (m *mockWorkflowCrud) GetWorkflowText(_ context.Context, rq entity.GetWorkflowTextRequest) (*entity.GetWorkflowTextResponse, error) {
	m.got = rq
	if m.err != nil {
		return nil, m.err
	}
	return &entity.GetWorkflowTextResponse{Text: "workflow: release\n"}, nil
}

func (m *mockWorkflowCrud) ReplaceWorkflowFromText(_ context.Context, rq entity.ReplaceWorkflowFromTextRequest) (*entity.ReplaceWorkflowFromTextResponse, error) {
	m.got = rq
	if m.err != nil {
		return nil, m.err
	}
	return &entity.ReplaceWorkflowFromTextResponse{Workflow: entity.Workflow{ID: rq.ID, Name: "release"}, StepCount: 2}, nil
}

func workflowApp(c crud.Controller) *fiber.App {
	app := fiber.New()
	app.Use(func(ctx *fiber.Ctx) error {
		ctx.Locals("user_id", "user-1")
		return ctx.Next()
	})
	h := &handler{crud: c, router: app}
	h.registerWorkflowRoutes()
	return app
}

var (
	wfID     = monoflake.ID(5).String()
	wfStepID = monoflake.ID(300).String()
	wfEvent  = monoflake.ID(10).String()
	wfEmit   = monoflake.ID(11).String()
	wfWS     = monoflake.ID(20).String()
)

func doWorkflow(t *testing.T, c crud.Controller, method, path, body string) (int, string) {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, r)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := workflowApp(c).Test(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(b)
}

func TestWorkflowRoutes(t *testing.T) {
	errDB := errors.New("db down")

	for _, tc := range []struct {
		name, method, path, body string
		err                      error
		status                   int
		// want is the request the controller must have received; nil means
		// the controller must not have been called at all.
		want     any
		contains []string
	}{
		// ── create ──
		{name: "create", method: http.MethodPost, path: "/workflows",
			body:     `{"name":"release","description":"d","startEventId":"` + wfEvent + `"}`,
			status:   http.StatusCreated,
			want:     entity.CreateWorkflowRequest{Name: "release", Description: "d", StartEventID: 10, UserID: "user-1"},
			contains: []string{`"workflow":{"id":"` + wfID + `"`, `"name":"release"`, `"startEventId":"` + wfEvent + `"`}},
		{name: "create with malformed JSON", method: http.MethodPost, path: "/workflows", body: `{`,
			status: http.StatusUnprocessableEntity, contains: []string{"invalid request payload"}},
		{name: "create without a name", method: http.MethodPost, path: "/workflows", body: `{"description":"d"}`,
			status: http.StatusUnprocessableEntity, contains: []string{"invalid request payload"}},
		{name: "create with an invalid name", method: http.MethodPost, path: "/workflows", body: `{"name":"Bad"}`,
			err:      crud.ErrInvalidWorkflowName,
			status:   http.StatusUnprocessableEntity,
			want:     entity.CreateWorkflowRequest{Name: "Bad", UserID: "user-1"},
			contains: []string{`must match`, `"code":422`}},
		{name: "create with a taken name", method: http.MethodPost, path: "/workflows", body: `{"name":"release"}`,
			err:      fmt.Errorf("%w: %q", crud.ErrDuplicateName, "release"),
			status:   http.StatusConflict,
			want:     entity.CreateWorkflowRequest{Name: "release", UserID: "user-1"},
			contains: []string{`name already exists`, `"code":409`}},
		{name: "create fails", method: http.MethodPost, path: "/workflows", body: `{"name":"release"}`,
			err:      errDB,
			status:   http.StatusInternalServerError,
			want:     entity.CreateWorkflowRequest{Name: "release", UserID: "user-1"},
			contains: []string{"internal server error"}},

		// ── list ──
		{name: "list", method: http.MethodGet, path: "/workflows",
			status:   http.StatusOK,
			want:     entity.ListWorkflowsRequest{UserID: "user-1"},
			contains: []string{`"workflows":[`, `"name":"release"`, `"name":"deploy"`}},
		{name: "list fails", method: http.MethodGet, path: "/workflows", err: errDB,
			status: http.StatusInternalServerError,
			want:   entity.ListWorkflowsRequest{UserID: "user-1"}},

		// ── get ──
		{name: "get", method: http.MethodGet, path: "/workflows/" + wfID,
			status:   http.StatusOK,
			want:     entity.GetWorkflowRequest{ID: 5, UserID: "user-1"},
			contains: []string{`"id":"` + wfID + `"`, `"layout":{"x":1}`}},
		{name: "get with a bad id", method: http.MethodGet, path: "/workflows/0",
			status: http.StatusUnprocessableEntity},
		{name: "get someone else's", method: http.MethodGet, path: "/workflows/" + wfID, err: base.ErrNotFound,
			status:   http.StatusNotFound,
			want:     entity.GetWorkflowRequest{ID: 5, UserID: "user-1"},
			contains: []string{"not found"}},

		// ── update ──
		{name: "update every field", method: http.MethodPatch, path: "/workflows/" + wfID,
			body:   `{"name":"renamed","description":"d","startEventId":"` + wfEvent + `","layout":{"x":1}}`,
			status: http.StatusOK,
			want: entity.UpdateWorkflowRequest{ID: 5, UserID: "user-1",
				Name: ptrTo("renamed"), Description: ptrTo("d"), StartEventID: ptrTo(int64(10)), Layout: ptrTo(`{"x":1}`)},
			contains: []string{`"name":"renamed"`}},
		{name: "update only the layout", method: http.MethodPatch, path: "/workflows/" + wfID,
			body:   `{"layout":[]}`,
			status: http.StatusOK,
			want:   entity.UpdateWorkflowRequest{ID: 5, UserID: "user-1", Layout: ptrTo(`[]`)}},
		{name: "update with a bad id", method: http.MethodPatch, path: "/workflows/0", body: `{}`,
			status: http.StatusUnprocessableEntity},
		{name: "update with malformed JSON", method: http.MethodPatch, path: "/workflows/" + wfID, body: `{`,
			status: http.StatusUnprocessableEntity},
		{name: "update with an invalid name", method: http.MethodPatch, path: "/workflows/" + wfID, body: `{"name":"Bad"}`,
			err:      crud.ErrInvalidWorkflowName,
			status:   http.StatusUnprocessableEntity,
			want:     entity.UpdateWorkflowRequest{ID: 5, UserID: "user-1", Name: ptrTo("Bad")},
			contains: []string{`must match`}},
		{name: "update fails", method: http.MethodPatch, path: "/workflows/" + wfID, body: `{}`,
			err:    base.ErrNotFound,
			status: http.StatusNotFound,
			want:   entity.UpdateWorkflowRequest{ID: 5, UserID: "user-1"}},

		// ── delete ──
		{name: "delete", method: http.MethodDelete, path: "/workflows/" + wfID,
			status: http.StatusNoContent,
			want:   entity.DeleteWorkflowRequest{ID: 5, UserID: "user-1"}},
		{name: "delete with a bad id", method: http.MethodDelete, path: "/workflows/0",
			status: http.StatusUnprocessableEntity},
		{name: "delete fails", method: http.MethodDelete, path: "/workflows/" + wfID, err: base.ErrNotFound,
			status: http.StatusNotFound,
			want:   entity.DeleteWorkflowRequest{ID: 5, UserID: "user-1"}},

		// ── steps ──
		{name: "create step defaults the assignee to the agent", method: http.MethodPost, path: "/workflows/" + wfID + "/steps",
			body:   `{"eventId":"` + wfEvent + `","workspaceId":"` + wfWS + `","emitEventId":"` + wfEmit + `","title":"t","body":"b","allowAllCommands":true}`,
			status: http.StatusCreated,
			want: entity.CreateWorkflowStepRequest{WorkflowID: 5, EventID: 10, WorkspaceID: 20, EmitEventID: 11,
				Title: "t", Body: "b", Assignee: "agent", AllowAllCommands: true, UserID: "user-1"},
			contains: []string{`"workflowStep":{"id":"` + wfStepID + `"`, `"emitEventId":"` + wfEmit + `"`, `"assignee":"agent"`}},
		{name: "create step keeps an explicit assignee", method: http.MethodPost, path: "/workflows/" + wfID + "/steps",
			body:   `{"eventId":"` + wfEvent + `","workspaceId":"` + wfWS + `","title":"t","assignee":"human"}`,
			status: http.StatusCreated,
			want: entity.CreateWorkflowStepRequest{WorkflowID: 5, EventID: 10, WorkspaceID: 20,
				Title: "t", Assignee: "human", UserID: "user-1"}},
		{name: "create step with a bad workflow id", method: http.MethodPost, path: "/workflows/0/steps",
			body: `{"eventId":"` + wfEvent + `","workspaceId":"` + wfWS + `","title":"t"}`, status: http.StatusUnprocessableEntity},
		{name: "create step with malformed JSON", method: http.MethodPost, path: "/workflows/" + wfID + "/steps",
			body: `{`, status: http.StatusUnprocessableEntity},
		{name: "create step without a title", method: http.MethodPost, path: "/workflows/" + wfID + "/steps",
			body: `{"eventId":"` + wfEvent + `","workspaceId":"` + wfWS + `"}`, status: http.StatusUnprocessableEntity},
		{name: "create step without an event", method: http.MethodPost, path: "/workflows/" + wfID + "/steps",
			body: `{"workspaceId":"` + wfWS + `","title":"t"}`, status: http.StatusUnprocessableEntity},
		{name: "create step without a workspace", method: http.MethodPost, path: "/workflows/" + wfID + "/steps",
			body: `{"eventId":"` + wfEvent + `","title":"t"}`, status: http.StatusUnprocessableEntity},
		{name: "create step that closes a cycle", method: http.MethodPost, path: "/workflows/" + wfID + "/steps",
			body:   `{"eventId":"` + wfEvent + `","workspaceId":"` + wfWS + `","title":"t"}`,
			err:    crud.ErrWorkflowCycle,
			status: http.StatusConflict,
			want: entity.CreateWorkflowStepRequest{WorkflowID: 5, EventID: 10, WorkspaceID: 20,
				Title: "t", Assignee: "agent", UserID: "user-1"},
			contains: []string{"cycle", `"code":409`}},
		{name: "create step fails", method: http.MethodPost, path: "/workflows/" + wfID + "/steps",
			body:   `{"eventId":"` + wfEvent + `","workspaceId":"` + wfWS + `","title":"t"}`,
			err:    errDB,
			status: http.StatusInternalServerError,
			want: entity.CreateWorkflowStepRequest{WorkflowID: 5, EventID: 10, WorkspaceID: 20,
				Title: "t", Assignee: "agent", UserID: "user-1"}},
		{name: "list steps", method: http.MethodGet, path: "/workflows/" + wfID + "/steps",
			status:   http.StatusOK,
			want:     entity.ListWorkflowStepsRequest{WorkflowID: 5, UserID: "user-1"},
			contains: []string{`"workflowSteps":[{"id":"` + wfStepID + `"`, `"workspaceId":"` + wfWS + `"`}},
		{name: "list steps with a bad id", method: http.MethodGet, path: "/workflows/0/steps",
			status: http.StatusUnprocessableEntity},
		{name: "list steps fails", method: http.MethodGet, path: "/workflows/" + wfID + "/steps", err: errDB,
			status: http.StatusInternalServerError,
			want:   entity.ListWorkflowStepsRequest{WorkflowID: 5, UserID: "user-1"}},
		{name: "delete step", method: http.MethodDelete, path: "/workflows/" + wfID + "/steps/" + wfStepID,
			status: http.StatusNoContent,
			want:   entity.DeleteWorkflowStepRequest{ID: 300, WorkflowID: 5, UserID: "user-1"}},
		{name: "delete step with a bad step id", method: http.MethodDelete, path: "/workflows/" + wfID + "/steps/0",
			status: http.StatusUnprocessableEntity},
		{name: "delete step with a bad workflow id", method: http.MethodDelete, path: "/workflows/0/steps/" + wfStepID,
			status: http.StatusUnprocessableEntity},
		{name: "delete step fails", method: http.MethodDelete, path: "/workflows/" + wfID + "/steps/" + wfStepID,
			err:    base.ErrNotFound,
			status: http.StatusNotFound,
			want:   entity.DeleteWorkflowStepRequest{ID: 300, WorkflowID: 5, UserID: "user-1"}},

		// ── tasks ──
		{name: "list tasks", method: http.MethodGet, path: "/workflows/" + wfID + "/tasks",
			status:   http.StatusOK,
			want:     entity.ListTasksFromWorkflowRequest{WorkflowID: 5, UserID: "user-1"},
			contains: []string{`"tasks":[`, `"title":"spawned"`}},
		{name: "list tasks with a bad id", method: http.MethodGet, path: "/workflows/0/tasks",
			status: http.StatusUnprocessableEntity},
		{name: "list tasks fails", method: http.MethodGet, path: "/workflows/" + wfID + "/tasks", err: errDB,
			status: http.StatusInternalServerError,
			want:   entity.ListTasksFromWorkflowRequest{WorkflowID: 5, UserID: "user-1"}},

		// ── text ──
		{name: "get text", method: http.MethodGet, path: "/workflows/" + wfID + "/text",
			status:   http.StatusOK,
			want:     entity.GetWorkflowTextRequest{ID: 5, UserID: "user-1"},
			contains: []string{`"text":"workflow: release\n"`}},
		{name: "get text with a bad id", method: http.MethodGet, path: "/workflows/0/text",
			status: http.StatusUnprocessableEntity},
		{name: "get text fails", method: http.MethodGet, path: "/workflows/" + wfID + "/text", err: base.ErrNotFound,
			status: http.StatusNotFound,
			want:   entity.GetWorkflowTextRequest{ID: 5, UserID: "user-1"}},
		{name: "replace from text", method: http.MethodPut, path: "/workflows/" + wfID + "/text",
			body:     `{"text":"workflow: release\n"}`,
			status:   http.StatusOK,
			want:     entity.ReplaceWorkflowFromTextRequest{ID: 5, UserID: "user-1", Text: "workflow: release\n"},
			contains: []string{`"stepCount":2`, `"workflow":{"id":"` + wfID + `"`}},
		{name: "replace from text with a bad id", method: http.MethodPut, path: "/workflows/0/text",
			body: `{"text":""}`, status: http.StatusUnprocessableEntity},
		{name: "replace from text with malformed JSON", method: http.MethodPut, path: "/workflows/" + wfID + "/text",
			body: `{`, status: http.StatusUnprocessableEntity},
		{name: "replace from a document with a mistake", method: http.MethodPut, path: "/workflows/" + wfID + "/text",
			body:     `{"text":"x"}`,
			err:      &crud.WorkflowTextError{Line: 3, Msg: "unknown workspace \"ghost\""},
			status:   http.StatusUnprocessableEntity,
			want:     entity.ReplaceWorkflowFromTextRequest{ID: 5, UserID: "user-1", Text: "x"},
			contains: []string{`{"error":{"message":"unknown workspace \"ghost\"","line":3}}`}},
		{name: "replace from text fails", method: http.MethodPut, path: "/workflows/" + wfID + "/text",
			body:   `{"text":"x"}`,
			err:    errDB,
			status: http.StatusInternalServerError,
			want:   entity.ReplaceWorkflowFromTextRequest{ID: 5, UserID: "user-1", Text: "x"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &mockWorkflowCrud{err: tc.err}
			status, body := doWorkflow(t, c, tc.method, tc.path, tc.body)
			if status != tc.status {
				t.Fatalf("status = %d, want %d (body %s)", status, tc.status, body)
			}
			if tc.want == nil {
				if c.got != nil {
					t.Errorf("the controller should not have been called, got %+v", c.got)
				}
			} else if !equalWorkflowRequest(c.got, tc.want) {
				t.Errorf("controller got %#v, want %#v", c.got, tc.want)
			}
			for _, s := range tc.contains {
				if !strings.Contains(body, s) {
					t.Errorf("body %s does not contain %s", body, s)
				}
			}
		})
	}
}

func ptrTo[T any](v T) *T { return &v }

// equalWorkflowRequest compares requests by value, following the pointer
// fields of UpdateWorkflowRequest rather than their addresses.
func equalWorkflowRequest(got, want any) bool {
	g, ok := got.(entity.UpdateWorkflowRequest)
	if !ok {
		return got == want
	}
	w, ok := want.(entity.UpdateWorkflowRequest)
	if !ok {
		return false
	}
	return g.ID == w.ID && g.UserID == w.UserID &&
		samePtr(g.Name, w.Name) && samePtr(g.Description, w.Description) &&
		samePtr(g.StartEventID, w.StartEventID) && samePtr(g.Layout, w.Layout)
}

func samePtr[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
