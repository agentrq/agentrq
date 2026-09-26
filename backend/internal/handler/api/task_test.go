// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	mcpctrl "github.com/agentrq/agentrq/backend/internal/controller/mcp"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
	"github.com/gofiber/fiber/v2"
	"github.com/mustafaturan/monoflake"
)

// isASCII reports whether every byte is one a header value may legally carry.
// This is the property that actually matters: the desktop client's HTTP stack
// refuses a header containing anything above 255 rather than mangling it, and
// refusing crashed its main process.
func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] > 0x7e || s[i] < 0x20 {
			return false
		}
	}
	return true
}

func TestContentDisposition_PlainNameIsLeftAlone(t *testing.T) {
	got := contentDisposition("report.pdf")

	if want := `inline; filename="report.pdf"`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if strings.Contains(got, "filename*") {
		t.Errorf("an ASCII name needs no extended form, got %q", got)
	}
}

func TestContentDisposition_MacOSScreenshotDoesNotEscapeIntoTheHeader(t *testing.T) {
	// The exact filename from the crash report. The space before PM is U+202F
	// NARROW NO-BREAK SPACE, which is what macOS puts there, and the old
	// fmt.Sprintf copied it straight into the header at index 50 -- the index
	// and the code point 8239 both named in the reported TypeError.
	name := "Screenshot 2026-08-30 at 5.51.27 PM.png"

	got := contentDisposition(name)

	if !isASCII(got) {
		t.Fatalf("header value is not ASCII: %q", got)
	}
	if !strings.Contains(got, `filename="Screenshot 2026-08-30 at 5.51.27_PM.png"`) {
		t.Errorf("expected a readable ASCII fallback, got %q", got)
	}
	// %E2%80%AF is U+202F encoded as UTF-8, so a client that understands the
	// extended form still recovers the exact name.
	if !strings.Contains(got, "filename*=UTF-8''Screenshot%202026-08-30%20at%205.51.27%E2%80%AFPM.png") {
		t.Errorf("expected the exact name in extended form, got %q", got)
	}
}

func TestContentDisposition_RejectsHeaderInjection(t *testing.T) {
	// A quote used to close the parameter early and let the rest of the
	// filename be read as parameters of its own.
	got := contentDisposition(`evil".png`)

	if strings.Contains(got, `evil".png`) {
		t.Errorf("quote survived into the header: %q", got)
	}
	if !strings.Contains(got, `filename="evil_.png"`) {
		t.Errorf("expected the quote replaced, got %q", got)
	}
}

func TestContentDisposition_RejectsHeaderSplitting(t *testing.T) {
	// A carriage return would end the header and start another.
	got := contentDisposition("a\r\nX-Injected: yes.png")

	if strings.ContainsAny(got, "\r\n") {
		t.Fatalf("header value contains a line break: %q", got)
	}
	if !isASCII(got) {
		t.Fatalf("header value is not ASCII: %q", got)
	}
}

func TestContentDisposition_BackslashIsNeutralised(t *testing.T) {
	// Inside a quoted string a backslash escapes whatever follows it, so a name
	// ending in one would escape the closing quote.
	got := contentDisposition(`back\slash.png`)

	if !strings.Contains(got, `filename="back_slash.png"`) {
		t.Errorf("expected the backslash replaced, got %q", got)
	}
}

func TestContentDisposition_NamesWithNothingUsableStillGetOne(t *testing.T) {
	// A name made entirely of characters that cannot appear in the header would
	// otherwise produce filename="", which some clients treat as no name at all.
	got := contentDisposition("日本語")

	if !strings.Contains(got, `filename="___"`) {
		t.Errorf("expected placeholder characters, got %q", got)
	}
	if !strings.Contains(got, "filename*=UTF-8''%E6%97%A5%E6%9C%AC%E8%AA%9E") {
		t.Errorf("expected the exact name in extended form, got %q", got)
	}
}

func TestContentDisposition_EmptyName(t *testing.T) {
	if want := `inline; filename="download"`; contentDisposition("") != want {
		t.Errorf("got %q, want %q", contentDisposition(""), want)
	}
}

func TestRFC5987Encode_LeavesOnlyAttrCharsAlone(t *testing.T) {
	// Percent-encoding more than necessary is harmless; encoding less is not.
	// ~ is an attr-char in RFC 5987 section 3.2.1, so it stays as it is.
	if got, want := rfc5987Encode("a-b_c.d~1"), "a-b_c.d~1"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	// ; and = would be read as parameter syntax, so they must not survive.
	if got, want := rfc5987Encode("a;b=c"), "a%3Bb%3Dc"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := rfc5987Encode("a b"), "a%20b"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// A slash command only reaches an ACP agent if it leads the prompt text, so the
// envelope that gives an ordinary reply its context is precisely what stops a
// command being one. These pin which replies lose it.
func TestReplyChannelContent(t *testing.T) {
	const taskID = int64(1234)
	envelope := "[Reply to task " + monoflake.ID(taskID).String() + "] "

	advertised := &mcpctrl.AgentCommandsSnapshot{
		Commands: []mcpctrl.AgentCommand{
			{Name: "compact", Description: "Shorten the context"},
			{Name: "web", Hint: "query"},
		},
	}

	t.Run("delivers an advertised command bare", func(t *testing.T) {
		if got := replyChannelContent(taskID, "/compact", nil, advertised); got != "/compact" {
			t.Errorf("got %q, want the command alone", got)
		}
	})

	t.Run("keeps the command's argument with it", func(t *testing.T) {
		const text = "/web agent client protocol"
		if got := replyChannelContent(taskID, text, nil, advertised); got != text {
			t.Errorf("got %q, want %q", got, text)
		}
	})

	t.Run("trims leading whitespace so the command still leads the prompt", func(t *testing.T) {
		if got := replyChannelContent(taskID, "  /compact", nil, advertised); got != "/compact" {
			t.Errorf("got %q, want the command at the start", got)
		}
	})

	t.Run("keeps the envelope for a path that only looks like a command", func(t *testing.T) {
		// The guard that matters: these are ordinary sentences, and delivering
		// them stripped of their task context would be a silent loss.
		for _, text := range []string{
			"/Users/mt/thing is broken",
			"/etc/hosts looks wrong",
			"/compactify the logs",
			"/",
			"/ compact",
		} {
			want := envelope + text
			if got := replyChannelContent(taskID, text, nil, advertised); got != want {
				t.Errorf("%q: got %q, want the envelope kept", text, got)
			}
		}
	})

	t.Run("keeps the envelope for ordinary text", func(t *testing.T) {
		const text = "please compact the context"
		want := envelope + text
		if got := replyChannelContent(taskID, text, nil, advertised); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("keeps the envelope when no agent has advertised anything", func(t *testing.T) {
		// Anything that is not an ACP agent reports nothing, so nothing about
		// its behaviour changes.
		want := envelope + "/compact"
		if got := replyChannelContent(taskID, "/compact", nil, nil); got != want {
			t.Errorf("got %q, want the envelope kept", got)
		}
		empty := &mcpctrl.AgentCommandsSnapshot{}
		if got := replyChannelContent(taskID, "/compact", nil, empty); got != want {
			t.Errorf("with an empty list: got %q, want the envelope kept", got)
		}
	})

	t.Run("matches the advertised name exactly", func(t *testing.T) {
		// The agent matches what it advertised; a near miss is not a command.
		for _, text := range []string{"/Compact", "/COMPACT", "/compac"} {
			want := envelope + text
			if got := replyChannelContent(taskID, text, nil, advertised); got != want {
				t.Errorf("%q: got %q, want the envelope kept", text, got)
			}
		}
	})

	t.Run("lists an attachment's link when it has one", func(t *testing.T) {
		got := formatAttachments([]entity.Attachment{
			{ID: "att-1", Filename: "log.txt", MimeType: "text/plain"},
			{ID: "att-2", Filename: "a.png", MimeType: "image/png", URL: "https://cdn/attachments/att-2"},
		})
		want := "Attachments:\n  - id=att-1 name=log.txt type=text/plain\n  - id=att-2 name=a.png type=image/png url=https://cdn/attachments/att-2"
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("lists attachments after the message in both branches", func(t *testing.T) {
		atts := []entity.Attachment{{ID: "att-1", Filename: "log.txt", MimeType: "text/plain"}}
		listed := formatAttachments(atts)
		if listed == "" {
			t.Fatal("expected the attachment to be listed")
		}

		// After a bare command, the listing is on its own line, so it never
		// comes between the command and the start of the prompt.
		got := replyChannelContent(taskID, "/compact", atts, advertised)
		if got != "/compact\n"+listed {
			t.Errorf("command branch: got %q", got)
		}

		got = replyChannelContent(taskID, "look at this", atts, advertised)
		if got != envelope+"look at this\n"+listed {
			t.Errorf("envelope branch: got %q", got)
		}
	})
}

// mockCrudCreateTask is a crud.Controller that answers exactly CreateTask and
// ListTasks — everything createTask's immediate-notify branch needs — and
// panics on anything else, which is the point of embedding the real interface
// with nothing behind it.
type mockCrudCreateTask struct {
	crud.Controller
	createTaskFunc func(ctx context.Context, req entity.CreateTaskRequest) (*entity.CreateTaskResponse, error)
	listTasksFunc  func(ctx context.Context, req entity.ListTasksRequest) (*entity.ListTasksResponse, error)
}

func (m *mockCrudCreateTask) CreateTask(ctx context.Context, req entity.CreateTaskRequest) (*entity.CreateTaskResponse, error) {
	return m.createTaskFunc(ctx, req)
}

// ListTasks honours the request's status filter and limit, because the handler
// now relies on the query to do the filtering rather than sifting the rows
// itself. A double that handed back every task whatever was asked for would
// let a handler that forgot its filter pass.
func (m *mockCrudCreateTask) ListTasks(ctx context.Context, req entity.ListTasksRequest) (*entity.ListTasksResponse, error) {
	rs, err := m.listTasksFunc(ctx, req)
	if err != nil || rs == nil {
		return rs, err
	}
	if len(req.Status) == 0 && req.Limit == 0 {
		return rs, nil
	}
	wanted := make(map[string]bool, len(req.Status))
	for _, s := range req.Status {
		wanted[s] = true
	}
	kept := make([]entity.Task, 0, len(rs.Tasks))
	for _, t := range rs.Tasks {
		if len(wanted) > 0 && !wanted[t.Status] {
			continue
		}
		if req.Limit > 0 && len(kept) == req.Limit {
			break
		}
		kept = append(kept, t)
	}
	return &entity.ListTasksResponse{Tasks: kept}, nil
}

// A human creating a task for an idle agent is the common case, and the one
// that used to skip clearing entirely: createTask pushed the task straight
// over the MCP channel and left ClearContext for StartPoller's next tick,
// up to sixty seconds later, to act on — which is a /clear arriving after the
// task it was meant to precede. It must now clear before it notifies, exactly
// like the poller does. The push itself is recorded nowhere: the poller goes
// on offering the task until the agent takes it, and clearContextFor is what
// keeps the clear behind those repeats from happening twice.
func TestCreateTask_ClearsBeforeNotifyingWhenPushedImmediately(t *testing.T) {
	app := fiber.New()
	created := entity.Task{
		ID:           42,
		WorkspaceID:  1,
		CreatedBy:    "human",
		Assignee:     "agent",
		Status:       "notstarted",
		Title:        "Investigate the flake",
		Body:         "It only reproduces on CI.",
		ClearContext: true,
	}
	crudCtrl := &mockCrudCreateTask{
		createTaskFunc: func(ctx context.Context, req entity.CreateTaskRequest) (*entity.CreateTaskResponse, error) {
			return &entity.CreateTaskResponse{Task: created}, nil
		},
		listTasksFunc: func(ctx context.Context, req entity.ListTasksRequest) (*entity.ListTasksResponse, error) {
			// Nothing else ongoing or pending: the new task is the only row,
			// so the immediate-push branch fires rather than deferring to
			// the poller.
			return &entity.ListTasksResponse{Tasks: []entity.Task{created}}, nil
		},
	}
	srv := &fakeWorkspaceServer{}
	h := &handler{crud: crudCtrl, mcpManager: &fakeMCPManager{server: srv}, bus: eventbus.New()}

	app.Post("/api/v1/workspaces/:id/tasks", func(c *fiber.Ctx) error {
		c.Locals("user_id", monoflake.ID(100).String())
		return h.createTask()(c)
	})

	body := `{"task":{"title":"Investigate the flake","body":"It only reproduces on CI.","createdBy":"human","assignee":"agent","clearContext":true}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/"+monoflake.ID(1).String()+"/tasks", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}

	wantCalls := []string{"ClearContextForTask", "SendChannelNotification"}
	if strings.Join(srv.calls, ",") != strings.Join(wantCalls, ",") {
		t.Fatalf("calls = %v, want %v in that order", srv.calls, wantCalls)
	}
	if srv.clearedTaskID != created.ID || !srv.clearContextWanted {
		t.Errorf("cleared task %d wanted=%v, want task %d wanted=true", srv.clearedTaskID, srv.clearContextWanted, created.ID)
	}
	if srv.notifiedTaskID != created.ID {
		t.Errorf("notified task %d, want %d", srv.notifiedTaskID, created.ID)
	}
}

// A task that did not ask for a clean slate still gets pushed immediately —
// it just skips the clear itself, same as clearContextFor's own rule.
func TestCreateTask_SkipsClearWhenTheTaskDidNotAskForIt(t *testing.T) {
	app := fiber.New()
	created := entity.Task{
		ID:          43,
		WorkspaceID: 1,
		CreatedBy:   "human",
		Assignee:    "agent",
		Status:      "notstarted",
		Title:       "Small follow-up",
	}
	crudCtrl := &mockCrudCreateTask{
		createTaskFunc: func(ctx context.Context, req entity.CreateTaskRequest) (*entity.CreateTaskResponse, error) {
			return &entity.CreateTaskResponse{Task: created}, nil
		},
		listTasksFunc: func(ctx context.Context, req entity.ListTasksRequest) (*entity.ListTasksResponse, error) {
			return &entity.ListTasksResponse{Tasks: []entity.Task{created}}, nil
		},
	}
	srv := &fakeWorkspaceServer{}
	h := &handler{crud: crudCtrl, mcpManager: &fakeMCPManager{server: srv}, bus: eventbus.New()}

	app.Post("/api/v1/workspaces/:id/tasks", func(c *fiber.Ctx) error {
		c.Locals("user_id", monoflake.ID(100).String())
		return h.createTask()(c)
	})

	body := `{"task":{"title":"Small follow-up","createdBy":"human","assignee":"agent"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/"+monoflake.ID(1).String()+"/tasks", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	if _, err := app.Test(req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if srv.clearContextWanted {
		t.Error("asked to clear a task that never requested one")
	}
	wantCalls := []string{"ClearContextForTask", "SendChannelNotification"}
	if strings.Join(srv.calls, ",") != strings.Join(wantCalls, ",") {
		t.Fatalf("calls = %v, want %v (clearContextFor's own no-op branch decides whether to actually clear, not the handler)", srv.calls, wantCalls)
	}
}

// The reported bug, at the handler end (task 0j1wvKhUB5V). A task already
// waiting in the queue used to suppress the push for the task created behind
// it — and StartPoller only ever offered the queue's oldest task, so the newer
// one was never sent by anything. One task the agent ignored hid every task
// created after it.
func TestCreateTask_PushesEvenWhenAnotherTaskIsAlreadyPending(t *testing.T) {
	app := fiber.New()
	created := entity.Task{
		ID:          47,
		WorkspaceID: 1,
		CreatedBy:   "human",
		Assignee:    "agent",
		Status:      "notstarted",
		Title:       "Created behind a stuck one",
	}
	stuck := entity.Task{ID: 46, WorkspaceID: 1, Assignee: "agent", Status: "notstarted", Title: "Never picked up"}
	crudCtrl := &mockCrudCreateTask{
		createTaskFunc: func(ctx context.Context, req entity.CreateTaskRequest) (*entity.CreateTaskResponse, error) {
			return &entity.CreateTaskResponse{Task: created}, nil
		},
		listTasksFunc: func(ctx context.Context, req entity.ListTasksRequest) (*entity.ListTasksResponse, error) {
			return &entity.ListTasksResponse{Tasks: []entity.Task{stuck, created}}, nil
		},
	}
	srv := &fakeWorkspaceServer{}
	h := &handler{crud: crudCtrl, mcpManager: &fakeMCPManager{server: srv}, bus: eventbus.New()}

	app.Post("/api/v1/workspaces/:id/tasks", func(c *fiber.Ctx) error {
		c.Locals("user_id", monoflake.ID(100).String())
		return h.createTask()(c)
	})

	body := `{"task":{"title":"Created behind a stuck one","createdBy":"human","assignee":"agent"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/"+monoflake.ID(1).String()+"/tasks", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	if _, err := app.Test(req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if srv.notifiedTaskID != created.ID {
		t.Fatalf("notified task %d, want %d — a queued task must not hide the one created behind it", srv.notifiedTaskID, created.ID)
	}
}

// Still held back while the agent is actually working: that one is not a
// starvation risk, because the poller offers the queue again the moment the
// ongoing task is done.
func TestCreateTask_DoesNotPushWhileAnotherTaskIsOngoing(t *testing.T) {
	app := fiber.New()
	created := entity.Task{
		ID:          48,
		WorkspaceID: 1,
		CreatedBy:   "human",
		Assignee:    "agent",
		Status:      "notstarted",
		Title:       "Created mid-work",
	}
	working := entity.Task{ID: 49, WorkspaceID: 1, Assignee: "agent", Status: "ongoing", Title: "In progress"}
	crudCtrl := &mockCrudCreateTask{
		createTaskFunc: func(ctx context.Context, req entity.CreateTaskRequest) (*entity.CreateTaskResponse, error) {
			return &entity.CreateTaskResponse{Task: created}, nil
		},
		listTasksFunc: func(ctx context.Context, req entity.ListTasksRequest) (*entity.ListTasksResponse, error) {
			return &entity.ListTasksResponse{Tasks: []entity.Task{working, created}}, nil
		},
	}
	srv := &fakeWorkspaceServer{}
	h := &handler{crud: crudCtrl, mcpManager: &fakeMCPManager{server: srv}, bus: eventbus.New()}

	app.Post("/api/v1/workspaces/:id/tasks", func(c *fiber.Ctx) error {
		c.Locals("user_id", monoflake.ID(100).String())
		return h.createTask()(c)
	})

	body := `{"task":{"title":"Created mid-work","createdBy":"human","assignee":"agent"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/"+monoflake.ID(1).String()+"/tasks", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	if _, err := app.Test(req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(srv.calls) != 0 {
		t.Fatalf("calls = %v, want none while another task is ongoing", srv.calls)
	}
}

// The gate is the agent's own concurrency limit, not "is anything running".
// A gateway that will run four tasks at once and is running one has room, and
// withholding the task until it finished made the concurrency control
// meaningless from the push side.
func TestCreateTask_PushesWhileTheAgentHasRoomForMoreTasks(t *testing.T) {
	app := fiber.New()
	created := entity.Task{ID: 50, WorkspaceID: 1, CreatedBy: "human", Assignee: "agent", Status: "notstarted", Title: "Third of four"}
	running := []entity.Task{
		{ID: 51, WorkspaceID: 1, Assignee: "agent", Status: "ongoing", Title: "One"},
		{ID: 52, WorkspaceID: 1, Assignee: "agent", Status: "ongoing", Title: "Two"},
	}
	var asked entity.ListTasksRequest
	crudCtrl := &mockCrudCreateTask{
		createTaskFunc: func(ctx context.Context, req entity.CreateTaskRequest) (*entity.CreateTaskResponse, error) {
			return &entity.CreateTaskResponse{Task: created}, nil
		},
		listTasksFunc: func(ctx context.Context, req entity.ListTasksRequest) (*entity.ListTasksResponse, error) {
			asked = req
			return &entity.ListTasksResponse{Tasks: append(append([]entity.Task{}, running...), created)}, nil
		},
	}
	srv := &fakeWorkspaceServer{}
	mgr := &fakeMCPManager{server: srv, concurrency: &mcpctrl.AgentConcurrencySnapshot{MaxConcurrency: 4}}
	h := &handler{crud: crudCtrl, mcpManager: mgr, bus: eventbus.New()}

	app.Post("/api/v1/workspaces/:id/tasks", func(c *fiber.Ctx) error {
		c.Locals("user_id", monoflake.ID(100).String())
		return h.createTask()(c)
	})

	body := `{"task":{"title":"Third of four","createdBy":"human","assignee":"agent"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/"+monoflake.ID(1).String()+"/tasks", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if _, err := app.Test(req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if srv.notifiedTaskID != created.ID {
		t.Fatalf("notified task %d, want %d — two of four slots busy is room", srv.notifiedTaskID, created.ID)
	}
	// The query has to do the work: statuses named, and bounded by the limit
	// rather than fetching the workspace to count two rows.
	if len(asked.Status) != 1 || asked.Status[0] != "ongoing" {
		t.Errorf("asked for status %v, want exactly [ongoing]", asked.Status)
	}
	if asked.Limit != 5 {
		t.Errorf("asked for limit %d, want 5 (the limit, plus one for the task being created)", asked.Limit)
	}
}

// Full is full: every slot busy holds the push back, and the poller offers the
// task as soon as one frees.
func TestCreateTask_DoesNotPushWhenEverySlotIsBusy(t *testing.T) {
	app := fiber.New()
	created := entity.Task{ID: 53, WorkspaceID: 1, CreatedBy: "human", Assignee: "agent", Status: "notstarted", Title: "No room"}
	running := []entity.Task{
		{ID: 54, WorkspaceID: 1, Assignee: "agent", Status: "ongoing", Title: "One"},
		{ID: 55, WorkspaceID: 1, Assignee: "agent", Status: "ongoing", Title: "Two"},
	}
	crudCtrl := &mockCrudCreateTask{
		createTaskFunc: func(ctx context.Context, req entity.CreateTaskRequest) (*entity.CreateTaskResponse, error) {
			return &entity.CreateTaskResponse{Task: created}, nil
		},
		listTasksFunc: func(ctx context.Context, req entity.ListTasksRequest) (*entity.ListTasksResponse, error) {
			return &entity.ListTasksResponse{Tasks: append(append([]entity.Task{}, running...), created)}, nil
		},
	}
	srv := &fakeWorkspaceServer{}
	mgr := &fakeMCPManager{server: srv, concurrency: &mcpctrl.AgentConcurrencySnapshot{MaxConcurrency: 2}}
	h := &handler{crud: crudCtrl, mcpManager: mgr, bus: eventbus.New()}

	app.Post("/api/v1/workspaces/:id/tasks", func(c *fiber.Ctx) error {
		c.Locals("user_id", monoflake.ID(100).String())
		return h.createTask()(c)
	})

	body := `{"task":{"title":"No room","createdBy":"human","assignee":"agent"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/"+monoflake.ID(1).String()+"/tasks", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if _, err := app.Test(req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(srv.calls) != 0 {
		t.Fatalf("calls = %v, want none: both of two slots are busy", srv.calls)
	}
}

// A task created directly as ongoing does not count itself out of its own
// push. It sorts first on this query's `updated_at desc`, which is why one row
// more than the limit is asked for — at a limit of one, asking for a single
// row would return only this task and read as a workspace with no room.
func TestCreateTask_DoesNotCountTheNewTaskAgainstTheLimit(t *testing.T) {
	app := fiber.New()
	created := entity.Task{ID: 56, WorkspaceID: 1, CreatedBy: "human", Assignee: "agent", Status: "ongoing", Title: "Starts as ongoing"}
	crudCtrl := &mockCrudCreateTask{
		createTaskFunc: func(ctx context.Context, req entity.CreateTaskRequest) (*entity.CreateTaskResponse, error) {
			return &entity.CreateTaskResponse{Task: created}, nil
		},
		listTasksFunc: func(ctx context.Context, req entity.ListTasksRequest) (*entity.ListTasksResponse, error) {
			// The only ongoing task in the workspace is the one just created.
			return &entity.ListTasksResponse{Tasks: []entity.Task{created}}, nil
		},
	}
	srv := &fakeWorkspaceServer{}
	h := &handler{crud: crudCtrl, mcpManager: &fakeMCPManager{server: srv}, bus: eventbus.New()}

	app.Post("/api/v1/workspaces/:id/tasks", func(c *fiber.Ctx) error {
		c.Locals("user_id", monoflake.ID(100).String())
		return h.createTask()(c)
	})

	body := `{"task":{"title":"Starts as ongoing","createdBy":"human","assignee":"agent","status":"ongoing"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/"+monoflake.ID(1).String()+"/tasks", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if _, err := app.Test(req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if srv.notifiedTaskID != created.ID {
		t.Fatalf("notified task %d, want %d — the new task must not block its own push", srv.notifiedTaskID, created.ID)
	}
}

// Nothing has reported a limit — a workspace with nothing attached, or a
// gateway too old to say. One task at a time, which is what every workspace
// did before a gateway could name a limit.
func TestAgentTaskConcurrencyDefaultsToOne(t *testing.T) {
	if got := agentTaskConcurrency(&fakeMCPManager{}, 1); got != 1 {
		t.Errorf("no snapshot: got %d, want 1", got)
	}
	if got := agentTaskConcurrency(&fakeMCPManager{concurrency: &mcpctrl.AgentConcurrencySnapshot{}}, 1); got != 1 {
		t.Errorf("snapshot reporting no limit: got %d, want 1", got)
	}
	if got := agentTaskConcurrency(nil, 1); got != 1 {
		t.Errorf("no manager at all: got %d, want 1", got)
	}
	if got := agentTaskConcurrency(&fakeMCPManager{concurrency: &mcpctrl.AgentConcurrencySnapshot{MaxConcurrency: 7}}, 1); got != 7 {
		t.Errorf("reported limit: got %d, want 7", got)
	}
}

// mockCrudUpdateTaskAssignee answers exactly UpdateTaskAssignee.
type mockCrudUpdateTaskAssignee struct {
	crud.Controller
	updateTaskAssigneeFunc func(ctx context.Context, req entity.UpdateTaskAssigneeRequest) (*entity.UpdateTaskAssigneeResponse, error)
}

func (m *mockCrudUpdateTaskAssignee) UpdateTaskAssignee(ctx context.Context, req entity.UpdateTaskAssigneeRequest) (*entity.UpdateTaskAssigneeResponse, error) {
	return m.updateTaskAssigneeFunc(ctx, req)
}

// Reassigning a task to the agent is the same kind of immediate push as
// createTask's, on a task that may equally have asked for a clean slate, so
// it has to clear first for the same reason.
func TestUpdateTaskAssignee_ClearsBeforeNotifyingWhenReassignedToAgent(t *testing.T) {
	app := fiber.New()
	reassigned := entity.Task{ID: 44, WorkspaceID: 1, Title: "Pick this back up", ClearContext: true}
	crudCtrl := &mockCrudUpdateTaskAssignee{
		updateTaskAssigneeFunc: func(ctx context.Context, req entity.UpdateTaskAssigneeRequest) (*entity.UpdateTaskAssigneeResponse, error) {
			return &entity.UpdateTaskAssigneeResponse{Task: reassigned}, nil
		},
	}
	srv := &fakeWorkspaceServer{}
	h := &handler{crud: crudCtrl, mcpManager: &fakeMCPManager{server: srv}, bus: eventbus.New()}

	app.Patch("/api/v1/workspaces/:id/tasks/:taskID/assignee", func(c *fiber.Ctx) error {
		c.Locals("user_id", monoflake.ID(100).String())
		return h.updateTaskAssignee()(c)
	})

	req := httptest.NewRequest(
		http.MethodPatch,
		"/api/v1/workspaces/"+monoflake.ID(1).String()+"/tasks/"+monoflake.ID(44).String()+"/assignee",
		strings.NewReader(`{"assignee":{"value":"agent"}}`),
	)
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	wantCalls := []string{"ClearContextForTask", "SendChannelNotification"}
	if strings.Join(srv.calls, ",") != strings.Join(wantCalls, ",") {
		t.Fatalf("calls = %v, want %v in that order", srv.calls, wantCalls)
	}
	if srv.clearedTaskID != reassigned.ID || !srv.clearContextWanted {
		t.Errorf("cleared task %d wanted=%v, want task %d wanted=true", srv.clearedTaskID, srv.clearContextWanted, reassigned.ID)
	}
}
