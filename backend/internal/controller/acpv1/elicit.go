// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package acpv1

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/agentrq/agentrq/backend/internal/controller/agentlink"
)

// createElicitation puts the agent's question to the human. A question tied to
// a session with a task goes to that task; one asked before any task existed
// — a login's device code — gets a task of its own, as the gateway does, so it
// shows up as an active task rather than nowhere.
func (e *engineV1) createElicitation(ctx context.Context, params json.RawMessage) (any, *rpcError) {
	var req createElicitationRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, &rpcError{Code: codeInvalidParams, Message: "the question could not be read"}
	}
	// AgentRQ advertised form and url; the spec answers any other mode
	// -32602.
	if req.Mode != "form" && req.Mode != "url" {
		return nil, &rpcError{Code: codeInvalidParams, Message: "elicitation mode " + strconv.Quote(req.Mode) + " was not advertised"}
	}
	cancel := createElicitationResponse{Action: "cancel"}
	var taskID int64
	if s := e.sessionByID(req.SessionID); s != nil {
		taskID = e.taskOf(s)
	}
	if taskID == 0 {
		id, err := e.ws.CreateHumanTask(ctx, req.Message, req.Message)
		if err != nil {
			return cancel, nil
		}
		taskID = id
	}
	if req.Mode == "url" && req.ElicitationID != "" {
		var stop context.CancelFunc
		ctx, stop = context.WithCancel(ctx)
		e.mu.Lock()
		e.urlQuestions[req.ElicitationID] = stop
		e.mu.Unlock()
		defer func() {
			e.mu.Lock()
			delete(e.urlQuestions, req.ElicitationID)
			e.mu.Unlock()
			stop()
		}()
	}
	ans, err := e.ws.AskHuman(ctx, taskID, agentlink.Question{Mode: req.Mode, Message: req.Message,
		RequestedSchema: req.RequestedSchema, URL: req.URL})
	if err != nil {
		return cancel, nil
	}
	out := createElicitationResponse{Action: ans.Action}
	if ans.Action == "accept" {
		out.Content = ans.Content
	}
	return out, nil
}

// completeElicitation closes a url question the agent no longer waits on — a
// login it finished by itself while the card was still open.
func (e *engineV1) completeElicitation(params json.RawMessage) {
	var n completeElicitationNotification
	if json.Unmarshal(params, &n) != nil {
		return
	}
	e.mu.Lock()
	stop := e.urlQuestions[n.ElicitationID]
	e.mu.Unlock()
	if stop != nil {
		stop()
	}
}
