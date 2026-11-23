// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package main

import (
	"context"
	"embed"
	"errors"
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

func main() {
	cfgSvc, err := config.NewFS(bundled)
	if err != nil {
		zlog.Fatal().Err(err).Msg("config")
	}

	// The database the yaml names holds the rest of the config.
	cfg, err := app.Load(cfgSvc)
	if err != nil {
		zlog.Fatal().Err(err).Msg("config")
	}
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
