// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package coremcp

import (
	"context"
	"fmt"
	"sync"

	mcpevent "github.com/agentrq/agentrq/backend/internal/controller/mcp"
	"github.com/agentrq/agentrq/backend/internal/service/auth"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	zlog "github.com/rs/zerolog/log"
)

// A supervisor hears of a change to a task it follows through
// subscriptions/listen, which the SDK serves on this stateless server: the
// acknowledgement first, the listen's id on every notice, and a complete
// result when the listen's context ends. What is added here is who may listen
// to what, the changes themselves, and ending every listen on shutdown.

// maxListenURIs is the most task resources one listen may follow.
const maxListenURIs = 100

// taskChangeQueue is how many changes wait for the notifier before more are
// dropped, so a slow listener never holds up the code that changed a task.
const taskChangeQueue = 256

// TaskListeners tells the open listens of this process which task resources
// changed.
type TaskListeners struct {
	mu     sync.Mutex
	server *mcp.Server
	// held counts the open listens following each URI.
	held map[string]int

	changes chan taskChange
	// closing ends every listen, each with a complete result.
	closing context.Context
	close   context.CancelFunc
}

// taskChange is a task that changed: its workspace, and the workspace that
// delegated it, if one did.
type taskChange struct {
	workspaceID, taskID, delegatedByWorkspaceID int64
}

// NewTaskListeners makes the listeners of a CoreMCP server that does not
// exist yet, so a change can be told to it from wherever it is made.
func NewTaskListeners() *TaskListeners {
	closing, closeFn := context.WithCancel(context.Background())
	return &TaskListeners{
		held:    map[string]int{},
		changes: make(chan taskChange, taskChangeQueue),
		closing: closing,
		close:   closeFn,
	}
}

// serve starts telling srv's listens of changes, until Close.
func (l *TaskListeners) serve(srv *mcp.Server) {
	l.mu.Lock()
	l.server = srv
	l.mu.Unlock()
	go l.run()
}

// Listening reports whether any listen is open, so a change needs telling.
func (l *TaskListeners) Listening() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.held) > 0
}

// TaskChanged tells the listens following a task, its workspace, or the
// workspace that delegated it (0 for none) that the task changed: its status,
// a message, or a question its agent asks. It never waits: when the queue is
// full, the change is dropped.
func (l *TaskListeners) TaskChanged(workspaceID, taskID, delegatedByWorkspaceID int64) {
	select {
	case l.changes <- taskChange{workspaceID: workspaceID, taskID: taskID, delegatedByWorkspaceID: delegatedByWorkspaceID}:
	default:
		zlog.Warn().Int64("task_id", taskID).Msg("[coremcp] task change dropped: the listeners' queue is full")
	}
}

// Close ends every open listen with a complete result, and stops telling them
// of changes. Call it before the HTTP server drains, which a listen would
// otherwise hold open until its deadline.
func (l *TaskListeners) Close() {
	l.close()
}

func (l *TaskListeners) run() {
	for {
		select {
		case <-l.closing.Done():
			return
		case c := <-l.changes:
			l.notify(c)
		}
	}
}

// notify tells the listens following the URIs c changed. The SDK sends to
// one listen at a time, each within its own deadline.
func (l *TaskListeners) notify(c taskChange) {
	uris := []string{taskURI(c.workspaceID, c.taskID), workspaceTasksURI(c.workspaceID)}
	if c.delegatedByWorkspaceID != 0 {
		uris = append(uris, delegatedTasksURI(c.delegatedByWorkspaceID))
	}
	l.mu.Lock()
	var followed []string
	for _, uri := range uris {
		if l.held[uri] > 0 {
			followed = append(followed, uri)
		}
	}
	srv := l.server
	l.mu.Unlock()
	for _, uri := range followed {
		_ = srv.ResourceUpdated(context.Background(), &mcp.ResourceUpdatedNotificationParams{URI: uri})
	}
}

func (l *TaskListeners) hold(uris []string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, uri := range uris {
		l.held[uri]++
	}
}

func (l *TaskListeners) release(uris []string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, uri := range uris {
		if l.held[uri]--; l.held[uri] == 0 {
			delete(l.held, uri)
		}
	}
}

// errLegacySubscribe turns down an older revision's resources/subscribe: on
// a stateless server it has no stream to deliver on, so it would subscribe to
// nothing. The 2026-07-28 revision has none, and the SDK says so.
var errLegacySubscribe = &jsonrpc.Error{Code: jsonrpc.CodeMethodNotFound, Message: "method not found"}

// subscriptionsMiddleware lets a listen follow only the task resources of the
// caller's own workspaces, and ends it when the listeners close.
func (s *WorkspaceServer) subscriptionsMiddleware(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		switch method {
		case "resources/subscribe", "resources/unsubscribe":
			return nil, errLegacySubscribe
		case "subscriptions/listen":
			return s.listen(ctx, method, req, next)
		}
		return next(ctx, method, req)
	}
}

func (s *WorkspaceServer) listen(ctx context.Context, method string, req mcp.Request, next mcp.MethodHandler) (mcp.Result, error) {
	s.emitTelemetry(ctx, mcpevent.ActionMCPMethodCall, "subscriptions/listen", 0)
	var kept []string
	if p, _ := req.GetParams().(*mcp.SubscriptionsListenParams); p != nil && p.Notifications != nil {
		uris := p.Notifications.ResourceSubscriptions
		if len(uris) > maxListenURIs {
			return nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: fmt.Sprintf("a listen follows at most %d resources", maxListenURIs)}
		}
		kept = s.ownTaskURIs(ctx, uris)
		p.Notifications.ResourceSubscriptions = kept
	}
	s.listeners.hold(kept)
	defer s.listeners.release(kept)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer context.AfterFunc(s.listeners.closing, cancel)()
	// A listen lasts no longer than the token that opened it: the client opens
	// a new one with a fresh token, and is authorized again.
	if claims, _ := ctx.Value(auth.CtxKeyMCPClaims).(*auth.Claims); claims != nil && claims.ExpiresAt != nil {
		var stop context.CancelFunc
		ctx, stop = context.WithDeadline(ctx, claims.ExpiresAt.Time)
		defer stop()
	}
	return next(ctx, method, req)
}

// ownTaskURIs keeps the task resource URIs, each once, of workspaces the
// caller owns. The acknowledgement then shows the client what was kept.
func (s *WorkspaceServer) ownTaskURIs(ctx context.Context, uris []string) []string {
	userID := getUserID(ctx)
	owned := map[int64]bool{}
	seen := map[string]bool{}
	kept := []string{}
	for _, uri := range uris {
		ref := parseTaskURI(uri)
		if ref.kind == taskURIInvalid || seen[uri] {
			continue
		}
		ok, checked := owned[ref.workspaceID]
		if !checked {
			ok, _ = s.crud.CheckWorkspaceAccess(ctx, ref.workspaceID, userID)
			owned[ref.workspaceID] = ok
		}
		if ok {
			seen[uri] = true
			kept = append(kept, uri)
		}
	}
	return kept
}

// subscribeNothing is the SDK's subscribe hook. The SDK advertises
// subscriptions only when it is set; who may follow what is decided in
// subscriptionsMiddleware, before the SDK sees the listen.
func subscribeNothing(context.Context, *mcp.SubscribeRequest) error { return nil }

func unsubscribeNothing(context.Context, *mcp.UnsubscribeRequest) error { return nil }
