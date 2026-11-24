// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/service/mcp/jsonrpc"
	"github.com/google/jsonschema-go/jsonschema"
)

// mustPanic runs f and checks it panics with a message containing want.
func mustPanic(t *testing.T, want string, f func()) {
	t.Helper()
	defer func() {
		t.Helper()
		r := recover()
		if r == nil {
			t.Fatalf("no panic, want one about %q", want)
		}
		if msg := fmt.Sprint(r); !strings.Contains(msg, want) {
			t.Fatalf("panicked with %q, want it to mention %q", msg, want)
		}
	}()
	f()
}

// callTyped runs the handler of the tool named name with raw arguments.
func callTyped(t *testing.T, s *Server, name, args string) (*CallToolResult, error) {
	t.Helper()
	s.mu.Lock()
	st := s.tools[name]
	s.mu.Unlock()
	var raw json.RawMessage
	if args != "" {
		raw = json.RawMessage(args)
	}
	return st.handler(context.Background(), &CallToolRequest{Params: &CallToolParamsRaw{Name: name, Arguments: raw}})
}

func resultText(r *CallToolResult) string {
	var sb strings.Builder
	for _, c := range r.Content {
		sb.WriteString(c.(*TextContent).Text)
	}
	return sb.String()
}

type greetArgs struct {
	Name  string `json:"name" jsonschema:"who to greet"`
	Times int    `json:"times,omitempty"`
}

// A typed tool's arguments are checked against the schema inferred from its
// input type before the handler runs, and a refusal is the tool's failure,
// not a protocol error.
func TestTypedToolValidatesArguments(t *testing.T) {
	s := NewServer(&Implementation{Name: "t", Version: "1"}, nil)
	var got greetArgs
	AddTool(s, &Tool{Name: "greet"}, func(_ context.Context, _ *CallToolRequest, in greetArgs) (*CallToolResult, any, error) {
		got = in
		return &CallToolResult{Content: []Content{&TextContent{Text: "hi " + in.Name}}}, nil, nil
	})

	res, err := callTyped(t, s, "greet", `{"name":"ada","times":2}`)
	if err != nil || res.IsError || resultText(res) != "hi ada" || got.Times != 2 {
		t.Fatalf("valid call = %+v, %v (handler saw %+v)", res, err, got)
	}
	for args, want := range map[string]string{
		`{"name":5}`:               `validating "arguments"`,
		`{"name":"a","x":1}`:       "unexpected additional properties",
		`[1]`:                      "unmarshaling arguments",
		`"text"`:                   "unmarshaling arguments",
		`{"name":"a","times":"x"}`: "validating",
	} {
		res, err := callTyped(t, s, "greet", args)
		if err != nil || !res.IsError || !strings.Contains(resultText(res), want) {
			t.Errorf("arguments %s = %+v, %v, want a tool error about %q", args, res, err, want)
		}
	}
	// No arguments, and null ones, are an empty object: valid when nothing is
	// required, and refused when something is.
	type optional struct {
		X string `json:"x,omitempty"`
	}
	var seen *optional
	AddTool(s, &Tool{Name: "opt"}, func(_ context.Context, _ *CallToolRequest, in optional) (*CallToolResult, any, error) {
		seen = &in
		return nil, nil, nil
	})
	for _, args := range []string{"", "null"} {
		seen = nil
		if res, err := callTyped(t, s, "opt", args); err != nil || res.IsError || seen == nil || seen.X != "" {
			t.Errorf("arguments %q = %+v, %v (handler saw %+v)", args, res, err, seen)
		}
		if res, err := callTyped(t, s, "greet", args); err != nil || !res.IsError || !strings.Contains(resultText(res), "required") {
			t.Errorf("arguments %q to a tool with a required field = %+v, %v", args, res, err)
		}
	}
}

// A schema given as a map is honoured as given; arguments it lets through
// that do not fit the input type fail as the tool's error.
func TestTypedToolWithGivenSchema(t *testing.T) {
	s := NewServer(&Implementation{Name: "t", Version: "1"}, nil)
	AddTool(s, &Tool{Name: "n", InputSchema: map[string]any{"type": "object"}}, func(context.Context, *CallToolRequest, struct {
		N int `json:"n"`
	}) (*CallToolResult, any, error) {
		return nil, nil, nil
	})
	res, err := callTyped(t, s, "n", `{"n":"not a number"}`)
	if err != nil || !res.IsError {
		t.Fatalf("mistyped argument = %+v, %v, want a tool error", res, err)
	}
	res, err = callTyped(t, s, "n", `{"n":1}`)
	if err != nil || res.IsError || res.Content != nil {
		t.Fatalf("a handler returning nothing = %+v, %v, want an empty result", res, err)
	}
}

// Defaults the schema gives are filled in before the handler sees the
// arguments.
func TestTypedToolAppliesDefaults(t *testing.T) {
	s := NewServer(&Implementation{Name: "t", Version: "1"}, nil)
	schema := &jsonschema.Schema{Type: "object", Properties: map[string]*jsonschema.Schema{
		"mode": {Type: "string", Default: json.RawMessage(`"fast"`)},
	}}
	var seen string
	AddTool(s, &Tool{Name: "d", InputSchema: schema}, func(_ context.Context, _ *CallToolRequest, in map[string]any) (*CallToolResult, any, error) {
		seen, _ = in["mode"].(string)
		return nil, nil, nil
	})
	if _, err := callTyped(t, s, "d", `{}`); err != nil || seen != "fast" {
		t.Fatalf("default not applied: saw %q, %v", seen, err)
	}
}

// An input of type any takes any object.
func TestTypedToolWithAnyInput(t *testing.T) {
	s := NewServer(&Implementation{Name: "t", Version: "1"}, nil)
	AddTool(s, &Tool{Name: "any"}, func(_ context.Context, _ *CallToolRequest, in any) (*CallToolResult, any, error) {
		return nil, nil, nil
	})
	if s.tools["any"].tool.InputSchema.(*jsonschema.Schema).Type != "object" {
		t.Errorf("schema = %+v, want an empty object schema", s.tools["any"].tool.InputSchema)
	}
	if res, err := callTyped(t, s, "any", `{"whatever":[1,2]}`); err != nil || res.IsError {
		t.Fatalf("any object = %+v, %v", res, err)
	}
}

// A handler's error is the tool's failure, except a JSON-RPC error, which is
// the protocol's.
func TestTypedToolErrors(t *testing.T) {
	s := NewServer(&Implementation{Name: "t", Version: "1"}, nil)
	AddTool(s, &Tool{Name: "fail"}, func(context.Context, *CallToolRequest, struct{}) (*CallToolResult, any, error) {
		return nil, nil, errors.New("disk full")
	})
	AddTool(s, &Tool{Name: "refuse"}, func(context.Context, *CallToolRequest, struct{}) (*CallToolResult, any, error) {
		return nil, nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: "bad"}
	})
	res, err := callTyped(t, s, "fail", `{}`)
	if err != nil || !res.IsError || resultText(res) != "disk full" {
		t.Errorf("failure = %+v, %v", res, err)
	}
	res, err = callTyped(t, s, "refuse", `{}`)
	var wire *jsonrpc.Error
	if res != nil || !errors.As(err, &wire) || wire.Code != jsonrpc.CodeInvalidParams {
		t.Errorf("refusal = %+v, %v, want the JSON-RPC error", res, err)
	}
}

type report struct {
	Count int    `json:"count"`
	Note  string `json:"note,omitempty"`
}

// A typed output is the result's structured content, and its JSON the text
// when the handler gave none.
func TestTypedToolOutput(t *testing.T) {
	s := NewServer(&Implementation{Name: "t", Version: "1"}, nil)
	AddTool(s, &Tool{Name: "plain"}, func(context.Context, *CallToolRequest, struct{}) (*CallToolResult, report, error) {
		return nil, report{Count: 3}, nil
	})
	AddTool(s, &Tool{Name: "worded"}, func(context.Context, *CallToolRequest, struct{}) (*CallToolResult, report, error) {
		return &CallToolResult{Content: []Content{&TextContent{Text: "three"}}}, report{Count: 3}, nil
	})
	AddTool(s, &Tool{Name: "ptr"}, func(context.Context, *CallToolRequest, struct{}) (*CallToolResult, *report, error) {
		return nil, nil, nil
	})
	AddTool(s, &Tool{Name: "list"}, func(context.Context, *CallToolRequest, struct{}) (*CallToolResult, []string, error) {
		return nil, []string{"a"}, nil
	})
	if s.tools["plain"].tool.OutputSchema == nil {
		t.Error("no output schema was inferred")
	}

	res, _ := callTyped(t, s, "plain", `{}`)
	if string(res.StructuredContent.(json.RawMessage)) != `{"count":3}` || resultText(res) != `{"count":3}` {
		t.Errorf("plain output = %s / %q", res.StructuredContent, resultText(res))
	}
	res, _ = callTyped(t, s, "worded", `{}`)
	if resultText(res) != "three" {
		t.Errorf("given content was replaced: %q", resultText(res))
	}
	res, _ = callTyped(t, s, "ptr", `{}`)
	if string(res.StructuredContent.(json.RawMessage)) != `{"count":0}` {
		t.Errorf("a nil pointer output = %s, want its zero value", res.StructuredContent)
	}
	res, _ = callTyped(t, s, "list", `{}`)
	if string(res.StructuredContent.(json.RawMessage)) != `["a"]` {
		t.Errorf("a list output = %s", res.StructuredContent)
	}
}

// An output the schema refuses, or one JSON cannot hold, is a protocol
// error: the tool broke its own contract.
func TestTypedToolBadOutput(t *testing.T) {
	s := NewServer(&Implementation{Name: "t", Version: "1"}, nil)
	required := &jsonschema.Schema{Type: "object", Required: []string{"x"}}
	AddTool(s, &Tool{Name: "missing", OutputSchema: required}, func(context.Context, *CallToolRequest, struct{}) (*CallToolResult, any, error) {
		return nil, map[string]any{}, nil
	})
	AddTool(s, &Tool{Name: "nothing", OutputSchema: &jsonschema.Schema{Type: "object"}}, func(context.Context, *CallToolRequest, struct{}) (*CallToolResult, any, error) {
		return nil, nil, nil
	})
	AddTool(s, &Tool{Name: "chan"}, func(context.Context, *CallToolRequest, struct{}) (*CallToolResult, map[string]any, error) {
		return nil, map[string]any{"c": make(chan int)}, nil
	})
	if _, err := callTyped(t, s, "missing", `{}`); err == nil || !strings.Contains(err.Error(), "validating tool output") {
		t.Errorf("an output missing a required field = %v", err)
	}
	if res, err := callTyped(t, s, "nothing", `{}`); err != nil || string(res.StructuredContent.(json.RawMessage)) != `{}` {
		t.Errorf("no output for an object schema = %+v, %v, want {}", res, err)
	}
	if _, err := callTyped(t, s, "chan", `{}`); err == nil || !strings.Contains(err.Error(), "marshaling output") {
		t.Errorf("an output JSON cannot hold = %v", err)
	}
}

// A tool whose schema cannot be had is refused when it is added.
func TestAddToolRefusesBadSchemas(t *testing.T) {
	s := NewServer(&Implementation{Name: "t", Version: "1"}, nil)
	noop := func(context.Context, *CallToolRequest, any) (*CallToolResult, any, error) { return nil, nil, nil }
	mustPanic(t, "input schema", func() {
		AddTool(s, &Tool{Name: "f"}, func(context.Context, *CallToolRequest, func()) (*CallToolResult, any, error) { return nil, nil, nil })
	})
	mustPanic(t, "not a JSON schema", func() { AddTool(s, &Tool{Name: "c", InputSchema: make(chan int)}, noop) })
	mustPanic(t, "schema is nil", func() { AddTool(s, &Tool{Name: "n", InputSchema: (*jsonschema.Schema)(nil)}, noop) })
	mustPanic(t, "schema is nil", func() { AddTool(s, &Tool{Name: "j", InputSchema: json.RawMessage("null")}, noop) })
	mustPanic(t, "input schema", func() {
		AddTool(s, &Tool{Name: "r", InputSchema: &jsonschema.Schema{Type: "object", Ref: "#/nowhere"}}, noop)
	})
	mustPanic(t, "output schema", func() {
		AddTool(s, &Tool{Name: "o"}, func(context.Context, *CallToolRequest, struct{}) (*CallToolResult, func(), error) {
			return nil, nil, nil
		})
	})
	mustPanic(t, `must have type "object"`, func() { AddTool(s, &Tool{Name: "s", InputSchema: map[string]any{"type": "string"}}, noop) })
}

// A raw tool must have an object input schema, in whatever form it is given.
func TestCheckToolInputSchema(t *testing.T) {
	mustPanic(t, "missing input schema", func() { checkToolInputSchema(&Tool{Name: "a"}) })
	mustPanic(t, `must have type "object"`, func() { checkToolInputSchema(&Tool{Name: "a", InputSchema: &jsonschema.Schema{Type: "string"}}) })
	mustPanic(t, `must have type "object"`, func() { checkToolInputSchema(&Tool{Name: "a", InputSchema: (*jsonschema.Schema)(nil)}) })
	mustPanic(t, `must have type "object"`, func() { checkToolInputSchema(&Tool{Name: "a", InputSchema: map[string]any{"type": "array"}}) })
	mustPanic(t, `must have type "object"`, func() { checkToolInputSchema(&Tool{Name: "a", InputSchema: make(chan int)}) })
	checkToolInputSchema(&Tool{Name: "a", InputSchema: map[string]any{"type": "object"}})
	checkToolInputSchema(&Tool{Name: "bad name!", InputSchema: &jsonschema.Schema{Type: "object"}}) // logged, not refused
}

// A tool name is 1 to 128 letters, digits, '_', '-' and '.'; each bad
// character is named once, in order.
func TestValidateToolName(t *testing.T) {
	if err := validateToolName("task.get_v-2"); err != nil {
		t.Errorf("a valid name was refused: %v", err)
	}
	if err := validateToolName(""); err == nil {
		t.Error("an empty name was accepted")
	}
	if err := validateToolName(strings.Repeat("a", 129)); err == nil || !strings.Contains(err.Error(), "129") {
		t.Errorf("a long name = %v", err)
	}
	if err := validateToolName("a b!c d"); err == nil || err.Error() != `tool name contains invalid characters: " ", "!"` {
		t.Errorf("a name with spaces = %v", err)
	}
}

// applySchema leaves data alone with no schema, and reports output that is
// not JSON.
func TestApplySchemaEdges(t *testing.T) {
	if got, err := applySchema(json.RawMessage(`{"a":1}`), nil, false); err != nil || string(got) != `{"a":1}` {
		t.Errorf("no schema = %s, %v", got, err)
	}
	resolved, err := (&jsonschema.Schema{}).Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := applySchema(json.RawMessage(`{`), resolved, true); err == nil || !strings.Contains(err.Error(), "unmarshaling output") {
		t.Errorf("malformed output = %v", err)
	}
}

// A default that is not JSON fails when it is applied: on input, and on an
// output the schema says is an object. Only a schema resolved without
// checking its defaults can carry one; AddTool always checks them.
func TestApplySchemaBadDefault(t *testing.T) {
	schema := &jsonschema.Schema{Type: "object", Properties: map[string]*jsonschema.Schema{
		"a": {Default: json.RawMessage(`{bad`)},
	}}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		t.Skipf("the schema library refuses the default when resolving: %v", err)
	}
	if _, err := applySchema(json.RawMessage(`{}`), resolved, false); err == nil || !strings.Contains(err.Error(), "applying schema defaults") {
		t.Errorf("input with a broken default = %v", err)
	}
	if _, err := applySchema(nil, resolved, true); err == nil || !strings.Contains(err.Error(), "applying schema defaults") {
		t.Errorf("output with a broken default = %v", err)
	}
}
