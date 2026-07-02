package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	zlog "github.com/rs/zerolog/log"

	"github.com/agentrq/agentrq/backend/internal/app"
	"github.com/agentrq/agentrq/backend/internal/service/config"
)

func main() {
	cfgSvc, err := config.New()
	if err != nil {
		zlog.Fatal().Err(err).Msg("config")
	}

	var cfg app.Config
	if err := cfgSvc.Populate("app", &cfg.App); err != nil {
		zlog.Fatal().Err(err).Msg("config app")
	}
	if err := cfgSvc.Populate("auth", &cfg.Auth); err != nil {
		zlog.Fatal().Err(err).Msg("config auth")
	}
	if err := cfgSvc.Populate("smtp", &cfg.SMTP); err != nil {
		zlog.Fatal().Err(err).Msg("config smtp")
	}
	if err := cfgSvc.Populate("ssl", &cfg.SSL); err != nil {
		zlog.Fatal().Err(err).Msg("config ssl")
	}
	if err := cfgSvc.Populate("slack", &cfg.Slack); err != nil {
		zlog.Fatal().Err(err).Msg("config slack")
	}
	if err := cfgSvc.Populate("webPush", &cfg.WebPush); err != nil {
		zlog.Fatal().Err(err).Msg("config webPush")
	}
	if err := cfgSvc.Populate("storage", &cfg.Storage); err != nil {
		zlog.Fatal().Err(err).Msg("config storage")
	}

	cfg.ConfigSvc = cfgSvc

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
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := a.Shutdown(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			zlog.Error().Err(err).Msg("graceful shutdown error")
		}
	}
}
