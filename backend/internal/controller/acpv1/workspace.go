// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package acpv1

import (
	"context"
	"errors"
)

// TokenAuth resolves a bearer token to the workspace and user it was minted for. app implements it from the token service and the CRUD controller (Task 6).
type TokenAuth interface {
	WorkspaceForToken(ctx context.Context, token string) (workspaceID int64, userID string, err error)
}

var (
	ErrUnauthorized = errors.New("not a workspace access token")
	ErrForbidden    = errors.New("no access to this workspace")
	ErrArchived     = errors.New("workspace is archived")
)
