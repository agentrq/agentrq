// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/mustafaturan/monoflake"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
)

type sessionCrud struct {
	crud.Controller
	active func(ctx context.Context, req entity.ActiveSessionRequest) (*entity.SessionView, error)
	// What the handler asked for, so the test can check the request was
	// assembled from the path and the session rather than from anything a
	// caller could choose.
	saw entity.ActiveSessionRequest
}

func (s *sessionCrud) ActiveSessionForWorkspace(ctx context.Context, req entity.ActiveSessionRequest) (*entity.SessionView, error) {
	s.saw = req
	return s.active(ctx, req)
}

var testSessionWorkspaceID = monoflake.ID(4242).String()

func workspaceSessionApp(c crud.Controller) *fiber.App {
	h := &handler{crud: c}
	app := fiber.New()
	// The session's user, exactly as the auth middleware supplies it.
	app.Use(func(ctx fiber.Ctx) error {
		ctx.Locals("user_id", "user-1")
		return ctx.Next()
	})
	app.Get(_routePathAgentSession, h.workspaceSession())
	return app
}

func TestWorkspaceSessionReturnsTheRunningAgent(t *testing.T) {
	started := time.Now()
	c := &sessionCrud{
		active: func(context.Context, entity.ActiveSessionRequest) (*entity.SessionView, error) {
			return &entity.SessionView{
				ID:          monoflake.ID(77).String(),
				MachineID:   monoflake.ID(88).String(),
				WorkspaceID: testSessionWorkspaceID,
				Kind:        "claude-code",
				Status:      "running",
				StartedAt:   &started,
				CreatedAt:   started,
			}, nil
		},
	}

	res, _ := workspaceSessionApp(c).Test(
		httptest.NewRequest(http.MethodGet, "/workspaces/"+testSessionWorkspaceID+"/session", nil))

	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	var body struct {
		Session map[string]any `json:"session"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Session == nil {
		t.Fatal("expected a session")
	}
	// The id is the whole point: it is what the terminal page is addressed by.
	if body.Session["id"] != monoflake.ID(77).String() {
		t.Errorf("id = %v", body.Session["id"])
	}
	// The kind decides whether the interface offers a terminal at all.
	if body.Session["kind"] != "claude-code" || body.Session["status"] != "running" {
		t.Errorf("got %v", body.Session)
	}
	for _, key := range []string{"machineId", "workspaceId", "createdAt"} {
		if _, ok := body.Session[key]; !ok {
			t.Errorf("missing %q — the API is camelCase", key)
		}
	}

	// Assembled from the path and the signed-in user, which is what keeps one
	// person from reading another's sessions by changing a number.
	if c.saw.WorkspaceID != testSessionWorkspaceID || c.saw.UserID != "user-1" {
		t.Errorf("asked for %+v", c.saw)
	}
}

// Nothing running is the ordinary answer, and it has to be `null` rather than
// an error or an empty object: most workspaces have no agent most of the time,
// and a session with no id reads as a session until something tries to open
// it.
func TestWorkspaceSessionSaysNullWhenNothingIsRunning(t *testing.T) {
	c := &sessionCrud{
		active: func(context.Context, entity.ActiveSessionRequest) (*entity.SessionView, error) {
			return nil, nil
		},
	}

	res, _ := workspaceSessionApp(c).Test(
		httptest.NewRequest(http.MethodGet, "/workspaces/"+testSessionWorkspaceID+"/session", nil))

	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 — no agent is not an error", res.StatusCode)
	}
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(res.Body).Decode(&raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	body, ok := raw["session"]
	if !ok {
		t.Fatal("the key must be present, so a caller can tell the answer from a broken response")
	}
	if string(body) != "null" {
		t.Errorf("session = %s, want null", body)
	}
}

func TestWorkspaceSessionReportsAFailedLookup(t *testing.T) {
	c := &sessionCrud{
		active: func(context.Context, entity.ActiveSessionRequest) (*entity.SessionView, error) {
			return nil, fmt.Errorf("invalid id")
		},
	}

	res, _ := workspaceSessionApp(c).Test(
		httptest.NewRequest(http.MethodGet, "/workspaces/nonsense/session", nil))

	if res.StatusCode == http.StatusOK {
		t.Fatalf("status = %d, want a failure", res.StatusCode)
	}
}
