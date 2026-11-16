// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"time"

	"github.com/getsentry/sentry-go"
	fibersentry "github.com/gofiber/contrib/v3/sentry"
	fiberrecover "github.com/gofiber/fiber/v3/middleware/recover"
	"github.com/rs/zerolog"
	zlog "github.com/rs/zerolog/log"

	"github.com/agentrq/agentrq/backend/internal/service/config"
)

// SentryConfig turns error reporting to Sentry on. Off by default.
type SentryConfig struct {
	Enabled bool   `yaml:"enabled"`
	DSN     string `yaml:"dsn"`
}

// sentryFlushTimeout bounds how long shutdown waits for queued events.
const sentryFlushTimeout = 2 * time.Second

// sentryTransport replaces the HTTP transport in tests; nil sends to the DSN.
var sentryTransport sentry.Transport

// errSentryDSNRequired refuses Sentry turned on with nowhere to send.
var errSentryDSNRequired = errors.New("sentry.dsn: required when sentry is enabled")

// checkSentry refuses an enabled config without a DSN Sentry can parse.
func checkSentry(c SentryConfig) error {
	if !c.Enabled {
		return nil
	}
	if c.DSN == "" {
		return errSentryDSNRequired
	}
	if _, err := sentry.NewDsn(c.DSN); err != nil {
		return errors.New("sentry.dsn: " + err.Error())
	}
	return nil
}

// startSentry starts the client, its release the app's version and its
// environment the config's, and sends every error-level log line to it too,
// so errors outside a request are reported as well.
func startSentry(c SentryConfig, cfgSvc config.Service) error {
	if !c.Enabled {
		return nil
	}
	if c.DSN == "" {
		return errSentryDSNRequired
	}
	if err := sentry.Init(sentry.ClientOptions{
		Dsn:              c.DSN,
		Release:          cfgSvc.Version(),
		Environment:      cfgSvc.Env(),
		AttachStacktrace: true,
		Transport:        sentryTransport,
	}); err != nil {
		return errors.New("sentry.dsn: " + err.Error())
	}
	zlog.Logger = zlog.Logger.Output(zerolog.MultiLevelWriter(os.Stderr, sentryLogs{}))
	return nil
}

// sentryFiber reports a Fiber handler's panic, then answers it 500 through
// Fiber's recover, so the panic never reaches the stdlib reporter as well.
func sentryFiber() []any {
	return []any{
		fiberrecover.New(fiberrecover.Config{EnableStackTrace: true}),
		fibersentry.New(fibersentry.Config{Repanic: true}),
	}
}

// reportPanics reports a panic from the stdlib routes (event streams,
// WebSockets) and panics again, so net/http handles it as before. It leaves
// the ResponseWriter unwrapped: a hijack or flush must reach the real one.
func reportPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v != http.ErrAbortHandler {
					hub := sentry.CurrentHub().Clone()
					hub.Scope().SetRequest(r)
					hub.RecoverWithContext(r.Context(), v)
				}
				panic(v)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// sentryLogs sends each error-level log line as an event: its message, and
// its error when it has one. Other fields stay out, as they may name people.
type sentryLogs struct{}

func (sentryLogs) Write(p []byte) (int, error) { return len(p), nil }

func (sentryLogs) WriteLevel(level zerolog.Level, p []byte) (int, error) {
	if level < zerolog.ErrorLevel || level == zerolog.NoLevel || level == zerolog.Disabled {
		return len(p), nil
	}
	var line struct {
		Message string `json:"message"`
		Error   string `json:"error"`
	}
	_ = json.Unmarshal(p, &line)
	msg := line.Message
	if line.Error != "" {
		if msg != "" {
			msg += ": "
		}
		msg += line.Error
	}
	event := sentry.NewEvent()
	event.Level = sentry.LevelError
	if level >= zerolog.FatalLevel {
		event.Level = sentry.LevelFatal
	}
	event.Message = msg
	sentry.CaptureEvent(event)
	if level >= zerolog.FatalLevel {
		// Fatal exits right after this write.
		sentry.Flush(sentryFlushTimeout)
	}
	return len(p), nil
}
