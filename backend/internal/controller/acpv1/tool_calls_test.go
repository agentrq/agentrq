// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package acpv1

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/controller/agentlink"
)

// rows is what the engine recorded: the rows and the status changes after.
func (r *recorder) rows() ([]recordedCall, []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]recordedCall(nil), r.toolCalls...), append([]string(nil), r.callStatus...)
}

func TestToolCallRows(t *testing.T) {
	ctx := context.Background()
	request := string(fixture(t, "session_request_permission.request.json")) // asks about call-2

	t.Run("a call run without asking is recorded once it runs, and its row follows it", func(t *testing.T) {
		rec, a, l := midTurn(t, agentOpts{})
		a.update("sess-1", `{"sessionUpdate":"tool_call","toolCallId":"c1","title":"Read a.go","kind":"read","status":"pending","rawInput":{"path":"a.go"}}`)
		a.sync(l)
		if calls, _ := rec.rows(); len(calls) != 0 {
			t.Fatalf("a pending call was recorded %+v, want nothing until it runs", calls)
		}
		a.update("sess-1", `{"sessionUpdate":"tool_call_update","toolCallId":"c1","status":"in_progress"}`)
		a.update("sess-1", `{"sessionUpdate":"tool_call_update","toolCallId":"c1","status":"in_progress"}`)
		a.update("sess-1", `{"sessionUpdate":"tool_call_update","toolCallId":"c1","status":"completed"}`)
		a.sync(l)
		calls, statuses := rec.rows()
		want := []recordedCall{{42, 100, agentlink.ToolCall{ToolName: "Read a.go", Description: "Read a.go", InputPreview: `{"path":"a.go"}`, Status: "running"}}}
		if !slices.Equal(calls, want) {
			t.Fatalf("the rows recorded were %+v, want %+v", calls, want)
		}
		if want := []string{"100|completed"}; !slices.Equal(statuses, want) {
			t.Fatalf("the row's statuses were %q, want %q", statuses, want)
		}
	})

	t.Run("a call first reported finished is recorded as it ended, named as its card would be", func(t *testing.T) {
		rec, a, l := midTurn(t, agentOpts{})
		a.update("sess-1", `{"sessionUpdate":"tool_call","toolCallId":"c1","name":"grep","status":"failed"}`)
		a.update("sess-1", `{"sessionUpdate":"tool_call","toolCallId":"c2","status":"completed"}`)
		a.sync(l)
		calls, statuses := rec.rows()
		want := []recordedCall{
			{42, 100, agentlink.ToolCall{ToolName: "grep", Description: "grep", InputPreview: "{}", Status: "failed"}},
			{42, 101, agentlink.ToolCall{ToolName: "Unknown Tool", Description: "Unknown Tool", InputPreview: "{}", Status: "completed"}},
		}
		if !slices.Equal(calls, want) || len(statuses) != 0 {
			t.Fatalf("the rows were %+v then %q, want %+v and no status change", calls, statuses, want)
		}
	})

	t.Run("a call asked about gets its row before the request, which owns it from then on", func(t *testing.T) {
		rec, a, l := midTurn(t, agentOpts{})
		a.update("sess-1", `{"sessionUpdate":"tool_call","toolCallId":"call-2","title":"Bash","status":"pending","rawInput":{"cmd":"ls"}}`)
		a.request(5, methodRequestPermission, request)
		p := rec.permissionRequests(t, 1)[0]
		if p.ToolCallRowID != 100 {
			t.Fatalf("the request named row %d, want the row recorded for it, 100", p.ToolCallRowID)
		}
		if !l.DeliverVerdict(ctx, p.RequestID, "allow") {
			t.Fatal("the verdict for the waiting call was refused")
		}
		a.answerTo(5)
		a.update("sess-1", `{"sessionUpdate":"tool_call_update","toolCallId":"call-2","status":"in_progress"}`)
		a.update("sess-1", `{"sessionUpdate":"tool_call_update","toolCallId":"call-2","status":"completed"}`)
		a.sync(l)
		calls, statuses := rec.rows()
		want := []recordedCall{{42, 100, agentlink.ToolCall{ToolName: "Bash", Description: "Bash", InputPreview: `{"cmd":"ls"}`, Status: "pending"}}}
		if !slices.Equal(calls, want) || len(statuses) != 0 {
			t.Fatalf("the engine recorded %+v then %q, want %+v and the verdict kept", calls, statuses, want)
		}
	})

	t.Run("an allowed call that fails moves its row to failed, with what it said", func(t *testing.T) {
		rec, a, l := midTurn(t, agentOpts{})
		a.request(5, methodRequestPermission, request)
		p := rec.permissionRequests(t, 1)[0]
		l.DeliverVerdict(ctx, p.RequestID, "allow")
		a.answerTo(5)
		a.update("sess-1", `{"sessionUpdate":"tool_call","toolCallId":"call-2","title":"Bash","status":"in_progress"}`)
		a.update("sess-1", `{"sessionUpdate":"tool_call_update","toolCallId":"call-2","status":"failed","content":[{"type":"content","content":{"type":"text","text":"exit status 1"}}]}`)
		a.sync(l)
		if _, statuses := rec.rows(); !slices.Equal(statuses, []string{"100|failed|exit status 1"}) {
			t.Fatalf("the row's statuses were %q, want it failed with the agent's words", statuses)
		}
	})

	t.Run("a denied call that fails keeps its verdict", func(t *testing.T) {
		rec, a, l := midTurn(t, agentOpts{})
		a.request(5, methodRequestPermission, request)
		p := rec.permissionRequests(t, 1)[0]
		l.DeliverVerdict(ctx, p.RequestID, "deny")
		a.answerTo(5)
		a.update("sess-1", `{"sessionUpdate":"tool_call_update","toolCallId":"call-2","status":"failed","rawOutput":"the user refused"}`)
		a.sync(l)
		if _, statuses := rec.rows(); len(statuses) != 0 {
			t.Fatalf("the row's statuses were %q, want the denial kept", statuses)
		}
	})

	t.Run("a call run without asking that fails keeps what it said", func(t *testing.T) {
		rec, a, l := midTurn(t, agentOpts{})
		a.update("sess-1", `{"sessionUpdate":"tool_call","toolCallId":"c1","title":"Read","status":"in_progress"}`)
		a.update("sess-1", `{"sessionUpdate":"tool_call_update","toolCallId":"c1","status":"failed","rawOutput":{"error":"no such file"}}`)
		a.update("sess-1", `{"sessionUpdate":"tool_call","toolCallId":"c2","title":"Grep","status":"failed","rawOutput":"bad pattern"}`)
		a.sync(l)
		calls, statuses := rec.rows()
		if len(calls) != 2 || calls[1].call.Output != "bad pattern" || calls[1].call.Status != "failed" {
			t.Fatalf("the rows were %+v, want Grep recorded failed with its words", calls)
		}
		if want := []string{`100|failed|{"error":"no such file"}`}; !slices.Equal(statuses, want) {
			t.Fatalf("the row's statuses were %q, want %q", statuses, want)
		}
	})

	t.Run("a call that runs before it asks hands its row to the request", func(t *testing.T) {
		rec, a, l := midTurn(t, agentOpts{})
		a.update("sess-1", `{"sessionUpdate":"tool_call","toolCallId":"call-2","title":"Bash","status":"in_progress","rawInput":{"cmd":"ls"}}`)
		a.request(5, methodRequestPermission, request)
		p := rec.permissionRequests(t, 1)[0]
		if p.ToolCallRowID != 100 {
			t.Fatalf("the request named row %d, want the call's row 100", p.ToolCallRowID)
		}
		l.DeliverVerdict(ctx, p.RequestID, "allow")
		a.answerTo(5)
		a.update("sess-1", `{"sessionUpdate":"tool_call_update","toolCallId":"call-2","status":"completed"}`)
		a.sync(l)
		calls, statuses := rec.rows()
		if len(calls) != 1 || len(statuses) != 0 {
			t.Fatalf("the engine recorded %+v then %q, want one row and its verdict kept", calls, statuses)
		}
	})

	t.Run("the workspace's own tools and an idle session's calls are not recorded", func(t *testing.T) {
		rec, a, l := midTurn(t, agentOpts{})
		a.update("sess-1", `{"sessionUpdate":"tool_call","toolCallId":"c1","title":"mcp__agentrq-workspace__task","status":"in_progress","rawInput":{"action":"get"}}`)
		a.update("sess-1", `{"sessionUpdate":"tool_call_update","toolCallId":"c1","status":"completed"}`)
		a.update("sess-1", `{"sessionUpdate":"tool_call","toolCallId":"c2","title":"Replying to task","name":"mcp__agentrq-workspace__task","status":"in_progress","rawInput":{"action":"reply"}}`)
		a.update("sess-1", `{"sessionUpdate":"tool_call_update","toolCallId":"c2","status":"completed"}`)
		a.sync(l)

		idle := &recorder{}
		b := startAgent(t, idle)
		il := b.ready(idle, `{"sessionId":"idle-1"}`)
		b.update("idle-1", `{"sessionUpdate":"tool_call","toolCallId":"c1","title":"Read","status":"in_progress"}`)
		b.sync(il)
		for _, r := range []*recorder{rec, idle} {
			if calls, statuses := r.rows(); len(calls) != 0 || len(statuses) != 0 {
				t.Fatalf("the engine recorded %+v then %q, want nothing", calls, statuses)
			}
		}
	})
}

func TestFailureOutput(t *testing.T) {
	failed, completed := "failed", "completed"
	for _, tc := range []struct {
		name string
		u    toolCallUpdate
		want string
	}{
		{"a call that did not fail says nothing", toolCallUpdate{Status: &completed, RawOutput: json.RawMessage(`"fine"`)}, ""},
		{"text content, joined", toolCallUpdate{Status: &failed, RawOutput: json.RawMessage(`"ignored"`), Content: []toolContent{
			{Type: "content", Content: contentBlock{Type: "text", Text: "first"}},
			{Type: "content", Content: contentBlock{Type: "text", Text: "  "}},
			{Type: "diff"},
			{Type: "content", Content: contentBlock{Type: "image"}},
			{Type: "content", Content: contentBlock{Type: "text", Text: "second"}},
		}}, "first\nsecond"},
		{"a raw output string", toolCallUpdate{Status: &failed, RawOutput: json.RawMessage(`"boom"`)}, "boom"},
		{"a raw output object", toolCallUpdate{Status: &failed, RawOutput: json.RawMessage(`{"code":2}`)}, `{"code":2}`},
		{"a null raw output", toolCallUpdate{Status: &failed, RawOutput: json.RawMessage(`null`)}, ""},
		{"nothing at all", toolCallUpdate{Status: &failed}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := failureOutput(tc.u); got != tc.want {
				t.Errorf("failureOutput = %q, want %q", got, tc.want)
			}
		})
	}
}
