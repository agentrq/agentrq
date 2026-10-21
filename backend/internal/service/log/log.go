// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package log

import (
	"os"
	"time"

	"github.com/agentrq/agentrq/backend/internal/service/config"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// Setup makes zlog's global logger stamp every line with the app's name,
// version and environment and a UTC time, and routes library logs to it.
// It starts from a fresh logger, so calling it again adds no fields twice.
func Setup(cfg config.Service) {
	zerolog.TimestampFunc = func() time.Time {
		return time.Now().UTC()
	}
	zerolog.DurationFieldUnit = time.Millisecond
	log.Logger = zerolog.New(os.Stderr).With().
		Timestamp().
		Str("name", cfg.AppShortName()).
		Str("version", cfg.Version()).
		Str("env", cfg.Env()).
		Logger()
	RouteLibraryLogs()
}
