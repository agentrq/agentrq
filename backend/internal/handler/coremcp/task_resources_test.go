// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package coremcp

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	view "github.com/agentrq/agentrq/backend/internal/data/view/api"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type resourceHandler func(context.Context, *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error)

func readURI(h resourceHandler, uri string) (*mcp.ReadResourceResult, error) {
	return h(authedContext(), &mcp.ReadResourceRequest{Params: &mcp.ReadResourceParams{URI: uri}})
}

// readJSON reads uri, which must answer one JSON document, into out.
func readJSON(t *testing.T, h resourceHandler, uri string, out any) {
	t.Helper()
	res, err := readURI(h, uri)
	if err != nil {
		t.Fatalf("read %s: %v", uri, err)
	}
	if len(res.Contents) != 1 || res.Contents[0].URI != uri || res.Contents[0].MIMEType != "application/json" {
		t.Fatalf("read %s: contents %+v, want one JSON document of that URI", uri, res.Contents)
	}
	if err := json.Unmarshal([]byte(res.Contents[0].Text), out); err != nil {
		t.Fatal(err)
	}
}

func expectNotFound(t *testing.T, h resourceHandler, uri string) {
	t.Helper()
	if _, err := readURI(h, uri); err == nil || err.Error() != mcp.ResourceNotFoundError(uri).Error() {
		t.Errorf("read %s: err %v, want resource not found", uri, err)
	}
}

// A task resource is what a change notice sends the supervisor to read: its
// state and the end of its conversation, without the agent at work.
func TestReadTask_IsTheStateAndTheLastTextMessages(t *testing.T) {
	srv := busyTaskServer(t)
	var got view.TaskResource
	readJSON(t, srv.readTask, taskURI(testWorkspace, testTriggerID), &got)

	if got.ID != base62(testTriggerID) || got.WorkspaceID != base62(testWorkspace) || got.Status != "blocked" ||
		!got.UpdatedAt.Equal(threadStart.Add(time.Hour)) || !got.AwaitingReply {
		t.Errorf("task = %+v, want it blocked, updated an hour in, awaiting a reply", got)
	}
	want := []string{"Release v0.9.20.", "On it.", "Which branch should I release from?", "From main."}
	if !slices.Equal(texts(got.Messages), want) {
		t.Errorf("messages = %q, want %q", texts(got.Messages), want)
	}
}

func TestReadTask_KeepsTheLastFiveMessages(t *testing.T) {
	var thread []entity.Message
	for n := range 8 {
		thread = append(thread, message(n, "agent", string(rune('a'+n)), nil))
	}
	srv := taskServer(&mockTaskCrud{getTask: func(context.Context, entity.GetTaskRequest) (*entity.GetTaskResponse, error) {
		return &entity.GetTaskResponse{Task: entity.Task{Messages: thread}}, nil
	}})
	var got view.TaskResource
	readJSON(t, srv.readTask, taskURI(1, 2), &got)
	if want := []string{"d", "e", "f", "g", "h"}; !slices.Equal(texts(got.Messages), want) {
		t.Errorf("messages = %q, want %q", texts(got.Messages), want)
	}
}

func TestReadTask_NotFound(t *testing.T) {
	srv := taskServer(&mockTaskCrud{getTask: func(context.Context, entity.GetTaskRequest) (*entity.GetTaskResponse, error) {
		return nil, errors.New("task not found")
	}})
	expectNotFound(t, srv.readTask, taskURI(1, 2))
	expectNotFound(t, srv.readTask, workspaceTasksURI(1))
}

// Both lists are the task tool's list, read as the caller, newest 50.
func TestReadTaskLists(t *testing.T) {
	var got []entity.ListTasksRequest
	srv := taskServer(&mockTaskCrud{listTasks: func(_ context.Context, rq entity.ListTasksRequest) (*entity.ListTasksResponse, error) {
		got = append(got, rq)
		return &entity.ListTasksResponse{Tasks: []entity.Task{busyTask().Task}}, nil
	}})

	for _, tc := range []struct {
		read resourceHandler
		uri  string
	}{{srv.readWorkspaceTasks, workspaceTasksURI(testWorkspace)}, {srv.readDelegatedTasks, delegatedTasksURI(testWorkspace)}} {
		var list view.SupervisorListTasksResponse
		readJSON(t, tc.read, tc.uri, &list)
		if len(list.Tasks) != 1 || !list.Tasks[0].AwaitingReply {
			t.Errorf("%s: tasks %+v, want the one task, awaiting a reply", tc.uri, list.Tasks)
		}
	}
	want := []entity.ListTasksRequest{
		{WorkspaceID: testWorkspace, UserID: testUserID, PreloadReplyMessages: true, Limit: 50},
		{CreatedByWorkspaceID: testWorkspace, UserID: testUserID, PreloadReplyMessages: true, Limit: 50},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ListTasks(%+v), want %+v", got, want)
	}
}

func TestReadTaskLists_NotFound(t *testing.T) {
	srv := taskServer(&mockTaskCrud{listTasks: func(context.Context, entity.ListTasksRequest) (*entity.ListTasksResponse, error) {
		return nil, errors.New("database unavailable")
	}})
	expectNotFound(t, srv.readWorkspaceTasks, workspaceTasksURI(1))
	expectNotFound(t, srv.readDelegatedTasks, delegatedTasksURI(1))
	expectNotFound(t, srv.readWorkspaceTasks, delegatedTasksURI(1))
	expectNotFound(t, srv.readDelegatedTasks, workspaceTasksURI(1))
}
