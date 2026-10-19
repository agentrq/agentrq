// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package coremcp

import (
	"context"

	mcpevent "github.com/agentrq/agentrq/backend/internal/controller/mcp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// taskToolDescription is the supervisor's task tool's description.
const taskToolDescription = "Read and manage the tasks of every workspace on the account. Two inputs: action, and payload, the action's fields. " +
	"Run action help for the actions, and help with payload {\"topic\": \"<action>\"} for one's payload. " +
	"list and listAll list one workspace's tasks or every workspace's; create, get, reply and delete do what they say; " +
	"respond answers the permission request a task is waiting on; updateStatus, updateSortingOrder, updateAssignee, updateAllowAll and updateSchedule change one thing about a task; " +
	"getAttachment gets a task's attachment. delete cannot be undone: to stop a scheduled task without losing its history, updateStatus it to rejected instead."

// TaskParams is the input to the task tool.
type TaskParams struct {
	Action  string         `json:"action" jsonschema:"enum: list, listAll, create, get, reply, respond, updateStatus, updateSortingOrder, updateAssignee, updateAllowAll, updateSchedule, delete, getAttachment, help"`
	Payload map[string]any `json:"payload,omitempty" jsonschema:"The action's fields, as help prints them."`
}

// taskActions are the task tool's actions, each run by the handler of the
// tool it replaced, with that tool's parameters as its payload.
func (s *WorkspaceServer) taskActions() []mcpevent.ToolAction {
	return []mcpevent.ToolAction{
		mcpevent.NewToolAction("list", "List a workspace's tasks.", s.handleListTasks).
			WithDetails("filter, status and createdBy narrow the list; limit (default 5, at most 50) and offset page through it.").
			WithExample(`{"workspaceId": "0ZzhYQG2qtl", "status": "blocked", "limit": 10}`),
		mcpevent.NewToolAction("listAll", "List the tasks of every workspace.", s.handleListAllTasks).
			WithDetails("filter, status and createdBy narrow the list; limit (default 5, at most 50) and offset page through it.").
			WithExample(`{"status": "ongoing"}`),
		mcpevent.NewToolAction("create", "Create a task in a workspace, assigned to the human unless assignee says otherwise.", s.handleCreateTask).
			WithDetails("Write the full context into body: the receiving agent has nothing else. cronSchedule makes it a scheduled task: a recurring one runs at most hourly, a one-time one fixes dom and month.").
			WithExample(`{"workspaceId": "0ZzhYQG2qtl", "title": "Release v0.9.20", "body": "Tag main and publish the release notes.", "assignee": "agent", "clearContext": true}`),
		mcpevent.NewToolAction("get", "Get a task.", s.handleGetTask).
			WithExample(`{"workspaceId": "0ZzhYQG2qtl", "taskId": "0kGogHmaqvZ"}`),
		mcpevent.NewToolAction("reply", "Post a message to a task's thread.", s.handleReplyToTask).
			WithExample(`{"workspaceId": "0ZzhYQG2qtl", "taskId": "0kGogHmaqvZ", "text": "Looks good, go ahead."}`),
		mcpevent.NewToolAction("respond", "Answer the permission request a task is waiting on: allow, allow_all, reject, or text to reply without deciding.", s.handleRespondToTask).
			WithExample(`{"workspaceId": "0ZzhYQG2qtl", "taskId": "0kGogHmaqvZ", "action": "allow"}`),
		mcpevent.NewToolAction("updateStatus", "Set a task's status.", s.handleUpdateTaskStatus).
			WithExample(`{"workspaceId": "0ZzhYQG2qtl", "taskId": "0kGogHmaqvZ", "status": "completed"}`),
		mcpevent.NewToolAction("updateSortingOrder", "Set a task's sort order.", s.handleUpdateTaskOrder).
			WithExample(`{"workspaceId": "0ZzhYQG2qtl", "taskId": "0kGogHmaqvZ", "sortOrder": 1.5}`),
		mcpevent.NewToolAction("updateAssignee", "Hand a task to the agent or the human.", s.handleUpdateTaskAssignee).
			WithExample(`{"workspaceId": "0ZzhYQG2qtl", "taskId": "0kGogHmaqvZ", "assignee": "agent"}`),
		mcpevent.NewToolAction("updateAllowAll", "Let a task's agent run every command without asking, or stop it.", s.handleUpdateTaskAllowAll).
			WithExample(`{"workspaceId": "0ZzhYQG2qtl", "taskId": "0kGogHmaqvZ", "allowAll": false}`),
		mcpevent.NewToolAction("updateSchedule", "Change a scheduled task's title, body or schedule.", s.handleUpdateScheduledTask).
			WithExample(`{"workspaceId": "0ZzhYQG2qtl", "taskId": "0kGogHmaqvZ", "cronSchedule": "0 9 * * 1"}`),
		mcpevent.NewToolAction("delete", "Delete a task, with its messages and attachments. This cannot be undone.", s.handleDeleteTask).
			WithDetails("To stop a scheduled task without losing its history, updateStatus it to rejected instead.").
			WithExample(`{"workspaceId": "0ZzhYQG2qtl", "taskId": "0kGogHmaqvZ"}`),
		mcpevent.NewToolAction("getAttachment", "Get an attachment of a task: its public link, or its content as base64 or plain text, and metadata.", s.handleGetAttachment).
			WithExample(`{"workspaceId": "0ZzhYQG2qtl", "taskId": "0kGogHmaqvZ", "attachmentId": "att-1"}`),
	}
}

func (s *WorkspaceServer) handleTask(ctx context.Context, req *mcp.CallToolRequest, p TaskParams) (*mcp.CallToolResult, any, error) {
	return s.runTool(ctx, req, "task", s.taskActions(), p.Action, p.Payload)
}
