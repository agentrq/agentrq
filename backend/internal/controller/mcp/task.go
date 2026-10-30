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

// TaskCLIHint points a workspace's agent at agentrq-ws for attachments, which
// reads and writes the files on disk, so no base64 passes through the agent's
// context. It is for the workspace server only: agentrq-ws reads a workspace's
// .mcp.json.
const TaskCLIHint = " Prefer the shell for attachments to save tokens: `npx -y @agentrq/agentrq-ws@latest reply <taskId> \"text\" --attach a.png` " +
	"and `task create <title> --body @brief.md --attach a.png` upload files from disk, and `attachment get <attachmentId> --task <taskId> --out DIR` " +
	"downloads one there, printing its path; see `--help`."

// TaskParams is the input to the task tool.
type TaskParams struct {
	Action  string         `json:"action" jsonschema:"What to do: one of the actions this tool's description lists, or help for each action's fields and an example."`
	Payload map[string]any `json:"payload,omitempty" jsonschema:"The fields the chosen action works on, such as the ids it reads or the text it writes; help with payload {\"topic\": \"<action>\"} names them and gives an example. Leave it out for an action that takes none."`
}

// taskActions are the task tool's actions, each run by the handler of the
// tool it replaced, with that tool's parameters as its payload.
func (ps *WorkspaceServer) taskActions() []ToolAction {
	return []ToolAction{
		NewToolAction("create", "Create a task for the human or the agent, optionally scheduled. Returns its ID.", ps.handleCreateTask).
			WithExample(`{"title": "Review the release notes", "body": "Check the v0.9.20 notes before Friday.", "assignee": "human"}`),
		NewToolAction("get", "Get a task, or with no taskId the next not started task assigned to the agent (dequeues the work queue).", ps.handleGetTask).
			WithExample(`{"taskId": "0kGogHmaqvZ", "includeConversation": true, "limit": 10}`),
		NewToolAction("updateStatus", "Set a task's status: ongoing when you start, completed when you finish, blocked when you need the human.", ps.handleUpdateTaskStatus).
			WithExample(`{"taskId": "0kGogHmaqvZ", "status": "ongoing"}`),
		NewToolAction("reply", "Post a message to the task, optionally with attachments. The human sees only these.", ps.handleReply).
			WithExample(`{"taskId": "0kGogHmaqvZ", "text": "Tests pass; opening the pull request."}`),
		NewToolAction("elicit", "Ask the human a question and wait for the answer: {action, content}, action being accept, decline or cancel.", ps.handleElicit).
			WithExample(`{"taskId": "0kGogHmaqvZ", "message": "Which branch should I release from?", "mode": "form", "requestedSchema": {"type": "object", "properties": {"branch": {"type": "string", "enum": ["main", "release"]}}, "required": ["branch"]}}`),
		NewToolAction("getAttachment", "Get an attachment of a task: its name, type and public link, or its content as base64 or plain text.", ps.handleGetAttachment).
			WithExample(`{"taskId": "0kGogHmaqvZ", "attachmentId": "att_1"}`),
	}
}

func (ps *WorkspaceServer) handleTask(ctx context.Context, req *mcp.CallToolRequest, p TaskParams) (*mcp.CallToolResult, any, error) {
	return ps.runTool(ctx, req, "task", ps.taskActions(), p.Action, p.Payload)
}
