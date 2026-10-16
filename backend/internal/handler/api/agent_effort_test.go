// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/controller/agentlink"
	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	mcpctrl "github.com/agentrq/agentrq/backend/internal/controller/mcp"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/gofiber/fiber/v2"
	"github.com/mustafaturan/monoflake"
)

func TestSetAgentEffort(t *testing.T) {
	cases := []struct {
		name        string
		body        string
		access      bool
		noServer    bool
		err         error
		countErr    error
		want        int
		wantBody    string
		wantAsked   bool
		wantCounted bool
	}{
		{name: "a body that is not JSON", body: `not json`, access: true, want: http.StatusBadRequest, wantBody: "invalid request payload"},
		{name: "no level named", body: `{"effort":"  "}`, access: true, want: http.StatusBadRequest, wantBody: "effort is required"},
		{name: "a caller without access", body: `{"effort":"low"}`, want: http.StatusForbidden},
		{name: "no server for the workspace", body: `{"effort":"low"}`, access: true, noServer: true, want: http.StatusNotFound},
		{name: "nothing connected offers a level", body: `{"effort":"low"}`, access: true, err: mcpctrl.ErrEffortSelectUnsupported, want: http.StatusConflict,
			wantBody: "the connected agent does not support choosing a reasoning level", wantAsked: true},
		{name: "a level not on offer", body: `{"effort":"max"}`, access: true, err: mcpctrl.ErrEffortNotOffered, want: http.StatusBadRequest, wantAsked: true},
		{name: "the agent went away", body: `{"effort":"low"}`, access: true, err: mcpctrl.ErrEffortSetNotDelivered, want: http.StatusConflict, wantAsked: true},
		{name: "anything else", body: `{"effort":"low"}`, access: true, err: errors.New("unexpected effort switch failure"), want: http.StatusInternalServerError,
			wantBody: "failed to select the reasoning level", wantAsked: true},
		{name: "asked and counted", body: `{"effort":"low"}`, access: true, want: http.StatusAccepted, wantBody: `{"requested":"low"}`, wantAsked: true, wantCounted: true},
		{name: "asked though the count failed", body: `{"effort":"low"}`, access: true, countErr: errors.New("database unavailable"), want: http.StatusAccepted, wantAsked: true, wantCounted: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var counted []entity.Action
			crudCtrl := &mockCrudWorkspaceAccess{
				checkWorkspaceAccessFunc: func(ctx context.Context, id int64, userID string) (bool, error) { return tc.access, nil },
				recordTelemetryFunc: func(ctx context.Context, rq entity.RecordTelemetryRequest) error {
					counted = append(counted, rq.Action)
					return tc.countErr
				},
			}
			srv := &fakeWorkspaceServer{effortErr: tc.err}
			mgr := &fakeMCPManager{server: srv}
			if tc.noServer {
				mgr.server = nil
			}
			h := &handler{crud: crudCtrl, mcpManager: mgr}
			app := fiber.New()
			app.Post("/api/v1/workspaces/:id/agent/effort", func(c *fiber.Ctx) error {
				c.Locals("user_id", monoflake.ID(100).String())
				return h.setAgentEffort()(c)
			})

			req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/"+monoflake.ID(1).String()+"/agent/effort", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			body, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != tc.want || !strings.Contains(string(body), tc.wantBody) {
				t.Fatalf("got %d %s, want %d containing %q", resp.StatusCode, body, tc.want, tc.wantBody)
			}
			if asked := srv.effortCalls == 1; asked != tc.wantAsked {
				t.Fatalf("asked the agent %d times, want asked=%v", srv.effortCalls, tc.wantAsked)
			}
			if tc.wantAsked && !strings.Contains(tc.body, `"`+srv.effort+`"`) {
				t.Fatalf("asked for %q", srv.effort)
			}
			if got := len(counted) == 1 && counted[0] == entity.ActionAgentEffortSelect; got != tc.wantCounted {
				t.Fatalf("counted %v, want counted=%v", counted, tc.wantCounted)
			}
		})
	}
}

func TestAgentEffortEntity(t *testing.T) {
	if agentEffortEntity(nil) != nil {
		t.Fatal("a nil snapshot did not stay nil")
	}
	got := agentEffortEntity(&agentlink.EffortSnapshot{
		SessionID: "s1", ConfigID: "effort", CurrentEffort: "high", CanSet: true,
		Efforts: []agentlink.Effort{{ID: "high", Name: "High", Description: "slower"}},
	})
	if got.ConfigID != "effort" || got.CurrentEffort != "high" || !got.CanSet ||
		len(got.Efforts) != 1 || got.Efforts[0] != (entity.AgentEffortLevel{ID: "high", Name: "High", Description: "slower"}) {
		t.Fatalf("got %+v", got)
	}
	if agentEffortEntity(&agentlink.EffortSnapshot{CanSet: true}).CanSet {
		t.Fatal("an offer with no config option to write to was settable")
	}
}

type effortWorkspaceCrud struct{ crud.Controller }

func (effortWorkspaceCrud) GetWorkspace(context.Context, entity.GetWorkspaceRequest) (*entity.GetWorkspaceResponse, error) {
	return &entity.GetWorkspaceResponse{Workspace: entity.Workspace{ID: 1, Name: "ws"}}, nil
}

func (effortWorkspaceCrud) CreateWorkspace(context.Context, entity.CreateWorkspaceRequest) (*entity.CreateWorkspaceResponse, error) {
	return &entity.CreateWorkspaceResponse{Workspace: entity.Workspace{ID: 1, Name: "ws"}}, nil
}

// The workspace payload carries the agent's reasoning levels, so a page that
// loads after the agent reported them shows the picker.
func TestWorkspacePayloadCarriesTheAgentEffort(t *testing.T) {
	h := &handler{crud: effortWorkspaceCrud{}, mcpManager: &fakeMCPManager{effort: &agentlink.EffortSnapshot{
		ConfigID: "effort", CurrentEffort: "high", CanSet: true, Efforts: []agentlink.Effort{{ID: "high", Name: "High"}},
	}}}
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error { c.Locals("user_id", monoflake.ID(100).String()); return c.Next() })
	app.Get("/workspaces/:id", h.getWorkspace())
	app.Post("/workspaces", h.createWorkspace())

	for _, req := range []*http.Request{
		httptest.NewRequest(http.MethodGet, "/workspaces/"+monoflake.ID(1).String(), nil),
		httptest.NewRequest(http.MethodPost, "/workspaces", strings.NewReader(`{"workspace":{"name":"ws"}}`)),
	} {
		resp, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		if want := `"agentEffort":{"configId":"effort","currentEffort":"high","efforts":[{"id":"high","name":"High"}],"canSet":true}`; !strings.Contains(string(body), want) {
			t.Fatalf("%s %s answered %d %s, want it to carry %s", req.Method, req.URL.Path, resp.StatusCode, body, want)
		}
	}
}
