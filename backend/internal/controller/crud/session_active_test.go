// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package crud

import (
	"errors"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/mustafaturan/monoflake"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
)

// The half of "does this workspace already have an agent?" that the database
// answers. A session that has been started but has not connected yet is
// invisible to the live-connection check, and that window is exactly long
// enough for somebody to press the button twice.
func TestActiveSessionForWorkspace(t *testing.T) {
	env := newTestController(t)
	uid := monoflake.IDFromBase62(testUserBase62).Int64()
	env.repo.EXPECT().ActiveSessionForWorkspace(gomock.Any(), int64(7), uid).
		Return(model.Session{ID: 9, WorkspaceID: 7, Status: "starting"}, nil)

	got, err := env.controller.ActiveSessionForWorkspace(t.Context(), entity.ActiveSessionRequest{
		UserID: testUserBase62, WorkspaceID: monoflake.ID(7).String(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Status != "starting" {
		t.Fatalf("active = %+v", got)
	}
}

// No agent is the ordinary answer, and not an error: every first launch for a
// workspace passes through here.
func TestNoActiveSessionIsNotAnError(t *testing.T) {
	env := newTestController(t)
	uid := monoflake.IDFromBase62(testUserBase62).Int64()
	env.repo.EXPECT().ActiveSessionForWorkspace(gomock.Any(), int64(7), uid).
		Return(model.Session{}, base.ErrNotFound)

	got, err := env.controller.ActiveSessionForWorkspace(t.Context(), entity.ActiveSessionRequest{
		UserID: testUserBase62, WorkspaceID: monoflake.ID(7).String(),
	})
	if err != nil {
		t.Fatalf("a workspace with no agent reported an error: %v", err)
	}
	if got != nil {
		t.Errorf("active = %+v, want nothing", got)
	}
}

// A database that is down must not read as "no agent here", which would let a
// second one start.
func TestARealFailureIsNotMistakenForNoAgent(t *testing.T) {
	env := newTestController(t)
	uid := monoflake.IDFromBase62(testUserBase62).Int64()
	env.repo.EXPECT().ActiveSessionForWorkspace(gomock.Any(), int64(7), uid).
		Return(model.Session{}, errors.New("database is down"))

	if _, err := env.controller.ActiveSessionForWorkspace(t.Context(), entity.ActiveSessionRequest{
		UserID: testUserBase62, WorkspaceID: monoflake.ID(7).String(),
	}); err == nil {
		t.Error("a failed query was reported as no agent")
	}
}

func TestActiveSessionRefusesAnUnusableID(t *testing.T) {
	env := newTestController(t)
	if _, err := env.controller.ActiveSessionForWorkspace(t.Context(), entity.ActiveSessionRequest{}); err == nil {
		t.Error("an empty request was accepted")
	}
}
