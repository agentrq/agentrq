// Package backfill is a generic run-once engine for one-time data migrations.
// Like the scheduler, it is a background, repository-backed lifecycle service:
// callers hand it a set of named backfills, and each one is run at most once
// (recorded in the backfills table) across restarts. It holds no knowledge of
// what any individual backfill does — that domain logic lives in a controller.
package backfill

import (
	"context"

	zlog "github.com/rs/zerolog/log"

	"github.com/agentrq/agentrq/backend/internal/repository/base"
)

// Backfill is a named one-time data migration supplied by a caller.
type Backfill struct {
	// Name is the stable idempotency key recorded in the backfills table.
	Name string
	// Run performs the migration. It MUST be idempotent: it is recorded only
	// after it succeeds, so a crash before recording re-runs it next startup.
	Run func(ctx context.Context) error
}

// Backfiller runs pending backfills.
type Backfiller interface {
	// Run records which of the given backfills have already been applied (one
	// query) and launches any pending ones in the background, recording each on
	// success. Only the initial load can fail synchronously, so migrations never
	// block startup.
	Run(ctx context.Context, backfills []Backfill) error
}

type service struct {
	repo base.Repository
}

// New builds the backfill engine.
func New(repo base.Repository) Backfiller {
	return &service{repo: repo}
}

func (s *service) Run(ctx context.Context, backfills []Backfill) error {
	applied, err := s.repo.SystemListBackfills(ctx)
	if err != nil {
		return err
	}
	done := make(map[string]bool, len(applied))
	for _, b := range applied {
		done[b.Name] = true
	}

	pending := make([]Backfill, 0, len(backfills))
	for _, b := range backfills {
		if !done[b.Name] {
			pending = append(pending, b)
		}
	}
	if len(pending) == 0 {
		return nil
	}

	// Run off the startup path: backfills only touch historical rows and are not
	// required for the server to begin serving requests.
	go s.apply(context.Background(), pending)
	return nil
}

// apply runs each pending backfill sequentially and records it on success. A
// failure is logged and the remaining backfills still run; running sequentially
// in one worker avoids SQLite write-lock contention.
func (s *service) apply(ctx context.Context, pending []Backfill) {
	for _, b := range pending {
		if err := b.Run(ctx); err != nil {
			zlog.Error().Err(err).Str("backfill", b.Name).Msg("[backfill] failed")
			continue
		}
		if err := s.repo.SystemRecordBackfill(ctx, b.Name); err != nil {
			zlog.Error().Err(err).Str("backfill", b.Name).Msg("[backfill] applied but failed to record")
			continue
		}
		zlog.Info().Str("backfill", b.Name).Msg("[backfill] applied")
	}
}
