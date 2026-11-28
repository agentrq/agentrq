// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package app

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agentrq/agentrq/backend/internal/controller/appconfig"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/service/config"
	"github.com/agentrq/agentrq/backend/internal/service/s3"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const testTokenKey = "0123456789abcdef0123456789abcdef"

// bundledConfig reads the server's own base.yaml with the env set.
func bundledConfig(t *testing.T) config.Service {
	t.Helper()
	svc, err := config.NewFS(os.DirFS(filepath.Join("..", "..", "cmd", "server")))
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

// configDatabases sets the env for SQLite, and for Postgres when
// AGENTRQ_TEST_POSTGRES_DSN names one, each a fresh database.
func configDatabases(t *testing.T) map[string]func(t *testing.T) {
	dbs := map[string]func(t *testing.T){
		"sqlite": func(t *testing.T) {
			t.Setenv("AGENTRQ_SQLITE_DSN", filepath.Join(t.TempDir(), "agentrq.db"))
		},
	}
	if os.Getenv("AGENTRQ_TEST_POSTGRES_DSN") != "" {
		dbs["postgres"] = func(t *testing.T) {
			t.Setenv("AGENTRQ_SQLITE_ENABLED", "false")
			t.Setenv("AGENTRQ_POSTGRES_ENABLED", "true")
			parts := dsnParts(t, os.Getenv("AGENTRQ_TEST_POSTGRES_DSN"))
			parts["dbname"] = freshDatabase(t, parts)
			for k, env := range map[string]string{"host": "HOST", "port": "PORT", "user": "USER", "password": "PASSWORD", "dbname": "DBNAME"} {
				t.Setenv("AGENTRQ_POSTGRES_"+env, parts[k])
			}
		}
	}
	return dbs
}

// dsnParts reads a DSN as a URL, postgres://user:pass@host:port/db, its
// password percent-encoded, or as key=value pairs.
func dsnParts(t *testing.T, dsn string) map[string]string {
	t.Helper()
	parts := map[string]string{"port": "5432"}
	if !strings.Contains(dsn, "://") {
		for _, kv := range strings.Fields(dsn) {
			k, v, _ := strings.Cut(kv, "=")
			parts[k] = v
		}
		return parts
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("AGENTRQ_TEST_POSTGRES_DSN: %v", err)
	}
	parts["host"], parts["user"], parts["dbname"] = u.Hostname(), u.User.Username(), strings.TrimPrefix(u.Path, "/")
	parts["password"], _ = u.User.Password()
	if u.Port() != "" {
		parts["port"] = u.Port()
	}
	return parts
}

// freshDatabase creates an empty database where parts name a server, and
// names it.
func freshDatabase(t *testing.T, parts map[string]string) string {
	t.Helper()
	dsn := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		parts["host"], parts["port"], parts["user"], parts["password"], cmp.Or(parts["dbname"], "postgres"))
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	name := fmt.Sprintf("app_config_%d", time.Now().UnixNano())
	if err := admin.Exec("CREATE DATABASE " + name).Error; err != nil {
		t.Fatalf("create database: %v", err)
	}
	t.Cleanup(func() {
		admin.Exec("DROP DATABASE IF EXISTS " + name + " WITH (FORCE)")
		sqlDB, _ := admin.DB()
		sqlDB.Close()
	})
	return name
}

func setConfigEnv(t *testing.T) {
	t.Setenv("ENV", "development")
	t.Setenv("AGENTRQ_AUTH_JWT_SECRET", "jwt-from-env")
	t.Setenv("AGENTRQ_AUTH_WORKSPACE_TOKEN_KEY", testTokenKey)
	t.Setenv("AGENTRQ_AUTH_ROOT_ACCESS_TOKEN", "root-1")
	t.Setenv("AGENTRQ_SMTP_HOST", "smtp-from-env")
}

// The first start stores the config from yaml and env vars; after that the
// stored config wins over both, apart from the settings kept in env vars.
func TestLoadStoresTheConfigThenPrefersIt(t *testing.T) {
	for name, useDB := range configDatabases(t) {
		t.Run(name, func(t *testing.T) {
			setConfigEnv(t)
			useDB(t)

			cfg, err := Load(bundledConfig(t))
			if err != nil {
				t.Fatal(err)
			}
			if cfg.SMTP.Host != "smtp-from-env" || cfg.Auth.APIJWTSecret != "jwt-from-env" || cfg.Auth.MCPJWTSecret != "jwt-from-env" || cfg.ConfigSvc == nil || cfg.AppConfig == nil {
				t.Fatalf("first start: smtp %q, jwt %q", cfg.SMTP.Host, cfg.Auth.APIJWTSecret)
			}
			var rows []model.AppConfiguration
			if err := cfg.DB.Conn(context.Background()).Order("config_key").Find(&rows).Error; err != nil {
				t.Fatal(err)
			}
			var keys []string
			for _, r := range rows {
				keys = append(keys, r.ConfigKey)
			}
			want := "account app artifacts auth ddos githubApp gitlabApp hooks machines mcp memory network pano prometheus pubsub ratelimit s3 sentry sentryFrontend skills slack smtp ssl storage teams telemetry webPush"
			if got := strings.Join(keys, " "); got != want {
				t.Fatalf("stored blocks = %s\nwant %s", got, want)
			}

			// What the admin page does: a change, checked like a start.
			if _, err := cfg.AppConfig.Update(context.Background(), entity.UpdateAppConfigurationRequest{
				Key: "smtp", Value: json.RawMessage(`{"host":"smtp-from-admin","port":2525}`),
			}); err != nil {
				t.Fatal(err)
			}
			// The page never gets the JWT secret, and saving auth as shown keeps it.
			blocks, err := cfg.AppConfig.List(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			for _, b := range blocks {
				if strings.Contains(string(b.Value), "jwt-from-env") {
					t.Fatalf("%s sent a secret: %s", b.Key, b.Value)
				}
				if b.Key == "auth" {
					if _, err := cfg.AppConfig.Update(context.Background(), entity.UpdateAppConfigurationRequest{Key: "auth", Value: b.Value}); err != nil {
						t.Fatal(err)
					}
				}
			}
			_, err = cfg.AppConfig.Update(context.Background(), entity.UpdateAppConfigurationRequest{
				Key: "memory", Value: json.RawMessage(`{"maxFileSize":-1}`),
			})
			var refused *entity.AppConfigurationError
			if !errors.As(err, &refused) || !strings.HasPrefix(refused.Message, "memory.maxFileSize: -1 bytes") {
				t.Fatalf("a block the server could not start with: %v", err)
			}
			cfg.DB.Close(context.Background())

			t.Setenv("AGENTRQ_SMTP_HOST", "smtp-env-changed")
			t.Setenv("AGENTRQ_AUTH_JWT_SECRET", "jwt-env-changed")
			t.Setenv("AGENTRQ_AUTH_ROOT_ACCESS_TOKEN", "root-2")
			cfg, err = Load(bundledConfig(t))
			if err != nil {
				t.Fatal(err)
			}
			defer cfg.DB.Close(context.Background())
			if cfg.SMTP.Host != "smtp-from-admin" || cfg.SMTP.Port != 2525 || cfg.Auth.APIJWTSecret != "jwt-from-env" || cfg.Auth.MCPJWTSecret != "jwt-from-env" {
				t.Fatalf("second start: smtp %q:%d, jwt %q", cfg.SMTP.Host, cfg.SMTP.Port, cfg.Auth.APIJWTSecret)
			}
			if cfg.Auth.RootAccessToken != "root-2" || cfg.Auth.WorkspaceTokenKey != testTokenKey {
				t.Fatalf("the env-only settings: root %q, key %q", cfg.Auth.RootAccessToken, cfg.Auth.WorkspaceTokenKey)
			}
		})
	}
}

func TestLoadErrors(t *testing.T) {
	t.Run("a config that cannot be read", func(t *testing.T) {
		setConfigEnv(t)
		t.Setenv("AGENTRQ_SMTP_PORT", "many")
		if _, err := Load(bundledConfig(t)); err == nil || !strings.HasPrefix(err.Error(), "smtp: ") {
			t.Fatalf("Load: %v", err)
		}
	})

	t.Run("no database", func(t *testing.T) {
		setConfigEnv(t)
		t.Setenv("AGENTRQ_SQLITE_ENABLED", "false")
		if _, err := Load(bundledConfig(t)); err == nil || err.Error() != "neither postgres nor sqlite must be enabled in config" {
			t.Fatalf("Load: %v", err)
		}
	})

	t.Run("a key AES cannot use", func(t *testing.T) {
		setConfigEnv(t)
		t.Setenv("AGENTRQ_SQLITE_DSN", filepath.Join(t.TempDir(), "agentrq.db"))
		t.Setenv("AGENTRQ_AUTH_WORKSPACE_TOKEN_KEY", "short")
		if _, err := Load(bundledConfig(t)); err == nil || !strings.Contains(err.Error(), "config: auth.workspaceTokenKey") {
			t.Fatalf("Load: %v", err)
		}
	})

	t.Run("another key", func(t *testing.T) {
		setConfigEnv(t)
		t.Setenv("AGENTRQ_SQLITE_DSN", filepath.Join(t.TempDir(), "agentrq.db"))
		cfg, err := Load(bundledConfig(t))
		if err != nil {
			t.Fatal(err)
		}
		cfg.DB.Close(context.Background())
		t.Setenv("AGENTRQ_AUTH_WORKSPACE_TOKEN_KEY", "fedcba9876543210fedcba9876543210")
		if _, err := Load(bundledConfig(t)); err == nil || !strings.Contains(err.Error(), "cannot decrypt it with auth.workspaceTokenKey") {
			t.Fatalf("Load: %v", err)
		}
	})

	t.Run("a stored block that cannot be read", func(t *testing.T) {
		setConfigEnv(t)
		t.Setenv("AGENTRQ_SQLITE_DSN", filepath.Join(t.TempDir(), "agentrq.db"))
		base := bundledConfig(t)
		db, err := openMigratedDatabase(base)
		if err != nil {
			t.Fatal(err)
		}
		unchecked, _ := appconfig.New(appconfig.Params{DB: db, Base: base, Key: testTokenKey, Check: func(config.Service) error { return nil }})
		if _, err := unchecked.Update(context.Background(), entity.UpdateAppConfigurationRequest{Key: "smtp", Value: json.RawMessage(`{"port":"many"}`)}); err != nil {
			t.Fatal(err)
		}
		db.Close(context.Background())
		if _, err := Load(bundledConfig(t)); err == nil || !strings.HasPrefix(err.Error(), "config: smtp: ") {
			t.Fatalf("Load: %v", err)
		}
	})
}

// checkConfig passes the bundled defaults, and refuses each block New would
// refuse to start with.
func TestCheckConfig(t *testing.T) {
	setConfigEnv(t)
	base := bundledConfig(t)
	if err := checkConfig(base); err != nil {
		t.Fatalf("the defaults: %v", err)
	}
	// S3 storage builds a client, which connects to nothing yet.
	inS3, _ := config.Overlay(base, map[string][]byte{"skills": []byte(`{"storage":"s3"}`)})
	if err := checkConfig(inS3); err != nil {
		t.Fatalf("skills in S3: %v", err)
	}
	for _, tc := range []struct{ key, block, want string }{
		{"smtp", `{"port":"many"}`, "smtp: "},
		{"auth", `{"apiJwtSecret":""}`, "auth: API JWT secret must be configured"},
		{"auth", `{"mcpJwtSecret":""}`, "auth: MCP JWT secret must be configured"},
		{"memory", `{"maxFileSize":-1}`, "memory.maxFileSize: "},
		{"hooks", `{"maxPayloadSize":1}`, "hooks.maxPayloadSize"},
		{"app", `{"drainDelay":"-1s"}`, "app.drainDelay: -1s is negative"},
		{"network", `{"trustedProxies":"not an address"}`, "network.trustedProxies: "},
		{"ssl", `{"s3":{"enabled":true,"bucket":""}}`, "ssl.s3.bucket is required"},
		{"skills", `{"storage":"gcs"}`, `skills storage: unknown skills storage "gcs"`},
		{"storage", `{"attachmentRetentionPeriod":"soon"}`, "cleanup: invalid retention period"},
		{"auth", `{"oauth2Providers":[{"id":"Bad Id"}]}`, "auth.oauth2Providers[0].id"},
		{"sentry", `{"enabled":true}`, "sentry.dsn: required when sentry is enabled"},
		{"sentry", `{"enabled":true,"dsn":"not a dsn"}`, "sentry.dsn: "},
		{"sentryFrontend", `{"enabled":true}`, "sentryFrontend.dsn: required when sentryFrontend is enabled"},
		{"prometheus", `{"enabled":true}`, "prometheus.token: must be 32 characters"},
		{"prometheus", `{"enabled":true,"token":"short"}`, "prometheus.token: must be 32 characters"},
		{"gitlabApp", `{"host":"https://gitlab.com"}`, "gitlabApp.host: "},
		{"gitlabApp", `{"enabled":true,"clientId":""}`, "gitlabApp: clientId and clientSecret are required"},
	} {
		svc, err := config.Overlay(base, map[string][]byte{tc.key: []byte(tc.block)})
		if err != nil {
			t.Fatal(err)
		}
		if err := checkConfig(svc); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s %s: %v, want %q", tc.key, tc.block, err, tc.want)
		}
	}

	old := newS3
	t.Cleanup(func() { newS3 = old })
	newS3 = func(s3.Params) (s3.Service, error) { return nil, errors.New("no S3 credentials configured") }
	svc, _ := config.Overlay(base, map[string][]byte{"s3": []byte(`{"endpoint":"https://s3.example.com","bucket":"b"}`)})
	if err := checkConfig(svc); err == nil || err.Error() != "avatars: s3: no S3 credentials configured" {
		t.Errorf("avatars: %v", err)
	}
}

func TestNoStorageCreatesNothing(t *testing.T) {
	if s, err := noStorage(); s != nil || err != nil {
		t.Fatalf("noStorage = %v, %v", s, err)
	}
}
