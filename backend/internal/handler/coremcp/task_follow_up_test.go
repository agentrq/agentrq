// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package coremcp

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
)

// followUpCrud answers FollowUpTask with the task in status, or err, and
// keeps the request.
type followUpCrud struct {
	mockServerCrud
	req    entity.FollowUpTaskRequest
	status string
	err    error
}

func (m *followUpCrud) FollowUpTask(_ context.Context, req entity.FollowUpTaskRequest) (*entity.FollowUpTaskResponse, error) {
	m.req = req
	if m.err != nil {
		return nil, m.err
	}
	return &entity.FollowUpTaskResponse{
		Task:      entity.Task{ID: req.TaskID, WorkspaceID: req.WorkspaceID, Title: "Release", Status: m.status, Assignee: "agent", Body: "Tag main."},
		MessageID: 101,
	}, nil
}

// pushed is one message a notifier sent.
type pushed struct {
	workspaceID, taskID int64
	userID, content     string
}

type fakeNotifier struct{ sent []pushed }

func (n *fakeNotifier) SendChannelNotification(_ context.Context, workspaceID int64, userID string, taskID int64, content string, _ ...entity.Attachment) {
	n.sent = append(n.sent, pushed{workspaceID, taskID, userID, content})
}

func callFollowUp(srv *WorkspaceServer, verbose bool) callResult {
	payload := map[string]any{"workspaceId": base62(testWorkspace), "taskId": base62(42), "text": "Also cover the error path."}
	if verbose {
		payload["verbose"] = true
	}
	return toolResult(srv.handleTask(authedContext(), nil, TaskParams{Action: "followUp", Payload: payload}))
}

// A finished task is reopened by the controller and handed over by the
// poller: nothing is pushed now, and the answer is as short as reply's.
func TestTaskFollowUp_ReopensAFinishedTask(t *testing.T) {
	fake := &followUpCrud{status: "notstarted"}
	n := &fakeNotifier{}
	text := textOf(t, callFollowUp(&WorkspaceServer{crud: fake, notifier: n}, false))

	if fake.req != (entity.FollowUpTaskRequest{WorkspaceID: testWorkspace, TaskID: 42, Text: "Also cover the error path.", UserID: testUserID}) {
		t.Errorf("request = %+v", fake.req)
	}
	if len(n.sent) != 0 {
		t.Errorf("pushed %+v; a reopened task waits for the poller", n.sent)
	}
	if !strings.Contains(text, `"status":"notstarted"`) || !strings.Contains(text, `"messageId":"`+base62(101)+`"`) || strings.Contains(text, "Tag main.") {
		t.Errorf("answer = %s, want the task's summary and the message posted", text)
	}
}

// An agent still on the task is sent the follow-up now.
func TestTaskFollowUp_SendsItNowToAnAgentOnTheTask(t *testing.T) {
	n := &fakeNotifier{}
	textOf(t, callFollowUp(&WorkspaceServer{crud: &followUpCrud{status: "ongoing"}, notifier: n}, false))

	want := pushed{testWorkspace, 42, testUserID, "[Follow-up from supervisor on task " + base62(42) + "] Also cover the error path."}
	if len(n.sent) != 1 || n.sent[0] != want {
		t.Errorf("pushed %+v, want %+v", n.sent, want)
	}
	// With no notifier the follow-up is still posted.
	textOf(t, callFollowUp(&WorkspaceServer{crud: &followUpCrud{status: "ongoing"}}, false))
}

func TestTaskFollowUp_Verbose(t *testing.T) {
	text := textOf(t, callFollowUp(&WorkspaceServer{crud: &followUpCrud{status: "notstarted"}}, true))
	if !strings.Contains(text, "Tag main.") {
		t.Errorf("verbose answer = %s, want the whole task", text)
	}
}

func TestTaskFollowUp_Refused(t *testing.T) {
	n := &fakeNotifier{}
	res := callFollowUp(&WorkspaceServer{crud: &followUpCrud{err: errors.New("the task is assigned to the human")}, notifier: n}, false)
	if !res.isError || !strings.Contains(res.text, "assigned to the human") || len(n.sent) != 0 {
		t.Errorf("result %+v, pushed %+v; want the refusal and nothing sent", res, n.sent)
	}
}

// New hands the server the MCP manager app.go passes as its notifier.
func TestNew_WiresTheNotifier(t *testing.T) {
	n := &fakeNotifier{}
	h, err := New(Params{Mux: http.NewServeMux(), Notifier: n})
	if err != nil {
		t.Fatal(err)
	}
	if got := h.(*handler).coremcpServer.notifier; got != n {
		t.Errorf("notifier = %v", got)
	}
}
