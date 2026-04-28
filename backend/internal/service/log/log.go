package log

import (
	"time"

	"github.com/agentrq/agentrq/backend/internal/service/config"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

type (
	Params struct {
		Config config.Service
	}

	Service interface {
	}

	service struct {
	}
)

func New(p Params) (Service, error) {
	zerolog.TimestampFunc = func() time.Time {
		return time.Now().UTC()
	}
	zerolog.DurationFieldUnit = time.Millisecond
	log.Logger = log.With().
		Str("name", p.Config.AppShortName()).
		Str("version", p.Config.Version()).
		Str("env", p.Config.Env()).
		Logger()

	return &service{}, nil
}
