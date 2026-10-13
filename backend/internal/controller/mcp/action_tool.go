// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// An action tool takes two inputs, like a command line: the action, and its
// payload, an object whose fields depend on the action. The schema lists
// only those two; each action's payload is its own struct, which decodes the
// payload and is what the help action prints, so the two cannot disagree.

// ToolAction is one action of an action tool.
type ToolAction struct {
	Name    string
	Summary string
	// payload is the type the payload decodes into, nil when it takes none.
	payload reflect.Type
	run     func(ctx context.Context, req *mcp.CallToolRequest, payload map[string]any) (*mcp.CallToolResult, any, error)
}

// NewToolAction is an action whose payload decodes into T, the parameter type of
// the handler that runs it.
func NewToolAction[T any](name, summary string, handler func(context.Context, *mcp.CallToolRequest, T) (*mcp.CallToolResult, any, error)) ToolAction {
	return ToolAction{Name: name, Summary: summary, payload: reflect.TypeFor[T](), run: func(ctx context.Context, req *mcp.CallToolRequest, payload map[string]any) (*mcp.CallToolResult, any, error) {
		var p T
		if err := decodePayload(payload, &p); err != nil {
			return toolError("%s: %v", name, err), nil, nil
		}
		return handler(ctx, req, p)
	}}
}

// RunAction runs one call of the action tool tool: help, or one of actions.
func RunAction(ctx context.Context, req *mcp.CallToolRequest, tool string, actions []ToolAction, action string, payload map[string]any) (*mcp.CallToolResult, any, error) {
	if action == "help" {
		var p struct {
			Topic string `json:"topic,omitempty"`
		}
		if err := decodePayload(payload, &p); err != nil {
			return toolError("help: %v", err), nil, nil
		}
		return ActionHelp(tool, actions, p.Topic), nil, nil
	}
	names := make([]string, 0, len(actions)+1)
	for _, a := range actions {
		if a.Name == action {
			return a.run(ctx, req, payload)
		}
		names = append(names, a.Name)
	}
	return toolError("unknown action %q: use %s or help", action, strings.Join(names, ", ")), nil, nil
}

// IsAction reports whether name is one of actions, which record themselves:
// a call that is not, help or a mistake, is the tool's to record.
func IsAction(actions []ToolAction, name string) bool {
	return slices.ContainsFunc(actions, func(a ToolAction) bool { return a.Name == name })
}

// decodePayload fills out, a struct, from a payload: an unknown field or a
// missing required one (a field without omitempty) is refused, as the
// schema refused them when each action was a tool of its own.
func decodePayload(payload map[string]any, out any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("bad payload: %s", strings.TrimPrefix(err.Error(), "json: "))
	}
	for _, f := range payloadFields(reflect.TypeOf(out).Elem()) {
		if _, ok := payload[f.name]; f.required && !ok {
			return fmt.Errorf("payload needs %s", f.name)
		}
	}
	return nil
}

type payloadField struct {
	name, kind, desc string
	required         bool
}

func payloadFields(t reflect.Type) []payloadField {
	var out []payloadField
	for i := range t.NumField() {
		f := t.Field(i)
		name, opts, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "" || name == "-" {
			continue
		}
		desc := f.Tag.Get("jsonschema")
		if enum, ok := strings.CutPrefix(desc, "enum: "); ok {
			desc = "One of: " + enum + "."
		}
		out = append(out, payloadField{name: name, kind: jsonKind(f.Type), desc: desc, required: !strings.Contains(opts, "omitempty")})
	}
	return out
}

// ActionHelp prints an action tool's help like a command line's: with no
// topic every action and its summary, with one that action's payload.
func ActionHelp(tool string, actions []ToolAction, topic string) *mcp.CallToolResult {
	var b strings.Builder
	if topic == "" {
		fmt.Fprintf(&b, "Usage: %s {\"action\": \"<action>\", \"payload\": {...}}\n\nActions:\n", tool)
		for _, a := range actions {
			fmt.Fprintf(&b, "  %-20s %s\n", a.Name, a.Summary)
		}
		fmt.Fprintf(&b, "  %-20s %s\n", "help", "This list, or with payload {\"topic\": \"<action>\"} that action's payload.")
		return textResult("%s", strings.TrimRight(b.String(), "\n"))
	}
	for _, a := range actions {
		if a.Name != topic {
			continue
		}
		var fields []payloadField
		if a.payload != nil {
			fields = payloadFields(a.payload)
		}
		var usage []string
		for _, f := range fields {
			if f.required {
				usage = append(usage, fmt.Sprintf("%q: <%s>", f.name, f.kind))
			}
		}
		if len(usage) < len(fields) {
			usage = append(usage, "...")
		}
		if len(fields) == 0 {
			fmt.Fprintf(&b, "Usage: %s {\"action\": %q}\n\n%s", tool, a.Name, a.Summary)
			return textResult("%s", b.String())
		}
		fmt.Fprintf(&b, "Usage: %s {\"action\": %q, \"payload\": {%s}}\n\n%s\n", tool, a.Name, strings.Join(usage, ", "), a.Summary)
		for _, required := range []bool{true, false} {
			title := map[bool]string{true: "Required", false: "Optional"}[required]
			wrote := false
			for _, f := range fields {
				if f.required != required {
					continue
				}
				if !wrote {
					fmt.Fprintf(&b, "\n%s:\n", title)
					wrote = true
				}
				fmt.Fprintf(&b, "  %-20s %-8s %s\n", f.name, f.kind, f.desc)
			}
		}
		return textResult("%s", strings.TrimRight(b.String(), "\n"))
	}
	return toolError("%s has no action %q: run help for the list", tool, topic)
}

func jsonKind(t reflect.Type) string {
	switch t.Kind() {
	case reflect.String:
		return "string"
	case reflect.Bool:
		return "boolean"
	case reflect.Int, reflect.Int64:
		return "integer"
	case reflect.Float64:
		return "number"
	case reflect.Slice:
		return "array"
	}
	return "object"
}

// ActionToolsMiddleware keeps two kinds of older call working. A call to a
// retired tool name runs as the action it became, its arguments the payload.
// And a call to an action tool that puts the payload's fields beside action,
// as the pano tool once took them, has them moved into the payload.
func ActionToolsMiddleware(actionTools []string, legacy map[string]LegacyTool) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			call, ok := req.(*mcp.CallToolRequest)
			if !ok || method != "tools/call" || call.Params == nil {
				return next(ctx, method, req)
			}
			l, retired := legacy[call.Params.Name]
			if !retired && !slices.Contains(actionTools, call.Params.Name) {
				return next(ctx, method, req)
			}
			var args map[string]any
			if len(call.Params.Arguments) > 0 {
				if err := json.Unmarshal(call.Params.Arguments, &args); err != nil {
					return next(ctx, method, req)
				}
			}
			if retired {
				for from, to := range l.Renamed {
					if v, ok := args[from]; ok {
						delete(args, from)
						args[to] = v
					}
				}
				payload := args
				args = map[string]any{"action": l.Action}
				if len(payload) > 0 {
					args["payload"] = payload
				}
				call.Params.Name = l.Tool
			} else {
				payload, _ := args["payload"].(map[string]any)
				moved := false
				for k, v := range args {
					if k == "action" || k == "payload" {
						continue
					}
					if payload == nil {
						payload = map[string]any{}
					}
					payload[k] = v
					delete(args, k)
					moved = true
				}
				if !moved {
					return next(ctx, method, req)
				}
				args["payload"] = payload
			}
			// Decoded from JSON a moment ago, so it encodes again.
			call.Params.Arguments, _ = json.Marshal(args)
			return next(ctx, method, req)
		}
	}
}

// LegacyTool is a retired tool name that a tools/call may still use: the tool
// and action it became, and the arguments it renamed (old name → new).
type LegacyTool struct {
	Tool    string
	Action  string
	Renamed map[string]string
}
