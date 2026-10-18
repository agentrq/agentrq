// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/service/auth"
	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
	mock_repository "github.com/agentrq/agentrq/backend/internal/service/mocks/repository"
	"github.com/gofiber/fiber/v2"
	"github.com/golang/mock/gomock"
)

const testJWTSecret = "test-secret-for-events-handler"

func TestNewRequiresJWTSecretBeforeInitialization(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("expected an error when the JWT secret is missing")
	}
}

func newTestTokenService(t *testing.T) auth.TokenService {
	t.Helper()
	return auth.NewTokenService(auth.TokenConfig{JWTSecret: testJWTSecret})
}

func newTestToken(t *testing.T, svc auth.TokenService, userID string) string {
	t.Helper()
	token, err := svc.CreateToken(userID, userID+"@example.com", "Test User", "")
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}
	return token
}

// connect opens an SSE request against srv and returns the response once the
// header block has been received. The caller cancels ctx to end the stream.
//
// The crud.Controller is nil throughout this file: it is only consulted for a
// workspace-scoped stream that gets past the ID check, and none of these tests
// reach that point.
func connect(t *testing.T, srv *httptest.Server, path, token string, timeout time.Duration) (*http.Response, context.CancelFunc, time.Duration) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+path, nil)
	if err != nil {
		cancel()
		t.Fatalf("NewRequest: %v", err)
	}
	if token != "" {
		req.AddCookie(&http.Cookie{Name: "at", Value: token})
	}

	start := time.Now()
	resp, err := srv.Client().Do(req)
	elapsed := time.Since(start)
	if err != nil {
		cancel()
		t.Fatalf("request did not return within %s: %v", timeout, err)
	}
	return resp, cancel, elapsed
}

// The regression this file exists for.
//
// The handler set its SSE headers but never flushed them, so net/http held the
// whole response in its buffer until the first published event or the 30-second
// keepalive tick. A client therefore saw nothing at all on connect, and
// EventSource — which only fires `onopen` once headers arrive — reported a
// perfectly healthy stream as disconnected for up to half a minute.
//
// A 3-second deadline is well past what a local connect needs and far short of
// the 30-second tick, so this fails outright against the unflushed handler.
func TestEventsHandlerSendsHeadersOnConnect(t *testing.T) {
	tokenSvc := newTestTokenService(t)
	srv := httptest.NewServer(eventsHandler(nil, eventbus.New(), tokenSvc))
	defer srv.Close()

	resp, cancel, elapsed := connect(t, srv, "/", newTestToken(t, tokenSvc, "user-1"), 3*time.Second)
	defer cancel()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if got := resp.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Errorf("Content-Type = %q, want %q", got, "text/event-stream")
	}
	if got := resp.Header.Get("Cache-Control"); got != "no-cache" {
		t.Errorf("Cache-Control = %q, want %q", got, "no-cache")
	}
	if elapsed > 2*time.Second {
		t.Errorf("headers took %s to arrive; they should be flushed on connect", elapsed)
	}
}

func TestEventsHandlerStreamsPublishedEvents(t *testing.T) {
	tokenSvc := newTestTokenService(t)
	bus := eventbus.New()
	srv := httptest.NewServer(eventsHandler(nil, bus, tokenSvc))
	defer srv.Close()

	resp, cancel, _ := connect(t, srv, "/", newTestToken(t, tokenSvc, "user-1"), 3*time.Second)
	defer cancel()
	defer resp.Body.Close()

	// Safe to publish now: the handler subscribes before it flushes, so the
	// returned headers mean the subscription is already registered.
	bus.Publish(0, "user-1", eventbus.Event{Type: "task.created", Payload: map[string]string{"id": "abc"}})

	line, err := bufio.NewReader(resp.Body).ReadString('\n')
	if err != nil {
		t.Fatalf("reading event: %v", err)
	}
	if !strings.HasPrefix(line, "data: ") {
		t.Errorf("line = %q, want an SSE data line", line)
	}
	if !strings.Contains(line, "task.created") {
		t.Errorf("line = %q, want it to carry the published event type", line)
	}
}

func TestEventsHandlerDoesNotLeakEventsAcrossUsers(t *testing.T) {
	tokenSvc := newTestTokenService(t)
	bus := eventbus.New()
	srv := httptest.NewServer(eventsHandler(nil, bus, tokenSvc))
	defer srv.Close()

	resp, cancel, _ := connect(t, srv, "/", newTestToken(t, tokenSvc, "user-1"), 3*time.Second)
	defer cancel()
	defer resp.Body.Close()

	bus.Publish(0, "someone-else", eventbus.Event{Type: "task.created"})

	// Nothing addressed to user-1 was published, so the stream must stay silent
	// rather than deliver another user's event. The read runs in a goroutine
	// because the response body has no read deadline to set; cancelling the
	// request in the deferred cancel unblocks it.
	received := make(chan string, 1)
	go func() {
		buf := make([]byte, 64)
		if n, err := resp.Body.Read(buf); err == nil && n > 0 {
			received <- string(buf[:n])
		}
	}()

	select {
	case data := <-received:
		t.Errorf("received %q, want nothing for a different user", data)
	case <-time.After(300 * time.Millisecond):
		// Silence is the pass condition.
	}
}

func TestEventsHandlerRejectsUnauthenticated(t *testing.T) {
	tokenSvc := newTestTokenService(t)
	srv := httptest.NewServer(eventsHandler(nil, eventbus.New(), tokenSvc))
	defer srv.Close()

	tests := []struct {
		name   string
		cookie string
	}{
		{name: "no cookie", cookie: ""},
		{name: "malformed token", cookie: "not-a-jwt"},
		{name: "token signed with another secret", cookie: newTestToken(t,
			auth.NewTokenService(auth.TokenConfig{JWTSecret: "a-different-secret"}), "user-1")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, cancel, _ := connect(t, srv, "/", tt.cookie, 3*time.Second)
			defer cancel()
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusUnauthorized {
				t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
			}
		})
	}
}

func TestEventsHandlerRejectsUnparseableWorkspaceID(t *testing.T) {
	tokenSvc := newTestTokenService(t)

	// A ServeMux is needed here so the {id} wildcard populates PathValue; the
	// bare handler used elsewhere in this file always sees an empty id, which is
	// the global-stream case.
	mux := http.NewServeMux()
	mux.Handle("/api/v1/workspaces/{id}/events", eventsHandler(nil, eventbus.New(), tokenSvc))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// "0" is valid base62 but decodes to the zero ID, which is never a real
	// workspace — the check that rejects it runs before any repository access,
	// which is why a nil controller is safe here.
	resp, cancel, _ := connect(t, srv, "/api/v1/workspaces/0/events", newTestToken(t, tokenSvc, "user-1"), 3*time.Second)
	defer cancel()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusUnprocessableEntity)
	}
}

// A page is not a file, and the static handler must not be asked to find one.
// It opens the path to find out, and logs every miss — which is why navigating
// to /login wrote an error line for a request that then succeeded.
func TestSkipStaticHandler(t *testing.T) {
	cases := []struct {
		path string
		skip bool
		why  string
	}{
		{"/login", true, "an app route names no file"},
		{"/workspaces/0i7trFAL0PR", true, "nor does a nested one"},
		{"/", true, "the root is served by the fallback"},
		{"/index.html", true, "and so is the shell, so its base path can be injected"},
		{"/api/v1/workspaces", true, "the API is not static content"},
		{"/mcp", true, "neither is the MCP endpoint"},
		{"/assets/index-a1b2c3.js", false, "a hashed asset is a real file"},
		{"/favicon.ico", false, "so is the favicon"},
		{"/robots.txt", false, "and robots.txt"},
		{"/manifest.webmanifest", false, "and the manifest"},
		{"/sw.js", false, "and the service worker"},
		{"/missing.js", false, "a missing asset is still worth reporting"},
		{"/.well-known/oauth-protected-resource", true, "a dot in a directory is not an extension"},
	}

	for _, tc := range cases {
		if got := skipStaticHandler(tc.path); got != tc.skip {
			t.Errorf("skipStaticHandler(%q) = %v, want %v: %s", tc.path, got, tc.skip, tc.why)
		}
	}
}

func TestNamesAFile(t *testing.T) {
	cases := map[string]bool{
		"/assets/app.js":   true,
		"/favicon.ico":     true,
		"/login":           false,
		"/":                false,
		"/a.b/c":           false, // the dot is in a directory, not the file
		"/a.b/c.d":         true,
		"":                 false,
		"/trailing/slash/": false,
	}
	for path, want := range cases {
		if got := namesAFile(path); got != want {
			t.Errorf("namesAFile(%q) = %v, want %v", path, got, want)
		}
	}
}

// The static handler really is bypassed for an app route, not merely allowed to
// miss: a file sitting at that exact path must still lose to the app.
func TestStaticHandlerIsBypassedForAppRoutes(t *testing.T) {
	public := fstest.MapFS{
		"assets/app.js": {Data: []byte("console.log(1)")},
		// A file named exactly like an app route. Serving this would mean the
		// static handler had been consulted after all.
		"login": {Data: []byte("STATIC FILE")},
	}

	app := fiber.New()
	app.Use("/", staticHandler(public))
	app.Get("/*", func(c *fiber.Ctx) error { return c.SendString("APP SHELL") })

	cases := []struct {
		path string
		body string
	}{
		{"/login", "APP SHELL"},
		{"/assets/app.js", "console.log(1)"},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("GET %s: %v", tc.path, err)
		}
		defer resp.Body.Close()
		buf := new(strings.Builder)
		if _, err := io.Copy(buf, resp.Body); err != nil {
			t.Fatalf("read body: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Errorf("GET %s: status %d, want 200", tc.path, resp.StatusCode)
		}
		if buf.String() != tc.body {
			t.Errorf("GET %s: body %q, want %q", tc.path, buf.String(), tc.body)
		}
	}
}

// A file is sent as the compressed copy the build carries beside it, the best
// one the client accepts, and as itself when there is none it accepts.
func TestStaticHandlerServesPrecompressedCopies(t *testing.T) {
	app := fiber.New()
	app.Use("/", staticHandler(fstest.MapFS{
		"assets/app.js":    {Data: []byte("PLAIN")},
		"assets/app.js.br": {Data: []byte("BROTLI")},
		"assets/app.js.gz": {Data: []byte("GZIP")},
		"assets/solo.css":  {Data: []byte("SOLO")},
	}))

	for _, tc := range []struct {
		path, accept, body, encoding, contentType string
	}{
		{"/assets/app.js", "gzip, deflate, br", "BROTLI", "br", "text/javascript"},
		{"/assets/app.js", "gzip", "GZIP", "gzip", "text/javascript"},
		{"/assets/app.js", "br;q=0, gzip", "GZIP", "gzip", "text/javascript"},
		{"/assets/app.js", "identity", "PLAIN", "", "text/javascript"},
		{"/assets/app.js", "", "PLAIN", "", "text/javascript"},
		{"/assets/solo.css", "br, gzip", "SOLO", "", "text/css"},
	} {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		if tc.accept != "" {
			req.Header.Set("Accept-Encoding", tc.accept)
		}
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("GET %s: %v", tc.path, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if string(body) != tc.body || resp.Header.Get("Content-Encoding") != tc.encoding ||
			!strings.HasPrefix(resp.Header.Get("Content-Type"), tc.contentType) {
			t.Errorf("GET %s with %q: %q, Content-Encoding %q, Content-Type %q; want %q, %q, %s",
				tc.path, tc.accept, body, resp.Header.Get("Content-Encoding"), resp.Header.Get("Content-Type"),
				tc.body, tc.encoding, tc.contentType)
		}
		if tc.encoding != "" && resp.Header.Get("Vary") != "Accept-Encoding" {
			t.Errorf("GET %s with %q: Vary %q, want Accept-Encoding", tc.path, tc.accept, resp.Header.Get("Vary"))
		}
	}
}

// The whole app serves the web build it is given, the index.html behind every
// app route included, without reading the working directory.
func TestTheAppServesItsWebBuild(t *testing.T) {
	a := newTestApp(t, "true")
	for _, tc := range []struct {
		path string
		code int
		body string
	}{
		{"/", http.StatusOK, "window.__AGENTRQ_BASE_PATH__"},
		{"/workspaces/1/tasks", http.StatusOK, `<script src="/assets/app.js">`},
		{"/assets/app.js", http.StatusOK, "console.log(1)"},
		{"/assets/missing.js", http.StatusNotFound, "Not Found"},
	} {
		req := httptest.NewRequest(http.MethodGet, "http://localhost"+tc.path, nil)
		rr := httptest.NewRecorder()
		a.server.(http.Handler).ServeHTTP(rr, req)
		if rr.Code != tc.code || !strings.Contains(rr.Body.String(), tc.body) {
			t.Errorf("GET %s answered %d %.80q, want %d with %q", tc.path, rr.Code, rr.Body.String(), tc.code, tc.body)
		}
	}
}

// The SSE payload carries the task's messages but never its tool calls: those
// arrive one at a time, and reloading them all on every event grew as N² (#921).
// The strict mock fails if the tool calls are read.
func TestTaskEventPayloadCarriesMessagesNotToolCalls(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	repo := mock_repository.NewMockRepository(ctrl)
	ctx := context.Background()

	repo.EXPECT().SystemGetTask(ctx, int64(7)).Return(model.Task{ID: 7, Title: "Ship it"}, nil)
	repo.EXPECT().ListMessages(ctx, int64(7)).Return([]model.Message{{ID: 1}, {ID: 2}}, nil)

	task, err := taskEventPayload(ctx, repo, 7)
	if err != nil {
		t.Fatalf("taskEventPayload: %v", err)
	}
	if len(task.Messages) != 2 {
		t.Errorf("expected 2 messages, got %d", len(task.Messages))
	}
	if task.ToolCalls != nil {
		t.Errorf("expected no tool calls, got %d", len(task.ToolCalls))
	}
}

// A relation that cannot be loaded is worth less than the event itself: the
// status change still reaches the client.
func TestTaskEventPayloadStillPublishesWhenARelationFails(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	repo := mock_repository.NewMockRepository(ctrl)
	ctx := context.Background()

	repo.EXPECT().SystemGetTask(ctx, int64(7)).Return(model.Task{ID: 7, Status: "completed"}, nil)
	repo.EXPECT().ListMessages(ctx, int64(7)).Return(nil, errors.New("messages unavailable"))

	task, err := taskEventPayload(ctx, repo, 7)
	if err != nil {
		t.Fatalf("taskEventPayload: %v", err)
	}
	if task.Status != "completed" {
		t.Errorf("expected the task itself, got status %q", task.Status)
	}
	if task.Messages != nil {
		t.Errorf("expected the failed messages to be left empty")
	}
}

// No task, no event: a payload for a task that cannot be read would be worse
// than silence, since the client would replace what it has with it.
func TestTaskEventPayloadFailsWhenTheTaskCannotBeRead(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	repo := mock_repository.NewMockRepository(ctrl)
	ctx := context.Background()

	repo.EXPECT().SystemGetTask(ctx, int64(7)).Return(model.Task{}, errors.New("task not found"))

	if _, err := taskEventPayload(ctx, repo, 7); err == nil {
		t.Fatal("expected an error when the task cannot be read")
	}
}

// A task an agent creates over the workspace MCP server skips the CRUD
// controller, so it must get the self-learning-loop note here or not at all —
// and from the workspace as it is now, not as it was when the server started.
func TestMCPTaskDefaultsReadTheWorkspaceFresh(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	repo := mock_repository.NewMockRepository(ctrl)
	ctx := context.Background()

	repo.EXPECT().SystemGetWorkspace(ctx, int64(5)).Return(model.Workspace{
		ID: 5, SelfLearningLoopNote: "Save what you learned.", AllowAllCommands: true, ClearContextDefault: true,
	}, nil)

	task := mcpTaskDefaults(ctx, repo, 5, model.Workspace{ID: 5}, model.Task{Assignee: "agent", Body: "Do it."})
	if task.Body != "Do it.\n\nSave what you learned." {
		t.Errorf("expected the note appended, got %q", task.Body)
	}
	if !task.AllowAllCommands || !task.ClearContext {
		t.Errorf("expected the workspace's current defaults, got allowAll=%v clearContext=%v", task.AllowAllCommands, task.ClearContext)
	}
}

// The note is instructions for an agent; a task for the human does not get it.
func TestMCPTaskDefaultsLeaveAHumanTaskAlone(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	repo := mock_repository.NewMockRepository(ctrl)
	ctx := context.Background()

	repo.EXPECT().SystemGetWorkspace(ctx, int64(5)).Return(model.Workspace{ID: 5, SelfLearningLoopNote: "Save what you learned."}, nil)

	task := mcpTaskDefaults(ctx, repo, 5, model.Workspace{ID: 5}, model.Task{Assignee: "human", Body: "Approve it."})
	if task.Body != "Approve it." {
		t.Errorf("expected the body unchanged, got %q", task.Body)
	}
}

// A failed read is no reason to refuse the task: the copy read when the server
// started is used instead, and an explicit clean-context request is kept.
func TestMCPTaskDefaultsFallBackWhenTheWorkspaceCannotBeRead(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	repo := mock_repository.NewMockRepository(ctrl)
	ctx := context.Background()

	repo.EXPECT().SystemGetWorkspace(ctx, int64(5)).Return(model.Workspace{}, errors.New("database unavailable"))

	task := mcpTaskDefaults(ctx, repo, 5, model.Workspace{ID: 5, SelfLearningLoopNote: "Note."}, model.Task{Assignee: "agent", Body: "Do it.", ClearContext: true})
	if task.Body != "Do it.\n\nNote." {
		t.Errorf("expected the startup copy's note, got %q", task.Body)
	}
	if !task.ClearContext {
		t.Errorf("expected an explicit clean-context request to be kept")
	}
}
