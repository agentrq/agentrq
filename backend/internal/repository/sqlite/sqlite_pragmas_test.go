package sqlite

import (
	"path/filepath"
	"strings"
	"testing"

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
			[]string{"file:./_storage/agentrq.db?", "_pragma=journal_mode(WAL)", "_pragma=busy_timeout(5000)"}, false},
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
