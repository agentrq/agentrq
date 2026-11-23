// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package dbconn

import (
	"context"

	"gorm.io/gorm"
)

// DBConn is the database connection interface.
// It abstracts the underlying database driver (sqlite, postgres, etc.)
// so that the rest of the application can remain database-agnostic.
type DBConn interface {
	Conn(ctx context.Context) *gorm.DB
	Close(ctx context.Context)
}
