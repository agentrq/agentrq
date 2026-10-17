// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"context"
	mathrand "math/rand/v2"
	"time"

	zlog "github.com/rs/zerolog/log"
)

// machineSweepInterval is how often the agents of machines away too long are
// ended. A session outlives the threshold by at most this much.
const machineSweepInterval = 5 * time.Minute

// sweepJitter is the most each wait between sweeps is lengthened by, a random
// whole number of seconds, so instances started together do not sweep
// together.
const sweepJitter = 2 * time.Minute

// machineSweeper is the part of the crud controller the sweep runs.
type machineSweeper interface {
	DeleteStaleSessions(ctx context.Context) (int64, error)
}

// startMachineSweep ends the sessions of stale machines every interval, plus
// up to jitter, until ctx ends. Every backend instance runs it; the delete is
// idempotent.
func startMachineSweep(ctx context.Context, m machineSweeper, every, jitter time.Duration) {
	go func() {
		timer := time.NewTimer(sweepWait(every, jitter))
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				sweepMachines(ctx, m)
				timer.Reset(sweepWait(every, jitter))
			}
		}
	}()
}

// sweepWait is every plus a random whole number of seconds up to jitter.
func sweepWait(every, jitter time.Duration) time.Duration {
	return every + time.Duration(mathrand.N(int64(jitter/time.Second)+1))*time.Second
}

func sweepMachines(ctx context.Context, m machineSweeper) {
	n, err := m.DeleteStaleSessions(ctx)
	if err != nil {
		zlog.Error().Err(err).Msg("machines: ending the sessions of stale machines failed; retried at the next sweep")
		return
	}
	if n > 0 {
		zlog.Info().Int64("deleted", n).Msg("machines: ended the sessions of stale machines")
	}
}
