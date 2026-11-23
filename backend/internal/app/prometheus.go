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

// prometheusFiber guards /metrics with the token, then records every request
// and answers the scrape. A scrape without the token is a 404, as if metrics
// were off, so the endpoint is not found by probing. The scrape answers from
// reg, which the telemetry actions are counted in too.
func prometheusFiber(c PrometheusConfig, reg *prometheus.Registry) []any {
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
		Registerer:  reg,
		Gatherer:    reg,
	})}
}
