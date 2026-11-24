// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package app

import (
	"testing"

	"github.com/agentrq/agentrq/backend/internal/service/config"
	"github.com/prometheus/client_golang/prometheus"
)

// Each toggle turns off only its own place.
func TestTelemetryParams(t *testing.T) {
	reg := prometheus.NewRegistry()
	for _, tc := range []struct {
		name        string
		cfg         TelemetryConfig
		registerer  prometheus.Registerer
		wantStore   bool
		wantCounted bool
	}{
		{"both on", TelemetryConfig{Database: true, Prometheus: true}, reg, true, true},
		{"database only", TelemetryConfig{Database: true}, reg, true, false},
		{"prometheus only", TelemetryConfig{Prometheus: true}, reg, false, true},
		{"both off", TelemetryConfig{}, reg, false, false},
		{"prometheus on, /metrics off", TelemetryConfig{Database: true, Prometheus: true}, nil, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := telemetryParams(tc.cfg, nil, nil, tc.registerer)
			if p.SkipStorage == tc.wantStore {
				t.Errorf("SkipStorage = %v, want %v", p.SkipStorage, !tc.wantStore)
			}
			if (p.Registerer != nil) != tc.wantCounted {
				t.Errorf("Registerer set = %v, want %v", p.Registerer != nil, tc.wantCounted)
			}
		})
	}
}

// Both are on by default, so a server that sets neither keeps its telemetry,
// and each has its own env var.
func TestTelemetryConfig_Defaults(t *testing.T) {
	t.Chdir("../../cmd/server")
	load := func() TelemetryConfig {
		t.Helper()
		svc, err := config.New()
		if err != nil {
			t.Fatal(err)
		}
		cfg, err := LoadConfig(svc)
		if err != nil {
			t.Fatal(err)
		}
		return cfg.Telemetry
	}
	if got := load(); got != (TelemetryConfig{Database: true, Prometheus: true}) {
		t.Fatalf("default = %+v, want both on", got)
	}
	t.Setenv("AGENTRQ_TELEMETRY_DATABASE", "false")
	if got := load(); got != (TelemetryConfig{Prometheus: true}) {
		t.Fatalf("database off = %+v", got)
	}
	t.Setenv("AGENTRQ_TELEMETRY_DATABASE", "true")
	t.Setenv("AGENTRQ_TELEMETRY_PROMETHEUS", "false")
	if got := load(); got != (TelemetryConfig{Database: true}) {
		t.Fatalf("prometheus off = %+v", got)
	}
}
