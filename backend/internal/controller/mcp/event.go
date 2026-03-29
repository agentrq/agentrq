package mcp

import (
	"fmt"
)

type Action int

const (
	ActionMCPToolCall Action = iota + 1
	ActionMCPMethodCall
	ActionMCPNotification
)

func (a Action) String() string {
	switch a {
	case ActionMCPToolCall:
		return "tool_call"
	case ActionMCPMethodCall:
		return "method_call"
	case ActionMCPNotification:
		return "notification"
	}
	return "unknown"
}

type MCPEvent struct {
	Action      Action `json:"action"`
	WorkspaceID int64  `json:"workspace_id"`
	UserID      int64  `json:"user_id"`
	Method      string `json:"method,omitempty"`
	ToolName    string `json:"tool_name,omitempty"`
	Actor       uint8  `json:"actor"` // 1: Human, 2: Agent
}

func (e MCPEvent) String() string {
	return fmt.Sprintf("mcp_event:%s", e.Action.String())
}
