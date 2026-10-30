// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package sqlite

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	configmocks "github.com/agentrq/agentrq/backend/internal/service/mocks/config"
	"github.com/glebarez/sqlite"
	"github.com/golang/mock/gomock"
	"gorm.io/gorm"
)

func mockConfig(t *testing.T, cfg sqliteConfig, err error) *configmocks.MockService {
	t.Helper()
	m := configmocks.NewMockService(gomock.NewController(t))
	m.EXPECT().Populate("sqlite", gomock.Any()).DoAndReturn(func(_ string, v any) error {
		*v.(*sqliteConfig) = cfg
		return err
	})
	return m
}

func TestNewConfigError(t *testing.T) {
	want := errors.New("sqlite section is not valid yaml")
	if _, err := New(Params{Config: mockConfig(t, sqliteConfig{}, want)}); !errors.Is(err, want) {
		t.Errorf("got %v, want the config error", err)
	}
}

// A disabled SQLite is no connection and no error: Postgres is in use.
func TestNewIsDisabled(t *testing.T) {
	conn, err := New(Params{Config: mockConfig(t, sqliteConfig{}, nil)})
	if conn != nil || err != nil {
		t.Errorf("got %v, %v; want nil, nil", conn, err)
	}
}

func TestNewAppliesThePoolLimits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agentrq.db")
	conn, err := New(Params{Config: mockConfig(t, sqliteConfig{Enabled: true, DSN: path, MaxIdleConns: 2, MaxOpenConns: 3, MaxConnLifetime: time.Minute}, nil)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer conn.Close(t.Context())
	sqlDB, err := conn.Conn(t.Context()).DB()
	if err != nil {
		t.Fatal(err)
	}
	if got := sqlDB.Stats().MaxOpenConnections; got != 3 {
		t.Errorf("max open connections = %d, want 3", got)
	}
}

// A database file that cannot be opened is reported, not left for the first
// query to find.
func TestNewReportsADatabaseItCannotOpen(t *testing.T) {
	dir := t.TempDir() // a folder, where the database file should be
	_, err := New(Params{Config: mockConfig(t, sqliteConfig{Enabled: true, DSN: dir}, nil)})
	if err == nil || !strings.Contains(err.Error(), "failed to connect") {
		t.Fatalf("New = %v, want a connect error", err)
	}
}

// noPool is a connection pool that is not a *sql.DB, so the database handle
// behind it cannot be reached.
type noPool struct{ gorm.ConnPool }

func TestCloseWithoutADatabaseHandle(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()
	db.ConnPool = noPool{}
	db.Statement.ConnPool = db.ConnPool
	(&repository{db: db}).Close(t.Context()) // must not panic
	if err := sqlDB.Ping(); err != nil {
		t.Errorf("Close reached a database it had no handle to: %v", err)
	}
}
