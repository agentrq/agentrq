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

const compactReplyMessageID = int64(4242)

// busyChangedTask is a task the size of a busy one: a long body, a long conversation
// and many tool calls.
func busyChangedTask(id int64) entity.Task {
	t := entity.Task{
		ID:          id,
		WorkspaceID: testWorkspace,
		Title:       "Release v0.9.20",
		Body:        strings.Repeat("Tag main and publish the release notes. ", 50),
		Status:      "ongoing",
		Assignee:    "agent",
		CreatedBy:   "human",
		UpdatedAt:   time.Date(2026, 11, 14, 4, 8, 11, 0, time.UTC),
	}
	for i := range 40 {
		t.Messages = append(t.Messages, entity.Message{ID: int64(1000 + i), TaskID: id, Sender: "agent",
			Text: strings.Repeat("Ran the suite, every package passes. ", 16)})
	}
	for i := range 150 {
		t.ToolCalls = append(t.ToolCalls, entity.ToolCall{ID: int64(5000 + i), TaskID: id, WorkspaceID: testWorkspace,
			ToolName: "Bash", Description: "Run the tests", InputPreview: strings.Repeat("go test ./internal/... ", 18), Status: "allowed"})
	}
	t.ToolCallCount = int64(len(t.ToolCalls))
	return t
}

// busyTaskCrud answers every action that changes a task with a busy task.
type busyTaskCrud struct{ mockServerCrud }

func (*busyTaskCrud) CreateTask(_ context.Context, req entity.CreateTaskRequest) (*entity.CreateTaskResponse, error) {
	return &entity.CreateTaskResponse{Task: busyChangedTask(testTriggerID)}, nil
}

func (*busyTaskCrud) RespondToTask(_ context.Context, req entity.RespondToTaskRequest) (*entity.RespondToTaskResponse, error) {
	return &entity.RespondToTaskResponse{Task: busyChangedTask(req.TaskID)}, nil
}

func (*busyTaskCrud) ReplyToTask(_ context.Context, req entity.ReplyToTaskRequest) (*entity.ReplyToTaskResponse, error) {
	return &entity.ReplyToTaskResponse{Task: busyChangedTask(req.TaskID), MessageID: compactReplyMessageID}, nil
}

func (*busyTaskCrud) UpdateTaskStatus(_ context.Context, req entity.UpdateTaskStatusRequest) (*entity.UpdateTaskStatusResponse, error) {
	return &entity.UpdateTaskStatusResponse{Task: busyChangedTask(req.TaskID)}, nil
}

func (*busyTaskCrud) UpdateTaskOrder(_ context.Context, req entity.UpdateTaskOrderRequest) (*entity.UpdateTaskOrderResponse, error) {
	return &entity.UpdateTaskOrderResponse{Task: busyChangedTask(req.TaskID)}, nil
}

func (*busyTaskCrud) UpdateTaskAssignee(_ context.Context, req entity.UpdateTaskAssigneeRequest) (*entity.UpdateTaskAssigneeResponse, error) {
	return &entity.UpdateTaskAssigneeResponse{Task: busyChangedTask(req.TaskID)}, nil
}

func (*busyTaskCrud) UpdateTaskAllowAllCommands(_ context.Context, req entity.UpdateTaskAllowAllCommandsRequest) (*entity.UpdateTaskAllowAllCommandsResponse, error) {
	return &entity.UpdateTaskAllowAllCommandsResponse{Task: busyChangedTask(req.TaskID)}, nil
}

func (*busyTaskCrud) UpdateScheduledTask(_ context.Context, req entity.UpdateScheduledTaskRequest) (*entity.UpdateScheduledTaskResponse, error) {
	return &entity.UpdateScheduledTaskResponse{Task: busyChangedTask(req.TaskID)}, nil
}

// changingActions are the task tool's actions that change a task, each with
// a payload it accepts.
func changingActions() []struct {
	action  string
	payload map[string]any
} {
	ws, task := base62(testWorkspace), base62(testTriggerID)
	return []struct {
		action  string
		payload map[string]any
	}{
		{"create", map[string]any{"workspaceId": ws, "title": "t"}},
		{"reply", map[string]any{"workspaceId": ws, "taskId": task, "text": "hi"}},
		{"respond", map[string]any{"workspaceId": ws, "taskId": task, "action": "allow"}},
		{"updateStatus", map[string]any{"workspaceId": ws, "taskId": task, "status": "ongoing"}},
		{"updateSortingOrder", map[string]any{"workspaceId": ws, "taskId": task, "sortOrder": 1.5}},
		{"updateAssignee", map[string]any{"workspaceId": ws, "taskId": task, "assignee": "agent"}},
		{"updateAllowAll", map[string]any{"workspaceId": ws, "taskId": task, "allowAll": true}},
		{"updateSchedule", map[string]any{"workspaceId": ws, "taskId": task, "title": "t"}},
	}
}

func runChangingAction(t *testing.T, action string, payload map[string]any) string {
	t.Helper()
	srv := &WorkspaceServer{crud: &busyTaskCrud{}}
	res := toolResult(srv.handleTask(authedContext(), nil, TaskParams{Action: action, Payload: payload}))
	if res.isError {
		t.Fatalf("%s failed: %s", action, res.text)
	}
	return res.text
}

func sortedKeys(m map[string]json.RawMessage) []string {
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// By default an action that changes a task answers only which task it is and
// where it stands, under the same task key the whole task came under, so a
// caller reading task.id or task.status reads it as before.
func TestTaskTool_ChangingATaskAnswersItsSummary(t *testing.T) {
	for _, tc := range changingActions() {
		t.Run(tc.action, func(t *testing.T) {
			text := runChangingAction(t, tc.action, tc.payload)
			var top map[string]json.RawMessage
			if err := json.Unmarshal([]byte(text), &top); err != nil {
				t.Fatalf("%s answered %q, not JSON: %v", tc.action, text, err)
			}
			wantTop := []string{"task"}
			if tc.action == "reply" {
				wantTop = []string{"messageId", "task"}
			}
			if got := sortedKeys(top); !slices.Equal(got, wantTop) {
				t.Errorf("%s answered the fields %v, want %v", tc.action, got, wantTop)
			}
			var task map[string]json.RawMessage
			if err := json.Unmarshal(top["task"], &task); err != nil {
				t.Fatalf("%s answered a task that is no object: %s", tc.action, top["task"])
			}
			if got, want := sortedKeys(task), []string{"assignee", "id", "status", "title", "updatedAt", "workspaceId"}; !slices.Equal(got, want) {
				t.Errorf("%s answered a task with %v, want %v", tc.action, got, want)
			}
			var summary struct {
				Task struct {
					ID, WorkspaceID, Title, Status, Assignee string
					UpdatedAt                                time.Time
				}
				MessageID string
			}
			_ = json.Unmarshal([]byte(text), &summary)
			if summary.Task.ID != base62(testTriggerID) || summary.Task.WorkspaceID != base62(testWorkspace) ||
				summary.Task.Title != "Release v0.9.20" || summary.Task.Status != "ongoing" || summary.Task.Assignee != "agent" ||
				!summary.Task.UpdatedAt.Equal(time.Date(2026, 11, 14, 4, 8, 11, 0, time.UTC)) {
				t.Errorf("%s answered %s", tc.action, text)
			}
			if tc.action == "reply" && summary.MessageID != base62(compactReplyMessageID) {
				t.Errorf("reply answered the message id %q, want %q", summary.MessageID, base62(compactReplyMessageID))
			}
			if len(text) > 300 {
				t.Errorf("%s answered %d bytes for a busy task, want at most 300", tc.action, len(text))
			}
		})
	}
}

// verbose: true answers with the whole task, as every action did before.
func TestTaskTool_ChangingATaskVerboselyAnswersTheWholeTask(t *testing.T) {
	for _, tc := range changingActions() {
		t.Run(tc.action, func(t *testing.T) {
			payload := map[string]any{"verbose": true}
			for k, v := range tc.payload {
				payload[k] = v
			}
			text := runChangingAction(t, tc.action, payload)
			var full struct {
				Task struct {
					ID        string            `json:"id"`
					Body      string            `json:"body"`
					Messages  []json.RawMessage `json:"messages"`
					ToolCalls []json.RawMessage `json:"toolCalls"`
				} `json:"task"`
			}
			if err := json.Unmarshal([]byte(text), &full); err != nil {
				t.Fatalf("%s answered %q, not JSON: %v", tc.action, text, err)
			}
			if full.Task.ID != base62(testTriggerID) || full.Task.Body == "" || len(full.Task.Messages) != 40 || len(full.Task.ToolCalls) != 150 {
				t.Errorf("%s answered id %q, a body of %d bytes, %d messages and %d tool calls; want the whole task",
					tc.action, full.Task.ID, len(full.Task.Body), len(full.Task.Messages), len(full.Task.ToolCalls))
			}
			t.Logf("%s on a busy task: %d bytes verbose, %d by default", tc.action, len(text),
				len(runChangingAction(t, tc.action, tc.payload)))
		})
	}
}

// Every action that answers a task says so in its payload's help: verbose is
// a field its strict payload accepts and help prints.
func TestTaskTool_HelpOffersVerboseOnEveryChangingAction(t *testing.T) {
	srv := &WorkspaceServer{crud: &busyTaskCrud{}}
	for _, tc := range changingActions() {
		res := toolResult(srv.handleTask(authedContext(), nil, TaskParams{Action: "help", Payload: map[string]any{"topic": tc.action}}))
		if res.isError || !strings.Contains(res.text, "verbose") {
			t.Errorf("help for %s says nothing of verbose: %s", tc.action, res.text)
		}
	}
}
