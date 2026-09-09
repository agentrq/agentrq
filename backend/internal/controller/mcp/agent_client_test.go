package mcp

import (
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
)

// What is attached to a workspace comes off the live session rather than a
// cached notification, so unlike the models and commands beside it there is no
// way for this to describe a client that has gone.

func TestAgentClient(t *testing.T) {
	t.Run("names the connected client", func(t *testing.T) {
		replies := 0
		ps := permissionServer(t, &replies)
		ps.bus = eventbus.New()
		connectedServer(t, ps, "acp-gateway")
		ps.agentConnections.Add(1) // what the HTTP handler does for a live stream

		got := ps.AgentClient()
		if got == nil {
			t.Fatal("AgentClient() = nil with a session attached")
		}
		if got.Name != "acp-gateway" {
			t.Errorf("Name = %q, want acp-gateway", got.Name)
		}
		if got.Version != "0" {
			t.Errorf("Version = %q, want the version the client sent", got.Version)
		}
	})

	t.Run("has nothing to report when the attached client named nobody", func(t *testing.T) {
		// The loop runs and finds a session, but nothing worth showing. A name
		// is the only thing this field is for, so an anonymous client is the
		// same as none.
		replies := 0
		ps := permissionServer(t, &replies)
		ps.bus = eventbus.New()
		connectedServer(t, ps, "")
		ps.agentConnections.Add(1)

		if got := ps.AgentClient(); got != nil {
			t.Errorf("AgentClient() = %+v, want nil", got)
		}
	})

	t.Run("stops naming a client once its stream has gone", func(t *testing.T) {
		// An MCP session outlives the stream that carried it, so the session
		// list still holds the departed gateway. Reported against a workspace
		// the interface is calling offline, that name is worse than silence.
		replies := 0
		ps := permissionServer(t, &replies)
		ps.bus = eventbus.New()
		connectedServer(t, ps, "acp-gateway")
		ps.agentConnections.Add(1)
		if ps.AgentClient() == nil {
			t.Fatal("expected the client to be named while its stream is up")
		}

		ps.agentConnections.Add(-1) // the stream drops; the session lingers

		if got := ps.AgentClient(); got != nil {
			t.Errorf("AgentClient() = %+v after the stream went, want nil", got)
		}
	})

	t.Run("has nothing to report with no server running", func(t *testing.T) {
		ps := &WorkspaceServer{}
		if got := ps.AgentClient(); got != nil {
			t.Errorf("AgentClient() = %+v, want nil", got)
		}
	})

	t.Run("has nothing to report when nothing is attached", func(t *testing.T) {
		replies := 0
		ps := permissionServer(t, &replies)
		ps.bus = eventbus.New()
		// A server with no session: the same state a workspace is in between
		// connections.
		if got := ps.AgentClient(); got != nil {
			t.Errorf("AgentClient() = %+v, want nil", got)
		}
	})
}

func TestClientInfoFrom(t *testing.T) {
	t.Run("reads the name and version a client sent", func(t *testing.T) {
		got := clientInfoFrom(&mcp.InitializeParams{
			ClientInfo: &mcp.Implementation{Name: "  claude-code  ", Version: " 1.2.3 "},
		})
		if got == nil || got.Name != "claude-code" || got.Version != "1.2.3" {
			t.Errorf("got %+v, want the trimmed name and version", got)
		}
	})

	t.Run("reads nothing from a handshake that named nobody", func(t *testing.T) {
		for _, p := range []*mcp.InitializeParams{
			nil,
			{},
			{ClientInfo: &mcp.Implementation{}},
			{ClientInfo: &mcp.Implementation{Name: "   "}},
		} {
			if got := clientInfoFrom(p); got != nil {
				t.Errorf("clientInfoFrom(%+v) = %+v, want nil", p, got)
			}
		}
	})
}
