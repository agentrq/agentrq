// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getsentry/sentry-go"
	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"
	zlog "github.com/rs/zerolog/log"

	handlerapi "github.com/agentrq/agentrq/backend/internal/handler/api"
	"github.com/agentrq/agentrq/backend/internal/service/config"
)

const testSentryDSN = "https://key@sentry.example.com/1"

// stagingConfig is the app's config as a staging server runs it.
type stagingConfig struct{ config.Service }

func (stagingConfig) Version() string { return "v9.8.7" }
func (stagingConfig) Env() string     { return "staging" }

// withSentry starts Sentry as New does, sending to a transport the test
// reads, and puts the global hub and logger back afterwards.
func withSentry(t *testing.T) *sentry.MockTransport {
	t.Helper()
	transport := &sentry.MockTransport{}
	oldTransport, oldClient, oldLogger := sentryTransport, sentry.CurrentHub().Client(), zlog.Logger
	t.Cleanup(func() {
		sentryTransport = oldTransport
		sentry.CurrentHub().BindClient(oldClient)
		zlog.Logger = oldLogger
	})
	sentryTransport = transport
	if err := startSentry(SentryConfig{Enabled: true, DSN: testSentryDSN}, stagingConfig{}); err != nil {
		t.Fatal(err)
	}
	return transport
}

func TestCheckSentry(t *testing.T) {
	for _, tc := range []struct {
		cfg  SentryConfig
		want string
	}{
		{SentryConfig{}, ""},
		{SentryConfig{DSN: "not a dsn"}, ""},
		{SentryConfig{Enabled: true, DSN: testSentryDSN}, ""},
		{SentryConfig{Enabled: true}, "sentry.dsn: required when sentry is enabled"},
		{SentryConfig{Enabled: true, DSN: "not a dsn"}, "sentry.dsn: "},
	} {
		err := checkSentry("sentry", tc.cfg)
		if tc.want == "" && err != nil || tc.want != "" && (err == nil || !strings.HasPrefix(err.Error(), tc.want)) {
			t.Errorf("checkSentry(%+v) = %v, want %q", tc.cfg, err, tc.want)
		}
	}
}

// Off, the web app is told nowhere; on, the DSN and the server's environment.
func TestFrontendSentry(t *testing.T) {
	if got := frontendSentry(SentryConfig{DSN: testSentryDSN}, nil); got != (handlerapi.Sentry{}) {
		t.Errorf("off: %+v", got)
	}
	want := handlerapi.Sentry{DSN: testSentryDSN, Environment: "staging"}
	if got := frontendSentry(SentryConfig{Enabled: true, DSN: testSentryDSN}, stagingConfig{}); got != want {
		t.Errorf("on: %+v, want %+v", got, want)
	}
}

// Off, nothing starts; on with no DSN, the server does not start.
func TestStartSentryOffOrRefused(t *testing.T) {
	client := sentry.CurrentHub().Client()
	if err := startSentry(SentryConfig{}, nil); err != nil || sentry.CurrentHub().Client() != client {
		t.Fatalf("off: %v, client changed %v", err, sentry.CurrentHub().Client() != client)
	}
	if err := startSentry(SentryConfig{Enabled: true}, nil); err == nil || !strings.Contains(err.Error(), "sentry.dsn") {
		t.Fatalf("no DSN: %v", err)
	}
	var cfg Config
	cfg.Auth.APIJWTSecret, cfg.Auth.MCPJWTSecret = "secret", "secret"
	cfg.ConfigSvc = bundledConfig(t)
	cfg.Sentry.Enabled = true
	if _, err := New(cfg); err == nil || !strings.Contains(err.Error(), "sentry.dsn") {
		t.Fatalf("New = %v, want a sentry.dsn error", err)
	}
}

func TestStartSentryRefusesABadDSN(t *testing.T) {
	err := startSentry(SentryConfig{Enabled: true, DSN: "not a dsn"}, stagingConfig{})
	if err == nil || !strings.HasPrefix(err.Error(), "sentry.dsn: ") {
		t.Fatalf("err = %v", err)
	}
}

// Events carry the app's version as release and ENV as environment.
func TestSentryReleaseAndEnvironment(t *testing.T) {
	withSentry(t)
	opts := sentry.CurrentHub().Client().Options()
	if opts.Release != "v9.8.7" {
		t.Errorf("release = %q", opts.Release)
	}
	if opts.Environment != "staging" {
		t.Errorf("environment = %q", opts.Environment)
	}
	if opts.SendDefaultPII {
		t.Error("sends request bodies, cookies and auth headers")
	}
}

// An error-level log line is an event, with its error but not its other
// fields; a warning is not.
func TestSentryReportsErrorLogLines(t *testing.T) {
	transport := withSentry(t)
	zlog.Warn().Msg("only a warning")
	zlog.Error().Err(errors.New("disk full")).Str("email", "a@example.com").Msg("save failed")
	zlog.Error().Msg("no error field")
	zlog.Error().Err(errors.New("bare")).Send()
	// Only this test's lines: an app an earlier test started may still be
	// logging errors from its background work as it winds down.
	ours := map[string]bool{"only a warning": true, "save failed: disk full": true, "no error field": true, "bare": true}
	var got []string
	for _, e := range transport.Events() {
		if !ours[e.Message] {
			continue
		}
		got = append(got, e.Message)
		if e.Level != sentry.LevelError {
			t.Errorf("%q level = %s", e.Message, e.Level)
		}
	}
	if want := "save failed: disk full|no error field|bare"; strings.Join(got, "|") != want {
		t.Fatalf("events = %q, want %q", strings.Join(got, "|"), want)
	}
}

func TestSentryLogsLevels(t *testing.T) {
	transport := withSentry(t)
	w := sentryLogs{}
	for _, l := range []zerolog.Level{zerolog.NoLevel, zerolog.Disabled, zerolog.TraceLevel, zerolog.InfoLevel} {
		if n, err := w.WriteLevel(l, []byte(`{"message":"x"}`)); n != 15 || err != nil {
			t.Fatalf("WriteLevel(%s) = %d, %v", l, n, err)
		}
	}
	if n, err := w.Write([]byte("abc")); n != 3 || err != nil {
		t.Fatalf("Write = %d, %v", n, err)
	}
	w.WriteLevel(zerolog.FatalLevel, []byte(`{"message":"dying"}`))
	w.WriteLevel(zerolog.PanicLevel, []byte(`not json`))
	events := transport.Events()
	if len(events) != 2 || events[0].Message != "dying" || events[0].Level != sentry.LevelFatal || events[1].Level != sentry.LevelFatal {
		t.Fatalf("events = %+v", events)
	}
}

// A panic on a stdlib route is reported and still reaches net/http; an
// aborted handler is not reported.
func TestReportPanics(t *testing.T) {
	transport := withSentry(t)
	h := reportPanics(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/boom":
			panic("boom")
		case "/abort":
			panic(http.ErrAbortHandler)
		}
		w.WriteHeader(http.StatusTeapot)
	}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/fine", nil))
	if rr.Code != http.StatusTeapot {
		t.Fatalf("status = %d", rr.Code)
	}
	for _, path := range []string{"/boom", "/abort"} {
		func() {
			defer func() {
				if v := recover(); v == nil {
					t.Errorf("%s: the panic did not reach the server", path)
				}
			}()
			h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
		}()
	}
	events := transport.Events()
	if len(events) != 1 || events[0].Message != "boom" || events[0].Request == nil || !strings.HasSuffix(events[0].Request.URL, "/boom") {
		t.Fatalf("events = %+v", events)
	}
}

// A Fiber handler's panic is reported once and answered 500.
func TestSentryFiber(t *testing.T) {
	transport := withSentry(t)
	app := fiber.New()
	app.Use(sentryFiber()...)
	app.Get("/boom", func(fiber.Ctx) error { panic("fiber boom") })
	res, err := app.Test(httptest.NewRequest(http.MethodGet, "/boom", nil))
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d", res.StatusCode)
	}
	var panics int
	for _, e := range transport.Events() {
		if e.Message == "fiber boom" {
			panics++
		}
	}
	if panics != 1 {
		t.Fatalf("panic events = %d, want 1: %+v", panics, transport.Events())
	}
}

// With Sentry on, the whole app starts, serves and shuts down.
func TestTheAppStartsWithSentry(t *testing.T) {
	transport := &sentry.MockTransport{}
	oldTransport, oldClient, oldLogger := sentryTransport, sentry.CurrentHub().Client(), zlog.Logger
	t.Cleanup(func() {
		sentryTransport = oldTransport
		sentry.CurrentHub().BindClient(oldClient)
		zlog.Logger = oldLogger
	})
	sentryTransport = transport
	t.Setenv("AGENTRQ_SENTRY_ENABLED", "true")
	t.Setenv("AGENTRQ_SENTRY_DSN", testSentryDSN)
	a := newTestApp(t)
	if sentry.CurrentHub().Client() == nil {
		t.Fatal("no Sentry client")
	}
	rr := httptest.NewRecorder()
	a.server.(http.Handler).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "http://localhost/api/acp/v1", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("GET /api/acp/v1 = %d", rr.Code)
	}
}
