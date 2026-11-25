// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package base

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/glebarez/sqlite"
	"gorm.io/datatypes"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// editRepos is a repository on a SQLite file opened as the server opens one
// (WAL, immediate transactions), and on Postgres too when
// AGENTRQ_TEST_POSTGRES_DSN names a throwaway database: concurrent edits need
// more than one connection.
func editRepos(t *testing.T) map[string]*repository {
	t.Helper()
	open := func(d gorm.Dialector) *repository {
		db, err := gorm.Open(d, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
		if err != nil {
			t.Fatalf("open db: %v", err)
		}
		t.Cleanup(func() {
			if sqlDB, err := db.DB(); err == nil {
				_ = sqlDB.Close()
			}
		})
		if err := db.Migrator().DropTable(&model.Message{}); err != nil {
			t.Fatal(err)
		}
		if err := db.AutoMigrate(&model.Message{}); err != nil {
			t.Fatal(err)
		}
		return New(&mockDB{db: db}).(*repository)
	}
	dsn := "file:" + filepath.Join(t.TempDir(), "a.db") + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_txlock=immediate"
	repos := map[string]*repository{"sqlite": open(sqlite.Open(dsn))}
	if dsn := os.Getenv("AGENTRQ_TEST_POSTGRES_DSN"); dsn != "" {
		repos["postgres"] = open(postgres.Open(dsn))
	}
	return repos
}

func TestEditMessage(t *testing.T) {
	ctx := context.Background()
	for name, r := range editRepos(t) {
		t.Run(name, func(t *testing.T) {
			msg := model.Message{ID: 1, TaskID: 10, Text: "Building", Metadata: datatypes.JSON(`{"type":"a2ui"}`)}
			if err := r.conn(ctx).Create(&msg).Error; err != nil {
				t.Fatal(err)
			}
			read := func() model.Message {
				var m model.Message
				if err := r.conn(ctx).First(&m, 1).Error; err != nil {
					t.Fatal(err)
				}
				return m
			}

			err := r.EditMessage(ctx, 99, 1, func(model.Message) (string, []byte, error) {
				t.Error("edit called for another task's message")
				return "", nil, nil
			})
			if !errors.Is(err, ErrNotFound) {
				t.Errorf("another task's message = %v, want ErrNotFound", err)
			}

			refused := errors.New("card was answered")
			if err := r.EditMessage(ctx, 10, 1, func(model.Message) (string, []byte, error) {
				return "changed", []byte(`{}`), refused
			}); !errors.Is(err, refused) {
				t.Errorf("refused edit = %v, want its error", err)
			}
			if m := read(); m.Text != "Building" || !sameJSON(m.Metadata, `{"type":"a2ui"}`) {
				t.Errorf("after a refused edit: %q %s, want it unchanged", m.Text, m.Metadata)
			}

			if err := r.EditMessage(ctx, 10, 1, func(m model.Message) (string, []byte, error) {
				if m.Text != "Building" {
					t.Errorf("edit read %q", m.Text)
				}
				return "Built", []byte(`{"type":"a2ui","n":1}`), nil
			}); err != nil {
				t.Fatal(err)
			}
			var meta map[string]any
			if m := read(); m.Text != "Built" || json.Unmarshal(m.Metadata, &meta) != nil || meta["n"] != 1.0 {
				t.Errorf("after the edit: %q %s", m.Text, m.Metadata)
			}

			// No metadata drops it: a deleted card leaves only the text.
			if err := r.EditMessage(ctx, 10, 1, func(model.Message) (string, []byte, error) { return "Gone", nil, nil }); err != nil {
				t.Fatal(err)
			}
			if m := read(); m.Text != "Gone" || m.Metadata != nil {
				t.Errorf("after dropping the metadata: %q %q", m.Text, m.Metadata)
			}

			if err := r.conn(ctx).Migrator().DropTable(&model.Message{}); err != nil {
				t.Fatal(err)
			}
			if err := r.EditMessage(ctx, 10, 1, func(model.Message) (string, []byte, error) {
				t.Error("edit called without a message")
				return "", nil, nil
			}); err == nil || errors.Is(err, ErrNotFound) {
				t.Errorf("with the database unavailable = %v, want its error", err)
			}
		})
	}
}

// sameJSON compares JSON as values: Postgres stores jsonb, spaced its way.
func sameJSON(got []byte, want string) bool {
	var g, w any
	return json.Unmarshal(got, &g) == nil && json.Unmarshal([]byte(want), &w) == nil && fmt.Sprint(g) == fmt.Sprint(w)
}

// Each edit reads what the one before it wrote: none is lost.
func TestEditMessage_ConcurrentEditsAreEachApplied(t *testing.T) {
	ctx := context.Background()
	for name, r := range editRepos(t) {
		t.Run(name, func(t *testing.T) {
			if err := r.conn(ctx).Create(&model.Message{ID: 1, TaskID: 10, Metadata: datatypes.JSON(`{"n":0}`)}).Error; err != nil {
				t.Fatal(err)
			}
			const edits = 20
			var wg sync.WaitGroup
			errs := make(chan error, edits)
			for range edits {
				wg.Add(1)
				go func() {
					defer wg.Done()
					errs <- r.EditMessage(ctx, 10, 1, func(m model.Message) (string, []byte, error) {
						var meta struct{ N int }
						if err := json.Unmarshal(m.Metadata, &meta); err != nil {
							return "", nil, err
						}
						return fmt.Sprint(meta.N + 1), []byte(fmt.Sprintf(`{"n":%d}`, meta.N+1)), nil
					})
				}()
			}
			wg.Wait()
			close(errs)
			for err := range errs {
				if err != nil {
					t.Fatal(err)
				}
			}
			var m model.Message
			if err := r.conn(ctx).First(&m, 1).Error; err != nil {
				t.Fatal(err)
			}
			if m.Text != fmt.Sprint(edits) || !sameJSON(m.Metadata, fmt.Sprintf(`{"n":%d}`, edits)) {
				t.Errorf("after %d edits: %q %s; an edit was lost", edits, m.Text, m.Metadata)
			}
		})
	}
}
