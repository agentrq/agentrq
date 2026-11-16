// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
)

// fakeSentry stands in for a Sentry project, recording what reaches it.
type fakeSentry struct {
	path, contentType, body string
}

func newFakeSentry(t *testing.T, status int, header http.Header) (*fakeSentry, *httptest.Server) {
	t.Helper()
	got := &fakeSentry{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got.path, got.contentType, got.body = r.URL.Path, r.Header.Get("Content-Type"), string(b)
		for k, v := range header {
			w.Header()[k] = v
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return got, srv
}

// sentryApp serves the API's public Sentry routes for the DSN given.
func sentryApp(dsn string) (*fiber.App, *handler) {
	app := fiber.New()
	h := &handler{router: app.Group("/api/v1"), sentry: newSentryRelay(Sentry{DSN: dsn, Environment: "staging"})}
	h.registerSentryRoutes()
	return app, h
}

func envelopeFor(dsn string) string {
	return `{"dsn":"` + dsn + `","sent_at":"2026-11-16T07:00:00Z"}` + "\n" + `{"type":"event"}` + "\n" + `{"message":"boom"}`
}

func postEnvelope(t *testing.T, app *fiber.App, body string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sentry", strings.NewReader(body))
	req.Header.Set("Content-Type", "text/plain;charset=UTF-8")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestNewSentryRelayIsOffWithoutAValidDSN(t *testing.T) {
	for _, dsn := range []string{"", "not a dsn"} {
		if r := newSentryRelay(Sentry{DSN: dsn}); r.cfg != nil || r.dsn != nil {
			t.Errorf("%q: relay on", dsn)
		}
	}
}

// Off, there is no relay, and the read answers null.
func TestSentryOff(t *testing.T) {
	app, _ := sentryApp("")
	if resp := postEnvelope(t, app, envelopeFor("https://key@sentry.example.com/1")); resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST /sentry = %d, want 405", resp.StatusCode)
	}
	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/api/v1/sentry", nil))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || string(body) != "null" {
		t.Errorf("GET /sentry = %d %s, want 200 null", resp.StatusCode, body)
	}
}

// On, the web app is told the DSN and the environment.
func TestSentryConfig(t *testing.T) {
	app, _ := sentryApp("https://key@sentry.example.com/1")
	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/api/v1/sentry", nil))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	if want := `{"dsn":"https://key@sentry.example.com/1","environment":"staging"}`; resp.StatusCode != http.StatusOK || string(body) != want {
		t.Errorf("GET /sentry = %d %s, want %s", resp.StatusCode, body, want)
	}
}

// An envelope for the project goes on to its envelope endpoint, and Sentry's
// status and rate-limit headers come back.
func TestSentryRelaysTheProjectsEnvelope(t *testing.T) {
	got, srv := newFakeSentry(t, http.StatusTooManyRequests, http.Header{
		"Retry-After":          {"60"},
		"X-Sentry-Rate-Limits": {"60:error:organization"},
	})
	dsn := strings.Replace(srv.URL, "http://", "http://key@", 1) + "/1"
	app, _ := sentryApp(dsn)
	body := envelopeFor(dsn)

	resp := postEnvelope(t, app, body)
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", resp.StatusCode)
	}
	if resp.Header.Get("Retry-After") != "60" || resp.Header.Get("X-Sentry-Rate-Limits") != "60:error:organization" {
		t.Errorf("headers = %v", resp.Header)
	}
	if got.path != "/api/1/envelope/" || got.contentType != "application/x-sentry-envelope" || got.body != body {
		t.Errorf("Sentry got %+v", got)
	}
}

// Anything but an envelope for the configured project is refused, so the
// route relays to no one else's.
func TestSentryRefusesOtherEnvelopes(t *testing.T) {
	got, srv := newFakeSentry(t, http.StatusOK, nil)
	dsn := strings.Replace(srv.URL, "http://", "http://key@", 1) + "/1"
	app, _ := sentryApp(dsn)
	for name, body := range map[string]string{
		"empty":         "",
		"not json":      "hello\n{}",
		"no dsn":        `{"sent_at":"x"}` + "\n{}",
		"bad dsn":       envelopeFor("not a dsn"),
		"other key":     envelopeFor(strings.Replace(dsn, "key@", "other@", 1)),
		"other project": envelopeFor(strings.TrimSuffix(dsn, "/1") + "/2"),
		"other host":    envelopeFor("https://key@sentry.example.com/1"),
	} {
		if resp := postEnvelope(t, app, body); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", name, resp.StatusCode)
		}
	}
	if got.path != "" {
		t.Errorf("Sentry got %+v", got)
	}
}

// Sentry out of reach is a 502.
func TestSentryUnreachable(t *testing.T) {
	_, srv := newFakeSentry(t, http.StatusOK, nil)
	dsn := strings.Replace(srv.URL, "http://", "http://key@", 1) + "/1"
	srv.Close()
	app, _ := sentryApp(dsn)
	if resp := postEnvelope(t, app, envelopeFor(dsn)); resp.StatusCode != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", resp.StatusCode)
	}
}
