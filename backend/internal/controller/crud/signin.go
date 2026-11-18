// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package crud

import (
	"context"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
)

// SignInController counts people signing in and out.
type SignInController interface {
	RecordSignIn(ctx context.Context, req entity.RecordSignInRequest)
}

// RecordSignIn counts a sign-in, or a sign-out, against the account.
func (c *controller) RecordSignIn(ctx context.Context, req entity.RecordSignInRequest) {
	if req.UserID == 0 {
		return
	}
	action := entity.ActionUserSignIn
	if req.SignOut {
		action = entity.ActionUserSignOut
	}
	c.emitEvent(ctx, entity.CRUDEvent{
		Action:       action,
		UserID:       req.UserID,
		ResourceType: entity.ResourceUser,
		ResourceID:   req.UserID,
		Actor:        entity.ActorHuman,
	})
}
