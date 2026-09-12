// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/mustafaturan/monoflake"

	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
)

// The notification this file handles used to be rejected outright — the SDK
// answered "JSON RPC not handled" and the gateway logged a transport error at
// the user. These tests pin both halves of the fix: that the payload the
// gateways actually send is understood, and that a snapshot never outlives the
// session that reported it.

func newModelsServer() *WorkspaceServer {
	// A bus, because recording a change now announces it: a server without one
	// panics on the first report rather than failing an assertion, which is a
	// confusing way to learn that publishing exists.
	return &WorkspaceServer{
		workspaceID: 100,
		bus:         eventbus.New(),
		agentModels: make(map[string]AgentModelsSnapshot),
	}
}

// The exact frame acp-gateway puts on the wire (src/acpClient.ts), so a rename
// on either side fails here rather than in production.
const gatewayModelsFrame = `{
  "jsonrpc": "2.0",
  "method": "notifications/claude/channel/models",
  "params": {
    "task_id": "0iF1U1qQwS1",
    "session_id": "01a07404-01a7-7033-a58f-47b119036378",
    "config_id": "model",
    "current_model": "gemini-2.5-pro",
    "models": [
      {"id": "gemini-2.5-pro", "name": "Gemini 2.5 Pro", "description": "Most capable", "current": true, "group": "Google"},
      {"id": "gemini-2.5-flash", "name": "Gemini 2.5 Flash"}
    ]
  }
}`

func TestAgentModelsParamsMatchesTheGatewayWireFormat(t *testing.T) {
	var frame struct {
		Method string            `json:"method"`
		Params AgentModelsParams `json:"params"`
	}
	if err := json.Unmarshal([]byte(gatewayModelsFrame), &frame); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if frame.Method != AgentModelsNotificationMethod {
		t.Errorf("method = %q, want %q", frame.Method, AgentModelsNotificationMethod)
	}

	p := frame.Params
	if p.TaskID != "0iF1U1qQwS1" {
		t.Errorf("task_id = %q", p.TaskID)
	}
	if p.SessionID != "01a07404-01a7-7033-a58f-47b119036378" {
		t.Errorf("session_id = %q", p.SessionID)
	}
	if p.ConfigID != "model" {
		t.Errorf("config_id = %q", p.ConfigID)
	}
	if p.CurrentModel != "gemini-2.5-pro" {
		t.Errorf("current_model = %q", p.CurrentModel)
	}
	if len(p.Models) != 2 {
		t.Fatalf("models = %d, want 2", len(p.Models))
	}
	want := AgentModel{
		ID: "gemini-2.5-pro", Name: "Gemini 2.5 Pro",
		Description: "Most capable", Current: true, Group: "Google",
	}
	if !reflect.DeepEqual(p.Models[0], want) {
		t.Errorf("models[0] = %+v, want %+v", p.Models[0], want)
	}
	if p.Models[1].Description != "" || p.Models[1].Current || p.Models[1].Group != "" {
		t.Errorf("optional fields should stay zero when absent: %+v", p.Models[1])
	}
}

func TestHandleCustomNotificationRoutesModels(t *testing.T) {
	ps := newModelsServer()
	ps.HandleCustomNotification(context.Background(), "transport-session", []byte(gatewayModelsFrame))

	// Keyed by the MCP transport session — the namespace liveSessionIDs
	// reports — and not by the ACP session the payload names.
	got, ok := ps.agentModels["transport-session"]
	if !ok {
		t.Fatalf("models were not recorded; map = %+v", ps.agentModels)
	}
	if _, wrong := ps.agentModels["01a07404-01a7-7033-a58f-47b119036378"]; wrong {
		t.Error("keyed by the payload's ACP session, which liveSessionIDs never reports")
	}
	if len(got.Models) != 2 || got.ConfigID != "model" {
		t.Errorf("recorded = %+v", got)
	}
	if got.SessionID != "01a07404-01a7-7033-a58f-47b119036378" {
		t.Errorf("the agent's session should be kept on the snapshot, got %q", got.SessionID)
	}
}

func TestHandleAgentModels(t *testing.T) {
	t.Run("records what the session reported", func(t *testing.T) {
		ps := newModelsServer()
		ps.HandleAgentModels(context.Background(), "sess-1", AgentModelsParams{
			ConfigID:     "model",
			CurrentModel: "b",
			Models:       []AgentModel{{ID: "a"}, {ID: "b"}},
		})

		got := ps.agentModels["sess-1"]
		if got.ConfigID != "model" || got.CurrentModel != "b" || len(got.Models) != 2 {
			t.Errorf("recorded = %+v", got)
		}
	})

	t.Run("keys on the transport session, not the agent's", func(t *testing.T) {
		// The payload's session_id is the gateway's ACP session; the key has to
		// be the MCP transport session, because that is the namespace
		// liveSessionIDs reports and therefore the one the reader filters on.
		// Keying on the payload would file every snapshot under an ID that
		// never appears live, and none would ever be advertised.
		ps := newModelsServer()
		ps.HandleAgentModels(context.Background(), "mcp-session", AgentModelsParams{
			SessionID: "acp-session",
			Models:    []AgentModel{{ID: "a"}},
		})

		if _, ok := ps.agentModels["mcp-session"]; !ok {
			t.Errorf("expected the transport session as the key, got %+v", ps.agentModels)
		}
		if _, ok := ps.agentModels["acp-session"]; ok {
			t.Error("must not key on the agent's own session id")
		}
		if got := ps.agentModels["mcp-session"].SessionID; got != "acp-session" {
			t.Errorf("the agent's session should still be recorded on the snapshot, got %q", got)
		}
	})

	t.Run("records against a blank transport session rather than dropping it", func(t *testing.T) {
		// Some transports leave the session ID empty. The reader filters
		// against the same list this is keyed on, so "" is internally
		// consistent — and dropping it would lose the models outright.
		ps := newModelsServer()
		ps.HandleAgentModels(context.Background(), "", AgentModelsParams{Models: []AgentModel{{ID: "a"}}})

		if _, ok := ps.agentModels[""]; !ok {
			t.Errorf("expected the models recorded under the blank key, got %+v", ps.agentModels)
		}
		if got := pickAgentModels(ps.agentModels, []string{""}); got == nil {
			t.Error("a blank-keyed snapshot must still be readable for a blank live session")
		}
	})

	t.Run("the newest report replaces the previous one", func(t *testing.T) {
		ps := newModelsServer()
		ps.HandleAgentModels(context.Background(), "sess-1", AgentModelsParams{Models: []AgentModel{{ID: "a"}, {ID: "b"}}})
		ps.HandleAgentModels(context.Background(), "sess-1", AgentModelsParams{Models: []AgentModel{{ID: "c"}}})

		got := ps.agentModels["sess-1"]
		if len(got.Models) != 1 || got.Models[0].ID != "c" {
			t.Errorf("expected the second report to win, got %+v", got)
		}
	})

	t.Run("an empty list is recorded, because it withdraws the choice", func(t *testing.T) {
		ps := newModelsServer()
		ps.HandleAgentModels(context.Background(), "sess-1", AgentModelsParams{Models: []AgentModel{{ID: "a"}}})
		ps.HandleAgentModels(context.Background(), "sess-1", AgentModelsParams{Models: nil})

		got, ok := ps.agentModels["sess-1"]
		if !ok {
			t.Fatal("the withdrawal should still be recorded")
		}
		if len(got.Models) != 0 {
			t.Errorf("expected the list cleared, got %+v", got.Models)
		}
	})

	t.Run("falls back to the per-model current flag", func(t *testing.T) {
		// Gateways send the top-level field or the flag, not always both.
		ps := newModelsServer()
		ps.HandleAgentModels(context.Background(), "sess-1", AgentModelsParams{
			Models: []AgentModel{{ID: "a"}, {ID: "b", Current: true}},
		})

		if got := ps.agentModels["sess-1"].CurrentModel; got != "b" {
			t.Errorf("CurrentModel = %q, want %q", got, "b")
		}
	})

	t.Run("leaves the current model empty when nothing claims to be it", func(t *testing.T) {
		ps := newModelsServer()
		ps.HandleAgentModels(context.Background(), "sess-1", AgentModelsParams{Models: []AgentModel{{ID: "a"}}})

		if got := ps.agentModels["sess-1"].CurrentModel; got != "" {
			t.Errorf("CurrentModel = %q, want empty", got)
		}
	})

	t.Run("the top-level field beats the flag when both are set", func(t *testing.T) {
		ps := newModelsServer()
		ps.HandleAgentModels(context.Background(), "sess-1", AgentModelsParams{
			CurrentModel: "a",
			Models:       []AgentModel{{ID: "b", Current: true}},
		})

		if got := ps.agentModels["sess-1"].CurrentModel; got != "a" {
			t.Errorf("CurrentModel = %q, want %q", got, "a")
		}
	})
}

func TestPickAgentModels(t *testing.T) {
	snapshots := map[string]AgentModelsSnapshot{
		"live":     {ConfigID: "model", Models: []AgentModel{{ID: "a"}}},
		"dead":     {ConfigID: "stale", Models: []AgentModel{{ID: "z"}}},
		"withdrew": {ConfigID: "empty"},
	}

	t.Run("reports what a live session offers", func(t *testing.T) {
		got := pickAgentModels(snapshots, []string{"live"})
		if got == nil || got.ConfigID != "model" {
			t.Fatalf("got %+v", got)
		}
	})

	t.Run("never reports a disconnected session's models", func(t *testing.T) {
		// The whole reason for the filter: the snapshot is cached, so a
		// workspace would otherwise advertise the models of an agent that left.
		if got := pickAgentModels(snapshots, []string{"dead-gone"}); got != nil {
			t.Errorf("got %+v, want nil", got)
		}
		if got := pickAgentModels(snapshots, nil); got != nil {
			t.Errorf("got %+v, want nil", got)
		}
	})

	t.Run("skips a session that withdrew its offer", func(t *testing.T) {
		if got := pickAgentModels(snapshots, []string{"withdrew"}); got != nil {
			t.Errorf("got %+v, want nil", got)
		}
	})

	t.Run("passes over an empty offer to find a real one", func(t *testing.T) {
		got := pickAgentModels(snapshots, []string{"withdrew", "live"})
		if got == nil || got.ConfigID != "model" {
			t.Fatalf("got %+v", got)
		}
	})

	t.Run("follows session order rather than map order", func(t *testing.T) {
		// Two identical calls must not disagree, which iterating the map would
		// allow.
		for i := 0; i < 50; i++ {
			got := pickAgentModels(snapshots, []string{"dead", "live"})
			if got == nil || got.ConfigID != "stale" {
				t.Fatalf("call %d got %+v, want the first live session's", i, got)
			}
		}
	})

	t.Run("has nothing to report when nothing was recorded", func(t *testing.T) {
		if got := pickAgentModels(map[string]AgentModelsSnapshot{}, []string{"live"}); got != nil {
			t.Errorf("got %+v, want nil", got)
		}
		if got := pickAgentModels(nil, []string{"live"}); got != nil {
			t.Errorf("got %+v, want nil", got)
		}
	})
}

func TestPruneAgentModels(t *testing.T) {
	t.Run("drops the sessions that have gone", func(t *testing.T) {
		snapshots := map[string]AgentModelsSnapshot{"a": {}, "b": {}, "c": {}}
		pruneAgentModels(snapshots, []string{"b"})

		if len(snapshots) != 1 {
			t.Fatalf("snapshots = %+v, want only b", snapshots)
		}
		if _, ok := snapshots["b"]; !ok {
			t.Errorf("kept the wrong one: %+v", snapshots)
		}
	})

	t.Run("keeps everything when nothing is connected", func(t *testing.T) {
		// No live sessions means "between connections", not "all stale" —
		// pruning there would discard the notification being recorded.
		snapshots := map[string]AgentModelsSnapshot{"a": {}}
		pruneAgentModels(snapshots, nil)

		if len(snapshots) != 1 {
			t.Errorf("snapshots = %+v, want a kept", snapshots)
		}
	})
}

func TestAgentModelsWithoutAnMCPServer(t *testing.T) {
	// No server means no live sessions, so there is nothing to advertise even
	// though something was recorded earlier.
	ps := newModelsServer()
	ps.agentModels["sess-1"] = AgentModelsSnapshot{Models: []AgentModel{{ID: "a"}}}

	if got := ps.AgentModels(); got != nil {
		t.Errorf("AgentModels() = %+v, want nil", got)
	}
	if got := ps.liveSessionIDs(); got != nil {
		t.Errorf("liveSessionIDs() = %v, want nil", got)
	}
}

// The end-to-end shape: a real session connects, reports its models, and the
// workspace advertises them only for as long as that session is there. The
// pure functions above cover the rules; this covers the wiring to the SDK.
func TestAgentModelsWithAConnectedSession(t *testing.T) {
	replies := 0
	ps := permissionServer(t, &replies)
	// A bus, because recording a report now announces it.
	ps.bus = eventbus.New()
	ps.agentModels = make(map[string]AgentModelsSnapshot)
	sessionID := connectedServer(t, ps, "acp-gateway")
	streamFor(ps, sessionID)

	if got := ps.liveSessionIDs(); len(got) != 1 || got[0] != sessionID {
		t.Fatalf("liveSessionIDs() = %v, want [%s]", got, sessionID)
	}

	// Nothing reported yet, so there is nothing to advertise.
	if got := ps.AgentModels(); got != nil {
		t.Errorf("AgentModels() = %+v before any report, want nil", got)
	}

	ps.HandleAgentModels(context.Background(), sessionID, AgentModelsParams{
		ConfigID:     "model",
		CurrentModel: "gemini-2.5-pro",
		Models:       []AgentModel{{ID: "gemini-2.5-pro"}, {ID: "gemini-2.5-flash"}},
	})

	got := ps.AgentModels()
	if got == nil {
		t.Fatal("AgentModels() = nil after the session reported")
	}
	if got.ConfigID != "model" || got.CurrentModel != "gemini-2.5-pro" || len(got.Models) != 2 {
		t.Errorf("AgentModels() = %+v", got)
	}
}

// A snapshot recorded by a session that is no longer connected is never
// advertised, even though it is still cached — the case the live-session filter
// exists for.
func TestAgentModelsIgnoresADepartedSession(t *testing.T) {
	replies := 0
	ps := permissionServer(t, &replies)
	ps.agentModels = map[string]AgentModelsSnapshot{
		"a-session-that-left": {ConfigID: "model", Models: []AgentModel{{ID: "a"}}},
	}
	connectedServer(t, ps, "acp-gateway")

	if got := ps.AgentModels(); got != nil {
		t.Errorf("AgentModels() = %+v, want nil for a departed session", got)
	}
}

// Recording a new session's models evicts a departed one's, so the map does not
// grow for the life of the process.
func TestHandleAgentModelsPrunesDepartedSessions(t *testing.T) {
	replies := 0
	ps := permissionServer(t, &replies)
	ps.agentModels = map[string]AgentModelsSnapshot{
		"a-session-that-left": {Models: []AgentModel{{ID: "old"}}},
	}
	sessionID := connectedServer(t, ps, "acp-gateway")

	ps.HandleAgentModels(context.Background(), sessionID, AgentModelsParams{Models: []AgentModel{{ID: "new"}}})

	if _, ok := ps.agentModels["a-session-that-left"]; ok {
		t.Errorf("the departed session should have been pruned: %+v", ps.agentModels)
	}
	if _, ok := ps.agentModels[sessionID]; !ok {
		t.Errorf("the live session's models should have been kept: %+v", ps.agentModels)
	}
}

// The bug this guards: an MCP session outlives the stream that carried it, so
// the session list still holds a gateway that has gone and the snapshot keyed
// to it still looks live. A workspace would advertise the models of an agent
// nobody can reach.
func TestAgentModelsStopAtTheStream(t *testing.T) {
	replies := 0
	ps := permissionServer(t, &replies)
	ps.agentModels = make(map[string]AgentModelsSnapshot)
	sessionID := connectedServer(t, ps, "acp-gateway")
	drop := streamFor(ps, sessionID)

	ps.HandleAgentModels(context.Background(), sessionID, AgentModelsParams{
		CurrentModel: "gpt-5-codex",
		Models:       []AgentModel{{ID: "gpt-5-codex"}},
	})
	if ps.AgentModels() == nil {
		t.Fatal("expected the models while the stream is up")
	}

	drop() // the stream goes; the session lingers

	if got := ps.AgentModels(); got != nil {
		t.Errorf("AgentModels() = %+v after the stream went, want nil", got)
	}
	// The snapshot is still cached — it is the *reader* that has to be honest,
	// since the agent may come back on a new stream.
	if _, ok := ps.agentModels[sessionID]; !ok {
		t.Error("the snapshot should still be held, only not served")
	}
}

// Two gateways can be attached to one workspace, and one of them leaving must
// take its own models with it and leave the other's alone. A workspace-wide
// "is anything connected" cannot do that: it stays true while the departed
// session is still listed, and the picker takes the first snapshot it finds.
func TestAgentModelsWithTwoConnectedSessions(t *testing.T) {
	replies := 0
	ps := permissionServer(t, &replies)
	ps.agentModels = make(map[string]AgentModelsSnapshot)
	ps.mcpServer = mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0"}, nil)

	// Two sessions, each having reported its own models.
	first := connectSession(t, ps, "gateway-a")
	second := connectSession(t, ps, "gateway-b")
	dropFirst := streamFor(ps, first)
	streamFor(ps, second)
	ps.HandleAgentModels(context.Background(), first, AgentModelsParams{
		CurrentModel: "from-a", Models: []AgentModel{{ID: "from-a"}},
	})
	ps.HandleAgentModels(context.Background(), second, AgentModelsParams{
		CurrentModel: "from-b", Models: []AgentModel{{ID: "from-b"}},
	})

	dropFirst() // one goes; the other is still working

	got := ps.AgentModels()
	if got == nil {
		t.Fatal("AgentModels() = nil while a session is still streaming")
	}
	if got.CurrentModel != "from-b" {
		t.Errorf("CurrentModel = %q, want the surviving session's", got.CurrentModel)
	}
}

// connectedModelsServer is a workspace with a real session attached, which the
// publish tests need: what goes out on the bus when a session withdraws is what
// the workspace would now serve over REST, and that answer is filtered by the
// live sessions.
func connectedModelsServer(t *testing.T) (*WorkspaceServer, string, chan []byte) {
	t.Helper()
	replies := 0
	ps := permissionServer(t, &replies)
	ps.bus = eventbus.New()
	ps.agentModels = make(map[string]AgentModelsSnapshot)
	sessionID := connectedServer(t, ps, "acp-gateway")
	streamFor(ps, sessionID)

	ch := ps.bus.Subscribe(ps.workspaceID, "")
	t.Cleanup(func() { ps.bus.Unsubscribe(ps.workspaceID, "", ch) })
	return ps, sessionID, ch
}

// The model changes from outside the app — an agent reports one when its
// session comes up, long after any page load, and again every time it is
// switched — so the human clients have to be told rather than left to
// re-fetch. Before this the name on the Overview card was fixed at page load.
func TestHandleAgentModelsPublishesTheChange(t *testing.T) {
	// waitForEvent reads one event, or fails: a silent timeout here would look
	// like a passing test that asserts nothing.
	waitForEvent := func(t *testing.T, ch chan []byte) map[string]any {
		t.Helper()
		select {
		case raw := <-ch:
			// The bus frames events for SSE, so what arrives is
			// "data: {...}\n\n" rather than bare JSON.
			var evt struct {
				Type    string         `json:"type"`
				Payload map[string]any `json:"payload"`
			}
			body := strings.TrimSpace(strings.TrimPrefix(string(raw), "data: "))
			if err := json.Unmarshal([]byte(body), &evt); err != nil {
				t.Fatalf("unmarshal event %q: %v", raw, err)
			}
			if evt.Type != "agent.models" {
				t.Fatalf("event type = %q, want agent.models", evt.Type)
			}
			return evt.Payload
		case <-time.After(time.Second):
			t.Fatal("no event was published")
			return nil
		}
	}

	expectSilence := func(t *testing.T, ch chan []byte) {
		t.Helper()
		select {
		case raw := <-ch:
			t.Errorf("an unchanged report was announced: %s", raw)
		case <-time.After(50 * time.Millisecond):
		}
	}

	t.Run("announces the models with a base62 workspace id", func(t *testing.T) {
		// Base62 because that is the only form the frontend ever holds; a raw
		// int64 would match nothing and the picker would never move.
		ps, sessionID, ch := connectedModelsServer(t)

		ps.HandleAgentModels(context.Background(), sessionID, AgentModelsParams{
			ConfigID:     "model",
			CurrentModel: "gemini-2.5-pro",
			Models: []AgentModel{
				{ID: "gemini-2.5-pro", Name: "Gemini 2.5 Pro", Group: "Google"},
				{ID: "gpt-5-codex", Name: "GPT-5 Codex"},
			},
		})

		payload := waitForEvent(t, ch)
		want := monoflake.ID(ps.workspaceID).String()
		if got, ok := payload["workspaceId"].(string); !ok || got != want {
			t.Errorf("workspaceId = %v, want the base62 form %q", payload["workspaceId"], want)
		}
		// camelCase, matching the REST view rather than the snake_case
		// notification this came from: the client reads it into the same store
		// slot the workspace payload fills.
		if payload["configId"] != "model" {
			t.Errorf("configId = %v, want %q", payload["configId"], "model")
		}
		if payload["currentModel"] != "gemini-2.5-pro" {
			t.Errorf("currentModel = %v, want %q", payload["currentModel"], "gemini-2.5-pro")
		}
		models, ok := payload["models"].([]any)
		if !ok || len(models) != 2 {
			t.Fatalf("models = %v", payload["models"])
		}
		first, _ := models[0].(map[string]any)
		if first["id"] != "gemini-2.5-pro" || first["name"] != "Gemini 2.5 Pro" || first["group"] != "Google" {
			t.Errorf("models[0] = %v", first)
		}
	})

	t.Run("stays quiet when the agent re-advertises the same list", func(t *testing.T) {
		// An agent re-sends its models on every session config change, so a
		// thinking-effort switch repeats the whole list unchanged. Broadcasting
		// that to every open tab would be noise about a picker that did not move.
		ps, sessionID, ch := connectedModelsServer(t)

		same := AgentModelsParams{
			ConfigID:     "model",
			CurrentModel: "a",
			Models:       []AgentModel{{ID: "a", Name: "A"}, {ID: "b", Name: "B"}},
		}
		ps.HandleAgentModels(context.Background(), sessionID, same)
		waitForEvent(t, ch)

		ps.HandleAgentModels(context.Background(), sessionID, AgentModelsParams{
			ConfigID:     "model",
			CurrentModel: "a",
			Models:       []AgentModel{{ID: "a", Name: "A"}, {ID: "b", Name: "B"}},
		})

		expectSilence(t, ch)
	})

	t.Run("announces a switch that changes only the current model", func(t *testing.T) {
		// The event this whole path exists to carry. A gateway may send the
		// top-level current_model without the per-model `current` flag, and then
		// a switch leaves the list byte-identical — so comparing lists alone
		// would call the one change that matters "unchanged".
		ps, sessionID, ch := connectedModelsServer(t)

		models := []AgentModel{{ID: "a", Name: "A"}, {ID: "b", Name: "B"}}
		ps.HandleAgentModels(context.Background(), sessionID, AgentModelsParams{
			ConfigID: "model", CurrentModel: "a", Models: models,
		})
		waitForEvent(t, ch)

		ps.HandleAgentModels(context.Background(), sessionID, AgentModelsParams{
			ConfigID: "model", CurrentModel: "b", Models: models,
		})

		payload := waitForEvent(t, ch)
		if payload["currentModel"] != "b" {
			t.Errorf("currentModel = %v, want the model just switched to", payload["currentModel"])
		}
	})

	t.Run("announces a withdrawal as an empty list", func(t *testing.T) {
		// This is what takes the picker away. Without it the interface would go
		// on offering models the agent has stopped accepting.
		ps, sessionID, ch := connectedModelsServer(t)

		ps.HandleAgentModels(context.Background(), sessionID, AgentModelsParams{
			ConfigID: "model", Models: []AgentModel{{ID: "a"}},
		})
		waitForEvent(t, ch)

		ps.HandleAgentModels(context.Background(), sessionID, AgentModelsParams{Models: nil})

		payload := waitForEvent(t, ch)
		if models, ok := payload["models"].([]any); !ok || len(models) != 0 {
			t.Errorf("models = %v, want an empty list", payload["models"])
		}
	})

	t.Run("keeps the picker when one session withdraws but another still offers", func(t *testing.T) {
		// Two gateways can be attached to one workspace. One of them dropping
		// its models says nothing about the other's, and clearing the picker
		// would take away a choice that still works.
		ps, sessionID, ch := connectedModelsServer(t)

		ps.HandleAgentModels(context.Background(), sessionID, AgentModelsParams{
			ConfigID: "model", CurrentModel: "still-here", Models: []AgentModel{{ID: "still-here"}},
		})
		waitForEvent(t, ch)

		ps.HandleAgentModels(context.Background(), "a-second-session", AgentModelsParams{Models: nil})

		payload := waitForEvent(t, ch)
		models, ok := payload["models"].([]any)
		if !ok || len(models) != 1 {
			t.Fatalf("models = %v, want the surviving session's list", payload["models"])
		}
		first, _ := models[0].(map[string]any)
		if first["id"] != "still-here" {
			t.Errorf("models[0] = %v, want the session that is still offering", first)
		}
		if payload["currentModel"] != "still-here" {
			t.Errorf("currentModel = %v, want the surviving session's", payload["currentModel"])
		}
	})
}

func TestSameAgentModels(t *testing.T) {
	base := AgentModelsSnapshot{
		SessionID:    "acp-1",
		ConfigID:     "model",
		CurrentModel: "a",
		Models:       []AgentModel{{ID: "a", Name: "A"}, {ID: "b", Name: "B"}},
	}

	t.Run("a reconnected session offering the same list is not a change", func(t *testing.T) {
		// SessionID is not published, so a gateway that rebuilt its ACP session
		// while offering an identical list has changed nothing a reader can see.
		other := base
		other.SessionID = "acp-2"
		if !sameAgentModels(base, other) {
			t.Error("a new agent session alone should not count as a change")
		}
	})

	t.Run("the current model is part of the comparison", func(t *testing.T) {
		other := base
		other.CurrentModel = "b"
		if sameAgentModels(base, other) {
			t.Error("a switch with an unchanged list must count as a change")
		}
	})

	t.Run("the config id is part of the comparison", func(t *testing.T) {
		// A selection is written back to this option, so a picker holding a
		// stale one would write to something the agent no longer advertises.
		other := base
		other.ConfigID = "model_id"
		if sameAgentModels(base, other) {
			t.Error("a different config option must count as a change")
		}
	})

	t.Run("the list itself is part of the comparison", func(t *testing.T) {
		other := base
		other.Models = []AgentModel{{ID: "a", Name: "A"}}
		if sameAgentModels(base, other) {
			t.Error("a shorter list must count as a change")
		}
	})
}

// Choosing a model is the second thing this server ever asks an agent to do,
// after stopping. These pin the three ways it can refuse — because a picker
// that silently does nothing is the failure this whole capability gate exists
// to prevent — and that the request reaches exactly one session.
func TestSendSetModelNotification(t *testing.T) {
	selectable := AgentModelsParams{
		SessionID:    "acp-session-1",
		ConfigID:     "model",
		CurrentModel: "a",
		CanSet:       true,
		Models:       []AgentModel{{ID: "a"}, {ID: "b"}},
	}

	t.Run("refuses when nothing has reported any models", func(t *testing.T) {
		ps, _, _ := connectedModelsServer(t)

		if err := ps.SendSetModelNotification(context.Background(), "a"); !errors.Is(err, ErrModelSelectUnsupported) {
			t.Errorf("err = %v, want ErrModelSelectUnsupported", err)
		}
	})

	t.Run("refuses a gateway that never said it can set one", func(t *testing.T) {
		// Every gateway in the field reports models and none of the older ones
		// will act on being told to change one. Reading silence as consent is
		// what would offer a picker that does nothing on every deployment that
		// exists today.
		ps, sessionID, _ := connectedModelsServer(t)
		params := selectable
		params.CanSet = false
		ps.HandleAgentModels(context.Background(), sessionID, params)

		if err := ps.SendSetModelNotification(context.Background(), "a"); !errors.Is(err, ErrModelSelectUnsupported) {
			t.Errorf("err = %v, want ErrModelSelectUnsupported", err)
		}
	})

	t.Run("refuses a willing gateway that named no config option", func(t *testing.T) {
		// A selection is a write to that option. Willingness without somewhere
		// to write is a promise that cannot be kept.
		ps, sessionID, _ := connectedModelsServer(t)
		params := selectable
		params.ConfigID = ""
		ps.HandleAgentModels(context.Background(), sessionID, params)

		if err := ps.SendSetModelNotification(context.Background(), "a"); !errors.Is(err, ErrModelSelectUnsupported) {
			t.Errorf("err = %v, want ErrModelSelectUnsupported", err)
		}
	})

	t.Run("refuses a model the agent never offered", func(t *testing.T) {
		// A tab that loaded before the agent narrowed its list would otherwise
		// write an ID the agent no longer accepts, and the failure would reach
		// the human as silence rather than as a message.
		ps, sessionID, _ := connectedModelsServer(t)
		ps.HandleAgentModels(context.Background(), sessionID, selectable)

		if err := ps.SendSetModelNotification(context.Background(), "not-offered"); !errors.Is(err, ErrModelNotOffered) {
			t.Errorf("err = %v, want ErrModelNotOffered", err)
		}
	})

	t.Run("sends to the session that offered the choice", func(t *testing.T) {
		ps, sessionID, _ := connectedModelsServer(t)
		ps.HandleAgentModels(context.Background(), sessionID, selectable)

		if err := ps.SendSetModelNotification(context.Background(), "b"); err != nil {
			t.Fatalf("SendSetModelNotification() = %v, want nil", err)
		}
	})

	t.Run("does not record the switch it only asked for", func(t *testing.T) {
		// The agent answers with a models notification once it has actually
		// switched, and that report is the only thing that knows whether it
		// did. Recording it here would make the interface claim a switch the
		// agent may have refused.
		ps, sessionID, _ := connectedModelsServer(t)
		ps.HandleAgentModels(context.Background(), sessionID, selectable)

		if err := ps.SendSetModelNotification(context.Background(), "b"); err != nil {
			t.Fatalf("SendSetModelNotification() = %v", err)
		}

		if got := ps.AgentModels().CurrentModel; got != "a" {
			t.Errorf("CurrentModel = %q, want the agent's last report %q", got, "a")
		}
	})

	t.Run("stops offering the choice once the session drops its stream", func(t *testing.T) {
		// The trap #504 fixed for the Stop button: a session outlives the
		// stream that carried it, so a snapshot keyed to a departed gateway
		// still looks live to anything reading the session list alone.
		ps, sessionID, _ := connectedModelsServer(t)
		ps.HandleAgentModels(context.Background(), sessionID, selectable)
		ps.removeStreamingSession(sessionID)

		if err := ps.SendSetModelNotification(context.Background(), "b"); !errors.Is(err, ErrModelSelectUnsupported) {
			t.Errorf("err = %v, want ErrModelSelectUnsupported", err)
		}
	})
}

func TestAgentModelsSelectable(t *testing.T) {
	cases := []struct {
		name     string
		snapshot AgentModelsSnapshot
		want     bool
	}{
		{"willing and with somewhere to write", AgentModelsSnapshot{CanSet: true, ConfigID: "model"}, true},
		{"willing but naming no config option", AgentModelsSnapshot{CanSet: true}, false},
		{"a config option but never said it can set", AgentModelsSnapshot{ConfigID: "model"}, false},
		{"an older gateway, saying neither", AgentModelsSnapshot{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.snapshot.Selectable(); got != tc.want {
				t.Errorf("Selectable() = %v, want %v", got, tc.want)
			}
		})
	}
}

// can_set is what turns the picker on, so a gateway starting to advertise it
// has to reach the open tabs — even when the list it comes with is unchanged.
func TestHandleAgentModelsPublishesCanSetChange(t *testing.T) {
	ps, sessionID, ch := connectedModelsServer(t)

	models := []AgentModel{{ID: "a"}}
	ps.HandleAgentModels(context.Background(), sessionID, AgentModelsParams{
		ConfigID: "model", CurrentModel: "a", Models: models,
	})
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("the first report was not announced")
	}

	ps.HandleAgentModels(context.Background(), sessionID, AgentModelsParams{
		ConfigID: "model", CurrentModel: "a", Models: models, CanSet: true,
	})

	select {
	case raw := <-ch:
		var evt struct {
			Payload map[string]any `json:"payload"`
		}
		body := strings.TrimSpace(strings.TrimPrefix(string(raw), "data: "))
		if err := json.Unmarshal([]byte(body), &evt); err != nil {
			t.Fatalf("unmarshal event: %v", err)
		}
		if evt.Payload["canSet"] != true {
			t.Errorf("canSet = %v, want true", evt.Payload["canSet"])
		}
	case <-time.After(time.Second):
		t.Fatal("a gateway that began offering selection was not announced")
	}
}

// A gateway willing to switch but naming no config option to switch through.
// Selectable() has always refused it; the live event used to say otherwise, so
// a picker appeared, failed with a 409 on use, and vanished on the next reload.
func TestPublishedCanSetFollowsTheSameRuleAsTheWorkspacePayload(t *testing.T) {
	ps, sessionID, ch := connectedModelsServer(t)

	ps.HandleAgentModels(context.Background(), sessionID, AgentModelsParams{
		CanSet: true,
		Models: []AgentModel{{ID: "a"}},
	})

	select {
	case raw := <-ch:
		var evt struct {
			Payload map[string]any `json:"payload"`
		}
		body := strings.TrimSpace(strings.TrimPrefix(string(raw), "data: "))
		if err := json.Unmarshal([]byte(body), &evt); err != nil {
			t.Fatalf("unmarshal event: %v", err)
		}
		if evt.Payload["canSet"] != false {
			t.Errorf("canSet = %v, want false — there is no config option to write to", evt.Payload["canSet"])
		}
	case <-time.After(time.Second):
		t.Fatal("the report was not announced")
	}

	// The two answers have to agree, which is the whole point of the rule.
	if got := ps.AgentModels(); got == nil || got.Selectable() {
		t.Errorf("Selectable() = %v, want false", got != nil && got.Selectable())
	}
}

// Two gateways on one workspace, the capable one second in session order.
// Taking the first with a model list would report the older one's inability and
// refuse a switch that the newer one would have carried out.
func TestPickAgentModelsPrefersASessionThatCanActuallySwitch(t *testing.T) {
	readOnly := AgentModelsSnapshot{
		ConfigID: "model",
		Models:   []AgentModel{{ID: "old"}},
	}
	capable := AgentModelsSnapshot{
		ConfigID: "model",
		CanSet:   true,
		Models:   []AgentModel{{ID: "new"}},
	}
	snapshots := map[string]AgentModelsSnapshot{"first": readOnly, "second": capable}

	id, got := pickAgentModelsSession(snapshots, []string{"first", "second"})
	if got == nil || !got.Selectable() {
		t.Fatalf("picked %+v, want the session that can switch", got)
	}
	if id != "second" {
		t.Errorf("session = %q, want the capable one so the notification reaches it", id)
	}

	t.Run("still shows a read-only gateway when that is all there is", func(t *testing.T) {
		// Every deployment today. Preferring selectable must not mean showing
		// nothing when nothing is selectable.
		id, got := pickAgentModelsSession(map[string]AgentModelsSnapshot{"only": readOnly}, []string{"only"})
		if got == nil || len(got.Models) != 1 || got.Models[0].ID != "old" {
			t.Fatalf("picked %+v, want the read-only gateway's list", got)
		}
		if id != "only" {
			t.Errorf("session = %q, want %q", id, "only")
		}
	})
}

// The delivery path has to agree with the preference, or the notification is
// addressed to a gateway that will ignore it while a capable one stands beside.
func TestSendSetModelReachesTheCapableGatewayOfTwo(t *testing.T) {
	ps, readOnlyID, _ := connectedModelsServer(t)
	capableID := connectSession(t, ps, "acp-gateway")
	streamFor(ps, capableID)

	ps.HandleAgentModels(context.Background(), readOnlyID, AgentModelsParams{
		ConfigID: "model", CurrentModel: "old", Models: []AgentModel{{ID: "old"}},
	})
	ps.HandleAgentModels(context.Background(), capableID, AgentModelsParams{
		ConfigID: "model", CurrentModel: "new", CanSet: true,
		Models: []AgentModel{{ID: "new"}, {ID: "newer"}},
	})

	// "newer" exists only on the capable gateway, so a refusal here would mean
	// the read-only one was consulted.
	if err := ps.SendSetModelNotification(context.Background(), "newer"); err != nil {
		t.Errorf("SendSetModelNotification() = %v, want it to reach the capable gateway", err)
	}
}
