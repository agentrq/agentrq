// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	mcpctrl "github.com/agentrq/agentrq/backend/internal/controller/mcp"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
	"github.com/gofiber/fiber/v2"
	"github.com/mustafaturan/monoflake"
)

// The error paths of the task routes: a request the mapper refuses, a
// controller that fails, and — where nothing else reaches it — the success
// that follows. Each route is driven through registerTaskRoutes, so a route
// wired to the wrong handler fails here too.

var (
	errPathUser      = monoflake.ID(100).String()
	errPathWorkspace = monoflake.ID(1).String()
	errPathTask      = monoflake.ID(2).String()
	errPathOther     = monoflake.ID(3).String()
	errPathDBDown    = errors.New("database is down")
)

// errPathCrud is a crud.Controller that answers every call the task routes
// make: with err when it is set, otherwise with task. Anything else panics
// through the nil embedded interface, which is the point.
type errPathCrud struct {
	crud.Controller
	err  error
	task entity.Task

	// access is CheckWorkspaceAccess's answer, accessErr its error.
	access    bool
	accessErr error

	// closed is CloseExpiredElicitation's answer; closeErr its error.
	closed   *entity.CloseExpiredElicitationResponse
	closeErr error

	attachment *entity.GetAttachmentResponse
}

func (m *errPathCrud) CreateTask(context.Context, entity.CreateTaskRequest) (*entity.CreateTaskResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &entity.CreateTaskResponse{Task: m.task}, nil
}

func (m *errPathCrud) ListTasks(context.Context, entity.ListTasksRequest) (*entity.ListTasksResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &entity.ListTasksResponse{Tasks: []entity.Task{m.task}}, nil
}

func (m *errPathCrud) GetTask(context.Context, entity.GetTaskRequest) (*entity.GetTaskResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &entity.GetTaskResponse{Task: m.task}, nil
}

func (m *errPathCrud) RespondToTask(context.Context, entity.RespondToTaskRequest) (*entity.RespondToTaskResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &entity.RespondToTaskResponse{Task: m.task}, nil
}

func (m *errPathCrud) ReplyToTask(context.Context, entity.ReplyToTaskRequest) (*entity.ReplyToTaskResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &entity.ReplyToTaskResponse{Task: m.task}, nil
}

func (m *errPathCrud) UpdateTaskStatus(context.Context, entity.UpdateTaskStatusRequest) (*entity.UpdateTaskStatusResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &entity.UpdateTaskStatusResponse{Task: m.task}, nil
}

func (m *errPathCrud) UpdateTaskOrder(context.Context, entity.UpdateTaskOrderRequest) (*entity.UpdateTaskOrderResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &entity.UpdateTaskOrderResponse{Task: m.task}, nil
}

func (m *errPathCrud) UpdateTaskAssignee(context.Context, entity.UpdateTaskAssigneeRequest) (*entity.UpdateTaskAssigneeResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &entity.UpdateTaskAssigneeResponse{Task: m.task}, nil
}

func (m *errPathCrud) MoveTask(context.Context, entity.MoveTaskRequest) (*entity.MoveTaskResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &entity.MoveTaskResponse{Task: m.task}, nil
}

func (m *errPathCrud) UpdateTaskAllowAllCommands(context.Context, entity.UpdateTaskAllowAllCommandsRequest) (*entity.UpdateTaskAllowAllCommandsResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &entity.UpdateTaskAllowAllCommandsResponse{Task: m.task}, nil
}

func (m *errPathCrud) UpdateScheduledTask(context.Context, entity.UpdateScheduledTaskRequest) (*entity.UpdateScheduledTaskResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &entity.UpdateScheduledTaskResponse{Task: m.task}, nil
}

func (m *errPathCrud) DeleteTask(context.Context, entity.DeleteTaskRequest) (*entity.DeleteTaskResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &entity.DeleteTaskResponse{}, nil
}

func (m *errPathCrud) GetAttachment(context.Context, entity.GetAttachmentRequest) (*entity.GetAttachmentResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.attachment, nil
}

func (m *errPathCrud) GetGlobalTaskStats(context.Context, string) (*entity.GlobalTaskStatsResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &entity.GlobalTaskStatsResponse{PendingTasks: 4, ScheduledTasks: 2}, nil
}

func (m *errPathCrud) GetWorkspaceTaskCounts(context.Context, entity.GetWorkspaceTaskCountsRequest) (map[string]int64, error) {
	if m.err != nil {
		return nil, m.err
	}
	return map[string]int64{"ongoing": 1}, nil
}

func (m *errPathCrud) CheckWorkspaceAccess(context.Context, int64, string) (bool, error) {
	return m.access, m.accessErr
}

func (m *errPathCrud) CloseExpiredElicitation(context.Context, entity.CloseExpiredElicitationRequest) (*entity.CloseExpiredElicitationResponse, error) {
	return m.closed, m.closeErr
}

// errPathTaskApp mounts the task routes behind a stand-in for the auth
// middleware, which only ever sets the caller.
func errPathTaskApp(h *handler) *fiber.App {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user_id", errPathUser)
		return c.Next()
	})
	h.router = app
	if err := h.registerTaskRoutes(); err != nil {
		panic(err)
	}
	return app
}

func errPathDo(t *testing.T, app *fiber.App, method, path, body string) (*http.Response, string) {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rd)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

// errPathEventType drains one event from a bus subscription, or "" if none
// was published.
func errPathEventType(t *testing.T, ch chan []byte) string {
	t.Helper()
	select {
	case line := <-ch:
		var evt struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal([]byte(strings.TrimSuffix(strings.TrimPrefix(string(line), "data: "), "\n\n")), &evt); err != nil {
			t.Fatalf("unreadable event %q: %v", line, err)
		}
		return evt.Type
	default:
		return ""
	}
}

func TestTaskRoutes_RefuseWhatTheMapperCannotRead(t *testing.T) {
	// Nothing behind the controller: a refused request must never reach it.
	app := errPathTaskApp(&handler{crud: &errPathCrud{}})

	task := "/workspaces/" + errPathWorkspace + "/tasks/" + errPathTask
	bad := "/workspaces/!/tasks/" + errPathTask
	cases := []struct {
		name, method, path, body string
	}{
		{"create without a creator", http.MethodPost, "/workspaces/" + errPathWorkspace + "/tasks", `{"task":{"title":"t"}}`},
		{"get with a bad workspace", http.MethodGet, bad, ""},
		{"respond without an action", http.MethodPost, task + "/respond", `{"response":{}}`},
		{"reply that is not JSON", http.MethodPost, task + "/reply", `{`},
		{"status without a value", http.MethodPatch, task + "/status", `{"status":{}}`},
		{"order that is not JSON", http.MethodPatch, task + "/order", `{`},
		{"order with a bad workspace", http.MethodPatch, bad + "/order", `{"order":{"value":1}}`},
		{"assignee without a value", http.MethodPatch, task + "/assignee", `{"assignee":{}}`},
		{"move that is not JSON", http.MethodPatch, task + "/workspace", `{`},
		{"move to no workspace", http.MethodPatch, task + "/workspace", `{"workspace":{"value":""}}`},
		{"allow-all that is not JSON", http.MethodPatch, task + "/allow_all", `{`},
		{"allow-all with a bad workspace", http.MethodPatch, bad + "/allow_all", `{"allowAll":{"value":true}}`},
		{"schedule without a title", http.MethodPut, task + "/scheduled", `{"task":{}}`},
		{"delete with a bad workspace", http.MethodDelete, bad, ""},
		{"events for a bad workspace", http.MethodGet, "/workspaces/!/events", ""},
		{"attachment with a bad workspace", http.MethodGet, bad + "/attachments/att-1", ""},
		{"counts for a bad workspace", http.MethodGet, "/workspaces/!/tasks/counts", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, body := errPathDo(t, app, tc.method, tc.path, tc.body)
			if resp.StatusCode != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422 (body %s)", resp.StatusCode, body)
			}
			if body != string(_invalidPayload) {
				t.Errorf("body = %s, want the invalid-payload error", body)
			}
		})
	}
}

func TestTaskRoutes_AnswerAControllerFailureWithItsStatus(t *testing.T) {
	task := "/workspaces/" + errPathWorkspace + "/tasks/" + errPathTask
	cases := []struct {
		name, method, path, body string
	}{
		{"create", http.MethodPost, "/workspaces/" + errPathWorkspace + "/tasks", `{"task":{"title":"t","createdBy":"human","assignee":"agent"}}`},
		{"list", http.MethodGet, "/tasks", ""},
		{"get", http.MethodGet, task, ""},
		{"respond", http.MethodPost, task + "/respond", `{"response":{"action":"allow"}}`},
		{"reply", http.MethodPost, task + "/reply", `{"reply":{"text":"hi"}}`},
		{"status", http.MethodPatch, task + "/status", `{"status":{"value":"completed"}}`},
		{"order", http.MethodPatch, task + "/order", `{"order":{"value":1.5}}`},
		{"assignee", http.MethodPatch, task + "/assignee", `{"assignee":{"value":"agent"}}`},
		{"move", http.MethodPatch, task + "/workspace", `{"workspace":{"value":"` + errPathOther + `"}}`},
		{"allow-all", http.MethodPatch, task + "/allow_all", `{"allowAll":{"value":true}}`},
		{"schedule", http.MethodPut, task + "/scheduled", `{"task":{"title":"t","cronSchedule":"0 * * * *"}}`},
		{"delete", http.MethodDelete, task, ""},
		{"attachment", http.MethodGet, task + "/attachments/att-1", ""},
		{"global stats", http.MethodGet, "/tasks/stats", ""},
		{"counts", http.MethodGet, "/workspaces/" + errPathWorkspace + "/tasks/counts", ""},
	}
	errs := []struct {
		err  error
		want int
	}{
		{errPathDBDown, http.StatusInternalServerError},
		{base.ErrNotFound, http.StatusNotFound},
	}
	for _, tc := range cases {
		for _, e := range errs {
			t.Run(tc.name+"/"+e.err.Error(), func(t *testing.T) {
				// A server and a bus are there to be told nothing: a failed
				// write must not reach the agent or the stream.
				srv := &fakeWorkspaceServer{}
				bus := eventbus.New()
				ch := bus.Subscribe(1, errPathUser)
				app := errPathTaskApp(&handler{
					crud:       &errPathCrud{err: e.err},
					mcpManager: &fakeMCPManager{server: srv},
					bus:        bus,
				})

				resp, body := errPathDo(t, app, tc.method, tc.path, tc.body)
				if resp.StatusCode != e.want {
					t.Fatalf("status = %d, want %d (body %s)", resp.StatusCode, e.want, body)
				}
				if ct := resp.Header.Get("Content-Type"); ct != _mimeJSON {
					t.Errorf("Content-Type = %q, want %q", ct, _mimeJSON)
				}
				if e.err == errPathDBDown && strings.Contains(body, e.err.Error()) {
					t.Errorf("body leaks the internal error: %s", body)
				}
				if len(srv.calls) != 0 {
					t.Errorf("the agent was told %v about a write that failed", srv.calls)
				}
				if got := errPathEventType(t, ch); got != "" {
					t.Errorf("published %q for a write that failed", got)
				}
			})
		}
	}
}

func TestTaskRoutes_PublishWhatTheySucceededAt(t *testing.T) {
	task := "/workspaces/" + errPathWorkspace + "/tasks/" + errPathTask
	cases := []struct {
		name, method, path, body string
		want                     int
		event                    string
	}{
		{"get", http.MethodGet, task, "", http.StatusOK, ""},
		{"status", http.MethodPatch, task + "/status", `{"status":{"value":"completed"}}`, http.StatusOK, "status.updated"},
		{"order", http.MethodPatch, task + "/order", `{"order":{"value":1.5}}`, http.StatusOK, "task.updated"},
		{"move", http.MethodPatch, task + "/workspace", `{"workspace":{"value":"` + errPathOther + `"}}`, http.StatusOK, "task.deleted"},
		{"allow-all", http.MethodPatch, task + "/allow_all", `{"allowAll":{"value":true}}`, http.StatusOK, "task.updated"},
		{"schedule", http.MethodPut, task + "/scheduled", `{"task":{"title":"t","cronSchedule":"0 * * * *"}}`, http.StatusOK, "task.updated"},
		{"delete", http.MethodDelete, task, "", http.StatusNoContent, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bus := eventbus.New()
			ch := bus.Subscribe(1, errPathUser)
			app := errPathTaskApp(&handler{
				crud: &errPathCrud{task: entity.Task{ID: 2, WorkspaceID: 1, Title: "t", Status: "completed"}},
				bus:  bus,
			})

			resp, body := errPathDo(t, app, tc.method, tc.path, tc.body)
			if resp.StatusCode != tc.want {
				t.Fatalf("status = %d, want %d (body %s)", resp.StatusCode, tc.want, body)
			}
			if tc.want == http.StatusOK {
				var rs struct {
					Task struct {
						ID string `json:"id"`
					} `json:"task"`
				}
				if err := json.Unmarshal([]byte(body), &rs); err != nil || rs.Task.ID != errPathTask {
					t.Errorf("body = %s, want task %s", body, errPathTask)
				}
			}
			if got := errPathEventType(t, ch); got != tc.event {
				t.Errorf("published %q, want %q", got, tc.event)
			}
		})
	}
}

// A moved task leaves one workspace's stream and arrives in the other's.
func TestMoveTask_AnnouncesTheTaskInItsNewWorkspace(t *testing.T) {
	bus := eventbus.New()
	dest := bus.Subscribe(3, errPathUser)
	app := errPathTaskApp(&handler{
		crud: &errPathCrud{task: entity.Task{ID: 2, WorkspaceID: 3}},
		bus:  bus,
	})

	resp, body := errPathDo(t, app, http.MethodPatch,
		"/workspaces/"+errPathWorkspace+"/tasks/"+errPathTask+"/workspace",
		`{"workspace":{"value":"`+errPathOther+`"}}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", resp.StatusCode, body)
	}
	if got := errPathEventType(t, dest); got != "task.created" {
		t.Errorf("destination heard %q, want task.created", got)
	}
}

// The human's answer reaches the agent with the files it carries listed.
func TestRespondToTask_ListsTheAttachmentsForTheAgent(t *testing.T) {
	srv := &fakeWorkspaceServer{}
	app := errPathTaskApp(&handler{
		crud:       &errPathCrud{task: entity.Task{ID: 2, WorkspaceID: 1}},
		mcpManager: &fakeMCPManager{server: srv},
		bus:        eventbus.New(),
	})

	resp, body := errPathDo(t, app, http.MethodPost,
		"/workspaces/"+errPathWorkspace+"/tasks/"+errPathTask+"/respond",
		`{"response":{"action":"allow","text":"go ahead","attachments":[{"id":"att-1","filename":"log.txt","mimeType":"text/plain"}]}}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", resp.StatusCode, body)
	}
	want := "[Response to task " + errPathTask + "] action=allow: go ahead\nAttachments:\n  - id=att-1 name=log.txt type=text/plain"
	if srv.notifiedContent != want {
		t.Errorf("agent was told %q, want %q", srv.notifiedContent, want)
	}
	if len(srv.notifiedAttachments) != 1 || srv.notifiedAttachments[0].ID != "att-1" {
		t.Errorf("attachments = %+v, want att-1", srv.notifiedAttachments)
	}
}

func TestGetAttachment_ServesTheFileUnderASafeName(t *testing.T) {
	app := errPathTaskApp(&handler{crud: &errPathCrud{attachment: &entity.GetAttachmentResponse{
		Data:     []byte("hello"),
		Filename: "notes.txt",
		MimeType: "text/plain",
	}}})

	resp, body := errPathDo(t, app, http.MethodGet,
		"/workspaces/"+errPathWorkspace+"/tasks/"+errPathTask+"/attachments/att-1", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if body != "hello" {
		t.Errorf("body = %q, want the file", body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/plain" {
		t.Errorf("Content-Type = %q, want text/plain", ct)
	}
	if cd := resp.Header.Get("Content-Disposition"); cd != `inline; filename="notes.txt"` {
		t.Errorf("Content-Disposition = %q", cd)
	}
}

func TestTaskStats_AnswerWithTheCounts(t *testing.T) {
	app := errPathTaskApp(&handler{crud: &errPathCrud{}})

	resp, body := errPathDo(t, app, http.MethodGet, "/tasks/stats", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if body != `{"pendingTasks":4,"scheduledTasks":2}` {
		t.Errorf("body = %s", body)
	}
}

// verdictServer is a connected agent whose permission verdicts fail.
type verdictServer struct {
	*fakeWorkspaceServer
	err error
}

func (v verdictServer) SendPermissionVerdictFrom(ctx context.Context, taskID int64, requestID, behavior, decidedBy string) error {
	return v.err
}

func TestSendPermissionVerdict_ErrorPaths(t *testing.T) {
	path := "/workspaces/" + errPathWorkspace + "/tasks/" + errPathTask + "/permission"
	cases := []struct {
		name   string
		body   string
		server workspaceServer
		want   int
	}{
		{"a body that is not JSON", `{`, &fakeWorkspaceServer{}, http.StatusBadRequest},
		{"no server for the workspace", `{"requestId":"r1","behavior":"allow"}`, nil, http.StatusNotFound},
		{"a request the server forgot", `{"requestId":"r1","behavior":"allow"}`,
			verdictServer{&fakeWorkspaceServer{}, errors.New("permission request r1 not found (expired)")}, http.StatusGone},
		{"a decider that is not an extension", `{"requestId":"r1","behavior":"allow","decidedBy":"x"}`,
			verdictServer{&fakeWorkspaceServer{}, mcpctrl.ErrBadDecider}, http.StatusBadRequest},
		{"an extension asking to remember", `{"requestId":"r1","behavior":"allow"}`,
			verdictServer{&fakeWorkspaceServer{}, mcpctrl.ErrExtensionCannotRemember}, http.StatusBadRequest},
		{"any other failure", `{"requestId":"r1","behavior":"allow"}`,
			verdictServer{&fakeWorkspaceServer{}, errPathDBDown}, http.StatusInternalServerError},
		{"a verdict delivered", `{"requestId":"r1","behavior":"allow"}`, &fakeWorkspaceServer{}, http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := errPathTaskApp(&handler{
				crud:       &errPathCrud{access: true},
				mcpManager: &fakeMCPManager{server: tc.server},
			})
			resp, body := errPathDo(t, app, http.MethodPost, path, tc.body)
			if resp.StatusCode != tc.want {
				t.Fatalf("status = %d, want %d (body %s)", resp.StatusCode, tc.want, body)
			}
		})
	}
}

func TestStopTask_ReportsWhatTheStopDid(t *testing.T) {
	srv := &fakeWorkspaceServer{stopOutcome: mcpctrl.StopOutcome{Stopped: true}}
	app := errPathTaskApp(&handler{
		crud:       &errPathCrud{access: true},
		mcpManager: &fakeMCPManager{server: srv},
	})

	resp, body := errPathDo(t, app, http.MethodPost,
		"/workspaces/"+errPathWorkspace+"/tasks/"+errPathTask+"/stop", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", resp.StatusCode, body)
	}
	if body != `{"approvalsDenied":0,"stopped":true}` {
		t.Errorf("body = %s", body)
	}
}

func TestRespondToElicitation_ServerPaths(t *testing.T) {
	path := "/workspaces/" + errPathWorkspace + "/tasks/" + errPathTask + "/elicitation"
	body := `{"requestId":"r1","action":"accept","content":{"answer":"yes"}}`

	t.Run("no server for the workspace", func(t *testing.T) {
		app := errPathTaskApp(&handler{crud: &errPathCrud{access: true}, mcpManager: &fakeMCPManager{}})
		if resp, b := errPathDo(t, app, http.MethodPost, path, body); resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status = %d, want 404 (body %s)", resp.StatusCode, b)
		}
	})

	t.Run("an answer delivered", func(t *testing.T) {
		app := errPathTaskApp(&handler{
			crud:       &errPathCrud{access: true},
			mcpManager: &fakeMCPManager{server: &fakeWorkspaceServer{}},
		})
		if resp, b := errPathDo(t, app, http.MethodPost, path, body); resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %s)", resp.StatusCode, b)
		}
	})

	t.Run("a question nobody can close", func(t *testing.T) {
		// The agent has gone, and closing the question failed too: still gone,
		// but nothing was closed and nothing is announced.
		bus := eventbus.New()
		ch := bus.Subscribe(1, errPathUser)
		app := errPathTaskApp(&handler{
			crud:       &errPathCrud{access: true, closeErr: errPathDBDown},
			mcpManager: &fakeMCPManager{server: &fakeWorkspaceServer{elicitErr: errors.New("nobody waiting")}},
			bus:        bus,
		})
		resp, b := errPathDo(t, app, http.MethodPost, path, body)
		if resp.StatusCode != http.StatusGone {
			t.Fatalf("status = %d, want 410 (body %s)", resp.StatusCode, b)
		}
		if strings.Contains(b, `"closed"`) {
			t.Errorf("body = %s, claims a close that did not happen", b)
		}
		if got := errPathEventType(t, ch); got != "" {
			t.Errorf("published %q", got)
		}
	})
}

// The stream opens with a comment naming the workspace and then carries what
// the bus publishes for it, framed as server-sent events.
func TestSSEEvents(t *testing.T) {
	path := "/workspaces/" + errPathWorkspace + "/events"

	t.Run("refuses a caller without access", func(t *testing.T) {
		for name, ctrl := range map[string]*errPathCrud{
			"no access":    {access: false},
			"access error": {access: true, accessErr: errPathDBDown},
		} {
			t.Run(name, func(t *testing.T) {
				app := errPathTaskApp(&handler{crud: ctrl, bus: eventbus.New()})
				if resp, b := errPathDo(t, app, http.MethodGet, path, ""); resp.StatusCode != http.StatusForbidden {
					t.Fatalf("status = %d, want 403 (body %s)", resp.StatusCode, b)
				}
			})
		}
	})

	t.Run("streams the workspace's events", func(t *testing.T) {
		h := &handler{crud: &errPathCrud{access: true}, bus: eventbus.New()}
		app := fiber.New()
		// The stream never ends on its own, so the route reads the first two
		// frames itself and answers with them, which also closes the stream.
		app.Get("/workspaces/:id/events", func(c *fiber.Ctx) error {
			c.Locals("user_id", errPathUser)
			if err := h.sseEvents()(c); err != nil {
				return err
			}
			stream := c.Context().Response.BodyStream()
			if stream == nil {
				return errors.New("no stream was opened")
			}
			if d, ok := stream.(interface{ SetReadDeadline(time.Time) error }); ok {
				_ = d.SetReadDeadline(time.Now().Add(5 * time.Second))
			}
			r := bufio.NewReader(stream)
			hello, err := readSSEFrame(r)
			if err != nil {
				return err
			}
			h.bus.Publish(1, errPathUser, eventbus.Event{Type: "task.updated", Payload: map[string]string{"id": errPathTask}})
			evt, err := readSSEFrame(r)
			if err != nil {
				return err
			}
			c.Context().Response.SetBodyString(hello + evt)
			return nil
		})

		resp, body := errPathDo(t, app, http.MethodGet, path, "")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %s)", resp.StatusCode, body)
		}
		if ct := resp.Header.Get("Content-Type"); ct != _mimeEventStream {
			t.Errorf("Content-Type = %q, want %q", ct, _mimeEventStream)
		}
		if cc := resp.Header.Get("Cache-Control"); cc != "no-cache" {
			t.Errorf("Cache-Control = %q, want no-cache", cc)
		}
		if xab := resp.Header.Get("X-Accel-Buffering"); xab != "no" {
			t.Errorf("X-Accel-Buffering = %q, want no", xab)
		}
		want := ": connected to workspace " + errPathWorkspace + " events\n\n" +
			`data: {"type":"task.updated","payload":{"id":"` + errPathTask + `"}}` + "\n\n"
		if body != want {
			t.Errorf("body = %q, want %q", body, want)
		}
	})
}

// readSSEFrame reads one server-sent event: everything up to its blank line.
func readSSEFrame(r *bufio.Reader) (string, error) {
	var b strings.Builder
	for {
		line, err := r.ReadString('\n')
		b.WriteString(line)
		if err != nil {
			return b.String(), err
		}
		if line == "\n" {
			return b.String(), nil
		}
	}
}

// gonePeer takes ok writes and then fails every one after, as fasthttp's pipe
// does once the client has disconnected.
type gonePeer struct {
	ok  int
	got strings.Builder
}

func (p *gonePeer) Write(b []byte) (int, error) {
	if p.ok == 0 {
		return 0, io.ErrClosedPipe
	}
	p.ok--
	return p.got.Write(b)
}

// streamEventsReturns runs streamEvents and fails the test if it is still
// writing after five seconds.
func streamEventsReturns(t *testing.T, p *gonePeer, ch <-chan []byte, keepalive time.Duration) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		streamEvents(bufio.NewWriter(p), errPathWorkspace, ch, keepalive)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the stream kept running")
	}
}

// A stream ends as soon as the client is gone, so the handler releases its
// bus subscription instead of holding it until the process exits.
func TestStreamEvents(t *testing.T) {
	hello := ": connected to workspace " + errPathWorkspace + " events\n\n"

	t.Run("ends when the bus closes the channel", func(t *testing.T) {
		ch := make(chan []byte, 1)
		ch <- []byte("data: x\n\n")
		close(ch)
		p := &gonePeer{ok: 10}
		streamEventsReturns(t, p, ch, time.Hour)
		if got := p.got.String(); got != hello+"data: x\n\n" {
			t.Errorf("wrote %q", got)
		}
	})

	t.Run("ends when the client is gone before the opening line", func(t *testing.T) {
		p := &gonePeer{}
		streamEventsReturns(t, p, make(chan []byte), time.Hour)
		if got := p.got.String(); got != "" {
			t.Errorf("wrote %q", got)
		}
	})

	t.Run("ends when an event cannot be written", func(t *testing.T) {
		ch := make(chan []byte, 1)
		ch <- []byte("data: x\n\n")
		p := &gonePeer{ok: 1}
		streamEventsReturns(t, p, ch, time.Hour)
		if got := p.got.String(); got != hello {
			t.Errorf("wrote %q", got)
		}
	})

	t.Run("an idle stream finds out from its keepalive", func(t *testing.T) {
		p := &gonePeer{ok: 2}
		streamEventsReturns(t, p, make(chan []byte), time.Millisecond)
		if got := p.got.String(); got != hello+": agentrq\n\n" {
			t.Errorf("wrote %q", got)
		}
	})
}
