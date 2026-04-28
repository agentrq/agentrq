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
