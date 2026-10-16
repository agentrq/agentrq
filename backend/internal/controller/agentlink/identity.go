// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package agentlink

// IdentityReport is the payload of an agent notification.
//
// snake_case, unlike the REST surface: this is the wire format the gateways
// send (acp-gateway's `sendAgentToWorkspace`), the same reasoning the models
// and commands payloads beside it record.
type IdentityReport struct {
	TaskID    string `json:"task_id"`
	SessionID string `json:"session_id"`
	Name      string `json:"name"`
	Title     string `json:"title"`
	Version   string `json:"version"`
}

// ClientInfo is what the client attached to a workspace said it was when
// it connected.
//
// Not a cached notification like the models and commands beside it — it is read
// off the session itself — but that alone is not enough to keep it current. An
// MCP session outlives the stream that carried it: after a gateway goes away
// its session is still listed, so a reader that trusted the session list would
// go on naming a client that had already left. AgentClient checks the same
// signal the workspace uses to say an agent is live.
type ClientInfo struct {
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
