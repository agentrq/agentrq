// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package postgres

import (
	"context"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// noPool is a connection pool that is not a *sql.DB, so the database handle
// behind it cannot be reached.
type noPool struct{ gorm.ConnPool }

// noHandleDB opens a database whose handle cannot be reached.
func noHandleDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	db.ConnPool = noPool{}
	db.Statement.ConnPool = db.ConnPool
	return db
}

func TestNewReportsAMissingDatabaseHandle(t *testing.T) {
	oldOpen := openDB
	t.Cleanup(func() { openDB = oldOpen })
	openDB = func(string) (*gorm.DB, error) { return noHandleDB(t), nil }
	_, err := New(Params{Config: mockConfig(t, postgresConfig{Enabled: true, DSN: "host=db"}, nil)})
	if err == nil || !strings.Contains(err.Error(), "failed to get database connection") {
		t.Fatalf("New = %v, want a database handle error", err)
	}
}

func TestCloseWithoutADatabaseHandle(t *testing.T) {
	(&repository{db: noHandleDB(t)}).Close(context.Background()) // must not panic
}
