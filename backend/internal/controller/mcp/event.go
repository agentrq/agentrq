// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"fmt"
)

type Action int

const (
	ActionMCPToolCall Action = iota + 1
	// A protocol method that isn't a tool call — a resource read or a prompt
	// get. Kept separate from ActionMCPToolCall so a supervisor agent reading a
	// guide resource doesn't inflate the same count as it calling a tool.
	ActionMCPMethodCall
	ActionMCPNotification
	ActionMCPConnect
	// The backend asking the agent's session for a clean context ahead of a
	// task push. Emitted once per successful /clear, never per attempt — a
	// failed one changed nothing worth counting.
	ActionMCPClearContext
	// A native agent's plan for a task, written or replaced. Emitted once
	// the plan is stored, never for a failed write.
	ActionMCPPlanUpdate
)

func (a Action) String() string {
	switch a {
	case ActionMCPToolCall:
		return "tool_call"
	case ActionMCPMethodCall:
		return "method_call"
	case ActionMCPNotification:
		return "notification"
	case ActionMCPConnect:
		return "connect"
	case ActionMCPClearContext:
		return "clear_context"
	case ActionMCPPlanUpdate:
		return "plan_update"
	}
	return "unknown"
}

type MCPEvent struct {
	Action        Action `json:"action"`
	WorkspaceID   int64  `json:"workspaceId"`
	UserID        int64  `json:"userId"`
	Method        string `json:"method,omitempty"`
	ToolName      string `json:"toolName,omitempty"`
	Actor         uint8  `json:"actor"`                   // 1: Human, 2: Agent, 3: Extension
	ClientID      int64  `json:"clientId,omitempty"`      // xxhash64(name+version) of the calling MCP client, 0 if unknown
	ClientName    string `json:"clientName,omitempty"`    // e.g. "claude-code", empty if unknown
	ClientVersion string `json:"clientVersion,omitempty"` // empty if unknown
}

func (e MCPEvent) String() string {
	return fmt.Sprintf("mcp_event:%s", e.Action.String())
}
