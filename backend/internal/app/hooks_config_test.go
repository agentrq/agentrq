// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agentrq/agentrq/backend/internal/controller/hook"
	"github.com/agentrq/agentrq/backend/internal/service/config"
)

// loadHooks reads the hooks block from the real base.yaml, as main does.
func loadHooks(t *testing.T) hook.Config {
	t.Helper()
	t.Chdir("../../cmd/server")
	svc, err := config.New()
	if err != nil {
		t.Fatal(err)
	}
	var cfg Config
	if err := svc.Populate("hooks", &cfg.Hooks); err != nil {
		t.Fatal(err)
	}
	return cfg.Hooks
}

func TestHooksSettingsDefaultWhenNoEnvIsSet(t *testing.T) {
	t.Setenv("ENV", "development")
	for _, env := range []string{"AGENTRQ_HOOKS_ALLOW_USER_HOOKS", "AGENTRQ_HOOKS_DELIVERY_RETENTION", "AGENTRQ_HOOKS_MAX_PAYLOAD_SIZE"} {
		t.Setenv(env, "")
	}
	s, err := loadHooks(t).Settings()
	if err != nil {
		t.Fatal(err)
	}
	want := hook.Settings{DeliveryRetention: 7 * 24 * time.Hour, MaxPayloadSize: 65536}
	if s != want {
		t.Errorf("settings = %+v, want %+v", s, want)
	}
}

func TestHooksSettingsComeFromTheEnv(t *testing.T) {
	t.Setenv("ENV", "development")
	t.Setenv("AGENTRQ_HOOKS_ALLOW_USER_HOOKS", "true")
	t.Setenv("AGENTRQ_HOOKS_DELIVERY_RETENTION", "30d")
	t.Setenv("AGENTRQ_HOOKS_MAX_PAYLOAD_SIZE", "131072")
	s, err := loadHooks(t).Settings()
	if err != nil {
		t.Fatal(err)
	}
	want := hook.Settings{AllowUserHooks: true, DeliveryRetention: 30 * 24 * time.Hour, MaxPayloadSize: 131072}
	if s != want {
		t.Errorf("settings = %+v, want %+v", s, want)
	}
}

func TestNewRefusesBadHooksSettings(t *testing.T) {
	var cfg Config
	cfg.Auth.JWTSecret = "secret"
	cfg.Hooks.DeliveryRetention = "1h"
	_, err := New(cfg)
	if err == nil || !strings.Contains(err.Error(), "hooks.deliveryRetention") {
		t.Fatalf("New = %v, want a hooks.deliveryRetention error", err)
	}
}

func TestNewRefusesAnUnknownHooksStorage(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ENV", "development")
	t.Setenv("AGENTRQ_AUTH_JWT_SECRET", "hooks-test-secret")
	t.Setenv("AGENTRQ_SQLITE_DSN", filepath.Join(dir, "agentrq.db"))
	t.Setenv("AGENTRQ_STORAGE_DIR", dir)
	t.Setenv("AGENTRQ_HOOKS_STORAGE", "gcs")
	t.Chdir("../../cmd/server")
	svc, err := config.New()
	if err != nil {
		t.Fatal(err)
	}
	var cfg Config
	for key, dst := range map[string]any{"app": &cfg.App, "auth": &cfg.Auth, "storage": &cfg.Storage, "idgen": &cfg.Idgen} {
		if err := svc.Populate(key, dst); err != nil {
			t.Fatalf("config %s: %v", key, err)
		}
	}
	cfg.ConfigSvc = svc
	if _, err := New(cfg); err == nil || !strings.Contains(err.Error(), `hook storage: unknown hooks storage "gcs"`) {
		t.Fatalf("New = %v, want the hooks storage refused", err)
	}
}
