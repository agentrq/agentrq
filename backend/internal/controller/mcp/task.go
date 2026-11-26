// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package mcp

import (
	"context"

	"github.com/agentrq/agentrq/backend/internal/service/mcp"
)

// TaskToolDescription is the workspace server's task tool's description.
const TaskToolDescription = "Work on this workspace's tasks and talk to the human in them. Two inputs: action, and payload, the action's fields. " +
	"Run action help for the actions, and help with payload {\"topic\": \"<action>\"} for one's payload. " +
	"get {taskId}: a task, or with no taskId the next not started task assigned to the agent. " +
	"updateStatus {taskId, status}: 'ongoing' when you start, 'completed' when you finish, 'blocked' when you need the human. " +
	"reply {taskId, text}: a message to the task; the human sees only these. Add `ui`, an A2UI card (help task.reply), when it has structure: progress, results, a choice. " +
	"elicit {taskId, message, mode}: asks the human and waits for the answer. getAttachment {taskId, attachmentId}: a task's attachment. create {title, body}: a new task. " +
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
		NewToolAction("reply", "Post a message to the task, optionally with attachments and an A2UI card. The human sees only these.", ps.handleReply).
			WithDetails("Send a ui card, not text alone, whenever the message has structure: progress steps, results, a table, a choice. text stays the one-line fallback for push, email and Slack. Guide: skill://agentrq/references/a2ui.md.").
			WithExample(`{"taskId": "0kGogHmaqvZ", "text": "Build passed: 412 tests.", "attachments": [{"filename": "chart.png", "mimeType": "image/png", "data": "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="}], "ui": [{"version": "1.0", "createSurface": {"surfaceId": "build", "catalogId": "https://a2ui.org/specification/v1_0/catalogs/basic/catalog.json"}}, {"version": "1.0", "updateComponents": {"surfaceId": "build", "components": [{"id": "root", "component": "Card", "child": "col"}, {"id": "col", "component": "Column", "children": ["title", "chart"]}, {"id": "title", "component": "Text", "text": "## Build passed"}, {"id": "chart", "component": "Image", "url": "attachment:chart.png"}]}}]}`),
		NewToolAction("elicit", "Ask the human a question and wait for the answer: {action, content}, action being accept, decline or cancel.", ps.handleElicit).
			WithDetails("mode a2ui draws the question as a card, ui being reply's, with no media: a Button's click answers, content being {name, context, dataModel}.").
			WithExample(`{"taskId": "0kGogHmaqvZ", "message": "Which branch should I release from?", "mode": "form", "requestedSchema": {"type": "object", "properties": {"branch": {"type": "string", "enum": ["main", "release"]}}, "required": ["branch"]}}`),
		NewToolAction("getAttachment", "Get an attachment of a task: its name, type and public link, or its content as base64 or plain text.", ps.handleGetAttachment).
			WithExample(`{"taskId": "0kGogHmaqvZ", "attachmentId": "att_1"}`),
	}
}

func (ps *WorkspaceServer) handleTask(ctx context.Context, req *mcp.CallToolRequest, p TaskParams) (*mcp.CallToolResult, any, error) {
	return ps.runTool(ctx, req, "task", ps.taskActions(), p.Action, p.Payload)
}
