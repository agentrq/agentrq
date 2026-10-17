// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
	"github.com/gofiber/fiber/v2"
	"github.com/mustafaturan/monoflake"
)

type mockCrudResetWorkspaceID struct {
	crud.Controller
	got entity.ResetWorkspaceIDRequest
	err error
}

func (m *mockCrudResetWorkspaceID) ResetWorkspaceID(_ context.Context, req entity.ResetWorkspaceIDRequest) (*entity.ResetWorkspaceIDResponse, error) {
	m.got = req
	if m.err != nil {
		return nil, m.err
	}
	return &entity.ResetWorkspaceIDResponse{ID: 30, ForkIDs: []int64{wfForkID}}, nil
}

func resetApp(ctrl *mockCrudResetWorkspaceID, mgr *fakeMCPManager) *fiber.App {
	app := fiber.New()
	h := &handler{crud: ctrl, mcpManager: mgr, bus: eventbus.New(), router: app.Group("")}
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user_id", monoflake.ID(100).String())
		return c.Next()
	})
	_ = h.registerWorkspaceRoutes()
	return app
}

func TestResetWorkspaceIDRoute_AnswersTheNewIDAndDropsTheServers(t *testing.T) {
	ctrl := &mockCrudResetWorkspaceID{}
	mgr := &fakeMCPManager{}
	status, body := send(t, resetApp(ctrl, mgr), http.MethodPost, "/workspaces/"+monoflake.ID(wfParentID).String()+"/reset-id", "")
	if status != http.StatusOK {
		t.Fatalf("the reset answered %d %s, want 200", status, body)
	}
	if ctrl.got.ID != wfParentID || ctrl.got.UserID != monoflake.ID(100).String() {
		t.Errorf("the controller was asked %+v, want workspace %d for user %s", ctrl.got, wfParentID, monoflake.ID(100).String())
	}
	if want := `{"id":"` + monoflake.ID(30).String() + `"}`; body != want {
		t.Errorf("the reset answered %s, want %s", body, want)
	}
	// Its forks' servers too: they read its content under the old id.
	if want := []int64{wfParentID, wfForkID}; !reflect.DeepEqual(mgr.removed, want) {
		t.Errorf("the servers of %v were dropped, want %v", mgr.removed, want)
	}
}

func TestResetWorkspaceIDRoute_ABadIDIsRefused(t *testing.T) {
	ctrl := &mockCrudResetWorkspaceID{}
	if status, body := send(t, resetApp(ctrl, &fakeMCPManager{}), http.MethodPost, "/workspaces/0/reset-id", ""); status != http.StatusUnprocessableEntity {
		t.Fatalf("a reset of workspace 0 answered %d %s, want 422", status, body)
	}
	if ctrl.got.ID != 0 {
		t.Errorf("the controller was asked to reset %d, want no call", ctrl.got.ID)
	}
}

func TestResetWorkspaceIDRoute_AFailureKeepsTheServers(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
	}{
		{base.ErrNotFound, http.StatusNotFound},
		{errors.New("database unavailable"), http.StatusInternalServerError},
	} {
		mgr := &fakeMCPManager{}
		status, body := send(t, resetApp(&mockCrudResetWorkspaceID{err: tc.err}, mgr), http.MethodPost, "/workspaces/"+monoflake.ID(wfParentID).String()+"/reset-id", "")
		if status != tc.status {
			t.Errorf("a reset failing with %v answered %d %s, want %d", tc.err, status, body, tc.status)
		}
		if len(mgr.removed) != 0 {
			t.Errorf("a reset failing with %v dropped the servers of %v, want none", tc.err, mgr.removed)
		}
	}
}
