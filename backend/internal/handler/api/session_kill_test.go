// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/mustafaturan/monoflake"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	machinectrl "github.com/agentrq/agentrq/backend/internal/controller/machine"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/daemon/wire"
)

// A daemon socket that accepts whatever it is sent.
type stubDaemonConn struct{}

func (c *stubDaemonConn) Send(wire.Frame) error { return nil }
func (c *stubDaemonConn) Close() error          { return nil }

type killCrud struct {
	crud.Controller
	session entity.SessionView
	// kills records what the handler asked to be counted, so the test can
	// check it was counted once and with the right scope.
	kills []entity.RecordSessionKillRequest
}

func (k *killCrud) GetSession(_ context.Context, _ entity.GetSessionRequest) (*entity.GetSessionResponse, error) {
	return &entity.GetSessionResponse{Session: k.session}, nil
}

func (k *killCrud) RecordSessionKill(_ context.Context, req entity.RecordSessionKillRequest) {
	k.kills = append(k.kills, req)
}

func killApp(c crud.Controller, reg *machinectrl.Registry) *fiber.App {
	h := &handler{crud: c, machineRegistry: reg}
	app := fiber.New()
	app.Use(func(ctx fiber.Ctx) error {
		ctx.Locals("user_id", "user-1")
		return ctx.Next()
	})
	app.Delete(_routePathSession, h.killSession())
	return app
}

func runningSession() entity.SessionView {
	return entity.SessionView{
		ID:          monoflake.ID(500).String(),
		MachineID:   monoflake.ID(11).String(),
		WorkspaceID: monoflake.ID(70).String(),
		Kind:        "claude-code",
		Status:      machinectrl.SessionRunning,
	}
}

// Somebody stopping an agent is the count that the close cannot provide: the
// daemon reports the same terminal state whether the agent finished, crashed,
// or was cut short.
func TestKillSessionCountsTheStop(t *testing.T) {
	reg := machinectrl.NewRegistry("pod-a")
	reg.Add(11, &stubDaemonConn{})
	c := &killCrud{session: runningSession()}

	res, _ := killApp(c, reg).Test(
		httptest.NewRequest(http.MethodDelete, "/sessions/"+monoflake.ID(500).String(), nil))

	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", res.StatusCode)
	}
	if len(c.kills) != 1 {
		t.Fatalf("counted %d stops, want exactly 1", len(c.kills))
	}
	got := c.kills[0]
	if got.UserID != "user-1" {
		t.Errorf("userID = %q", got.UserID)
	}
	// The session knows its workspace, so the stop is attributable to it.
	if got.WorkspaceID != monoflake.ID(70).String() {
		t.Errorf("workspaceID = %q", got.WorkspaceID)
	}
	if got.SessionID != monoflake.ID(500).String() {
		t.Errorf("sessionID = %q", got.SessionID)
	}
}

// A kill that never reached a machine did not happen, and counting the attempt
// would make the number mean something else.
func TestKillSessionDoesNotCountARefusal(t *testing.T) {
	// Nothing registered for machine 11, so the request is refused with 409.
	reg := machinectrl.NewRegistry("pod-a")
	c := &killCrud{session: runningSession()}

	res, _ := killApp(c, reg).Test(
		httptest.NewRequest(http.MethodDelete, "/sessions/"+monoflake.ID(500).String(), nil))

	if res.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", res.StatusCode)
	}
	if len(c.kills) != 0 {
		t.Errorf("counted %+v, want nothing", c.kills)
	}
}

// A session that is already over is answered as success — the caller asked for
// it to be dead and it is — but nobody stopped anything.
func TestKillSessionDoesNotCountAnAlreadyFinishedSession(t *testing.T) {
	reg := machinectrl.NewRegistry("pod-a")
	reg.Add(11, &stubDaemonConn{})
	session := runningSession()
	session.Status = machinectrl.SessionExited
	c := &killCrud{session: session}

	res, _ := killApp(c, reg).Test(
		httptest.NewRequest(http.MethodDelete, "/sessions/"+monoflake.ID(500).String(), nil))

	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", res.StatusCode)
	}
	if len(c.kills) != 0 {
		t.Errorf("counted %+v, want nothing", c.kills)
	}
}

// A daemon socket that has gone away under us.
type brokenDaemonConn struct{}

func (c *brokenDaemonConn) Send(wire.Frame) error { return errors.New("broken pipe") }
func (c *brokenDaemonConn) Close() error          { return nil }

// The kill that cannot be delivered says which way it failed.
func TestKillSessionUndelivered(t *testing.T) {
	broken := machinectrl.NewRegistry("pod-a")
	broken.Add(11, &brokenDaemonConn{})
	for name, tc := range map[string]struct {
		reg  *machinectrl.Registry
		want int
	}{
		"no machine connections": {nil, http.StatusServiceUnavailable},
		"socket broken":          {broken, http.StatusBadGateway},
	} {
		c := &killCrud{session: runningSession()}
		res, _ := killApp(c, tc.reg).Test(
			httptest.NewRequest(http.MethodDelete, "/sessions/"+monoflake.ID(500).String(), nil))
		if res.StatusCode != tc.want || len(c.kills) != 0 {
			t.Errorf("%s: status = %d, kills %v", name, res.StatusCode, c.kills)
		}
	}
}
