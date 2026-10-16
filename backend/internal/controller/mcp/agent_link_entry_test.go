// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/agentrq/agentrq/backend/internal/controller/agentlink"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/mustafaturan/monoflake"
)

// entryRecorder is a link server wired up to post messages and change tasks,
// with a record of what it posted.
type entryRecorder struct {
	ps *WorkspaceServer

	mu     sync.Mutex
	posted []postedMessage
	agents []entity.TaskAgent // what registerTaskAgent was handed
	posts  chan postedMessage
}

func (r *entryRecorder) postedMessages() []postedMessage {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]postedMessage(nil), r.posted...)
}

func newEntryServer(t *testing.T) *entryRecorder {
	t.Helper()
	ps, _ := newLinkServer(t)
	rec := &entryRecorder{ps: ps, posts: make(chan postedMessage, 8)}
	ps.idgen = &fakeIDs{}
	ps.requestTools = make(map[string]string)
	ps.requestParams = make(map[string]*agentlink.PermissionRequest)
	ps.sessionTasks = make(map[string]int64)
	ps.permissionResponses = make(map[string]int64)
	ps.toolCallIDs = make(map[string]int64)
	ps.undeliveredVerdicts = make(map[string]string)
	ps.getTask = func(ctx context.Context, taskID int64) (model.Task, error) { return model.Task{ID: taskID}, nil }
	ps.listTasks = func(ctx context.Context, filter ListTasksFilter) ([]model.Task, error) { return nil, nil }
	ps.updateMessageMetadata = func(ctx context.Context, taskID, messageID int64, metadata any) error { return nil }
	ps.reply = func(ctx context.Context, chatID, text string, a []entity.Attachment, metadata any) (int64, error) {
		m := postedMessage{chatID: chatID, text: text, metadata: asMap(t, metadata)}
		if m.metadata == nil {
			m.metadata = map[string]any{}
		}
		if agent := entity.GetTaskAgent(ctx); agent.Name != "" {
			m.metadata["agent"] = agent.Name
		}
		rec.mu.Lock()
		rec.posted = append(rec.posted, m)
		n := len(rec.posted)
		rec.mu.Unlock()
		rec.posts <- m
		return int64(700 + n), nil
	}
	ps.registerTaskAgent = func(ctx context.Context, a entity.TaskAgent) entity.TaskAgent {
		rec.mu.Lock()
		rec.agents = append(rec.agents, a)
		rec.mu.Unlock()
		return a
	}
	return rec
}

func TestAgentLinkEntryPoints(t *testing.T) {
	ctx := context.Background()
	task42 := monoflake.ID(42).String()

	t.Run("a link's permission request takes the custom-notification relay", func(t *testing.T) {
		rec := newEntryServer(t)
		rec.ps.HandlePermissionRequest(ctx, "acp1:a", agentlink.PermissionRequest{
			RequestID: "acp1:a:s1:t1", TaskID: task42, ToolName: "Bash", Description: "Bash", InputPreview: "{}",
		})

		posted := rec.postedMessages()
		if len(posted) != 1 {
			t.Fatalf("posted %d messages, want one permission card", len(posted))
		}
		md := posted[0].metadata
		if md["type"] != "permission_request" || md["requestId"] != "acp1:a:s1:t1" || md["status"] != "pending" {
			t.Fatalf("card metadata was %v, want a pending permission_request for acp1:a:s1:t1", md)
		}
		if got := rec.ps.permissionRequests["acp1:a:s1:t1"]; got != "acp1:a" {
			t.Fatalf("the request was filed under %q, want the link acp1:a", got)
		}
	})

	t.Run("a link's permission request records its tool call against the request", func(t *testing.T) {
		rec := newEntryServer(t)
		rec.ps.recordToolCall = func(ctx context.Context, tc model.ToolCall) (model.ToolCall, error) {
			tc.ID = 900
			return tc, nil
		}
		rec.ps.HandlePermissionRequest(ctx, "acp1:a", agentlink.PermissionRequest{
			RequestID: "acp1:a:s1:t3", TaskID: task42, ToolName: "Bash", InputPreview: "{}",
		})

		if got := rec.ps.toolCallIDs["acp1:a:s1:t3"]; got != 900 {
			t.Fatalf("the request's tool call was %d, want 900", got)
		}
	})

	t.Run("a link's permission request with no task to show it in posts nothing", func(t *testing.T) {
		rec := newEntryServer(t)
		rec.ps.getTask = func(ctx context.Context, taskID int64) (model.Task, error) {
			return model.Task{}, errors.New("task not found")
		}
		rec.ps.sessionTasks["sess-other"] = 9
		rec.ps.HandlePermissionRequest(ctx, "acp1:a", agentlink.PermissionRequest{
			RequestID: "acp1:a:s1:t4", TaskID: task42, ToolName: "Bash", InputPreview: "{}",
		})

		if posted := rec.postedMessages(); len(posted) != 0 {
			t.Fatalf("posted %v, want nothing without a task", posted)
		}
	})

	t.Run("an auto-allowed tool is answered without a card", func(t *testing.T) {
		rec := newEntryServer(t)
		rec.ps.autoAllowedTools = []string{"Read"}
		link := &fakeLink{id: "acp1:a"}
		rec.ps.AttachAgentLink(link)

		rec.ps.HandlePermissionRequest(ctx, "acp1:a", agentlink.PermissionRequest{
			RequestID: "acp1:a:s1:t2", TaskID: task42, ToolName: "Read", InputPreview: "{}",
		})

		deadline := time.Now().Add(500 * time.Millisecond)
		for {
			if _, _, verdicts := link.recorded(); len(verdicts) == 1 {
				if verdicts[0] != "acp1:a:s1:t2=allow" {
					t.Fatalf("link verdicts were %v, want [acp1:a:s1:t2=allow]", verdicts)
				}
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("the link was never told the auto-allowed request was allowed")
			}
			time.Sleep(10 * time.Millisecond)
		}
		if posted := rec.postedMessages(); len(posted) != 0 {
			t.Fatalf("posted %v, want no card for an auto-allowed tool", posted)
		}
	})

	t.Run("a withdrawn request's card is marked cancelled and forgotten", func(t *testing.T) {
		rec := newEntryServer(t)
		rec.ps.recordToolCall = func(ctx context.Context, tc model.ToolCall) (model.ToolCall, error) {
			tc.ID = 901
			return tc, nil
		}
		type metadataUpdate struct {
			taskID, messageID int64
			status            any
		}
		var cards []metadataUpdate
		rec.ps.updateMessageMetadata = func(ctx context.Context, taskID, messageID int64, metadata any) error {
			cards = append(cards, metadataUpdate{taskID, messageID, metadata.(map[string]any)["status"]})
			return nil
		}
		var toolCalls []string
		rec.ps.updateToolCallStatus = func(ctx context.Context, id int64, status string) error {
			toolCalls = append(toolCalls, fmt.Sprintf("%d=%s", id, status))
			return nil
		}
		rec.ps.HandlePermissionRequest(ctx, "acp1:a", agentlink.PermissionRequest{
			RequestID: "acp1:a:s1:t5", TaskID: task42, ToolName: "Bash", InputPreview: "{}",
		})

		rec.ps.ClosePermissionRequest(ctx, "acp1:a:s1:t5")

		if len(cards) != 1 || cards[0] != (metadataUpdate{42, 701, "cancelled"}) {
			t.Fatalf("card updates were %+v, want message 701 of task 42 marked cancelled", cards)
		}
		if len(toolCalls) != 1 || toolCalls[0] != "901=cancelled" {
			t.Fatalf("tool call updates were %v, want [901=cancelled]", toolCalls)
		}
		if _, ok := rec.ps.permissionRequests["acp1:a:s1:t5"]; ok {
			t.Fatal("the withdrawn request is still known, want it forgotten")
		}
		if _, ok := rec.ps.requestTaskIDs["acp1:a:s1:t5"]; ok {
			t.Fatal("the withdrawn request still names its task, want it forgotten")
		}
	})

	t.Run("PostAgentReply refuses on an archived workspace", func(t *testing.T) {
		rec := newEntryServer(t)
		now := time.Now()
		rec.ps.archivedAt = &now

		if _, err := rec.ps.PostAgentReply(ctx, "acp1:a", 42, "done"); !errors.Is(err, ErrWorkspaceArchived) {
			t.Fatalf("PostAgentReply returned %v, want ErrWorkspaceArchived", err)
		}
		if posted := rec.postedMessages(); len(posted) != 0 {
			t.Fatalf("posted %v on an archived workspace, want nothing", posted)
		}
	})

	t.Run("PostAgentReply posts as the agent the link reported, with its model", func(t *testing.T) {
		rec := newEntryServer(t)
		rec.ps.AttachAgentLink(&fakeLink{id: "acp1:a"})
		rec.ps.HandleAgentIdentity(ctx, "acp1:a", agentlink.IdentityReport{Name: "codex-acp"})
		rec.ps.HandleAgentModels(ctx, "acp1:a", agentlink.ModelsReport{
			SessionID: "s1", ConfigID: "model", CurrentModel: "m1", Models: []agentlink.Model{{ID: "m1", Name: "Model one"}},
		})

		id, err := rec.ps.PostAgentReply(ctx, "acp1:a", 42, "done")
		if err != nil || id == 0 {
			t.Fatalf("PostAgentReply returned (%d, %v), want a message id and no error", id, err)
		}
		posted := rec.postedMessages()
		if len(posted) != 1 || posted[0].chatID != task42 || posted[0].text != "done" || posted[0].metadata["agent"] != "codex-acp" {
			t.Fatalf("posted %+v, want one reply in chat %s attributed to codex-acp", posted, task42)
		}
		if len(rec.agents) != 1 || rec.agents[0] != (entity.TaskAgent{Name: "codex-acp", Model: "Model one"}) {
			t.Fatalf("registered agents %+v, want codex-acp on Model one", rec.agents)
		}
	})

	t.Run("PostAgentReply from a link that named no agent is attributed to nobody", func(t *testing.T) {
		rec := newEntryServer(t)
		if _, err := rec.ps.PostAgentReply(ctx, "acp1:a", 42, "done"); err != nil {
			t.Fatalf("PostAgentReply returned %v, want nil", err)
		}
		if posted := rec.postedMessages(); len(posted) != 1 || posted[0].metadata["agent"] != nil {
			t.Fatalf("posted %+v, want one reply with no agent", posted)
		}
		if len(rec.agents) != 0 {
			t.Fatalf("registered agents %+v, want none", rec.agents)
		}
	})

	t.Run("AskHuman posts the elicit tool's card and returns the answer", func(t *testing.T) {
		rec := newEntryServer(t)
		go func() {
			m := <-rec.posts
			id, _ := m.metadata["requestId"].(string)
			_ = rec.ps.RespondToElicitation(id, "accept", map[string]any{"ok": true})
		}()

		got, err := rec.ps.AskHuman(ctx, 42, agentlink.Question{
			Mode: "form", Message: "Proceed?",
			RequestedSchema: map[string]any{"type": "object", "properties": map[string]any{"ok": map[string]any{"type": "boolean"}}},
		})
		if err != nil {
			t.Fatalf("AskHuman returned %v, want nil", err)
		}
		if got.Action != "accept" || got.Content["ok"] != true {
			t.Fatalf("AskHuman returned %+v, want accept with ok=true", got)
		}
		md := rec.postedMessages()[0].metadata
		if md["type"] != "elicitation_request" || md["mode"] != "form" || md["status"] != "pending" {
			t.Fatalf("card metadata was %v, want a pending form elicitation_request", md)
		}
	})

	t.Run("AskHuman with a link posts a url card", func(t *testing.T) {
		rec := newEntryServer(t)
		go func() {
			m := <-rec.posts
			id, _ := m.metadata["requestId"].(string)
			_ = rec.ps.RespondToElicitation(id, "decline", nil)
		}()

		got, err := rec.ps.AskHuman(ctx, 42, agentlink.Question{Mode: "url", Message: "Sign in", URL: "https://example.com/login"})
		if err != nil || got.Action != "decline" {
			t.Fatalf("AskHuman returned (%+v, %v), want decline", got, err)
		}
		if md := rec.postedMessages()[0].metadata; md["url"] != "https://example.com/login" {
			t.Fatalf("card metadata was %v, want the url", md)
		}
	})

	t.Run("AskHuman reports a card that could not be posted", func(t *testing.T) {
		rec := newEntryServer(t)
		errDown := errors.New("database unavailable")
		rec.ps.reply = func(ctx context.Context, chatID, text string, a []entity.Attachment, metadata any) (int64, error) {
			return 0, errDown
		}
		if _, err := rec.ps.AskHuman(ctx, 42, agentlink.Question{Mode: "url", Message: "Sign in", URL: "https://example.com"}); !errors.Is(err, errDown) {
			t.Fatalf("AskHuman returned %v, want the posting error", err)
		}
	})

	t.Run("AskHuman refuses an empty message, a form without a schema, a url without a link and an unknown mode", func(t *testing.T) {
		rec := newEntryServer(t)
		for name, q := range map[string]agentlink.Question{
			"empty message":       {Mode: "url", URL: "https://example.com"},
			"form without schema": {Mode: "form", Message: "?"},
			"url without link":    {Mode: "url", Message: "?"},
			"unknown mode":        {Mode: "chat", Message: "?"},
		} {
			if _, err := rec.ps.AskHuman(ctx, 42, q); !errors.Is(err, ErrBadQuestion) {
				t.Errorf("%s: AskHuman returned %v, want ErrBadQuestion", name, err)
			}
		}
		if posted := rec.postedMessages(); len(posted) != 0 {
			t.Fatalf("posted %v, want nothing", posted)
		}
	})

	t.Run("AskHuman refuses a schema the elicit tool would refuse", func(t *testing.T) {
		rec := newEntryServer(t)
		_, err := rec.ps.AskHuman(ctx, 42, agentlink.Question{Mode: "form", Message: "?", RequestedSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"nested": map[string]any{"type": "object"}},
		}})
		if err == nil || !strings.Contains(err.Error(), "invalid requestedSchema") {
			t.Fatalf("AskHuman returned %v, want an invalid requestedSchema error", err)
		}
	})

	t.Run("AskHuman waits an hour at most", func(t *testing.T) {
		for in, want := range map[time.Duration]time.Duration{
			0:                time.Hour,
			-time.Second:     time.Hour,
			time.Minute:      time.Minute,
			2 * time.Hour:    time.Hour,
			time.Hour:        time.Hour,
			90 * time.Second: 90 * time.Second,
		} {
			if got := questionTimeout(in); got != want {
				t.Errorf("questionTimeout(%v) was %v, want %v", in, got, want)
			}
		}
	})

	t.Run("CreateHumanTask makes an ongoing human task with the title cut to 80 characters", func(t *testing.T) {
		rec := newEntryServer(t)
		var created model.Task
		rec.ps.createTask = func(ctx context.Context, task model.Task) (model.Task, error) {
			created = task
			return task, nil
		}

		id, err := rec.ps.CreateHumanTask(ctx, strings.Repeat("é", 100), "Sign in, please")
		if err != nil || id == 0 || id != created.ID {
			t.Fatalf("CreateHumanTask returned (%d, %v), want the new task's id", id, err)
		}
		if created.Assignee != "human" || created.Status != "ongoing" || created.CreatedBy != "agent" || created.Body != "Sign in, please" {
			t.Fatalf("created %+v, want an ongoing human task created by the agent", created)
		}
		if utf8.RuneCountInString(created.Title) != 80 || !strings.HasSuffix(created.Title, "...") {
			t.Fatalf("title was %q (%d runes), want 80 runes ending in ...", created.Title, utf8.RuneCountInString(created.Title))
		}
	})

	t.Run("CreateHumanTask keeps a short title and reports a failed create", func(t *testing.T) {
		rec := newEntryServer(t)
		errDown := errors.New("database unavailable")
		var title string
		rec.ps.createTask = func(ctx context.Context, task model.Task) (model.Task, error) {
			title = task.Title
			return model.Task{}, errDown
		}

		if _, err := rec.ps.CreateHumanTask(ctx, "Sign in", ""); !errors.Is(err, errDown) {
			t.Fatalf("CreateHumanTask returned %v, want the create error", err)
		}
		if title != "Sign in" {
			t.Fatalf("title was %q, want it unchanged", title)
		}
	})

	t.Run("CreateHumanTask refuses on an archived workspace", func(t *testing.T) {
		rec := newEntryServer(t)
		now := time.Now()
		rec.ps.archivedAt = &now
		if _, err := rec.ps.CreateHumanTask(ctx, "Sign in", ""); !errors.Is(err, ErrWorkspaceArchived) {
			t.Fatalf("CreateHumanTask returned %v, want ErrWorkspaceArchived", err)
		}
	})

	t.Run("MarkTaskOngoing moves a notstarted task to ongoing and leaves the rest alone", func(t *testing.T) {
		rec := newEntryServer(t)
		status := map[int64]string{1: "notstarted", 2: "completed"}
		rec.ps.getTask = func(ctx context.Context, taskID int64) (model.Task, error) {
			return model.Task{ID: taskID, Status: status[taskID]}, nil
		}
		var updates []string
		rec.ps.updateStatus = func(ctx context.Context, taskID int64, s string) (model.Task, error) {
			updates = append(updates, monoflake.ID(taskID).String()+"="+s)
			return model.Task{}, nil
		}

		for _, id := range []int64{1, 2} {
			if err := rec.ps.MarkTaskOngoing(ctx, "acp1:a", id); err != nil {
				t.Fatalf("MarkTaskOngoing(%d) returned %v, want nil", id, err)
			}
		}
		if want := monoflake.ID(1).String() + "=ongoing"; len(updates) != 1 || updates[0] != want {
			t.Fatalf("status updates were %v, want only [%s]", updates, want)
		}
	})

	t.Run("MarkTaskOngoing reports a task it cannot read", func(t *testing.T) {
		rec := newEntryServer(t)
		errDown := errors.New("database unavailable")
		rec.ps.getTask = func(ctx context.Context, taskID int64) (model.Task, error) { return model.Task{}, errDown }
		if err := rec.ps.MarkTaskOngoing(ctx, "acp1:a", 1); !errors.Is(err, errDown) {
			t.Fatalf("MarkTaskOngoing returned %v, want the read error", err)
		}
	})

	t.Run("OfferNextTask runs one poll with the poller's repository", func(t *testing.T) {
		rec := newEntryServer(t)
		rec.ps.done = make(chan struct{})
		close(rec.ps.done) // clearContextFor's settle wait returns at once
		link := &fakeLink{id: "acp1:a", free: 1}
		rec.ps.AttachAgentLink(link)
		rec.ps.StartPoller(&pollRepo{list: func(ctx context.Context, req entity.ListTasksRequest, userID int64) ([]model.Task, error) {
			if len(req.Status) == 1 && req.Status[0] == "notstarted" {
				return []model.Task{{ID: 7, Title: "Next", Body: "do it", Status: "notstarted", Assignee: "agent"}}, nil
			}
			return nil, nil
		}})

		if got := rec.ps.OfferNextTask(ctx); got != 7 {
			t.Fatalf("OfferNextTask returned %d, want 7", got)
		}
		if tasks, _, _ := link.recorded(); len(tasks) != 1 || !strings.Contains(tasks[0], "Next assigned task") {
			t.Fatalf("link got %v, want the offered task", tasks)
		}
	})

	t.Run("OfferNextTask before the poller started offers nothing", func(t *testing.T) {
		rec := newEntryServer(t)
		if got := rec.ps.OfferNextTask(ctx); got != 0 {
			t.Fatalf("OfferNextTask returned %d, want 0", got)
		}
	})

	t.Run("ToolNames lists exactly the tools the server registered", func(t *testing.T) {
		rec := newEntryServer(t)
		srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0"}, nil)
		for _, name := range []string{"reply", "getTask"} {
			mcp.AddTool(srv, &mcp.Tool{Name: name}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, any, error) {
				return &mcp.CallToolResult{}, nil, nil
			})
		}
		rec.ps.mcpServer = srv

		got := rec.ps.ToolNames()
		if _, ok := got["reply"]; !ok || len(got) != 2 {
			t.Fatalf("ToolNames returned %v, want {reply, getTask}", got)
		}
		if _, ok := got["getTask"]; !ok {
			t.Fatalf("ToolNames returned %v, want {reply, getTask}", got)
		}
		if rec.ps.liveSessionIDs() != nil {
			t.Fatalf("the listing left sessions %v behind, want none", rec.ps.liveSessionIDs())
		}
	})

	t.Run("ToolNames reads again after a listing that failed", func(t *testing.T) {
		rec := newEntryServer(t)
		srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0"}, nil)
		mcp.AddTool(srv, &mcp.Tool{Name: "reply"}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, any, error) {
			return &mcp.CallToolResult{}, nil, nil
		})
		rec.ps.mcpServer = srv

		restore := toolNameTransports
		defer func() { toolNameTransports = restore }()
		toolNameTransports = func() (mcp.Transport, mcp.Transport) {
			c, _ := mcp.NewInMemoryTransports()
			return c, unreachableTransport{}
		}
		if got := rec.ps.ToolNames(); len(got) != 0 {
			t.Fatalf("with the server unreachable, ToolNames returned %v, want none", got)
		}
		toolNameTransports = restore
		if _, ok := rec.ps.ToolNames()["reply"]; !ok {
			t.Fatal("ToolNames kept the failed listing, want it read again")
		}
	})

	t.Run("ToolNames with no server lists nothing", func(t *testing.T) {
		rec := newEntryServer(t)
		if got := rec.ps.ToolNames(); len(got) != 0 {
			t.Fatalf("ToolNames returned %v, want none", got)
		}
	})

	t.Run("ToolNames lists nothing when the server cannot be reached or refuses", func(t *testing.T) {
		failing := func(method string) *mcp.Server {
			srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0"}, nil)
			mcp.AddTool(srv, &mcp.Tool{Name: "reply"}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, any, error) {
				return &mcp.CallToolResult{}, nil, nil
			})
			srv.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
				return func(ctx context.Context, m string, req mcp.Request) (mcp.Result, error) {
					if strings.Contains(method, m) {
						return nil, errors.New("server unavailable")
					}
					return next(ctx, m, req)
				}
			})
			return srv
		}
		// A client opens with server/discover, falling back to initialize.
		for _, method := range []string{"server/discover initialize", "tools/list"} {
			if got := listToolNames(failing(method)); len(got) != 0 {
				t.Errorf("with %s refused, listToolNames returned %v, want none", method, got)
			}
		}

		defer func(f func() (mcp.Transport, mcp.Transport)) { toolNameTransports = f }(toolNameTransports)
		toolNameTransports = func() (mcp.Transport, mcp.Transport) {
			c, _ := mcp.NewInMemoryTransports()
			return c, unreachableTransport{}
		}
		if got := listToolNames(failing("none")); len(got) != 0 {
			t.Errorf("with the server unreachable, listToolNames returned %v, want none", got)
		}
	})
}

// unreachableTransport is a server end that cannot be connected.
type unreachableTransport struct{}

func (unreachableTransport) Connect(context.Context) (mcp.Connection, error) {
	return nil, errors.New("transport unavailable")
}
