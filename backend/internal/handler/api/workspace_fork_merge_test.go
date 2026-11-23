// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	"github.com/agentrq/agentrq/backend/internal/controller/forkmerge"
	machinectrl "github.com/agentrq/agentrq/backend/internal/controller/machine"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
	"github.com/agentrq/agentrq/daemon/wire"
	"github.com/gofiber/fiber/v3"
	"github.com/mustafaturan/monoflake"
)

const (
	mfMachineID = int64(11)
	mfSessionID = int64(500)
)

// mergeCrud is a fork whose agent may be running, and records the order the
// merge does things in.
type mergeCrud struct {
	crud.Controller
	mu        sync.Mutex
	checkErr  error
	session   *entity.SessionView
	status    string // what the session row says now
	sessErr   error
	machineOK bool
	steps     []string
	machines  []string
	merged    entity.MergeForkRequest
}

func (m *mergeCrud) step(s string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.steps = append(m.steps, s)
}

func (m *mergeCrud) CheckForkMerge(_ context.Context, req entity.MergeForkRequest) error {
	m.step("check")
	return m.checkErr
}

func (m *mergeCrud) ActiveSessionForWorkspace(_ context.Context, req entity.ActiveSessionRequest) (*entity.SessionView, error) {
	if req.WorkspaceID != monoflake.ID(wfForkID).String() || req.UserID != monoflake.ID(100).String() {
		return nil, errors.New("asked about the wrong workspace")
	}
	return m.session, nil
}

func (m *mergeCrud) ForkFolderMachines(context.Context, entity.ActiveSessionRequest) ([]string, error) {
	return m.machines, nil
}

func (m *mergeCrud) GetMachine(_ context.Context, req entity.GetMachineRequest) (*entity.GetMachineResponse, error) {
	if !m.machineOK {
		return nil, errors.New("not found")
	}
	return &entity.GetMachineResponse{Machine: entity.MachineView{ID: req.MachineID, Name: "laptop"}}, nil
}

func (m *mergeCrud) GetSession(_ context.Context, req entity.GetSessionRequest) (*entity.GetSessionResponse, error) {
	if m.sessErr != nil {
		return nil, m.sessErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return &entity.GetSessionResponse{Session: entity.SessionView{ID: req.SessionID, Status: m.status}}, nil
}

func (m *mergeCrud) setStatus(s string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.status = s
}

func (m *mergeCrud) MergeFork(_ context.Context, req entity.MergeForkRequest) (*entity.MergeForkResponse, error) {
	m.step("merge")
	m.merged = req
	return &entity.MergeForkResponse{ParentID: monoflake.ID(wfParentID).String(), MovedTasks: 1,
		Tasks: []entity.Task{{ID: 7, WorkspaceID: wfParentID}}}, nil
}

// A daemon that reports the kill it is sent, as the real one does through the
// session row.
type killingDaemon struct {
	crud   *mergeCrud
	report string // the state the daemon reports; empty reports nothing
	frames []wire.Control
}

func (d *killingDaemon) Send(f wire.Frame) error {
	c, err := wire.ParseControl(f)
	if err != nil {
		return err
	}
	d.frames = append(d.frames, c)
	d.crud.step("kill")
	if d.report != "" {
		go d.crud.setStatus(d.report)
	}
	return nil
}
func (d *killingDaemon) Close() error { return nil }

func mergeApp(c *mergeCrud, reg *machinectrl.Registry, mgr *fakeMCPManager) *fiber.App {
	app := fiber.New()
	forks := &forkmerge.Merger{Crud: c, Machines: reg, Servers: mgr, Bus: eventbus.New(), StopWait: time.Second, StopPoll: 5 * time.Millisecond}
	h := &handler{crud: c, forks: forks, router: app.Group("")}
	app.Use(func(ctx fiber.Ctx) error {
		ctx.Locals("user_id", monoflake.ID(100).String())
		return ctx.Next()
	})
	_ = h.registerWorkspaceRoutes()
	return app
}

func forkSession() *entity.SessionView {
	return &entity.SessionView{
		ID:          monoflake.ID(mfSessionID).String(),
		MachineID:   monoflake.ID(mfMachineID).String(),
		WorkspaceID: monoflake.ID(wfForkID).String(),
		Status:      machinectrl.SessionRunning,
	}
}

func mergeURL() string { return "/workspaces/" + monoflake.ID(wfForkID).String() + "/merge" }

// The fork's agent is killed on its machine, and the tasks move only once the
// daemon says it is dead.
func TestMergeFork_KillsTheAgentFirst(t *testing.T) {
	c := &mergeCrud{session: forkSession(), status: machinectrl.SessionRunning, machineOK: true}
	d := &killingDaemon{crud: c, report: machinectrl.SessionKilled}
	reg := machinectrl.NewRegistry("pod-a")
	reg.Add(mfMachineID, d)
	mgr := &fakeMCPManager{}

	status, body := send(t, mergeApp(c, reg, mgr), http.MethodPost, mergeURL(), "")
	if status != http.StatusOK {
		t.Fatalf("status = %d, body %s", status, body)
	}
	if body != `{"parentId":"`+monoflake.ID(wfParentID).String()+`","movedTasks":1}` {
		t.Errorf("body = %s", body)
	}
	if got := strings.Join(c.steps, ","); got != "check,kill,merge" {
		t.Errorf("steps = %s, want the check, then the kill, then the merge", got)
	}
	if len(d.frames) != 1 || d.frames[0].Op != wire.OpKillSession ||
		!strings.Contains(string(d.frames[0].Body), `500`) {
		t.Errorf("frames = %+v, want one kill of session 500", d.frames)
	}
	if len(mgr.removed) != 1 || mgr.removed[0] != wfForkID {
		t.Errorf("removed = %v", mgr.removed)
	}
}

// A merge that is going to be refused leaves the agent running, and the
// refusal reaches the page as a 409 with its reason. The stop itself is
// tested in forkmerge.
func TestMergeFork_UnfinishedDoesNotKill(t *testing.T) {
	c := &mergeCrud{
		checkErr: entity.NewForkError(entity.ErrForkUnfinished, "1 task in this fork is not finished"),
		session:  forkSession(), status: machinectrl.SessionRunning, machineOK: true,
	}
	d := &killingDaemon{crud: c}
	reg := machinectrl.NewRegistry("pod-a")
	reg.Add(mfMachineID, d)
	mgr := &fakeMCPManager{}

	status, body := send(t, mergeApp(c, reg, mgr), http.MethodPost, mergeURL(), "")
	if status != http.StatusConflict || !strings.Contains(body, "1 task in this fork is not finished") {
		t.Fatalf("status = %d, body %s", status, body)
	}
	if len(d.frames) != 0 || strings.Join(c.steps, ",") != "check" || len(mgr.removed) != 0 {
		t.Errorf("frames %v, steps %v, removed %v: a refused merge touched the agent", d.frames, c.steps, mgr.removed)
	}
}

// An agent on a machine that cannot be reached cannot be stopped, so the merge
// is refused rather than leave it working for a workspace that is gone.
func TestMergeFork_MachineOffline(t *testing.T) {
	for name, tc := range map[string]struct {
		reg       *machinectrl.Registry
		machineOK bool
		want      string
	}{
		"not connected": {machinectrl.NewRegistry("pod-a"), true, "the fork's agent is on laptop, which is offline"},
	} {
		c := &mergeCrud{session: forkSession(), status: machinectrl.SessionRunning, machineOK: tc.machineOK}
		mgr := &fakeMCPManager{}
		status, body := send(t, mergeApp(c, tc.reg, mgr), http.MethodPost, mergeURL(), "")
		if status != http.StatusConflict || !strings.Contains(body, tc.want) || !strings.Contains(body, "stop it from the machine page") {
			t.Errorf("%s: status = %d, body %s", name, status, body)
		}
		if strings.Join(c.steps, ",") != "check" || len(mgr.removed) != 0 {
			t.Errorf("%s: steps %v, removed %v: nothing may move", name, c.steps, mgr.removed)
		}
	}
}

// {"deleteFolder":true} reaches the merge, and the daemon is told to delete
// the folder.
func TestMergeFork_DeleteFolder(t *testing.T) {
	c := &mergeCrud{machineOK: true, machines: []string{monoflake.ID(mfMachineID).String()}}
	d := &killingDaemon{crud: c}
	reg := machinectrl.NewRegistry("pod-a")
	reg.Add(mfMachineID, d)
	reg.SetCapabilities(mfMachineID, d, []string{wire.CapabilityForkCleanup})

	status, body := send(t, mergeApp(c, reg, &fakeMCPManager{}), http.MethodPost, mergeURL(), `{"deleteFolder":true}`)
	if status != http.StatusOK {
		t.Fatalf("status = %d, body %s", status, body)
	}
	if !c.merged.DeleteFolder {
		t.Errorf("request = %+v, want DeleteFolder", c.merged)
	}
	if len(d.frames) != 1 || d.frames[0].Op != wire.OpRemoveForkDir {
		t.Errorf("frames = %+v, want one removeForkDir", d.frames)
	}
}

// An offline machine holding the folder refuses the merge with a 409.
func TestMergeFork_DeleteFolderRefusedOffline(t *testing.T) {
	c := &mergeCrud{machineOK: true, machines: []string{monoflake.ID(mfMachineID).String()}}
	status, body := send(t, mergeApp(c, machinectrl.NewRegistry("pod-a"), &fakeMCPManager{}), http.MethodPost, mergeURL(), `{"deleteFolder":true}`)
	if status != http.StatusConflict || !strings.Contains(body, "laptop") {
		t.Errorf("status = %d, body %s", status, body)
	}
	if got := strings.Join(c.steps, ","); got != "check" {
		t.Errorf("steps = %s", got)
	}
}

func TestMergeFork_MalformedBody(t *testing.T) {
	c := &mergeCrud{}
	status, _ := send(t, mergeApp(c, nil, &fakeMCPManager{}), http.MethodPost, mergeURL(), `{not json`)
	if status != http.StatusUnprocessableEntity || len(c.steps) != 0 {
		t.Errorf("status = %d, steps %v", status, c.steps)
	}
}
