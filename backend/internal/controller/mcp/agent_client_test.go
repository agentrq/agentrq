package mcp

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
)

// connectedIdentityServer is a workspace with a session attached and its
// connection counted, which is what AgentClient requires before it says
// anything at all.
func connectedIdentityServer(t *testing.T, clientName string) (*WorkspaceServer, string) {
	t.Helper()
	replies := 0
	ps := permissionServer(t, &replies)
	ps.bus = eventbus.New()
	ps.agentIdentities = make(map[string]AgentClientInfo)
	sessionID := connectedServer(t, ps, clientName)
	ps.agentConnections.Add(1) // what the HTTP handler does for a live stream
	return ps, sessionID
}

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

// The gateway's MCP client is the gateway, so left to the handshake alone a
// workspace can only ever name the bridge. These cover the notification that
// tells it what is really behind there.

const gatewayAgentFrame = `{
  "jsonrpc": "2.0",
  "method": "notifications/claude/channel/agent",
  "params": {
    "task_id": "0iF1U1qQwS1",
    "session_id": "01a07404-01a7-7033-a58f-47b119036378",
    "name": "codex",
    "title": "Codex CLI",
    "version": "1.10.0"
  }
}`

func TestHandleCustomNotificationRoutesAgentIdentity(t *testing.T) {
	ps := &WorkspaceServer{agentIdentities: make(map[string]AgentClientInfo)}
	ps.HandleCustomNotification(context.Background(), "transport-session", []byte(gatewayAgentFrame))

	// Keyed by the MCP transport session, not the ACP session in the payload.
	got, ok := ps.agentIdentities["transport-session"]
	if !ok {
		t.Fatalf("the agent was not recorded; map = %+v", ps.agentIdentities)
	}
	if _, wrong := ps.agentIdentities["01a07404-01a7-7033-a58f-47b119036378"]; wrong {
		t.Error("keyed by the payload's ACP session, which the session list never reports")
	}
	if got.Name != "codex" || got.Title != "Codex CLI" || got.Version != "1.10.0" {
		t.Errorf("recorded = %+v", got)
	}
}

func TestHandleAgentIdentity(t *testing.T) {
	t.Run("trims what the gateway sent", func(t *testing.T) {
		ps := &WorkspaceServer{agentIdentities: make(map[string]AgentClientInfo)}
		ps.HandleAgentIdentity(context.Background(), "sess-1", AgentIdentityParams{
			Name: "  gemini  ", Title: "  Gemini CLI  ", Version: "  2.0  ",
		})

		got := ps.agentIdentities["sess-1"]
		if got.Name != "gemini" || got.Title != "Gemini CLI" || got.Version != "2.0" {
			t.Errorf("recorded = %+v", got)
		}
	})

	t.Run("ignores a payload that named nobody", func(t *testing.T) {
		// The name is the entire point of this notification, and an empty one
		// would replace a perfectly good client name with nothing.
		ps := &WorkspaceServer{agentIdentities: make(map[string]AgentClientInfo)}
		ps.HandleAgentIdentity(context.Background(), "sess-1", AgentIdentityParams{Name: "   "})

		if len(ps.agentIdentities) != 0 {
			t.Errorf("recorded %+v, want nothing", ps.agentIdentities)
		}
	})

	t.Run("the newest report replaces the previous one", func(t *testing.T) {
		ps := &WorkspaceServer{agentIdentities: make(map[string]AgentClientInfo)}
		ps.HandleAgentIdentity(context.Background(), "sess-1", AgentIdentityParams{Name: "codex"})
		ps.HandleAgentIdentity(context.Background(), "sess-1", AgentIdentityParams{Name: "gemini"})

		if got := ps.agentIdentities["sess-1"].Name; got != "gemini" {
			t.Errorf("Name = %q, want the second report", got)
		}
	})
}

func TestAgentClientPrefersTheReportedAgent(t *testing.T) {
	t.Run("names the agent rather than the gateway in front of it", func(t *testing.T) {
		ps, sessionID := connectedIdentityServer(t, "acp-gateway")
		ps.HandleAgentIdentity(context.Background(), sessionID, AgentIdentityParams{
			Name: "codex", Title: "Codex CLI", Version: "1.10.0",
		})

		got := ps.AgentClient()
		if got == nil {
			t.Fatal("AgentClient() = nil")
		}
		if got.Name != "codex" || got.Title != "Codex CLI" || got.Version != "1.10.0" {
			t.Errorf("AgentClient() = %+v, want the agent behind the gateway", got)
		}
	})

	t.Run("falls back to the client's own name when no agent was reported", func(t *testing.T) {
		// A gateway too old to send the notification, and every client that is
		// not a gateway at all. Both keep working exactly as before.
		ps, _ := connectedIdentityServer(t, "claude-code")

		got := ps.AgentClient()
		if got == nil || got.Name != "claude-code" {
			t.Errorf("AgentClient() = %+v, want the client's own name", got)
		}
	})

	t.Run("still says nothing once the stream has gone", func(t *testing.T) {
		// The reported agent must not outlive its connection either.
		ps, sessionID := connectedIdentityServer(t, "acp-gateway")
		ps.HandleAgentIdentity(context.Background(), sessionID, AgentIdentityParams{Name: "codex"})

		ps.agentConnections.Add(-1)

		if got := ps.AgentClient(); got != nil {
			t.Errorf("AgentClient() = %+v after the stream went, want nil", got)
		}
	})
}

func TestPruneAgentIdentities(t *testing.T) {
	t.Run("drops the sessions that have gone", func(t *testing.T) {
		identities := map[string]AgentClientInfo{"a": {Name: "x"}, "b": {Name: "y"}}
		pruneAgentIdentities(identities, []string{"b"})

		if len(identities) != 1 {
			t.Fatalf("identities = %+v, want only b", identities)
		}
		if _, ok := identities["b"]; !ok {
			t.Errorf("kept the wrong one: %+v", identities)
		}
	})

	t.Run("keeps everything when nothing is connected", func(t *testing.T) {
		identities := map[string]AgentClientInfo{"a": {Name: "x"}}
		pruneAgentIdentities(identities, nil)

		if len(identities) != 1 {
			t.Errorf("identities = %+v, want a kept", identities)
		}
	})
}
