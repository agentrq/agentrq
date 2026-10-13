// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package acpv1

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("fixture %s not found: %v", name, err)
	}
	return b
}

func TestWireTypes(t *testing.T) {
	t.Run("an initialize answer keeps capabilities, logins and identity", func(t *testing.T) {
		var r initializeResponse
		if err := r.decode(fixture(t, "initialize.response.json")); err != nil {
			t.Fatalf("decode returned %v, want nil", err)
		}
		if r.ProtocolVersion != 1 || !r.AgentCapabilities.LoadSession {
			t.Errorf("got version %d and loadSession %v, want 1 and true", r.ProtocolVersion, r.AgentCapabilities.LoadSession)
		}
		if h := r.AgentCapabilities.MCPCapabilities.HTTP; h == nil || !*h {
			t.Errorf("mcp http was %v, want an explicit true", h)
		}
		if a := r.AgentCapabilities.MCPCapabilities.ACP; a != nil {
			t.Errorf("mcp acp was %v, want unstated (nil)", *a)
		}
		if !supported(r.AgentCapabilities.SessionCapabilities.Close) {
			t.Error("session close read as unsupported, want supported ({} means yes)")
		}
		if supported(r.AgentCapabilities.SessionCapabilities.List) {
			t.Error("session list read as supported, want unsupported (it was not offered)")
		}
		if len(r.AuthMethods) != 2 || r.AuthMethods[1].Type != "terminal" || r.AuthMethods[0].Type != "" {
			t.Errorf("auth methods were %+v, want an agent login then a terminal one", r.AuthMethods)
		}
		if r.AgentInfo == nil || r.AgentInfo.Name != "example-agent" || r.AgentInfo.Version != "0.9.2" {
			t.Errorf("agent info was %+v, want example-agent 0.9.2", r.AgentInfo)
		}
	})

	t.Run("an initialize answer with a garbled optional field still decodes", func(t *testing.T) {
		var r initializeResponse
		err := r.decode([]byte(`{"protocolVersion":1,"agentCapabilities":"oops","authMethods":7,"agentInfo":[1]}`))
		if err != nil {
			t.Fatalf("decode returned %v, want lenient success", err)
		}
		if r.ProtocolVersion != 1 || len(r.AuthMethods) != 0 || r.AgentInfo != nil {
			t.Errorf("got %+v, want version 1 and every garbled field empty", r)
		}
	})

	t.Run("an initialize answer with only a protocol version reads as offering nothing", func(t *testing.T) {
		var r initializeResponse
		if err := r.decode([]byte(`{"protocolVersion":1,"agentCapabilities":null}`)); err != nil {
			t.Fatalf("decode returned %v, want nil", err)
		}
		if r.AgentCapabilities.LoadSession || r.AgentCapabilities.MCPCapabilities.HTTP != nil || r.AuthMethods != nil || r.AgentInfo != nil {
			t.Errorf("got %+v, want no capabilities, no logins and no identity", r)
		}
	})

	t.Run("an initialize answer without a protocol version is refused", func(t *testing.T) {
		var r initializeResponse
		if err := r.decode([]byte(`{"agentCapabilities":{}}`)); err == nil {
			t.Fatal("decode returned nil, want an error naming protocolVersion")
		}
	})

	t.Run("an initialize answer that is not an object is refused", func(t *testing.T) {
		var r initializeResponse
		if err := r.decode([]byte(`"ready"`)); err == nil {
			t.Fatal("decode returned nil, want a decoding error")
		}
	})

	t.Run("AgentRQ declares no file system, no terminal and no terminal logins", func(t *testing.T) {
		b, _ := json.Marshal(initializeRequest{ProtocolVersion: ProtocolVersion, ClientCapabilities: clientCaps, ClientInfo: implementation{Name: "agentrq", Title: "AgentRQ", Version: "0.9.20"}})
		want := `{"protocolVersion":1,"clientCapabilities":{"fs":{"readTextFile":false,"writeTextFile":false},"terminal":false,"elicitation":{"form":{},"url":{}},"plan":{},"auth":{"terminal":false}},"clientInfo":{"name":"agentrq","title":"AgentRQ","version":"0.9.20"}}`
		if string(b) != want {
			t.Errorf("initialize request was\n%s\nwant\n%s", b, want)
		}
	})

	t.Run("a session with no MCP servers sends an empty list, never null", func(t *testing.T) {
		b, _ := json.Marshal(newSessionRequest{Cwd: "/home/me/work"})
		if string(b) != `{"cwd":"/home/me/work","mcpServers":[]}` {
			t.Errorf("session/new request was %s, want mcpServers []", b)
		}
	})

	t.Run("a session with an MCP server sends it with its headers", func(t *testing.T) {
		b, _ := json.Marshal(newSessionRequest{Cwd: "/w", MCPServers: []mcpServerHTTP{{Type: "http", Name: "agentrq-workspace", URL: "https://mcp.example/x", Headers: []httpHeader{{Name: "Authorization", Value: "Bearer t"}}}}})
		want := `{"cwd":"/w","mcpServers":[{"type":"http","name":"agentrq-workspace","url":"https://mcp.example/x","headers":[{"name":"Authorization","value":"Bearer t"}]}]}`
		if string(b) != want {
			t.Errorf("session/new request was\n%s\nwant\n%s", b, want)
		}
	})

	t.Run("session/new answers keep modes and every config option", func(t *testing.T) {
		var r newSessionResponse
		if err := json.Unmarshal(fixture(t, "session_new.response.json"), &r); err != nil {
			t.Fatalf("decode returned %v, want nil", err)
		}
		if r.SessionID != "sess-1" || r.Modes == nil || r.Modes.CurrentModeID != "agent" || len(r.Modes.AvailableModes) != 3 {
			t.Fatalf("got %+v, want session sess-1 in mode agent with 3 modes", r)
		}
		var meta struct {
			Kind string `json:"kind"`
		}
		if err := json.Unmarshal(r.Modes.AvailableModes[1].Meta, &meta); err != nil || meta.Kind != "auto_review" {
			t.Errorf("mode 2's _meta was %s, want kind auto_review", r.Modes.AvailableModes[1].Meta)
		}
		if len(r.ConfigOptions) != 2 {
			t.Fatalf("got %d config options, want 2", len(r.ConfigOptions))
		}
		model := r.ConfigOptions[0]
		if model.ID != "model" || model.Category != "model" || string(model.CurrentValue) != `"model-large"` {
			t.Errorf("first option was %+v, want the model select on model-large", model)
		}
		if want := `[{"value":"model-large","name":"Large"},{"value":"model-small","name":"Small","description":"faster"}]`; string(model.Options) != want {
			t.Errorf("model options were %s, want the raw list %s", model.Options, want)
		}
		opts := model.selectOptions()
		if len(opts) != 2 || opts[1].Name != "Small" || opts[1].Description != "faster" || opts[1].Group != "" {
			t.Errorf("flattened options were %+v, want Large then Small (faster), no group", opts)
		}
	})

	t.Run("each update kind decodes into its own type", func(t *testing.T) {
		updateOf := func(name string) json.RawMessage {
			var n sessionNotification
			if err := json.Unmarshal(fixture(t, name), &n); err != nil || n.SessionID != "sess-1" {
				t.Fatalf("%s decoded as %+v (%v), want session sess-1", name, n, err)
			}
			return n.Update
		}

		u := updateOf("session_update.agent_message_chunk.json")
		var chunk contentChunk
		if kindOf(u) != "agent_message_chunk" || json.Unmarshal(u, &chunk) != nil || chunk.Content.Text != "Hello" {
			t.Errorf("message chunk read as %q %+v, want agent_message_chunk with Hello", kindOf(u), chunk)
		}

		u = updateOf("session_update.tool_call.json")
		var call toolCallUpdate
		if kindOf(u) != "tool_call" || json.Unmarshal(u, &call) != nil || call.ToolCallID != "call-1" ||
			call.Title == nil || *call.Title != "mcp__agentrq-workspace__reply" ||
			call.Status == nil || *call.Status != "completed" || call.Name != nil {
			t.Errorf("tool call read as %q %+v, want completed call-1 titled with the reply tool and no name", kindOf(u), call)
		}
		var input struct {
			ChatID string `json:"chatId"`
		}
		if json.Unmarshal(call.RawInput, &input) != nil || input.ChatID != "0kFbQ0UCE5J" {
			t.Errorf("tool call input was %s, want chatId 0kFbQ0UCE5J", call.RawInput)
		}

		u = updateOf("session_update.usage_update.json")
		var usage usageUpdate
		if kindOf(u) != "usage_update" || json.Unmarshal(u, &usage) != nil || usage.Used != 53000 || usage.Size != 200000 ||
			usage.Cost == nil || usage.Cost.Amount != 0.0042 || usage.Cost.Currency != "USD" {
			t.Errorf("usage read as %q %+v, want 53000 of 200000 at 0.0042 USD", kindOf(u), usage)
		}

		u = updateOf("session_update.plan_update.json")
		var plan planUpdate
		if kindOf(u) != "plan_update" || json.Unmarshal(u, &plan) != nil || plan.Plan.Type != "items" || plan.Plan.PlanID != "p1" ||
			len(plan.Plan.Entries) != 2 || plan.Plan.Entries[1].Status != "in_progress" {
			t.Errorf("plan read as %q %+v, want items plan p1 with its second entry in progress", kindOf(u), plan)
		}
	})

	t.Run("an unknown update kind is reported as unknown, not as an error", func(t *testing.T) {
		if k := kindOf(json.RawMessage(`{"sessionUpdate":"hologram"}`)); k != "hologram" {
			t.Errorf("kind was %q, want hologram", k)
		}
		if k := kindOf(json.RawMessage(`[1,2]`)); k != "" {
			t.Errorf("kind of a garbled update was %q, want empty", k)
		}
	})

	t.Run("a permission request without a title still decodes", func(t *testing.T) {
		var r requestPermissionRequest
		if err := json.Unmarshal(fixture(t, "session_request_permission.request.json"), &r); err != nil {
			t.Fatalf("decode returned %v, want nil", err)
		}
		if r.ToolCall.ToolCallID != "call-2" || r.ToolCall.Title != nil {
			t.Errorf("tool call was %+v, want call-2 with no title", r.ToolCall)
		}
		if len(r.Options) != 3 || r.Options[0].Kind != "allow_always" || r.Options[1].OptionID != "approved" || r.Options[2].Kind != "reject_once" {
			t.Errorf("options were %+v, want always, yes, no in that order", r.Options)
		}
	})

	t.Run("a request-scoped url question keeps its request id as raw JSON", func(t *testing.T) {
		var r createElicitationRequest
		if err := json.Unmarshal(fixture(t, "elicitation_create.url.request.json"), &r); err != nil {
			t.Fatalf("decode returned %v, want nil", err)
		}
		if string(r.RequestID) != "3" || r.SessionID != "" || r.Mode != "url" || r.ElicitationID != "e-1" || r.URL != "https://auth.example/device" {
			t.Errorf("question was %+v, want a url question e-1 for request 3 and no session", r)
		}
	})

	t.Run("a cancelled permission answer has no option id", func(t *testing.T) {
		b, _ := json.Marshal(requestPermissionResponse{Outcome: permissionOutcome{Outcome: "cancelled"}})
		if string(b) != `{"outcome":{"outcome":"cancelled"}}` {
			t.Errorf("answer was %s, want only the cancelled outcome", b)
		}
	})

	t.Run("a select config option with groups is read option by option", func(t *testing.T) {
		o := configOption{Options: json.RawMessage(`[{"group":"g","name":"G","options":[{"value":"a","name":"A"},{"value":"b","name":"B"}]},{"value":"c","name":"C"},{"name":"no value"}]`)}
		got := o.selectOptions()
		if len(got) != 3 {
			t.Fatalf("got %d options, want 3 (two grouped, one plain, one without a value dropped)", len(got))
		}
		if got[0].Group != "G" || string(got[0].Value) != `"a"` || got[1].Group != "G" || got[1].Name != "B" {
			t.Errorf("grouped options were %+v, want A and B in group G", got[:2])
		}
		if got[2].Group != "" || string(got[2].Value) != `"c"` {
			t.Errorf("plain option was %+v, want C with no group", got[2])
		}
	})

	t.Run("a select config option whose options are garbled lists nothing", func(t *testing.T) {
		if got := (configOption{Options: json.RawMessage(`{"value":"a"}`)}).selectOptions(); got != nil {
			t.Errorf("got %+v, want no options", got)
		}
	})
}
