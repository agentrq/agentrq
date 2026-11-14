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
	apiMapper "github.com/agentrq/agentrq/backend/internal/mapper/api"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// taskToolDescription is the supervisor's task tool's description.
const taskToolDescription = "Hand out, follow and steer the work of every workspace on the account. A task is one piece of work, assigned to a workspace's agent or to the human, with a conversation between them. " +
	"Use it to give a workspace work, check on it, answer an agent that is waiting, and close it. " + twoInputs +
	"An action on one task takes its workspaceId and taskId; workspaceId comes from workspace list. " +
	"list (workspaceId) and listAll list one workspace's tasks or every workspace's, 5 by default and at most 50, filtered by text, status, creator or awaitingReply, each as a summary without its body unless includeBody is true: poll with them and messages, not get; " +
	"create (workspaceId, title, body) adds a task, assigned to the human unless assignee says agent, once or on a cronSchedule; get reads a task in full; " +
	"messages reads only a task's conversation, far smaller than get, to follow up on a task; reply posts to it; " +
	"respond answers the permission request a task is waiting on; updateStatus, updateSortingOrder, updateAssignee, updateAllowAll and updateSchedule change one thing about a task; " +
	"those and create answer only the task's id, workspaceId, title, status, assignee and updatedAt, and reply the messageId it posted; verbose: true in their payload answers the whole task. " +
	"getAttachment gets a task's attachment. delete removes a task with its messages and attachments and cannot be undone: to stop a scheduled task without losing its history, updateStatus it to rejected instead."

// awaitingReplyHelp is the rule replyState follows, as list, listAll and get
// tell it.
const awaitingReplyHelp = "Each task has awaitingReply: true while a question or permission request of its agent is still pending, " +
	"or when the newest message of its conversation (what messages returns) is the agent's and ends with \"?\", with no reply after it. " +
	"lastMessageAt and lastMessageBy say when that newest message was sent, and by whom; a supervisor's reply counts as the human's. " +
	"An agent often waits on an answer with its task still ongoing, so check awaitingReply, not only the status."

// taskListHelp is what list and listAll take and answer.
const taskListHelp = "filter keeps the tasks whose title or body contains it, ignoring case; status, createdBy ('human' or 'agent'), createdByWorkspaceId (the workspace that delegated it through create) and awaitingReply narrow the list further; limit (default 5, at most 50) and offset page through it. " +
	"Each task is a summary: id, workspaceId, title, status, assignee, createdAt, updatedAt, lastMessageAt, lastMessageBy, awaitingReply and, for a scheduled one, cronSchedule. " +
	"includeBody: true answers each task whole, its body included; to check on tasks, the summary is enough. " + awaitingReplyHelp

// TaskParams is the input to the task tool.
type TaskParams struct {
	Action  string         `json:"action" jsonschema:"What to do: one of the actions this tool's description lists, or help for each action's fields and an example."`
	Payload map[string]any `json:"payload,omitempty" jsonschema:"The fields the chosen action works on, such as the ids it reads or the text it writes; help with payload {\"topic\": \"<action>\"} names them and gives an example. Leave it out for an action that takes none."`
}

// taskActions are the task tool's actions, each run by the handler of the
// tool it replaced, with that tool's parameters as its payload.
func (s *WorkspaceServer) taskActions() []mcpevent.ToolAction {
	return []mcpevent.ToolAction{
		mcpevent.NewToolAction("list", "List a workspace's tasks.", s.handleListTasks).
			WithDetails(taskListHelp).
			WithExample(`{"workspaceId": "0ZzhYQG2qtl", "status": "blocked", "limit": 10}`).
			ReadOnly(),
		mcpevent.NewToolAction("listAll", "List the tasks of every workspace.", s.handleListAllTasks).
			WithDetails(taskListHelp).
			WithExample(`{"status": "ongoing"}`).
			ReadOnly(),
		mcpevent.NewToolAction("create", "Create a task in a workspace, assigned to the human unless assignee says otherwise.", s.handleCreateTask).
			WithDetails("Write the full context into body: the receiving agent has nothing else. cronSchedule makes it a scheduled task: a recurring one runs at most hourly, a one-time one fixes dom and month. The task records the supervisor workspace as its createdByWorkspaceId, which list and listAll filter on.").
			WithExample(`{"workspaceId": "0ZzhYQG2qtl", "title": "Release v0.9.20", "body": "Tag main and publish the release notes.", "assignee": "agent", "clearContext": true}`),
		mcpevent.NewToolAction("get", "Get a task.", s.handleGetTask).
			WithDetails("The agent's tool calls, most of a busy task's size, are left out unless includeToolCalls is true; toolCallCount says how many there are. messagesSince keeps only the messages created after it. To follow up on a reply, messages is more efficient still. " + awaitingReplyHelp).
			WithExample(`{"workspaceId": "0ZzhYQG2qtl", "taskId": "0kGogHmaqvZ", "messagesSince": "2026-10-26T09:00:00Z"}`).
			ReadOnly(),
		mcpevent.NewToolAction("messages", "Read a task's conversation: its text messages, oldest first, with the task's status and updatedAt.", s.handleTaskMessages).
			WithDetails("Each message has its id, createdAt, sender (human or agent), text and attachment names; an agent's question to the human is one. Tool calls, permission requests and the agent's thoughts, usage and notices are left out. limit (default 5, at most 100) keeps the newest. since, a message id or an RFC3339 time, keeps the messages after it: pass the id of the last one you read to poll for news. before, the same, keeps those before it: pass the id of the oldest you have to page back. total says how many matched, hasMore whether the limit left older ones out. maxChars cuts each text to that many characters, marking the cut, to skim.").
			WithExample(`{"workspaceId": "0ZzhYQG2qtl", "taskId": "0kGogHmaqvZ", "since": "0kGoh3Xr2Pa", "maxChars": 500}`).
			ReadOnly(),
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
			WithExample(`{"workspaceId": "0ZzhYQG2qtl", "taskId": "0kGogHmaqvZ", "attachmentId": "att-1"}`).
			ReadOnly(),
	}
}

func (s *WorkspaceServer) handleTask(ctx context.Context, req *mcp.CallToolRequest, p TaskParams) (*mcp.CallToolResult, any, error) {
	return s.runTool(ctx, req, "task", s.taskActions(), p.Action, p.Payload)
}

// taskChanged answers an action that changed task t: with verbose, the whole
// task as full renders it; otherwise only what says which task it is and where
// it stands, a busy task's full answer being tens of thousands of tokens.
// messageID, when not 0, is the message a reply posted.
func taskChanged(verbose bool, t entity.Task, messageID int64, full func() []byte) *mcp.CallToolResult {
	if verbose {
		return textResponse(string(full()))
	}
	return textResponse(string(apiMapper.FromTaskEntityToTaskSummaryMCPResponse(t, messageID)))
}

// How many messages the task tool's messages action returns by default, and
// at most.
const (
	defaultTaskMessages = 5
	maxTaskMessages     = 100
)

// messageCursor is where the task tool's messages action starts or stops
// reading a thread: a message, by id, or a time. The zero cursor is unset.
type messageCursor struct {
	id int64
	at time.Time
}

// parseMessageCursor reads field's value as an RFC3339 time or a message id;
// empty is the unset cursor.
func parseMessageCursor(field, value string) (messageCursor, error) {
	if value == "" {
		return messageCursor{}, nil
	}
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return messageCursor{at: t}, nil
	}
	if id := parseID(value); id > 0 && isBase62(value) {
		return messageCursor{id: id}, nil
	}
	return messageCursor{}, fmt.Errorf("%s must be a message id or an RFC3339 time, like 2026-10-26T09:00:00Z", field)
}

func isBase62(s string) bool {
	return strings.Trim(s, "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz") == ""
}

// after says whether m comes after c; every message does when c is unset.
func (c messageCursor) after(m entity.Message) bool {
	if c.id != 0 {
		return m.ID > c.id
	}
	return c.at.IsZero() || m.CreatedAt.After(c.at)
}

// before says whether m comes before c; every message does when c is unset.
func (c messageCursor) before(m entity.Message) bool {
	if c.id != 0 {
		return m.ID < c.id
	}
	return c.at.IsZero() || m.CreatedAt.Before(c.at)
}

// truncateMessages cuts each message's text to maxChars characters, ending
// it with a marker that says how many were cut; maxChars 0 keeps them whole.
func truncateMessages(msgs []entity.Message, maxChars int) {
	if maxChars <= 0 {
		return
	}
	for i, m := range msgs {
		text := []rune(m.Text)
		if len(text) > maxChars {
			msgs[i].Text = fmt.Sprintf("%s…[%d more characters]", string(text[:maxChars]), len(text)-maxChars)
		}
	}
}

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

// taskList answers list and listAll with the tasks res holds: with
// includeBody, each task whole; otherwise a summary of where each stands,
// a task's body being most of its size.
func taskList(includeBody bool, res *entity.ListTasksResponse) *mcp.CallToolResult {
	states := replyStates(res.Tasks)
	if includeBody {
		return textResponse(string(apiMapper.FromListTasksResponseEntityToMCPResponse(res, states)))
	}
	return textResponse(string(apiMapper.FromListTasksResponseEntityToTaskListMCPResponse(res, states)))
}
