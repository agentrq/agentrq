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
	"time"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	"github.com/agentrq/agentrq/backend/internal/controller/forkmerge"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
	"github.com/gofiber/fiber/v3"
	"github.com/mustafaturan/monoflake"
)

const (
	wfParentID = int64(10)
	wfForkID   = int64(20)
)

type mockCrudWorkspaceFork struct {
	crud.Controller
	forkGot  entity.ForkWorkspaceRequest
	mergeGot entity.MergeForkRequest
	err      error
	list     []entity.Workspace
	forkIDs  []int64
}

func (m *mockCrudWorkspaceFork) ForkWorkspace(_ context.Context, req entity.ForkWorkspaceRequest) (*entity.ForkWorkspaceResponse, error) {
	m.forkGot = req
	if m.err != nil {
		return nil, m.err
	}
	return &entity.ForkWorkspaceResponse{Workspace: entity.Workspace{
		ID: wfForkID, Name: "api fork", ForkOfID: wfParentID,
		ForkOf: &entity.WorkspaceRef{ID: wfParentID, Name: "api"},
	}}, nil
}

func (m *mockCrudWorkspaceFork) MergeFork(_ context.Context, req entity.MergeForkRequest) (*entity.MergeForkResponse, error) {
	m.mergeGot = req
	if m.err != nil {
		return nil, m.err
	}
	return &entity.MergeForkResponse{
		ParentID:   monoflake.ID(wfParentID).String(),
		MovedTasks: 2,
		Tasks:      []entity.Task{{ID: 7, WorkspaceID: wfParentID, Title: "done"}},
	}, nil
}

// The pre-check and the agent lookup a merge makes first; the merge tests in
// workspace_fork_merge_test.go answer them otherwise.
func (m *mockCrudWorkspaceFork) CheckForkMerge(context.Context, entity.MergeForkRequest) error {
	return nil
}

func (m *mockCrudWorkspaceFork) ActiveSessionForWorkspace(context.Context, entity.ActiveSessionRequest) (*entity.SessionView, error) {
	return nil, nil
}

func (m *mockCrudWorkspaceFork) ListWorkspaces(context.Context, entity.ListWorkspacesRequest) (*entity.ListWorkspacesResponse, error) {
	return &entity.ListWorkspacesResponse{Workspaces: m.list}, nil
}

func (m *mockCrudWorkspaceFork) UpdateWorkspace(_ context.Context, req entity.UpdateWorkspaceRequest) (*entity.UpdateWorkspaceResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &entity.UpdateWorkspaceResponse{Workspace: req.Workspace, ForkIDs: m.forkIDs}, nil
}

func forkApp(ctrl *mockCrudWorkspaceFork, mgr *fakeMCPManager, bus *eventbus.Bus) *fiber.App {
	app := fiber.New()
	forks := &forkmerge.Merger{Crud: ctrl, Servers: mgr, Bus: bus}
	h := &handler{crud: ctrl, mcpManager: mgr, bus: bus, forks: forks, router: app.Group("")}
	app.Use(func(c fiber.Ctx) error {
		c.Locals("user_id", monoflake.ID(100).String())
		return c.Next()
	})
	_ = h.registerWorkspaceRoutes()
	return app
}

func send(t *testing.T, app *fiber.App, method, url, body string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(method, url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestForkWorkspaceRoute_Creates(t *testing.T) {
	ctrl := &mockCrudWorkspaceFork{}
	app := forkApp(ctrl, &fakeMCPManager{}, eventbus.New())
	status, body := send(t, app, http.MethodPost, "/workspaces/"+monoflake.ID(wfParentID).String()+"/forks", `{"name":"  billing  "}`)
	if status != http.StatusCreated {
		t.Fatalf("status = %d, body %s", status, body)
	}
	if ctrl.forkGot.WorkspaceID != wfParentID || ctrl.forkGot.Name != "billing" || ctrl.forkGot.UserID != monoflake.ID(100).String() {
		t.Errorf("request = %+v", ctrl.forkGot)
	}
	var out struct {
		Workspace struct {
			ID       string `json:"id"`
			ForkOfID string `json:"forkOfId"`
			ForkOf   struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"forkOf"`
			MCPURL string `json:"mcpUrl"`
		} `json:"workspace"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatal(err)
	}
	w := out.Workspace
	if w.ID != monoflake.ID(wfForkID).String() || w.ForkOfID != monoflake.ID(wfParentID).String() || w.ForkOf.Name != "api" || w.ForkOf.ID != w.ForkOfID {
		t.Errorf("workspace = %+v", w)
	}
}

func TestForkWorkspaceRoute_NoBodyIsFine(t *testing.T) {
	ctrl := &mockCrudWorkspaceFork{}
	app := forkApp(ctrl, &fakeMCPManager{}, eventbus.New())
	if status, body := send(t, app, http.MethodPost, "/workspaces/"+monoflake.ID(wfParentID).String()+"/forks", ""); status != http.StatusCreated {
		t.Fatalf("status = %d, body %s", status, body)
	}
	if ctrl.forkGot.Name != "" {
		t.Errorf("name = %q, want the default left to the controller", ctrl.forkGot.Name)
	}
}

func TestForkWorkspaceRoute_BadRequests(t *testing.T) {
	app := forkApp(&mockCrudWorkspaceFork{}, &fakeMCPManager{}, eventbus.New())
	for name, tc := range map[string]struct{ url, body string }{
		"bad id":        {"/workspaces/0/forks", `{}`},
		"not json":      {"/workspaces/" + monoflake.ID(wfParentID).String() + "/forks", `nope`},
		"name too long": {"/workspaces/" + monoflake.ID(wfParentID).String() + "/forks", `{"name":"` + strings.Repeat("x", 129) + `"}`},
	} {
		if status, _ := send(t, app, http.MethodPost, tc.url, tc.body); status != http.StatusUnprocessableEntity {
			t.Errorf("%s: status = %d, want 422", name, status)
		}
	}
}

// A refusal says why, in words the interface can show as they are.
func TestForkRoutes_RefusalsCarryTheirReason(t *testing.T) {
	for _, tc := range []struct {
		err    error
		route  string
		status int
	}{
		{entity.NewForkError(entity.ErrForkOfFork, "a fork cannot be forked"), "/forks", http.StatusUnprocessableEntity},
		{entity.NewForkError(entity.ErrForkUnfinished, "2 tasks in this fork are not finished"), "/merge", http.StatusConflict},
		{entity.NewForkError(entity.ErrNotAFork, "only a fork can be merged"), "/merge", http.StatusUnprocessableEntity},
	} {
		mgr := &fakeMCPManager{}
		app := forkApp(&mockCrudWorkspaceFork{err: tc.err}, mgr, eventbus.New())
		status, body := send(t, app, http.MethodPost, "/workspaces/"+monoflake.ID(wfForkID).String()+tc.route, "")
		if status != tc.status || !strings.Contains(body, tc.err.Error()) {
			t.Errorf("%v: %d %s", tc.err, status, body)
		}
		if len(mgr.removed) != 0 {
			t.Errorf("%v: a refused merge removed the fork's server", tc.err)
		}
	}
}

func TestMergeForkRoute_MovesAndTells(t *testing.T) {
	ctrl := &mockCrudWorkspaceFork{}
	mgr := &fakeMCPManager{}
	bus := eventbus.New()
	user := monoflake.ID(100).String()
	forkCh := bus.Subscribe(wfForkID, user)
	parentCh := bus.Subscribe(wfParentID, user)
	app := forkApp(ctrl, mgr, bus)

	status, body := send(t, app, http.MethodPost, "/workspaces/"+monoflake.ID(wfForkID).String()+"/merge", "")
	if status != http.StatusOK {
		t.Fatalf("status = %d, body %s", status, body)
	}
	if ctrl.mergeGot.WorkspaceID != wfForkID || ctrl.mergeGot.UserID != user {
		t.Errorf("request = %+v", ctrl.mergeGot)
	}
	if body != `{"parentId":"`+monoflake.ID(wfParentID).String()+`","movedTasks":2}` {
		t.Errorf("body = %s", body)
	}
	if len(mgr.removed) != 1 || mgr.removed[0] != wfForkID {
		t.Errorf("removed = %v, want the fork's server", mgr.removed)
	}
	for ch, want := range map[chan []byte]string{forkCh: "task.deleted", parentCh: "task.created"} {
		select {
		case msg := <-ch:
			if !strings.Contains(string(msg), want) || !strings.Contains(string(msg), monoflake.ID(7).String()) {
				t.Errorf("got %s, want %s", msg, want)
			}
		case <-time.After(time.Second):
			t.Errorf("no %s", want)
		}
	}
	if status, _ := send(t, app, http.MethodPost, "/workspaces/0/merge", ""); status != http.StatusUnprocessableEntity {
		t.Errorf("bad id: status = %d", status)
	}
}

func TestListWorkspacesRoute_CarriesForks(t *testing.T) {
	ctrl := &mockCrudWorkspaceFork{list: []entity.Workspace{
		{ID: wfParentID, Name: "api", ForkCount: 1},
		{ID: wfForkID, Name: "api fork", ForkOfID: wfParentID, ForkOf: &entity.WorkspaceRef{ID: wfParentID, Name: "api"}, UnfinishedTasks: 3},
	}}
	app := forkApp(ctrl, &fakeMCPManager{}, eventbus.New())
	status, body := send(t, app, http.MethodGet, "/workspaces", "")
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	var out struct {
		Workspaces []map[string]any `json:"workspaces"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatal(err)
	}
	parent, fork := out.Workspaces[0], out.Workspaces[1]
	if parent["forkCount"] != float64(1) || parent["forkOfId"] != nil || parent["unfinishedTasks"] != nil {
		t.Errorf("parent = %v", parent)
	}
	if fork["forkOfId"] != monoflake.ID(wfParentID).String() || fork["unfinishedTasks"] != float64(3) || fork["forkCount"] != nil {
		t.Errorf("fork = %v", fork)
	}
	if ref, _ := fork["forkOf"].(map[string]any); ref["name"] != "api" {
		t.Errorf("forkOf = %v", fork["forkOf"])
	}
}

// A parent's new tool list reaches its forks' running servers as well as its own.
func TestUpdateWorkspaceRoute_RefreshesTheForks(t *testing.T) {
	srv := &fakeWorkspaceServer{}
	ctrl := &mockCrudWorkspaceFork{forkIDs: []int64{21, 22}}
	app := forkApp(ctrl, &fakeMCPManager{server: srv}, eventbus.New())
	status, body := send(t, app, http.MethodPatch, "/workspaces/"+monoflake.ID(wfParentID).String(), `{"workspace":{"name":"api","autoAllowedTools":["Bash"]}}`)
	if status != http.StatusOK {
		t.Fatalf("status = %d, body %s", status, body)
	}
	if srv.autoAllowedCalls != 3 {
		t.Errorf("tool list pushed %d times, want the parent and both forks", srv.autoAllowedCalls)
	}
}
