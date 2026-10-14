// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package acpv1

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"sync"
	"time"

	mcpctrl "github.com/agentrq/agentrq/backend/internal/controller/mcp"
)

// pendingPermission is a tool call waiting for the human.
type pendingPermission struct {
	sessionID string
	options   []permissionOption
	settle    func(permissionOutcome) // answers once; later calls do nothing
}

// requestPermission puts a tool call through the workspace's permission
// pipeline — its auto-allow rules, the card, the tool_calls row — and answers
// the agent with the option the verdict picks. It runs on the peer's request
// goroutine and may wait for half an hour, so it never holds e.mu while it
// waits.
func (e *engineV1) requestPermission(ctx context.Context, params json.RawMessage) (any, *rpcError) {
	var req requestPermissionRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, &rpcError{Code: codeInvalidParams, Message: "the permission request could not be read"}
	}
	cancelled := requestPermissionResponse{Outcome: permissionOutcome{Outcome: "cancelled"}}
	// The tool_call update that names the call usually comes just before the
	// request: let it be applied first.
	e.barrier()
	s := e.sessionByID(req.SessionID)
	if s == nil {
		return cancelled, nil
	}
	title, rawInput := describeToolCall(s, req.ToolCall)
	if _, ours := workspaceTool(title, e.ws.ToolNames()); ours {
		return requestPermissionResponse{Outcome: allowOwnTool(req.Options)}, nil
	}

	requestID := e.linkID + ":" + req.SessionID + ":" + req.ToolCall.ToolCallID
	answer := make(chan permissionOutcome, 1)
	var once sync.Once
	p := &pendingPermission{sessionID: req.SessionID, options: req.Options,
		settle: func(o permissionOutcome) { once.Do(func() { answer <- o }) }}
	e.mu.Lock()
	earlier := e.permissions[requestID]
	e.permissions[requestID] = p
	e.mu.Unlock()
	if earlier != nil {
		// Asked again for the same call: the new question takes the card over.
		earlier.settle(cancelled.Outcome)
	}
	defer func() {
		e.mu.Lock()
		if e.permissions[requestID] == p {
			delete(e.permissions, requestID)
		}
		e.mu.Unlock()
	}()

	e.ws.HandlePermissionRequest(ctx, e.linkID, mcpctrl.PermissionRequestParams{
		RequestID: requestID, TaskID: b62(e.taskOf(s)),
		ToolName: title, Description: title, InputPreview: inputPreview(rawInput),
	})

	var timeout <-chan time.Time
	if wait := e.permissionWait(); wait > 0 {
		t := time.NewTimer(wait)
		defer t.Stop()
		timeout = t.C
	}
	select {
	case o := <-answer:
		return requestPermissionResponse{Outcome: o}, nil
	case <-timeout:
		// The turn is cancelled as well as the call: "cancelled" is the answer
		// a client gives because it cancelled, and answering alone would leave
		// the agent free to carry on and ask again.
		_ = e.peer.notify(methodSessionCancel, sessionIDParams{SessionID: req.SessionID})
		e.ws.ClosePermissionRequest(context.WithoutCancel(ctx), requestID)
		return cancelled, nil
	case <-ctx.Done():
		// The agent withdrew the request, or the connection ended for good.
		e.ws.ClosePermissionRequest(context.WithoutCancel(ctx), requestID)
		return cancelled, nil
	}
}

// permissionWait is how long a tool call waits for the human: the bridge's
// X-AgentRQ-Permission-Timeout when it sent one (0 is for ever), else the
// default.
func (e *engineV1) permissionWait() time.Duration {
	if d := e.c.Meta().PermissionTimeout; d >= 0 {
		return d
	}
	return e.lim.permission
}

// describeToolCall names a call the way the card shows it: the request's own
// title, else what an earlier tool_call update said, else its name, else
// "Unknown Tool" — some agents send the request with nothing but the id.
func describeToolCall(s *session, tc toolCallUpdate) (string, json.RawMessage) {
	known := s.proj.toolCall(tc.ToolCallID)
	raw := tc.RawInput
	if len(raw) == 0 || string(raw) == "null" {
		raw = known.rawInput
	}
	for _, title := range []string{deref(tc.Title), known.title, deref(tc.Name), known.name} {
		if title != "" {
			return title, raw
		}
	}
	return "Unknown Tool", raw
}

func inputPreview(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return "{}"
	}
	return string(raw)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// looksLikeAllow is an option that grants the call, by its kind or its name.
func looksLikeAllow(o permissionOption) bool {
	name := strings.ToLower(o.Name)
	return strings.HasPrefix(o.Kind, "allow") || strings.Contains(name, "allow") || strings.Contains(name, "yes") || strings.Contains(name, "approve")
}

// allowOwnTool picks the option for one of the workspace's own tools, as the
// gateway does: the first option that allows, else the first.
func allowOwnTool(options []permissionOption) permissionOutcome {
	if len(options) == 0 {
		return permissionOutcome{Outcome: "cancelled"}
	}
	for _, o := range options {
		if looksLikeAllow(o) {
			return permissionOutcome{Outcome: "selected", OptionID: o.OptionID}
		}
	}
	return permissionOutcome{Outcome: "selected", OptionID: options[0].OptionID}
}

// verdict answers a permission request the link raised; false when there is
// no such request waiting.
func (e *engineV1) verdict(requestID, behavior string) bool {
	e.mu.Lock()
	p := e.permissions[requestID]
	delete(e.permissions, requestID)
	e.mu.Unlock()
	if p == nil {
		return false
	}
	p.settle(outcomeFor(p.options, behavior))
	return true
}

var persistentOption = regexp.MustCompile(`(?i)always|remember|don'?t ask`)

// outcomeFor turns the human's verdict on one tool call into one of the
// options the agent offered, the gateway's way. A persistent option is never
// picked: the agent would remember it and stop asking, so later calls would
// never reach the human. An unmatched deny never falls back to an allow
// option; with nothing safe to pick, the request is cancelled.
func outcomeFor(options []permissionOption, behavior string) permissionOutcome {
	var once []permissionOption
	for _, o := range options {
		if !persistentOption.MatchString(o.Kind) && !persistentOption.MatchString(o.Name) {
			once = append(once, o)
		}
	}
	allow := behavior == "allow"
	for _, o := range once {
		name := strings.ToLower(o.Name)
		if allow && looksLikeAllow(o) ||
			!allow && (strings.HasPrefix(o.Kind, "reject") || strings.Contains(name, "deny") || strings.Contains(name, "reject") || strings.Contains(name, "no")) {
			return permissionOutcome{Outcome: "selected", OptionID: o.OptionID}
		}
	}
	for _, o := range once {
		if allow || !strings.HasPrefix(o.Kind, "allow") {
			return permissionOutcome{Outcome: "selected", OptionID: o.OptionID}
		}
	}
	return permissionOutcome{Outcome: "cancelled"}
}

// settlePermissions answers every waiting request of a session "cancelled",
// or of every session when acpSessionID is "", and returns their ids.
func (e *engineV1) settlePermissions(acpSessionID string) []string {
	e.mu.Lock()
	var ids []string
	var settle []*pendingPermission
	for id, p := range e.permissions {
		if acpSessionID == "" || p.sessionID == acpSessionID {
			ids = append(ids, id)
			settle = append(settle, p)
			delete(e.permissions, id)
		}
	}
	e.mu.Unlock()
	for _, p := range settle {
		p.settle(permissionOutcome{Outcome: "cancelled"})
	}
	return ids
}
