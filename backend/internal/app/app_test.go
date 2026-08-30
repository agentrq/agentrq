package app

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/agentrq/agentrq/backend/internal/service/auth"
	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
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
