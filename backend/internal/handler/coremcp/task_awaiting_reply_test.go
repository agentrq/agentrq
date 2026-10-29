// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package coremcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	view "github.com/agentrq/agentrq/backend/internal/data/view/api"
)

// The conversations of the repository's awaitingReplyCases, which its
// awaitingReplyClause must judge the same way.
func TestReplyState(t *testing.T) {
	cases := []struct {
		name     string
		msgs     []entity.Message
		awaiting bool
		lastBy   string
	}{
		{name: "an agent's question", awaiting: true, lastBy: "agent", msgs: []entity.Message{
			message(0, "human", "Fix the build.", nil),
			message(1, "agent", "Should I also bump the version?\n \t", nil),
		}},
		{name: "answered by the human", lastBy: "human", msgs: []entity.Message{
			message(0, "agent", "Should I also bump the version?", nil),
			message(1, "human", "No.", nil),
		}},
		{name: "answered from Slack", lastBy: "slack", msgs: []entity.Message{
			message(0, "agent", "Should I also bump the version?", nil),
			message(1, "slack", "No.", map[string]any{"slack_user": "U1"}),
		}},
		{name: "the agent at work after its question", awaiting: true, lastBy: "agent", msgs: []entity.Message{
			message(0, "agent", "Which branch?", nil),
			message(1, "agent", "1200 tokens", map[string]any{"type": "usage"}),
		}},
		{name: "a statement", lastBy: "agent", msgs: []entity.Message{
			message(0, "agent", "Done. What else? Nothing, it is merged.", nil),
		}},
		{name: "a pending permission request", awaiting: true, lastBy: "agent", msgs: []entity.Message{
			message(0, "agent", "Run git push", map[string]any{"type": "permission_request", "status": "pending"}),
			message(1, "agent", "Waiting for approval.", nil),
		}},
		{name: "a pending elicitation", awaiting: true, lastBy: "agent", msgs: []entity.Message{
			message(0, "agent", "Pick a name", map[string]any{"type": "elicitation_request", "status": "pending"}),
		}},
		{name: "an answered elicitation", lastBy: "agent", msgs: []entity.Message{
			message(0, "agent", "Merge it?", map[string]any{"type": "elicitation_request", "status": "accept"}),
		}},
		{name: "an allowed permission request", msgs: []entity.Message{
			message(0, "agent", "Run git push?", map[string]any{"type": "permission_request", "status": "allow"}),
		}},
		{name: "no messages"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := replyState(c.msgs)
			if got.AwaitingReply != c.awaiting {
				t.Errorf("awaitingReply = %v, want %v", got.AwaitingReply, c.awaiting)
			}
			if got.LastMessageBy != c.lastBy {
				t.Errorf("lastMessageBy = %q, want %q", got.LastMessageBy, c.lastBy)
			}
			if c.lastBy == "" && !got.LastMessageAt.IsZero() {
				t.Errorf("lastMessageAt = %v with no conversation, want none", got.LastMessageAt)
			}
		})
	}
}

// list and listAll filter in the query, so paging still holds, and answer
// from only the messages the rule reads, which never reach the reply.
func TestListTasks_AwaitingReply(t *testing.T) {
	question := message(1, "agent", "Should I also bump the version?", nil)
	var got []entity.ListTasksRequest
	srv := taskServer(&mockTaskCrud{listTasks: func(_ context.Context, req entity.ListTasksRequest) (*entity.ListTasksResponse, error) {
		got = append(got, req)
		return &entity.ListTasksResponse{Tasks: []entity.Task{
			{ID: testTriggerID, WorkspaceID: testWorkspace, Status: "ongoing", Messages: []entity.Message{question}},
			{ID: testTriggerID + 1, WorkspaceID: testWorkspace, Status: "notstarted"},
		}}, nil
	}})

	for _, text := range []string{
		textOf(t, toolResult(srv.handleListTasks(authedContext(), nil, ListTasksParams{WorkspaceID: base62(testWorkspace), AwaitingReply: true}))),
		textOf(t, toolResult(srv.handleListAllTasks(authedContext(), nil, ListAllTasksParams{AwaitingReply: true}))),
	} {
		var res view.SupervisorListTasksResponse
		if err := json.Unmarshal([]byte(text), &res); err != nil {
			t.Fatalf("list answered %s: %v", text, err)
		}
		if len(res.Tasks) != 2 {
			t.Fatalf("listed %d tasks, want 2", len(res.Tasks))
		}
		asked, idle := res.Tasks[0], res.Tasks[1]
		if !asked.AwaitingReply || asked.LastMessageBy != "agent" || asked.LastMessageAt == nil || !asked.LastMessageAt.Equal(question.CreatedAt) {
			t.Errorf("the asking task = awaitingReply %v, lastMessageBy %q, lastMessageAt %v; want true, agent, %v", asked.AwaitingReply, asked.LastMessageBy, asked.LastMessageAt, question.CreatedAt)
		}
		if idle.AwaitingReply || idle.LastMessageBy != "" || idle.LastMessageAt != nil {
			t.Errorf("the idle task = %+v, want no reply awaited and no last message", idle)
		}
		if strings.Contains(text, `"messages"`) {
			t.Errorf("list answered %s, want no messages", text)
		}
		if !strings.Contains(text, `"awaitingReply":false`) {
			t.Errorf("list answered %s, want awaitingReply false spelled out", text)
		}
	}
	for i, req := range got {
		if !req.AwaitingReply || !req.PreloadReplyMessages {
			t.Errorf("call %d: AwaitingReply %v, PreloadReplyMessages %v; want both", i, req.AwaitingReply, req.PreloadReplyMessages)
		}
	}
}

// get answers from the whole conversation, even when messagesSince leaves
// out the question it is waiting on.
func TestGetTask_AwaitingReply(t *testing.T) {
	text := textOf(t, callTask(t, busyTaskServer(t), "get", map[string]any{"messagesSince": threadStart.Add(time.Hour).Format(time.RFC3339)}))
	var res view.SupervisorGetTaskResponse
	if err := json.Unmarshal([]byte(text), &res); err != nil {
		t.Fatalf("get answered %s: %v", text, err)
	}
	if !res.Task.AwaitingReply {
		t.Error("awaitingReply = false with an elicitation pending, want true")
	}
	if want := threadStart.Add(7 * time.Minute); res.Task.LastMessageBy != "human" || res.Task.LastMessageAt == nil || !res.Task.LastMessageAt.Equal(want) {
		t.Errorf("last message by %q at %v, want the human's at %v", res.Task.LastMessageBy, res.Task.LastMessageAt, want)
	}
	if len(res.Task.Messages) != 0 {
		t.Errorf("get kept %d messages, want none after messagesSince", len(res.Task.Messages))
	}
	if res.Task.ID != base62(testTriggerID) || res.Timing.WorkedSeconds != 0 || res.StateTransitions == nil {
		t.Errorf("get answered %s, want the task with its timing and state transitions", text)
	}
}
