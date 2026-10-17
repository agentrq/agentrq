// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"context"
	"time"

	zlog "github.com/rs/zerolog/log"
)

// machineSweepInterval is how often the agents of machines away too long are
// ended, and finished sessions still stored are removed. A session outlives
// the threshold by at most this much.
const machineSweepInterval = 5 * time.Minute

// machineSweeper is the part of the crud controller the sweep runs.
type machineSweeper interface {
	DeleteStaleSessions(ctx context.Context) (int64, error)
	DeleteFinishedSessions(ctx context.Context) (int64, error)
}

// startMachineSweep ends the sessions of stale machines every interval until
// ctx ends. Every backend instance runs it; the delete is idempotent.
func startMachineSweep(ctx context.Context, m machineSweeper, every time.Duration) {
	go func() {
		ticker := time.NewTicker(every)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				sweepMachines(ctx, m)
			}
		}
	}()
}

func sweepMachines(ctx context.Context, m machineSweeper) {
	if n, err := m.DeleteStaleSessions(ctx); err != nil {
		zlog.Error().Err(err).Msg("machines: ending the sessions of stale machines failed; retried at the next sweep")
	} else if n > 0 {
		zlog.Info().Int64("deleted", n).Msg("machines: ended the sessions of stale machines")
	}
	if n, err := m.DeleteFinishedSessions(ctx); err != nil {
		zlog.Error().Err(err).Msg("machines: removing finished sessions failed; retried at the next sweep")
	} else if n > 0 {
		zlog.Info().Int64("deleted", n).Msg("machines: removed finished sessions")
	}
}
