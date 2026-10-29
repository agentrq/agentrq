// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package coremcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	view "github.com/agentrq/agentrq/backend/internal/data/view/api"
)

var threadStart = time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)

// message is the nth message of a thread, a minute after the one before it.
func message(n int, sender, text string, metadata any) entity.Message {
	return entity.Message{
		ID:        int64(1000 + n),
		CreatedAt: threadStart.Add(time.Duration(n) * time.Minute),
		TaskID:    testTriggerID,
		Sender:    sender,
		Text:      text,
		Metadata:  metadata,
	}
}

// busyTask is a task an agent has worked on for a while: a question in the
// middle of its thread, between the noise of it working.
func busyTask() *entity.GetTaskResponse {
	question := message(4, "agent", "Which branch should I release from?", map[string]any{"type": "elicitation_request", "status": "pending"})
	reply := message(1, "agent", "On it.", nil)
	reply.Attachments = []entity.Attachment{{ID: "a1", Filename: "plan.md", Data: "aGVsbG8="}, {ID: "a2", Filename: "chart.png"}}
	return &entity.GetTaskResponse{Task: entity.Task{
		ID:          testTriggerID,
		WorkspaceID: testWorkspace,
		Status:      "blocked",
		UpdatedAt:   threadStart.Add(time.Hour),
		// Out of order, as a preload may return them.
		Messages: []entity.Message{
			question,
			message(0, "human", "Release v0.9.20.", nil),
			message(2, "agent", "Allow Bash: git tag v0.9.20", map[string]any{"type": "permission_request"}),
			message(3, "agent", "Thinking about the branch...", map[string]any{"type": "agent_thought"}),
			reply,
			message(5, "agent", "12k tokens", map[string]any{"type": "agent_usage"}),
			message(6, "agent", "The session restarted", map[string]any{"type": "agent_notice"}),
			message(7, "human", "From main.", map[string]any{"type": ""}),
		},
		ToolCalls:     []entity.ToolCall{{ID: 1, TaskID: testTriggerID, ToolName: "Bash", InputPreview: "git log --oneline -500"}},
		ToolCallCount: 1,
	}}
}

func busyTaskServer(t *testing.T) *WorkspaceServer {
	return taskServer(&mockTaskCrud{getTask: func(_ context.Context, req entity.GetTaskRequest) (*entity.GetTaskResponse, error) {
		if req.WorkspaceID != testWorkspace || req.TaskID != testTriggerID || req.UserID != testUserID {
			t.Errorf("GetTask(%+v), want workspace %d, task %d, user %s", req, testWorkspace, testTriggerID, testUserID)
		}
		return busyTask(), nil
	}})
}

func callTask(t *testing.T, srv *WorkspaceServer, action string, payload map[string]any) callResult {
	t.Helper()
	payload["workspaceId"] = base62(testWorkspace)
	payload["taskId"] = base62(testTriggerID)
	return toolResult(srv.handleTask(authedContext(), nil, TaskParams{Action: action, Payload: payload}))
}

func taskMessages(t *testing.T, srv *WorkspaceServer, payload map[string]any) view.TaskMessagesResponse {
	t.Helper()
	var got view.TaskMessagesResponse
	if err := json.Unmarshal([]byte(textOf(t, callTask(t, srv, "messages", payload))), &got); err != nil {
		t.Fatal(err)
	}
	return got
}

func texts(msgs []view.TaskMessage) []string {
	var out []string
	for _, m := range msgs {
		out = append(out, m.Text)
	}
	return out
}

// A supervisor following up wants what was said, not the agent at work: an
// agent's question counts, its permission requests, thoughts, usage and
// notices do not.
func TestTaskMessages_ReadsOnlyTheConversationOldestFirst(t *testing.T) {
	got := taskMessages(t, busyTaskServer(t), map[string]any{})

	want := []string{"Release v0.9.20.", "On it.", "Which branch should I release from?", "From main."}
	if !slices.Equal(texts(got.Messages), want) {
		t.Errorf("messages = %q, want %q", texts(got.Messages), want)
	}
	if got.Status != "blocked" || !got.UpdatedAt.Equal(threadStart.Add(time.Hour)) || got.Total != 4 {
		t.Errorf("status %q, updatedAt %v, total %d; want blocked, %v, 4", got.Status, got.UpdatedAt, got.Total, threadStart.Add(time.Hour))
	}
	first, second := got.Messages[0], got.Messages[1]
	if first.ID != base62(1000) || !first.CreatedAt.Equal(threadStart) || first.Sender != "human" || first.Attachments != nil {
		t.Errorf("first message = %+v, want id %s, created %v, sent by the human, no attachments", first, base62(1000), threadStart)
	}
	if !slices.Equal(second.Attachments, []string{"plan.md", "chart.png"}) {
		t.Errorf("attachments = %q, want only their names", second.Attachments)
	}
}

// since is the createdAt of the last message read: that one is not sent again.
func TestTaskMessages_SinceKeepsOnlyLaterMessages(t *testing.T) {
	got := taskMessages(t, busyTaskServer(t), map[string]any{"since": threadStart.Add(time.Minute).Format(time.RFC3339Nano)})

	want := []string{"Which branch should I release from?", "From main."}
	if !slices.Equal(texts(got.Messages), want) || got.Total != 2 {
		t.Errorf("messages = %q, total %d; want %q, 2", texts(got.Messages), got.Total, want)
	}
}

// The newest reply is the one a supervisor is following up on, so a limit
// cuts the oldest; total says how many there were.
func TestTaskMessages_LimitKeepsTheNewest(t *testing.T) {
	got := taskMessages(t, busyTaskServer(t), map[string]any{"limit": 2})

	want := []string{"Which branch should I release from?", "From main."}
	if !slices.Equal(texts(got.Messages), want) || got.Total != 4 {
		t.Errorf("messages = %q, total %d; want %q, 4", texts(got.Messages), got.Total, want)
	}
}

func TestTaskMessages_LimitDefaultsTo20AndIsCappedAt100(t *testing.T) {
	var thread []entity.Message
	for n := range 150 {
		thread = append(thread, message(n, "agent", fmt.Sprint(n), nil))
	}
	srv := taskServer(&mockTaskCrud{getTask: func(context.Context, entity.GetTaskRequest) (*entity.GetTaskResponse, error) {
		return &entity.GetTaskResponse{Task: entity.Task{Messages: thread}}, nil
	}})

	for _, tc := range []struct {
		limit       int
		want, first int
	}{{0, 20, 130}, {500, 100, 50}} {
		got := taskMessages(t, srv, map[string]any{"limit": tc.limit})
		if len(got.Messages) != tc.want || got.Messages[0].Text != fmt.Sprint(tc.first) || got.Total != 150 {
			t.Errorf("limit %d: %d messages from %q, total %d; want %d from %d, total 150", tc.limit, len(got.Messages), got.Messages[0].Text, got.Total, tc.want, tc.first)
		}
	}
}

func TestTaskMessages_EmptyThreadIsAnEmptyList(t *testing.T) {
	srv := taskServer(&mockTaskCrud{getTask: func(context.Context, entity.GetTaskRequest) (*entity.GetTaskResponse, error) {
		return &entity.GetTaskResponse{Task: entity.Task{Status: "notstarted"}}, nil
	}})

	if text := textOf(t, callTask(t, srv, "messages", map[string]any{})); text != `{"status":"notstarted","updatedAt":"0001-01-01T00:00:00Z","total":0,"messages":[]}` {
		t.Errorf("an empty thread answered %s", text)
	}
}

func TestTaskMessages_RefusesABadSinceAndReportsCrudErrors(t *testing.T) {
	srv := taskServer(&mockTaskCrud{getTask: func(context.Context, entity.GetTaskRequest) (*entity.GetTaskResponse, error) {
		return nil, errors.New("task not found")
	}})

	for _, tc := range []struct {
		action  string
		payload map[string]any
		want    string
	}{
		{"messages", map[string]any{"since": "yesterday"}, "since must be an RFC3339 time, like 2026-10-26T09:00:00Z"},
		{"get", map[string]any{"messagesSince": "2026-10-26"}, "messagesSince must be an RFC3339 time, like 2026-10-26T09:00:00Z"},
		{"messages", map[string]any{}, "task not found"},
	} {
		if res := callTask(t, srv, tc.action, tc.payload); !res.isError || res.text != tc.want {
			t.Errorf("%s %v answered %q (error %v), want the error %q", tc.action, tc.payload, res.text, res.isError, tc.want)
		}
	}
}

func getTask(t *testing.T, payload map[string]any) view.GetTaskResponse {
	t.Helper()
	var got view.GetTaskResponse
	if err := json.Unmarshal([]byte(textOf(t, callTask(t, busyTaskServer(t), "get", payload))), &got); err != nil {
		t.Fatal(err)
	}
	return got
}

// The tool calls are most of a busy task's size, so get leaves them out
// unless asked; their count stays, to say how busy the task is.
func TestGetTask_LeavesOutToolCallsUnlessAsked(t *testing.T) {
	for _, tc := range []struct {
		payload   map[string]any
		toolCalls int
	}{{map[string]any{}, 0}, {map[string]any{"includeToolCalls": false}, 0}, {map[string]any{"includeToolCalls": true}, 1}} {
		got := getTask(t, tc.payload)
		if len(got.Task.ToolCalls) != tc.toolCalls || got.Task.ToolCallCount != 1 || len(got.Task.Messages) != 8 {
			t.Errorf("get %v: %d tool calls (count %d), %d messages; want %d (count 1) and 8", tc.payload, len(got.Task.ToolCalls), got.Task.ToolCallCount, len(got.Task.Messages), tc.toolCalls)
		}
	}
}

func TestGetTask_MessagesSinceKeepsOnlyLaterMessages(t *testing.T) {
	got := getTask(t, map[string]any{"messagesSince": threadStart.Add(5 * time.Minute).Format(time.RFC3339)})

	var kept []string
	for _, m := range got.Task.Messages {
		kept = append(kept, m.Text)
	}
	if want := []string{"The session restarted", "From main."}; !slices.Equal(kept, want) {
		t.Errorf("messages = %q, want %q", kept, want)
	}
}
