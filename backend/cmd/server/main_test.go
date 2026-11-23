// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package main

import (
	"errors"
	"io/fs"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/agentrq/agentrq/backend/internal/app"
	"github.com/agentrq/agentrq/backend/internal/service/config"
)

// The binary carries what the server used to read from beside it.
func TestTheBinaryCarriesTheWebAppAndConfig(t *testing.T) {
	if _, err := fs.Stat(webApp(), "index.html"); err != nil {
		t.Errorf("web app: %v", err)
	}
	for _, name := range []string{"_config/base.yaml", "_config/development.yaml", "_config/production.yaml"} {
		if _, err := fs.Stat(bundled, name); err != nil {
			t.Errorf("config: %v", err)
		}
	}
}

// recorder notes the blocks read.
type recorder struct {
	config.Service
	keys map[string]bool
}

func (r *recorder) Populate(key string, _ any) error {
	r.keys[key] = true
	return nil
}

// A block missing from LoadConfig is silently its zero value: its env vars
// do nothing, with no error anywhere.
func TestLoadConfigReadsEveryBlock(t *testing.T) {
	r := &recorder{keys: map[string]bool{}}
	if _, err := app.LoadConfig(r); err != nil {
		t.Fatal(err)
	}
	typ := reflect.TypeOf(app.Config{})
	for i := range typ.NumField() {
		key, _, _ := strings.Cut(typ.Field(i).Tag.Get("yaml"), ",")
		if key == "" || key == "-" {
			continue
		}
		if !r.keys[key] {
			t.Errorf("the %q block is never loaded", key)
		}
		delete(r.keys, key)
	}
	for key := range r.keys {
		t.Errorf("loads %q, which app.Config does not have", key)
	}
}

func TestLoadConfigTakesTheEnv(t *testing.T) {
	t.Setenv("ENV", "development")
	t.Setenv("AGENTRQ_TRUSTED_PROXIES", "10.0.0.0/8")
	t.Setenv("AGENTRQ_CLIENT_IP_HEADER", "X-Forwarded-For")
	t.Setenv("AGENTRQ_MCP_OPENAI_APPS_CHALLENGE", "challenge")
	t.Setenv("AGENTRQ_SENTRY_DSN", "https://key@sentry.example.com/1")
	t.Setenv("AGENTRQ_PROMETHEUS_TOKEN", "0123456789abcdef0123456789abcdef")
	svc, err := config.NewFS(bundled)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := app.LoadConfig(svc)
	if err != nil {
		t.Fatal(err)
	}
	// On by default, with limits a first visit to the web app stays under: about
	// 160 requests in half a second, with the service worker's precache.
	if !cfg.Ddos.Enabled || cfg.Ddos.MaxRequestsPerSecond != 200 || cfg.Ddos.BlockDuration != time.Minute {
		t.Errorf("ddos = %+v", cfg.Ddos)
	}
	if !cfg.Ratelimit.Enabled || cfg.Ratelimit.MaxPerIP != 1200 || cfg.Ratelimit.MaxPerUser != 600 || cfg.Ratelimit.Window != time.Minute {
		t.Errorf("ratelimit = %+v", cfg.Ratelimit)
	}
	if cfg.Network.TrustedProxies != "10.0.0.0/8" || cfg.Network.ClientIPHeader != "X-Forwarded-For" || cfg.MCP.OpenAIAppsChallenge != "challenge" {
		t.Errorf("network = %+v, mcp = %+v", cfg.Network, cfg.MCP)
	}
	// Off unless turned on.
	if cfg.Sentry.Enabled || cfg.Sentry.DSN != "https://key@sentry.example.com/1" {
		t.Errorf("sentry = %+v", cfg.Sentry)
	}
	if cfg.Prometheus.Enabled || cfg.Prometheus.Token != "0123456789abcdef0123456789abcdef" {
		t.Errorf("prometheus = %+v", cfg.Prometheus)
	}
}

type failing struct{ config.Service }

func (failing) Populate(string, any) error { return errors.New("bad yaml") }

func TestLoadConfigNamesTheBadBlock(t *testing.T) {
	if _, err := app.LoadConfig(failing{}); err == nil || !strings.HasSuffix(err.Error(), ": bad yaml") {
		t.Errorf("err = %v", err)
	}
}
