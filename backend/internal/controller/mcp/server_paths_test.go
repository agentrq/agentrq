// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"bytes"
	"context"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agentrq/agentrq/backend/internal/controller/agentlink"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/service/auth"
	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
	mock_auth "github.com/agentrq/agentrq/backend/internal/service/mocks/auth"
	mock_idgen "github.com/agentrq/agentrq/backend/internal/service/mocks/idgen"
	"github.com/golang-jwt/jwt/v5"
	"github.com/golang/mock/gomock"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/mustafaturan/monoflake"
)

// idTransport names the sessions it carries, as the streamable HTTP transport
// does; the in-memory one leaves every session's ID empty, which hides every
// path that keys something by session.
type idTransport struct {
	mcp.Transport
	id string
}

func (t idTransport) Connect(ctx context.Context) (mcp.Connection, error) {
	c, err := t.Transport.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return idConn{Connection: c, id: t.id}, nil
}

type idConn struct {
	mcp.Connection
	id string
}

func (c idConn) SessionID() string { return c.id }

// namedSession attaches a client to ps's MCP server (creating one when it has
// none) over a session with the given ID, and returns the server's side of it.
func namedSession(t *testing.T, ps *WorkspaceServer, id string) *mcp.ServerSession {
	t.Helper()
	if ps.mcpServer == nil {
		ps.mcpServer = mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	}
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	ctx := context.Background()
	ss, err := ps.mcpServer.Connect(ctx, idTransport{Transport: serverTransport, id: id}, nil)
	if err != nil {
		t.Fatalf("connect server: %v", err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "claude-code", Version: "1"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connect client: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return ss
}

func callOn(ss *mcp.ServerSession) *mcp.CallToolRequest {
	return &mcp.CallToolRequest{Session: ss, Params: &mcp.CallToolParamsRaw{}}
}

func toolResultText(res *mcp.CallToolResult) string {
	var sb strings.Builder
	for _, c := range res.Content {
		if text, ok := c.(*mcp.TextContent); ok {
			sb.WriteString(text.Text)
		}
	}
	return sb.String()
}

func wantToolError(t *testing.T, res *mcp.CallToolResult, want string) {
	t.Helper()
	if res == nil || !res.IsError || !strings.Contains(toolResultText(res), want) {
		t.Errorf("answered %+v, want the tool error %q", res, want)
	}
}

func fixedIDs(t *testing.T, id int64) *mock_idgen.MockService {
	t.Helper()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	ids := mock_idgen.NewMockService(ctrl)
	ids.EXPECT().NextID().Return(id).AnyTimes()
	return ids
}

func sessionTask(ps *WorkspaceServer, sessID string) (int64, bool) {
	ps.sessionTasksMu.RLock()
	defer ps.sessionTasksMu.RUnlock()
	id, ok := ps.sessionTasks[sessID]
	return id, ok
}

// Every tool that names a task remembers it as the session's, so a permission
// request on that session is filed under it.
func TestToolsRememberTheSessionsTask(t *testing.T) {
	t.Run("createTask", func(t *testing.T) {
		replies := 0
		ps := permissionServer(t, &replies)
		ps.idgen = fixedIDs(t, 55)
		ps.createTask = func(_ context.Context, task model.Task) (model.Task, error) { return task, nil }
		ss := namedSession(t, ps, "session-create")

		res, _, _ := ps.handleCreateTask(context.Background(), callOn(ss), CreateTaskParams{Title: "t", Body: "b"})
		if res.IsError {
			t.Fatalf("create failed: %s", toolResultText(res))
		}
		if id, _ := sessionTask(ps, "session-create"); id != 55 {
			t.Errorf("the session's task is %d, want 55", id)
		}
	})

	t.Run("reply", func(t *testing.T) {
		replies := 0
		ps := permissionServer(t, &replies)
		ss := namedSession(t, ps, "session-reply")

		res, _, _ := ps.handleReply(context.Background(), callOn(ss), ReplyParams{TaskID: monoflake.ID(66).String(), Text: "done"})
		if res.IsError {
			t.Fatalf("reply failed: %s", toolResultText(res))
		}
		if id, _ := sessionTask(ps, "session-reply"); id != 66 {
			t.Errorf("the session's task is %d, want 66", id)
		}
	})

	t.Run("getTask with no task named", func(t *testing.T) {
		replies := 0
		ps := permissionServer(t, &replies)
		ps.getNextTask = func(context.Context) (model.Task, error) { return model.Task{ID: 77, Title: "next"}, nil }
		ss := namedSession(t, ps, "session-next")

		res, _, _ := ps.handleGetTask(context.Background(), callOn(ss), GetTaskParams{})
		if res.IsError {
			t.Fatalf("getTask failed: %s", toolResultText(res))
		}
		if id, _ := sessionTask(ps, "session-next"); id != 77 {
			t.Errorf("the session's task is %d, want 77", id)
		}
	})
}

func TestHandlers_RefuseIncompleteCalls(t *testing.T) {
	replies := 0
	ps := permissionServer(t, &replies)
	ctx := context.Background()

	res, _, _ := ps.handleCreateTask(ctx, nil, CreateTaskParams{Title: "t"})
	wantToolError(t, res, "body is required")

	res, _, _ = ps.handleUpdateTaskStatus(ctx, nil, UpdateTaskStatusParams{TaskID: "abc"})
	wantToolError(t, res, "status is required")

	res, _, _ = ps.handleUpdateTaskStatus(ctx, nil, UpdateTaskStatusParams{TaskID: "0", Status: "ongoing"})
	wantToolError(t, res, "invalid taskId format")

	res, _, _ = ps.handleGetAttachment(ctx, nil, GetAttachmentParams{TaskID: "0", AttachmentID: "a1"})
	wantToolError(t, res, "invalid taskId format")

	res, _, _ = ps.handlePublishEvent(ctx, nil, PublishEventParams{})
	wantToolError(t, res, "name is required")
}

// A store that fails is reported as a tool error naming what failed.
func TestHandlers_ReportAFailingStore(t *testing.T) {
	replies := 0
	ps := permissionServer(t, &replies)
	ctx := context.Background()
	ps.idgen = fixedIDs(t, 55)
	ps.createTask = func(context.Context, model.Task) (model.Task, error) { return model.Task{}, errNotTheRealThing }
	ps.updateStatus = func(context.Context, int64, string) (model.Task, error) { return model.Task{}, errNotTheRealThing }
	ps.reply = func(context.Context, string, string, []entity.Attachment, any) (int64, error) {
		return 0, errNotTheRealThing
	}
	ps.getTask = func(context.Context, int64) (model.Task, error) { return model.Task{}, errNotTheRealThing }
	ps.publishEvent = func(context.Context, string, string, []entity.EventFAQ, WorkflowRunContext) error {
		return errNotTheRealThing
	}
	task := monoflake.ID(42).String()

	res, _, _ := ps.handleCreateTask(ctx, nil, CreateTaskParams{Title: "t", Body: "b"})
	wantToolError(t, res, "failed to create task: not the real thing")

	res, _, _ = ps.handleUpdateTaskStatus(ctx, nil, UpdateTaskStatusParams{TaskID: task, Status: "ongoing"})
	wantToolError(t, res, "failed to update task status: not the real thing")

	res, _, _ = ps.handleReply(ctx, nil, ReplyParams{TaskID: task, Text: "hi"})
	wantToolError(t, res, "failed to deliver reply: not the real thing")

	res, _, _ = ps.handleGetAttachment(ctx, nil, GetAttachmentParams{TaskID: task, AttachmentID: "a1"})
	wantToolError(t, res, "failed to get task: not the real thing")

	res, _, _ = ps.handlePublishEvent(ctx, nil, PublishEventParams{Name: "deployed"})
	wantToolError(t, res, "failed to publish event: not the real thing")
}

// A publish is placed in a workflow run only when its task can be found; every
// way of not finding it places it in none.
func TestCurrentWorkflowRun_NoTaskIsNoRun(t *testing.T) {
	replies := 0
	ps := permissionServer(t, &replies)
	ctx := context.Background()
	ps.sessionTasks = map[string]int64{}
	ps.listTasks = func(context.Context, ListTasksFilter) ([]model.Task, error) { return nil, nil }

	if got := ps.currentWorkflowRun(ctx, nil, ""); got != (WorkflowRunContext{}) {
		t.Errorf("nothing to go on gave run %+v", got)
	}

	ss := namedSession(t, ps, "session-run")
	if got := ps.currentWorkflowRun(ctx, callOn(ss), ""); got != (WorkflowRunContext{}) {
		t.Errorf("a session with no task gave run %+v", got)
	}

	ps.sessionTasks["session-run"] = 42
	ps.getTask = func(context.Context, int64) (model.Task, error) { return model.Task{}, errNotTheRealThing }
	if got := ps.currentWorkflowRun(ctx, callOn(ss), ""); got != (WorkflowRunContext{}) {
		t.Errorf("a task that cannot be read gave run %+v", got)
	}

	ps.getTask = func(_ context.Context, id int64) (model.Task, error) {
		return model.Task{ID: id, WorkflowID: 9, WorkflowDepth: 2}, nil
	}
	if got := ps.currentWorkflowRun(ctx, callOn(ss), ""); got != (WorkflowRunContext{WorkflowID: 9, Depth: 2}) {
		t.Errorf("the session's task gave run %+v, want workflow 9 at depth 2", got)
	}
}

// A cursor before the start reads from the start, and one past the end reads
// nothing.
func TestBuildConversationJSON_ClampsTheCursor(t *testing.T) {
	task := model.Task{Messages: []model.Message{{ID: 1, Text: "first"}, {ID: 2, Text: "second"}}}

	if got := buildConversationJSON(task, -3, 0); !strings.Contains(got, "first") || !strings.Contains(got, `"cursor":2`) {
		t.Errorf("a negative cursor read %s, want from the start", got)
	}
	if got := buildConversationJSON(task, 10, 5); strings.Contains(got, "first") || strings.Contains(got, "second") {
		t.Errorf("a cursor past the end read %s, want nothing", got)
	}
}

func TestTruncateForStorage_CutsALongInput(t *testing.T) {
	if got := truncateForStorage("abcdef", 3); got != "abc…" {
		t.Errorf("truncateForStorage = %q, want abc…", got)
	}
	if got := truncateForStorage("abc", 3); got != "abc" {
		t.Errorf("truncateForStorage = %q, want abc untouched", got)
	}
}

// With no session mapping, the task a request belongs to is the workspace's
// ongoing one, and it is remembered for the session that asked — but never
// under the empty key every sessionless caller shares.
func TestResolveTaskID_FallsBackToTheOngoingTask(t *testing.T) {
	replies := 0
	ps := permissionServer(t, &replies)
	ps.sessionTasks = map[string]int64{}

	if id, ok := ps.resolveTaskID(context.Background(), "session-x", ""); !ok || id != 42 {
		t.Fatalf("resolved %d (%v), want the ongoing task 42", id, ok)
	}
	if id, _ := sessionTask(ps, "session-x"); id != 42 {
		t.Errorf("the session was mapped to %d, want 42", id)
	}

	if id, ok := ps.resolveTaskID(context.Background(), "", ""); !ok || id != 42 {
		t.Fatalf("resolved %d (%v) with no session, want 42", id, ok)
	}
	if _, cached := sessionTask(ps, ""); cached {
		t.Error("a sessionless lookup was cached under the empty key")
	}
}

// verdictLink is a native link that says when a verdict reaches it, so a test
// can wait for one sent from a goroutine.
type verdictLink struct {
	*fakeLink
	delivered chan string
}

func (l verdictLink) DeliverVerdict(_ context.Context, requestID, behavior string) bool {
	l.delivered <- requestID + "=" + behavior
	return true
}

func awaitVerdict(t *testing.T, l verdictLink, want string) {
	t.Helper()
	select {
	case got := <-l.delivered:
		if got != want {
			t.Errorf("verdict %q, want %q", got, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("no verdict was delivered, want %q", want)
	}
}

func middlewareRequest(ss *mcp.ServerSession, p agentlink.PermissionRequest) mcp.Request {
	return &mcp.ServerRequest[*permissionRequestParams]{Session: ss, Params: &permissionRequestParams{
		LoggingMessageParams: &mcp.LoggingMessageParams{},
		body:                 p,
	}}
}

func relayThroughMiddleware(t *testing.T, ps *WorkspaceServer, req mcp.Request) {
	t.Helper()
	next := func(context.Context, string, mcp.Request) (mcp.Result, error) {
		t.Error("a permission request reached the next handler")
		return nil, nil
	}
	if _, err := ps.notificationMiddleware(next)(context.Background(), "notifications/claude/channel/permission_request", req); err != nil {
		t.Fatalf("middleware returned %v, want nil", err)
	}
}

// A request a rule allows is answered without asking anyone, from the
// session's own task: by a workspace rule, or by the task allowing every
// command.
func TestNotificationMiddleware_AnswersWhatIsAlreadyAllowed(t *testing.T) {
	for _, tc := range []struct {
		name     string
		rules    []string
		allowAll bool
	}{
		{"a workspace rule", []string{"Read"}, false},
		{"a task that allows every command", nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			replies := 0
			ps := permissionServer(t, &replies)
			ps.autoAllowedTools = tc.rules
			ps.getTask = func(_ context.Context, id int64) (model.Task, error) {
				return model.Task{ID: id, AllowAllCommands: tc.allowAll}, nil
			}
			var recorded []string
			ps.idgen = fixedIDs(t, 5)
			ps.recordToolCall = func(_ context.Context, tc model.ToolCall) (model.ToolCall, error) {
				recorded = append(recorded, tc.ToolName+":"+tc.Status)
				return tc, nil
			}
			ss := namedSession(t, ps, "acp-like-session")
			link := verdictLink{fakeLink: &fakeLink{id: "acp-like-session"}, delivered: make(chan string, 1)}
			ps.AttachAgentLink(link)

			relayThroughMiddleware(t, ps, middlewareRequest(ss, agentlink.PermissionRequest{
				RequestID: "req-auto", TaskID: monoflake.ID(42).String(), ToolName: "Read",
			}))

			awaitVerdict(t, link, "req-auto=allow")
			if replies != 0 {
				t.Errorf("the person was asked (%d replies) about an allowed request", replies)
			}
			if !slices.Equal(recorded, []string{"Read:auto_allowed"}) {
				t.Errorf("recorded %v, want the call as auto-allowed", recorded)
			}
		})
	}
}

// An allowed request with no task to file it under is still answered.
func TestNotificationMiddleware_AnswersAnAllowedRequestWithNoTask(t *testing.T) {
	replies := 0
	ps := permissionServer(t, &replies)
	ps.autoAllowedTools = []string{"Read"}
	ps.sessionTasks = map[string]int64{}
	ps.listTasks = func(context.Context, ListTasksFilter) ([]model.Task, error) { return nil, nil }
	ss := namedSession(t, ps, "session-taskless")
	link := verdictLink{fakeLink: &fakeLink{id: "session-taskless"}, delivered: make(chan string, 1)}
	ps.AttachAgentLink(link)

	relayThroughMiddleware(t, ps, middlewareRequest(ss, agentlink.PermissionRequest{RequestID: "req-free", ToolName: "Read"}))

	awaitVerdict(t, link, "req-free=allow")
}

// A request that needs a person is filed under the session's task, with the
// tool call recorded as pending, and bound to the session that asked.
func TestNotificationMiddleware_AsksAboutTheRest(t *testing.T) {
	replies := 0
	ps := permissionServer(t, &replies)
	ps.idgen = fixedIDs(t, 5)
	ps.recordToolCall = func(_ context.Context, tc model.ToolCall) (model.ToolCall, error) {
		tc.ID = 77
		return tc, nil
	}
	ss := namedSession(t, ps, "session-ask")

	relayThroughMiddleware(t, ps, middlewareRequest(ss, agentlink.PermissionRequest{
		RequestID: "req-ask", TaskID: monoflake.ID(42).String(), ToolName: "Write",
	}))

	if replies != 1 {
		t.Errorf("the person was asked %d times, want once", replies)
	}
	if got := ps.permissionRequests["req-ask"]; got != "session-ask" {
		t.Errorf("the request is bound to %q, want session-ask", got)
	}
	if got := ps.toolCallIDs["req-ask"]; got != 77 {
		t.Errorf("the request's tool call is %d, want 77", got)
	}
}

// A request already asked about, arriving on a new session, is re-bound to it
// rather than asked again.
func TestNotificationMiddleware_RebindsARequestAlreadyAsked(t *testing.T) {
	replies := 0
	ps := permissionServer(t, &replies)
	ps.permissionResponses["req-again"] = 701
	ss := namedSession(t, ps, "session-new")

	relayThroughMiddleware(t, ps, middlewareRequest(ss, agentlink.PermissionRequest{RequestID: "req-again", ToolName: "Write"}))

	if replies != 0 {
		t.Errorf("the person was asked again (%d replies)", replies)
	}
	if got := ps.permissionRequests["req-again"]; got != "session-new" {
		t.Errorf("the request is bound to %q, want session-new", got)
	}
}

// A request nothing can be filed under is not relayed to anyone.
func TestNotificationMiddleware_DropsARequestWithNoTask(t *testing.T) {
	replies := 0
	ps := permissionServer(t, &replies)
	ps.sessionTasks = map[string]int64{}
	ps.listTasks = func(context.Context, ListTasksFilter) ([]model.Task, error) { return nil, nil }
	ss := namedSession(t, ps, "session-lost")

	relayThroughMiddleware(t, ps, middlewareRequest(ss, agentlink.PermissionRequest{RequestID: "req-lost", ToolName: "Write"}))

	if replies != 0 {
		t.Errorf("a request with no task was posted (%d replies)", replies)
	}
}

// toolCallStatuses records what each tool call row was moved to.
type toolCallStatuses struct {
	mu   sync.Mutex
	rows []string
}

func (s *toolCallStatuses) update(err error) UpdateToolCallStatusFunc {
	return func(_ context.Context, id int64, status string) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.rows = append(s.rows, monoflake.ID(id).String()+"="+status)
		return err
	}
}

// A verdict whose task the caller got wrong lands on the task the request was
// filed under, and moves its tool call row to the decision — even when that
// row cannot be saved.
func TestSendVerdict_FilesUnderTheRequestsOwnTask(t *testing.T) {
	for _, tc := range []struct {
		behavior, status string
		err              error
	}{
		{"allow", "allowed", nil},
		{"deny", "denied", errNotTheRealThing},
	} {
		t.Run(tc.behavior, func(t *testing.T) {
			replies := 0
			ps := permissionServer(t, &replies)
			ss := namedSession(t, ps, "session-verdict")
			ps.getTask = func(_ context.Context, id int64) (model.Task, error) {
				if id == 999 {
					return model.Task{}, errNotTheRealThing
				}
				return model.Task{ID: id}, nil
			}
			statuses := &toolCallStatuses{}
			ps.updateToolCallStatus = statuses.update(tc.err)
			var updatedTask int64
			ps.updateMessageMetadata = func(_ context.Context, taskID, _ int64, _ any) error {
				updatedTask = taskID
				return nil
			}
			ps.permissionRequests["req-v"] = ss.ID()
			ps.requestTaskIDs["req-v"] = 42
			ps.toolCallIDs["req-v"] = 5
			ps.permissionResponses["req-v"] = 701

			if err := ps.SendPermissionVerdict(context.Background(), 999, "req-v", tc.behavior); err != nil {
				t.Fatalf("verdict failed: %v", err)
			}
			if updatedTask != 42 {
				t.Errorf("the verdict was filed under task %d, want 42", updatedTask)
			}
			if want := monoflake.ID(5).String() + "=" + tc.status; !slices.Equal(statuses.rows, []string{want}) {
				t.Errorf("tool call rows moved to %v, want %s", statuses.rows, want)
			}
		})
	}
}

// "Allow always" writes its rule once: a rule already there is not added again,
// and a new one is saved for the workspace.
func TestSendVerdict_AllowAlwaysWritesEachRuleOnce(t *testing.T) {
	replies := 0
	ps := permissionServer(t, &replies)
	ss := namedSession(t, ps, "session-always")
	ps.autoAllowedTools = []string{"Read"}
	var saved [][]string
	ps.updateAutoAllowed = func(_ context.Context, tools []string) error {
		saved = append(saved, slices.Clone(tools))
		return nil
	}

	for _, req := range []struct{ id, tool string }{{"req-read", "Read"}, {"req-grep", "Grep"}} {
		ps.permissionRequests[req.id] = ss.ID()
		ps.requestTools[req.id] = req.tool
		if err := ps.SendPermissionVerdict(context.Background(), 42, req.id, "allow_always"); err != nil {
			t.Fatalf("%s: verdict failed: %v", req.id, err)
		}
	}

	if !slices.Equal(ps.autoAllowedTools, []string{"Read", "Grep"}) {
		t.Errorf("rules are %v, want Read then Grep", ps.autoAllowedTools)
	}
	if len(saved) != 1 || !slices.Equal(saved[0], []string{"Read", "Grep"}) {
		t.Errorf("saved %v, want only the new rule's save", saved)
	}
}

// A notification is delivered only to the session it is addressed to.
func TestNotifySession_ReachesOnlyTheNamedSession(t *testing.T) {
	replies := 0
	ps := permissionServer(t, &replies)
	namedSession(t, ps, "session-a")
	namedSession(t, ps, "session-b")
	ctx := context.Background()

	for _, id := range []string{"session-a", "session-b"} {
		if !ps.notifySession(ctx, id, "notifications/claude/channel/permission", map[string]any{"request_id": "r"}) {
			t.Errorf("%s was not reached", id)
		}
	}
	if ps.notifySession(ctx, "session-gone", "notifications/claude/channel/permission", nil) {
		t.Error("a session that is not connected was reported reached")
	}
}

// A body that is not JSON is dropped without touching any request.
func TestHandleCustomNotification_DropsABodyThatIsNotJSON(t *testing.T) {
	replies := 0
	ps := permissionServer(t, &replies)

	ps.HandleCustomNotification(context.Background(), "session-1", []byte(`{"method":`))

	if len(ps.permissionRequests) != 0 || replies != 0 {
		t.Errorf("a malformed notification was acted on: %v, %d replies", ps.permissionRequests, replies)
	}
}

// A push goes to each session, noting which ones hold a valid token, and a
// session that cannot take it does not stop the rest.
func TestSendChannelNotification_EverySession(t *testing.T) {
	ps := pushServer(t)
	ctrl := gomock.NewController(t)
	tokens := mock_auth.NewMockTokenService(ctrl)
	tokens.EXPECT().ValidateToken("a-session-id-longer-than-twelve").
		Return(&auth.Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: "user-1"}}, nil).Times(2)
	ps.tokenSvc = tokens
	namedSession(t, ps, "a-session-id-longer-than-twelve")

	ps.SendChannelNotification(context.Background(), 7, "next task")

	// A cancelled context makes every write fail, which is reported per
	// session and not returned to the caller.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ps.SendChannelNotification(ctx, 7, "next task")
}

// The poller offers nothing when the pending backlog cannot be read.
func TestPollOnce_SurvivesAFailingPendingQuery(t *testing.T) {
	ps := pushServer(t)

	got := ps.pollOnce(listTasksFunc(func(_ context.Context, req entity.ListTasksRequest, _ int64) ([]model.Task, error) {
		if slices.Contains(req.Status, "notstarted") {
			return nil, errNotTheRealThing
		}
		return nil, nil
	}))

	if got != 0 {
		t.Fatalf("a failed pending query offered task %d", got)
	}
}

// plainWriter is a ResponseWriter that cannot flush, as some proxies hand out.
type plainWriter struct {
	header http.Header
	code   int
	body   bytes.Buffer
}

func (w *plainWriter) Header() http.Header         { return w.header }
func (w *plainWriter) Write(b []byte) (int, error) { return w.body.Write(b) }
func (w *plainWriter) WriteHeader(code int)        { w.code = code }

// A writer that cannot flush is still served, only buffered.
func TestHandler_ServesAWriterThatCannotFlush(t *testing.T) {
	ps := NewWorkspaceServer(
		1, 0, "user", "http://localhost",
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		eventbus.New(), nil, nil, "icon", "name", "desc", nil, nil, nil, nil,
	)
	t.Cleanup(ps.Close)
	req, err := http.NewRequest(http.MethodPost, "/mcp", strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"0"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	w := &plainWriter{header: http.Header{}}
	ps.Handler().ServeHTTP(w, req)

	if w.code != 0 && w.code != http.StatusOK {
		t.Fatalf("answered %d: %s", w.code, w.body.String())
	}
	if w.body.Len() == 0 {
		t.Error("nothing was written to a writer that cannot flush")
	}
}
