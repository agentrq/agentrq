// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"errors"
	"net/http"
	"time"

	"github.com/getsentry/sentry-go"
	fibersentry "github.com/gofiber/contrib/v3/sentry"
	fiberrecover "github.com/gofiber/fiber/v3/middleware/recover"

	handlerapi "github.com/agentrq/agentrq/backend/internal/handler/api"
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

// checkSentry refuses an enabled config, of the block named, without a DSN
// Sentry can parse.
func checkSentry(block string, c SentryConfig) error {
	if !c.Enabled {
		return nil
	}
	if c.DSN == "" {
		return errors.New(block + ".dsn: required when " + block + " is enabled")
	}
	if _, err := sentry.NewDsn(c.DSN); err != nil {
		return errors.New(block + ".dsn: " + err.Error())
	}
	return nil
}

// frontendSentry is where the web app reports its errors; the zero value,
// nowhere.
func frontendSentry(c SentryConfig, cfgSvc config.Service) handlerapi.Sentry {
	if !c.Enabled {
		return handlerapi.Sentry{}
	}
	return handlerapi.Sentry{DSN: c.DSN, Environment: cfgSvc.Env()}
}

// startSentry starts the client, its release the app's version and its
// environment the config's. Only panics are reported: a log line, even at
// Error, is not, as most are an expected failure already answered.
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
