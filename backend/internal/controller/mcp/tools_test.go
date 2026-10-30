// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	mock_pubsub "github.com/agentrq/agentrq/backend/internal/service/mocks/pubsub"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
	"github.com/golang/mock/gomock"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// recordingServer is a server whose site tools answer from a fake, and which
// keeps the tool name of every call it records.
func recordingServer(t *testing.T) (*WorkspaceServer, *[]string) {
	t.Helper()
	s := newSiteServer(t, &fakeSiteTools{shares: []SiteShareView{githubShare()}, text: "ok"}, "accept", nil)
	pub := mock_pubsub.NewMockService(gomock.NewController(t))
	var recorded []string
	pub.EXPECT().Publish(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, req pubsub.PublishRequest) (*pubsub.PublishResponse, error) {
		recorded = append(recorded, req.Event.(MCPEvent).ToolName)
		return nil, nil
	}).AnyTimes()
	s.pubsub = pub
	s.listTasks = func(context.Context, ListTasksFilter) ([]model.Task, error) { return nil, nil }
	s.getTask = func(context.Context, int64) (model.Task, error) { return model.Task{}, nil }
	s.publishEvent = func(context.Context, string, string, []entity.EventFAQ, WorkflowRunContext) error { return nil }
	return s.WorkspaceServer, &recorded
}

// Every action that takes a payload has an example, and the example is one
// the action accepts: it decodes into the payload, with every required field.
func TestTools_EveryExampleIsAPayloadItsActionTakes(t *testing.T) {
	for _, tool := range (&WorkspaceServer{}).tools() {
		for _, a := range tool.Actions {
			if a.payload.NumField() == 0 {
				continue
			}
			if a.Example == "" {
				t.Errorf("%s.%s has no example", tool.Name, a.Name)
				continue
			}
			var payload map[string]any
			if err := json.Unmarshal([]byte(a.Example), &payload); err != nil {
				t.Errorf("%s.%s's example is not a JSON object: %v", tool.Name, a.Name, err)
				continue
			}
			if err := decodePayload(payload, reflect.New(a.payload).Interface()); err != nil {
				t.Errorf("%s.%s's example is refused: %v", tool.Name, a.Name, err)
			}
		}
	}
}

// The enum of action each tool offers a client is exactly its actions and help.
func TestTools_EnumsMatchTheirActions(t *testing.T) {
	enums := map[string][]any{}
	for _, tool := range toolsOverTheWire(t) {
		schema, _ := tool["inputSchema"].(map[string]any)
		props, _ := schema["properties"].(map[string]any)
		action, _ := props["action"].(map[string]any)
		enum, _ := action["enum"].([]any)
		enums[tool["name"].(string)] = enum
	}
	for _, tool := range (&WorkspaceServer{}).tools() {
		want := []any{}
		for _, a := range tool.Actions {
			want = append(want, a.Name)
		}
		if got := enums[tool.Name]; !reflect.DeepEqual(got, append(want, "help")) {
			t.Errorf("%s offers actions %v, but runs %v and help", tool.Name, got, want)
		}
	}
}

// A params type the schema package cannot describe is a programming error,
// caught when the server registers its tools.
func TestActionInputSchema_PanicsOnATypeWithNoSchema(t *testing.T) {
	defer func() {
		if r := recover(); r == nil || !strings.Contains(fmt.Sprint(r), "the input schema of struct { Action chan int") {
			t.Errorf("ActionInputSchema of a channel field recovered %v, want a panic naming the type", r)
		}
	}()
	ActionInputSchema[struct{ Action chan int }](nil)
}

// Each action reaches the handler of the tool it replaced, which records the
// call under that tool's name; help, or an action a tool does not have, is
// recorded as the tool.
func TestTools_RouteEachActionToItsHandler(t *testing.T) {
	ps, recorded := recordingServer(t)
	example := func(tool, action string) map[string]any {
		for _, tt := range ps.tools() {
			for _, a := range tt.Actions {
				if tt.Name == tool && a.Name == action && a.Example != "" {
					var p map[string]any
					_ = json.Unmarshal([]byte(a.Example), &p)
					return p
				}
			}
		}
		return nil
	}
	calls := []struct {
		run  func(context.Context, *mcp.CallToolRequest, string, map[string]any) (*mcp.CallToolResult, any, error)
		tool string
		want []string // [action, the name it is recorded as]
	}{
		{func(ctx context.Context, req *mcp.CallToolRequest, a string, p map[string]any) (*mcp.CallToolResult, any, error) {
			return ps.handleMemory(ctx, req, MemoryParams{Action: a, Payload: p})
		}, "memory", []string{"load", "loadMemory", "save", "saveMemory", "delete", "deleteMemory", "help", "memory", "forget", "memory"}},
		{func(ctx context.Context, req *mcp.CallToolRequest, a string, p map[string]any) (*mcp.CallToolResult, any, error) {
			return ps.handleSkills(ctx, req, SkillsParams{Action: a, Payload: p})
		}, "skills", []string{"search", "searchSkills", "load", "loadSkill", "save", "saveSkill", "delete", "deleteSkill", "help", "skills"}},
		{func(ctx context.Context, req *mcp.CallToolRequest, a string, p map[string]any) (*mcp.CallToolResult, any, error) {
			return ps.handleWorkspace(ctx, req, WorkspaceParams{Action: a, Payload: p})
		}, "workspace", []string{"get", "getWorkspace", "publishEvent", "publishEvent", "help", "workspace"}},
		{func(ctx context.Context, req *mcp.CallToolRequest, a string, p map[string]any) (*mcp.CallToolResult, any, error) {
			return ps.handleSiteTools(ctx, req, SiteToolsParams{Action: a, Payload: p})
		}, "siteTools", []string{"search", "listSiteTools", "getDefinition", "getSiteToolDefinition", "call", "callSiteTool", "help", "siteTools"}},
	}
	for _, c := range calls {
		for i := 0; i < len(c.want); i += 2 {
			*recorded = nil
			if _, _, err := c.run(context.Background(), nil, c.want[i], example(c.tool, c.want[i])); err != nil {
				t.Fatalf("%s.%s: %v", c.tool, c.want[i], err)
			}
			if len(*recorded) == 0 || (*recorded)[0] != c.want[i+1] {
				t.Errorf("%s.%s was recorded as %v, want %s", c.tool, c.want[i], *recorded, c.want[i+1])
			}
		}
	}
}

func TestHelpTool(t *testing.T) {
	ps, recorded := recordingServer(t)
	help := func(topic string) (string, bool) {
		res, _, _ := ps.handleHelp(context.Background(), nil, HelpParams{Action: topic})
		return taskResult(t, res)
	}

	text, _ := help("")
	for _, tool := range ps.tools() {
		if !strings.Contains(text, "\n  "+tool.Name+" ") {
			t.Errorf("help does not list %s:\n%s", tool.Name, text)
		}
	}
	if !strings.Contains(text, "actions: load, save, delete") || !strings.Contains(text, `{"action": "<tool>.<action>"}`) {
		t.Errorf("help does not list a tool's actions or say how to ask for one:\n%s", text)
	}

	text, _ = help("siteTools")
	if !strings.HasPrefix(text, `Usage: siteTools {"action": "<action>", "payload": {...}}`) || !strings.Contains(text, "  getDefinition ") {
		t.Errorf("help siteTools:\n%s", text)
	}

	text, _ = help("memory.save")
	for _, want := range []string{
		`Usage: memory {"action": "save", "payload": {"content": <string>, ...}}`,
		"There is no append",
		"\nExample:\n  memory {\"action\": \"save\", \"payload\": {\"name\": \"deploys.md\"",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("help memory.save lacks %q:\n%s", want, text)
		}
	}

	// An action with no payload says so, with what more there is to know.
	text, _ = help("workspace.get")
	if text != "Usage: workspace {\"action\": \"get\"}\n\nGet this workspace's title and mission description." {
		t.Errorf("help workspace.get = %q", text)
	}

	if text, isErr := help("tasks"); !isErr || text != `there is no tool "tasks": the tools are task, memory, skills, workspace, siteTools, pano` {
		t.Errorf("help on an unknown tool answered %q (error %v)", text, isErr)
	}
	if len(*recorded) != 5 || (*recorded)[0] != "help" {
		t.Errorf("help was recorded as %v", *recorded)
	}
}

// An action with nothing to add has no details, and one without a payload
// prints them all the same.
func TestActionHelp_PrintsDetailsOfAnActionWithoutPayload(t *testing.T) {
	a := ToolAction{Name: "ping", Summary: "Pings."}.WithDetails("Twice, if asked.")
	text, _ := taskResult(t, ActionHelp("x", []ToolAction{a}, "ping"))
	if text != "Usage: x {\"action\": \"ping\"}\n\nPings.\n\nTwice, if asked." {
		t.Errorf("help = %q", text)
	}
}

// The retired names still answer, unlisted, as the actions they became.
func TestTools_RetiredNamesStillAnswer(t *testing.T) {
	cs := connectWorkspace(t)
	listed, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range listed.Tools {
		names = append(names, tool.Name)
	}
	slices.Sort(names)
	if want := []string{"help", "memory", "pano", "siteTools", "skills", "task", "workspace"}; !slices.Equal(names, want) {
		t.Errorf("the server lists %v, want %v", names, want)
	}
	for _, tc := range []struct {
		name string
		args map[string]any
		want string
	}{
		{"loadMemory", map[string]any{"name": "memory.md"}, "memory is not available on this server"},
		{"searchSkills", nil, "skills are not available on this server"},
		{"saveSkill", map[string]any{"uri": "skill://x"}, "save: payload needs content"},
		{"memory", map[string]any{"action": "delete", "name": "x.md"}, "memory is not available on this server"},
	} {
		res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: tc.name, Arguments: tc.args})
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if text, isErr := taskResult(t, res); !isErr || text != tc.want {
			t.Errorf("%s %v answered %q (error %v), want the error %q", tc.name, tc.args, text, isErr, tc.want)
		}
	}
}
