// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"context"
	"errors"
	"testing"

	"github.com/mustafaturan/monoflake"

	"github.com/agentrq/agentrq/backend/internal/controller/agentlink"
	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
)

var settableEffort = agentlink.EffortReport{
	SessionID: "s1", ConfigID: "effort", CurrentEffort: "high", CanSet: true,
	Efforts: []agentlink.Effort{{ID: "low", Name: "Low"}, {ID: "high", Name: "High", Description: "slower"}},
}

// effortEvent reads events until the next agent.effort one.
func effortEvent(t *testing.T, sub chan []byte) busEvent {
	t.Helper()
	for {
		if evt := readEvent(t, sub); evt.Type == "agent.effort" {
			return evt
		}
	}
}

// linkedEffortServer is a server with native links attached under ids, its
// event subscription drained of their connection events.
func linkedEffortServer(t *testing.T, ids ...string) (*WorkspaceServer, chan []byte, []*fakeLink) {
	t.Helper()
	ps, sub := newLinkServer(t)
	var links []*fakeLink
	for _, id := range ids {
		l := &fakeLink{id: id}
		ps.AttachAgentLink(l)
		links = append(links, l)
	}
	for len(sub) > 0 {
		<-sub
	}
	return ps, sub, links
}

func TestHandleAgentEffort(t *testing.T) {
	t.Run("publishes the level camelCase with a base62 workspace id, and only when it changed", func(t *testing.T) {
		ps, sub, _ := linkedEffortServer(t, "acp1:a")
		ps.HandleAgentEffort(context.Background(), "acp1:a", settableEffort)

		evt := effortEvent(t, sub)
		if evt.Payload["workspaceId"] != monoflake.ID(ps.workspaceID).String() || evt.Payload["configId"] != "effort" ||
			evt.Payload["currentEffort"] != "high" || evt.Payload["canSet"] != true {
			t.Fatalf("event payload = %+v", evt.Payload)
		}
		efforts, _ := evt.Payload["efforts"].([]any)
		if second, _ := efforts[1].(map[string]any); len(efforts) != 2 || second["id"] != "high" || second["description"] != "slower" {
			t.Fatalf("efforts = %+v", efforts)
		}

		ps.HandleAgentEffort(context.Background(), "acp1:a", settableEffort)
		expectNoEvent(t, sub)
	})

	t.Run("a withdrawal publishes an empty list, or what another session still offers", func(t *testing.T) {
		ps, sub, _ := linkedEffortServer(t, "acp1:a", "acp1:b")
		ps.HandleAgentEffort(context.Background(), "acp1:a", settableEffort)
		effortEvent(t, sub)

		ps.HandleAgentEffort(context.Background(), "acp1:a", agentlink.EffortReport{SessionID: "s1"})
		if evt := effortEvent(t, sub); len(evt.Payload["efforts"].([]any)) != 0 || evt.Payload["canSet"] != false {
			t.Fatalf("withdrawal payload = %+v, want no levels", evt.Payload)
		}
		if ps.AgentEffort() != nil {
			t.Fatal("AgentEffort is not nil after the only offer was withdrawn")
		}

		other := settableEffort
		other.CurrentEffort = "low"
		ps.HandleAgentEffort(context.Background(), "acp1:b", other)
		effortEvent(t, sub)
		ps.HandleAgentEffort(context.Background(), "acp1:a", settableEffort)
		effortEvent(t, sub)
		ps.HandleAgentEffort(context.Background(), "acp1:a", agentlink.EffortReport{SessionID: "s1"})
		if evt := effortEvent(t, sub); evt.Payload["currentEffort"] != "low" {
			t.Fatalf("withdrawal payload = %+v, want b's offer still shown", evt.Payload)
		}
	})

	t.Run("prefers a session that can switch, and forgets sessions that left", func(t *testing.T) {
		ps, _, _ := linkedEffortServer(t, "acp1:a", "acp1:b")
		readOnly := settableEffort
		readOnly.CanSet = false
		ps.HandleAgentEffort(context.Background(), "acp1:a", readOnly)
		if got := ps.AgentEffort(); got == nil || got.Selectable() {
			t.Fatalf("AgentEffort = %+v, want the read-only offer as a fallback", got)
		}
		ps.HandleAgentEffort(context.Background(), "acp1:b", settableEffort)
		if got := ps.AgentEffort(); got == nil || !got.Selectable() {
			t.Fatalf("AgentEffort = %+v, want b's settable offer", got)
		}

		ps.DetachAgentLink(context.Background(), "acp1:a")
		ps.HandleAgentEffort(context.Background(), "acp1:b", settableEffort)
		ps.agentEffortMu.RLock()
		_, kept := ps.agentEffort["acp1:a"]
		ps.agentEffortMu.RUnlock()
		if kept {
			t.Fatal("a detached link's level was kept")
		}
	})
}

func TestSetAgentEffort(t *testing.T) {
	t.Run("refuses when nothing offers a level", func(t *testing.T) {
		ps, _, _ := linkedEffortServer(t, "acp1:a")
		err := ps.SetAgentEffort(context.Background(), "low")
		if !errors.Is(err, ErrEffortSelectUnsupported) || err.Error() != "the connected agent does not support choosing a reasoning level" {
			t.Fatalf("err = %v, want ErrEffortSelectUnsupported", err)
		}
	})

	t.Run("refuses an offer that cannot be written back", func(t *testing.T) {
		ps, _, _ := linkedEffortServer(t, "acp1:a")
		p := settableEffort
		p.ConfigID = ""
		ps.HandleAgentEffort(context.Background(), "acp1:a", p)
		if err := ps.SetAgentEffort(context.Background(), "low"); !errors.Is(err, ErrEffortSelectUnsupported) {
			t.Fatalf("err = %v, want ErrEffortSelectUnsupported", err)
		}
	})

	t.Run("refuses a level the agent did not offer", func(t *testing.T) {
		ps, _, links := linkedEffortServer(t, "acp1:a")
		ps.HandleAgentEffort(context.Background(), "acp1:a", settableEffort)
		if err := ps.SetAgentEffort(context.Background(), "max"); !errors.Is(err, ErrEffortNotOffered) {
			t.Fatalf("err = %v, want ErrEffortNotOffered", err)
		}
		if len(links[0].efforts) != 0 {
			t.Fatalf("the link was asked %v", links[0].efforts)
		}
	})

	t.Run("reaches the link that offered it as a typed call, without recording the switch", func(t *testing.T) {
		ps, _, links := linkedEffortServer(t, "acp1:a")
		ps.HandleAgentEffort(context.Background(), "acp1:a", settableEffort)
		if err := ps.SetAgentEffort(context.Background(), "low"); err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		if len(links[0].efforts) != 1 || links[0].efforts[0] != "s1/effort/low" {
			t.Fatalf("link efforts were %v, want [s1/effort/low]", links[0].efforts)
		}
		if got := ps.AgentEffort().CurrentEffort; got != "high" {
			t.Fatalf("CurrentEffort = %q, want the agent's last report", got)
		}
	})

	t.Run("reports a link that could not be reached", func(t *testing.T) {
		ps, _, links := linkedEffortServer(t, "acp1:a")
		links[0].unreachable = true
		ps.HandleAgentEffort(context.Background(), "acp1:a", settableEffort)
		if err := ps.SetAgentEffort(context.Background(), "low"); !errors.Is(err, ErrEffortSetNotDelivered) {
			t.Fatalf("err = %v, want ErrEffortSetNotDelivered", err)
		}
	})
}

// Only a native link is ever asked to change a setting: acp-gateway speaks
// only native ACP, so a report keyed to an MCP session is not reachable.
func TestAgentSettingsNeverReachAnMCPSession(t *testing.T) {
	replies := 0
	ps := permissionServer(t, &replies)
	ps.bus = eventbus.New()
	ps.agentModels = make(map[string]agentlink.ModelsSnapshot)
	ps.agentEffort = make(map[string]agentlink.EffortSnapshot)
	ps.agentConcurrency = make(map[string]agentlink.ConcurrencySnapshot)
	sessionID := connectedServer(t, ps, "claude-code")
	streamFor(ps, sessionID)

	ps.HandleAgentEffort(context.Background(), sessionID, settableEffort)
	if err := ps.SetAgentEffort(context.Background(), "low"); !errors.Is(err, ErrEffortSetNotDelivered) {
		t.Errorf("effort: err = %v, want ErrEffortSetNotDelivered", err)
	}

	ps.HandleAgentModels(context.Background(), sessionID, agentlink.ModelsReport{
		ConfigID: "model", CurrentModel: "a", CanSet: true, Models: []agentlink.Model{{ID: "a"}, {ID: "b"}},
	})
	if err := ps.SetAgentModel(context.Background(), "b"); !errors.Is(err, ErrModelSetNotDelivered) {
		t.Errorf("model: err = %v, want ErrModelSetNotDelivered", err)
	}

	limit, canSet := 4, true
	ps.HandleAgentConcurrency(context.Background(), sessionID, agentlink.ConcurrencyReport{MaxConcurrency: &limit, CanSet: &canSet})
	if err := ps.SetAgentConcurrency(context.Background(), 8); !errors.Is(err, ErrConcurrencySetNotDelivered) {
		t.Errorf("concurrency: err = %v, want ErrConcurrencySetNotDelivered", err)
	}
}

func TestPruneAgentEffortKeepsEverythingBetweenConnections(t *testing.T) {
	snapshots := map[string]agentlink.EffortSnapshot{"gone": {ConfigID: "effort"}}
	pruneAgentEffort(snapshots, nil)
	if len(snapshots) != 1 {
		t.Fatal("a report was dropped while no session was live")
	}
}

func TestManagerAgentEffort(t *testing.T) {
	ps, _, _ := linkedEffortServer(t, "acp1:a")
	ps.HandleAgentEffort(context.Background(), "acp1:a", settableEffort)
	m := &Manager{servers: map[int64]*WorkspaceServer{100: ps}}
	if got := m.AgentEffort(100); got == nil || got.CurrentEffort != "high" {
		t.Fatalf("AgentEffort(100) = %+v, want high", got)
	}
	if got := m.AgentEffort(200); got != nil {
		t.Fatalf("AgentEffort(200) = %+v, want nil", got)
	}
}
