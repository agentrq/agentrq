// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package coremcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/agentrq/agentrq/backend/internal/controller/agentlaunch"
	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
)

type mockMachineCrud struct {
	crud.Controller

	createEnrolmentCode func(ctx context.Context, req entity.CreateEnrolmentCodeRequest) (*entity.CreateEnrolmentCodeResponse, error)
	listMachines        func(ctx context.Context, req entity.ListMachinesRequest) (*entity.ListMachinesResponse, error)
}

func (m *mockMachineCrud) ListMachines(ctx context.Context, req entity.ListMachinesRequest) (*entity.ListMachinesResponse, error) {
	return m.listMachines(ctx, req)
}

func (m *mockMachineCrud) CreateEnrolmentCode(ctx context.Context, req entity.CreateEnrolmentCodeRequest) (*entity.CreateEnrolmentCodeResponse, error) {
	return m.createEnrolmentCode(ctx, req)
}

func machineServer(baseURL string, ctrl *mockMachineCrud) *WorkspaceServer {
	return &WorkspaceServer{crud: ctrl, baseURL: baseURL}
}

func TestCreateEnrolmentCode_ScopesToTheAuthenticatedUser(t *testing.T) {
	var got entity.CreateEnrolmentCodeRequest
	expiresAt := time.Unix(1_700_000_000, 0).UTC()
	ctrl := &mockMachineCrud{createEnrolmentCode: func(_ context.Context, req entity.CreateEnrolmentCodeRequest) (*entity.CreateEnrolmentCodeResponse, error) {
		got = req
		return &entity.CreateEnrolmentCodeResponse{Code: "ABC123", ExpiresAt: expiresAt}, nil
	}}

	body := textOf(t, toolResult(machineServer("https://agentrq.example", ctrl).handleCreateEnrolmentCode(authedContext(), nil, struct{}{})))

	if got.UserID != testUserID {
		t.Errorf("UserID = %q, want %q", got.UserID, testUserID)
	}

	var parsed struct {
		Code         string `json:"code"`
		EnrolCommand string `json:"enrolCommand"`
	}
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if parsed.Code != "ABC123" {
		t.Errorf("code = %q, want %q", parsed.Code, "ABC123")
	}
	want := "agentrqd enroll --server https://agentrq.example --code ABC123"
	if parsed.EnrolCommand != want {
		t.Errorf("enrolCommand = %q, want %q", parsed.EnrolCommand, want)
	}
}

// The tool mints a code and nothing else — it must not swallow an error the
// crud layer had a reason to return (rate limiting, for one).
func TestCreateEnrolmentCode_PropagatesError(t *testing.T) {
	ctrl := &mockMachineCrud{createEnrolmentCode: func(_ context.Context, _ entity.CreateEnrolmentCodeRequest) (*entity.CreateEnrolmentCodeResponse, error) {
		return nil, errors.New("rate limited")
	}}

	result := toolResult(machineServer("https://agentrq.example", ctrl).handleCreateEnrolmentCode(authedContext(), nil, struct{}{}))
	if !result.isError {
		t.Fatal("expected an error result")
	}
	if !strings.Contains(result.text, "rate limited") {
		t.Errorf("error text = %q, want it to mention %q", result.text, "rate limited")
	}
}

func TestListMachines_ListsTheCallersMachines(t *testing.T) {
	var got entity.ListMachinesRequest
	ctrl := &mockMachineCrud{listMachines: func(_ context.Context, req entity.ListMachinesRequest) (*entity.ListMachinesResponse, error) {
		got = req
		return &entity.ListMachinesResponse{Machines: []entity.MachineView{{ID: "m1", Name: "laptop", Enabled: true, Online: true, Sessions: 2}}}, nil
	}}

	body := textOf(t, toolResult(machineServer("", ctrl).handleListMachines(authedContext(), nil, struct{}{})))

	if got.UserID != testUserID {
		t.Errorf("UserID = %q, want %q", got.UserID, testUserID)
	}
	for _, want := range []string{`"id":"m1"`, `"name":"laptop"`, `"enabled":true`, `"online":true`, `"sessions":2`} {
		if !strings.Contains(body, want) {
			t.Errorf("body = %s, want it to carry %s", body, want)
		}
	}
}

func TestListMachines_PropagatesError(t *testing.T) {
	ctrl := &mockMachineCrud{listMachines: func(context.Context, entity.ListMachinesRequest) (*entity.ListMachinesResponse, error) {
		return nil, errors.New("database unavailable")
	}}
	r := toolResult(machineServer("", ctrl).handleListMachines(authedContext(), nil, struct{}{}))
	if !r.isError || !strings.Contains(r.text, "database unavailable") {
		t.Errorf("result = %+v", r)
	}
}

// fakeLauncher records what it was asked and answers as told.
type fakeLauncher struct {
	launch  agentlaunch.Request
	stop    agentlaunch.StopRequest
	session *entity.SessionView
	err     error
}

func (f *fakeLauncher) Launch(_ context.Context, rq agentlaunch.Request) (*entity.CreateSessionResponse, error) {
	f.launch = rq
	if f.err != nil {
		return nil, f.err
	}
	return &entity.CreateSessionResponse{Session: entity.SessionView{ID: "s1", Kind: rq.Kind, Status: "starting"}}, nil
}

func (f *fakeLauncher) Stop(_ context.Context, rq agentlaunch.StopRequest) (*entity.SessionView, error) {
	f.stop = rq
	return f.session, f.err
}

// launchAgent runs the launch the web form runs, as the calling account.
func TestLaunchAgent_UsesTheSharedLaunch(t *testing.T) {
	l := &fakeLauncher{}
	s := &WorkspaceServer{launcher: l}

	body := textOf(t, toolResult(s.handleLaunchAgent(authedContext(), nil, LaunchAgentParams{
		WorkspaceID: "w1", MachineID: "m1", Kind: "claude-code", Model: "opus", Effort: "high", Agent: "a",
	})))

	want := agentlaunch.Request{UserID: testUserID, WorkspaceID: "w1", MachineID: "m1", Kind: "claude-code", Model: "opus", Effort: "high", Agent: "a"}
	if l.launch != want {
		t.Errorf("request = %+v, want %+v", l.launch, want)
	}
	if body != `{"session":{"id":"s1","machineId":"","kind":"claude-code","status":"starting","createdAt":"0001-01-01T00:00:00Z"}}` {
		t.Errorf("body = %s", body)
	}
}

func TestLaunchAgent_Refused(t *testing.T) {
	l := &fakeLauncher{err: entity.NewLaunchError(entity.ErrLaunchBusy, "this workspace already has an agent connected")}
	r := toolResult((&WorkspaceServer{launcher: l}).handleLaunchAgent(authedContext(), nil, LaunchAgentParams{WorkspaceID: "w1", MachineID: "m1", Kind: "claude-code"}))
	if !r.isError || r.text != "this workspace already has an agent connected" {
		t.Errorf("result = %+v", r)
	}
}

func TestStopAgent_StopsTheWorkspacesAgent(t *testing.T) {
	l := &fakeLauncher{session: &entity.SessionView{ID: "s1", Status: "running"}}
	s := &WorkspaceServer{launcher: l}

	body := textOf(t, toolResult(s.handleStopAgent(authedContext(), nil, StopAgentParams{WorkspaceID: "w1"})))

	if l.stop != (agentlaunch.StopRequest{UserID: testUserID, WorkspaceID: "w1"}) {
		t.Errorf("request = %+v", l.stop)
	}
	if !strings.Contains(body, `"stopped":true`) || !strings.Contains(body, `"id":"s1"`) {
		t.Errorf("body = %s", body)
	}
}

func TestStopAgent_NothingRunning(t *testing.T) {
	body := textOf(t, toolResult((&WorkspaceServer{launcher: &fakeLauncher{}}).handleStopAgent(authedContext(), nil, StopAgentParams{WorkspaceID: "w1"})))
	if body != `{"session":null,"stopped":false}` {
		t.Errorf("body = %s", body)
	}
}

func TestStopAgent_Refused(t *testing.T) {
	l := &fakeLauncher{err: entity.NewLaunchError(entity.ErrLaunchBusy, "that machine is not connected")}
	r := toolResult((&WorkspaceServer{launcher: l}).handleStopAgent(authedContext(), nil, StopAgentParams{WorkspaceID: "w1"}))
	if !r.isError || r.text != "that machine is not connected" {
		t.Errorf("result = %+v", r)
	}
}

// New hands the server the launcher app.go shares with REST.
func TestNew_WiresTheLauncher(t *testing.T) {
	l := &fakeLauncher{}
	h, err := New(Params{Mux: http.NewServeMux(), Launcher: l})
	if err != nil {
		t.Fatal(err)
	}
	if got := h.(*handler).coremcpServer.launcher; got != l {
		t.Errorf("launcher = %v", got)
	}
}

// The SDK treats the enum as description only, so an unknown kind is refused
// here before it reaches the launcher.
func TestLaunchAgent_RefusesAnUnknownKind(t *testing.T) {
	for _, kind := range []string{"", "claude"} {
		l := &fakeLauncher{}
		r := toolResult((&WorkspaceServer{launcher: l}).handleLaunchAgent(authedContext(), nil, LaunchAgentParams{WorkspaceID: "w1", MachineID: "m1", Kind: kind}))
		if !r.isError || r.text != "kind must be one of claude-code, acp-gateway" {
			t.Errorf("%q: result = %+v", kind, r)
		}
		if l.launch != (agentlaunch.Request{}) {
			t.Errorf("%q reached the launcher: %+v", kind, l.launch)
		}
	}
}
