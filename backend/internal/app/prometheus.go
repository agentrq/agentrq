// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package app

import (
	"crypto/subtle"
	"fmt"
	"strings"

	fiberprometheus "github.com/gofiber/contrib/v3/prometheus"
	"github.com/gofiber/fiber/v3"
	"github.com/prometheus/client_golang/prometheus"
)

// PrometheusConfig serves the HTTP metrics at /metrics. Off by default.
type PrometheusConfig struct {
	Enabled bool   `yaml:"enabled"`
	Token   string `yaml:"token"`
}

const (
	// metricsPath is where a scrape is answered.
	metricsPath = "/metrics"
	// metricsTokenParam is the query parameter a scrape carries its token in.
	metricsTokenParam = "prometheus_token"
	// metricsTokenLen is the only token length accepted.
	metricsTokenLen = 32
)

// checkPrometheus refuses metrics turned on without a 32-character token.
func checkPrometheus(c PrometheusConfig) error {
	if !c.Enabled || len(c.Token) == metricsTokenLen {
		return nil
	}
	return fmt.Errorf("prometheus.token: must be %d characters when prometheus is enabled", metricsTokenLen)
}

// metricsRegistry is the one registry /metrics answers from, and the
// Registerer every metric goes in through: it labels each with the running
// build's app_version, so a change in any series lines up with a release. It
// also exports agentrq_build_info{app_version} 1, for deploy annotations.
func metricsRegistry(version string) (*prometheus.Registry, prometheus.Registerer) {
	reg := prometheus.NewRegistry()
	labelled := prometheus.WrapRegistererWith(prometheus.Labels{"app_version": version}, reg)
	info := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "agentrq_build_info",
		Help: "Always 1; its app_version label is the running build.",
	})
	info.Set(1)
	labelled.MustRegister(info)
	return reg, labelled
}

// prometheusFiber guards /metrics with the token, then records every request
// and answers the scrape. A scrape without the token is a 404, as if metrics
// were off, so the endpoint is not found by probing. Metrics go in through
// registerer and the scrape answers from reg, which the telemetry actions are
// counted in too.
func prometheusFiber(c PrometheusConfig, reg *prometheus.Registry, registerer prometheus.Registerer) []any {
	token := []byte(c.Token)
	guard := func(ctx fiber.Ctx) error {
		if path := strings.TrimRight(ctx.Path(), "/"); path != metricsPath {
			return ctx.Next()
		}
		given := []byte(ctx.Query(metricsTokenParam))
		if len(token) != metricsTokenLen || subtle.ConstantTimeCompare(given, token) != 1 {
			return ctx.Status(fiber.StatusNotFound).SendString("Not Found")
		}
		return ctx.Next()
	}
	return []any{guard, fiberprometheus.New(fiberprometheus.Config{
		ServiceName: "agentrq",
		MetricsPath: metricsPath,
		Registerer:  registerer,
		Gatherer:    reg,
	})}
}
