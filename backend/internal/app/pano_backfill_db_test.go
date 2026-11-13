// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"bytes"
	"context"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/rs/zerolog"
	zlog "github.com/rs/zerolog/log"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
)

type countingBackfill struct{ runs atomic.Int32 }

func (c *countingBackfill) BackfillPanoDocTasks(context.Context) (int64, error) {
	c.runs.Add(1)
	return 0, nil
}

// The doc-task backfill's claim goes into the real table, where Postgres
// enforces the column's length. Several instances
// booting at once run it exactly once, on SQLite always and on Postgres when
// AGENTRQ_TEST_POSTGRES_DSN is set.
func TestBackfillPanoDocTasksClaimsOnceInTheDatabase(t *testing.T) {
	dialectors := map[string]gorm.Dialector{"sqlite": sqlite.Open("file::memory:")}
	if dsn := os.Getenv("AGENTRQ_TEST_POSTGRES_DSN"); dsn != "" {
		dialectors["postgres"] = postgres.Open(dsn)
	} else {
		t.Log("AGENTRQ_TEST_POSTGRES_DSN is not set; running on SQLite only")
	}
	for name, d := range dialectors {
		t.Run(name, func(t *testing.T) {
			db, err := gorm.Open(d, &gorm.Config{Logger: logger.Discard})
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			t.Cleanup(func() {
				if sqlDB, err := db.DB(); err == nil {
					_ = sqlDB.Close()
				}
			})
			if name == "sqlite" {
				// Every connection to :memory: is a database of its own.
				sqlDB, _ := db.DB()
				sqlDB.SetMaxOpenConns(1)
			}
			if err := db.Migrator().DropTable(&model.TelemetryAggregation{}); err != nil {
				t.Fatalf("drop: %v", err)
			}
			if err := db.AutoMigrate(&model.TelemetryAggregation{}); err != nil {
				t.Fatalf("migrate: %v", err)
			}
			var buf bytes.Buffer
			old := zlog.Logger
			zlog.Logger = zerolog.New(zerolog.SyncWriter(&buf))
			t.Cleanup(func() { zlog.Logger = old })
			var backfill countingBackfill
			var wg sync.WaitGroup
			for range 4 {
				repo := base.New(gormConn{db})
				wg.Add(1)
				go func() {
					defer wg.Done()
					backfillPanoDocTasks(context.Background(), repo, &backfill)
				}()
			}
			wg.Wait()
			// A later boot finds it claimed and leaves it.
			backfillPanoDocTasks(context.Background(), base.New(gormConn{db}), &backfill)
			if n := backfill.runs.Load(); n != 1 {
				t.Errorf("the backfill ran %d times, want once; logged %q", n, buf.String())
			}
			if strings.Contains(buf.String(), "failed to claim") {
				t.Errorf("the claim failed: %s", buf.String())
			}
		})
	}
}
