// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package crud

import (
	"context"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
)

// OAuthConsentController counts answers on the OAuth consent page.
type OAuthConsentController interface {
	RecordOAuthConsent(ctx context.Context, req entity.RecordOAuthConsentRequest)
}

// RecordOAuthConsent counts an app allowed, or denied, on the consent page.
func (c *controller) RecordOAuthConsent(ctx context.Context, req entity.RecordOAuthConsentRequest) {
	if req.UserID == 0 {
		return
	}
	action := entity.ActionOAuthConsentAllow
	if !req.Allowed {
		action = entity.ActionOAuthConsentDeny
	}
	resourceType, resourceID := entity.ResourceWorkspace, req.WorkspaceID
	if req.WorkspaceID == 0 {
		resourceType, resourceID = entity.ResourceUser, req.UserID
	}
	c.emitEvent(ctx, entity.CRUDEvent{
		Action:       action,
		WorkspaceID:  req.WorkspaceID,
		UserID:       req.UserID,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		Actor:        entity.ActorHuman,
	})
}
