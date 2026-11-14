// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package coremcp

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
)

// listedTasks are three busy tasks as a list returns them: the first
// scheduled, the last with no conversation yet.
func listedTasks() []entity.Task {
	tasks := []entity.Task{busyChangedTask(testTriggerID), busyChangedTask(testTriggerID + 1), busyChangedTask(testTriggerID + 2)}
	for i := range tasks {
		tasks[i].CreatedAt = time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)
		tasks[i].ToolCalls, tasks[i].ToolCallCount = nil, 0 // a list never loads them
		for j := range tasks[i].Messages {
			tasks[i].Messages[j].CreatedAt = time.Date(2026, 11, 14, 4, 0, j, 0, time.UTC)
		}
	}
	tasks[0].CronSchedule = "0 9 * * 1"
	tasks[0].Messages[len(tasks[0].Messages)-1].Text = "Shall I tag it?"
	tasks[2].Messages = nil
	return tasks
}

func listingServer() *WorkspaceServer {
	return taskServer(&mockTaskCrud{listTasks: func(context.Context, entity.ListTasksRequest) (*entity.ListTasksResponse, error) {
		return &entity.ListTasksResponse{Tasks: listedTasks()}, nil
	}})
}

// A supervisor checking on its tasks needs where each stands, not its body:
// listing three tasks used to cost about 15k tokens.
func TestTaskList_AnswersSummariesUnlessBodiesAreAsked(t *testing.T) {
	srv := listingServer()
	for _, action := range []string{"list", "listAll"} {
		payload := map[string]any{}
		if action == "list" {
			payload["workspaceId"] = base62(testWorkspace)
		}
		summary := textOf(t, toolResult(srv.handleTask(authedContext(), nil, TaskParams{Action: action, Payload: payload})))
		var got struct{ Tasks []map[string]any }
		if err := json.Unmarshal([]byte(summary), &got); err != nil || len(got.Tasks) != 3 {
			t.Fatalf("%s answered %s, want three tasks", action, summary)
		}
		want := []string{"assignee", "awaitingReply", "createdAt", "cronSchedule", "id", "lastMessageAt", "lastMessageBy", "status", "title", "updatedAt", "workspaceId"}
		if keys := fieldNames(got.Tasks[0]); !slices.Equal(keys, want) {
			t.Errorf("%s: a scheduled task's fields are %v, want %v", action, keys, want)
		}
		first := got.Tasks[0]
		if first["id"] != base62(testTriggerID) || first["workspaceId"] != base62(testWorkspace) || first["title"] != "Release v0.9.20" ||
			first["status"] != "ongoing" || first["assignee"] != "agent" || first["cronSchedule"] != "0 9 * * 1" ||
			first["awaitingReply"] != true || first["lastMessageBy"] != "agent" || first["createdAt"] != "2026-11-13T09:00:00Z" || first["lastMessageAt"] != "2026-11-14T04:00:39Z" ||
			first["updatedAt"] != "2026-11-14T04:08:11Z" {
			t.Errorf("%s: first task is %v", action, first)
		}
		if keys := fieldNames(got.Tasks[2]); slices.Contains(keys, "cronSchedule") || slices.Contains(keys, "lastMessageAt") || slices.Contains(keys, "lastMessageBy") {
			t.Errorf("%s: an unscheduled task with no conversation has %v, want no cronSchedule or lastMessage fields", action, keys)
		}
		if got.Tasks[1]["awaitingReply"] != false {
			t.Errorf("%s: a task whose agent asked nothing awaits a reply", action)
		}

		payload["includeBody"] = true
		full := textOf(t, toolResult(srv.handleTask(authedContext(), nil, TaskParams{Action: action, Payload: payload})))
		if !strings.Contains(full, `"body":"Tag main and publish the release notes.`) || !strings.Contains(full, `"awaitingReply":true`) {
			t.Errorf("%s with includeBody answered no body or reply state: %.300s", action, full)
		}
		if strings.Contains(summary, "Tag main") {
			t.Errorf("%s answered a body without includeBody", action)
		}
		t.Logf("%s of three busy tasks: %d bytes as summaries, %d with includeBody", action, len(summary), len(full))
	}
}

func fieldNames(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
