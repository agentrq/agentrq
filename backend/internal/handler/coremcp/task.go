// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package coremcp

import (
	"context"

	mcpevent "github.com/agentrq/agentrq/backend/internal/controller/mcp"
	"github.com/agentrq/agentrq/backend/internal/service/mcphint"
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
		mcpevent.NewToolAction("list", "List a workspace's tasks.", s.handleListTasks),
		mcpevent.NewToolAction("listAll", "List the tasks of every workspace.", s.handleListAllTasks),
		mcpevent.NewToolAction("create", "Create a task in a workspace, assigned to the human unless assignee says otherwise.", s.handleCreateTask),
		mcpevent.NewToolAction("get", "Get a task.", s.handleGetTask),
		mcpevent.NewToolAction("reply", "Post a message to a task's thread.", s.handleReplyToTask),
		mcpevent.NewToolAction("respond", "Answer the permission request a task is waiting on: allow, allow_all, reject, or text to reply without deciding.", s.handleRespondToTask),
		mcpevent.NewToolAction("updateStatus", "Set a task's status.", s.handleUpdateTaskStatus),
		mcpevent.NewToolAction("updateSortingOrder", "Set a task's sort order.", s.handleUpdateTaskOrder),
		mcpevent.NewToolAction("updateAssignee", "Hand a task to the agent or the human.", s.handleUpdateTaskAssignee),
		mcpevent.NewToolAction("updateAllowAll", "Let a task's agent run every command without asking, or stop it.", s.handleUpdateTaskAllowAll),
		mcpevent.NewToolAction("updateSchedule", "Change a scheduled task's title, body or schedule.", s.handleUpdateScheduledTask),
		mcpevent.NewToolAction("delete", "Delete a task, with its messages and attachments. This cannot be undone.", s.handleDeleteTask),
		mcpevent.NewToolAction("getAttachment", "Get an attachment of a task: its public link, or its content as base64, and metadata.", s.handleGetAttachment),
	}
}

// legacyTaskTools are the supervisor tools the task tool replaced.
var legacyTaskTools = map[string]mcpevent.LegacyTool{
	"listTasks":           {Tool: "task", Action: "list"},
	"listAllTasks":        {Tool: "task", Action: "listAll"},
	"createTask":          {Tool: "task", Action: "create"},
	"getTask":             {Tool: "task", Action: "get"},
	"respondToTask":       {Tool: "task", Action: "respond"},
	"replyToTask":         {Tool: "task", Action: "reply"},
	"updateTaskStatus":    {Tool: "task", Action: "updateStatus"},
	"updateTaskOrder":     {Tool: "task", Action: "updateSortingOrder"},
	"updateTaskAssignee":  {Tool: "task", Action: "updateAssignee"},
	"updateTaskAllowAll":  {Tool: "task", Action: "updateAllowAll"},
	"updateScheduledTask": {Tool: "task", Action: "updateSchedule"},
	"deleteTask":          {Tool: "task", Action: "delete"},
	"getAttachment":       {Tool: "task", Action: "getAttachment"},
}

// registerTaskTool adds the task tool. It deletes, so it is marked
// destructive, and its reads lose their read-only hint with it.
func (s *WorkspaceServer) registerTaskTool() {
	mcp.AddTool(s.server, &mcp.Tool{Name: "task", Description: taskToolDescription, Annotations: mcphint.Overwrite("Manage tasks")}, s.handleTask)
	s.server.AddReceivingMiddleware(mcpevent.ActionToolsMiddleware([]string{"task", "pano"}, legacyTaskTools))
}

func (s *WorkspaceServer) handleTask(ctx context.Context, req *mcp.CallToolRequest, p TaskParams) (*mcp.CallToolResult, any, error) {
	actions := s.taskActions()
	if !mcpevent.IsAction(actions, p.Action) {
		s.emitTelemetry(ctx, mcpevent.ActionMCPToolCall, "task", 0)
	}
	return mcpevent.RunAction(ctx, req, "task", actions, p.Action, p.Payload)
}
