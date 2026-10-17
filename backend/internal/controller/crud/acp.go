// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package crud

import (
	"context"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
)

// ACPController counts native ACP connections and their turns.
type ACPController interface {
	RecordACPV1(ctx context.Context, req entity.RecordACPV1Request)
}

// RecordACPV1 counts a native ACP v1 connection coming up, a turn it ran, or
// the agent logging out.
func (c *controller) RecordACPV1(ctx context.Context, req entity.RecordACPV1Request) {
	if req.UserID == 0 {
		return
	}
	action := entity.ActionACPV1Connect
	switch {
	case req.Turn:
		action = entity.ActionACPV1Turn
	case req.Logout:
		action = entity.ActionACPV1Logout
	}
	c.emitEvent(ctx, entity.CRUDEvent{
		Action:       action,
		WorkspaceID:  req.WorkspaceID,
		UserID:       req.UserID,
		ResourceType: entity.ResourceWorkspace,
		ResourceID:   req.WorkspaceID,
		Actor:        entity.ActorAgent,
	})
}
