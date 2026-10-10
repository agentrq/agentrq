// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/mustafaturan/monoflake"

	"github.com/agentrq/agentrq/backend/internal/controller/agentlaunch"
	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	machinectrl "github.com/agentrq/agentrq/backend/internal/controller/machine"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/auth"
	"github.com/agentrq/agentrq/daemon/wire"
)

// errNotFoundForTest stands in for whatever error the real crud controller
// returns when a machine or workspace does not belong to the caller — these
// handlers fail open the same way regardless of which one it is, so the tests
// do not need the real sentinel.
var errNotFoundForTest = errors.New("not found")

// acpGatewayCrud answers only the two lookups these handlers make; anything
// else panics through the embedded nil interface, the same guard killCrud
// uses in session_kill_test.go.
type acpGatewayCrud struct {
	crud.Controller
	machineErr   error
	workspace    entity.Workspace
	workspaceErr error
}

func (c *acpGatewayCrud) GetMachine(context.Context, entity.GetMachineRequest) (*entity.GetMachineResponse, error) {
	if c.machineErr != nil {
		return nil, c.machineErr
	}
	return &entity.GetMachineResponse{}, nil
}

func (c *acpGatewayCrud) GetWorkspace(context.Context, entity.GetWorkspaceRequest) (*entity.GetWorkspaceResponse, error) {
	if c.workspaceErr != nil {
		return nil, c.workspaceErr
	}
	return &entity.GetWorkspaceResponse{Workspace: c.workspace}, nil
}

// answeringConn stands in for a daemon that replies to whatever it is sent
// immediately, so [machinectrl.Registry.Ask] resolves without a real socket
// or a test-slowing timeout. It parses the request itself, so a test can
// assert on what the handler actually asked for.
type answeringConn struct {
	reg     *machinectrl.Registry
	replyOp wire.Op
	body    any
	got     wire.Control
	asks    int
}

func (c *answeringConn) Send(f wire.Frame) error {
	ctl, err := wire.ParseControl(f)
	if err != nil {
		return err
	}
	c.got = ctl
	c.asks++
	c.reg.Deliver(wire.Control{ID: ctl.ID, Op: c.replyOp, Body: mustMarshal(c.body)})
	return nil
}

func (c *answeringConn) Close() error { return nil }

func mustMarshal(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func acpGatewayApp(c crud.Controller, reg *machinectrl.Registry) *fiber.App {
	return acpGatewayAppCached(c, reg, nil)
}

func acpGatewayAppCached(c crud.Controller, reg *machinectrl.Registry, cache *machinectrl.LookupCache) *fiber.App {
	h := &handler{crud: c, machineRegistry: reg, acpLookups: cache}
	app := fiber.New()
	app.Use(func(ctx *fiber.Ctx) error {
		ctx.Locals("user_id", "user-1")
		return ctx.Next()
	})
	app.Get(_routePathAcpAgents, h.listAcpAgents())
	app.Get(_routePathAcpModels, h.listAcpModels())
	return app
}

func decodeAcpAgents(t *testing.T, res *http.Response) wire.AcpAgentsList {
	t.Helper()
	var out wire.AcpAgentsList
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return out
}

func decodeAcpModels(t *testing.T, res *http.Response) wire.AcpModelsList {
	t.Helper()
	var out wire.AcpModelsList
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return out
}

func TestListAcpAgentsReturnsTheDaemonsAnswer(t *testing.T) {
	reg := machinectrl.NewRegistry("pod-a")
	conn := &answeringConn{reg: reg, replyOp: wire.OpAcpAgents, body: wire.AcpAgentsList{
		Agents: []wire.AcpAgent{{ID: "codex-acp", Name: "Codex", Runtimes: []string{"npx"}}},
	}}
	reg.Add(11, conn)

	res, _ := acpGatewayApp(&acpGatewayCrud{}, reg).Test(
		httptest.NewRequest(http.MethodGet, "/machines/"+monoflake.ID(11).String()+"/acp-agents", nil))

	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	got := decodeAcpAgents(t, res)
	if len(got.Agents) != 1 || got.Agents[0].ID != "codex-acp" {
		t.Errorf("agents = %+v", got.Agents)
	}
	if conn.got.Op != wire.OpListAcpAgents {
		t.Errorf("daemon was asked op %q, want %q", conn.got.Op, wire.OpListAcpAgents)
	}
}

// Every reason this could fail to get an answer — a machine that is not the
// caller's, one this instance does not hold — is the same 200 with an empty
// list: the launch form's free text is always the fallback, and there is
// nothing an error status would tell it that emptiness does not already say.
func TestListAcpAgentsFailsOpen(t *testing.T) {
	tests := map[string]struct {
		crud *acpGatewayCrud
		reg  *machinectrl.Registry
	}{
		"unowned or missing machine": {
			crud: &acpGatewayCrud{machineErr: errNotFoundForTest},
			reg:  machinectrl.NewRegistry("pod-a"),
		},
		"machine not connected here": {
			crud: &acpGatewayCrud{},
			reg:  machinectrl.NewRegistry("pod-a"), // nothing added for machine 11
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			res, _ := acpGatewayApp(tc.crud, tc.reg).Test(
				httptest.NewRequest(http.MethodGet, "/machines/"+monoflake.ID(11).String()+"/acp-agents", nil))
			if res.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want 200", res.StatusCode)
			}
			if got := decodeAcpAgents(t, res); len(got.Agents) != 0 {
				t.Errorf("agents = %+v, want none", got.Agents)
			}
		})
	}
}

func TestListAcpModelsAsksWithTheWorkspacesDirectory(t *testing.T) {
	reg := machinectrl.NewRegistry("pod-a")
	conn := &answeringConn{reg: reg, replyOp: wire.OpAcpModels, body: wire.AcpModelsList{
		Agent:  "codex-acp",
		Models: []wire.AcpModel{{ID: "gpt-5.5", Name: "5.5", Current: true}},
	}}
	reg.Add(11, conn)
	c := &acpGatewayCrud{workspace: entity.Workspace{WorkingDirectory: "/work/ws"}}

	url := "/workspaces/" + monoflake.ID(70).String() + "/acp-models?machineId=" + monoflake.ID(11).String() + "&agent=codex-acp"
	res, _ := acpGatewayApp(c, reg).Test(httptest.NewRequest(http.MethodGet, url, nil))

	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	got := decodeAcpModels(t, res)
	if got.Agent != "codex-acp" || len(got.Models) != 1 || got.Models[0].ID != "gpt-5.5" {
		t.Errorf("AcpModelsList = %+v", got)
	}

	var sent wire.ListAcpModels
	if err := json.Unmarshal(conn.got.Body, &sent); err != nil {
		t.Fatal(err)
	}
	if sent.Dir != "/work/ws" || sent.Agent != "codex-acp" {
		t.Errorf("daemon was asked %+v, want dir=/work/ws agent=codex-acp", sent)
	}
}

func TestListAcpModelsFailsOpen(t *testing.T) {
	base := "/workspaces/" + monoflake.ID(70).String() + "/acp-models"
	machineQ := "machineId=" + monoflake.ID(11).String()

	tests := map[string]struct {
		url      string
		crud     *acpGatewayCrud
		noDaemon bool
	}{
		"no agent named": {
			url:  base + "?" + machineQ,
			crud: &acpGatewayCrud{workspace: entity.Workspace{WorkingDirectory: "/work/ws"}},
		},
		"no machine named": {
			url:  base + "?agent=codex-acp",
			crud: &acpGatewayCrud{workspace: entity.Workspace{WorkingDirectory: "/work/ws"}},
		},
		"workspace has no working directory yet": {
			url:  base + "?" + machineQ + "&agent=codex-acp",
			crud: &acpGatewayCrud{workspace: entity.Workspace{}},
		},
		"workspace not owned by the caller": {
			url:  base + "?" + machineQ + "&agent=codex-acp",
			crud: &acpGatewayCrud{workspaceErr: errNotFoundForTest},
		},
		"machine not owned by the caller": {
			url:  base + "?" + machineQ + "&agent=codex-acp",
			crud: &acpGatewayCrud{machineErr: errNotFoundForTest, workspace: entity.Workspace{WorkingDirectory: "/work/ws"}},
		},
		"machine not connected here": {
			url:      base + "?" + machineQ + "&agent=codex-acp",
			crud:     &acpGatewayCrud{workspace: entity.Workspace{WorkingDirectory: "/work/ws"}},
			noDaemon: true,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			reg := machinectrl.NewRegistry("pod-a")
			if !tc.noDaemon {
				reg.Add(11, &stubDaemonConn{})
			}
			res, _ := acpGatewayApp(tc.crud, reg).Test(httptest.NewRequest(http.MethodGet, tc.url, nil))
			if res.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want 200", res.StatusCode)
			}
			if got := decodeAcpModels(t, res); len(got.Models) != 0 {
				t.Errorf("models = %+v, want none", got.Models)
			}
		})
	}
}

func TestListAcpAgentsAsksAMachineOnceWithinTheTTL(t *testing.T) {
	reg := machinectrl.NewRegistry("pod-a")
	conn := &answeringConn{reg: reg, replyOp: wire.OpAcpAgents, body: wire.AcpAgentsList{
		Agents: []wire.AcpAgent{{ID: "codex-acp", Name: "Codex"}},
	}}
	reg.Add(11, conn)
	app := acpGatewayAppCached(&acpGatewayCrud{}, reg, machinectrl.NewLookupCache(time.Minute))

	for i := range 2 {
		res, _ := app.Test(httptest.NewRequest(http.MethodGet, "/machines/"+monoflake.ID(11).String()+"/acp-agents", nil))
		if got := decodeAcpAgents(t, res); len(got.Agents) != 1 || got.Agents[0].ID != "codex-acp" {
			t.Errorf("request %d: agents = %+v", i, got.Agents)
		}
	}
	if conn.asks != 1 {
		t.Errorf("daemon asked %d times, want 1", conn.asks)
	}
}

// Models are kept per machine and agent, so another workspace asking about the
// same agent on the same machine is answered from the cache.
func TestListAcpModelsAsksAMachineOncePerAgentWithinTheTTL(t *testing.T) {
	reg := machinectrl.NewRegistry("pod-a")
	conn := &answeringConn{reg: reg, replyOp: wire.OpAcpModels, body: wire.AcpModelsList{
		Agent:  "codex-acp",
		Models: []wire.AcpModel{{ID: "gpt-5.5", Name: "5.5"}},
	}}
	reg.Add(11, conn)
	cache := machinectrl.NewLookupCache(time.Minute)
	ask := func(workspaceID int64, dir, agent string) wire.AcpModelsList {
		c := &acpGatewayCrud{workspace: entity.Workspace{WorkingDirectory: dir}}
		url := "/workspaces/" + monoflake.ID(workspaceID).String() + "/acp-models?machineId=" + monoflake.ID(11).String() + "&agent=" + agent
		res, _ := acpGatewayAppCached(c, reg, cache).Test(httptest.NewRequest(http.MethodGet, url, nil))
		return decodeAcpModels(t, res)
	}

	ask(70, "/work/a", "codex-acp")
	if got := ask(71, "/work/b", "codex-acp"); len(got.Models) != 1 || got.Models[0].ID != "gpt-5.5" {
		t.Errorf("cached answer = %+v", got)
	}
	if conn.asks != 1 {
		t.Fatalf("daemon asked %d times for one agent, want 1", conn.asks)
	}
	ask(70, "/work/a", "claude")
	if conn.asks != 2 {
		t.Errorf("daemon asked %d times for two agents, want 2", conn.asks)
	}
}

// Ownership is checked on every request, ahead of the cache: an answer cached
// for the machine's owner is never served to anyone else, for either lookup.
func TestCachedLookupsStillRequireOwningTheMachine(t *testing.T) {
	machineQ := "machineId=" + monoflake.ID(11).String()
	tests := map[string]struct {
		url     string
		replyOp wire.Op
		body    any
		empty   func(*http.Response) bool
	}{
		"agents": {
			url:     "/machines/" + monoflake.ID(11).String() + "/acp-agents",
			replyOp: wire.OpAcpAgents,
			body:    wire.AcpAgentsList{Agents: []wire.AcpAgent{{ID: "codex-acp"}}},
			empty:   func(res *http.Response) bool { return len(decodeAcpAgents(t, res).Agents) == 0 },
		},
		"models": {
			url:     "/workspaces/" + monoflake.ID(70).String() + "/acp-models?" + machineQ + "&agent=codex-acp",
			replyOp: wire.OpAcpModels,
			body:    wire.AcpModelsList{Agent: "codex-acp", Models: []wire.AcpModel{{ID: "gpt-5.5"}}},
			empty:   func(res *http.Response) bool { return len(decodeAcpModels(t, res).Models) == 0 },
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			reg := machinectrl.NewRegistry("pod-a")
			conn := &answeringConn{reg: reg, replyOp: tc.replyOp, body: tc.body}
			reg.Add(11, conn)
			cache := machinectrl.NewLookupCache(time.Minute)
			ws := entity.Workspace{WorkingDirectory: "/work/ws"}

			owner, _ := acpGatewayAppCached(&acpGatewayCrud{workspace: ws}, reg, cache).Test(httptest.NewRequest(http.MethodGet, tc.url, nil))
			if tc.empty(owner) {
				t.Fatal("the owner got nothing; the cache was never warmed")
			}

			other := &acpGatewayCrud{machineErr: errNotFoundForTest, workspace: ws}
			res, _ := acpGatewayAppCached(other, reg, cache).Test(httptest.NewRequest(http.MethodGet, tc.url, nil))
			if !tc.empty(res) {
				t.Error("a caller who does not own the machine was served the cached answer")
			}
			if conn.asks != 1 {
				t.Errorf("daemon asked %d times, want 1", conn.asks)
			}
		})
	}
}

// An empty list is how every failure on the machine arrives, so neither it nor
// an error reply is kept: the next request asks again.
func TestListAcpAgentsDoesNotKeepAnEmptyOrErrorAnswer(t *testing.T) {
	tests := map[string]*answeringConn{
		"empty list":  {replyOp: wire.OpAcpAgents, body: wire.AcpAgentsList{}},
		"error reply": {replyOp: wire.OpError, body: wire.AcpAgentsList{Agents: []wire.AcpAgent{{ID: "codex-acp"}}}},
	}
	for name, conn := range tests {
		t.Run(name, func(t *testing.T) {
			reg := machinectrl.NewRegistry("pod-a")
			conn.reg = reg
			reg.Add(11, conn)
			app := acpGatewayAppCached(&acpGatewayCrud{}, reg, machinectrl.NewLookupCache(time.Minute))
			for range 2 {
				_, _ = app.Test(httptest.NewRequest(http.MethodGet, "/machines/"+monoflake.ID(11).String()+"/acp-agents", nil))
			}
			if conn.asks != 2 {
				t.Errorf("daemon asked %d times, want 2", conn.asks)
			}
		})
	}
}

// fakeConn is a no-op daemon socket: launchAgent only needs Send to succeed
// (or fail, for the one test that wants that) to reach the code past it.
type fakeConn struct {
	sendErr error
}

func (f *fakeConn) Send(wire.Frame) error { return f.sendErr }
func (f *fakeConn) Close() error          { return nil }

// fakeLaunchCrud is the minimum crud.Controller launchAgent touches, on the
// success path up to and including the telemetry call.
type fakeLaunchCrud struct {
	crud.Controller

	workspace entity.Workspace
	machine   entity.MachineView
	// parent answers a read of the fork's parent; parentErr fails it.
	parent    entity.Workspace
	parentErr error

	recordTelemetryFunc func(ctx context.Context, rq entity.RecordTelemetryRequest) error
}

func (f *fakeLaunchCrud) GetWorkspace(ctx context.Context, req entity.GetWorkspaceRequest) (*entity.GetWorkspaceResponse, error) {
	if f.workspace.ForkOfID != 0 && req.ID == f.workspace.ForkOfID {
		if f.parentErr != nil {
			return nil, f.parentErr
		}
		return &entity.GetWorkspaceResponse{Workspace: f.parent}, nil
	}
	return &entity.GetWorkspaceResponse{Workspace: f.workspace}, nil
}

func (f *fakeLaunchCrud) ActiveSessionForWorkspace(ctx context.Context, req entity.ActiveSessionRequest) (*entity.SessionView, error) {
	return nil, nil
}

func (f *fakeLaunchCrud) GetMachine(ctx context.Context, req entity.GetMachineRequest) (*entity.GetMachineResponse, error) {
	return &entity.GetMachineResponse{Machine: f.machine}, nil
}

func (f *fakeLaunchCrud) CreateSession(ctx context.Context, req entity.CreateSessionRequest) (*entity.CreateSessionResponse, error) {
	return &entity.CreateSessionResponse{Session: entity.SessionView{
		ID:        monoflake.ID(999).String(),
		Kind:      req.Kind,
		MachineID: req.MachineID,
	}}, nil
}

func (f *fakeLaunchCrud) RecordTelemetry(ctx context.Context, rq entity.RecordTelemetryRequest) error {
	if f.recordTelemetryFunc == nil {
		return nil
	}
	return f.recordTelemetryFunc(ctx, rq)
}

// launchTestHandler wires a handler whose launchAgent can run end to end: a
// registry holding a fake connection for the one machine the test uses, a
// real token service (minting is not what these tests are about), and a crud
// fake that accepts the launch all the way through to sendStart.
func launchTestHandler(t *testing.T, machineID int64, crudCtrl *fakeLaunchCrud, conn *fakeConn) *handler {
	t.Helper()
	registry := machinectrl.NewRegistry("test-instance")
	registry.Add(machineID, conn)

	return withLauncher(&handler{
		crud:            crudCtrl,
		mcpManager:      &fakeMCPManager{},
		machineRegistry: registry,
		tokenSvc:        auth.NewTokenService(auth.TokenConfig{JWTSecret: "test-secret"}),
	})
}

// withLauncher gives h the launcher app.go would, built from its own fields.
func withLauncher(h *handler) *handler {
	h.launcher = &agentlaunch.Launcher{
		Crud:     h.crud,
		Agents:   h.mcpManager,
		Machines: h.machineRegistry,
		Tokens:   h.tokenSvc,
		URLs:     h.mcpURLs(),
	}
	return h
}

func launchApp(h *handler, userID string) *fiber.App {
	app := fiber.New()
	app.Post("/api/v1/workspaces/:id/agent", func(c *fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.launchAgent()(c)
	})
	return app
}

// Counted right after the start frame reaches the machine, once per kind, so
// the two are directly comparable counts.
func TestLaunchAgent_CountsByKind(t *testing.T) {
	cases := []struct {
		kind       string
		wantAction entity.Action
	}{
		{"claude-code", entity.ActionAgentLaunchClaudeCode},
		{"acp-gateway", entity.ActionAgentLaunchACPGateway},
	}

	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			machineID := monoflake.ID(5)
			crudCtrl := &fakeLaunchCrud{
				workspace: entity.Workspace{ID: 1, Name: "agentrq", WorkingDirectory: "/srv/app"},
				machine:   entity.MachineView{ID: machineID.String(), Enabled: true},
			}
			var counted []entity.Action
			crudCtrl.recordTelemetryFunc = func(ctx context.Context, rq entity.RecordTelemetryRequest) error {
				counted = append(counted, rq.Action)
				return nil
			}
			h := launchTestHandler(t, machineID.Int64(), crudCtrl, &fakeConn{})
			app := launchApp(h, monoflake.ID(100).String())

			req := httptest.NewRequest(
				http.MethodPost,
				"/api/v1/workspaces/"+monoflake.ID(1).String()+"/agent",
				strings.NewReader(`{"machineId":"`+machineID.String()+`","kind":"`+tc.kind+`"}`),
			)
			req.Header.Set("Content-Type", "application/json")

			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resp.StatusCode != http.StatusAccepted {
				t.Fatalf("expected 202, got %d", resp.StatusCode)
			}
			if len(counted) != 1 || counted[0] != tc.wantAction {
				t.Errorf("counted %v, want one %v", counted, tc.wantAction)
			}
		})
	}
}

// A kind neither server recognises is left uncounted rather than guessed at
// — the daemon is the actual authority on valid kinds.
func TestLaunchAgent_DoesNotCountAnUnrecognisedKind(t *testing.T) {
	machineID := monoflake.ID(5)
	crudCtrl := &fakeLaunchCrud{
		workspace: entity.Workspace{ID: 1, Name: "agentrq", WorkingDirectory: "/srv/app"},
		machine:   entity.MachineView{ID: machineID.String(), Enabled: true},
	}
	var counted []entity.Action
	crudCtrl.recordTelemetryFunc = func(ctx context.Context, rq entity.RecordTelemetryRequest) error {
		counted = append(counted, rq.Action)
		return nil
	}
	h := launchTestHandler(t, machineID.Int64(), crudCtrl, &fakeConn{})
	app := launchApp(h, monoflake.ID(100).String())

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/workspaces/"+monoflake.ID(1).String()+"/agent",
		strings.NewReader(`{"machineId":"`+machineID.String()+`","kind":"something-else"}`),
	)
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", resp.StatusCode)
	}
	if len(counted) != 0 {
		t.Errorf("counted %v, want nothing counted for an unrecognised kind", counted)
	}
}

// The launch happened; the count did not. Reporting that as a failed launch
// would be a lie the caller acts on — the frame has already reached the
// machine.
func TestLaunchAgent_StillSucceedsWhenTheCountFails(t *testing.T) {
	machineID := monoflake.ID(5)
	crudCtrl := &fakeLaunchCrud{
		workspace: entity.Workspace{ID: 1, Name: "agentrq", WorkingDirectory: "/srv/app"},
		machine:   entity.MachineView{ID: machineID.String(), Enabled: true},
		recordTelemetryFunc: func(ctx context.Context, rq entity.RecordTelemetryRequest) error {
			return errors.New("the counter is down")
		},
	}
	h := launchTestHandler(t, machineID.Int64(), crudCtrl, &fakeConn{})
	app := launchApp(h, monoflake.ID(100).String())

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/workspaces/"+monoflake.ID(1).String()+"/agent",
		strings.NewReader(`{"machineId":"`+machineID.String()+`","kind":"claude-code"}`),
	)
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("expected 202, got %d", resp.StatusCode)
	}
}

// capturingConn keeps the start request the handler sent, so a test can assert
// on what the daemon was actually asked to do.
type capturingConn struct {
	start wire.StartSession
}

func (c *capturingConn) Send(f wire.Frame) error {
	ctl, err := wire.ParseControl(f)
	if err != nil {
		return err
	}
	return json.Unmarshal(ctl.Body, &c.start)
}

func (c *capturingConn) Close() error { return nil }

func launchInto(t *testing.T, workspaceName string) wire.StartSession {
	t.Helper()
	machineID := monoflake.ID(5)
	crudCtrl := &fakeLaunchCrud{
		workspace: entity.Workspace{ID: 1, Name: workspaceName, WorkingDirectory: "/srv/app"},
		machine:   entity.MachineView{ID: machineID.String(), Enabled: true},
	}
	conn := &capturingConn{}
	registry := machinectrl.NewRegistry("test-instance")
	registry.Add(machineID.Int64(), conn)
	h := withLauncher(&handler{
		crud:            crudCtrl,
		mcpManager:      &fakeMCPManager{},
		machineRegistry: registry,
		tokenSvc:        auth.NewTokenService(auth.TokenConfig{JWTSecret: "test-secret"}),
		mcpBaseURL:      "https://agentrq.example",
	})

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/workspaces/"+monoflake.ID(1).String()+"/agent",
		strings.NewReader(`{"machineId":"`+machineID.String()+`","kind":"claude-code"}`),
	)
	req.Header.Set("Content-Type", "application/json")

	resp, err := launchApp(h, monoflake.ID(100).String()).Test(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", resp.StatusCode)
	}
	return conn.start
}

// The supervisor workspace works across every other one, so its agent is given
// the account-wide server as well as its own.
func TestLaunchAgent_TheSupervisorGetsTheCoreServer(t *testing.T) {
	start := launchInto(t, agentlaunch.SupervisorWorkspaceName)
	if start.CoreMCPURL != "https://agentrq.example/mcp" {
		t.Errorf("coreMcpUrl = %q, want the account-wide server", start.CoreMCPURL)
	}
	// And it carries no credential: that server authenticates over its own
	// OAuth flow, so a token on this URL would be one minted for nothing.
	if strings.Contains(start.CoreMCPURL, "token=") {
		t.Errorf("the core URL carries a token: %q", start.CoreMCPURL)
	}
}

// Every other workspace gets one entry. The daemon writes the second only when
// it is given a URL, so leaving this empty is the whole decision.
func TestLaunchAgent_AnOrdinaryWorkspaceGetsNoCoreServer(t *testing.T) {
	if start := launchInto(t, "agentrq-code"); start.CoreMCPURL != "" {
		t.Errorf("coreMcpUrl = %q, want it empty", start.CoreMCPURL)
	}
}

// Templated from the same host as the per-workspace URL and by the same rule,
// so the two never disagree about which deployment they mean.
func TestCoreMCPURLFollowsTheDeployment(t *testing.T) {
	masked := &handler{domain: "agentrq.com", cookieSecure: true, mcpBaseURL: "https://agentrq.com"}
	if got := masked.coreMCPURL(); got != "https://mcp.agentrq.com/mcp" {
		t.Errorf("masked = %q", got)
	}
	insecure := &handler{domain: "agentrq.test", mcpBaseURL: "http://agentrq.test"}
	if got := insecure.coreMCPURL(); got != "http://mcp.agentrq.test/mcp" {
		t.Errorf("http deployment = %q", got)
	}
	for _, domain := range []string{"", "localhost", "127.0.0.1"} {
		local := &handler{domain: domain, mcpBaseURL: "http://localhost:3000"}
		if got := local.coreMCPURL(); got != "http://localhost:3000/mcp" {
			t.Errorf("domain %q = %q, want the bare base URL", domain, got)
		}
	}
}

// launchFork launches into a fork of "api" on a machine whose daemon said
// the given capabilities in its hello, and the first version that runs forks.
func launchFork(t *testing.T, crudCtrl *fakeLaunchCrud, caps []string) (*http.Response, wire.StartSession) {
	t.Helper()
	return launchForkOn(t, crudCtrl, caps, wire.MinForkVersion)
}

// launchForkOn is launchFork on a daemon that said the given version.
func launchForkOn(t *testing.T, crudCtrl *fakeLaunchCrud, caps []string, version string) (*http.Response, wire.StartSession) {
	t.Helper()
	return launchWith(t, crudCtrl, caps, version, `{"kind":"claude-code"}`)
}

// launchWith launches with the given body, which gains the machine's id, on a
// daemon that said the given capabilities and version.
func launchWith(t *testing.T, crudCtrl *fakeLaunchCrud, caps []string, version, body string) (*http.Response, wire.StartSession) {
	t.Helper()
	machineID := monoflake.ID(5)
	crudCtrl.machine = entity.MachineView{ID: machineID.String(), Enabled: true, Version: version}
	conn := &capturingConn{}
	registry := machinectrl.NewRegistry("test-instance")
	registry.Add(machineID.Int64(), conn)
	registry.SetCapabilities(machineID.Int64(), conn, caps)
	h := withLauncher(&handler{
		crud:            crudCtrl,
		mcpManager:      &fakeMCPManager{},
		machineRegistry: registry,
		tokenSvc:        auth.NewTokenService(auth.TokenConfig{JWTSecret: "test-secret"}),
		mcpBaseURL:      "https://agentrq.example",
	})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/workspaces/"+monoflake.ID(crudCtrl.workspace.ID).String()+"/agent",
		strings.NewReader(`{"machineId":"`+machineID.String()+`",`+strings.TrimPrefix(body, "{")))
	req.Header.Set("Content-Type", "application/json")
	resp, err := launchApp(h, monoflake.ID(100).String()).Test(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp, conn.start
}

func forkLaunchCrud() *fakeLaunchCrud {
	return &fakeLaunchCrud{
		workspace: entity.Workspace{ID: 2, Name: "api fork", ForkOfID: 1},
		parent:    entity.Workspace{ID: 1, Name: "api", WorkingDirectory: "/srv/api"},
	}
}

func responseMessage(t *testing.T, resp *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// A fork is started from its parent's folder, in one the daemon makes for it.
func TestLaunchAgent_AForkRunsInAFolderMadeFromItsParents(t *testing.T) {
	resp, start := launchFork(t, forkLaunchCrud(), []string{wire.CapabilityFork})
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d: %s", resp.StatusCode, responseMessage(t, resp))
	}
	want := wire.ForkSpec{ID: monoflake.ID(2).String(), From: "/srv/api"}
	if start.Fork == nil || *start.Fork != want {
		t.Errorf("fork = %+v, want %+v", start.Fork, want)
	}
	if start.Dir != "/srv/api" {
		t.Errorf("dir = %q, want the parent's", start.Dir)
	}
	if !strings.Contains(start.MCPURL, "/"+monoflake.ID(2).String()) {
		t.Errorf("the fork's agent is pointed at %q, not at the fork", start.MCPURL)
	}
}

// A relaunch sends the same fork, whatever folder the fork has recorded: the
// daemon finds its folder by the fork's id.
func TestLaunchAgent_ARelaunchedForkNamesTheSameFolder(t *testing.T) {
	c := forkLaunchCrud()
	c.workspace.WorkingDirectory = "/home/u/.agentrq/forks/2"
	_, start := launchFork(t, c, []string{wire.CapabilityFork})
	if start.Fork == nil || start.Fork.From != "/srv/api" || start.Dir != "/srv/api" {
		t.Errorf("start = %+v", start)
	}
}

// Review Focus 1: an old agentrqd ignores the fork field and would run the
// fork's agent in the parent's folder, connected as the parent.
func TestLaunchAgent_AForkIsRefusedOnADaemonThatCannotRunOne(t *testing.T) {
	resp, start := launchFork(t, forkLaunchCrud(), nil)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", resp.StatusCode)
	}
	if msg := responseMessage(t, resp); !strings.Contains(msg, "update agentrqd on this machine to run a fork") {
		t.Errorf("message = %s", msg)
	}
	if start.SessionID != 0 {
		t.Error("the start was sent anyway")
	}
}

// The capability alone is not enough: a build older than the first release
// that runs forks, or one that is no release at all, is refused the same way.
func TestLaunchAgent_AForkIsRefusedOnADaemonBelowTheVersionFloor(t *testing.T) {
	for _, version := range []string{"0.9.2", "0.9.3-rc1", "dev", ""} {
		resp, start := launchForkOn(t, forkLaunchCrud(), []string{wire.CapabilityFork}, version)
		if resp.StatusCode != http.StatusConflict {
			t.Fatalf("%q: status = %d, want 409", version, resp.StatusCode)
		}
		if msg := responseMessage(t, resp); !strings.Contains(msg, "update agentrqd on this machine to run a fork (it needs 0.9.3 or newer)") {
			t.Errorf("%q: message = %s", version, msg)
		}
		if start.SessionID != 0 {
			t.Errorf("%q: the start was sent anyway", version)
		}
	}
}

// Compared as versions, not text: 0.9.10 is after 0.9.3.
func TestLaunchAgent_AForkRunsOnALaterDaemon(t *testing.T) {
	resp, start := launchForkOn(t, forkLaunchCrud(), []string{wire.CapabilityFork}, "0.9.10")
	if resp.StatusCode != http.StatusAccepted || start.Fork == nil {
		t.Errorf("status = %d fork = %+v: %s", resp.StatusCode, start.Fork, responseMessage(t, resp))
	}
}

// Only a fork needs the capability and the version.
func TestLaunchAgent_AnOrdinaryWorkspaceNeedsNoCapability(t *testing.T) {
	resp, start := launchForkOn(t, &fakeLaunchCrud{
		workspace: entity.Workspace{ID: 1, Name: "api", WorkingDirectory: "/srv/api"},
	}, nil, "dev")
	if resp.StatusCode != http.StatusAccepted || start.Fork != nil {
		t.Errorf("status = %d fork = %+v", resp.StatusCode, start.Fork)
	}
}

func TestLaunchAgent_AForkNeedsItsParentsFolder(t *testing.T) {
	c := forkLaunchCrud()
	c.parent.WorkingDirectory = ""
	resp, _ := launchFork(t, c, []string{wire.CapabilityFork})
	if resp.StatusCode != http.StatusPreconditionRequired {
		t.Fatalf("status = %d, want 428", resp.StatusCode)
	}
	if msg := responseMessage(t, resp); !strings.Contains(msg, "set api's working directory") {
		t.Errorf("message = %s, want the parent named", msg)
	}
}

func TestLaunchAgent_AForkWhoseParentCannotBeReadIsRefused(t *testing.T) {
	c := forkLaunchCrud()
	c.parentErr = base.ErrNotFound
	resp, start := launchFork(t, c, []string{wire.CapabilityFork})
	if resp.StatusCode != http.StatusNotFound || start.SessionID != 0 {
		t.Errorf("status = %d, start = %+v", resp.StatusCode, start)
	}
}

func claudeModelLaunchCrud() *fakeLaunchCrud {
	return &fakeLaunchCrud{workspace: entity.Workspace{ID: 1, Name: "api", WorkingDirectory: "/srv/api"}}
}

// The chosen model and effort reach the daemon on a machine that says it
// passes them on.
func TestLaunchAgent_ClaudeCodeCarriesTheChosenModelAndEffort(t *testing.T) {
	resp, start := launchWith(t, claudeModelLaunchCrud(), []string{wire.CapabilityClaudeOptions}, "0.9.15",
		`{"kind":"claude-code","model":"opus","effort":"high"}`)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d: %s", resp.StatusCode, responseMessage(t, resp))
	}
	if start.Model != "opus" || start.Effort != "high" {
		t.Errorf("the start frame names model %q and effort %q, want opus and high", start.Model, start.Effort)
	}
}

// An older agentrqd would start Claude's default model and say nothing, so the
// launch is refused before anything is sent.
func TestLaunchAgent_AClaudeCodeModelOrEffortIsRefusedOnADaemonThatWouldDropIt(t *testing.T) {
	for _, body := range []string{
		`{"kind":"claude-code","model":"opus"}`,
		`{"kind":"claude-code","effort":"max"}`,
	} {
		resp, start := launchWith(t, claudeModelLaunchCrud(), nil, "0.9.14", body)
		if resp.StatusCode != http.StatusConflict {
			t.Fatalf("%s: status = %d, want 409", body, resp.StatusCode)
		}
		if msg := responseMessage(t, resp); !strings.Contains(msg, "update agentrqd on this machine to choose Claude Code's model or effort") {
			t.Errorf("%s: message = %s, want it to ask for an agentrqd update", body, msg)
		}
		if start.SessionID != 0 {
			t.Errorf("%s: the start was sent anyway", body)
		}
	}
}

// Leaving the model blank, or choosing the gateway's, needs nothing new from
// the daemon: both worked before the capability existed.
func TestLaunchAgent_OnlyAClaudeCodeModelNeedsTheCapability(t *testing.T) {
	for _, body := range []string{
		`{"kind":"claude-code"}`,
		`{"kind":"acp-gateway","agent":"codex-acp","model":"gpt-5"}`,
	} {
		resp, _ := launchWith(t, claudeModelLaunchCrud(), nil, "0.9.14", body)
		if resp.StatusCode != http.StatusAccepted {
			t.Errorf("%s: status = %d, want 202: %s", body, resp.StatusCode, responseMessage(t, resp))
		}
	}
}
