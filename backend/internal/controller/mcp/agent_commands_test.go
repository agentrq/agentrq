// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/mustafaturan/monoflake"

	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
)

// The commands an ACP agent advertises used to stop at the gateway. These tests
// pin the workspace end of that path: that the payload the gateways actually
// send is understood, that a snapshot never outlives the session that reported
// it, and that the human's open tabs are told when the menu changes — since the
// commands arrive long after any page load.

// connectedCommandsServer is a workspace with a real session attached, which
// the publish tests need: what goes out on the bus is what the workspace would
// now serve over REST, and that answer is filtered by the live sessions.
func connectedCommandsServer(t *testing.T) (*WorkspaceServer, string, chan []byte) {
	t.Helper()
	replies := 0
	ps := permissionServer(t, &replies)
	ps.bus = eventbus.New()
	ps.agentCommands = make(map[string]AgentCommandsSnapshot)
	sessionID := connectedServer(t, ps, "acp-gateway")
	streamFor(ps, sessionID)

	ch := ps.bus.Subscribe(ps.workspaceID, "")
	t.Cleanup(func() { ps.bus.Unsubscribe(ps.workspaceID, "", ch) })
	return ps, sessionID, ch
}

func newCommandsServer() *WorkspaceServer {
	return &WorkspaceServer{
		workspaceID:   100,
		bus:           eventbus.New(),
		agentCommands: make(map[string]AgentCommandsSnapshot),
	}
}

// The exact frame acp-gateway puts on the wire (src/acpClient.ts), so a rename
// on either side fails here rather than in production.
const gatewayCommandsFrame = `{
  "jsonrpc": "2.0",
  "method": "notifications/claude/channel/commands",
  "params": {
    "task_id": "0iF1U1qQwS1",
    "session_id": "01a07404-01a7-7033-a58f-47b119036378",
    "commands": [
      {"name": "web", "description": "Search the web for information", "hint": "query to search for"},
      {"name": "compact"}
    ]
  }
}`

func TestAgentCommandsParamsMatchesTheGatewayWireFormat(t *testing.T) {
	var frame struct {
		Method string              `json:"method"`
		Params AgentCommandsParams `json:"params"`
	}
	if err := json.Unmarshal([]byte(gatewayCommandsFrame), &frame); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if frame.Method != AgentCommandsNotificationMethod {
		t.Errorf("method = %q, want %q", frame.Method, AgentCommandsNotificationMethod)
	}

	p := frame.Params
	if p.TaskID != "0iF1U1qQwS1" {
		t.Errorf("task_id = %q", p.TaskID)
	}
	if p.SessionID != "01a07404-01a7-7033-a58f-47b119036378" {
		t.Errorf("session_id = %q", p.SessionID)
	}
	if len(p.Commands) != 2 {
		t.Fatalf("commands = %d, want 2", len(p.Commands))
	}
	want := AgentCommand{Name: "web", Description: "Search the web for information", Hint: "query to search for"}
	if !reflect.DeepEqual(p.Commands[0], want) {
		t.Errorf("commands[0] = %+v, want %+v", p.Commands[0], want)
	}
	if p.Commands[1].Description != "" || p.Commands[1].Hint != "" {
		t.Errorf("optional fields should stay zero when absent: %+v", p.Commands[1])
	}
}

func TestHandleCustomNotificationRoutesCommands(t *testing.T) {
	ps := newCommandsServer()
	ps.HandleCustomNotification(context.Background(), "transport-session", []byte(gatewayCommandsFrame))

	// Keyed by the MCP transport session — the namespace liveSessionIDs
	// reports — and not by the ACP session the payload names.
	got, ok := ps.agentCommands["transport-session"]
	if !ok {
		t.Fatalf("commands were not recorded; map = %+v", ps.agentCommands)
	}
	if _, wrong := ps.agentCommands["01a07404-01a7-7033-a58f-47b119036378"]; wrong {
		t.Error("keyed by the payload's ACP session, which liveSessionIDs never reports")
	}
	if len(got.Commands) != 2 || got.Commands[0].Name != "web" {
		t.Errorf("recorded = %+v", got)
	}
	if got.SessionID != "01a07404-01a7-7033-a58f-47b119036378" {
		t.Errorf("the agent's session should be kept on the snapshot, got %q", got.SessionID)
	}
}

func TestHandleAgentCommands(t *testing.T) {
	t.Run("records what the session reported", func(t *testing.T) {
		ps := newCommandsServer()
		ps.HandleAgentCommands(context.Background(), "sess-1", AgentCommandsParams{
			Commands: []AgentCommand{{Name: "init"}, {Name: "review", Description: "Review the diff"}},
		})

		got := ps.agentCommands["sess-1"]
		if len(got.Commands) != 2 || got.Commands[1].Description != "Review the diff" {
			t.Errorf("recorded = %+v", got)
		}
	})

	t.Run("keys on the transport session, not the agent's", func(t *testing.T) {
		// The payload's session_id is the gateway's ACP session; the key has to
		// be the MCP transport session, because that is the namespace
		// liveSessionIDs reports and therefore the one the reader filters on.
		// Keying on the payload would file every snapshot under an ID that
		// never appears live, and none would ever be advertised.
		ps := newCommandsServer()
		ps.HandleAgentCommands(context.Background(), "mcp-session", AgentCommandsParams{
			SessionID: "acp-session",
			Commands:  []AgentCommand{{Name: "init"}},
		})

		if _, ok := ps.agentCommands["mcp-session"]; !ok {
			t.Errorf("expected the transport session as the key, got %+v", ps.agentCommands)
		}
		if _, ok := ps.agentCommands["acp-session"]; ok {
			t.Error("must not key on the agent's own session id")
		}
		if got := ps.agentCommands["mcp-session"].SessionID; got != "acp-session" {
			t.Errorf("the agent's session should still be recorded on the snapshot, got %q", got)
		}
	})

	t.Run("records against a blank transport session rather than dropping it", func(t *testing.T) {
		// Some transports leave the session ID empty. The reader filters
		// against the same list this is keyed on, so "" is internally
		// consistent — and dropping it would lose the commands outright.
		ps := newCommandsServer()
		ps.HandleAgentCommands(context.Background(), "", AgentCommandsParams{Commands: []AgentCommand{{Name: "init"}}})

		if _, ok := ps.agentCommands[""]; !ok {
			t.Errorf("expected the commands recorded under the blank key, got %+v", ps.agentCommands)
		}
		if got := pickAgentCommands(ps.agentCommands, []string{""}); got == nil {
			t.Error("a blank-keyed snapshot must still be readable for a blank live session")
		}
	})

	t.Run("the newest report replaces the previous one", func(t *testing.T) {
		ps := newCommandsServer()
		ps.HandleAgentCommands(context.Background(), "sess-1", AgentCommandsParams{
			Commands: []AgentCommand{{Name: "a"}, {Name: "b"}},
		})
		ps.HandleAgentCommands(context.Background(), "sess-1", AgentCommandsParams{
			Commands: []AgentCommand{{Name: "c"}},
		})

		got := ps.agentCommands["sess-1"]
		if len(got.Commands) != 1 || got.Commands[0].Name != "c" {
			t.Errorf("expected the second report to win, got %+v", got)
		}
	})

	t.Run("an empty list is recorded, because it withdraws the menu", func(t *testing.T) {
		ps := newCommandsServer()
		ps.HandleAgentCommands(context.Background(), "sess-1", AgentCommandsParams{Commands: []AgentCommand{{Name: "a"}}})
		ps.HandleAgentCommands(context.Background(), "sess-1", AgentCommandsParams{Commands: nil})

		got, ok := ps.agentCommands["sess-1"]
		if !ok {
			t.Fatal("the withdrawal should still be recorded")
		}
		if len(got.Commands) != 0 {
			t.Errorf("expected the list cleared, got %+v", got.Commands)
		}
	})
}

// The menu changes from outside the app — an agent advertises its commands once
// its session is up, which is after a page has long since loaded — so the human
// clients have to be told rather than left to re-fetch.
func TestHandleAgentCommandsPublishesTheChange(t *testing.T) {
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
			body := bytes.TrimSpace(bytes.TrimPrefix(raw, []byte("data: ")))
			if err := json.Unmarshal(body, &evt); err != nil {
				t.Fatalf("unmarshal event %q: %v", raw, err)
			}
			if evt.Type != "agent.commands" {
				t.Fatalf("event type = %q, want agent.commands", evt.Type)
			}
			return evt.Payload
		case <-time.After(time.Second):
			t.Fatal("no event was published")
			return nil
		}
	}

	t.Run("announces the commands with a base62 workspace id", func(t *testing.T) {
		// Base62 because that is the only form the frontend ever holds; a raw
		// int64 would match nothing and the menu would never move.
		ps, sessionID, ch := connectedCommandsServer(t)

		ps.HandleAgentCommands(context.Background(), sessionID, AgentCommandsParams{
			Commands: []AgentCommand{{Name: "compact", Description: "Shorten the context"}},
		})

		payload := waitForEvent(t, ch)
		want := monoflake.ID(ps.workspaceID).String()
		if got, ok := payload["workspaceId"].(string); !ok || got != want {
			t.Errorf("workspaceId = %v, want the base62 form %q", payload["workspaceId"], want)
		}
		commands, ok := payload["commands"].([]any)
		if !ok || len(commands) != 1 {
			t.Fatalf("commands = %v", payload["commands"])
		}
		first, _ := commands[0].(map[string]any)
		if first["name"] != "compact" || first["description"] != "Shorten the context" {
			t.Errorf("commands[0] = %v", first)
		}
	})

	t.Run("stays quiet when the agent re-advertises the same list", func(t *testing.T) {
		// An agent repeats its list whenever context shifts without changing
		// what it offers. Broadcasting that to every open tab would be noise
		// about a menu that did not move.
		ps, sessionID, ch := connectedCommandsServer(t)

		same := []AgentCommand{{Name: "init"}, {Name: "review", Hint: "what to review"}}
		ps.HandleAgentCommands(context.Background(), sessionID, AgentCommandsParams{Commands: same})
		waitForEvent(t, ch)

		ps.HandleAgentCommands(context.Background(), sessionID, AgentCommandsParams{
			Commands: []AgentCommand{{Name: "init"}, {Name: "review", Hint: "what to review"}},
		})

		select {
		case raw := <-ch:
			t.Errorf("an identical list was re-announced: %s", raw)
		case <-time.After(50 * time.Millisecond):
		}
	})

	t.Run("announces a withdrawal as an empty list", func(t *testing.T) {
		// This is what takes the menu away. Without it the composer would go on
		// offering commands the agent has stopped accepting.
		ps, sessionID, ch := connectedCommandsServer(t)

		ps.HandleAgentCommands(context.Background(), sessionID, AgentCommandsParams{Commands: []AgentCommand{{Name: "a"}}})
		waitForEvent(t, ch)

		ps.HandleAgentCommands(context.Background(), sessionID, AgentCommandsParams{Commands: nil})

		payload := waitForEvent(t, ch)
		if commands, ok := payload["commands"].([]any); !ok || len(commands) != 0 {
			t.Errorf("commands = %v, want an empty list", payload["commands"])
		}
	})

	t.Run("keeps the menu when one session withdraws but another still offers", func(t *testing.T) {
		// Two gateways can be attached to one workspace. One of them dropping
		// its commands says nothing about the other's, and clearing the menu
		// would take away commands that still work.
		ps, sessionID, ch := connectedCommandsServer(t)

		ps.HandleAgentCommands(context.Background(), sessionID, AgentCommandsParams{
			Commands: []AgentCommand{{Name: "still-here"}},
		})
		waitForEvent(t, ch)

		ps.HandleAgentCommands(context.Background(), "a-second-session", AgentCommandsParams{Commands: nil})

		payload := waitForEvent(t, ch)
		commands, _ := payload["commands"].([]any)
		if len(commands) != 1 {
			t.Fatalf("commands = %v, want the surviving session's", payload["commands"])
		}
		first, _ := commands[0].(map[string]any)
		if first["name"] != "still-here" {
			t.Errorf("commands[0] = %v", first)
		}
	})

	t.Run("announces a changed description, not just a changed name", func(t *testing.T) {
		ps, sessionID, ch := connectedCommandsServer(t)

		ps.HandleAgentCommands(context.Background(), sessionID, AgentCommandsParams{
			Commands: []AgentCommand{{Name: "plan", Description: "the old wording"}},
		})
		waitForEvent(t, ch)

		ps.HandleAgentCommands(context.Background(), sessionID, AgentCommandsParams{
			Commands: []AgentCommand{{Name: "plan", Description: "the revised wording"}},
		})

		payload := waitForEvent(t, ch)
		commands, _ := payload["commands"].([]any)
		if len(commands) != 1 {
			t.Fatalf("commands = %v", payload["commands"])
		}
		first, _ := commands[0].(map[string]any)
		if first["description"] != "the revised wording" {
			t.Errorf("commands[0] = %v", first)
		}
	})
}

func TestPickAgentCommands(t *testing.T) {
	snapshots := map[string]AgentCommandsSnapshot{
		"live":     {SessionID: "acp-live", Commands: []AgentCommand{{Name: "a"}}},
		"dead":     {SessionID: "acp-dead", Commands: []AgentCommand{{Name: "z"}}},
		"withdrew": {SessionID: "acp-empty"},
	}

	t.Run("reports what a live session offers", func(t *testing.T) {
		got := pickAgentCommands(snapshots, []string{"live"})
		if got == nil || got.SessionID != "acp-live" {
			t.Fatalf("got %+v", got)
		}
	})

	t.Run("never reports a disconnected session's commands", func(t *testing.T) {
		// The whole reason for the filter: the snapshot is cached, so a
		// workspace would otherwise offer commands of an agent that left, and
		// every one of them would fail.
		if got := pickAgentCommands(snapshots, []string{"dead-gone"}); got != nil {
			t.Errorf("got %+v, want nil", got)
		}
		if got := pickAgentCommands(snapshots, nil); got != nil {
			t.Errorf("got %+v, want nil", got)
		}
	})

	t.Run("skips a session that withdrew its commands", func(t *testing.T) {
		if got := pickAgentCommands(snapshots, []string{"withdrew"}); got != nil {
			t.Errorf("got %+v, want nil", got)
		}
	})

	t.Run("passes over an empty offer to find a real one", func(t *testing.T) {
		got := pickAgentCommands(snapshots, []string{"withdrew", "live"})
		if got == nil || got.SessionID != "acp-live" {
			t.Fatalf("got %+v", got)
		}
	})

	t.Run("follows session order rather than map order", func(t *testing.T) {
		// Two identical calls must not disagree, which iterating the map would
		// allow.
		for i := 0; i < 50; i++ {
			got := pickAgentCommands(snapshots, []string{"dead", "live"})
			if got == nil || got.SessionID != "acp-dead" {
				t.Fatalf("call %d got %+v, want the first live session's", i, got)
			}
		}
	})

	t.Run("has nothing to report when nothing was recorded", func(t *testing.T) {
		if got := pickAgentCommands(map[string]AgentCommandsSnapshot{}, []string{"live"}); got != nil {
			t.Errorf("got %+v, want nil", got)
		}
		if got := pickAgentCommands(nil, []string{"live"}); got != nil {
			t.Errorf("got %+v, want nil", got)
		}
	})
}

func TestPruneAgentCommands(t *testing.T) {
	t.Run("drops the sessions that have gone", func(t *testing.T) {
		snapshots := map[string]AgentCommandsSnapshot{"a": {}, "b": {}, "c": {}}
		pruneAgentCommands(snapshots, []string{"b"})

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
		snapshots := map[string]AgentCommandsSnapshot{"a": {}}
		pruneAgentCommands(snapshots, nil)

		if len(snapshots) != 1 {
			t.Errorf("snapshots = %+v, want a kept", snapshots)
		}
	})
}

func TestAgentCommandsWithoutAnMCPServer(t *testing.T) {
	// No server means no live sessions, so there is nothing to offer even
	// though something was recorded earlier.
	ps := newCommandsServer()
	ps.agentCommands["sess-1"] = AgentCommandsSnapshot{Commands: []AgentCommand{{Name: "a"}}}

	if got := ps.AgentCommands(); got != nil {
		t.Errorf("AgentCommands() = %+v, want nil", got)
	}
}

// The end-to-end shape: a real session connects, reports its commands, and the
// workspace offers them only for as long as that session is there. The pure
// functions above cover the rules; this covers the wiring to the SDK.
func TestAgentCommandsWithAConnectedSession(t *testing.T) {
	replies := 0
	ps := permissionServer(t, &replies)
	ps.bus = eventbus.New()
	ps.agentCommands = make(map[string]AgentCommandsSnapshot)
	sessionID := connectedServer(t, ps, "acp-gateway")
	streamFor(ps, sessionID)

	// Nothing reported yet, so there is no menu. A gateway creates its ACP
	// session lazily, once it has a task, so this is the ordinary state of a
	// freshly connected workspace rather than an error.
	if got := ps.AgentCommands(); got != nil {
		t.Errorf("AgentCommands() = %+v before any report, want nil", got)
	}

	ps.HandleAgentCommands(context.Background(), sessionID, AgentCommandsParams{
		SessionID: "acp-session",
		Commands:  []AgentCommand{{Name: "init"}, {Name: "compact"}},
	})

	got := ps.AgentCommands()
	if got == nil {
		t.Fatal("AgentCommands() = nil after the session reported")
	}
	if len(got.Commands) != 2 || got.SessionID != "acp-session" {
		t.Errorf("AgentCommands() = %+v", got)
	}
}

// A snapshot recorded by a session that is no longer connected is never
// offered, even though it is still cached — the case the live-session filter
// exists for.
func TestAgentCommandsIgnoresADepartedSession(t *testing.T) {
	replies := 0
	ps := permissionServer(t, &replies)
	ps.bus = eventbus.New()
	ps.agentCommands = map[string]AgentCommandsSnapshot{
		"a-session-that-left": {Commands: []AgentCommand{{Name: "a"}}},
	}
	connectedServer(t, ps, "acp-gateway")

	if got := ps.AgentCommands(); got != nil {
		t.Errorf("AgentCommands() = %+v, want nil for a departed session", got)
	}
}

// Recording a new session's commands evicts a departed one's, so the map does
// not grow for the life of the process.
func TestHandleAgentCommandsPrunesDepartedSessions(t *testing.T) {
	replies := 0
	ps := permissionServer(t, &replies)
	ps.bus = eventbus.New()
	ps.agentCommands = map[string]AgentCommandsSnapshot{
		"a-session-that-left": {Commands: []AgentCommand{{Name: "old"}}},
	}
	sessionID := connectedServer(t, ps, "acp-gateway")

	ps.HandleAgentCommands(context.Background(), sessionID, AgentCommandsParams{
		Commands: []AgentCommand{{Name: "new"}},
	})

	if _, ok := ps.agentCommands["a-session-that-left"]; ok {
		t.Errorf("the departed session should have been pruned: %+v", ps.agentCommands)
	}
	if _, ok := ps.agentCommands[sessionID]; !ok {
		t.Errorf("the live session's commands should have been kept: %+v", ps.agentCommands)
	}
}

func TestManagerAgentCommands(t *testing.T) {
	m := NewManager(func(workspaceID int64, userID string) *WorkspaceServer {
		return &WorkspaceServer{workspaceID: workspaceID, userID: userID}
	})

	t.Run("nothing to report for a workspace with no server running", func(t *testing.T) {
		if got := m.AgentCommands(999); got != nil {
			t.Errorf("AgentCommands(999) = %+v, want nil", got)
		}
	})

	t.Run("asks the workspace's own server", func(t *testing.T) {
		replies := 0
		ps := permissionServer(t, &replies)
		ps.bus = eventbus.New()
		ps.agentCommands = make(map[string]AgentCommandsSnapshot)
		sessionID := connectedServer(t, ps, "acp-gateway")
		streamFor(ps, sessionID)
		ps.HandleAgentCommands(context.Background(), sessionID, AgentCommandsParams{
			Commands: []AgentCommand{{Name: "init"}},
		})

		m2 := NewManager(func(int64, string) *WorkspaceServer { return ps })
		m2.Get(7, "user")

		got := m2.AgentCommands(7)
		if got == nil || len(got.Commands) != 1 || got.Commands[0].Name != "init" {
			t.Errorf("AgentCommands(7) = %+v", got)
		}
	})
}

// Same as the models beside them: the session outlives its stream, so without
// this the workspace keeps offering commands nobody is behind — and a reply
// opening with one would be delivered stripped of its envelope on the strength
// of that menu.
func TestAgentCommandsStopAtTheStream(t *testing.T) {
	replies := 0
	ps := permissionServer(t, &replies)
	ps.bus = eventbus.New()
	ps.agentCommands = make(map[string]AgentCommandsSnapshot)
	sessionID := connectedServer(t, ps, "acp-gateway")
	drop := streamFor(ps, sessionID)

	ps.HandleAgentCommands(context.Background(), sessionID, AgentCommandsParams{
		Commands: []AgentCommand{{Name: "compact"}},
	})
	if ps.AgentCommands() == nil {
		t.Fatal("expected the commands while the stream is up")
	}

	drop()

	if got := ps.AgentCommands(); got != nil {
		t.Errorf("AgentCommands() = %+v after the stream went, want nil", got)
	}
	if _, ok := ps.agentCommands[sessionID]; !ok {
		t.Error("the snapshot should still be held, only not served")
	}
}
