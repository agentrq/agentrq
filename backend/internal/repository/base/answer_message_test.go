// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package base

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/glebarez/sqlite"
	"gorm.io/datatypes"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// answerRepos is a repository on SQLite, and on Postgres too when
// AGENTRQ_TEST_POSTGRES_DSN names a throwaway database: the "answered" check
// is dialect SQL.
func answerRepos(t *testing.T) map[string]*repository {
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
	repos := map[string]*repository{"sqlite": open(sqlite.Open("file::memory:"))}
	if dsn := os.Getenv("AGENTRQ_TEST_POSTGRES_DSN"); dsn != "" {
		repos["postgres"] = open(postgres.Open(dsn))
	}
	return repos
}

func TestAnswerMessage_OnlyTheFirstAnswerIsKept(t *testing.T) {
	ctx := context.Background()
	for name, r := range answerRepos(t) {
		t.Run(name, func(t *testing.T) {
			card := model.Message{ID: 1, TaskID: 10, Metadata: datatypes.JSON(`{"type":"a2ui","surfaceId":"s"}`)}
			if err := r.conn(ctx).Create(&card).Error; err != nil {
				t.Fatal(err)
			}
			first := []byte(`{"type":"a2ui","surfaceId":"s","answered":{"name":"approve"}}`)
			second := []byte(`{"type":"a2ui","surfaceId":"s","answered":{"name":"reject"}}`)

			if ok, err := r.AnswerMessage(ctx, 99, 1, first); err != nil || ok {
				t.Fatalf("another task's message = %v, %v; want nothing changed", ok, err)
			}
			if ok, err := r.AnswerMessage(ctx, 10, 1, first); err != nil || !ok {
				t.Fatalf("first answer = %v, %v; want it kept", ok, err)
			}
			if ok, err := r.AnswerMessage(ctx, 10, 1, second); err != nil || ok {
				t.Fatalf("second answer = %v, %v; want it refused", ok, err)
			}
			var got model.Message
			if err := r.conn(ctx).First(&got, 1).Error; err != nil {
				t.Fatal(err)
			}
			var meta struct {
				Answered struct{ Name string } `json:"answered"`
			}
			_ = json.Unmarshal(got.Metadata, &meta)
			if meta.Answered.Name != "approve" {
				t.Errorf("stored metadata = %s, want the first answer", got.Metadata)
			}
		})
	}
}

// Without a Postgres to run on, the Postgres form is at least the one a
// Postgres connection picks; it was run by hand against Postgres 16.
func TestAnswerMessage_PostgresPicksItsOwnSQL(t *testing.T) {
	db, err := gorm.Open(postgres.New(postgres.Config{DSN: "host=127.0.0.1 port=1"}), &gorm.Config{DryRun: true, DisableAutomaticPing: true})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if got := unansweredClause(db); got != pgUnanswered {
		t.Errorf("clause = %q, want the Postgres one", got)
	}
}
