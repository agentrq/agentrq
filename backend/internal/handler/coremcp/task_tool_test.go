// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package coremcp

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	mcpevent "github.com/agentrq/agentrq/backend/internal/controller/mcp"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// taskCrud answers every task call the task tool makes.
type taskCrud struct{ mockServerCrud }

func (m *taskCrud) CreateTask(_ context.Context, req entity.CreateTaskRequest) (*entity.CreateTaskResponse, error) {
	return &entity.CreateTaskResponse{Task: req.Task}, nil
}

func (m *taskCrud) DeleteTask(_ context.Context, req entity.DeleteTaskRequest) (*entity.DeleteTaskResponse, error) {
	return &entity.DeleteTaskResponse{}, nil
}

// Every action runs the handler of the tool it replaced, which still records
// the call under that tool's name, so telemetry per action carries on.
func TestTaskTool_RunsEachActionAsTheToolItReplaced(t *testing.T) {
	ws, task := base62(testWorkspace), base62(testTriggerID)
	for _, tc := range []struct {
		action, tool string
		payload      map[string]any
	}{
		{"list", "listTasks", map[string]any{"workspaceId": ws, "status": "ongoing", "limit": 99}},
		{"listAll", "listAllTasks", nil},
		{"create", "createTask", map[string]any{"workspaceId": ws, "title": "t"}},
		{"get", "getTask", map[string]any{"workspaceId": ws, "taskId": task}},
		{"reply", "replyToTask", map[string]any{"workspaceId": ws, "taskId": task, "text": "hi"}},
		{"respond", "respondToTask", map[string]any{"workspaceId": ws, "taskId": task, "action": "allow"}},
		{"updateStatus", "updateTaskStatus", map[string]any{"workspaceId": ws, "taskId": task, "status": "ongoing"}},
		{"updateSortingOrder", "updateTaskOrder", map[string]any{"workspaceId": ws, "taskId": task, "sortOrder": 1.5}},
		{"updateAssignee", "updateTaskAssignee", map[string]any{"workspaceId": ws, "taskId": task, "assignee": "agent"}},
		{"updateAllowAll", "updateTaskAllowAll", map[string]any{"workspaceId": ws, "taskId": task, "allowAll": true}},
		{"updateSchedule", "updateScheduledTask", map[string]any{"workspaceId": ws, "taskId": task, "title": "t"}},
		{"delete", "deleteTask", map[string]any{"workspaceId": ws, "taskId": task}},
		{"getAttachment", "getAttachment", map[string]any{"workspaceId": ws, "taskId": task, "attachmentId": "att1"}},
	} {
		t.Run(tc.action, func(t *testing.T) {
			var got mcpevent.MCPEvent
			fps := &fakePubSub{onPublish: func(req pubsub.PublishRequest) { got = req.Event.(mcpevent.MCPEvent) }}
			srv := &WorkspaceServer{crud: &taskCrud{}, pubsub: fps}
			res := toolResult(srv.handleTask(authedContext(), nil, TaskParams{Action: tc.action, Payload: tc.payload}))
			if res.isError {
				t.Fatalf("%s failed: %s", tc.action, res.text)
			}
			if got.ToolName != tc.tool {
				t.Errorf("%s was recorded as %q, want %q", tc.action, got.ToolName, tc.tool)
			}
		})
	}
}

func TestTaskTool_RefusesABadCall(t *testing.T) {
	srv := &WorkspaceServer{crud: &taskCrud{}}
	for _, tc := range []struct {
		action  string
		payload map[string]any
		want    string
	}{
		{"create", map[string]any{"title": "t"}, "create: payload needs workspaceId"},
		{"respond", map[string]any{"workspaceId": "a", "taskId": "b", "decision": "allow"}, `respond: bad payload: unknown field "decision"`},
		{"archive", nil, `unknown action "archive": use list, listAll, create, get, reply, respond, updateStatus, updateSortingOrder, updateAssignee, updateAllowAll, updateSchedule, delete, getAttachment or help`},
	} {
		res := toolResult(srv.handleTask(authedContext(), nil, TaskParams{Action: tc.action, Payload: tc.payload}))
		if !res.isError || res.text != tc.want {
			t.Errorf("%s %v answered %q (error %v), want the error %q", tc.action, tc.payload, res.text, res.isError, tc.want)
		}
	}
}

func TestTaskTool_HelpPrintsEachActionsPayload(t *testing.T) {
	srv := &WorkspaceServer{}
	text := textOf(t, toolResult(srv.handleTask(authedContext(), nil, TaskParams{Action: "help"})))
	for _, a := range srv.taskActions() {
		if !strings.Contains(text, "\n  "+a.Name+" ") {
			t.Errorf("help does not list %s:\n%s", a.Name, text)
		}
	}
	text = textOf(t, toolResult(srv.handleTask(authedContext(), nil, TaskParams{Action: "help", Payload: map[string]any{"topic": "respond"}})))
	for _, want := range []string{
		`Usage: task {"action": "respond", "payload": {"workspaceId": <string>, "taskId": <string>, "action": <string>, ...}}`,
		"  action               string   One of: allow, allow_all, reject, text.",
		"\nOptional:\n  text                 string   With action text, the reply to post.",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("help respond lacks %q:\n%s", want, text)
		}
	}
}

// The enum the schema offers is exactly the actions the tool runs.
func TestTaskTool_EnumIsItsActions(t *testing.T) {
	f, _ := reflect.TypeOf(TaskParams{}).FieldByName("Action")
	var names []string
	for _, a := range (&WorkspaceServer{}).taskActions() {
		names = append(names, a.Name)
	}
	if got := strings.Split(strings.TrimPrefix(f.Tag.Get("jsonschema"), "enum: "), ", "); !slices.Equal(got, append(names, "help")) {
		t.Errorf("TaskParams offers %v, but the tool runs %v and help", got, names)
	}
}

// A call to a retired name still runs, unlisted, as the action it became:
// respondToTask's own action field reaches the handler untouched. A flat call
// has its fields moved into the payload.
func TestTaskTool_OlderCallsStillAnswer(t *testing.T) {
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
	for _, tool := range listed.Tools {
		if _, retired := legacyTaskTools[tool.Name]; retired {
			t.Errorf("the retired %s is still listed", tool.Name)
		}
	}

	ws, task := base62(testWorkspace), base62(testTriggerID)
	for _, tc := range []struct {
		name string
		args map[string]any
	}{
		{"respondToTask", map[string]any{"workspaceId": ws, "taskId": task, "action": "allow"}},
		{"updateTaskOrder", map[string]any{"workspaceId": ws, "taskId": task, "sortOrder": 2}},
		{"task", map[string]any{"action": "get", "workspaceId": ws, "taskId": task}},
	} {
		res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: tc.name, Arguments: tc.args})
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		text := res.Content[0].(*mcp.TextContent).Text
		if res.IsError {
			t.Errorf("%s failed: %s", tc.name, text)
		}
		var body map[string]any
		if err := json.Unmarshal([]byte(text), &body); err != nil {
			t.Errorf("%s answered %q, not the handler's JSON", tc.name, text)
		}
	}
}

// help, and a call naming no action, are recorded as the task tool itself.
func TestTaskTool_RecordsHelpAsItself(t *testing.T) {
	for _, action := range []string{"help", "archive"} {
		var got mcpevent.MCPEvent
		fps := &fakePubSub{onPublish: func(req pubsub.PublishRequest) { got = req.Event.(mcpevent.MCPEvent) }}
		srv := &WorkspaceServer{pubsub: fps}
		srv.handleTask(authedContext(), nil, TaskParams{Action: action})
		if got.ToolName != "task" {
			t.Errorf("%s was recorded as %q, want task", action, got.ToolName)
		}
	}
}
