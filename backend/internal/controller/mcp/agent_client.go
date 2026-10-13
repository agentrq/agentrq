// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"context"
	"strings"

	zlog "github.com/rs/zerolog/log"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// AgentIdentityNotificationMethod is the channel notification a gateway sends
// to say which agent is behind it.
//
// A gateway's MCP client is the gateway, so without this a workspace can only
// ever name the bridge. The agent named itself during its own ACP handshake and
// this carries that across.
const AgentIdentityNotificationMethod = "notifications/claude/channel/agent"

// AgentIdentityParams is the payload of an agent notification.
//
// snake_case, unlike the REST surface: this is the wire format the gateways
// send (acp-gateway's `sendAgentToWorkspace`), the same reasoning the models
// and commands payloads beside it record.
type AgentIdentityParams struct {
	TaskID    string `json:"task_id"`
	SessionID string `json:"session_id"`
	Name      string `json:"name"`
	Title     string `json:"title"`
	Version   string `json:"version"`
}

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
	// Name is the agent behind the connection when a gateway has said which one
	// it is driving, and otherwise the client's own name for itself —
	// "claude-code" for Claude Code attached directly, "acp-gateway" for a
	// gateway too old to report its agent.
	Name string
	// Title is a longer human-readable name, when the agent gave one.
	Title string
	// Version is what it reported, when it reported one.
	Version string
}

// HandleAgentIdentity records which agent a gateway says it is driving.
//
// Keyed by the MCP transport session, deliberately, and *not* by the payload's
// session_id — the trap agent_commands.go documents at length. The two are
// different namespaces: the transport session is this server's connection to
// the gateway, while session_id is the gateway's own ACP session with the agent
// behind it. Keying on the payload would file every identity under an ID the
// session list never reports.
//
// A nameless payload is ignored rather than recorded. The name is the entire
// point of this notification, and an empty one would replace a perfectly good
// client name with nothing.
func (ps *WorkspaceServer) HandleAgentIdentity(ctx context.Context, sessionID string, p AgentIdentityParams) {
	name := strings.TrimSpace(p.Name)
	if name == "" {
		zlog.Debug().Str("session_id", sessionID).Msg("ignoring an agent notification that named nobody")
		return
	}

	ps.agentIdentitiesMu.Lock()
	ps.agentIdentities[sessionID] = AgentClientInfo{
		Name:    name,
		Title:   strings.TrimSpace(p.Title),
		Version: strings.TrimSpace(p.Version),
	}
	// Same housekeeping as the commands next door: nothing else drops these, so
	// a long-lived workspace whose gateway reconnects repeatedly would collect
	// one entry per session for the life of the process.
	pruneAgentIdentities(ps.agentIdentities, ps.liveSessionIDs())
	ps.agentIdentitiesMu.Unlock()

	zlog.Debug().Str("session_id", sessionID).Str("agent", name).Msg("recorded the agent behind the gateway")
}

// reportedAgent returns what a session said it was driving, if anything.
func (ps *WorkspaceServer) reportedAgent(sessionID string) *AgentClientInfo {
	ps.agentIdentitiesMu.RLock()
	defer ps.agentIdentitiesMu.RUnlock()
	if info, ok := ps.agentIdentities[sessionID]; ok {
		return &info
	}
	return nil
}

// AgentClient reports what is attached to this workspace, or nil when nothing
// is.
//
// The first session that named itself wins. More than one client can attach to
// a workspace, but the interface has one line to say what is connected, and the
// session order the server reports is at least stable between two calls — the
// same reason pickAgentCommands iterates the live list rather than a map.
//
// A session that no longer holds a stream is passed over. Sessions linger after
// their stream drops, so without this the workspace would keep naming a gateway
// that left — beside an "agent offline" indicator when it was the only one, and
// in place of a gateway that is still there when it was not.
//
// Native links come after every MCP session, and are named only by what they
// reported: a link has no handshake of its own to fall back on.
func (ps *WorkspaceServer) AgentClient() *AgentClientInfo {
	if ps.mcpServer != nil {
		for sess := range ps.mcpServer.Sessions() {
			if !ps.isStreaming(sess.ID()) {
				continue
			}
			// What a bridge says it is driving wins over what the bridge calls
			// itself, because the question is which agent is attached.
			if agent := ps.reportedAgent(sess.ID()); agent != nil {
				return agent
			}
			info := clientInfoFrom(sess.InitializeParams())
			if info == nil || isBridge(info.Name) {
				continue
			}
			return info
		}
	}
	for _, id := range ps.linkIDs() {
		if agent := ps.reportedAgent(id); agent != nil {
			return agent
		}
	}
	return nil
}

// callingAgent names the agent behind a tool call, and the model it last said
// it was running, for the task history to record.
//
// The same precedence as AgentClient, but for the calling session only: a
// status change belongs to whoever made it, not to whichever agent happens to
// be listed first. A bridge that has not said what it drives names nobody.
func (ps *WorkspaceServer) callingAgent(req *mcp.CallToolRequest) entity.TaskAgent {
	if req == nil || req.Session == nil {
		return entity.TaskAgent{}
	}
	return ps.sessionAgent(req.Session.ID(), clientInfoFrom(req.Session.InitializeParams()))
}

// sessionAgent names the agent behind one session and the model it last said
// it was running: what the session reported first, else its client's own
// name unless that is a bridge. info is nil for a native link, which has no
// handshake.
func (ps *WorkspaceServer) sessionAgent(id string, info *AgentClientInfo) entity.TaskAgent {
	var agent entity.TaskAgent
	if reported := ps.reportedAgent(id); reported != nil {
		agent.Name = reported.Name
	} else if info != nil && !isBridge(info.Name) {
		agent.Name = info.Name
	}
	if agent.Name == "" {
		return agent
	}

	ps.agentModelsMu.RLock()
	snapshot, ok := ps.agentModels[id]
	ps.agentModelsMu.RUnlock()
	if ok {
		agent.Model = snapshot.currentModelName()
	}
	return agent
}

// withTaskAgent attaches the calling agent, registered, for a status change to
// record.
func (ps *WorkspaceServer) withTaskAgent(ctx context.Context, req *mcp.CallToolRequest) context.Context {
	agent := ps.callingAgent(req)
	if agent.Name == "" || ps.registerTaskAgent == nil {
		return ctx
	}
	return entity.WithTaskAgent(ctx, ps.registerTaskAgent(ctx, agent))
}

// linkTaskAgentContext attributes work done for a link to the agent behind
// it, as withTaskAgent does for an MCP tool call.
func (ps *WorkspaceServer) linkTaskAgentContext(ctx context.Context, linkID string) context.Context {
	agent := ps.sessionAgent(linkID, nil)
	if agent.Name == "" || ps.registerTaskAgent == nil {
		return ctx
	}
	return entity.WithTaskAgent(ctx, ps.registerTaskAgent(ctx, agent))
}

// bridgeClients names the MCP clients that are a way through to an agent rather
// than an agent themselves, keyed by the lowercased name from the handshake.
//
// Naming one of these answers the wrong question. "acp-gateway" tells a human
// how their agent is plumbed in, not what it is, and it appears in the one
// place the interface has to say which agent is working — so when a bridge has
// not said what is behind it, the workspace says nothing rather than naming the
// pipe. A client that is not a bridge is the agent, and is named.
//
// The same shape as stopCapableClients above, and for the same reason: the
// handshake name is the only thing distinguishing these clients, and there is
// exactly one of them today.
var bridgeClients = map[string]bool{
	"acp-gateway": true,
}

func isBridge(clientName string) bool {
	return bridgeClients[strings.ToLower(strings.TrimSpace(clientName))]
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

// pruneAgentIdentities drops what sessions that have gone away reported.
//
// No live sessions at all means the server is between connections rather than
// that every entry is stale, so nothing is dropped there — the same reasoning
// pruneAgentCommands records, and the same reason: an early call can observe
// the map before its own session is visible.
//
// The caller must hold the write lock.
func pruneAgentIdentities(identities map[string]AgentClientInfo, live []string) {
	if len(live) == 0 {
		return
	}

	keep := make(map[string]bool, len(live))
	for _, id := range live {
		keep[id] = true
	}
	for id := range identities {
		if !keep[id] {
			delete(identities, id)
		}
	}
}
