// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package coremcp

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	mcpevent "github.com/agentrq/agentrq/backend/internal/controller/mcp"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// taskToolDescription is the supervisor's task tool's description.
const taskToolDescription = "Read and manage the tasks of every workspace on the account. Two inputs: action, and payload, the action's fields. " +
	"Run action help for the actions, and help with payload {\"topic\": \"<action>\"} for one's payload. " +
	"list and listAll list one workspace's tasks or every workspace's; create, get, reply and delete do what they say; " +
	"messages reads only a task's conversation, far smaller than get, to follow up on a task; " +
	"respond answers the permission request a task is waiting on; updateStatus, updateSortingOrder, updateAssignee, updateAllowAll and updateSchedule change one thing about a task; " +
	"getAttachment gets a task's attachment. delete cannot be undone: to stop a scheduled task without losing its history, updateStatus it to rejected instead."

// awaitingReplyHelp is the rule replyState follows, as list, listAll and get
// tell it.
const awaitingReplyHelp = "Each task has awaitingReply: true while a question or permission request of its agent is still pending, " +
	"or when the newest message of its conversation (what messages returns) is the agent's and ends with \"?\", with no reply after it. " +
	"lastMessageAt and lastMessageBy say when that newest message was sent, and by whom; a supervisor's reply counts as the human's. " +
	"An agent often waits on an answer with its task still ongoing, so check awaitingReply, not only the status."

// TaskParams is the input to the task tool.
type TaskParams struct {
	Action  string         `json:"action" jsonschema:"enum: list, listAll, create, get, messages, reply, respond, updateStatus, updateSortingOrder, updateAssignee, updateAllowAll, updateSchedule, delete, getAttachment, help"`
	Payload map[string]any `json:"payload,omitempty" jsonschema:"The action's fields, as help prints them."`
}

// taskActions are the task tool's actions, each run by the handler of the
// tool it replaced, with that tool's parameters as its payload.
func (s *WorkspaceServer) taskActions() []mcpevent.ToolAction {
	return []mcpevent.ToolAction{
		mcpevent.NewToolAction("list", "List a workspace's tasks.", s.handleListTasks).
			WithDetails("filter keeps the tasks whose title or body contains it, ignoring case; status, createdBy ('human' or 'agent'), createdByWorkspaceId (the workspace that delegated it through create) and awaitingReply narrow the list further; limit (default 5, at most 50) and offset page through it. " + awaitingReplyHelp).
			WithExample(`{"workspaceId": "0ZzhYQG2qtl", "status": "blocked", "limit": 10}`),
		mcpevent.NewToolAction("listAll", "List the tasks of every workspace.", s.handleListAllTasks).
			WithDetails("filter keeps the tasks whose title or body contains it, ignoring case; status, createdBy ('human' or 'agent'), createdByWorkspaceId (the workspace that delegated it through create) and awaitingReply narrow the list further; limit (default 5, at most 50) and offset page through it. " + awaitingReplyHelp).
			WithExample(`{"status": "ongoing"}`),
		mcpevent.NewToolAction("create", "Create a task in a workspace, assigned to the human unless assignee says otherwise.", s.handleCreateTask).
			WithDetails("Write the full context into body: the receiving agent has nothing else. cronSchedule makes it a scheduled task: a recurring one runs at most hourly, a one-time one fixes dom and month. The task records the supervisor workspace as its createdByWorkspaceId, which list and listAll filter on.").
			WithExample(`{"workspaceId": "0ZzhYQG2qtl", "title": "Release v0.9.20", "body": "Tag main and publish the release notes.", "assignee": "agent", "clearContext": true}`),
		mcpevent.NewToolAction("get", "Get a task.", s.handleGetTask).
			WithDetails("The agent's tool calls, most of a busy task's size, are left out unless includeToolCalls is true; toolCallCount says how many there are. messagesSince keeps only the messages created after it. To follow up on a reply, messages is more efficient still. " + awaitingReplyHelp).
			WithExample(`{"workspaceId": "0ZzhYQG2qtl", "taskId": "0kGogHmaqvZ", "messagesSince": "2026-10-26T09:00:00Z"}`),
		mcpevent.NewToolAction("messages", "Read a task's conversation: its text messages, oldest first, with the task's status and updatedAt.", s.handleTaskMessages).
			WithDetails("Each message has its id, createdAt, sender (human or agent), text and attachment names; an agent's question to the human is one. Tool calls, permission requests and the agent's thoughts, usage and notices are left out. since keeps the messages created after it: pass the createdAt of the last one you read. limit (default 20, at most 100) keeps the newest; total says how many matched.").
			WithExample(`{"workspaceId": "0ZzhYQG2qtl", "taskId": "0kGogHmaqvZ", "since": "2026-10-26T09:00:00Z"}`),
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

// How many messages the task tool's messages action returns by default, and
// at most.
const (
	defaultTaskMessages = 20
	maxTaskMessages     = 100
)

// parseSince reads the RFC3339 time in field; empty is the zero time.
func parseSince(field, value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s must be an RFC3339 time, like 2026-10-26T09:00:00Z", field)
	}
	return t, nil
}

// messagesAfter keeps the messages created after since, or all of them when
// since is the zero time.
func messagesAfter(msgs []entity.Message, since time.Time) []entity.Message {
	if since.IsZero() {
		return msgs
	}
	var out []entity.Message
	for _, m := range msgs {
		if m.CreatedAt.After(since) {
			out = append(out, m)
		}
	}
	return out
}

// textMessages keeps what was said in a task's thread, oldest first: plain
// messages, and an agent's questions to the human. The rest is the agent at
// work: permission requests, thoughts, usage and notices.
func textMessages(msgs []entity.Message) []entity.Message {
	var out []entity.Message
	for _, m := range msgs {
		meta, _ := m.Metadata.(map[string]any)
		switch meta["type"] {
		case nil, "", "elicitation_request":
			out = append(out, m)
		}
	}
	slices.SortFunc(out, func(a, b entity.Message) int { return cmp.Compare(a.ID, b.ID) })
	return out
}

// replyState answers whether a task awaits a reply, from its messages or any
// subset holding its pending requests and its conversation's newest message:
// yes while a question to the human (a permission request or an elicitation)
// is still pending, or when that newest message is a plain agent message
// ending in "?". awaitingReplyClause in the repository is the same rule in SQL.
func replyState(msgs []entity.Message) entity.TaskReplyState {
	var state entity.TaskReplyState
	for _, m := range msgs {
		if meta, _ := m.Metadata.(map[string]any); meta["status"] == "pending" {
			state.AwaitingReply = true
		}
	}
	conversation := textMessages(msgs)
	if len(conversation) == 0 {
		return state
	}
	last := conversation[len(conversation)-1]
	state.LastMessageAt, state.LastMessageBy = last.CreatedAt, last.Sender
	if meta, _ := last.Metadata.(map[string]any); last.Sender == "agent" && meta["type"] != "elicitation_request" &&
		strings.HasSuffix(strings.TrimRight(last.Text, " \t\n\r"), "?") {
		state.AwaitingReply = true
	}
	return state
}

// replyStates is replyState for each task, from the messages the list
// preloaded for it, which then leave the task: a list carries no messages.
func replyStates(tasks []entity.Task) []entity.TaskReplyState {
	states := make([]entity.TaskReplyState, len(tasks))
	for i := range tasks {
		states[i] = replyState(tasks[i].Messages)
		tasks[i].Messages = nil
	}
	return states
}
