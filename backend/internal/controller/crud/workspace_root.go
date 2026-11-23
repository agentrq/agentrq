// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package crud

import (
	"context"

	"github.com/agentrq/agentrq/backend/internal/repository/base"
)

// ContentWorkspaceID is the workspace whose memory, skills and attachment
// files workspaceID uses: its parent for a fork, itself otherwise. A workspace
// userID does not own is not found.
func (c *controller) ContentWorkspaceID(ctx context.Context, workspaceID, userID int64) (int64, error) {
	w, err := c.repository.SystemGetWorkspace(ctx, workspaceID)
	if err != nil {
		return 0, err
	}
	if w.UserID != userID {
		return 0, base.ErrNotFound
	}
	return w.ContentID(), nil
}
