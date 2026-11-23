// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package sqlite

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestWithConcurrencyPragmas(t *testing.T) {
	cases := []struct {
		name          string
		in            string
		wantContains  []string
		wantUnchanged bool
	}{
		{"bare path gets file scheme + pragmas", "./_storage/agentrq.db",
			[]string{"file:./_storage/agentrq.db?", "_pragma=journal_mode(WAL)", "_pragma=busy_timeout(5000)", "_txlock=immediate"}, false},
		{"existing query uses & separator", "file:./x.db?cache=shared",
			[]string{"cache=shared", "&_pragma=journal_mode(WAL)"}, false},
		{"explicit pragmas are respected", "file:./x.db?_pragma=journal_mode(DELETE)", nil, true},
		{"in-memory untouched", ":memory:", nil, true},
		{"empty untouched", "", nil, true},
	}
	for _, c := range cases {
		got := withConcurrencyPragmas(c.in)
		if c.wantUnchanged {
			if got != c.in {
				t.Errorf("%s: got %q, want unchanged %q", c.name, got, c.in)
			}
			continue
		}
		for _, w := range c.wantContains {
			if !strings.Contains(got, w) {
				t.Errorf("%s: %q missing %q", c.name, got, w)
			}
		}
	}
}

// The produced DSN must actually put a real database into WAL mode with a busy timeout.
func TestWithConcurrencyPragmas_EnablesWALOnRealDB(t *testing.T) {
	dsn := withConcurrencyPragmas(filepath.Join(t.TempDir(), "wal.db"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()

	var journal string
	db.Raw("PRAGMA journal_mode").Scan(&journal)
	if !strings.EqualFold(journal, "wal") {
		t.Fatalf("journal_mode = %q, want wal", journal)
	}
	var busy int
	db.Raw("PRAGMA busy_timeout").Scan(&busy)
	if busy != 5000 {
		t.Fatalf("busy_timeout = %d, want 5000", busy)
	}
}

// A transaction that reads and then writes, while another connection writes
// in between, must wait for that writer rather than fail with SQLITE_BUSY:
// SQLite will not upgrade a deferred transaction's read to a write after
// another write, and skips the busy timeout when it refuses.
func TestWithConcurrencyPragmas_ReadThenWriteWaitsForAnotherWriter(t *testing.T) {
	dsn := withConcurrencyPragmas(filepath.Join(t.TempDir(), "busy.db"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()
	if err := db.Exec("CREATE TABLE rows (id INTEGER PRIMARY KEY, n INTEGER)").Error; err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := db.Exec("INSERT INTO rows (id, n) VALUES (1, 0), (2, 0)").Error; err != nil {
		t.Fatalf("insert: %v", err)
	}

	other := make(chan error, 1)
	err = db.Transaction(func(tx *gorm.DB) error {
		var n int
		if err := tx.Raw("SELECT n FROM rows WHERE id = 1").Scan(&n).Error; err != nil {
			return err
		}
		go func() { other <- db.Exec("UPDATE rows SET n = n + 1 WHERE id = 2").Error }()
		time.Sleep(200 * time.Millisecond)
		return tx.Exec("UPDATE rows SET n = ? WHERE id = 1", n+1).Error
	})
	if err != nil {
		t.Fatalf("the read-then-write transaction: %v", err)
	}
	select {
	case err := <-other:
		if err != nil {
			t.Fatalf("the other writer: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the other writer never finished")
	}
}
