// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// Every model migrates, and the task latency tables are among them: without
// them, closing a task fails, since its latency is written in the same
// transaction.
func TestMigratedModels(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.AutoMigrate(migratedModels()...); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	for _, table := range []string{"task_latencies", "hourly_task_latencies", "daily_task_latencies", "monthly_task_latencies", "task_state_transitions", "pano_docs", "pano_files", "pano_comments"} {
		if !db.Migrator().HasTable(table) {
			t.Errorf("%s is not migrated", table)
		}
	}
}
