// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package base

import (
	"strings"
	"sync"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/glebarez/sqlite"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// A 4-byte float holds a Unix-seconds sort order only to the nearest 128 s,
// which is what made a card dragged on the board snap back on Postgres.
func TestTaskSortOrderColumnIsNotSinglePrecisionOnPostgres(t *testing.T) {
	s, err := schema.Parse(&model.Task{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	field := s.LookUpField("SortOrder")
	if field == nil {
		t.Fatal("no SortOrder field")
	}
	typ := strings.ToLower(postgres.Dialector{Config: &postgres.Config{}}.DataTypeOf(field))
	for _, narrow := range []string{"real", "float4"} {
		if typ == narrow {
			t.Fatalf("sort_order is %q on Postgres, a 4-byte float", typ)
		}
	}
}

// SQLite keeps its existing 8-byte `real` column, so upgrading does not
// rebuild the tasks table, and sub-second positions survive a round trip.
func TestTaskSortOrderKeepsSubSecondPrecisionOnSQLite(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.AutoMigrate(&model.Task{}, &model.TaskStateTransition{}, &model.TaskLatency{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	var colType string
	if err := db.Raw("SELECT type FROM pragma_table_info('tasks') WHERE name = 'sort_order'").Scan(&colType).Error; err != nil {
		t.Fatalf("table_info: %v", err)
	}
	if strings.ToLower(colType) != "real" {
		t.Fatalf("sort_order column type = %q, want real", colType)
	}

	const order = 1790000015.3115
	if err := db.Create(&model.Task{ID: 1, SortOrder: order}).Error; err != nil {
		t.Fatalf("create: %v", err)
	}
	var got model.Task
	if err := db.First(&got, 1).Error; err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.SortOrder != order {
		t.Fatalf("sort order = %f, want %f", got.SortOrder, order)
	}
}
