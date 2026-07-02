package telemetry

import (
	"context"
	"testing"
	"time"

	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

type memDBConn struct{ db *gorm.DB }

func (m *memDBConn) Conn(ctx context.Context) *gorm.DB { return m.db }
func (m *memDBConn) Close(ctx context.Context)         {}

// Close must drain queued records and flush them, so shutdown does not silently drop
// up to a batch interval of telemetry.
func TestClose_DrainsAndFlushes(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.AutoMigrate(&model.Telemetry{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	c := &controller{
		db:        &memDBConn{db: db},
		queue:     make(chan model.Telemetry, 100),
		stop:      make(chan struct{}),
		batchSize: 1000,      // large, so only Close triggers the flush
		interval:  time.Hour, // long, so the ticker never fires during the test
	}
	c.wg.Add(1)
	go c.worker()

	const n = 5
	for i := 0; i < n; i++ {
		c.queue <- model.Telemetry{UserID: 1, WorkspaceID: 10, OccurredAt: int64(i), Action: 1}
	}

	c.Close() // must block until the buffer (and anything still queued) is flushed

	var count int64
	db.Model(&model.Telemetry{}).Count(&count)
	if count != n {
		t.Fatalf("flushed %d telemetry rows, want %d (records lost on shutdown)", count, n)
	}

	c.Close() // idempotent: must not panic or hang
}
