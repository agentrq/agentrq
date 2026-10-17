// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/agentrq/agentrq/backend/internal/service/config"
)

// postgresNamed is a SQLite database that calls itself postgres, so the lock
// path runs without a server, and fails where a real one would not.
type postgresNamed struct{ gorm.Dialector }

func (postgresNamed) Name() string { return "postgres" }

func openPostgresNamed(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(postgresNamed{sqlite.Open(":memory:")}, &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return db
}

func TestWithMigrationLockOnSQLiteJustMigrates(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	var got *gorm.DB
	if err := withMigrationLock(db, func(conn *gorm.DB) error { got = conn; return nil }); err != nil {
		t.Fatalf("withMigrationLock: %v", err)
	}
	if got != db {
		t.Error("SQLite should migrate on the database itself")
	}
}

func TestWithMigrationLockFailsToTakeTheLock(t *testing.T) {
	ran := false
	err := withMigrationLock(openPostgresNamed(t), func(*gorm.DB) error { ran = true; return nil })
	if err == nil || !strings.Contains(err.Error(), "take the migration lock") {
		t.Fatalf("err = %v, want the lock failure", err)
	}
	if ran {
		t.Error("migrated without the lock")
	}
}

func TestWithMigrationLockWithoutAConnection(t *testing.T) {
	db := openPostgresNamed(t)
	sqlDB, _ := db.DB()
	sqlDB.Close()
	ran := false
	if err := withMigrationLock(db, func(*gorm.DB) error { ran = true; return nil }); err == nil {
		t.Fatal("want an error from a closed database")
	}
	if ran {
		t.Error("migrated without a connection")
	}
}

func TestMigrateSchemaReportsAFailedMigration(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.Close()
	if err := migrateSchema(db); err == nil {
		t.Fatal("want the AutoMigrate error")
	}
}

// A schema AutoMigrate cannot bring up to date stops the app from starting.
func TestNewStopsWhenTheMigrationFails(t *testing.T) {
	dir := t.TempDir()
	dsn := filepath.Join(dir, "agentrq.db")
	seed, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// A view named like a table: AutoMigrate finds no table and fails creating it.
	if err := seed.Exec("CREATE VIEW workspaces AS SELECT 1 AS id").Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	sqlDB, _ := seed.DB()
	sqlDB.Close()

	t.Setenv("ENV", "development")
	t.Setenv("AGENTRQ_AUTH_JWT_SECRET", "migrate-test-secret")
	t.Setenv("AGENTRQ_SQLITE_DSN", dsn)
	t.Setenv("AGENTRQ_STORAGE_DIR", dir)
	t.Chdir("../../cmd/server")
	svc, err := config.New()
	if err != nil {
		t.Fatal(err)
	}
	var cfg Config
	for key, dst := range map[string]any{"app": &cfg.App, "auth": &cfg.Auth, "storage": &cfg.Storage, "idgen": &cfg.Idgen} {
		if err := svc.Populate(key, dst); err != nil {
			t.Fatalf("config %s: %v", key, err)
		}
	}
	cfg.ConfigSvc = svc
	if _, err := New(cfg); err == nil || !strings.Contains(err.Error(), "migrate db") {
		t.Fatalf("New err = %v, want a migrate db error", err)
	}
}

// postgresDSN is the server AGENTRQ_TEST_POSTGRES_DSN names, or a skip.
func postgresDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("AGENTRQ_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("AGENTRQ_TEST_POSTGRES_DSN is not set")
	}
	return dsn
}

// freshPostgres creates an empty database, so a migration starts from nothing.
func freshPostgres(t *testing.T) string {
	t.Helper()
	dsn := postgresDSN(t)
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	name := fmt.Sprintf("migrate_lock_%d", time.Now().UnixNano())
	if err := admin.Exec("CREATE DATABASE " + name).Error; err != nil {
		t.Fatalf("create database: %v", err)
	}
	t.Cleanup(func() {
		admin.Exec("DROP DATABASE IF EXISTS " + name + " WITH (FORCE)")
		sqlDB, _ := admin.DB()
		sqlDB.Close()
	})
	return dsn + " dbname=" + name
}

func openPostgres(t *testing.T, dsn string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		sqlDB.Close()
	})
	return db
}

func tryMigrationLock(t *testing.T, db *gorm.DB) bool {
	t.Helper()
	var got bool
	if err := db.Raw("SELECT pg_try_advisory_lock(?)", migrationLockKey).Scan(&got).Error; err != nil {
		t.Fatalf("try lock: %v", err)
	}
	if got {
		db.Exec("SELECT pg_advisory_unlock(?)", migrationLockKey)
	}
	return got
}

// The migration runs on the connection holding the lock, and the lock is
// released once it is done.
func TestWithMigrationLockPinsOneConnection(t *testing.T) {
	dsn := freshPostgres(t)
	db, other := openPostgres(t, dsn), openPostgres(t, dsn)

	err := withMigrationLock(db, func(conn *gorm.DB) error {
		var first, second int
		conn.Raw("SELECT pg_backend_pid()").Scan(&first)
		conn.Raw("SELECT pg_backend_pid()").Scan(&second)
		if first == 0 || first != second {
			t.Errorf("statements ran on backends %d and %d, want one", first, second)
		}
		if tryMigrationLock(t, other) {
			t.Error("another instance took the lock while this one migrated")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("withMigrationLock: %v", err)
	}
	if !tryMigrationLock(t, other) {
		t.Error("the lock was not released after the migration")
	}
}

// A failed migration still releases the lock, so the next instance can try.
func TestWithMigrationLockReleasesOnFailure(t *testing.T) {
	dsn := freshPostgres(t)
	db, other := openPostgres(t, dsn), openPostgres(t, dsn)
	failed := errors.New("column already exists")
	if err := withMigrationLock(db, func(*gorm.DB) error { return failed }); !errors.Is(err, failed) {
		t.Fatalf("err = %v, want %v", err, failed)
	}
	if !tryMigrationLock(t, other) {
		t.Error("the lock was not released after a failed migration")
	}
}

// Instances starting together on an empty database all come up.
func TestConcurrentInstancesMigrateOneAtATime(t *testing.T) {
	const instances = 4
	dsn := freshPostgres(t)
	dbs := make([]*gorm.DB, instances)
	for i := range dbs {
		dbs[i] = openPostgres(t, dsn)
	}
	var wg sync.WaitGroup
	errs := make([]error, instances)
	for i, db := range dbs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = withMigrationLock(db, migrateSchema)
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("instance %d: %v", i, err)
		}
	}
	if !dbs[0].Migrator().HasTable("tasks") {
		t.Error("tasks was not migrated")
	}
}
