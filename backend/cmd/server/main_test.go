// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"errors"
	"io/fs"
	"reflect"
	"strings"
	"testing"

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

// notLoaded are the blocks loadConfig leaves out on purpose, and why.
var notLoaded = map[string]string{
	// Never loaded since they were added, so both have always been off. Turning
	// them on with their defaults throttles every user; that is its own change.
	"ddos":      "off until turning it on is decided",
	"ratelimit": "off until turning it on is decided",
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

// A block missing from loadConfig is silently its zero value: its env vars
// do nothing, with no error anywhere.
func TestLoadConfigReadsEveryBlock(t *testing.T) {
	r := &recorder{keys: map[string]bool{}}
	if _, err := loadConfig(r); err != nil {
		t.Fatal(err)
	}
	typ := reflect.TypeOf(app.Config{})
	for i := range typ.NumField() {
		key, _, _ := strings.Cut(typ.Field(i).Tag.Get("yaml"), ",")
		if key == "" || key == "-" {
			continue
		}
		if _, skip := notLoaded[key]; !r.keys[key] && !skip {
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
	svc, err := config.NewFS(bundled)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(svc)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Network.TrustedProxies != "10.0.0.0/8" || cfg.Network.ClientIPHeader != "X-Forwarded-For" || cfg.MCP.OpenAIAppsChallenge != "challenge" {
		t.Errorf("network = %+v, mcp = %+v", cfg.Network, cfg.MCP)
	}
}

type failing struct{ config.Service }

func (failing) Populate(string, any) error { return errors.New("bad yaml") }

func TestLoadConfigNamesTheBadBlock(t *testing.T) {
	if _, err := loadConfig(failing{}); err == nil || !strings.HasSuffix(err.Error(), ": bad yaml") {
		t.Errorf("err = %v", err)
	}
}
