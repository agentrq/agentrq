// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	zlog "github.com/rs/zerolog/log"

	"github.com/agentrq/agentrq/backend/internal/app"
	"github.com/agentrq/agentrq/backend/internal/service/config"
)

// The web app and the config defaults are built into the binary, so the
// server runs from this one file. all: keeps Vite's _-prefixed chunks.
//
//go:embed all:public _config
var bundled embed.FS

// webApp is the built web app inside the binary.
func webApp() fs.FS {
	web, _ := fs.Sub(bundled, "public")
	return web
}

// loadConfig reads every block of the server's config. A block left out here
// is silently its zero value: its env vars do nothing.
func loadConfig(cfgSvc config.Service) (app.Config, error) {
	var cfg app.Config
	for key, dst := range map[string]any{
		"app": &cfg.App, "auth": &cfg.Auth, "smtp": &cfg.SMTP, "ssl": &cfg.SSL, "slack": &cfg.Slack,
		"webPush": &cfg.WebPush, "network": &cfg.Network, "storage": &cfg.Storage, "memory": &cfg.Memory,
		"skills": &cfg.Skills, "pano": &cfg.Pano, "hooks": &cfg.Hooks, "teams": &cfg.Teams, "mcp": &cfg.MCP, "acp": &cfg.ACP,
		"idgen": &cfg.Idgen, "ddos": &cfg.Ddos, "ratelimit": &cfg.Ratelimit,
	} {
		if err := cfgSvc.Populate(key, dst); err != nil {
			return app.Config{}, fmt.Errorf("%s: %w", key, err)
		}
	}
	return cfg, nil
}

func main() {
	cfgSvc, err := config.NewFS(bundled)
	if err != nil {
		zlog.Fatal().Err(err).Msg("config")
	}

	cfg, err := loadConfig(cfgSvc)
	if err != nil {
		zlog.Fatal().Err(err).Msg("config")
	}
	cfg.ConfigSvc = cfgSvc
	cfg.Web = webApp()

	a, err := app.New(cfg)
	if err != nil {
		zlog.Fatal().Err(err).Msg("init")
	}

	// Run the server in the background so we can react to termination signals and shut
	// down gracefully (drain in-flight requests, flush telemetry) instead of exiting hard.
	serverErr := make(chan error, 1)
	go func() { serverErr <- a.Run() }()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			zlog.Fatal().Err(err).Msg("run")
		}
	case sig := <-stop:
		zlog.Info().Str("signal", sig.String()).Msg("shutdown signal received; shutting down gracefully")
		ctx, cancel := context.WithTimeout(context.Background(), cfg.App.DrainDelay+15*time.Second)
		defer cancel()
		if err := a.Shutdown(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			zlog.Error().Err(err).Msg("graceful shutdown error")
		}
	}
}
