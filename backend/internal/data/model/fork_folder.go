// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package model

import "time"

// ForkFolder is a machine holding a fork's folder. The session that made it is
// deleted when it ends, so this row is what lets a merge find the folder again.
type ForkFolder struct {
	WorkspaceID int64 `gorm:"primaryKey;autoIncrement:false"`
	MachineID   int64 `gorm:"primaryKey;autoIncrement:false;index"`
	UserID      int64 `gorm:"not null;index"`
	CreatedAt   time.Time
}
