// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"reflect"
	"strings"
	"testing"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	view "github.com/agentrq/agentrq/backend/internal/data/view/api"
)

const notifyJSON = `"notificationSettings":{"taskCreated":true,"taskStatusUpdated":true,"taskReceivedMessage":true,"workspaceArchived":true,"workspaceUnarchived":true,"channels":["email"]}`

var notifyEntity = &entity.NotificationSettings{TaskCreated: true, TaskStatusUpdated: true, TaskReceivedMessage: true,
	WorkspaceArchived: true, WorkspaceUnarchived: true, Channels: []string{"email"}}

func TestFromHTTPRequestToCreateWorkspaceRequestEntity(t *testing.T) {
	checkRequest(t, "/workspaces", FromHTTPRequestToCreateWorkspaceRequestEntity, []requestCase[entity.CreateWorkspaceRequest]{
		{"every field", "/workspaces",
			`{"workspace":{"name":"api","description":"d","icon":"data:x",` + notifyJSON + `,"allowAllCommands":true,"clearContextDefault":true,` +
				`"selfLearningLoopNote":"n","inputSendDelaySeconds":3,"workingDirectory":"/src/api","autoAllowedTools":["Bash"]}}`,
			// Tools are allowed on an existing workspace, never on create.
			&entity.CreateWorkspaceRequest{Workspace: entity.Workspace{Name: "api", Description: "d", Icon: "data:x", NotificationSettings: notifyEntity,
				AllowAllCommands: true, ClearContextDefault: true, SelfLearningLoopNote: "n", InputSendDelaySeconds: 3, WorkingDirectory: "/src/api"}}},
		{"a name alone", "/workspaces", `{"workspace":{"name":"api"}}`, &entity.CreateWorkspaceRequest{Workspace: entity.Workspace{Name: "api"}}},
		{"no name", "/workspaces", `{"workspace":{"description":"d"}}`, nil},
		{"not JSON", "/workspaces", `{`, nil},
	})
}

func TestFromHTTPRequestToUpdateWorkspaceRequestEntity(t *testing.T) {
	ws := "/workspaces/" + b62(20)
	checkRequest(t, "/workspaces/:id", FromHTTPRequestToUpdateWorkspaceRequestEntity, []requestCase[entity.UpdateWorkspaceRequest]{
		{"every field", ws,
			`{"workspace":{"name":"api","description":"d","icon":"data:x",` + notifyJSON + `,"autoAllowedTools":["Bash"],"allowAllCommands":true,` +
				`"clearContextDefault":true,"selfLearningLoopNote":"n","inputSendDelaySeconds":3,"workingDirectory":"/src/api"}}`,
			&entity.UpdateWorkspaceRequest{Workspace: entity.Workspace{ID: 20, Name: "api", Description: "d", Icon: "data:x", NotificationSettings: notifyEntity,
				AutoAllowedTools: []string{"Bash"}, AllowAllCommands: true, ClearContextDefault: true, SelfLearningLoopNote: "n",
				InputSendDelaySeconds: 3, WorkingDirectory: "/src/api"}}},
		{"nothing", ws, `{}`, &entity.UpdateWorkspaceRequest{Workspace: entity.Workspace{ID: 20}}},
		{"workspace 0", "/workspaces/0", `{}`, nil},
		{"not JSON", ws, `{`, nil},
	})
}

func TestFromHTTPRequestToWorkspaceIDRequestEntities(t *testing.T) {
	ws := "/workspaces/" + b62(20)
	checkRequest(t, "/workspaces/:id", FromHTTPRequestToGetWorkspaceRequestEntity, []requestCase[entity.GetWorkspaceRequest]{
		{"a workspace", ws, ``, &entity.GetWorkspaceRequest{ID: 20}},
		{"workspace 0", "/workspaces/0", ``, nil},
	})
	checkRequest(t, "/workspaces/:id", FromHTTPRequestToDeleteWorkspaceRequestEntity, []requestCase[entity.DeleteWorkspaceRequest]{
		{"a workspace", ws, ``, &entity.DeleteWorkspaceRequest{ID: 20}},
		{"workspace 0", "/workspaces/0", ``, nil},
	})
	checkRequest(t, "/workspaces/:id/reset-id", FromHTTPRequestToResetWorkspaceIDRequestEntity, []requestCase[entity.ResetWorkspaceIDRequest]{
		{"a workspace", ws + "/reset-id", ``, &entity.ResetWorkspaceIDRequest{ID: 20}},
		{"workspace 0", "/workspaces/0/reset-id", ``, nil},
	})
}

func TestFromHTTPRequestToForkWorkspaceRequestEntity(t *testing.T) {
	ws := "/workspaces/" + b62(20) + "/fork"
	checkRequest(t, "/workspaces/:id/fork", FromHTTPRequestToForkWorkspaceRequestEntity, []requestCase[entity.ForkWorkspaceRequest]{
		{"a trimmed name", ws, `{"name":"  try-it  "}`, &entity.ForkWorkspaceRequest{WorkspaceID: 20, Name: "try-it"}},
		// The body is optional, and so is the name in it.
		{"no body", ws, ``, &entity.ForkWorkspaceRequest{WorkspaceID: 20}},
		{"no name", ws, `{}`, &entity.ForkWorkspaceRequest{WorkspaceID: 20}},
		{"128 characters", ws, `{"name":"` + strings.Repeat("ç", 128) + `"}`, &entity.ForkWorkspaceRequest{WorkspaceID: 20, Name: strings.Repeat("ç", 128)}},
		{"129 characters", ws, `{"name":"` + strings.Repeat("ç", 129) + `"}`, nil},
		{"workspace 0", "/workspaces/0/fork", ``, nil},
		{"not JSON", ws, `{`, nil},
	})
}

func TestFromHTTPRequestToMergeForkRequestEntity(t *testing.T) {
	ws := "/workspaces/" + b62(20) + "/merge"
	checkRequest(t, "/workspaces/:id/merge", FromHTTPRequestToMergeForkRequestEntity, []requestCase[entity.MergeForkRequest]{
		{"and delete the folder", ws, `{"deleteFolder":true}`, &entity.MergeForkRequest{WorkspaceID: 20, DeleteFolder: true}},
		// A merge with no body leaves the folder alone.
		{"no body", ws, ``, &entity.MergeForkRequest{WorkspaceID: 20}},
		{"workspace 0", "/workspaces/0/merge", ``, nil},
		{"not JSON", ws, `{`, nil},
	})
}

// fullWorkspace has every field set, and fullWorkspaceView is how the API
// writes it.
var (
	paused             = at.Add(3600e9)
	fullWorkspaceState = &entity.AgentState{WorkspaceID: "not in the view", WorkspaceName: "nor this", State: "paused", PausedUntil: &paused,
		MachineID: "m1", Model: "opus", TaskID: "t1", LastActivityAt: &at}
	fullWorkspaceStateView = &view.AgentState{State: "paused", PausedUntil: &paused, MachineID: "m1", Model: "opus", TaskID: "t1", LastActivityAt: &at}

	fullWorkspace = entity.Workspace{ID: 20, CreatedAt: at, UpdatedAt: at.Add(1), UserID: 9, Name: "api", Description: "d", ArchivedAt: &at,
		Icon: "data:x", NotificationSettings: notifyEntity, AgentConnected: true, Agent: fullWorkspaceState, AgentSupportsStop: true,
		AgentModels: &entity.AgentModels{ConfigID: "model", CurrentModel: "opus", CanSet: true,
			Models: []entity.AgentModel{{ID: "opus", Name: "Opus", Description: "big", Current: true, Group: "Claude"}, {ID: "haiku", Name: "Haiku"}}},
		AgentEffort:      &entity.AgentEffort{ConfigID: "effort", CurrentEffort: "high", Efforts: []entity.AgentEffortLevel{{ID: "high", Name: "High"}}},
		AgentCommands:    &entity.AgentCommands{Commands: []entity.AgentCommand{{Name: "compact", Description: "d", Hint: "h"}}},
		AgentClient:      &entity.AgentClient{Name: "acp-gateway", Title: "Gateway", Version: "0.2.13"},
		AgentConcurrency: &entity.AgentConcurrency{MaxConcurrency: 2, Active: 1, Queued: 3, Min: 1, Max: 8, CanSet: true},
		AutoAllowedTools: []string{"Bash"}, AllowAllCommands: true, ClearContextDefault: true, SelfLearningLoopNote: "n",
		InputSendDelaySeconds: 3, WorkingDirectory: "/src/api",
		Slack:    &entity.SlackConfig{Enabled: true, Installed: true, ChannelID: "C1", ChannelName: "api", AutoCreated: true, ClientID: "cid", AuthURL: "https://slack/auth"},
		ForkOfID: 19, ForkOf: &entity.WorkspaceRef{ID: 19, Name: "parent"}, ForkCount: 2, UnfinishedTasks: 4}
	fullWorkspaceView = view.Workspace{ID: b62(20), CreatedAt: at, UpdatedAt: at.Add(1), Name: "api", Description: "d", ArchivedAt: &at,
		Icon: "data:x", NotificationSettings: &view.NotificationSettings{TaskCreated: true, TaskStatusUpdated: true, TaskReceivedMessage: true,
			WorkspaceArchived: true, WorkspaceUnarchived: true, Channels: []string{"email"}},
		AgentConnected: true, Agent: fullWorkspaceStateView, AgentSupportsStop: true,
		AgentModels: &view.AgentModels{ConfigID: "model", CurrentModel: "opus", CanSet: true,
			Models: []view.AgentModel{{ID: "opus", Name: "Opus", Description: "big", Current: true, Group: "Claude"}, {ID: "haiku", Name: "Haiku"}}},
		AgentEffort:      &view.AgentEffort{ConfigID: "effort", CurrentEffort: "high", Efforts: []view.AgentEffortLevel{{ID: "high", Name: "High"}}},
		AgentCommands:    &view.AgentCommands{Commands: []view.AgentCommand{{Name: "compact", Description: "d", Hint: "h"}}},
		AgentClient:      &view.AgentClient{Name: "acp-gateway", Title: "Gateway", Version: "0.2.13"},
		AgentConcurrency: &view.AgentConcurrency{MaxConcurrency: 2, Active: 1, Queued: 3, Min: 1, Max: 8, CanSet: true},
		MCPURL:           "https://mcp/20", AutoAllowedTools: []string{"Bash"}, AllowAllCommands: true, ClearContextDefault: true,
		SelfLearningLoopNote: "n", InputSendDelaySeconds: 3, WorkingDirectory: "/src/api",
		Slack:    &view.SlackConfig{Enabled: true, Installed: true, ChannelID: "C1", ChannelName: "api", AutoCreated: true, ClientID: "cid", AuthURL: "https://slack/auth"},
		ForkOfID: b62(19), ForkOf: &view.WorkspaceRef{ID: b62(19), Name: "parent"}, ForkCount: 2, UnfinishedTasks: 4}

	// bareWorkspace leaves every optional field out.
	bareWorkspace     = entity.Workspace{ID: 21, CreatedAt: at, UpdatedAt: at, Name: "bare", Icon: "data:y"}
	bareWorkspaceView = view.Workspace{ID: b62(21), CreatedAt: at, UpdatedAt: at, Name: "bare", Icon: "data:y", MCPURL: "https://mcp/21"}
)

func TestFromEntityWorkspaceToView(t *testing.T) {
	if got := fromEntityWorkspaceToView(fullWorkspace, "https://mcp/20"); !reflect.DeepEqual(got, fullWorkspaceView) {
		t.Errorf("got  %+v\nwant %+v", got, fullWorkspaceView)
	}
	if got := fromEntityWorkspaceToView(bareWorkspace, "https://mcp/21"); !reflect.DeepEqual(got, bareWorkspaceView) {
		t.Errorf("got  %+v\nwant %+v", got, bareWorkspaceView)
	}
}

// The HTTP responses carry the icon; the MCP ones leave it out, since an
// agent has no use for an image in its context.
func TestWorkspaceResponses(t *testing.T) {
	noIcon := fullWorkspaceView
	noIcon.Icon = ""
	type one struct {
		Workspace view.Workspace `json:"workspace"`
	}
	for name, tc := range map[string]struct {
		raw  []byte
		want view.Workspace
	}{
		"create":     {FromCreateWorkspaceResponseEntityToHTTPResponse(&entity.CreateWorkspaceResponse{Workspace: fullWorkspace}, "https://mcp/20"), fullWorkspaceView},
		"create MCP": {FromCreateWorkspaceResponseEntityToMCPResponse(&entity.CreateWorkspaceResponse{Workspace: fullWorkspace}, "https://mcp/20"), noIcon},
		"get":        {FromGetWorkspaceResponseEntityToHTTPResponse(&entity.GetWorkspaceResponse{Workspace: fullWorkspace}, "https://mcp/20"), fullWorkspaceView},
		"get MCP":    {FromGetWorkspaceResponseEntityToMCPResponse(&entity.GetWorkspaceResponse{Workspace: fullWorkspace}, "https://mcp/20"), noIcon},
		"update":     {FromUpdateWorkspaceResponseEntityToHTTPResponse(&fullWorkspace, "https://mcp/20"), fullWorkspaceView},
		"update MCP": {FromUpdateWorkspaceResponseEntityToMCPResponse(&fullWorkspace, "https://mcp/20"), noIcon},
		"fork":       {FromForkWorkspaceResponseEntityToHTTPResponse(&entity.ForkWorkspaceResponse{Workspace: fullWorkspace}, "https://mcp/20"), fullWorkspaceView},
		"fork MCP":   {FromForkWorkspaceResponseEntityToMCPResponse(&entity.ForkWorkspaceResponse{Workspace: fullWorkspace}, "https://mcp/20"), noIcon},
	} {
		t.Run(name, func(t *testing.T) { checkResponse(t, tc.raw, one{tc.want}) })
	}
}

func TestListWorkspacesResponses(t *testing.T) {
	rs := &entity.ListWorkspacesResponse{Workspaces: []entity.Workspace{fullWorkspace, bareWorkspace}}
	mcpURL := func(id int64) string { return "https://mcp/" + map[int64]string{20: "20", 21: "21"}[id] }

	checkResponse(t, FromListWorkspacesResponseEntityToHTTPResponse(rs, mcpURL),
		view.ListWorkspacesResponse{Workspaces: []view.Workspace{fullWorkspaceView, bareWorkspaceView}})

	noIcon, bareNoIcon := fullWorkspaceView, bareWorkspaceView
	noIcon.Icon, bareNoIcon.Icon = "", ""
	checkResponse(t, FromListWorkspacesResponseEntityToMCPResponse(rs, mcpURL),
		view.ListWorkspacesResponse{Workspaces: []view.Workspace{noIcon, bareNoIcon}})

	checkResponse(t, FromListWorkspacesResponseEntityToMCPSummaryResponse(rs), view.ListWorkspaceSummariesResponse{Workspaces: []view.WorkspaceSummary{
		{ID: b62(20), Name: "api", ForkOfID: b62(19), WorkingDirectory: "/src/api", Agent: fullWorkspaceStateView},
		{ID: b62(21), Name: "bare"},
	}})

	empty := &entity.ListWorkspacesResponse{}
	for name, raw := range map[string][]byte{
		"HTTP":    FromListWorkspacesResponseEntityToHTTPResponse(empty, mcpURL),
		"MCP":     FromListWorkspacesResponseEntityToMCPResponse(empty, mcpURL),
		"summary": FromListWorkspacesResponseEntityToMCPSummaryResponse(empty),
	} {
		if string(raw) != `{"workspaces":[]}` {
			t.Errorf("%s: no workspaces = %s, want an empty list", name, raw)
		}
	}
}

func TestFromEntityAgentModelsToView(t *testing.T) {
	if fromEntityAgentModelsToView(nil) != nil {
		t.Error("nil did not stay nil")
	}
	// A list with no models is still a list.
	got := fromEntityAgentModelsToView(&entity.AgentModels{ConfigID: "model"})
	if got == nil || got.Models == nil || len(got.Models) != 0 || got.ConfigID != "model" {
		t.Errorf("got %+v", got)
	}
}

func TestWorkspaceIDResponses(t *testing.T) {
	if got := string(FromResetWorkspaceIDResponseEntityToHTTPResponse(&entity.ResetWorkspaceIDResponse{ID: 22})); got != `{"id":"`+b62(22)+`"}` {
		t.Errorf("reset = %s", got)
	}
	if got := string(FromMergeForkResponseEntityToHTTPResponse(&entity.MergeForkResponse{ParentID: b62(19), MovedTasks: 3, Tasks: []entity.Task{{ID: 1}}})); got !=
		`{"parentId":"`+b62(19)+`","movedTasks":3}` {
		t.Errorf("merge = %s", got)
	}
}

func TestFromMCPToForkWorkspaceRequestEntity(t *testing.T) {
	if got := FromMCPToForkWorkspaceRequestEntity(20, "\ttry\n"); !reflect.DeepEqual(got, &entity.ForkWorkspaceRequest{WorkspaceID: 20, Name: "try"}) {
		t.Errorf("got %+v", got)
	}
	// Spaces around a long name do not count against it.
	if got := FromMCPToForkWorkspaceRequestEntity(20, "  "+strings.Repeat("a", 128)+"  "); got == nil || got.Name != strings.Repeat("a", 128) {
		t.Errorf("got %+v", got)
	}
	if got := FromMCPToForkWorkspaceRequestEntity(20, strings.Repeat("a", 129)); got != nil {
		t.Errorf("a 129-character name = %+v, want nil", got)
	}
}
