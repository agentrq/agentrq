// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/gofiber/fiber/v2"
	"github.com/mustafaturan/monoflake"
)

type mockCrudEnsureSupervisor struct {
	crud.Controller
	ensureFunc func(ctx context.Context, req entity.EnsureSupervisorWorkspaceRequest) (*entity.GetWorkspaceResponse, error)
}

func (m *mockCrudEnsureSupervisor) EnsureSupervisorWorkspace(ctx context.Context, req entity.EnsureSupervisorWorkspaceRequest) (*entity.GetWorkspaceResponse, error) {
	return m.ensureFunc(ctx, req)
}

func ensureSupervisorApp(crudCtrl crud.Controller, mgr *fakeMCPManager) *fiber.App {
	app := fiber.New()
	h := &handler{crud: crudCtrl, mcpManager: mgr}
	app.Post("/api/v1/workspaces/supervisor", func(c *fiber.Ctx) error {
		c.Locals("user_id", monoflake.ID(100).String())
		return h.ensureSupervisorWorkspace()(c)
	})
	return app
}

func TestEnsureSupervisorWorkspace_AnswersWithTheWorkspaceAndWhetherItsAgentIsConnected(t *testing.T) {
	var askedFor string
	crudCtrl := &mockCrudEnsureSupervisor{ensureFunc: func(ctx context.Context, req entity.EnsureSupervisorWorkspaceRequest) (*entity.GetWorkspaceResponse, error) {
		askedFor = req.UserID
		return &entity.GetWorkspaceResponse{Workspace: entity.Workspace{ID: 7, Name: "supervisor"}}, nil
	}}
	app := ensureSupervisorApp(crudCtrl, &fakeMCPManager{connected: true})

	resp, err := app.Test(httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/supervisor", nil))
	if err != nil {
		t.Fatalf("the request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status was %d, want 200", resp.StatusCode)
	}
	if askedFor != monoflake.ID(100).String() {
		t.Errorf("the controller was asked for user %q, want the signed-in user %q", askedFor, monoflake.ID(100).String())
	}
	var body struct {
		Workspace struct {
			ID             string `json:"id"`
			Name           string `json:"name"`
			AgentConnected bool   `json:"agentConnected"`
		} `json:"workspace"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("the body is not JSON: %v", err)
	}
	if body.Workspace.ID != monoflake.ID(7).String() || body.Workspace.Name != "supervisor" || !body.Workspace.AgentConnected {
		t.Errorf("the body carried %+v, want the supervisor workspace %s with its agent connected", body.Workspace, monoflake.ID(7).String())
	}
}

func TestEnsureSupervisorWorkspace_AnswersAnErrorWhenTheControllerFails(t *testing.T) {
	crudCtrl := &mockCrudEnsureSupervisor{ensureFunc: func(ctx context.Context, req entity.EnsureSupervisorWorkspaceRequest) (*entity.GetWorkspaceResponse, error) {
		return nil, errors.New("database unavailable")
	}}
	app := ensureSupervisorApp(crudCtrl, &fakeMCPManager{})

	resp, err := app.Test(httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/supervisor", nil))
	if err != nil {
		t.Fatalf("the request failed: %v", err)
	}
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status was %d, want 500 for a database failure", resp.StatusCode)
	}
}
