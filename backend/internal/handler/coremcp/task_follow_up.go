// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package coremcp

import (
	"context"

	mcpevent "github.com/agentrq/agentrq/backend/internal/controller/mcp"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	apiMapper "github.com/agentrq/agentrq/backend/internal/mapper/api"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/mustafaturan/monoflake"
)

// TaskNotifier is the MCP manager: it hands a message to the agent of a
// workspace now, on the task it is working on.
type TaskNotifier interface {
	SendChannelNotification(ctx context.Context, workspaceID int64, userID string, taskID int64, content string, attachments ...entity.Attachment)
}

// taskFollowUpHelp is what followUp does, as help tells it.
const taskFollowUpHelp = "Use it instead of creating a new task that repeats a finished one's context: an agent ignores replies on a task it completed, and a reopened task restarts from its body. " +
	"Posts text to the task and hands it back to its agent: a finished, blocked or rejected task becomes notstarted, and the agent is handed it again with every follow-up before its body; " +
	"an ongoing one stays ongoing and its agent is sent the text now. clearContext is honoured as for a new task. Refused on a task assigned to the human (reply instead) and on a scheduled template. " +
	"Answers as reply does."

// FollowUpTaskParams is the payload of the task tool's followUp action.
type FollowUpTaskParams struct {
	WorkspaceID string `json:"workspaceId" jsonschema:"The workspace's ID (base62)."`
	TaskID      string `json:"taskId" jsonschema:"The task's ID (base62)."`
	Text        string `json:"text" jsonschema:"What the agent should do next on the task: write it whole, the agent may have cleared its context."`
	Verbose     bool   `json:"verbose,omitempty" jsonschema:"true answers with the whole task, its body, messages and tool calls included; by default only its id, workspaceId, title, status, assignee and updatedAt."`
}

// followUpPushContent is how an agent working on a task is sent a follow-up.
func followUpPushContent(taskID int64, text string) string {
	return "[Follow-up from supervisor on task " + monoflake.ID(taskID).String() + "] " + text
}

// handleFollowUpTask asks an agent for more on a task it has, or had: the
// follow-up goes first when the task is handed over again, or at once to an
// agent still on it.
func (s *WorkspaceServer) handleFollowUpTask(ctx context.Context, req *mcp.CallToolRequest, args FollowUpTaskParams) (*mcp.CallToolResult, any, error) {
	s.emitTelemetry(ctx, mcpevent.ActionMCPToolCall, "followUpTask", parseID(args.WorkspaceID))
	userID := getUserID(ctx)
	res, err := s.crud.FollowUpTask(ctx, entity.FollowUpTaskRequest{
		UserID:      userID,
		WorkspaceID: parseID(args.WorkspaceID),
		TaskID:      parseID(args.TaskID),
		Text:        args.Text,
	})
	if err != nil {
		return errorResponse(err), nil, nil
	}
	if res.Task.Status == "ongoing" && s.notifier != nil {
		s.notifier.SendChannelNotification(ctx, res.Task.WorkspaceID, userID, res.Task.ID, followUpPushContent(res.Task.ID, args.Text))
	}
	return taskChanged(args.Verbose, res.Task, res.MessageID, func() []byte {
		return apiMapper.FromReplyToTaskResponseEntityToHTTPResponse(&entity.ReplyToTaskResponse{Task: res.Task, MessageID: res.MessageID})
	}), nil, nil
}
