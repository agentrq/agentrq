// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/mustafaturan/monoflake"
)

func TestFromEntityAttachmentsToView_KeepsTheLink(t *testing.T) {
	got := fromEntityAttachmentsToView([]entity.Attachment{
		{ID: "a", Filename: "a.png", MimeType: "image/png", URL: "https://cdn/attachments/a"},
		{ID: "b", Filename: "b.txt", MimeType: "text/plain"},
	})
	b, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	// camelCase, and no url key at all for a local attachment.
	want := `[{"id":"a","filename":"a.png","mimeType":"image/png","data":"","url":"https://cdn/attachments/a"},{"id":"b","filename":"b.txt","mimeType":"text/plain","data":""}]`
	if string(b) != want {
		t.Errorf("got %s", b)
	}
}

func TestFromGetTaskResponseEntityToHTTPResponse_StateTransitions(t *testing.T) {
	at := time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)
	closeAfter := int64(600)
	raw := string(FromGetTaskResponseEntityToHTTPResponse(&entity.GetTaskResponse{
		Task: entity.Task{ID: 1},
		StateTransitions: []entity.TaskStateTransition{
			{FromState: "", ToState: "notstarted", CreatedAt: at},
			{FromState: "notstarted", ToState: "ongoing", Agent: "gemini", AgentModel: "Gemini 3 Flash", CreatedAt: at},
		},
		Timing: entity.TaskTiming{StartedAt: &at, StartToCloseSeconds: &closeAfter, BlockedSeconds: 3, NeedsInputSeconds: 5, WorkedSeconds: 4},
	}))
	for _, want := range []string{
		// No agent keys on a change nobody's agent made.
		`"stateTransitions":[{"fromState":"","toState":"notstarted","createdAt":"2026-09-27T10:00:00Z"},` +
			`{"fromState":"notstarted","toState":"ongoing","agent":"gemini","agentModel":"Gemini 3 Flash","createdAt":"2026-09-27T10:00:00Z"}]`,
		`"timing":{"startedAt":"2026-09-27T10:00:00Z","closedAt":null,"startToCloseSeconds":600,"blockedSeconds":3,"needsInputSeconds":5,"workedSeconds":4}`,
	} {
		if !strings.Contains(raw, want) {
			t.Errorf("response %s\nlacks %s", raw, want)
		}
	}
}

// A task view lists its tool calls the same way a toolCall.updated event
// carries one, so a client can upsert either into the other.
func TestFromModelTaskToView_ListsToolCallsAsTheEventCarriesThem(t *testing.T) {
	tc := model.ToolCall{ID: 5, TaskID: 101, WorkspaceID: 10, ToolName: "Bash", InputPreview: `{"command":"ls"}`, Status: "pending", CreatedAt: time.Unix(1, 0)}

	got := FromModelTaskToView(model.Task{ID: 101, WorkspaceID: 10, ToolCalls: []model.ToolCall{tc}})
	event := FromModelToolCallToUpdatedView(10, tc)

	if len(got.ToolCalls) != 1 || got.ToolCalls[0] != event.ToolCall {
		t.Fatalf("task view lists %+v, event carries %+v", got.ToolCalls, event.ToolCall)
	}
	if event.WorkspaceID != monoflake.ID(10).String() || event.ToolCall.TaskID != monoflake.ID(101).String() {
		t.Errorf("event = %+v", event)
	}
}

// A task view counts every tool call, though it lists only the latest.
func TestTaskViews_CountToolCalls(t *testing.T) {
	if got := FromModelTaskToView(model.Task{ID: 1, ToolCallCount: 250}); got.ToolCallCount != 250 {
		t.Errorf("event view counts %d, want 250", got.ToolCallCount)
	}
	if got := FromEntityTaskToView(entity.Task{ID: 1, ToolCallCount: 250}); got.ToolCallCount != 250 {
		t.Errorf("REST view counts %d, want 250", got.ToolCallCount)
	}
}
