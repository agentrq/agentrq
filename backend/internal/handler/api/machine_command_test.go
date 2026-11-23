// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/mustafaturan/monoflake"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	machinectrl "github.com/agentrq/agentrq/backend/internal/controller/machine"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/daemon/wire"
)

// A daemon socket that keeps what it is sent.
type recordingDaemonConn struct {
	mu     sync.Mutex
	frames []wire.Frame
}

func (c *recordingDaemonConn) Send(f wire.Frame) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.frames = append(c.frames, f)
	return nil
}
func (c *recordingDaemonConn) Close() error { return nil }

func (c *recordingDaemonConn) ops(t *testing.T) []wire.Control {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	controls := make([]wire.Control, 0, len(c.frames))
	for _, f := range c.frames {
		ctl, err := wire.ParseControl(f)
		if err != nil {
			t.Fatal(err)
		}
		controls = append(controls, ctl)
	}
	return controls
}

type commandCrud struct {
	crud.Controller
	running  string
	offered  string
	err      error
	recorded []entity.RecordMachineCommandRequest
}

func (m *commandCrud) RestartDaemon(_ context.Context, req entity.RestartDaemonRequest) (*entity.RestartDaemonResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &entity.RestartDaemonResponse{MachineID: monoflake.IDFromBase62(req.MachineID).Int64(), RunningVersion: m.running}, nil
}

func (m *commandCrud) ApproveMachineUpdate(_ context.Context, req entity.ApproveMachineUpdateRequest) (*entity.ApproveMachineUpdateResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	if req.Version != m.offered {
		return nil, crud.ErrNoUpdateOffered
	}
	return &entity.ApproveMachineUpdateResponse{
		MachineID: monoflake.IDFromBase62(req.MachineID).Int64(), Version: m.offered, RunningVersion: m.running,
	}, nil
}

func (m *commandCrud) RecordMachineCommand(_ context.Context, req entity.RecordMachineCommandRequest) {
	m.recorded = append(m.recorded, req)
}

func commandApp(ctrl crud.Controller, reg *machinectrl.Registry) *fiber.App {
	h := &handler{crud: ctrl, machineRegistry: reg}
	app := fiber.New()
	app.Use(func(ctx fiber.Ctx) error {
		ctx.Locals("user_id", "user-1")
		return ctx.Next()
	})
	app.Post(_routePathMachineRestart, h.restartDaemon())
	app.Post(_routePathMachineUpdate, h.approveMachineUpdate())
	return app
}

// connected is a registry holding machine 11, whose hello said caps.
func connected(caps ...string) (*machinectrl.Registry, *recordingDaemonConn) {
	reg := machinectrl.NewRegistry("pod-a")
	conn := &recordingDaemonConn{}
	reg.Add(11, conn)
	reg.SetCapabilities(11, conn, caps)
	return reg, conn
}

var machine11 = monoflake.ID(11).String()

func post(t *testing.T, app *fiber.App, path, body string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	respBody, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(respBody)
}

func TestRestartDaemonSendsTheRestartAndCountsIt(t *testing.T) {
	reg, conn := connected(wire.CapabilityFork, wire.CapabilityRestart)
	ctrl := &commandCrud{running: "0.9.3"}

	status, body := post(t, commandApp(ctrl, reg), "/machines/"+machine11+"/restart", "")

	if status != http.StatusAccepted {
		t.Fatalf("the request answered status %d (%s), want 202", status, body)
	}
	ops := conn.ops(t)
	if len(ops) != 1 || ops[0].Op != wire.OpRestart {
		t.Fatalf("the daemon was sent %+v, want one restart", ops)
	}
	if len(ctrl.recorded) != 1 || ctrl.recorded[0].Action != entity.ActionMachineRestart ||
		ctrl.recorded[0].MachineID != 11 || ctrl.recorded[0].UserID != "user-1" {
		t.Errorf("counted %+v, want one restart of machine 11 by user-1", ctrl.recorded)
	}
}

func TestApproveMachineUpdateSendsTheApprovedVersionAndCountsIt(t *testing.T) {
	reg, conn := connected(wire.CapabilityRestart, wire.CapabilityUpdate)
	ctrl := &commandCrud{running: "0.9.3", offered: "0.9.4"}

	status, body := post(t, commandApp(ctrl, reg), "/machines/"+machine11+"/update", `{"version":"0.9.4"}`)

	if status != http.StatusAccepted {
		t.Fatalf("the request answered status %d (%s), want 202", status, body)
	}
	ops := conn.ops(t)
	if len(ops) != 1 || ops[0].Op != wire.OpUpdateNow {
		t.Fatalf("the daemon was sent %+v, want one update", ops)
	}
	var req wire.UpdateNow
	if err := json.Unmarshal(ops[0].Body, &req); err != nil || req.Version != "0.9.4" {
		t.Errorf("the update body is %s (decode error %v), want version 0.9.4", ops[0].Body, err)
	}
	if len(ctrl.recorded) != 1 || ctrl.recorded[0].Action != entity.ActionMachineUpdate {
		t.Errorf("counted %+v, want one update", ctrl.recorded)
	}
}

// The restart route is the page's one command: with the offered version it
// updates, and counts it as an update.
func TestRestartDaemonWithAVersionUpdates(t *testing.T) {
	reg, conn := connected(wire.CapabilityRestart, wire.CapabilityUpdate)
	ctrl := &commandCrud{running: "0.9.3", offered: "0.9.4"}
	app := commandApp(ctrl, reg)

	if status, body := post(t, app, "/machines/"+machine11+"/restart", `{"version":"0.9.4"}`); status != http.StatusAccepted {
		t.Fatalf("the request answered status %d (%s), want 202", status, body)
	}
	ops := conn.ops(t)
	if len(ops) != 1 || ops[0].Op != wire.OpUpdateNow {
		t.Fatalf("the daemon was sent %+v, want one update", ops)
	}
	var req wire.UpdateNow
	if err := json.Unmarshal(ops[0].Body, &req); err != nil || req.Version != "0.9.4" {
		t.Errorf("the update body is %s (decode error %v), want version 0.9.4", ops[0].Body, err)
	}
	if len(ctrl.recorded) != 1 || ctrl.recorded[0].Action != entity.ActionMachineUpdate {
		t.Errorf("counted %+v, want one update", ctrl.recorded)
	}

	// An empty object is a plain restart, like no body at all.
	if status, _ := post(t, app, "/machines/"+machine11+"/restart", `{}`); status != http.StatusAccepted {
		t.Errorf("an empty object answered status %d, want 202", status)
	}
	if ops := conn.ops(t); len(ops) != 2 || ops[1].Op != wire.OpRestart {
		t.Errorf("the daemon was sent %+v, want the update and then a restart", ops)
	}
}

func TestRestartDaemonRefusals(t *testing.T) {
	reg, conn := connected(wire.CapabilityRestart, wire.CapabilityUpdate)
	app := commandApp(&commandCrud{running: "0.9.3", offered: "0.9.4"}, reg)

	if status, _ := post(t, app, "/machines/"+machine11+"/restart", `{"version":"0.9.9"}`); status != http.StatusConflict {
		t.Errorf("a version never offered answered status %d, want 409", status)
	}
	if status, _ := post(t, app, "/machines/"+machine11+"/restart", `not json`); status != http.StatusUnprocessableEntity {
		t.Errorf("an unreadable body answered status %d, want 422", status)
	}
	if len(conn.ops(t)) != 0 {
		t.Errorf("%d commands were sent, want none", len(conn.ops(t)))
	}
}

// An older daemon is refused rather than sent something it would ignore, or
// act on without bringing its agents back listed.
func TestAnOlderDaemonIsNotSentACommand(t *testing.T) {
	cases := map[string]struct {
		running string
		caps    []string
	}{
		"too old":                   {"0.9.2", []string{wire.CapabilityRestart, wire.CapabilityUpdate}},
		"a development build":       {"dev", []string{wire.CapabilityRestart, wire.CapabilityUpdate}},
		"new enough, not saying so": {"0.9.3", []string{wire.CapabilityFork}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			for _, path := range []string{"/restart", "/update"} {
				reg, conn := connected(tc.caps...)
				ctrl := &commandCrud{running: tc.running, offered: "0.9.4"}

				status, body := post(t, commandApp(ctrl, reg), "/machines/"+machine11+path, `{"version":"0.9.4"}`)

				if status != http.StatusConflict || !strings.Contains(body, wire.MinRemoteControlVersion) {
					t.Errorf("%s answered status %d (%s), want 409 naming the version", path, status, body)
				}
				if len(conn.ops(t)) != 0 || len(ctrl.recorded) != 0 {
					t.Errorf("%s: %d commands sent and %d counted, want none of either", path, len(conn.ops(t)), len(ctrl.recorded))
				}
			}
		})
	}
}

// A daemon whose update is not built in says restart and not update.
func TestAnUpdateNeedsItsOwnCapability(t *testing.T) {
	reg, conn := connected(wire.CapabilityRestart)
	ctrl := &commandCrud{running: "0.9.3", offered: "0.9.4"}

	status, _ := post(t, commandApp(ctrl, reg), "/machines/"+machine11+"/update", `{"version":"0.9.4"}`)
	if status != http.StatusConflict || len(conn.ops(t)) != 0 {
		t.Errorf("the request answered status %d and %d commands sent, want 409 and none", status, len(conn.ops(t)))
	}
}

func TestACommandThatCannotBeDeliveredSaysWhy(t *testing.T) {
	t.Run("a machine this server does not hold is a conflict", func(t *testing.T) {
		ctrl := &commandCrud{running: "0.9.3"}
		status, _ := post(t, commandApp(ctrl, machinectrl.NewRegistry("pod-a")), "/machines/"+machine11+"/restart", "")
		if status != http.StatusConflict {
			t.Errorf("the request answered status %d, want 409", status)
		}
	})
	t.Run("a server that takes no machine connections is unavailable", func(t *testing.T) {
		ctrl := &commandCrud{running: "0.9.3"}
		status, _ := post(t, commandApp(ctrl, nil), "/machines/"+machine11+"/restart", "")
		if status != http.StatusServiceUnavailable {
			t.Errorf("the request answered status %d, want 503", status)
		}
	})
	t.Run("a socket that fails the send is a bad gateway, and not counted", func(t *testing.T) {
		reg := machinectrl.NewRegistry("pod-a")
		conn := &brokenDaemonConn{}
		reg.Add(11, conn)
		reg.SetCapabilities(11, conn, []string{wire.CapabilityRestart})
		ctrl := &commandCrud{running: "0.9.3"}
		status, _ := post(t, commandApp(ctrl, reg), "/machines/"+machine11+"/restart", "")
		if status != http.StatusBadGateway || len(ctrl.recorded) != 0 {
			t.Errorf("the request answered status %d and %d commands counted, want 502 and none", status, len(ctrl.recorded))
		}
	})
	t.Run("a machine the caller cannot read is sent nothing", func(t *testing.T) {
		reg, conn := connected(wire.CapabilityRestart)
		ctrl := &commandCrud{err: errors.New("record not found")}
		status, _ := post(t, commandApp(ctrl, reg), "/machines/"+machine11+"/restart", "")
		if status == http.StatusAccepted || len(conn.ops(t)) != 0 {
			t.Errorf("the request answered status %d and %d commands sent, want a refusal and none", status, len(conn.ops(t)))
		}
	})
}

func TestApproveMachineUpdateRefusals(t *testing.T) {
	reg, conn := connected(wire.CapabilityRestart, wire.CapabilityUpdate)
	app := commandApp(&commandCrud{running: "0.9.3", offered: "0.9.4"}, reg)

	if status, _ := post(t, app, "/machines/"+machine11+"/update", `{"version":"0.9.9"}`); status != http.StatusConflict {
		t.Errorf("a version never offered answered status %d, want 409", status)
	}
	if status, _ := post(t, app, "/machines/"+machine11+"/update", `not json`); status != http.StatusUnprocessableEntity {
		t.Errorf("an unreadable body answered status %d, want 422", status)
	}
	failing := commandApp(&commandCrud{err: errors.New("database unavailable")}, reg)
	if status, _ := post(t, failing, "/machines/"+machine11+"/update", `{"version":"0.9.4"}`); status == http.StatusAccepted {
		t.Errorf("a failed read answered status %d, want anything but 202", status)
	}
	if len(conn.ops(t)) != 0 {
		t.Errorf("%d commands were sent, want none", len(conn.ops(t)))
	}
}

// Neither can fail for the values the handlers pass; covered so a change that
// makes them fail answers rather than panics.
func TestCommandMachineRefusesWhatItCannotEncode(t *testing.T) {
	reg, conn := connected(wire.CapabilityRestart)
	h := &handler{machineRegistry: reg}
	app := fiber.New()
	app.Post("/unencodable", func(c fiber.Ctx) error {
		h.commandMachine(c, 11, "0.9.3", wire.CapabilityRestart, wire.OpRestart, make(chan int))
		return nil
	})
	app.Post("/no-op", func(c fiber.Ctx) error {
		h.commandMachine(c, 11, "0.9.3", wire.CapabilityRestart, "", struct{}{})
		return nil
	})
	for _, path := range []string{"/unencodable", "/no-op"} {
		if status, _ := post(t, app, path, ""); status != http.StatusInternalServerError {
			t.Errorf("%s answered status %d, want 500", path, status)
		}
	}
	if len(conn.ops(t)) != 0 {
		t.Errorf("%d commands were sent, want none", len(conn.ops(t)))
	}
}
