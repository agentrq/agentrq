// Package backfill defines the application's one-time data migrations and runs
// them through the backfill service. The migrations themselves are domain logic
// over the repository; the generic run-once engine lives in
// internal/service/backfill.
package backfill

import (
	"context"

	backfillsvc "github.com/agentrq/agentrq/backend/internal/service/backfill"

	"github.com/agentrq/agentrq/backend/internal/repository/base"
)

// Controller registers and runs the application's backfills.
type Controller interface {
	Run(ctx context.Context) error
}

type controller struct {
	repo base.Repository
	svc  backfillsvc.Backfiller
}

// New builds the backfill controller. It leverages the backfill service to run
// the registered migrations at most once each.
func New(repo base.Repository, svc backfillsvc.Backfiller) Controller {
	return &controller{repo: repo, svc: svc}
}

func (c *controller) Run(ctx context.Context) error {
	return c.svc.Run(ctx, c.backfills())
}

// backfills lists the data migrations to apply. Append entries with a new
// stable name; never rename or reuse an existing name (it is the idempotency
// key recorded in the backfills table). Names are prefixed with the YYMMDD date
// they were added so the list stays chronologically ordered. Each migration
// MUST be idempotent.
func (c *controller) backfills() []backfillsvc.Backfill {
	return []backfillsvc.Backfill{
		{Name: "260627_camelcase_message_metadata_keys", Run: c.backfillMessageMetadataKeys},
	}
}
