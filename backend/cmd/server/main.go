package main

import (
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

	cfg.ConfigSvc = cfgSvc

	a, err := app.New(cfg)
	if err != nil {
		zlog.Fatal().Err(err).Msg("init")
	}

	if err := a.Run(); err != nil {
		zlog.Fatal().Err(err).Msg("run")
	}
}
