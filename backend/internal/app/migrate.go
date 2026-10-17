// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"fmt"

	zlog "github.com/rs/zerolog/log"
	"gorm.io/gorm"

	"github.com/agentrq/agentrq/backend/internal/data/model"
)

// migrationLockKey is the Postgres advisory lock every instance holds while it
// migrates: "agentrq" in ASCII. Any constant would do, as long as it never
// changes, since instances of two releases must agree on it.
const migrationLockKey int64 = 0x6167656e747271

// withMigrationLock runs migrate while holding the migration lock, so instances
// starting together migrate one at a time: GORM checks for a column and then
// adds it, and two instances doing that at once leave the loser failing with
// "already exists". The one that waited then finds nothing left to do.
//
// It all runs on one pinned connection, which holds the lock: a migration on
// any other connection would deadlock a pool of one. A holder that dies takes
// its session, and the lock, with it. SQLite is a single host, so it has
// nothing to lock.
func withMigrationLock(db *gorm.DB, migrate func(conn *gorm.DB) error) error {
	if db.Dialector.Name() != "postgres" {
		return migrate(db)
	}
	return db.Connection(func(pinned *gorm.DB) error {
		// A fresh statement per call that keeps the pinned connection.
		conn := pinned.Session(&gorm.Session{NewDB: true})
		zlog.Info().Msg("migrate: taking the migration lock; this waits while another instance migrates")
		if err := conn.Exec("SELECT pg_advisory_lock(?)", migrationLockKey).Error; err != nil {
			return fmt.Errorf("take the migration lock: %w", err)
		}
		defer conn.Exec("SELECT pg_advisory_unlock(?)", migrationLockKey)
		return migrate(conn)
	})
}

// migrateSchema brings the schema up to date with the models, and clears what
// earlier releases left behind.
func migrateSchema(conn *gorm.DB) error {
	// Drop old single-column unique index on push_subscriptions.endpoint — replaced
	// by composite uniqueIndex(endpoint, workspace_id) to allow one subscription per
	// browser per workspace.
	_ = conn.Exec("DROP INDEX IF EXISTS uni_push_subscriptions_endpoint").Error

	// idx_events_name_user_id / idx_workflows_name_user_id were created over
	// `name` alone despite their names, because only the Name field carried
	// the uniqueIndex tag. That made event and workflow names unique across
	// every account rather than per user. AutoMigrate will not redefine an
	// index that already exists, so the stale ones are dropped here and
	// recreated as the composite the model now declares. Dropping is safe:
	// the new index is strictly looser, so any data valid under the old one
	// is valid under the new one.
	_ = conn.Exec("DROP INDEX IF EXISTS idx_events_name_user_id").Error
	_ = conn.Exec("DROP INDEX IF EXISTS idx_workflows_name_user_id").Error

	// idx_tool_calls_task_id_id, over (task_id, id), pages a task's tool calls
	// and covers every query the task_id index served.
	_ = conn.Exec("DROP INDEX IF EXISTS idx_tool_calls_task_id").Error

	if err := conn.AutoMigrate(migratedModels()...); err != nil {
		return err
	}

	// Sessions that have finished are removed as they finish, but rows written
	// before that was true are still here — and they are what turns a machine
	// page into a list of everything that has ever run on it. A session row is
	// operational state, "what is running here", so a finished one has no
	// reader; the record of what happened is the audit log.
	//
	// Safe to repeat, and it only ever matches rows whose process is over.
	_ = conn.
		Where("status IN ?", []string{"exited", "killed", "failed"}).
		Delete(&model.Session{}).Error

	return nil
}
