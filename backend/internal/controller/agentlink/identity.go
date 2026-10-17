// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package agentlink

// IdentityReport is the agent a native link says it is driving.
type IdentityReport struct {
	TaskID    string
	SessionID string
	Name      string
	Title     string
	Version   string
}

// ClientInfo is what the client attached to a workspace said it was when
// it connected.
//
// Not a cached notification like the models and commands beside it — it is read
// off the session itself — but that alone is not enough to keep it current. An
// MCP session outlives the stream that carried it: after a client goes away
// its session is still listed, so a reader that trusted the session list would
// go on naming a client that had already left. AgentClient checks the same
// signal the workspace uses to say an agent is live.
type ClientInfo struct {
	// Name is the agent a native link said it is driving, or an MCP client's
	// own name for itself: "claude-code" for Claude Code attached directly.
	Name string
	// Title is a longer human-readable name, when the agent gave one.
	Title string
	// Version is what it reported, when it reported one.
	Version string
}
