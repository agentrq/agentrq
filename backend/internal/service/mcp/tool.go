// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/agentrq/agentrq/backend/internal/service/mcp/jsonrpc"
	"github.com/google/jsonschema-go/jsonschema"
	zlog "github.com/rs/zerolog/log"
)

// ToolHandlerFor handles a tool's calls with its arguments decoded into In.
// An error it returns is the tool's failure, reported to the model in the
// result, not a protocol error; a *jsonrpc.Error is a protocol error.
//
// A non-any Out is the tool's structured output.
type ToolHandlerFor[In, Out any] func(ctx context.Context, req *CallToolRequest, in In) (*CallToolResult, Out, error)

// AddTool adds a tool whose arguments are validated against its input
// schema before h runs. With no schema given, it is inferred from In, and
// an Out other than any has its output schema inferred.
func AddTool[In, Out any](s *Server, t *Tool, h ToolHandlerFor[In, Out]) {
	tt, th, err := typedTool(t, h)
	if err != nil {
		panic(fmt.Sprintf("AddTool: tool %q: %v", t.Name, err))
	}
	s.AddTool(tt, th)
}

func typedTool[In, Out any](t *Tool, h ToolHandlerFor[In, Out]) (*Tool, ToolHandler, error) {
	tt := *t
	if reflect.TypeFor[In]() == reflect.TypeFor[any]() && t.InputSchema == nil {
		tt.InputSchema = &jsonschema.Schema{Type: "object"}
	}
	input, _, err := resolveSchema[In](&tt.InputSchema)
	if err != nil {
		return nil, nil, fmt.Errorf("input schema: %w", err)
	}
	var (
		output   *jsonschema.Resolved
		elemZero any
	)
	if t.OutputSchema != nil || reflect.TypeFor[Out]() != reflect.TypeFor[any]() {
		output, elemZero, err = resolveSchema[Out](&tt.OutputSchema)
		if err != nil {
			return nil, nil, fmt.Errorf("output schema: %w", err)
		}
	}

	handler := func(ctx context.Context, req *CallToolRequest) (*CallToolResult, error) {
		args, err := applySchema(req.Params.Arguments, input, false)
		if err != nil {
			res := &CallToolResult{}
			res.SetError(fmt.Errorf("validating \"arguments\": %v", err))
			return res, nil
		}
		var in In
		if args != nil {
			if err := json.Unmarshal(args, &in); err != nil {
				res := &CallToolResult{}
				res.SetError(err)
				return res, nil
			}
		}
		res, out, err := h(ctx, req, in)
		if err != nil {
			if wire, ok := err.(*jsonrpc.Error); ok {
				return nil, wire
			}
			res := &CallToolResult{}
			res.SetError(err)
			return res, nil
		}
		if res == nil {
			res = &CallToolResult{}
		}
		if output == nil {
			return res, nil
		}
		var outval any = out
		if elemZero != nil {
			var zero Out
			if any(out) == any(zero) {
				outval = elemZero
			}
		}
		data, err := json.Marshal(outval)
		if err != nil {
			return nil, fmt.Errorf("marshaling output: %w", err)
		}
		structured, err := applySchema(data, output, true)
		if err != nil {
			return nil, fmt.Errorf("validating tool output: %w", err)
		}
		res.StructuredContent = json.RawMessage(structured)
		if res.Content == nil {
			res.Content = []Content{&TextContent{Text: string(structured)}}
		}
		return res, nil
	}
	return &tt, handler, nil
}

// resolveSchema resolves the schema in field, inferring it from T when
// none was given. For a pointer T it also returns the zero of what it
// points at, which stands in for a nil output.
func resolveSchema[T any](field *any) (*jsonschema.Resolved, any, error) {
	rt := reflect.TypeFor[T]()
	var zero any
	if rt.Kind() == reflect.Pointer {
		rt = rt.Elem()
		zero = reflect.Zero(rt).Interface()
	}
	var schema *jsonschema.Schema
	switch given := (*field).(type) {
	case nil:
		inferred, err := jsonschema.ForType(rt, &jsonschema.ForOptions{})
		if err != nil {
			return nil, nil, err
		}
		*field = inferred
		schema = inferred
	case *jsonschema.Schema:
		schema = given
		zero = nil
	default:
		if !remarshal(given, &schema) {
			return nil, nil, fmt.Errorf("schema of type %T is not a JSON schema", given)
		}
		zero = nil
	}
	if schema == nil {
		return nil, nil, fmt.Errorf("schema is nil for type %v", rt)
	}
	resolved, err := schema.Resolve(&jsonschema.ResolveOptions{ValidateDefaults: true})
	if err != nil {
		return nil, nil, err
	}
	return resolved, zero, nil
}

// applySchema validates data against resolved after filling its defaults,
// returning the data with the defaults in. Tool input must be an object;
// output may be any JSON.
func applySchema(data json.RawMessage, resolved *jsonschema.Resolved, output bool) (json.RawMessage, error) {
	if resolved == nil {
		return data, nil
	}
	var value any
	if !output {
		args := map[string]any{}
		if len(data) > 0 {
			if err := json.Unmarshal(data, &args); err != nil {
				return nil, fmt.Errorf("unmarshaling arguments: %w", err)
			}
			if args == nil {
				args = map[string]any{}
			}
		}
		value = args
	} else if len(data) > 0 {
		if err := json.Unmarshal(data, &value); err != nil {
			return nil, fmt.Errorf("unmarshaling output: %w", err)
		}
	}
	defaulted := false
	if _, ok := value.(map[string]any); ok {
		if err := resolved.ApplyDefaults(&value); err != nil {
			return nil, fmt.Errorf("applying schema defaults:\n%w", err)
		}
		defaulted = true
	} else if output && value == nil && resolved.Schema().Type == "object" {
		value = map[string]any{}
		if err := resolved.ApplyDefaults(&value); err != nil {
			return nil, fmt.Errorf("applying schema defaults:\n%w", err)
		}
		defaulted = true
	}
	if err := resolved.Validate(&value); err != nil {
		return nil, err
	}
	if !defaulted {
		return data, nil
	}
	// Decoded JSON with defaults that were JSON too: it always encodes.
	out, _ := json.Marshal(value)
	return out, nil
}

// checkToolInputSchema refuses a tool whose input is not an object schema,
// which is all the protocol allows, and logs a name clients may refuse.
func checkToolInputSchema(t *Tool) {
	if err := validateToolName(t.Name); err != nil {
		zlog.Error().Err(err).Str("tool", t.Name).Msg("mcp: invalid tool name")
	}
	if t.InputSchema == nil {
		panic(fmt.Errorf("AddTool %q: missing input schema", t.Name))
	}
	if s, ok := t.InputSchema.(*jsonschema.Schema); ok {
		if s == nil || s.Type != "object" {
			panic(fmt.Errorf(`AddTool %q: input schema must have type "object"`, t.Name))
		}
		return
	}
	var m map[string]any
	if !remarshal(t.InputSchema, &m) || m["type"] != "object" {
		panic(fmt.Errorf(`AddTool %q: input schema must have type "object"`, t.Name))
	}
}

// validateToolName reports a name outside the characters and length clients
// accept.
func validateToolName(name string) error {
	if name == "" {
		return fmt.Errorf("tool name cannot be empty")
	}
	if len(name) > 128 {
		return fmt.Errorf("tool name exceeds maximum length of 128 characters (current: %d)", len(name))
	}
	var bad []string
	seen := map[rune]bool{}
	for _, r := range name {
		ok := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.'
		if !ok && !seen[r] {
			seen[r] = true
			bad = append(bad, fmt.Sprintf("%q", string(r)))
		}
	}
	if len(bad) > 0 {
		return fmt.Errorf("tool name contains invalid characters: %s", strings.Join(bad, ", "))
	}
	return nil
}
