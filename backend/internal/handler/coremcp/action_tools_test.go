// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package coremcp

import (
	"context"
	"encoding/json"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"

	mcpevent "github.com/agentrq/agentrq/backend/internal/controller/mcp"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Every retired tool is an action, and that action, given its own example,
// runs the handler of the tool it replaced: the example decodes into the
// payload with every required field, and the call is still recorded under the
// old name. An action with no example takes no payload. An action added since
// replaced no tool, and is recorded under the name newerActions gives it.
func TestActionTools_EveryExampleRunsTheToolItReplaced(t *testing.T) {
	newerActions := map[string]mcpevent.LegacyTool{
		"getTaskMessages": {Tool: "task", Action: "messages"},
	}
	var recorded string
	fps := &fakePubSub{onPublish: func(req pubsub.PublishRequest) {
		if recorded == "" {
			recorded = req.Event.(mcpevent.MCPEvent).ToolName
		}
	}}
	srv := &WorkspaceServer{crud: &taskCrud{}, pubsub: fps}
	// Every handler records the call before it reaches the controller, so a
	// fake that does not answer it (a panic) still tells which handler ran.
	run := func(tool string, a mcpevent.ToolAction, payload map[string]any) (res *mcp.CallToolResult) {
		recorded = ""
		defer func() { _ = recover() }()
		res, _, _ = mcpevent.RunAction(authedContext(), nil, tool, []mcpevent.ToolAction{a}, a.Name, payload)
		return res
	}
	byTool := map[string][]mcpevent.ToolAction{}
	for _, tool := range srv.tools() {
		byTool[tool.Name] = tool.Actions
	}
	covered := map[string]bool{}
	recordedAs := maps.Clone(legacyTools)
	maps.Copy(recordedAs, newerActions)
	for old, l := range recordedAs {
		var action *mcpevent.ToolAction
		for i, a := range byTool[l.Tool] {
			if a.Name == l.Action {
				action = &byTool[l.Tool][i]
			}
		}
		if action == nil {
			t.Errorf("%s maps to %s.%s, which does not exist", old, l.Tool, l.Action)
			continue
		}
		covered[l.Tool+"."+l.Action] = true
		var payload map[string]any
		if action.Example != "" {
			if err := json.Unmarshal([]byte(action.Example), &payload); err != nil {
				t.Errorf("%s.%s's example is not JSON: %v", l.Tool, l.Action, err)
				continue
			}
		}
		res := run(l.Tool, *action, payload)
		if recorded != old {
			text := ""
			if res != nil && len(res.Content) > 0 {
				text = res.Content[0].(*mcp.TextContent).Text
			}
			t.Errorf("%s.%s with its example was recorded as %q (%s), want %q", l.Tool, l.Action, recorded, text, old)
		}
	}
	for tool, actions := range byTool {
		if tool == "pano" {
			continue
		}
		for _, a := range actions {
			if !covered[tool+"."+a.Name] {
				t.Errorf("%s.%s replaces no retired tool", tool, a.Name)
			}
		}
	}
}

// The enum each tool's schema offers is exactly its actions and help.
func TestActionTools_EnumsAreTheirActions(t *testing.T) {
	srv := &WorkspaceServer{}
	params := map[string]any{
		"workspace": WorkspaceToolParams{}, "task": TaskParams{}, "memory": MemoryToolParams{}, "skills": SkillsToolParams{},
		"events": EventsToolParams{}, "workflows": WorkflowsToolParams{}, "machines": MachinesToolParams{}, "pano": mcpevent.PanoParams{},
	}
	for _, tool := range srv.tools() {
		f, _ := reflect.TypeOf(params[tool.Name]).FieldByName("Action")
		var names []string
		for _, a := range tool.Actions {
			names = append(names, a.Name)
		}
		if got := strings.Split(strings.TrimPrefix(f.Tag.Get("jsonschema"), "enum: "), ", "); !slices.Equal(got, append(names, "help")) {
			t.Errorf("%s offers %v, but runs %v and help", tool.Name, got, names)
		}
	}
}

// Each tool routes its own actions, records help and a mistake as itself,
// and help explains the whole server.
func TestActionTools_HandlersAndHelp(t *testing.T) {
	var recorded []string
	fps := &fakePubSub{onPublish: func(req pubsub.PublishRequest) { recorded = append(recorded, req.Event.(mcpevent.MCPEvent).ToolName) }}
	srv := &WorkspaceServer{crud: &taskCrud{}, pubsub: fps}
	ctx := authedContext()
	ws := map[string]any{"workspaceId": base62(testWorkspace)}
	for _, call := range []func() (*mcp.CallToolResult, any, error){
		func() (*mcp.CallToolResult, any, error) {
			return srv.handleWorkspaceTool(ctx, nil, WorkspaceToolParams{Action: "list"})
		},
		func() (*mcp.CallToolResult, any, error) {
			return srv.handleMemoryTool(ctx, nil, MemoryToolParams{Action: "help"})
		},
		func() (*mcp.CallToolResult, any, error) {
			return srv.handleSkillsTool(ctx, nil, SkillsToolParams{Action: "help"})
		},
		func() (*mcp.CallToolResult, any, error) {
			return srv.handleEventsTool(ctx, nil, EventsToolParams{Action: "help"})
		},
		func() (*mcp.CallToolResult, any, error) {
			return srv.handleWorkflowsTool(ctx, nil, WorkflowsToolParams{Action: "nope"})
		},
		func() (*mcp.CallToolResult, any, error) {
			return srv.handleMachinesTool(ctx, nil, MachinesToolParams{Action: "help", Payload: map[string]any{"topic": "launchAgent"}})
		},
		func() (*mcp.CallToolResult, any, error) {
			return srv.handleTask(ctx, nil, TaskParams{Action: "list", Payload: ws})
		},
	} {
		if _, _, err := call(); err != nil {
			t.Fatal(err)
		}
	}
	if want := []string{"listWorkspaces", "memory", "skills", "events", "workflows", "machines", "listTasks"}; !slices.Equal(recorded, want) {
		t.Errorf("recorded %v, want %v", recorded, want)
	}

	recorded = nil
	for _, tc := range []struct{ topic, want string }{
		{"", "Every tool takes {\"action\": \"<action>\", \"payload\": {...}}."},
		{"", "  workflows    Graphs of events and the workspaces they hand off to."},
		{"", "  pano         Markdown docs every workspace of the account shares."},
		{"machines", "  createEnrolmentCode  Mint a one-time code"},
		{"events.createTrigger", "Example:\n  events {\"action\": \"createTrigger\", \"payload\": {\"eventId\": \"0kEvt7hQx2M\""},
		{"workspace.mergeFork", "Refused while any task in the fork is not completed or rejected."},
	} {
		res, _, _ := srv.handleHelpTool(ctx, nil, HelpToolParams{Action: tc.topic})
		if text := res.Content[0].(*mcp.TextContent).Text; res.IsError || !strings.Contains(text, tc.want) {
			t.Errorf("help %q lacks %q:\n%s", tc.topic, tc.want, text)
		}
	}
	res, _, _ := srv.handleHelpTool(ctx, nil, HelpToolParams{Action: "triggers"})
	if text := res.Content[0].(*mcp.TextContent).Text; !res.IsError || !strings.Contains(text, `there is no tool "triggers"`) {
		t.Errorf("help on an unknown tool answered %q", text)
	}
	if want := []string{"help", "help", "help", "help", "help", "help", "help"}; !slices.Equal(recorded, want) {
		t.Errorf("help was recorded as %v", recorded)
	}
}

// The retired names answer unlisted, through the server, as their actions.
func TestActionTools_RetiredNamesStillAnswer(t *testing.T) {
	srv := NewServer(&taskCrud{}, "https://agentrq.example", nil)
	ctx := context.Background()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	ss, err := srv.server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })

	listed, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range listed.Tools {
		names = append(names, tool.Name)
	}
	slices.Sort(names)
	if want := []string{"events", "help", "machines", "memory", "pano", "skills", "task", "workflows", "workspace"}; !slices.Equal(names, want) {
		t.Errorf("the supervisor lists %v, want %v", names, want)
	}

	for _, tc := range []struct {
		name string
		args map[string]any
	}{
		{"listWorkspaces", map[string]any{"includeArchived": true}},
		{"workspace", map[string]any{"action": "list"}},
		{"help", map[string]any{"action": "task.respond"}},
	} {
		res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: tc.name, Arguments: tc.args})
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		// No user in the context, so a call that reaches its handler is
		// refused there, not for its name or its payload.
		if text := res.Content[0].(*mcp.TextContent).Text; res.IsError && text != "context canceled" {
			t.Errorf("%s failed: %s", tc.name, text)
		}
	}
}
