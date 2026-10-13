// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TaskToolDescription is the workspace server's task tool's description.
const TaskToolDescription = "Work on this workspace's tasks and talk to the human in them. Two inputs: action, and payload, the action's fields. " +
	"Run action help for the actions, and help with payload {\"topic\": \"<action>\"} for one's payload. " +
	"get: a task, or with no taskId the next not started task assigned to the agent. " +
	"updateStatus: 'ongoing' when you start, 'completed' when you finish, 'blocked' when you need the human. " +
	"reply: a message to the task; the human sees only these. " +
	"elicit: asks the human and waits for the answer. getAttachment: a task's attachment. create: a new task. " +
	"taskId is the chat_id a task arrives with."

// TaskParams is the input to the task tool.
type TaskParams struct {
	Action  string         `json:"action" jsonschema:"enum: create, get, updateStatus, reply, elicit, getAttachment, help"`
	Payload map[string]any `json:"payload,omitempty" jsonschema:"The action's fields, as help prints them. Leave it out when the action takes none."`
}

// taskActions are the task tool's actions, each run by the handler of the
// tool it replaced, with that tool's parameters as its payload.
func (ps *WorkspaceServer) taskActions() []ToolAction {
	return []ToolAction{
		NewToolAction("create", "Create a task for the human or the agent, optionally scheduled. Returns its ID.", ps.handleCreateTask),
		NewToolAction("get", "Get a task, or with no taskId the next not started task assigned to the agent (dequeues the work queue).", ps.handleGetTask),
		NewToolAction("updateStatus", "Set a task's status: ongoing when you start, completed when you finish, blocked when you need the human.", ps.handleUpdateTaskStatus),
		NewToolAction("reply", "Post a message to the task, optionally with attachments. The human sees only these.", ps.handleReply),
		NewToolAction("elicit", "Ask the human a question and wait for the answer: {action, content}, action being accept, decline or cancel.", ps.handleElicit),
		NewToolAction("getAttachment", "Get an attachment of a task: its name, type and public link, or its content as base64.", ps.handleGetAttachment),
	}
}

// legacyTaskTools are the workspace tools the task tool replaced.
var legacyTaskTools = map[string]LegacyTool{
	"createTask":       {Tool: "task", Action: "create"},
	"getTask":          {Tool: "task", Action: "get"},
	"updateTaskStatus": {Tool: "task", Action: "updateStatus"},
	"reply":            {Tool: "task", Action: "reply", Renamed: map[string]string{"chatId": "taskId"}},
	"elicit":           {Tool: "task", Action: "elicit"},
	"getAttachment":    {Tool: "task", Action: "getAttachment"},
}

func (ps *WorkspaceServer) handleTask(ctx context.Context, req *mcp.CallToolRequest, p TaskParams) (*mcp.CallToolResult, any, error) {
	actions := ps.taskActions()
	if !IsAction(actions, p.Action) {
		ps.emitTelemetry(ctx, ActionMCPToolCall, "task", clientIdentityFromRequest(req))
	}
	return RunAction(ctx, req, "task", actions, p.Action, p.Payload)
}
