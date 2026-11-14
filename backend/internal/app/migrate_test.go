// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/data/model"
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
	for _, table := range []string{"task_latencies", "hourly_task_latencies", "daily_task_latencies", "monthly_task_latencies", "task_state_transitions", "pano_docs", "pano_files", "pano_comments", "hooks", "hook_registrations", "hook_deliveries", "hook_delivery_keys", "teams", "team_members", "team_invitations", "workspace_shares", "team_management_activities"} {
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

// A database migrated before the token was hashed has a NOT NULL token
// column no insert fills; migrating drops it, and keeps the rest.
func TestMigrateDropsThePlainHookToken(t *testing.T) {
	// A file: every pooled connection to ":memory:" is a database of its own.
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "a.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	// The table as the release before the hash made it.
	type legacy struct {
		model.HookRegistration
		Token string `gorm:"type:char(8);not null"`
	}
	if err := db.Table("hook_registrations").AutoMigrate(&legacy{}); err != nil {
		t.Fatal(err)
	}
	if err := migrateSchema(db); err != nil {
		t.Fatal(err)
	}
	m := db.Migrator()
	if m.HasColumn(&model.HookRegistration{}, "token") || !m.HasColumn(&model.HookRegistration{}, "token_hash") {
		t.Error("the plain token column is still there, or the hash is missing")
	}
	if err := db.Create(&model.HookRegistration{ID: 1, Name: "n", TokenHash: "h"}).Error; err != nil {
		t.Errorf("insert after migrating: %v", err)
	}
	// A second run finds nothing to drop.
	if err := migrateSchema(db); err != nil {
		t.Fatal(err)
	}
}

// A database migrated while teams had roles, share labels and a memory file
// has NOT NULL role columns no insert fills; migrating drops them, the label
// and the memory table, and keeps the rows.
func TestMigrateDropsTeamRolesLabelsAndMemory(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "a.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	type member struct {
		model.TeamMember
		Role string `gorm:"type:varchar(16);not null"`
	}
	type invitation struct {
		model.TeamInvitation
		Role string `gorm:"type:varchar(16);not null"`
	}
	type share struct {
		model.WorkspaceShare
		GroupName string `gorm:"type:varchar(128);not null"`
	}
	type memory struct {
		ID      int64 `gorm:"primaryKey"`
		Content string
	}
	for table, m := range map[string]any{"team_members": &member{}, "team_invitations": &invitation{}, "workspace_shares": &share{}, "team_memories": &memory{}} {
		if err := db.Table(table).AutoMigrate(m); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Table("team_members").Create(&member{TeamMember: model.TeamMember{ID: 1, TeamID: 2, UserID: 3}, Role: "owner"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := migrateSchema(db); err != nil {
		t.Fatal(err)
	}
	m := db.Migrator()
	if m.HasColumn(&model.TeamMember{}, "role") || m.HasColumn(&model.TeamInvitation{}, "role") || m.HasColumn(&model.WorkspaceShare{}, "group_name") {
		t.Error("a role or group column is still there")
	}
	if m.HasTable("team_memories") {
		t.Error("team_memories is still there")
	}
	var n int64
	if err := db.Model(&model.TeamMember{}).Where("id = 1").Count(&n).Error; err != nil || n != 1 {
		t.Errorf("the member row is gone: %d, %v", n, err)
	}
	if err := db.Create(&model.TeamMember{ID: 4, TeamID: 2, UserID: 5}).Error; err != nil {
		t.Errorf("insert a member after migrating: %v", err)
	}
	if err := migrateSchema(db); err != nil {
		t.Fatal(err)
	}
}

// A column that cannot be dropped fails the migration rather than leaving a
// NOT NULL role behind to refuse every new member.
func TestMigrateFailsWhenARoleCannotBeDropped(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "a.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := migrateSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ALTER TABLE team_members ADD COLUMN role varchar(16) NOT NULL DEFAULT 'owner'").Error; err != nil {
		t.Fatal(err)
	}
	full := errors.New("disk full")
	if err := db.Callback().Raw().Before("gorm:raw").Register("fail_drop_role", func(tx *gorm.DB) {
		if sql := strings.ToLower(tx.Statement.SQL.String()); strings.Contains(sql, "team_members") && strings.Contains(sql, "drop") {
			_ = tx.AddError(full)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := migrateSchema(db); !errors.Is(err, full) {
		t.Errorf("migrate = %v, want the failed drop", err)
	}
}
