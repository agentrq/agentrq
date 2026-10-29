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

	"github.com/agentrq/agentrq/backend/internal/controller/agentlink"
	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
)

type fakePresence struct {
	connected map[int64]bool
	models    map[int64]string
}

func (f fakePresence) IsAgentConnected(id int64) bool { return f.connected[id] }

func (f fakePresence) AgentModels(id int64) *agentlink.ModelsSnapshot {
	if m, ok := f.models[id]; ok {
		return &agentlink.ModelsSnapshot{CurrentModel: m}
	}
	return nil
}

type agentCrud struct {
	crud.Controller
	workspaces  []entity.Workspace
	machines    []entity.MachineView
	activity    map[int64]entity.AgentActivity
	activityErr error
}

func (m *agentCrud) ListWorkspaces(context.Context, entity.ListWorkspacesRequest) (*entity.ListWorkspacesResponse, error) {
	return &entity.ListWorkspacesResponse{Workspaces: m.workspaces}, nil
}

func (m *agentCrud) GetWorkspace(_ context.Context, req entity.GetWorkspaceRequest) (*entity.GetWorkspaceResponse, error) {
	return &entity.GetWorkspaceResponse{Workspace: entity.Workspace{ID: req.ID, Name: "api", Description: "a long mission"}}, nil
}

func (m *agentCrud) ListMachines(context.Context, entity.ListMachinesRequest) (*entity.ListMachinesResponse, error) {
	return &entity.ListMachinesResponse{Machines: m.machines}, nil
}

func (m *agentCrud) ListAgentActivity(context.Context, entity.ListAgentActivityRequest) (*entity.ListAgentActivityResponse, error) {
	if m.activityErr != nil {
		return nil, m.activityErr
	}
	return &entity.ListAgentActivityResponse{Workspaces: m.activity}, nil
}

func TestAgentStateOf(t *testing.T) {
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	at := func(d time.Duration) *time.Time { v := now.Add(d); return &v }
	cases := []struct {
		name      string
		a         entity.AgentActivity
		connected bool
		want      string
		paused    *time.Time
	}{
		{"nothing at all", entity.AgentActivity{}, false, agentOffline, nil},
		{"an ongoing task nobody works on", entity.AgentActivity{TaskID: 70}, false, agentOffline, nil},
		{"connected, no terminal, no task", entity.AgentActivity{}, true, agentIdle, nil},
		{"connected, no terminal, a task", entity.AgentActivity{TaskID: 70}, true, agentRunning, nil},
		{"a session not yet reported", entity.AgentActivity{MachineID: 3}, false, agentIdle, nil},
		{"printing", entity.AgentActivity{MachineID: 3, LastOutputAt: at(-10 * time.Second)}, true, agentRunning, nil},
		{"quiet", entity.AgentActivity{MachineID: 3, LastOutputAt: at(-time.Hour)}, true, agentIdle, nil},
		{"limited until later", entity.AgentActivity{MachineID: 3, LastOutputAt: at(-time.Second), UsageLimited: true, UsageResetsAt: at(time.Hour)}, true, agentPaused, at(time.Hour)},
		{"a limit that has lifted", entity.AgentActivity{MachineID: 3, LastOutputAt: at(-time.Hour), UsageLimited: true, UsageResetsAt: at(-time.Minute)}, true, agentIdle, nil},
		{"limited, no reset, quiet", entity.AgentActivity{MachineID: 3, LastOutputAt: at(-time.Hour), UsageLimited: true}, true, agentPaused, nil},
		{"limited, no reset, printing", entity.AgentActivity{MachineID: 3, LastOutputAt: at(-time.Second), UsageLimited: true}, true, agentRunning, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := agentStateOf(7, c.a, c.connected, "", now)
			if got.State != c.want {
				t.Errorf("state = %q, want %q", got.State, c.want)
			}
			if (got.PausedUntil == nil) != (c.paused == nil) || got.PausedUntil != nil && !got.PausedUntil.Equal(*c.paused) {
				t.Errorf("pausedUntil = %v, want %v", got.PausedUntil, c.paused)
			}
		})
	}
}

func TestAgentStateOf_Fields(t *testing.T) {
	seen := time.Now()
	a := entity.AgentActivity{WorkspaceName: "api", MachineID: 3, Model: "opus", TaskID: 70, LastOutputAt: &seen}
	got := agentStateOf(7, a, true, "", seen)
	want := entity.AgentState{WorkspaceID: b62(7), WorkspaceName: "api", State: agentRunning, MachineID: b62(3), Model: "opus", TaskID: b62(70), LastActivityAt: &seen}
	if got != want {
		t.Errorf("state = %+v, want %+v", got, want)
	}
	// The model the agent says it runs now beats the one it was launched with.
	if got := agentStateOf(7, a, true, "sonnet", seen); got.Model != "sonnet" {
		t.Errorf("model = %q, want sonnet", got.Model)
	}
}

// agentConnected was never filled for the supervisor and read false for an
// agent at work; get now says it and what the agent is doing.
func TestGetWorkspace_SaysWhatTheAgentIsDoing(t *testing.T) {
	s := &WorkspaceServer{
		crud:   &agentCrud{activity: map[int64]entity.AgentActivity{7: {MachineID: 3, TaskID: 70}}},
		agents: fakePresence{connected: map[int64]bool{7: true}, models: map[int64]string{7: "opus"}},
	}
	text := textOf(t, toolResult(s.handleGetWorkspace(authedContext(), nil, GetWorkspaceParams{ID: b62(7)})))
	var got struct {
		Workspace struct {
			AgentConnected bool `json:"agentConnected"`
			Agent          struct {
				State, MachineID, Model, TaskID string
			} `json:"agent"`
		} `json:"workspace"`
	}
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatal(err)
	}
	w := got.Workspace
	if !w.AgentConnected || w.Agent.State != agentRunning || w.Agent.MachineID != b62(3) || w.Agent.Model != "opus" || w.Agent.TaskID != b62(70) {
		t.Errorf("workspace = %s", text)
	}
}

// The short list leaves out every workspace's description and settings.
func TestListWorkspaces_IsShortUnlessDetailsAreAsked(t *testing.T) {
	s := &WorkspaceServer{crud: &agentCrud{
		workspaces: []entity.Workspace{
			{ID: 7, Name: "api", Description: "a long mission", WorkingDirectory: "/src/api"},
			{ID: 8, Name: "api-fork", ForkOfID: 7},
		},
		activity: map[int64]entity.AgentActivity{7: {TaskID: 70}},
	}}

	short := textOf(t, toolResult(s.handleListWorkspaces(authedContext(), nil, ListWorkspacesParams{})))
	want := `{"workspaces":[{"id":"` + b62(7) + `","name":"api","workingDirectory":"/src/api","agent":{"state":"offline","taskId":"` + b62(70) + `"}},` +
		`{"id":"` + b62(8) + `","name":"api-fork","forkOfId":"` + b62(7) + `","agent":{"state":"offline"}}]}`
	if short != want {
		t.Errorf("list = %s\nwant   %s", short, want)
	}

	full := textOf(t, toolResult(s.handleListWorkspaces(authedContext(), nil, ListWorkspacesParams{IncludeDetails: true})))
	if !strings.Contains(full, `"description":"a long mission"`) || !strings.Contains(full, `"agent":{"state":"offline","taskId":"`+b62(70)+`"}`) {
		t.Errorf("list with details = %s", full)
	}
}

// Each machine names the workspace of every agent on it, sorted by name.
func TestListMachines_ListsTheAgentsOnEachMachine(t *testing.T) {
	seen := time.Now()
	s := &WorkspaceServer{crud: &agentCrud{
		machines: []entity.MachineView{{ID: b62(3), Name: "laptop"}, {ID: b62(4), Name: "idle box"}},
		activity: map[int64]entity.AgentActivity{
			7:  {WorkspaceName: "web", MachineID: 3, LastOutputAt: &seen},
			8:  {WorkspaceName: "api", MachineID: 3, UsageLimited: true, LastOutputAt: &seen},
			9:  {WorkspaceName: "api", MachineID: 3},
			10: {TaskID: 100},                         // no machine
			11: {WorkspaceName: "gone", MachineID: 5}, // a machine not listed
		},
	}}
	text := textOf(t, toolResult(s.handleListMachines(authedContext(), nil, struct{}{})))
	var got entity.ListMachinesResponse
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatal(err)
	}
	agents := got.Machines[0].Agents
	if len(agents) != 3 || agents[0].WorkspaceID != b62(8) || agents[1].WorkspaceID != b62(9) || agents[2].WorkspaceName != "web" {
		t.Fatalf("laptop's agents = %+v", agents)
	}
	if agents[2].State != agentRunning || agents[0].State != agentRunning {
		t.Errorf("states = %q, %q", agents[0].State, agents[2].State)
	}
	if len(got.Machines[1].Agents) != 0 {
		t.Errorf("idle box's agents = %+v", got.Machines[1].Agents)
	}
}

func TestAgentActivityFailures(t *testing.T) {
	s := &WorkspaceServer{crud: &agentCrud{activityErr: errors.New("database unavailable")}}
	for name, r := range map[string]callResult{
		"get":      toolResult(s.handleGetWorkspace(authedContext(), nil, GetWorkspaceParams{ID: b62(7)})),
		"list":     toolResult(s.handleListWorkspaces(authedContext(), nil, ListWorkspacesParams{})),
		"machines": toolResult(s.handleListMachines(authedContext(), nil, struct{}{})),
	} {
		if !r.isError || r.text != "database unavailable" {
			t.Errorf("%s = %+v", name, r)
		}
	}
}

// New hands the server the MCP manager app.go passes it.
func TestNew_WiresTheAgents(t *testing.T) {
	p := fakePresence{}
	h, err := New(Params{Mux: http.NewServeMux(), Agents: p})
	if err != nil {
		t.Fatal(err)
	}
	if got := h.(*handler).coremcpServer.agents; got == nil {
		t.Error("agents not wired")
	}
}
