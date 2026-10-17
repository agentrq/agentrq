// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/agentrq/agentrq/backend/internal/controller/agentlink"
	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
)

// connectedIdentityServer is a workspace with a session attached and its
// connection counted, which is what AgentClient requires before it says
// anything at all.
func connectedIdentityServer(t *testing.T, clientName string) (*WorkspaceServer, string, func()) {
	t.Helper()
	replies := 0
	ps := permissionServer(t, &replies)
	ps.bus = eventbus.New()
	ps.agentIdentities = make(map[string]agentlink.ClientInfo)
	sessionID := connectedServer(t, ps, clientName)
	return ps, sessionID, streamFor(ps, sessionID)
}

// What is attached to a workspace comes off the live session rather than a
// cached notification, so unlike the models and commands beside it there is no
// way for this to describe a client that has gone.

func TestAgentClient(t *testing.T) {
	t.Run("names a client that is itself the agent", func(t *testing.T) {
		replies := 0
		ps := permissionServer(t, &replies)
		ps.bus = eventbus.New()
		streamFor(ps, connectedServer(t, ps, "claude-code"))

		got := ps.AgentClient()
		if got == nil {
			t.Fatal("AgentClient() = nil with a session attached")
		}
		if got.Name != "claude-code" {
			t.Errorf("Name = %q, want claude-code", got.Name)
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
		streamFor(ps, connectedServer(t, ps, ""))

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
		drop := streamFor(ps, connectedServer(t, ps, "claude-code"))
		if ps.AgentClient() == nil {
			t.Fatal("expected the client to be named while its stream is up")
		}

		drop() // the stream goes; the session lingers

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

// A native link has no handshake, so these cover the report that says which
// agent is behind it.

func TestHandleAgentIdentity(t *testing.T) {
	t.Run("trims what the gateway sent", func(t *testing.T) {
		ps := &WorkspaceServer{agentIdentities: make(map[string]agentlink.ClientInfo)}
		ps.HandleAgentIdentity(context.Background(), "sess-1", agentlink.IdentityReport{
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
		ps := &WorkspaceServer{agentIdentities: make(map[string]agentlink.ClientInfo)}
		ps.HandleAgentIdentity(context.Background(), "sess-1", agentlink.IdentityReport{Name: "   "})

		if len(ps.agentIdentities) != 0 {
			t.Errorf("recorded %+v, want nothing", ps.agentIdentities)
		}
	})

	t.Run("the newest report replaces the previous one", func(t *testing.T) {
		ps := &WorkspaceServer{agentIdentities: make(map[string]agentlink.ClientInfo)}
		ps.HandleAgentIdentity(context.Background(), "sess-1", agentlink.IdentityReport{Name: "codex"})
		ps.HandleAgentIdentity(context.Background(), "sess-1", agentlink.IdentityReport{Name: "gemini"})

		if got := ps.agentIdentities["sess-1"].Name; got != "gemini" {
			t.Errorf("Name = %q, want the second report", got)
		}
	})
}

func TestPruneAgentIdentities(t *testing.T) {
	t.Run("drops the sessions that have gone", func(t *testing.T) {
		identities := map[string]agentlink.ClientInfo{"a": {Name: "x"}, "b": {Name: "y"}}
		pruneAgentIdentities(identities, []string{"b"})

		if len(identities) != 1 {
			t.Fatalf("identities = %+v, want only b", identities)
		}
		if _, ok := identities["b"]; !ok {
			t.Errorf("kept the wrong one: %+v", identities)
		}
	})

	t.Run("keeps everything when nothing is connected", func(t *testing.T) {
		identities := map[string]agentlink.ClientInfo{"a": {Name: "x"}}
		pruneAgentIdentities(identities, nil)

		if len(identities) != 1 {
			t.Errorf("identities = %+v, want a kept", identities)
		}
	})
}

// A client may hold more than one stream at a time, so the session is only off
// the air when the last of them has gone.
func TestStreamingSessionCounting(t *testing.T) {
	ps := &WorkspaceServer{}

	ps.addStreamingSession("sess-1")
	ps.addStreamingSession("sess-1")
	if !ps.isStreaming("sess-1") {
		t.Fatal("expected the session to be streaming")
	}

	ps.removeStreamingSession("sess-1")
	if !ps.isStreaming("sess-1") {
		t.Error("one stream closing must not take the session off the air")
	}

	ps.removeStreamingSession("sess-1")
	if ps.isStreaming("sess-1") {
		t.Error("the last stream closing should")
	}

	// Removing what was never there is not an error; a handler may unwind a
	// request it never counted.
	ps.removeStreamingSession("never-seen")
	if ps.isStreaming("never-seen") {
		t.Error("a session nobody opened is not streaming")
	}
}
