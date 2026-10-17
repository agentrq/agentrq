// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Package health answers the liveness and readiness probes a load balancer or
// an orchestrator sends to each instance.
package health

import (
	"context"
	"net/http"
	"sync/atomic"
	"time"

	zlog "github.com/rs/zerolog/log"
)

const (
	LivePath  = "/healthz"
	ReadyPath = "/readyz"

	// pingTimeout keeps a hung database from holding a probe past the
	// orchestrator's own timeout, which would read as a dead instance.
	pingTimeout = 2 * time.Second
)

// Handler serves LivePath and ReadyPath.
//
// Liveness checks nothing but the process: restarting an instance because its
// database is slow only adds load to the database. Readiness pings the
// database, and fails from the moment shutdown starts, so traffic moves to the
// other instances while this one drains.
type Handler struct {
	ping     func(ctx context.Context) error
	draining atomic.Bool
}

func New(ping func(ctx context.Context) error) *Handler {
	return &Handler{ping: ping}
}

// Drain makes every readiness probe from now on fail.
func (h *Handler) Drain() {
	h.draining.Store(true)
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.Path == ReadyPath {
		if h.draining.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("draining"))
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), pingTimeout)
		defer cancel()
		if err := h.ping(ctx); err != nil {
			// The reason stays in the log: the probe is unauthenticated.
			zlog.Warn().Err(err).Msg("readiness: database unavailable")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("unavailable"))
			return
		}
	}
	_, _ = w.Write([]byte("ok"))
}
