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

// The claim the backfill of doc-task links runs under: once per deployment,
// by whichever instance claims it first.
const (
	panoBackfillClaim = "pano_doc_tasks_backfill"
	panoBackfillKey   = "v1"
)

// panoBackfiller is what the backfill of doc-task links needs.
type panoBackfiller interface {
	BackfillPanoDocTasks(ctx context.Context) (int64, error)
}

// panoClaimer claims a one-off run, reporting whether this caller got it.
type panoClaimer interface {
	ClaimTelemetryAggregation(ctx context.Context, aggregationType, periodKey string) (bool, error)
}

// backfillPanoDocTasks links every doc to the tasks its checklist items were
// assigned to before links were stored. The claim is kept even if the run
// fails, as the latency backfill's is: a failure is logged, never retried at
// every boot.
func backfillPanoDocTasks(ctx context.Context, claims panoClaimer, p panoBackfiller) {
	claimed, err := claims.ClaimTelemetryAggregation(ctx, panoBackfillClaim, panoBackfillKey)
	if err != nil {
		zlog.Error().Err(err).Msg("pano: failed to claim the doc-task link backfill")
		return
	}
	if !claimed {
		return
	}
	n, err := p.BackfillPanoDocTasks(ctx)
	if err != nil {
		zlog.Error().Err(err).Int64("linked", n).Msg("pano: the doc-task link backfill failed; a doc is linked again at its next save")
		return
	}
	zlog.Info().Int64("linked", n).Msg("pano: backfilled doc-task links")
}
