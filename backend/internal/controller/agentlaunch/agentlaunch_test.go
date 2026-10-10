// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package agentlaunch

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	machinectrl "github.com/agentrq/agentrq/backend/internal/controller/machine"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/daemon/wire"
	"github.com/mustafaturan/monoflake"
)

var (
	errDatabaseDown = errors.New("database unavailable")
	workspace1      = monoflake.ID(1).String()
	machine5        = monoflake.ID(5).String()
	session9        = monoflake.ID(9).String()
)

// fakeCrud answers what a launch or a stop reads, and keeps what it writes.
type fakeCrud struct {
	workspace    entity.Workspace
	workspaceErr error
	parent       entity.Workspace
	parentErr    error
	active       *entity.SessionView
	activeErr    error
	machine      entity.MachineView
	machineErr   error
	createErr    error
	telemetryErr error

	created   []entity.CreateSessionRequest
	failed    []entity.UpdateSessionStateRequest
	updateErr error
	counted   []entity.RecordTelemetryRequest
	killed    []entity.RecordSessionKillRequest
}

func (f *fakeCrud) GetWorkspace(_ context.Context, req entity.GetWorkspaceRequest) (*entity.GetWorkspaceResponse, error) {
	if f.workspace.ForkOfID != 0 && req.ID == f.workspace.ForkOfID {
		return &entity.GetWorkspaceResponse{Workspace: f.parent}, f.parentErr
	}
	return &entity.GetWorkspaceResponse{Workspace: f.workspace}, f.workspaceErr
}

func (f *fakeCrud) ActiveSessionForWorkspace(context.Context, entity.ActiveSessionRequest) (*entity.SessionView, error) {
	return f.active, f.activeErr
}

func (f *fakeCrud) GetMachine(context.Context, entity.GetMachineRequest) (*entity.GetMachineResponse, error) {
	return &entity.GetMachineResponse{Machine: f.machine}, f.machineErr
}

func (f *fakeCrud) CreateSession(_ context.Context, req entity.CreateSessionRequest) (*entity.CreateSessionResponse, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	f.created = append(f.created, req)
	return &entity.CreateSessionResponse{Session: entity.SessionView{ID: session9, Kind: req.Kind, MachineID: req.MachineID}}, nil
}

func (f *fakeCrud) UpdateSessionState(_ context.Context, req entity.UpdateSessionStateRequest) error {
	f.failed = append(f.failed, req)
	return f.updateErr
}

func (f *fakeCrud) RecordTelemetry(_ context.Context, rq entity.RecordTelemetryRequest) error {
	f.counted = append(f.counted, rq)
	return f.telemetryErr
}

func (f *fakeCrud) RecordSessionKill(_ context.Context, req entity.RecordSessionKillRequest) {
	f.killed = append(f.killed, req)
}

type fakeAgents struct{ connected bool }

func (a fakeAgents) IsAgentConnected(int64) bool { return a.connected }

type fakeTokens struct{ err error }

func (t fakeTokens) CreateMCPToken(userID, workspaceID, tokenType string) (string, error) {
	return "tok-" + workspaceID, t.err
}

// conn is a daemon socket that keeps every control message it is sent.
type conn struct {
	sendErr error
	sent    []wire.Control
}

func (c *conn) Send(f wire.Frame) error {
	if c.sendErr != nil {
		return c.sendErr
	}
	ctl, err := wire.ParseControl(f)
	if err != nil {
		return err
	}
	c.sent = append(c.sent, ctl)
	return nil
}

func (c *conn) Close() error { return nil }

func (c *conn) start(t *testing.T) wire.StartSession {
	t.Helper()
	if len(c.sent) != 1 || c.sent[0].Op != wire.OpStartSession {
		t.Fatalf("sent %+v, want one start", c.sent)
	}
	var s wire.StartSession
	if err := json.Unmarshal(c.sent[0].Body, &s); err != nil {
		t.Fatal(err)
	}
	return s
}

// launcher is a launcher for workspace 1 in /srv/app, with machine 5
// connected, enabled, and saying caps in its hello.
func launcher(caps ...string) (*Launcher, *fakeCrud, *conn) {
	crud := &fakeCrud{
		workspace: entity.Workspace{ID: 1, Name: "api", WorkingDirectory: "/srv/app"},
		machine:   entity.MachineView{ID: machine5, Enabled: true, Version: wire.MinForkVersion},
	}
	c := &conn{}
	reg := machinectrl.NewRegistry("test-instance")
	reg.Add(5, c)
	reg.SetCapabilities(5, c, caps)
	return &Launcher{
		Crud:     crud,
		Agents:   fakeAgents{},
		Machines: reg,
		Tokens:   fakeTokens{},
		URLs:     URLs{Base: "https://agentrq.example"},
	}, crud, c
}

func request(kind string) Request {
	return Request{UserID: "user-1", WorkspaceID: workspace1, MachineID: machine5, Kind: kind}
}

// wantRefusal asserts err is a LaunchError of kind, saying message.
func wantRefusal(t *testing.T, err error, kind error, message string) {
	t.Helper()
	var le *entity.LaunchError
	if !errors.As(err, &le) {
		t.Fatalf("err = %v, want a LaunchError", err)
	}
	if !errors.Is(err, kind) || le.Message != message {
		t.Errorf("err = %v (%q), want %v saying %q", le.Kind, le.Message, kind, message)
	}
}

func TestLaunch_StartsTheAgentAndCountsIt(t *testing.T) {
	l, crud, c := launcher()
	rq := request("claude-code")
	rq.Cols, rq.Rows = 120, 40

	res, err := l.Launch(context.Background(), rq)
	if err != nil {
		t.Fatal(err)
	}
	if res.Session.ID != session9 {
		t.Errorf("session = %+v", res.Session)
	}
	start := c.start(t)
	if start.Dir != "/srv/app" || start.Workspace != "api" || start.ServerName != mcpServerName ||
		start.SessionID != 9 || start.Cols != 120 || start.Rows != 40 || start.Fork != nil {
		t.Errorf("start = %+v", start)
	}
	if want := "https://agentrq.example/mcp/" + workspace1 + "?token=tok-" + workspace1; start.MCPURL != want {
		t.Errorf("mcpUrl = %q, want %q", start.MCPURL, want)
	}
	if start.CoreMCPURL != "" {
		t.Errorf("an ordinary workspace got the core server: %q", start.CoreMCPURL)
	}
	if len(crud.created) != 1 || crud.created[0].WorkspaceID != workspace1 || crud.created[0].Cols != 120 {
		t.Errorf("created %+v", crud.created)
	}
	if len(crud.counted) != 1 || crud.counted[0].Action != entity.ActionAgentLaunchClaudeCode || crud.counted[0].WorkspaceID != 1 {
		t.Errorf("counted %+v", crud.counted)
	}
}

func TestLaunch_TheSupervisorGetsTheCoreServer(t *testing.T) {
	l, crud, c := launcher()
	crud.workspace.Name = SupervisorWorkspaceName
	if _, err := l.Launch(context.Background(), request("claude-code")); err != nil {
		t.Fatal(err)
	}
	if got := c.start(t).CoreMCPURL; got != "https://agentrq.example/mcp" {
		t.Errorf("coreMcpUrl = %q", got)
	}
}

// The launch reached the machine; reporting a failed count as a failed launch
// would be a lie the caller acts on.
func TestLaunch_SucceedsWhenTheCountFails(t *testing.T) {
	l, crud, _ := launcher()
	crud.telemetryErr = errDatabaseDown
	if _, err := l.Launch(context.Background(), request("acp-gateway")); err != nil {
		t.Fatal(err)
	}
	if len(crud.counted) != 1 || crud.counted[0].Action != entity.ActionAgentLaunchACPGateway {
		t.Errorf("counted %+v", crud.counted)
	}
}

func TestLaunch_DoesNotCountAnUnknownKind(t *testing.T) {
	l, crud, _ := launcher()
	if _, err := l.Launch(context.Background(), request("something-else")); err != nil {
		t.Fatal(err)
	}
	if len(crud.counted) != 0 {
		t.Errorf("counted %+v", crud.counted)
	}
}

func TestLaunch_AForkRunsInAFolderMadeFromItsParents(t *testing.T) {
	l, crud, c := launcher(wire.CapabilityFork)
	crud.workspace = entity.Workspace{ID: 2, Name: "api-fork", ForkOfID: 1}
	crud.parent = entity.Workspace{ID: 1, Name: "api", WorkingDirectory: "/srv/api"}
	rq := request("claude-code")
	rq.WorkspaceID = monoflake.ID(2).String()

	if _, err := l.Launch(context.Background(), rq); err != nil {
		t.Fatal(err)
	}
	start := c.start(t)
	if start.Dir != "/srv/api" || start.Fork == nil || start.Fork.ID != monoflake.ID(2).String() || start.Fork.From != "/srv/api" {
		t.Errorf("start = %+v fork = %+v", start, start.Fork)
	}
}

func TestLaunch_ClaudeCodeCarriesItsModelAndEffort(t *testing.T) {
	l, _, c := launcher(wire.CapabilityClaudeOptions)
	rq := request("claude-code")
	rq.Model, rq.Effort = "opus", "high"
	if _, err := l.Launch(context.Background(), rq); err != nil {
		t.Fatal(err)
	}
	if start := c.start(t); start.Model != "opus" || start.Effort != "high" {
		t.Errorf("start = %+v", start)
	}
}

// Every refusal comes before a session row or a frame, so it leaves nothing
// behind.
func TestLaunch_Refusals(t *testing.T) {
	cases := []struct {
		name    string
		caps    []string
		arrange func(*Launcher, *fakeCrud, *Request)
		kind    error
		message string
	}{
		{"no workspace", nil, func(_ *Launcher, _ *fakeCrud, rq *Request) { rq.WorkspaceID = "" },
			entity.ErrLaunchInvalid, "name a workspace and a machine to launch an agent"},
		{"no machine", nil, func(_ *Launcher, _ *fakeCrud, rq *Request) { rq.MachineID = "" },
			entity.ErrLaunchInvalid, "name a workspace and a machine to launch an agent"},
		{"an agent is connected", nil, func(l *Launcher, _ *fakeCrud, _ *Request) { l.Agents = fakeAgents{connected: true} },
			entity.ErrLaunchBusy, "this workspace already has an agent connected"},
		{"an agent is starting", nil, func(_ *Launcher, f *fakeCrud, _ *Request) { f.active = &entity.SessionView{Status: "starting"} },
			entity.ErrLaunchBusy, "this workspace already has an agent starting on a machine"},
		{"no folder", nil, func(_ *Launcher, f *fakeCrud, _ *Request) { f.workspace.WorkingDirectory = "" },
			entity.ErrLaunchNoFolder, "set this workspace's working directory before launching an agent"},
		{"a fork whose parent has no folder", nil, func(_ *Launcher, f *fakeCrud, _ *Request) {
			f.workspace = entity.Workspace{ID: 2, ForkOfID: 3}
			f.parent = entity.Workspace{ID: 3, Name: "api"}
		}, entity.ErrLaunchNoFolder, "set api's working directory before launching an agent in its fork"},
		{"the machine is disabled", nil, func(_ *Launcher, f *fakeCrud, _ *Request) { f.machine.Enabled = false },
			entity.ErrLaunchBusy, "that machine is disabled"},
		{"no machine connections", nil, func(l *Launcher, _ *fakeCrud, _ *Request) { l.Machines = nil },
			entity.ErrLaunchUnavailable, "machine connections are not available on this server"},
		{"the machine is offline", nil, func(_ *Launcher, _ *fakeCrud, rq *Request) { rq.MachineID = monoflake.ID(6).String() },
			entity.ErrLaunchBusy, "that machine is not connected"},
		{"a fork on a daemon without forks", nil, func(_ *Launcher, f *fakeCrud, _ *Request) {
			f.workspace = entity.Workspace{ID: 2, ForkOfID: 3}
			f.parent = entity.Workspace{ID: 3, WorkingDirectory: "/srv/api"}
		}, entity.ErrLaunchBusy, "update agentrqd on this machine to run a fork (it needs " + wire.MinForkVersion + " or newer)"},
		{"a fork on a daemon below the floor", []string{wire.CapabilityFork}, func(_ *Launcher, f *fakeCrud, _ *Request) {
			f.workspace = entity.Workspace{ID: 2, ForkOfID: 3}
			f.parent = entity.Workspace{ID: 3, WorkingDirectory: "/srv/api"}
			f.machine.Version = "0.9.2"
		}, entity.ErrLaunchBusy, "update agentrqd on this machine to run a fork (it needs " + wire.MinForkVersion + " or newer)"},
		{"a Claude model on a daemon that would drop it", nil, func(_ *Launcher, _ *fakeCrud, rq *Request) { rq.Effort = "max" },
			entity.ErrLaunchBusy, "update agentrqd on this machine to choose Claude Code's model or effort, or leave both blank"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l, crud, c := launcher(tc.caps...)
			rq := request("claude-code")
			tc.arrange(l, crud, &rq)

			_, err := l.Launch(context.Background(), rq)
			wantRefusal(t, err, tc.kind, tc.message)
			if len(crud.created) != 0 || len(c.sent) != 0 {
				t.Errorf("a refused launch left created=%v sent=%v", crud.created, c.sent)
			}
		})
	}
}

// What the crud layer or the token service refuses with is passed on as it
// is, for the caller to map.
func TestLaunch_PassesOnWhatItCannotRead(t *testing.T) {
	cases := []struct {
		name    string
		arrange func(*Launcher, *fakeCrud)
	}{
		{"workspace", func(_ *Launcher, f *fakeCrud) { f.workspaceErr = errDatabaseDown }},
		{"active session", func(_ *Launcher, f *fakeCrud) { f.activeErr = errDatabaseDown }},
		{"fork parent", func(_ *Launcher, f *fakeCrud) {
			f.workspace = entity.Workspace{ID: 2, ForkOfID: 3}
			f.parentErr = errDatabaseDown
		}},
		{"machine", func(_ *Launcher, f *fakeCrud) { f.machineErr = errDatabaseDown }},
		{"token", func(l *Launcher, _ *fakeCrud) { l.Tokens = fakeTokens{err: errDatabaseDown} }},
		{"session", func(_ *Launcher, f *fakeCrud) { f.createErr = errDatabaseDown }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l, crud, c := launcher()
			tc.arrange(l, crud)
			if _, err := l.Launch(context.Background(), request("claude-code")); !errors.Is(err, errDatabaseDown) {
				t.Errorf("err = %v, want %v", err, errDatabaseDown)
			}
			if len(c.sent) != 0 {
				t.Errorf("sent %v", c.sent)
			}
		})
	}
}

// The row exists and the daemon never heard about it: marking it failed is
// what stops it blocking the workspace's next launch.
func TestLaunch_AStartThatCannotBeSentMarksTheSessionFailed(t *testing.T) {
	for _, updateErr := range []error{nil, errDatabaseDown} {
		l, crud, c := launcher()
		c.sendErr = errors.New("socket closed")
		crud.updateErr = updateErr

		_, err := l.Launch(context.Background(), request("claude-code"))
		wantRefusal(t, err, entity.ErrLaunchUnreachable, "could not reach that machine")
		if len(crud.failed) != 1 || crud.failed[0].SessionID != session9 ||
			crud.failed[0].Status != machinectrl.SessionFailed || crud.failed[0].Error != "socket closed" || crud.failed[0].EndedAt == nil {
			t.Errorf("failed = %+v", crud.failed)
		}
		if len(crud.counted) != 0 {
			t.Errorf("an undelivered launch was counted: %+v", crud.counted)
		}
	}
}

func TestStop_KillsTheRunningSessionAndCountsIt(t *testing.T) {
	l, crud, c := launcher()
	crud.active = &entity.SessionView{ID: session9, MachineID: machine5, WorkspaceID: workspace1, Status: "running"}

	session, err := l.Stop(context.Background(), StopRequest{UserID: "user-1", WorkspaceID: workspace1})
	if err != nil {
		t.Fatal(err)
	}
	if session != crud.active {
		t.Errorf("session = %+v", session)
	}
	if len(c.sent) != 1 || c.sent[0].Op != wire.OpKillSession {
		t.Fatalf("sent %+v", c.sent)
	}
	var kill wire.KillSession
	if err := json.Unmarshal(c.sent[0].Body, &kill); err != nil || kill.SessionID != 9 {
		t.Errorf("kill = %+v (%v)", kill, err)
	}
	if len(crud.killed) != 1 || crud.killed[0] != (entity.RecordSessionKillRequest{UserID: "user-1", WorkspaceID: workspace1, SessionID: session9}) {
		t.Errorf("killed %+v", crud.killed)
	}
}

func TestStop_NothingRunningIsNoError(t *testing.T) {
	l, crud, c := launcher()
	session, err := l.Stop(context.Background(), StopRequest{UserID: "user-1", WorkspaceID: workspace1})
	if session != nil || err != nil || len(c.sent) != 0 || len(crud.killed) != 0 {
		t.Errorf("session = %v err = %v sent = %v killed = %v", session, err, c.sent, crud.killed)
	}
}

func TestStop_Refusals(t *testing.T) {
	running := &entity.SessionView{ID: session9, MachineID: machine5, Status: "running"}
	cases := []struct {
		name    string
		arrange func(*Launcher, *fakeCrud, *conn, *StopRequest)
		kind    error
		message string
	}{
		{"no workspace", func(_ *Launcher, _ *fakeCrud, _ *conn, rq *StopRequest) { rq.WorkspaceID = "" },
			entity.ErrLaunchInvalid, "name a workspace to stop its agent"},
		{"no machine connections", func(l *Launcher, _ *fakeCrud, _ *conn, _ *StopRequest) { l.Machines = nil },
			entity.ErrLaunchUnavailable, "machine connections are not available on this server"},
		{"the machine is offline", func(_ *Launcher, f *fakeCrud, _ *conn, _ *StopRequest) {
			f.active = &entity.SessionView{ID: session9, MachineID: monoflake.ID(6).String()}
		}, entity.ErrLaunchBusy, "that machine is not connected"},
		{"the machine cannot be reached", func(_ *Launcher, _ *fakeCrud, c *conn, _ *StopRequest) { c.sendErr = errors.New("socket closed") },
			entity.ErrLaunchUnreachable, "could not reach that machine"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l, crud, c := launcher()
			crud.active = running
			rq := StopRequest{UserID: "user-1", WorkspaceID: workspace1}
			tc.arrange(l, crud, c, &rq)

			_, err := l.Stop(context.Background(), rq)
			wantRefusal(t, err, tc.kind, tc.message)
			if len(crud.killed) != 0 {
				t.Errorf("a refused stop was counted: %+v", crud.killed)
			}
		})
	}
}

func TestStop_PassesOnWhatItCannotRead(t *testing.T) {
	l, crud, _ := launcher()
	crud.activeErr = errDatabaseDown
	if _, err := l.Stop(context.Background(), StopRequest{UserID: "user-1", WorkspaceID: workspace1}); !errors.Is(err, errDatabaseDown) {
		t.Errorf("err = %v", err)
	}
}

// Every kind the supervisor offers is one this server counts.
func TestKindsAreAllCounted(t *testing.T) {
	for _, kind := range Kinds {
		if _, ok := launchAction(kind); !ok {
			t.Errorf("%q is offered but not counted", kind)
		}
	}
	if _, ok := launchAction(""); ok || slices.Contains(Kinds, "") {
		t.Error("an empty kind is counted")
	}
}

func TestURLsFollowTheDeployment(t *testing.T) {
	id := int64(1234567)
	cases := []struct {
		urls      URLs
		workspace string
		core      string
	}{
		{URLs{Base: "https://agentrq.com", Domain: "agentrq.com", Secure: true}, "https://qglj.mcp.agentrq.com", "https://mcp.agentrq.com/mcp"},
		{URLs{Base: "http://agentrq.test", Domain: "agentrq.test"}, "http://qglj.mcp.agentrq.test", "http://mcp.agentrq.test/mcp"},
		{URLs{Base: "http://localhost:3000"}, "http://localhost:3000/mcp/" + monoflake.ID(id).String(), "http://localhost:3000/mcp"},
		{URLs{Base: "http://localhost:3000", Domain: "localhost"}, "http://localhost:3000/mcp/" + monoflake.ID(id).String(), "http://localhost:3000/mcp"},
		{URLs{Base: "http://127.0.0.1:3000", Domain: "127.0.0.1"}, "http://127.0.0.1:3000/mcp/" + monoflake.ID(id).String(), "http://127.0.0.1:3000/mcp"},
	}
	for _, tc := range cases {
		if got := tc.urls.Workspace(id); got != tc.workspace {
			t.Errorf("%+v: workspace = %q, want %q", tc.urls, got, tc.workspace)
		}
		if got := tc.urls.Core(); got != tc.core {
			t.Errorf("%+v: core = %q, want %q", tc.urls, got, tc.core)
		}
		if strings.Contains(tc.urls.Core(), "token=") {
			t.Error("the core URL carries a credential")
		}
	}
}

// A second launch while the first is between its checks and its session row
// is refused, and the workspace is free again once the first returns.
func TestLaunch_OneAtATimePerWorkspace(t *testing.T) {
	l, crud, c := launcher()
	if !l.claim(1) {
		t.Fatal("a free workspace could not be claimed")
	}
	_, err := l.Launch(context.Background(), request("claude-code"))
	wantRefusal(t, err, entity.ErrLaunchBusy, "this workspace already has an agent starting")
	if len(crud.created) != 0 || len(c.sent) != 0 {
		t.Errorf("a refused launch left created=%v sent=%v", crud.created, c.sent)
	}

	l.release(1)
	for range 2 {
		if _, err := l.Launch(context.Background(), request("claude-code")); err != nil {
			t.Fatalf("after release: %v", err)
		}
	}
	if !l.claim(1) {
		t.Error("a finished launch kept the workspace claimed")
	}
}
