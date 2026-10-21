// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/gofiber/fiber/v2"
	"github.com/mustafaturan/monoflake"
)

type mockCrudWorkspaceArchive struct {
	crud.Controller
	err error
}

func (m *mockCrudWorkspaceArchive) ArchiveWorkspace(context.Context, entity.ArchiveWorkspaceRequest) error {
	return m.err
}

func (m *mockCrudWorkspaceArchive) UnarchiveWorkspace(context.Context, entity.UnarchiveWorkspaceRequest) error {
	return m.err
}

func archiveApp(ctrl *mockCrudWorkspaceArchive, mgr *fakeMCPManager) *fiber.App {
	app := fiber.New()
	h := &handler{crud: ctrl, mcpManager: mgr, router: app.Group("")}
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user_id", monoflake.ID(100).String())
		return c.Next()
	})
	_ = h.registerWorkspaceRoutes()
	return app
}

// Archiving tells the running server at once, so its poller stops offering
// tasks without waiting for a restart; unarchiving tells it the reverse.
func TestArchiveWorkspaceRoute_TellsTheRunningServer(t *testing.T) {
	srv := &fakeWorkspaceServer{}
	app := archiveApp(&mockCrudWorkspaceArchive{}, &fakeMCPManager{server: srv})
	path := "/workspaces/" + monoflake.ID(wfParentID).String()

	if status, body := send(t, app, http.MethodPost, path+"/archive", ""); status != http.StatusOK {
		t.Fatalf("archive answered %d (%s), want 200", status, body)
	}
	if srv.archivedAt == nil {
		t.Fatal("the running server was not told the workspace is archived")
	}

	if status, body := send(t, app, http.MethodPost, path+"/unarchive", ""); status != http.StatusOK {
		t.Fatalf("unarchive answered %d (%s), want 200", status, body)
	}
	if srv.archivedAt != nil {
		t.Errorf("the running server still has the workspace archived at %v after unarchive", srv.archivedAt)
	}
}

// A refused archive leaves the running server as it was.
func TestArchiveWorkspaceRoute_LeavesTheServerAloneOnFailure(t *testing.T) {
	srv := &fakeWorkspaceServer{}
	app := archiveApp(&mockCrudWorkspaceArchive{err: base.ErrNotFound}, &fakeMCPManager{server: srv})

	if status, _ := send(t, app, http.MethodPost, "/workspaces/"+monoflake.ID(wfParentID).String()+"/archive", ""); status != http.StatusNotFound {
		t.Errorf("archive of a missing workspace answered %d, want 404", status)
	}
	if srv.archivedAtCalls != 0 {
		t.Errorf("the running server was updated %d times after a failed archive, want none", srv.archivedAtCalls)
	}
}
