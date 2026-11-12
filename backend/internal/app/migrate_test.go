// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"strings"
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
	for _, table := range []string{"task_latencies", "hourly_task_latencies", "daily_task_latencies", "monthly_task_latencies", "task_state_transitions", "pano_docs", "pano_files", "pano_comments", "hooks", "hook_registrations", "hook_deliveries", "hook_delivery_keys"} {
		if !db.Migrator().HasTable(table) {
			t.Errorf("%s is not migrated", table)
		}
	}
}

// The hooks' queue is claimed, and each registration's log read newest first,
// through composite indexes; a tag missing on one column builds an index on the
// other alone, which still exists by name.
func TestHookIndexesCoverTheirColumns(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.AutoMigrate(migratedModels()...); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	for name, cols := range map[string]string{
		"idx_hook_deliveries_registration_id_id":     "(`registration_id`,`id`)",
		"idx_hook_deliveries_status_next_attempt_at": "(`status`,`next_attempt_at`)",
		"idx_hook_deliveries_received_at":            "(`received_at`)",
		"idx_hook_delivery_keys_created_at":          "(`created_at`)",
		"idx_hook_registrations_user_id":             "(`user_id`)",
		"idx_hook_registrations_hook_id":             "(`hook_id`)",
		"uk_hooks_user_id_name":                      "(`user_id`,`name`)",
	} {
		var ddl string
		if err := db.Raw("SELECT sql FROM sqlite_master WHERE type = 'index' AND name = ?", name).Scan(&ddl).Error; err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(ddl, cols) {
			t.Errorf("%s = %q, want it on %s", name, ddl, cols)
		}
	}
}
