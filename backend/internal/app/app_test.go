package app

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agentrq/agentrq/backend/internal/service/auth"
	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
	"github.com/gofiber/fiber/v2"
)

const testJWTSecret = "test-secret-for-events-handler"

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
	public := t.TempDir()
	write := func(name, content string) {
		t.Helper()
		full := filepath.Join(public, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	write("assets/app.js", "console.log(1)")
	// A file named exactly like an app route. Serving this would mean the
	// static handler had been consulted after all.
	write("login", "STATIC FILE")

	app := fiber.New()
	app.Static("/", public, fiber.Static{
		Compress: false,
		Next:     func(c *fiber.Ctx) bool { return skipStaticHandler(c.Path()) },
	})
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
