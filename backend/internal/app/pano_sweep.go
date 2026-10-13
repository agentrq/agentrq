// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"context"
	"time"

	zlog "github.com/rs/zerolog/log"
)

// panoSweepInterval is how often expired Pano docs are deleted. Until then an
// expired doc is already hidden, so the sweep only reclaims its storage.
const panoSweepInterval = time.Hour

// panoSweeper is the part of the crud controller the sweep runs.
type panoSweeper interface {
	DeleteExpiredPanoDocs(ctx context.Context) (int, error)
}

// startPanoSweep deletes expired Pano docs every interval until ctx ends.
// Every backend instance runs it; a doc is deleted once whichever gets there
// first.
func startPanoSweep(ctx context.Context, p panoSweeper, every time.Duration) {
	go func() {
		ticker := time.NewTicker(every)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				sweepPano(ctx, p)
			}
		}
	}()
}

func sweepPano(ctx context.Context, p panoSweeper) {
	n, err := p.DeleteExpiredPanoDocs(ctx)
	if err != nil {
		zlog.Error().Err(err).Int("deleted", n).Msg("pano: deleting expired docs failed; the rest are retried at the next sweep")
		return
	}
	if n > 0 {
		zlog.Info().Int("deleted", n).Msg("pano: deleted expired docs")
	}
}
