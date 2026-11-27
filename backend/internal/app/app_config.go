// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/agentrq/agentrq/backend/internal/controller/appconfig"
	"github.com/agentrq/agentrq/backend/internal/handler/api/middleware/clientip"
	"github.com/agentrq/agentrq/backend/internal/repository/dbconn"
	"github.com/agentrq/agentrq/backend/internal/service/auth"
	"github.com/agentrq/agentrq/backend/internal/service/cleanup"
	"github.com/agentrq/agentrq/backend/internal/service/config"
	"github.com/agentrq/agentrq/backend/internal/service/githubapp"
	"github.com/agentrq/agentrq/backend/internal/service/gitlabapp"
	"github.com/agentrq/agentrq/backend/internal/service/s3"
	"github.com/agentrq/agentrq/backend/internal/service/storage"
)

// LoadConfig reads every block of the server's config. A block left out here
// is silently its zero value: its settings do nothing.
func LoadConfig(cfgSvc config.Service) (Config, error) {
	var cfg Config
	for key, dst := range map[string]any{
		"app": &cfg.App, "auth": &cfg.Auth, "smtp": &cfg.SMTP, "ssl": &cfg.SSL, "slack": &cfg.Slack, "githubApp": &cfg.GitHubApp, "gitlabApp": &cfg.GitLabApp,
		"webPush": &cfg.WebPush, "network": &cfg.Network, "storage": &cfg.Storage, "memory": &cfg.Memory,
		"skills": &cfg.Skills, "pano": &cfg.Pano, "hooks": &cfg.Hooks, "machines": &cfg.Machines, "teams": &cfg.Teams, "mcp": &cfg.MCP,
		"idgen": &cfg.Idgen, "ddos": &cfg.Ddos, "ratelimit": &cfg.Ratelimit, "sentry": &cfg.Sentry,
		"sentryFrontend": &cfg.SentryFrontend,
		"prometheus":     &cfg.Prometheus, "telemetry": &cfg.Telemetry,
	} {
		if err := cfgSvc.Populate(key, dst); err != nil {
			return Config{}, fmt.Errorf("%s: %w", key, err)
		}
	}
	cfg.ConfigSvc = cfgSvc
	return cfg, nil
}

// Load opens and migrates the database the yaml config names, then reads the
// config from it: the blocks stored there win over yaml and env vars, and any
// block not stored yet is stored from them first.
func Load(cfgSvc config.Service) (Config, error) {
	yamlCfg, err := LoadConfig(cfgSvc)
	if err != nil {
		return Config{}, err
	}
	db, err := openMigratedDatabase(cfgSvc)
	if err != nil {
		return Config{}, err
	}
	stored, err := appconfig.New(appconfig.Params{
		DB: db, Base: cfgSvc, Key: yamlCfg.Auth.WorkspaceTokenKey, Check: checkConfig,
	})
	if err == nil {
		cfgSvc, err = stored.Load(context.Background())
	}
	var cfg Config
	if err == nil {
		cfg, err = LoadConfig(cfgSvc)
	}
	if err != nil {
		db.Close(context.Background())
		return Config{}, fmt.Errorf("config: %w", err)
	}
	cfg.DB = db
	cfg.AppConfig = stored
	return cfg, nil
}

// openMigratedDatabase opens the database and brings its schema up to date.
func openMigratedDatabase(c config.Service) (dbconn.DBConn, error) {
	db, err := openDatabase(c)
	if err != nil {
		return nil, err
	}
	if db == nil {
		return nil, errors.New("neither postgres nor sqlite must be enabled in config")
	}
	if err := withMigrationLock(db.Conn(context.Background()), migrateSchema); err != nil {
		db.Close(context.Background())
		return nil, fmt.Errorf("migrate db: %w", err)
	}
	return db, nil
}

// checkConfig refuses a config New would refuse to start with, short of
// connecting to anything, so a block the operator saves cannot keep the
// server from starting again.
func checkConfig(c config.Service) error {
	cfg, err := LoadConfig(c)
	if err != nil {
		return err
	}
	if err := checkJWTSecrets(cfg); err != nil {
		return err
	}
	if err := checkLimits(cfg); err != nil {
		return err
	}
	if err := auth.CheckOAuth2Providers(cfg.Auth.OAuth2Providers); err != nil {
		return err
	}
	if _, err := cfg.Hooks.Settings(); err != nil {
		return err
	}
	if err := githubapp.Check(cfg.GitHubApp); err != nil {
		return err
	}
	if err := gitlabapp.Check(cfg.GitLabApp); err != nil {
		return err
	}
	if cfg.App.DrainDelay < 0 {
		return fmt.Errorf("app.drainDelay: %s is negative", cfg.App.DrainDelay)
	}
	if err := checkSentry("sentry", cfg.Sentry); err != nil {
		return err
	}
	if err := checkSentry("sentryFrontend", cfg.SentryFrontend); err != nil {
		return err
	}
	if err := checkPrometheus(cfg.Prometheus); err != nil {
		return err
	}
	if _, err := clientip.New(cfg.Network.TrustedProxies, cfg.Network.ClientIPHeader); err != nil {
		return fmt.Errorf("network.trustedProxies: %w", err)
	}
	if _, err := newCertStore(cfg.SSL.S3); err != nil {
		return err
	}
	for _, section := range []string{"artifacts", "skills", "pano", "hooks"} {
		if _, err := newBlobStorage(c, section, noStorage, func(s3.Service) storage.Service { return nil }); err != nil {
			return fmt.Errorf("%s storage: %w", section, err)
		}
	}
	if _, err := newAvatars(c, nil); err != nil {
		return fmt.Errorf("avatars: %w", err)
	}
	if _, err := cleanup.New(cfg.Storage); err != nil {
		return err
	}
	return nil
}

// noStorage stands in for a local store, which checkConfig must not create.
func noStorage() (storage.Service, error) { return nil, nil }

// checkJWTSecrets refuses a config missing either signing key.
func checkJWTSecrets(cfg Config) error {
	if cfg.Auth.APIJWTSecret == "" {
		return errors.New("auth: API JWT secret must be configured")
	}
	if cfg.Auth.MCPJWTSecret == "" {
		return errors.New("auth: MCP JWT secret must be configured")
	}
	return nil
}
