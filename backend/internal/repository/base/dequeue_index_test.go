package base

import (
	"testing"

	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// The agent dequeue filters on workspace_id, user_id and status. Assert AutoMigrate
// creates idx_tasks_dequeue covering that equality prefix in query order. assignee and
// sort_order are deliberately left out to keep the index small.
func TestTaskDequeueCompositeIndex(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.AutoMigrate(&model.Task{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	rows, err := db.Raw("PRAGMA index_info('idx_tasks_dequeue')").Rows()
	if err != nil {
		t.Fatalf("index_info: %v", err)
	}
	defer rows.Close()

	var cols []string
	for rows.Next() {
		var seqno, cid int
		var name string
		if err := rows.Scan(&seqno, &cid, &name); err != nil {
			t.Fatalf("scan: %v", err)
		}
		cols = append(cols, name)
	}

	want := []string{"workspace_id", "user_id", "status"}
	if len(cols) != len(want) {
		t.Fatalf("idx_tasks_dequeue columns = %v, want %v (index missing or wrong shape)", cols, want)
	}
	for i := range want {
		if cols[i] != want[i] {
			t.Fatalf("idx_tasks_dequeue col[%d] = %q, want %q (full: %v)", i, cols[i], want[i], cols)
		}
	}
}
