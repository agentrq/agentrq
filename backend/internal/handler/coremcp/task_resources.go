// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package coremcp

import (
	"context"
	"strings"

	mcpevent "github.com/agentrq/agentrq/backend/internal/controller/mcp"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	apiMapper "github.com/agentrq/agentrq/backend/internal/mapper/api"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/mustafaturan/monoflake"
)

// The task resources, which a supervisor reads and listens to. Every id is
// base62, written as monoflake writes it, since a change is announced on the
// URI exactly as it is written here.
const (
	workspacesURIPrefix = "agentrq://workspaces/"
	delegatedURIPrefix  = "agentrq://tasks/delegated-by/"
)

// How many messages a task resource ends with, and how many tasks a list of
// them holds, the most the task tool's list returns.
const (
	taskResourceMessages = 5
	taskResourceListSize = 50
)

// taskURIKind is which of the task resources a URI names.
type taskURIKind int

const (
	taskURIInvalid taskURIKind = iota
	taskURITask
	taskURIWorkspaceTasks
	taskURIDelegatedTasks
)

// taskURIRef is a task resource URI, read.
type taskURIRef struct {
	kind        taskURIKind
	workspaceID int64
	taskID      int64
}

func taskURI(workspaceID, taskID int64) string {
	return workspaceTasksURI(workspaceID) + "/" + monoflake.ID(taskID).String()
}

func workspaceTasksURI(workspaceID int64) string {
	return workspacesURIPrefix + monoflake.ID(workspaceID).String() + "/tasks"
}

func delegatedTasksURI(workspaceID int64) string {
	return delegatedURIPrefix + monoflake.ID(workspaceID).String()
}

// parseTaskURI reads a task resource URI. Anything else, an id written
// another way included, is taskURIInvalid: it would never be announced.
func parseTaskURI(uri string) taskURIRef {
	if rest, ok := strings.CutPrefix(uri, delegatedURIPrefix); ok {
		if id := canonicalID(rest); id != 0 {
			return taskURIRef{kind: taskURIDelegatedTasks, workspaceID: id}
		}
		return taskURIRef{}
	}
	rest, ok := strings.CutPrefix(uri, workspacesURIPrefix)
	if !ok {
		return taskURIRef{}
	}
	parts := strings.Split(rest, "/")
	if len(parts) < 2 || len(parts) > 3 || parts[1] != "tasks" {
		return taskURIRef{}
	}
	workspaceID := canonicalID(parts[0])
	if workspaceID == 0 {
		return taskURIRef{}
	}
	if len(parts) == 2 {
		return taskURIRef{kind: taskURIWorkspaceTasks, workspaceID: workspaceID}
	}
	if taskID := canonicalID(parts[2]); taskID != 0 {
		return taskURIRef{kind: taskURITask, workspaceID: workspaceID, taskID: taskID}
	}
	return taskURIRef{}
}

// canonicalID is the id s names, or 0 unless s is how monoflake writes it.
func canonicalID(s string) int64 {
	id := monoflake.IDFromBase62(s).Int64()
	if id <= 0 || monoflake.ID(id).String() != s {
		return 0
	}
	return id
}

// registerTaskResources adds the task resources, and lets a supervisor
// listen to them: see subscriptions.go.
func (s *WorkspaceServer) registerTaskResources() {
	s.server.AddResourceTemplate(&mcp.ResourceTemplate{
		URITemplate: workspacesURIPrefix + "{workspaceId}/tasks/{taskId}",
		Name:        "task",
		Title:       "A task",
		Description: "A task's status, updatedAt, awaitingReply and its last few text messages, without tool calls. Listen to it with subscriptions/listen to hear when its status changes, a message arrives, or its agent asks for an answer or a permission.",
		MIMEType:    "application/json",
	}, s.readTask)
	s.server.AddResourceTemplate(&mcp.ResourceTemplate{
		URITemplate: workspacesURIPrefix + "{workspaceId}/tasks",
		Name:        "workspace-tasks",
		Title:       "A workspace's tasks",
		Description: "The 50 newest tasks of a workspace, as the task tool's list returns them. Listen to it to hear when any task there changes.",
		MIMEType:    "application/json",
	}, s.readWorkspaceTasks)
	s.server.AddResourceTemplate(&mcp.ResourceTemplate{
		URITemplate: delegatedURIPrefix + "{workspaceId}",
		Name:        "delegated-tasks",
		Title:       "The tasks a workspace delegated",
		Description: "The 50 newest tasks a workspace created in any workspace through the task tool's create, as list returns them. Listen to it to hear when any of them changes.",
		MIMEType:    "application/json",
	}, s.readDelegatedTasks)
}

func (s *WorkspaceServer) readTask(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	ref := parseTaskURI(req.Params.URI)
	s.emitTelemetry(ctx, mcpevent.ActionMCPMethodCall, "resource:task", ref.workspaceID)
	if ref.kind != taskURITask {
		return nil, mcp.ResourceNotFoundError(req.Params.URI)
	}
	res, err := s.crud.GetTask(ctx, entity.GetTaskRequest{
		UserID:      getUserID(ctx),
		WorkspaceID: ref.workspaceID,
		TaskID:      ref.taskID,
	})
	if err != nil {
		return nil, mcp.ResourceNotFoundError(req.Params.URI)
	}
	msgs := textMessages(res.Task.Messages)
	msgs = msgs[max(0, len(msgs)-taskResourceMessages):]
	body := apiMapper.FromTaskEntityToTaskResourceMCPResponse(res.Task, msgs, replyState(res.Task.Messages).AwaitingReply)
	return jsonResource(req.Params.URI, body), nil
}

func (s *WorkspaceServer) readWorkspaceTasks(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	ref := parseTaskURI(req.Params.URI)
	s.emitTelemetry(ctx, mcpevent.ActionMCPMethodCall, "resource:workspace-tasks", ref.workspaceID)
	if ref.kind != taskURIWorkspaceTasks {
		return nil, mcp.ResourceNotFoundError(req.Params.URI)
	}
	return s.readTaskList(ctx, req.Params.URI, entity.ListTasksRequest{WorkspaceID: ref.workspaceID})
}

func (s *WorkspaceServer) readDelegatedTasks(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	ref := parseTaskURI(req.Params.URI)
	s.emitTelemetry(ctx, mcpevent.ActionMCPMethodCall, "resource:delegated-tasks", ref.workspaceID)
	if ref.kind != taskURIDelegatedTasks {
		return nil, mcp.ResourceNotFoundError(req.Params.URI)
	}
	return s.readTaskList(ctx, req.Params.URI, entity.ListTasksRequest{CreatedByWorkspaceID: ref.workspaceID})
}

// readTaskList reads the newest tasks rq names, as the task tool's list does.
func (s *WorkspaceServer) readTaskList(ctx context.Context, uri string, rq entity.ListTasksRequest) (*mcp.ReadResourceResult, error) {
	rq.UserID = getUserID(ctx)
	rq.PreloadReplyMessages = true
	rq.Limit = taskResourceListSize
	res, err := s.crud.ListTasks(ctx, rq)
	if err != nil {
		return nil, mcp.ResourceNotFoundError(uri)
	}
	return jsonResource(uri, apiMapper.FromListTasksResponseEntityToMCPResponse(res, replyStates(res.Tasks))), nil
}

func jsonResource(uri string, body []byte) *mcp.ReadResourceResult {
	return &mcp.ReadResourceResult{
		Contents: []*mcp.ResourceContents{{URI: uri, MIMEType: "application/json", Text: string(body)}},
	}
}
