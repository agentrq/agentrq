package mcp

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
)

// The notification this file handles used to be rejected outright — the SDK
// answered "JSON RPC not handled" and the gateway logged a transport error at
// the user. These tests pin both halves of the fix: that the payload the
// gateways actually send is understood, and that a snapshot never outlives the
// session that reported it.

func newModelsServer() *WorkspaceServer {
	return &WorkspaceServer{agentModels: make(map[string]AgentModelsSnapshot)}
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
	ps.agentModels = make(map[string]AgentModelsSnapshot)
	sessionID := connectedServer(t, ps, "acp-gateway")

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
