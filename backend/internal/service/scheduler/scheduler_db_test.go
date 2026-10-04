// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package scheduler

import (
	"bytes"
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
	"github.com/agentrq/agentrq/backend/internal/service/idgen"
	mock_pubsub "github.com/agentrq/agentrq/backend/internal/service/mocks/pubsub"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
	"github.com/glebarez/sqlite"
	"github.com/golang/mock/gomock"
	"github.com/rs/zerolog"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// These tests run every backend instance's scheduler against one real
// database, which is the only place the duplicate-run guarantees live. They
// use SQLite, and Postgres too when AGENTRQ_TEST_POSTGRES_DSN names a
// throwaway database: its task tables are dropped and recreated.

type testConn struct{ db *gorm.DB }

func (c testConn) Conn(context.Context) *gorm.DB { return c.db }
func (c testConn) Close(context.Context)         {}

type dialect struct {
	name string
	// sqlLogs is what GORM logged on any of its connections: a losing
	// instance must leave nothing here either.
	sqlLogs *bytes.Buffer
	// open returns a new connection to the same database each call, so each
	// instance has its own pool, as separate processes would.
	open func(t *testing.T) *gorm.DB
}

func dialects(t *testing.T) []dialect {
	t.Helper()
	// One file per test, with the pragmas the app adds to a file DSN.
	path := filepath.Join(t.TempDir(), "agentrq.db")
	sqliteLogs := &bytes.Buffer{}
	ds := []dialect{{name: "sqlite", sqlLogs: sqliteLogs, open: func(t *testing.T) *gorm.DB {
		return openDB(t, sqlite.Open("file:"+path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"), sqliteLogs)
	}}}
	if dsn := os.Getenv("AGENTRQ_TEST_POSTGRES_DSN"); dsn != "" {
		pgLogs := &bytes.Buffer{}
		ds = append(ds, dialect{name: "postgres", sqlLogs: pgLogs, open: func(t *testing.T) *gorm.DB {
			return openDB(t, postgres.Open(dsn), pgLogs)
		}})
	} else {
		t.Log("AGENTRQ_TEST_POSTGRES_DSN is not set; running on SQLite only")
	}
	return ds
}

func openDB(t *testing.T, d gorm.Dialector, sqlLogs *bytes.Buffer) *gorm.DB {
	t.Helper()
	// TranslateError, as the app opens its connections: it is what turns
	// either driver's unique violation into gorm.ErrDuplicatedKey. The logger
	// is the app's default one, which prints every failed statement.
	db, err := gorm.Open(d, &gorm.Config{
		TranslateError: true,
		Logger: logger.New(log.New(zerolog.SyncWriter(sqlLogs), "", 0), logger.Config{
			SlowThreshold: time.Hour, LogLevel: logger.Warn, IgnoreRecordNotFoundError: true,
		}),
	})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

var taskTables = []any{&model.Task{}, &model.TaskStateTransition{}, &model.TaskLatency{}}

// freshDB empties the task tables and migrates them as the app does.
func freshDB(t *testing.T, d dialect) *gorm.DB {
	t.Helper()
	db := d.open(t)
	if err := db.Migrator().DropTable(taskTables...); err != nil {
		t.Fatalf("drop tables: %v", err)
	}
	if err := db.AutoMigrate(taskTables...); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

// lateCheck is an instance whose "is a run already waiting?" check ran before
// the other instance's write landed: the interleaving the check cannot stop.
type lateCheck struct{ base.Repository }

func (lateCheck) SystemCheckTaskExists(context.Context, int64, int64, string) (bool, error) {
	return false, nil
}

// instance is one backend's scheduler, with its own connection and node ID,
// counting what it publishes.
type instance struct {
	s         *scheduler
	published atomic.Int32
}

func newInstance(t *testing.T, ctrl *gomock.Controller, repo base.Repository, node uint16, bus *eventbus.Bus) *instance {
	t.Helper()
	ids, err := idgen.New(node)
	if err != nil {
		t.Fatalf("idgen: %v", err)
	}
	in := &instance{}
	ps := mock_pubsub.NewMockService(ctrl)
	ps.EXPECT().Publish(gomock.Any(), gomock.Any()).DoAndReturn(func(context.Context, pubsub.PublishRequest) (*pubsub.PublishResponse, error) {
		in.published.Add(1)
		return &pubsub.PublishResponse{}, nil
	}).AnyTimes()
	in.s = New(repo, ids, bus, ps).(*scheduler)
	return in
}

func seedTemplate(t *testing.T, db *gorm.DB, id int64, schedule string) model.Task {
	t.Helper()
	tmpl := model.Task{
		ID: id, CreatedAt: time.Now(), UpdatedAt: time.Now(), UserID: 1, WorkspaceID: 10,
		CreatedBy: "human", Assignee: "agent", Status: "cron", Title: "nightly", Body: "run it", CronSchedule: schedule,
	}
	if _, err := base.New(testConn{db}).CreateTask(context.Background(), tmpl); err != nil {
		t.Fatalf("seed template: %v", err)
	}
	return tmpl
}

func drain(ch chan []byte) []string {
	var out []string
	for {
		select {
		case line := <-ch:
			out = append(out, string(line))
		default:
			return out
		}
	}
}

func TestTwoInstancesRecurringRun(t *testing.T) {
	for _, d := range dialects(t) {
		t.Run(d.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			db := freshDB(t, d)
			bus := eventbus.New()
			sse := bus.Subscribe(10, "")
			logs := captureLogs(t)
			tmpl := seedTemplate(t, db, 1, "0 9 * * *")

			a := newInstance(t, ctrl, base.New(testConn{db}), 1, bus)
			b := newInstance(t, ctrl, lateCheck{base.New(testConn{d.open(t)})}, 2, bus)
			a.s.spawn(context.Background(), tmpl, spawnAt)
			b.s.spawn(context.Background(), tmpl, spawnAt)

			var children []model.Task
			db.Where("parent_id = ?", tmpl.ID).Find(&children)
			if len(children) != 1 {
				t.Fatalf("expected exactly one run, got %d", len(children))
			}
			if a.published.Load() != 1 || b.published.Load() != 0 {
				t.Errorf("expected only the winner to publish, got a=%d b=%d", a.published.Load(), b.published.Load())
			}
			if events := drain(sse); len(events) != 1 {
				t.Errorf("expected one SSE event, got %d: %v", len(events), events)
			}
			if strings.Contains(logs.String(), `"level":"error"`) || d.sqlLogs.Len() != 0 {
				t.Errorf("losing the race was logged as an error: %s%s", logs.String(), d.sqlLogs.String())
			}
		})
	}
}

// Both instances really at once, many times over: whichever wins, each minute
// has one run and one announcement.
func TestTwoInstancesRecurringRunConcurrently(t *testing.T) {
	const runs = 20
	for _, d := range dialects(t) {
		t.Run(d.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			db := freshDB(t, d)
			bus := eventbus.New()
			logs := captureLogs(t)
			a := newInstance(t, ctrl, lateCheck{base.New(testConn{db})}, 1, bus)
			b := newInstance(t, ctrl, lateCheck{base.New(testConn{d.open(t)})}, 2, bus)

			for i := range runs {
				tmpl := seedTemplate(t, db, int64(i+1), "0 9 * * *")
				var wg sync.WaitGroup
				for _, in := range []*instance{a, b} {
					wg.Go(func() { in.s.spawn(context.Background(), tmpl, spawnAt) })
				}
				wg.Wait()
			}

			var count int64
			db.Model(&model.Task{}).Where("parent_id <> 0").Count(&count)
			if count != runs {
				t.Errorf("expected %d runs, got %d", runs, count)
			}
			if got := a.published.Load() + b.published.Load(); got != runs {
				t.Errorf("expected %d announcements, got %d", runs, got)
			}
			if strings.Contains(logs.String(), `"level":"error"`) || d.sqlLogs.Len() != 0 {
				t.Errorf("losing the race was logged as an error: %s%s", logs.String(), d.sqlLogs.String())
			}
		})
	}
}

func TestTwoInstancesOneTimeRun(t *testing.T) {
	for _, d := range dialects(t) {
		t.Run(d.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			db := freshDB(t, d)
			bus := eventbus.New()
			sse := bus.Subscribe(10, "")
			tmpl := seedTemplate(t, db, 1, "0 9 1 1 *")

			a := newInstance(t, ctrl, base.New(testConn{db}), 1, bus)
			b := newInstance(t, ctrl, lateCheck{base.New(testConn{d.open(t)})}, 2, bus)
			// b still holds the template as it listed it, in cron.
			a.s.spawn(context.Background(), tmpl, spawnAt)
			b.s.spawn(context.Background(), tmpl, spawnAt)

			var tasks []model.Task
			db.Find(&tasks)
			if len(tasks) != 1 {
				t.Fatalf("expected the template alone, no child, got %d tasks", len(tasks))
			}
			got := tasks[0]
			if got.ID != tmpl.ID || got.Status != "notstarted" || got.CronSchedule != "" {
				t.Errorf("expected task %d notstarted with no schedule, got id=%d status=%q schedule=%q", tmpl.ID, got.ID, got.Status, got.CronSchedule)
			}
			if !got.CreatedAt.Equal(spawnAt) {
				t.Errorf("expected created at the scheduled minute %v, got %v", spawnAt, got.CreatedAt)
			}
			if a.published.Load() != 1 || b.published.Load() != 0 {
				t.Errorf("expected only the winner to publish, got a=%d b=%d", a.published.Load(), b.published.Load())
			}
			if d.sqlLogs.Len() != 0 {
				t.Errorf("losing the race was logged: %s", d.sqlLogs.String())
			}
			events := drain(sse)
			if len(events) != 1 || !strings.Contains(events[0], `"type":"task.updated"`) {
				t.Errorf("expected one task.updated event, got %v", events)
			}

			history, err := base.New(testConn{db}).ListTaskStateTransitions(context.Background(), tmpl.ID)
			if err != nil {
				t.Fatalf("list transitions: %v", err)
			}
			if len(history) != 2 || history[1].FromState != model.TaskStateFromStatus("cron") || history[1].ToState != model.TaskStateFromStatus("notstarted") {
				t.Errorf("expected history cron then cron→notstarted once, got %+v", history)
			}
		})
	}
}

func TestTwoInstancesOneTimeRunConcurrently(t *testing.T) {
	const runs = 20
	for _, d := range dialects(t) {
		t.Run(d.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			db := freshDB(t, d)
			bus := eventbus.New()
			a := newInstance(t, ctrl, lateCheck{base.New(testConn{db})}, 1, bus)
			b := newInstance(t, ctrl, lateCheck{base.New(testConn{d.open(t)})}, 2, bus)

			for i := range runs {
				tmpl := seedTemplate(t, db, int64(i+1), "0 9 1 1 *")
				var wg sync.WaitGroup
				for _, in := range []*instance{a, b} {
					wg.Go(func() { in.s.spawn(context.Background(), tmpl, spawnAt) })
				}
				wg.Wait()
			}

			var started int64
			db.Model(&model.Task{}).Where("status = ?", "notstarted").Count(&started)
			if started != runs {
				t.Errorf("expected %d started tasks, got %d", runs, started)
			}
			if got := a.published.Load() + b.published.Load(); got != runs {
				t.Errorf("expected %d announcements, got %d", runs, got)
			}
		})
	}
}

// The index is added to databases that already hold tasks: many with no
// parent created at the same instant, and runs of a cron at distinct times.
func TestCronRunIndexOnExistingData(t *testing.T) {
	for _, d := range dialects(t) {
		t.Run(d.name, func(t *testing.T) {
			db := freshDB(t, d)
			// Plain SQL: GORM's DropIndex builds invalid SQL on Postgres.
			if err := db.Exec("DROP INDEX idx_tasks_cron_run").Error; err != nil {
				t.Fatalf("drop index: %v", err)
			}
			at := spawnAt
			rows := []model.Task{
				{ID: 1, CreatedAt: at, Status: "notstarted"},
				{ID: 2, CreatedAt: at, Status: "notstarted"},
				{ID: 3, CreatedAt: at, Status: "cron", CronSchedule: "0 9 * * *"},
				{ID: 4, CreatedAt: at.Add(time.Hour), ParentID: 3, Status: "completed"},
				{ID: 5, CreatedAt: at.Add(25 * time.Hour), ParentID: 3, Status: "notstarted"},
			}
			if err := db.Create(&rows).Error; err != nil {
				t.Fatalf("seed: %v", err)
			}

			if err := db.AutoMigrate(&model.Task{}); err != nil {
				t.Fatalf("migrate existing data: %v", err)
			}
			if !db.Migrator().HasIndex(&model.Task{}, "idx_tasks_cron_run") {
				t.Fatal("expected idx_tasks_cron_run after migrating")
			}

			if err := db.Create(&model.Task{ID: 6, CreatedAt: at}).Error; err != nil {
				t.Errorf("a task with no parent must not be constrained: %v", err)
			}
			err := db.Create(&model.Task{ID: 7, CreatedAt: at.Add(time.Hour), ParentID: 3}).Error
			if !errors.Is(err, gorm.ErrDuplicatedKey) {
				t.Errorf("expected a second run of the same minute refused as a duplicate, got %v", err)
			}
		})
	}
}
