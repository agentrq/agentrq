// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	machinectrl "github.com/agentrq/agentrq/backend/internal/controller/machine"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
)

// machineCrud answers the machine routes, and keeps what it was asked.
type machineCrud struct {
	crud.Controller
	err       error
	updateGot entity.UpdateMachineRequest
	deleteGot entity.DeleteMachineRequest
	enrolGot  entity.EnrolMachineRequest
}

func (m *machineCrud) ListMachines(_ context.Context, req entity.ListMachinesRequest) (*entity.ListMachinesResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &entity.ListMachinesResponse{Machines: []entity.MachineView{{ID: machine11, Name: "box of " + req.UserID}}}, nil
}

func (m *machineCrud) GetMachine(_ context.Context, req entity.GetMachineRequest) (*entity.GetMachineResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &entity.GetMachineResponse{Machine: entity.MachineView{ID: req.MachineID, Name: "box"}}, nil
}

func (m *machineCrud) UpdateMachine(_ context.Context, req entity.UpdateMachineRequest) (*entity.UpdateMachineResponse, error) {
	m.updateGot = req
	if m.err != nil {
		return nil, m.err
	}
	return &entity.UpdateMachineResponse{Machine: entity.MachineView{ID: req.MachineID}}, nil
}

func (m *machineCrud) DeleteMachine(_ context.Context, req entity.DeleteMachineRequest) error {
	m.deleteGot = req
	return m.err
}

func (m *machineCrud) CreateEnrolmentCode(_ context.Context, req entity.CreateEnrolmentCodeRequest) (*entity.CreateEnrolmentCodeResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &entity.CreateEnrolmentCodeResponse{Code: "ABCD-" + req.UserID}, nil
}

func (m *machineCrud) EnrolMachine(_ context.Context, req entity.EnrolMachineRequest) (*entity.EnrolMachineResponse, error) {
	m.enrolGot = req
	if m.err != nil {
		return nil, m.err
	}
	return &entity.EnrolMachineResponse{MachineID: machine11, MachineToken: "tok"}, nil
}

// machineApp serves the machine routes as the handler registers them, with
// the enrol route public and the rest signed in as user-1.
func machineApp(ctrl crud.Controller, reg *machinectrl.Registry) *fiber.App {
	app := fiber.New()
	h := &handler{crud: ctrl, machineRegistry: reg, router: app.Group("")}
	h.registerPublicMachineRoutes()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user_id", "user-1")
		return c.Next()
	})
	h.registerMachineRoutes()
	return app
}

func TestMachineRoutesAnswer(t *testing.T) {
	ctrl := &machineCrud{}
	app := machineApp(ctrl, nil)
	for _, tc := range []struct {
		method, path, body string
		status             int
		contains           string
	}{
		{http.MethodGet, "/machines", "", http.StatusOK, `"name":"box of user-1"`},
		{http.MethodGet, "/machines/" + machine11, "", http.StatusOK, `"id":"` + machine11 + `"`},
		{http.MethodPost, "/machines/codes", "", http.StatusCreated, `"code":"ABCD-user-1"`},
		{http.MethodPost, "/machines/enroll", `{"code":"c","name":"n","hostname":"h","os":"linux","arch":"arm64","version":"0.9.3"}`, http.StatusCreated, `"machineToken":"tok"`},
		{http.MethodPatch, "/machines/" + machine11, `{"name":"renamed"}`, http.StatusOK, `"id":"` + machine11 + `"`},
		{http.MethodDelete, "/machines/" + machine11, "", http.StatusNoContent, ""},
	} {
		status, body := send(t, app, tc.method, tc.path, tc.body)
		if status != tc.status || !strings.Contains(body, tc.contains) {
			t.Errorf("%s %s = %d %s, want %d containing %s", tc.method, tc.path, status, body, tc.status, tc.contains)
		}
	}
	if want := (entity.EnrolMachineRequest{Code: "c", Name: "n", Hostname: "h", OS: "linux", Arch: "arm64", Version: "0.9.3"}); ctrl.enrolGot != want {
		t.Errorf("enrolled with %+v, want %+v", ctrl.enrolGot, want)
	}
	if ctrl.updateGot.Name == nil || *ctrl.updateGot.Name != "renamed" || ctrl.updateGot.Enabled != nil {
		t.Errorf("update asked for %+v, want a rename that leaves enabled alone", ctrl.updateGot)
	}
	if ctrl.deleteGot.UserID != "user-1" || ctrl.deleteGot.MachineID != machine11 {
		t.Errorf("delete asked for %+v", ctrl.deleteGot)
	}
}

// Each route passes the controller's failure on with its own status.
func TestMachineRoutesPassOnFailures(t *testing.T) {
	app := machineApp(&machineCrud{err: base.ErrNotFound}, nil)
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodGet, "/machines", ""},
		{http.MethodGet, "/machines/" + machine11, ""},
		{http.MethodPost, "/machines/codes", ""},
		{http.MethodPost, "/machines/enroll", `{"code":"c"}`},
		{http.MethodPatch, "/machines/" + machine11, `{"enabled":false}`},
		{http.MethodDelete, "/machines/" + machine11, ""},
	} {
		if status, body := send(t, app, tc.method, tc.path, tc.body); status != http.StatusNotFound {
			t.Errorf("%s %s = %d %s, want 404", tc.method, tc.path, status, body)
		}
	}
}

// Every reason a code is refused gets one answer, so a stolen code cannot be
// probed for whether it was ever real.
func TestEnrolMachineRejectedCodeIsUnauthorised(t *testing.T) {
	app := machineApp(&machineCrud{err: crud.ErrEnrolmentRejected}, nil)
	status, body := send(t, app, http.MethodPost, "/machines/enroll", `{"code":"stolen"}`)
	if status != http.StatusUnauthorized || !strings.Contains(body, "enrolment code rejected") {
		t.Errorf("rejected code = %d %s, want 401 saying the code was rejected", status, body)
	}
	if strings.Contains(body, "expired") || strings.Contains(body, "used") {
		t.Errorf("the refusal says why: %s", body)
	}
}

func TestMachineRoutesRefuseMalformedBodies(t *testing.T) {
	ctrl := &machineCrud{}
	app := machineApp(ctrl, nil)
	for _, path := range []string{"/machines/enroll", "/machines/" + machine11} {
		method := http.MethodPost
		if path != "/machines/enroll" {
			method = http.MethodPatch
		}
		if status, body := send(t, app, method, path, `{`); status != http.StatusUnprocessableEntity {
			t.Errorf("%s %s with a broken body = %d %s, want 422", method, path, status, body)
		}
	}
	if ctrl.updateGot.MachineID != "" || ctrl.enrolGot.Code != "" {
		t.Error("a malformed body reached the controller")
	}
}

// Disabling or deleting a machine closes its socket here and now; a rename
// leaves it connected.
func TestDisablingOrDeletingAMachineDropsItsSocket(t *testing.T) {
	for _, tc := range []struct {
		name, method, body string
		dropped            bool
	}{
		{"rename", http.MethodPatch, `{"name":"x"}`, false},
		{"enable", http.MethodPatch, `{"enabled":true}`, false},
		{"disable", http.MethodPatch, `{"enabled":false}`, true},
		{"delete", http.MethodDelete, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reg, _ := connected()
			app := machineApp(&machineCrud{}, reg)
			if status, body := send(t, app, tc.method, "/machines/"+machine11, tc.body); status >= 300 {
				t.Fatalf("status %d %s", status, body)
			}
			_, err := reg.Get(11)
			if dropped := errors.Is(err, machinectrl.ErrNotConnected); dropped != tc.dropped {
				t.Errorf("socket dropped = %v, want %v", dropped, tc.dropped)
			}
		})
	}
}

// A machine id that does not decode drops nothing, rather than machine 0.
func TestDropMachineSocketIgnoresAnUndecodableID(t *testing.T) {
	reg, _ := connected()
	h := &handler{machineRegistry: reg}
	h.dropMachineSocket("")
	if _, err := reg.Get(11); err != nil {
		t.Errorf("machine 11 was dropped by an empty id: %v", err)
	}
}
