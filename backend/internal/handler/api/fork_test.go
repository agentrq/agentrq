// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
	"github.com/gofiber/fiber/v2"
	"github.com/mustafaturan/monoflake"
)

type mockCrudForkTask struct {
	crud.Controller
	ongoing []entity.Task
	forkErr error
	got     entity.ForkTaskRequest
}

func (m *mockCrudForkTask) ListTasks(ctx context.Context, req entity.ListTasksRequest) (*entity.ListTasksResponse, error) {
	return &entity.ListTasksResponse{Tasks: m.ongoing}, nil
}

func (m *mockCrudForkTask) ForkTask(ctx context.Context, req entity.ForkTaskRequest) (*entity.ForkTaskResponse, error) {
	m.got = req
	if m.forkErr != nil {
		return nil, m.forkErr
	}
	return &entity.ForkTaskResponse{Task: entity.Task{
		ID: 77, WorkspaceID: req.WorkspaceID, Status: req.Status, Title: "Fork: Build it", Body: "Forked from task x.",
	}}, nil
}

func postFork(t *testing.T, ctrl *mockCrudForkTask, srv *fakeWorkspaceServer, body string) *http.Response {
	t.Helper()
	app := fiber.New()
	h := &handler{crud: ctrl, mcpManager: &fakeMCPManager{server: srv}, bus: eventbus.New()}
	app.Post(_routePathFork, func(c *fiber.Ctx) error {
		c.Locals("user_id", monoflake.ID(100).String())
		return h.forkTask()(c)
	})
	url := "/workspaces/" + monoflake.ID(1).String() + "/tasks/" + monoflake.ID(9).String() + "/fork"
	req := httptest.NewRequest(http.MethodPost, url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// An idle agent gets the fork as ongoing, and straight away: the poller never
// offers an ongoing task, so a fork that is not pushed here is never sent.
func TestForkTask_OngoingAndPushedWhenTheAgentHasRoom(t *testing.T) {
	ctrl := &mockCrudForkTask{}
	srv := &fakeWorkspaceServer{}
	resp := postFork(t, ctrl, srv, `{"messageId":"`+monoflake.ID(5).String()+`"}`)

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("the request answered status %d, want 201", resp.StatusCode)
	}
	if ctrl.got.Status != "ongoing" || ctrl.got.WorkspaceID != 1 || ctrl.got.TaskID != 9 || ctrl.got.MessageID != 5 {
		t.Errorf("the controller was asked for %+v, want an ongoing fork of task 9 in workspace 1 from message 5", ctrl.got)
	}
	if srv.notifiedTaskID != 77 || !strings.Contains(srv.notifiedContent, "Forked from task") {
		t.Errorf("the agent was pushed task %d with %q, want task 77 with its \"Forked from task\" body", srv.notifiedTaskID, srv.notifiedContent)
	}
	var forked struct {
		Task struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"task"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&forked)
	if forked.Task.ID != monoflake.ID(77).String() || forked.Task.Status != "ongoing" {
		t.Errorf("the response task is %+v, want task 77, ongoing", forked.Task)
	}
}

// Forking the task the agent is working on is the usual case, and there the
// fork waits as notstarted for the poller rather than being pushed mid-turn.
func TestForkTask_WaitsAsNotStartedWhileTheAgentIsBusy(t *testing.T) {
	ctrl := &mockCrudForkTask{ongoing: []entity.Task{{ID: 9, Status: "ongoing"}}}
	srv := &fakeWorkspaceServer{}
	resp := postFork(t, ctrl, srv, `{"messageId":"`+monoflake.ID(5).String()+`"}`)

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("the request answered status %d, want 201", resp.StatusCode)
	}
	if ctrl.got.Status != "notstarted" {
		t.Errorf("the fork was requested as %q, want notstarted", ctrl.got.Status)
	}
	if len(srv.calls) != 0 {
		t.Errorf("the agent was sent %v, want no push while it is busy", srv.calls)
	}
}

func TestForkTask_RejectsAMissingMessageID(t *testing.T) {
	for _, body := range []string{`{}`, `not json`} {
		resp := postFork(t, &mockCrudForkTask{}, &fakeWorkspaceServer{}, body)
		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("the body %s answered status %d, want 422", body, resp.StatusCode)
		}
	}
}

func TestForkTask_AMissingTaskIsNotFoundAndPushesNothing(t *testing.T) {
	srv := &fakeWorkspaceServer{}
	resp := postFork(t, &mockCrudForkTask{forkErr: base.ErrNotFound}, srv, `{"messageId":"`+monoflake.ID(5).String()+`"}`)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("the request answered status %d, want 404", resp.StatusCode)
	}
	if len(srv.calls) != 0 {
		t.Errorf("the agent was sent %v, want nothing pushed for a failed fork", srv.calls)
	}
}

func TestForkTask_AFailedForkIsAServerError(t *testing.T) {
	errDB := errors.New("database unavailable")
	resp := postFork(t, &mockCrudForkTask{forkErr: errDB}, &fakeWorkspaceServer{}, `{"messageId":"`+monoflake.ID(5).String()+`"}`)
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("the request answered status %d, want 500", resp.StatusCode)
	}
}

type mockCrudPushEvent struct {
	crud.Controller
	err error
}

func (m *mockCrudPushEvent) GetEvent(ctx context.Context, req entity.GetEventRequest) (*entity.GetEventResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &entity.GetEventResponse{Event: entity.Event{Name: "deployed"}}, nil
}

// A task bound to an event carries the publish instruction in its push, and
// one whose event cannot be read is still pushed, without it.
func TestPushTaskToAgent_EventInstruction(t *testing.T) {
	task := entity.Task{ID: 7, WorkspaceID: 1, Title: "t", EventID: 3, Attachments: []entity.Attachment{{ID: "a1", Filename: "log.txt"}}}

	srv := &fakeWorkspaceServer{}
	h := &handler{crud: &mockCrudPushEvent{}, mcpManager: &fakeMCPManager{server: srv}}
	h.pushTaskToAgent(context.Background(), monoflake.ID(100).String(), task)
	if !strings.Contains(srv.notifiedContent, "deployed") || !strings.Contains(srv.notifiedContent, "log.txt") {
		t.Errorf("the agent was pushed %q, want the attachment and the event instruction", srv.notifiedContent)
	}

	srv = &fakeWorkspaceServer{}
	h = &handler{crud: &mockCrudPushEvent{err: errors.New("event store unavailable")}, mcpManager: &fakeMCPManager{server: srv}}
	h.pushTaskToAgent(context.Background(), monoflake.ID(100).String(), task)
	if srv.notifiedTaskID != 7 || strings.Contains(srv.notifiedContent, "deployed") {
		t.Errorf("the agent was pushed task %d with %q, want task 7 without the event instruction", srv.notifiedTaskID, srv.notifiedContent)
	}
}

// The push carries the workspace's notes after the body: they go with the task
// when the agent is handed it, never in what is stored.
func TestPushTaskToAgent_CarriesTheWorkspaceNotes(t *testing.T) {
	task := entity.Task{ID: 7, WorkspaceID: 1, Body: "Do it."}

	srv := &fakeWorkspaceServer{notes: "Keep me posted."}
	h := &handler{crud: &mockCrudPushEvent{}, mcpManager: &fakeMCPManager{server: srv}}
	h.pushTaskToAgent(context.Background(), monoflake.ID(100).String(), task)

	if want := "[Task " + monoflake.ID(7).String() + "]\nDo it.\n\nKeep me posted."; srv.notifiedContent != want {
		t.Errorf("the agent was pushed %q, want %q", srv.notifiedContent, want)
	}
}
