package mcp

import (
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// AgentClientInfo is what the client attached to a workspace said it was when
// it connected.
//
// Not a cached notification like the models and commands beside it — it is read
// off the session itself — but that alone is not enough to keep it current. An
// MCP session outlives the stream that carried it: after a gateway goes away
// its session is still listed, so a reader that trusted the session list would
// go on naming a client that had already left. AgentClient checks the same
// signal the workspace uses to say an agent is live.
type AgentClientInfo struct {
	// Name is the client's own name for itself — "claude-code" for Claude Code
	// attached directly, "acp-gateway" for anything reached through the gateway.
	Name string
	// Version is what it reported, when it reported one.
	Version string
}

// AgentClient reports what is attached to this workspace, or nil when nothing
// is.
//
// The first session that named itself wins. More than one client can attach to
// a workspace, but the interface has one line to say what is connected, and the
// session order the server reports is at least stable between two calls — the
// same reason pickAgentCommands iterates the live list rather than a map.
//
// Nothing is reported unless an agent is actually connected. Sessions linger
// after their stream drops, so without this the workspace would keep naming the
// gateway that left — and naming it beside an "agent offline" indicator, which
// is worse than saying nothing.
func (ps *WorkspaceServer) AgentClient() *AgentClientInfo {
	if ps.mcpServer == nil || !ps.IsAgentConnected() {
		return nil
	}
	for sess := range ps.mcpServer.Sessions() {
		if info := clientInfoFrom(sess.InitializeParams()); info != nil {
			return info
		}
	}
	return nil
}

// clientInfoFrom reads a client's identity out of the initialize handshake.
//
// This is the handshake SEP-2575 eventually replaces with per-request metadata,
// which is why the telemetry path deliberately does not read it. It is still
// the right source here: this server is pinned below that revision for as long
// as it needs to push notifications, and the question being asked — "what is
// attached to this workspace" — is about the session rather than about any one
// request. When the handshake goes, this reads whatever replaces it; until
// then, a request-scoped identity would answer a different question.
func clientInfoFrom(p *mcp.InitializeParams) *AgentClientInfo {
	if p == nil || p.ClientInfo == nil {
		return nil
	}
	name := strings.TrimSpace(p.ClientInfo.Name)
	if name == "" {
		return nil
	}
	return &AgentClientInfo{Name: name, Version: strings.TrimSpace(p.ClientInfo.Version)}
}
