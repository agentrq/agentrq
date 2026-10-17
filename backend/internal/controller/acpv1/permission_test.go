// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package acpv1

import (
	"context"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/agentrq/agentrq/backend/internal/controller/agentlink"
)

// request sends a request as the agent.
func (a *agent) request(id int, method, params string) {
	a.t.Helper()
	a.write(`{"jsonrpc":"2.0","id":` + strconv.Itoa(id) + `,"method":"` + method + `","params":` + params + `}`)
}

// answerTo returns AgentRQ's answer to the agent's request id, keeping any
// other frame for a later expect.
func (a *agent) answerTo(id int) rpcMessage {
	a.t.Helper()
	want := strconv.Itoa(id)
	for i, m := range a.backlog {
		if m.Method == "" && string(m.ID) == want {
			a.backlog = slices.Delete(a.backlog, i, i+1)
			return m
		}
	}
	for {
		m := a.next()
		if m.Method == "" && string(m.ID) == want {
			return m
		}
		a.backlog = append(a.backlog, m)
	}
}

// midTurn is an agent with task 42's turn running on sess-1.
func midTurn(t *testing.T, o agentOpts) (*recorder, *agent, agentlink.Link) {
	t.Helper()
	rec := &recorder{}
	a := startAgentWith(t, rec, o)
	l := a.readyAsking(rec)
	l.DeliverTask(context.Background(), 42, "go")
	a.expect("session/prompt")
	return rec, a, l
}

func (r *recorder) permissionRequests(t *testing.T, n int) []agentlink.PermissionRequest {
	t.Helper()
	r.wait(t, func() bool { return len(r.permissions) == n })
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]agentlink.PermissionRequest(nil), r.permissions...)
}

func (r *recorder) closedRequests() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.closed...)
}

const (
	answerApproved  = `{"outcome":{"outcome":"selected","optionId":"approved"}}`
	answerCancelled = `{"outcome":{"outcome":"cancelled"}}`
)

func TestVerdicts(t *testing.T) {
	options := []permissionOption{
		{OptionID: "always", Name: "Always", Kind: "allow_always"},
		{OptionID: "yes", Name: "Yes", Kind: "allow_once"},
		{OptionID: "no", Name: "No, and tell the agent what to do differently", Kind: "reject_once"},
	}
	cases := []struct {
		name     string
		options  []permissionOption
		behavior string
		want     permissionOutcome
	}{
		{"allow picks the once option, never the persistent one", options, "allow", permissionOutcome{Outcome: "selected", OptionID: "yes"}},
		{"deny picks the reject option", options, "deny", permissionOutcome{Outcome: "selected", OptionID: "no"}},
		{"allow falls back to the first once option", []permissionOption{{OptionID: "go", Name: "Go", Kind: "other"}}, "allow", permissionOutcome{Outcome: "selected", OptionID: "go"}},
		{"deny never falls back to an allow option", []permissionOption{{OptionID: "a", Name: "Proceed", Kind: "allow_once"}}, "deny", permissionOutcome{Outcome: "cancelled"}},
		{"deny falls back to a once option that is not an allow", []permissionOption{{OptionID: "a", Name: "Proceed", Kind: "allow_once"}, {OptionID: "s", Name: "Skip", Kind: "other"}}, "deny", permissionOutcome{Outcome: "selected", OptionID: "s"}},
		{"deny never picks an allow option whose name has a no in it", []permissionOption{{OptionID: "a", Name: "Allow now", Kind: "allow_once"}, {OptionID: "r", Name: "Reject", Kind: "reject_once"}}, "deny", permissionOutcome{Outcome: "selected", OptionID: "r"}},
		{"allow never picks a reject option whose name mentions allowing", []permissionOption{{OptionID: "r", Name: "Reject: allow nothing", Kind: "reject_once"}, {OptionID: "a", Name: "Run it", Kind: "allow_once"}}, "allow", permissionOutcome{Outcome: "selected", OptionID: "a"}},
		{"deny reads a no in the name of an option of no known kind", []permissionOption{{OptionID: "k", Name: "Know more", Kind: "other"}, {OptionID: "n", Name: "No", Kind: "other"}}, "deny", permissionOutcome{Outcome: "selected", OptionID: "n"}},
		{"only persistent options cancel", []permissionOption{{OptionID: "r", Name: "Remember this", Kind: "allow_once"}}, "allow", permissionOutcome{Outcome: "cancelled"}},
		{"only persistent options offered: allow cancels", []permissionOption{{OptionID: "aa", Name: "Allow always", Kind: "allow_always"}, {OptionID: "ra", Name: "Reject always", Kind: "reject_always"}}, "allow", permissionOutcome{Outcome: "cancelled"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := outcomeFor(c.options, c.behavior); got != c.want {
				t.Errorf("outcomeFor(%s) was %+v, want %+v", c.behavior, got, c.want)
			}
		})
	}

	ownCases := []struct {
		name    string
		options []permissionOption
		want    permissionOutcome
	}{
		{"one of AgentRQ's own tools takes the first allow option", options, permissionOutcome{Outcome: "selected", OptionID: "always"}},
		{"one of AgentRQ's own tools with no allow option takes the first", []permissionOption{{OptionID: "go", Name: "Go", Kind: "other"}}, permissionOutcome{Outcome: "selected", OptionID: "go"}},
		{"one of AgentRQ's own tools with no options is cancelled", nil, permissionOutcome{Outcome: "cancelled"}},
	}
	for _, c := range ownCases {
		t.Run(c.name, func(t *testing.T) {
			if got := allowOwnTool(c.options); got != c.want {
				t.Errorf("allowOwnTool was %+v, want %+v", got, c.want)
			}
		})
	}

	t.Run("a verdict answers the request it names, and Stop cancels the session's others", func(t *testing.T) {
		rec := &recorder{}
		a := startAgent(t, rec)
		l := a.ready(rec, `{"sessionId":"idle-1"}`)
		e := l.(*link).e
		got := map[string]permissionOutcome{}
		var mu sync.Mutex
		wait := func(id, sessionID string) {
			e.mu.Lock()
			e.permissions[id] = &pendingPermission{sessionID: sessionID, options: options, settle: func(o permissionOutcome) {
				mu.Lock()
				got[id] = o
				mu.Unlock()
			}}
			e.mu.Unlock()
		}
		wait("r1", "idle-1")
		wait("r2", "idle-1")
		wait("r3", "other")
		if !l.DeliverVerdict(context.Background(), "r1", "allow") {
			t.Fatal("a verdict for a waiting request was refused")
		}
		if l.DeliverVerdict(context.Background(), "r1", "allow") {
			t.Fatal("a second verdict for the same request was taken")
		}
		l.DeliverTask(context.Background(), 42, "go")
		p := a.expect("session/prompt")
		l.CancelTask(context.Background(), 42)
		a.answer(p, `{"stopReason":"cancelled"}`)
		mu.Lock()
		if got["r1"].OptionID != "yes" || got["r2"].Outcome != "cancelled" {
			t.Fatalf("outcomes were %+v, want r1 allowed once and r2 cancelled by Stop", got)
		}
		if _, settled := got["r3"]; settled {
			t.Fatal("Stop settled another session's request")
		}
		mu.Unlock()
		e.c.End(errBridgeBye) // teardown answers what is left, and closes its card
		waitFor(t, func() bool { mu.Lock(); defer mu.Unlock(); return got["r3"].Outcome == "cancelled" })
		rec.wait(t, func() bool { return slices.Contains(rec.closed, "r3") })
	})
}

func TestPermissions(t *testing.T) {
	ctx := context.Background()
	request := string(fixture(t, "session_request_permission.request.json"))

	t.Run("a permission request reaches the workspace with the gateway's request id and preview", func(t *testing.T) {
		rec, a, _ := midTurn(t, agentOpts{})
		a.request(5, methodRequestPermission, request)
		got := rec.permissionRequests(t, 1)[0]
		want := agentlink.PermissionRequest{RequestID: "acp1:" + a.connID + ":sess-1:call-2", TaskID: b62(42),
			ToolName: "Unknown Tool", Description: "Unknown Tool", InputPreview: "{}"}
		if got != want {
			t.Fatalf("the workspace was asked %+v, want %+v", got, want)
		}
	})

	t.Run("a title missing from the request is taken from the earlier tool call", func(t *testing.T) {
		rec, a, _ := midTurn(t, agentOpts{})
		a.update("sess-1", `{"sessionUpdate":"tool_call","toolCallId":"call-2","title":"Bash","status":"pending","rawInput":{"cmd":"ls"}}`)
		a.request(5, methodRequestPermission, request)
		got := rec.permissionRequests(t, 1)[0]
		if got.ToolName != "Bash" || got.Description != "Bash" || got.InputPreview != `{"cmd":"ls"}` {
			t.Fatalf("the workspace was asked %+v, want Bash with {\"cmd\":\"ls\"}", got)
		}
	})

	t.Run("a request's own title and input are used over the earlier tool call", func(t *testing.T) {
		rec, a, _ := midTurn(t, agentOpts{})
		a.update("sess-1", `{"sessionUpdate":"tool_call","toolCallId":"call-2","title":"Bash","status":"pending","rawInput":{"cmd":"ls"}}`)
		a.request(5, methodRequestPermission, `{"sessionId":"sess-1","toolCall":{"toolCallId":"call-2","title":"Edit","rawInput":{"path":"a"}},"options":[]}`)
		got := rec.permissionRequests(t, 1)[0]
		if got.ToolName != "Edit" || got.InputPreview != `{"path":"a"}` {
			t.Fatalf("the workspace was asked %+v, want Edit with {\"path\":\"a\"}", got)
		}
	})

	t.Run("AgentRQ's own tool calls are allowed without asking", func(t *testing.T) {
		rec, a, _ := midTurn(t, agentOpts{})
		a.request(5, methodRequestPermission, `{"sessionId":"sess-1","toolCall":{"toolCallId":"call-3","title":"mcp__agentrq-workspace__updateTaskStatus"},"options":[{"optionId":"approved","name":"Yes","kind":"allow_once"}]}`)
		if got := a.answerTo(5); string(got.Result) != answerApproved {
			t.Fatalf("the agent was answered %s, want %s", got.Result, answerApproved)
		}
		a.request(6, methodRequestPermission, `{"sessionId":"sess-1","toolCall":{"toolCallId":"call-4","title":"Replying to task","name":"mcp__agentrq-workspace__task"},"options":[{"optionId":"approved","name":"Yes","kind":"allow_once"}]}`)
		if got := a.answerTo(6); string(got.Result) != answerApproved {
			t.Fatalf("the agent was answered %s, want %s", got.Result, answerApproved)
		}
		a.update("sess-1", `{"sessionUpdate":"tool_call","toolCallId":"call-2","title":"Replying to task","name":"mcp__agentrq-workspace__task","status":"pending"}`)
		a.request(7, methodRequestPermission, request)
		if got := a.answerTo(7); string(got.Result) != `{"outcome":{"outcome":"selected","optionId":"approved-always"}}` {
			t.Fatalf("the agent was answered %s, want approved-always", got.Result)
		}
		a.request(8, methodRequestPermission, `{"sessionId":"sess-1","toolCall":{"toolCallId":"call-5","title":"mcp__agentrq-`+b62(100)+`__task"},"options":[{"optionId":"approved","name":"Yes","kind":"allow_once"}]}`)
		if got := a.answerTo(8); string(got.Result) != answerApproved {
			t.Fatalf("the agent was answered %s, want %s for the workspace's own agentrq-<id> server", got.Result, answerApproved)
		}
		rec.read(func() {
			if len(rec.permissions) != 0 {
				t.Fatalf("the workspace was asked %+v, want nothing for its own tool", rec.permissions)
			}
		})
	})

	t.Run("another workspace's agentrq-<id> server is asked about", func(t *testing.T) {
		rec, a, _ := midTurn(t, agentOpts{})
		a.request(5, methodRequestPermission, `{"sessionId":"sess-1","toolCall":{"toolCallId":"call-3","title":"mcp__agentrq-`+b62(200)+`__task"},"options":[{"optionId":"approved","name":"Yes","kind":"allow_once"}]}`)
		if got := rec.permissionRequests(t, 1)[0]; got.ToolName != "mcp__agentrq-"+b62(200)+"__task" {
			t.Fatalf("the workspace was asked about %q, want the other workspace's call", got.ToolName)
		}
	})

	t.Run("a verdict reaches the waiting call and answers the agent", func(t *testing.T) {
		rec, a, l := midTurn(t, agentOpts{})
		a.request(5, methodRequestPermission, request)
		id := rec.permissionRequests(t, 1)[0].RequestID
		if !l.DeliverVerdict(ctx, id, "allow") {
			t.Fatal("the verdict for the waiting call was refused")
		}
		if got := a.answerTo(5); string(got.Result) != answerApproved {
			t.Fatalf("the agent was answered %s, want %s", got.Result, answerApproved)
		}
	})

	t.Run("an unanswered request cancels the turn after the timeout", func(t *testing.T) {
		shrink(t, &defaultPermissionWait, 50*time.Millisecond)
		rec, a, _ := midTurn(t, agentOpts{})
		a.request(5, methodRequestPermission, request)
		a.expectParams(methodSessionCancel, `{"sessionId":"sess-1"}`)
		if got := a.answerTo(5); string(got.Result) != answerCancelled {
			t.Fatalf("the agent was answered %s, want %s", got.Result, answerCancelled)
		}
		id := rec.permissionRequests(t, 1)[0].RequestID
		rec.wait(t, func() bool { return slices.Contains(rec.closed, id) })
	})

	t.Run("a timeout answers the session's other waiting requests cancelled and closes their cards", func(t *testing.T) {
		shrink(t, &defaultPermissionWait, 50*time.Millisecond)
		rec, a, l := midTurn(t, agentOpts{})
		e := l.(*link).e
		got := make(chan permissionOutcome, 2)
		e.mu.Lock()
		e.permissions["same-session"] = &pendingPermission{sessionID: "sess-1", settle: func(o permissionOutcome) { got <- o }}
		e.permissions["other-session"] = &pendingPermission{sessionID: "sess-9", settle: func(o permissionOutcome) { got <- o }}
		e.mu.Unlock()
		a.request(5, methodRequestPermission, request)
		a.expectParams(methodSessionCancel, `{"sessionId":"sess-1"}`)
		if got := a.answerTo(5); string(got.Result) != answerCancelled {
			t.Fatalf("the agent was answered %s, want %s", got.Result, answerCancelled)
		}
		select {
		case o := <-got:
			if o.Outcome != "cancelled" {
				t.Fatalf("the session's other request was answered %+v, want cancelled", o)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("the session's other request was left waiting after the turn was cancelled")
		}
		rec.wait(t, func() bool { return slices.Contains(rec.closed, "same-session") })
		select {
		case o := <-got:
			t.Fatalf("another session's request was answered %+v, want it left waiting", o)
		default:
		}
		rec.read(func() {
			if slices.Contains(rec.closed, "other-session") {
				t.Fatal("another session's card was closed")
			}
		})
	})

	t.Run("a verdict that arrives after Stop answers nothing and leaves the card cancelled", func(t *testing.T) {
		rec, a, l := midTurn(t, agentOpts{})
		a.request(5, methodRequestPermission, request)
		id := rec.permissionRequests(t, 1)[0].RequestID
		if !l.CancelTask(ctx, 42) {
			t.Fatal("Stop found nothing to stop for a task whose turn was running")
		}
		a.expect(methodSessionCancel)
		if got := a.answerTo(5); string(got.Result) != answerCancelled {
			t.Fatalf("the agent was answered %s, want %s", got.Result, answerCancelled)
		}
		if l.DeliverVerdict(ctx, id, "allow") {
			t.Fatal("a verdict after Stop was taken")
		}
		a.quiet()
	})

	t.Run("the agent withdrawing its request closes the card and answers -32800", func(t *testing.T) {
		rec, a, _ := midTurn(t, agentOpts{})
		a.request(9, methodRequestPermission, request)
		id := rec.permissionRequests(t, 1)[0].RequestID
		a.write(`{"jsonrpc":"2.0","method":"$/cancel_request","params":{"requestId":9}}`)
		if got := a.answerTo(9); got.Error == nil || got.Error.Code != codeCancelled {
			t.Fatalf("the agent was answered %+v, want error -32800", got)
		}
		rec.wait(t, func() bool { return slices.Contains(rec.closed, id) })
	})

	t.Run("asking again for the same call answers the earlier request cancelled", func(t *testing.T) {
		rec, a, l := midTurn(t, agentOpts{})
		a.request(5, methodRequestPermission, request)
		id := rec.permissionRequests(t, 1)[0].RequestID
		a.request(6, methodRequestPermission, request)
		if got := a.answerTo(5); string(got.Result) != answerCancelled {
			t.Fatalf("the earlier request was answered %s, want %s", got.Result, answerCancelled)
		}
		rec.permissionRequests(t, 2)
		if !l.DeliverVerdict(ctx, id, "allow") {
			t.Fatal("the verdict for the newer request was refused")
		}
		if got := a.answerTo(6); string(got.Result) != answerApproved {
			t.Fatalf("the newer request was answered %s, want %s", got.Result, answerApproved)
		}
	})

	t.Run("a request for an unknown session is answered cancelled", func(t *testing.T) {
		rec, a, _ := midTurn(t, agentOpts{})
		a.request(5, methodRequestPermission, `{"sessionId":"nope","toolCall":{"toolCallId":"c"},"options":[]}`)
		if got := a.answerTo(5); string(got.Result) != answerCancelled {
			t.Fatalf("the agent was answered %s, want %s", got.Result, answerCancelled)
		}
		rec.read(func() {
			if len(rec.permissions) != 0 {
				t.Fatalf("the workspace was asked %+v, want nothing", rec.permissions)
			}
		})
	})

	t.Run("a request that cannot be read is answered -32602", func(t *testing.T) {
		_, a, _ := midTurn(t, agentOpts{})
		a.request(5, methodRequestPermission, `"not an object"`)
		if got := a.answerTo(5); got.Error == nil || got.Error.Code != codeInvalidParams {
			t.Fatalf("the agent was answered %+v, want error -32602", got)
		}
	})

	t.Run("a permission timeout of 0 waits for ever", func(t *testing.T) {
		shrink(t, &defaultPermissionWait, 50*time.Millisecond)
		rec, a, l := midTurn(t, agentOpts{header: map[string]string{"X-AgentRQ-Permission-Timeout": "0"}})
		a.request(5, methodRequestPermission, request)
		id := rec.permissionRequests(t, 1)[0].RequestID
		a.quiet() // well past the default, which the header switched off
		if !l.DeliverVerdict(ctx, id, "deny") {
			t.Fatal("the verdict for the waiting call was refused")
		}
		if got := a.answerTo(5); string(got.Result) != `{"outcome":{"outcome":"selected","optionId":"abort"}}` {
			t.Fatalf("the agent was answered %s, want the reject option abort", got.Result)
		}
	})

	t.Run("a request still waiting when the connection ends closes its card", func(t *testing.T) {
		rec, a, l := midTurn(t, agentOpts{})
		a.request(5, methodRequestPermission, request)
		id := rec.permissionRequests(t, 1)[0].RequestID
		l.(*link).e.c.End(errBridgeBye)
		rec.wait(t, func() bool { return slices.Contains(rec.closed, id) })
	})
}
