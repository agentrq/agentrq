package main

import (
	"log"

	"github.com/hasmcp/agentrq/backend/internal/app"
	"github.com/hasmcp/agentrq/backend/internal/service/config"
)

func main() {
	cfgSvc, err := config.New()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	var cfg app.Config
	if err := cfgSvc.Populate("app", &cfg.App); err != nil {
		log.Fatalf("config app: %v", err)
	}
	if err := cfgSvc.Populate("auth", &cfg.Auth); err != nil {
		log.Fatalf("config auth: %v", err)
	}
	if err := cfgSvc.Populate("smtp", &cfg.SMTP); err != nil {
		log.Fatalf("config smtp: %v", err)
	}
	if err := cfgSvc.Populate("ssl", &cfg.SSL); err != nil {
		log.Fatalf("config ssl: %v", err)
	}

	cfg.ConfigSvc = cfgSvc

	a, err := app.New(cfg)
	if err != nil {
		log.Fatalf("init: %v", err)
	}

	if err := a.Run(); err != nil {
		log.Fatalf("run: %v", err)
	}
}

