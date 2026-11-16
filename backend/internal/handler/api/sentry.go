// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/gofiber/fiber/v3"
	zlog "github.com/rs/zerolog/log"

	view "github.com/agentrq/agentrq/backend/internal/data/view/api"
)

// Sentry is where the web app reports its errors; the zero value, nowhere.
type Sentry struct {
	DSN         string
	Environment string
}

// sentryTimeout bounds one envelope's trip to Sentry.
const sentryTimeout = 10 * time.Second

// sentryRelay passes the web app's envelopes on to Sentry. The app sends them
// to its own origin because the desktop app's CSP allows no other.
type sentryRelay struct {
	cfg      *view.SentryConfig
	dsn      *sentry.Dsn
	envelope *url.URL // the project's envelope endpoint
	client   *http.Client
}

// newSentryRelay relays to the DSN given, or, with none, is off. checkConfig
// has already refused a DSN Sentry cannot parse.
func newSentryRelay(s Sentry) sentryRelay {
	if s.DSN == "" {
		return sentryRelay{}
	}
	dsn, err := sentry.NewDsn(s.DSN)
	if err != nil {
		return sentryRelay{}
	}
	return sentryRelay{
		cfg:      &view.SentryConfig{DSN: s.DSN, Environment: s.Environment},
		dsn:      dsn,
		envelope: dsn.GetAPIURL(),
		client:   &http.Client{Timeout: sentryTimeout},
	}
}

// accepts an envelope only for the configured project, so the route relays
// to no one else's.
func (r sentryRelay) accepts(envelope []byte) bool {
	line, _, _ := bytes.Cut(envelope, []byte("\n"))
	var header struct {
		DSN string `json:"dsn"`
	}
	if json.Unmarshal(line, &header) != nil || header.DSN == "" {
		return false
	}
	dsn, err := sentry.NewDsn(header.DSN)
	return err == nil && dsn.GetPublicKey() == r.dsn.GetPublicKey() && dsn.GetAPIURL().String() == r.envelope.String()
}

// registerSentryRoutes adds the routes only when the web app reports: off,
// the app's read is a 404 and it loads no SDK. Signed out, it is a 401, so
// only a signed-in user's errors are reported.
func (h *handler) registerSentryRoutes() {
	if h.sentry.dsn != nil {
		h.router.Get("/sentry", h.sentryConfig())
		h.router.Post("/sentry", h.relaySentry())
	}
}

// sentryConfig tells the web app where to report: the DSN and environment.
func (h *handler) sentryConfig() fiber.Handler {
	return func(c fiber.Ctx) error {
		return c.JSON(h.sentry.cfg)
	}
}

// relaySentry sends an envelope on and answers with Sentry's status and its
// rate-limit headers, so the browser's SDK backs off as Sentry asks.
func (h *handler) relaySentry() fiber.Handler {
	return func(c fiber.Ctx) error {
		body := c.Body()
		if !h.sentry.accepts(body) {
			return c.SendStatus(fiber.StatusBadRequest)
		}
		ctx, cancel := newContext(c)
		defer cancel()
		req := (&http.Request{
			Method:        http.MethodPost,
			URL:           h.sentry.envelope,
			Header:        http.Header{fiber.HeaderContentType: {"application/x-sentry-envelope"}},
			Body:          io.NopCloser(bytes.NewReader(body)),
			ContentLength: int64(len(body)),
		}).WithContext(ctx)
		resp, err := h.sentry.client.Do(req)
		if err != nil {
			// A warning: an error line would itself go to Sentry.
			zlog.Warn().Err(err).Msg("sentry: relaying the web app's envelope")
			return c.SendStatus(fiber.StatusBadGateway)
		}
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body)
		for _, k := range []string{"Retry-After", "X-Sentry-Rate-Limits"} {
			if v := resp.Header.Get(k); v != "" {
				c.Set(k, v)
			}
		}
		return c.SendStatus(resp.StatusCode)
	}
}
