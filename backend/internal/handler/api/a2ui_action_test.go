// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
	"github.com/mustafaturan/monoflake"
)

// a2uiActionCrud answers SendA2uiAction with err, or with the task and text,
// and keeps the request it was given.
type a2uiActionCrud struct {
	crud.Controller
	err      error
	got      entity.SendA2uiActionRequest
	reopened bool
}

func (m *a2uiActionCrud) SendA2uiAction(_ context.Context, req entity.SendA2uiActionRequest) (*entity.SendA2uiActionResponse, error) {
	m.got = req
	if m.err != nil {
		return nil, m.err
	}
	return &entity.SendA2uiActionResponse{Task: entity.Task{ID: req.TaskID, WorkspaceID: req.WorkspaceID}, MessageID: 600, Text: "Approve", Reopened: m.reopened}, nil
}

var a2uiActionPath = "/workspaces/" + errPathWorkspace + "/tasks/" + errPathTask + "/a2ui/actions"

func a2uiActionBody(context string) string {
	return `{"messageId":"` + monoflake.ID(500).String() + `","surfaceId":"deploy","name":"approve","sourceComponentId":"ok"` + context + `}`
}

// A click reaches the agent as a reply to the task, with the action after it,
// and the page hears of it as of any reply.
func TestSendA2uiAction_DeliversTheClickToTheAgent(t *testing.T) {
	c := &a2uiActionCrud{}
	srv := &fakeWorkspaceServer{}
	bus := eventbus.New()
	app := errPathTaskApp(&handler{crud: c, mcpManager: &fakeMCPManager{server: srv}, bus: bus})
	ch := bus.Subscribe(1, errPathUser)

	resp, body := errPathDo(t, app, http.MethodPost, a2uiActionPath, a2uiActionBody(`,"context":{"env":"prod"},"userMessage":"Ship it"`))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d %s, want 200", resp.StatusCode, body)
	}
	if c.got.UserID != errPathUser || c.got.WorkspaceID != 1 || c.got.TaskID != 2 || c.got.MessageID != 500 || c.got.UserMessage != "Ship it" {
		t.Errorf("controller got %+v", c.got)
	}
	var rs struct {
		Task struct{ ID string } `json:"task"`
	}
	if json.Unmarshal([]byte(body), &rs) != nil || rs.Task.ID != errPathTask {
		t.Errorf("body = %s, want the task", body)
	}
	want := "[Reply to task " + errPathTask + "] Approve\n[A2UI action] " +
		`{"context":{"env":"prod"},"messageId":"` + monoflake.ID(500).String() + `","name":"approve","surfaceId":"deploy"}`
	if srv.notifiedTaskID != 2 || srv.notifiedContent != want {
		t.Errorf("agent told %d %q\nwant %q", srv.notifiedTaskID, srv.notifiedContent, want)
	}
	if got := errPathEventType(t, ch); got != "reply.received" {
		t.Errorf("event = %q, want reply.received", got)
	}
}

// A click that reopened a completed task tells the agent so, last.
func TestSendA2uiAction_TellsTheAgentItReopenedTheTask(t *testing.T) {
	srv := &fakeWorkspaceServer{}
	app := errPathTaskApp(&handler{crud: &a2uiActionCrud{reopened: true}, mcpManager: &fakeMCPManager{server: srv}, bus: eventbus.New()})

	resp, body := errPathDo(t, app, http.MethodPost, a2uiActionPath, a2uiActionBody(""))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d %s, want 200", resp.StatusCode, body)
	}
	if !strings.HasSuffix(srv.notifiedContent, "\n[A2UI action] "+`{"messageId":"`+monoflake.ID(500).String()+`","name":"approve","surfaceId":"deploy"}`+
		"\nThis reply reopened the task (now ongoing); mark it completed again when done.") {
		t.Errorf("agent told %q", srv.notifiedContent)
	}
}

// A label that looks like a command is still a reply: the agent never runs it.
func TestA2uiActionChannelContent_NeverACommand(t *testing.T) {
	got := a2uiActionChannelContent(&entity.SendA2uiActionRequest{TaskID: 2, MessageID: 500, SurfaceID: "s", Name: "n"}, "/compact")
	if !strings.HasPrefix(got, "[Reply to task "+errPathTask+"] /compact\n[A2UI action] ") || strings.Contains(got, "context") {
		t.Errorf("content = %q", got)
	}
}

func TestSendA2uiAction_Refusals(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		err  error
		want int
	}{
		{"a request the mapper cannot read", `{"surfaceId":"deploy"}`, nil, http.StatusUnprocessableEntity},
		{"a foreign task or message", a2uiActionBody(""), base.ErrNotFound, http.StatusNotFound},
		{"an answered card", a2uiActionBody(""), &entity.A2uiActionError{Message: "this card is already answered", Conflict: true}, http.StatusConflict},
		{"an action not on the card", a2uiActionBody(""), &entity.A2uiActionError{Message: "no such Button"}, http.StatusUnprocessableEntity},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := &fakeWorkspaceServer{}
			app := errPathTaskApp(&handler{crud: &a2uiActionCrud{err: tc.err}, mcpManager: &fakeMCPManager{server: srv}, bus: eventbus.New()})
			resp, body := errPathDo(t, app, http.MethodPost, a2uiActionPath, tc.body)
			if resp.StatusCode != tc.want {
				t.Errorf("status = %d %s, want %d", resp.StatusCode, body, tc.want)
			}
			if len(srv.calls) != 0 || srv.notifiedContent != "" {
				t.Errorf("the agent was told %q of a refused click", srv.notifiedContent)
			}
		})
	}
}
