// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package app

import (
	"time"

	"github.com/agentrq/agentrq/backend/internal/controller/telemetry"
	"github.com/agentrq/agentrq/backend/internal/repository/dbconn"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
	"github.com/prometheus/client_golang/prometheus"
)

// TelemetryConfig turns each place recorded actions go on or off.
type TelemetryConfig struct {
	// Database stores them, for the stats pages.
	Database bool `yaml:"database"`
	// Prometheus counts them on /metrics, when prometheus is enabled.
	Prometheus bool `yaml:"prometheus"`
}

// telemetryParams is the telemetry controller cfg asks for. registerer is nil
// when prometheus is off.
func telemetryParams(cfg TelemetryConfig, db dbconn.DBConn, ps pubsub.Service, registerer prometheus.Registerer) telemetry.Params {
	p := telemetry.Params{
		DB:          db,
		PubSub:      ps,
		BatchSize:   1000,
		Interval:    5 * time.Second,
		SkipStorage: !cfg.Database,
	}
	if cfg.Prometheus {
		p.Registerer = registerer
	}
	return p
}
