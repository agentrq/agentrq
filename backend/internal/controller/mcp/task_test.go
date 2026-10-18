// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
	mock_pubsub "github.com/agentrq/agentrq/backend/internal/service/mocks/pubsub"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
	"github.com/golang/mock/gomock"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func taskResult(t *testing.T, res *mcp.CallToolResult) (string, bool) {
	t.Helper()
	if res == nil || len(res.Content) == 0 {
		t.Fatal("no result")
	}
	return res.Content[0].(*mcp.TextContent).Text, res.IsError
}

// Each action reaches the handler of the tool it replaced: the refusal each
// answers with is that handler's own, or the payload's when it is not one.
func TestTaskTool_RoutesEachActionToItsHandler(t *testing.T) {
	ps := &WorkspaceServer{}
	for _, tc := range []struct {
		action  string
		payload map[string]any
		want    string
	}{
		{"create", map[string]any{"title": "", "body": ""}, "title is required"},
		{"get", map[string]any{"taskId": "!"}, "invalid taskId format"},
		{"updateStatus", map[string]any{"taskId": "", "status": ""}, "taskId is required"},
		{"reply", map[string]any{"taskId": "", "text": "hi"}, "taskId and text are required"},
		{"elicit", map[string]any{"taskId": "", "message": "", "mode": ""}, "taskId and message are required"},
		{"getAttachment", map[string]any{"taskId": "1", "attachmentId": ""}, "attachmentId is required"},
		{"create", map[string]any{"title": "t"}, "create: payload needs body"},
		{"reply", map[string]any{"chatId": "1", "text": "hi"}, `reply: bad payload: unknown field "chatId"`},
		{"updateStatus", map[string]any{"taskId": 1, "status": "ongoing"}, "updateStatus: bad payload: cannot unmarshal number into Go struct field UpdateTaskStatusParams.taskId of type string"},
		{"list", nil, `unknown action "list": use create, get, updateStatus, reply, elicit, getAttachment or help`},
	} {
		res, _, err := ps.handleTask(context.Background(), nil, TaskParams{Action: tc.action, Payload: tc.payload})
		if err != nil {
			t.Fatal(err)
		}
		if text, isErr := taskResult(t, res); !isErr || text != tc.want {
			t.Errorf("%s %v answered %q (error %v), want the error %q", tc.action, tc.payload, text, isErr, tc.want)
		}
	}
}

func TestTaskTool_Help(t *testing.T) {
	ps := &WorkspaceServer{}
	res, _, _ := ps.handleTask(context.Background(), nil, TaskParams{Action: "help"})
	text, isErr := taskResult(t, res)
	if isErr {
		t.Fatalf("help failed: %s", text)
	}
	for _, a := range ps.taskActions() {
		if !strings.Contains(text, "\n  "+a.Name+" ") {
			t.Errorf("help does not list %s:\n%s", a.Name, text)
		}
	}
	if !strings.HasPrefix(text, `Usage: task {"action": "<action>", "payload": {...}}`) || !strings.Contains(text, `with payload {"topic": "<action>"}`) {
		t.Errorf("help does not say how to call the tool or get an action's payload:\n%s", text)
	}

	res, _, _ = ps.handleTask(context.Background(), nil, TaskParams{Action: "help", Payload: map[string]any{"topic": "elicit"}})
	text, _ = taskResult(t, res)
	for _, want := range []string{
		`Usage: task {"action": "elicit", "payload": {"taskId": <string>, "message": <string>, "mode": <string>, ...}}`,
		"Ask the human a question and wait for the answer",
		"\nRequired:\n  taskId ",
		"\nOptional:\n  requestedSchema      object   Required when mode is 'form'.",
		"  timeoutSeconds       integer  How long to wait",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("help elicit lacks %q:\n%s", want, text)
		}
	}

	for _, tc := range []struct {
		payload map[string]any
		want    string
	}{
		{map[string]any{"topic": "list"}, `task has no action "list": run help for the list`},
		{map[string]any{"action": "create"}, `help: bad payload: unknown field "action"`},
	} {
		res, _, _ = ps.handleTask(context.Background(), nil, TaskParams{Action: "help", Payload: tc.payload})
		if text, isErr := taskResult(t, res); !isErr || text != tc.want {
			t.Errorf("help %v answered %q (error %v), want %q", tc.payload, text, isErr, tc.want)
		}
	}
}

// assertActionsMatchEnum fails when a tool's action enum and its actions
// drift: an action the schema offers that nothing runs, or one it hides.
func assertActionsMatchEnum(t *testing.T, params any, actions []ToolAction) {
	t.Helper()
	f, _ := reflect.TypeOf(params).FieldByName("Action")
	enum, ok := strings.CutPrefix(f.Tag.Get("jsonschema"), "enum: ")
	if !ok {
		t.Fatalf("%T.Action has no enum", params)
	}
	var names []string
	for _, a := range actions {
		names = append(names, a.Name)
	}
	if offered := strings.Split(enum, ", "); !slices.Equal(offered, append(names, "help")) {
		t.Errorf("%T offers actions %v but runs %v and help", params, offered, names)
	}
}

func TestTaskAndPano_EnumsMatchTheirActions(t *testing.T) {
	assertActionsMatchEnum(t, TaskParams{}, (&WorkspaceServer{}).taskActions())
	assertActionsMatchEnum(t, PanoParams{}, PanoActions(nil, "", 0))
}

func TestPano_Help(t *testing.T) {
	res := RunPano(context.Background(), nil, "u", 1, PanoParams{Action: "help", Payload: map[string]any{"topic": "save"}})
	text, isErr := taskResult(t, res)
	if isErr || !strings.Contains(text, `Usage: pano {"action": "save", "payload": {"title": <string>, "markdown": <string>, ...}}`) ||
		!strings.Contains(text, "  color                string   One of: yellow, pink, green, blue, orange, purple, amber.") {
		t.Errorf("pano help save answered (error %v):\n%s", isErr, text)
	}
}

func TestActionHelp_PayloadKinds(t *testing.T) {
	type payload struct {
		F    float64  `json:"f"`
		M    struct{} `json:"m,omitempty"`
		B    bool     `json:"b,omitempty"`
		P    *string  `json:"p,omitempty"`
		Skip string   `json:"-"`
		None string
	}
	noop := func(context.Context, *mcp.CallToolRequest, payload) (*mcp.CallToolResult, any, error) {
		return nil, nil, nil
	}
	text, _ := taskResult(t, ActionHelp("x", []ToolAction{NewToolAction("do", "Does it.", noop)}, "do"))
	for _, want := range []string{`"f": <number>, ...`, "  m                    object", "  b                    boolean", "  p                    string"} {
		if !strings.Contains(text, want) {
			t.Errorf("help lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "Skip") || strings.Contains(text, "None") {
		t.Errorf("help lists a field JSON never carries:\n%s", text)
	}

	// An action that takes no payload says so.
	text, _ = taskResult(t, ActionHelp("x", []ToolAction{{Name: "ping", Summary: "Pings."}}, "ping"))
	if text != "Usage: x {\"action\": \"ping\"}\n\nPings." {
		t.Errorf("help for an action without a payload = %q", text)
	}
}

func TestDecodePayload_RefusesWhatCannotBeJSON(t *testing.T) {
	var out struct{}
	if err := decodePayload(map[string]any{"c": make(chan int)}, &out); err == nil {
		t.Error("a payload that is not JSON was accepted")
	}
}

func connectWorkspace(t *testing.T) *mcp.ClientSession {
	t.Helper()
	ps := NewWorkspaceServer(
		1, 0, "user", "http://localhost",
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		eventbus.New(), nil, nil, "icon", "name", "desc", nil, nil, nil, nil,
	)
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	ss, err := ps.MCPServer().Connect(context.Background(), serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

// The task and pano tools' schemas are the two inputs and nothing else.
func TestTaskTool_SchemaIsActionAndPayload(t *testing.T) {
	cs := connectWorkspace(t)
	listed, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, tool := range listed.Tools {
		if tool.Name != "task" && tool.Name != "pano" {
			continue
		}
		checked++
		raw, _ := json.Marshal(tool.InputSchema)
		var schema struct {
			Properties map[string]any `json:"properties"`
			Required   []string       `json:"required"`
		}
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatal(err)
		}
		var props []string
		for p := range schema.Properties {
			props = append(props, p)
		}
		slices.Sort(props)
		if !slices.Equal(props, []string{"action", "payload"}) || !slices.Equal(schema.Required, []string{"action"}) {
			t.Errorf("%s takes %v (required %v), want action and an optional payload", tool.Name, props, schema.Required)
		}
	}
	if checked != 2 {
		t.Errorf("found %d of the task and pano tools", checked)
	}
}

// The retired names are not listed, but a call to one still runs, as the
// action it became, with reply's chatId read as the taskId; and a flat call,
// fields beside action, has them moved into the payload.
func TestTaskTool_OlderCallsStillAnswer(t *testing.T) {
	cs := connectWorkspace(t)
	listed, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range listed.Tools {
		names = append(names, tool.Name)
	}
	for old := range legacyTools {
		if slices.Contains(names, old) {
			t.Errorf("the retired %s is still listed", old)
		}
	}

	for _, tc := range []struct {
		name string
		args map[string]any
		want string
	}{
		{"updateTaskStatus", map[string]any{"taskId": "", "status": ""}, "taskId is required"},
		{"reply", map[string]any{"chatId": "", "text": "hi"}, "taskId and text are required"},
		{"getAttachment", map[string]any{"taskId": "1", "attachmentId": ""}, "attachmentId is required"},
		{"createTask", nil, "create: payload needs title"},
		{"task", map[string]any{"action": "updateStatus", "taskId": "", "status": ""}, "taskId is required"},
		{"task", map[string]any{"action": "reply", "payload": map[string]any{"taskId": ""}, "text": "hi"}, "taskId and text are required"},
		// Past the schema, which a flat call would fail: this server has no Pano.
		{"pano", map[string]any{"action": "read", "id": "00000000000"}, "the Pano is not available on this server"},
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

// A retired name is rewritten; anything else passes through untouched: a call
// already in the two-input shape, and arguments that are not an object, which
// the SDK refuses.
func TestActionToolsMiddleware_PassesOthersThrough(t *testing.T) {
	var seen []string
	next := func(_ context.Context, method string, req mcp.Request) (mcp.Result, error) {
		if call, ok := req.(*mcp.CallToolRequest); ok {
			seen = append(seen, call.Params.Name+" "+string(call.Params.Arguments))
		}
		return nil, nil
	}
	h := ActionToolsMiddleware(actionToolNames, legacyTools)(next)
	for _, p := range []*mcp.CallToolParamsRaw{
		{Name: "loadMemory", Arguments: json.RawMessage(`{"name":"x"}`)},
		{Name: "help", Arguments: json.RawMessage(`{"action":"task"}`)},
		{Name: "pano", Arguments: json.RawMessage(`{"action":"help"}`)},
		{Name: "reply", Arguments: json.RawMessage(`[1]`)},
		{Name: "elicit"},
	} {
		h(context.Background(), "tools/call", &mcp.CallToolRequest{Params: p})
	}
	h(context.Background(), "tools/list", &mcp.ListToolsRequest{})
	want := []string{`memory {"action":"load","payload":{"name":"x"}}`, `help {"action":"task"}`, `pano {"action":"help"}`, `reply [1]`, `task {"action":"elicit"}`}
	if !slices.Equal(seen, want) {
		t.Errorf("calls reached the server as %q, want %q", seen, want)
	}
}

// help, and a call naming no action, are recorded as the task tool itself;
// an action is recorded by its handler, under the tool it replaced.
func TestTaskTool_RecordsHelpAsItself(t *testing.T) {
	ctrl := gomock.NewController(t)
	pub := mock_pubsub.NewMockService(ctrl)
	var recorded []string
	pub.EXPECT().Publish(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, req pubsub.PublishRequest) (*pubsub.PublishResponse, error) {
		recorded = append(recorded, req.Event.(MCPEvent).ToolName)
		return nil, nil
	}).AnyTimes()
	ps := &WorkspaceServer{pubsub: pub}
	for _, action := range []string{"help", "archive", "updateStatus"} {
		ps.handleTask(context.Background(), nil, TaskParams{Action: action, Payload: map[string]any{"taskId": "", "status": ""}})
	}
	if want := []string{"task", "task", "updateTaskStatus"}; !slices.Equal(recorded, want) {
		t.Errorf("recorded %v, want %v", recorded, want)
	}
}
