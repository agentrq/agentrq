// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package model

import "time"

// RevokedWorkspace is a workspace id that names no workspace any more: the
// workspace was deleted, or its id reset. A supervisor launch token naming it
// is refused. Not in workspaceIDTables: a reset must not move it.
type RevokedWorkspace struct {
	ID        int64     `gorm:"primaryKey;autoIncrement:false"`
	RevokedAt time.Time `gorm:"not null;index"`
}
