// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package app

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/service/config"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"

	"github.com/gofiber/fiber/v3"
)

const testMetricsToken = "0123456789abcdef0123456789abcdef"

func TestCheckPrometheus(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  PrometheusConfig
		ok   bool
	}{
		{"off", PrometheusConfig{}, true},
		{"off with a short token", PrometheusConfig{Token: "short"}, true},
		{"on with a 32-character token", PrometheusConfig{Enabled: true, Token: testMetricsToken}, true},
		{"on without a token", PrometheusConfig{Enabled: true}, false},
		{"on with a long token", PrometheusConfig{Enabled: true, Token: testMetricsToken + "x"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := checkPrometheus(tc.cfg)
			if (err == nil) != tc.ok {
				t.Fatalf("err = %v, want ok %v", err, tc.ok)
			}
			if err != nil && err.Error() != "prometheus.token: must be 32 characters when prometheus is enabled" {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

// metricsApp is a Fiber app with the metrics in front of one route.
func metricsApp(token string) *fiber.App {
	app := fiber.New()
	reg, registerer := metricsRegistry("v1.2.3")
	app.Use(prometheusFiber(PrometheusConfig{Enabled: true, Token: token}, reg, registerer)...)
	app.Get("/api/v1/ping", func(c fiber.Ctx) error { return c.SendString("pong") })
	return app
}

func get(t *testing.T, app *fiber.App, target string) (int, string) {
	t.Helper()
	res, err := app.Test(httptest.NewRequest(fiber.MethodGet, target, nil))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, string(body)
}

func TestMetricsNeedTheToken(t *testing.T) {
	app := metricsApp(testMetricsToken)
	for _, target := range []string{
		"/metrics",
		"/metrics/",
		"/metrics?prometheus_token=",
		"/metrics?prometheus_token=" + strings.ToUpper(testMetricsToken),
		"/metrics?prometheus_token=" + testMetricsToken + "x",
		"/metrics?token=" + testMetricsToken,
	} {
		if status, body := get(t, app, target); status != fiber.StatusNotFound || body != "Not Found" {
			t.Errorf("%s = %d %q, want 404", target, status, body)
		}
	}
}

func TestMetricsCountRequestsAndAnswerAScrapeWithTheToken(t *testing.T) {
	app := metricsApp(testMetricsToken)
	if status, body := get(t, app, "/api/v1/ping"); status != fiber.StatusOK || body != "pong" {
		t.Fatalf("ping = %d %q", status, body)
	}
	for _, target := range []string{"/metrics?prometheus_token=" + testMetricsToken, "/metrics/?prometheus_token=" + testMetricsToken} {
		status, body := get(t, app, target)
		if status != fiber.StatusOK {
			t.Fatalf("%s = %d %q", target, status, body)
		}
		want := `http_requests_total{app_version="v1.2.3",method="GET",path="/api/v1/ping",service="agentrq",status_code="200"} 1`
		if !strings.Contains(body, want) {
			t.Fatalf("%s has no %s:\n%s", target, want, body)
		}
	}
}

// A token of the wrong length never opens the endpoint, even when it is sent.
func TestMetricsRefuseAMisconfiguredToken(t *testing.T) {
	app := metricsApp("short")
	if status, _ := get(t, app, "/metrics?prometheus_token=short"); status != fiber.StatusNotFound {
		t.Fatalf("status = %d, want 404", status)
	}
}

// With Prometheus on, the whole app serves /metrics, but only with the token.
func TestTheAppServesMetricsWithPrometheus(t *testing.T) {
	t.Setenv("AGENTRQ_PROMETHEUS_ENABLED", "true")
	t.Setenv("AGENTRQ_PROMETHEUS_TOKEN", testMetricsToken)
	a := newTestApp(t)
	serve := func(target string) *httptest.ResponseRecorder {
		rr := httptest.NewRecorder()
		a.server.(http.Handler).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "http://localhost"+target, nil))
		return rr
	}
	if rr := serve("/metrics"); rr.Code != http.StatusNotFound {
		t.Fatalf("GET /metrics = %d, want 404", rr.Code)
	}
	rr := serve("/metrics?prometheus_token=" + testMetricsToken)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "go_goroutines") {
		t.Fatalf("GET /metrics with the token = %d\n%s", rr.Code, rr.Body.String())
	}
}

// The scrape also carries the telemetry actions, by name, from the same registry.
func TestTheAppServesTelemetryActionsWithPrometheus(t *testing.T) {
	t.Setenv("AGENTRQ_PROMETHEUS_ENABLED", "true")
	t.Setenv("AGENTRQ_PROMETHEUS_TOKEN", testMetricsToken)
	a := newTestApp(t)
	a.pubsub.Publish(context.Background(), pubsub.PublishRequest{PubSubID: entity.PubSubTopicCRUD, Event: entity.CRUDEvent{
		Action: entity.ActionTaskCreate, UserID: 1, WorkspaceID: 1,
	}})
	want := `agentrq_telemetry_actions_total{action="task_create",app_version="` + appVersion(t) + `",sub_action=""} 1`
	deadline := time.Now().Add(5 * time.Second)
	for {
		rr := httptest.NewRecorder()
		a.server.(http.Handler).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "http://localhost/metrics?prometheus_token="+testMetricsToken, nil))
		if strings.Contains(rr.Body.String(), want) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("no %s in:\n%s", want, rr.Body.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Every metric the app exports carries the running build's app_version, Go's
// runtime and process ones too, and agentrq_build_info names it once.
func TestEveryAppMetricCarriesTheAppVersion(t *testing.T) {
	t.Setenv("AGENTRQ_PROMETHEUS_ENABLED", "true")
	t.Setenv("AGENTRQ_PROMETHEUS_TOKEN", testMetricsToken)
	a := newTestApp(t)
	version := appVersion(t)
	label := `app_version="` + version + `"`
	a.pubsub.Publish(context.Background(), pubsub.PublishRequest{PubSubID: entity.PubSubTopicCRUD, Event: entity.CRUDEvent{
		Action: entity.ActionTaskCreate, UserID: 1, WorkspaceID: 1,
	}})
	serve := func(target string) string {
		rr := httptest.NewRecorder()
		a.server.(http.Handler).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "http://localhost"+target, nil))
		return rr.Body.String()
	}
	scrape := func() string { return serve("/metrics?prometheus_token=" + testMetricsToken) }
	serve("/") // an HTTP request to count
	body := scrape()
	for deadline := time.Now().Add(5 * time.Second); !strings.Contains(body, "agentrq_telemetry_actions_total{") || !strings.Contains(body, "http_requests_total{"); body = scrape() {
		if time.Now().After(deadline) {
			t.Fatalf("no telemetry or HTTP metrics in:\n%s", body)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if want := `agentrq_build_info{` + label + `} 1`; !strings.Contains(body, want) {
		t.Fatalf("no %s in:\n%s", want, body)
	}
	checked := 0
	for _, line := range strings.Split(body, "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		checked++
		if !strings.Contains(line, label) {
			t.Errorf("no %s on %s", label, line)
		}
	}
	if checked == 0 {
		t.Fatalf("no app metrics in:\n%s", body)
	}
}

// appVersion is the version the running build reports, as config.go sets it.
func appVersion(t *testing.T) string {
	t.Helper()
	svc, err := config.New()
	if err != nil {
		t.Fatal(err)
	}
	return svc.Version()
}
