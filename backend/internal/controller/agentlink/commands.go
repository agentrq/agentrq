// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package agentlink

// Command is one slash command the connected agent offers.
type Command struct {
	// Name is typed as /<name> at the start of a message.
	Name string `json:"name"`
	// Description says what the command does, and may be empty: the name is
	// what gets invoked, and an agent is not obliged to explain itself.
	Description string `json:"description,omitempty"`
	// Hint describes the argument, for the commands that take one.
	Hint string `json:"hint,omitempty"`
}

// CommandsReport is the payload of a commands notification.
//
// snake_case, unlike the REST surface: this is the wire format the gateways
// already send (acp-gateway's `sendCommandsToWorkspace`), and renaming the
// fields here would break every gateway in the field to satisfy a convention
// that governs the HTTP API rather than this channel.
type CommandsReport struct {
	TaskID    string    `json:"task_id"`
	SessionID string    `json:"session_id"`
	Commands  []Command `json:"commands"`
}

// CommandsSnapshot is what one connected session last reported.
//
// SessionID is the *agent's* session — see the note on keying in
// HandleAgentCommands — and is kept because it is the only thing tying a
// snapshot back to the conversation it came from when more than one session is
// connected.
type CommandsSnapshot struct {
	SessionID string
	Commands  []Command
}
