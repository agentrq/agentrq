// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package crud

import (
	"context"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
)

// SiteShareController counts sites shared from the Chrome extension.
type SiteShareController interface {
	RecordSiteShare(ctx context.Context, req entity.RecordSiteShareRequest)
}

// RecordSiteShare counts a site shared into a workspace, or withdrawn from it.
func (c *controller) RecordSiteShare(ctx context.Context, req entity.RecordSiteShareRequest) {
	if req.UserID == 0 {
		return
	}
	action := entity.ActionSiteShare
	if !req.Shared {
		action = entity.ActionSiteUnshare
	}
	c.emitEvent(ctx, entity.CRUDEvent{
		Action:       action,
		WorkspaceID:  req.WorkspaceID,
		UserID:       req.UserID,
		ResourceType: entity.ResourceWorkspace,
		ResourceID:   req.WorkspaceID,
		Actor:        entity.ActorHuman,
	})
}
