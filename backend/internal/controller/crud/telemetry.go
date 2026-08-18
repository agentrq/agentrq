package crud

import (
	"context"
	"fmt"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/mustafaturan/monoflake"
)

// TelemetryController records client-reported feature usage.
//
// Everything else on this topic is emitted by the backend right after it does
// the work being counted, which makes the event self-evidently true. These
// actions happen entirely in the user's browser — local models running in a
// tab — so the report is the only evidence there is, and the controller has to
// supply the trust the emission site normally would: a caller may only report
// an allowlisted action, only against a workspace it owns, and only so often.
type TelemetryController interface {
	RecordTelemetry(ctx context.Context, req entity.RecordTelemetryRequest) error
}

// ErrForbidden reports a caller acting on a workspace it does not own.
var ErrForbidden = fmt.Errorf("access denied")

func (c *controller) RecordTelemetry(ctx context.Context, req entity.RecordTelemetryRequest) error {
	userID := monoflake.IDFromBase62(req.UserID).Int64()
	if userID == 0 || req.WorkspaceID == 0 {
		return fmt.Errorf("invalid telemetry request")
	}

	if c.limiter != nil && !c.limiter.AllowTelemetry(userID) {
		return fmt.Errorf("rate limit exceeded")
	}

	// Without this a caller could attribute its own clicks to someone else's
	// workspace, which is the one thing that would corrupt a per-workspace
	// metric for a user who never generated it.
	ok, err := c.CheckWorkspaceAccess(ctx, req.WorkspaceID, req.UserID)
	if err != nil {
		return fmt.Errorf("check workspace access: %w", err)
	}
	if !ok {
		return ErrForbidden
	}

	c.emitEvent(ctx, entity.CRUDEvent{
		Action:      req.Action,
		WorkspaceID: req.WorkspaceID,
		UserID:      userID,
		Actor:       entity.ActorHuman,
	})
	return nil
}
