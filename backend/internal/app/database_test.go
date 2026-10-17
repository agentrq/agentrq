// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/repository/dbconn"
	repopg "github.com/agentrq/agentrq/backend/internal/repository/postgres"
	reposqlite "github.com/agentrq/agentrq/backend/internal/repository/sqlite"
	dbconnmocks "github.com/agentrq/agentrq/backend/internal/service/mocks/dbconn"
	"github.com/golang/mock/gomock"
)

func TestOpenDatabase(t *testing.T) {
	ctrl := gomock.NewController(t)
	pgConn, sqConn := dbconnmocks.NewMockDBConn(ctrl), dbconnmocks.NewMockDBConn(ctrl)
	oldPG, oldSQ := newPostgres, newSQLite
	t.Cleanup(func() { newPostgres, newSQLite = oldPG, oldSQ })

	for _, tt := range []struct {
		name    string
		pgErr   error
		sqErr   error
		want    dbconn.DBConn
		wantErr string
		sqlite  bool
	}{
		{name: "PostgresUp", want: pgConn},
		{name: "PostgresDisabled", pgErr: repopg.ErrDisabled, want: sqConn, sqlite: true},
		// The bug: an unreachable Postgres used to fall back to SQLite.
		{name: "PostgresUnreachable", pgErr: errors.New("[postgres] failed to connect after 8 attempts: connection refused"), wantErr: "postgres: [postgres] failed to connect"},
		{name: "SQLiteFails", pgErr: fmt.Errorf("wrapped: %w", repopg.ErrDisabled), sqErr: errors.New("disk full"), wantErr: "sqlite: disk full", sqlite: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			sqliteOpened := false
			newPostgres = func(repopg.Params) (dbconn.DBConn, error) {
				if tt.pgErr != nil {
					return nil, tt.pgErr
				}
				return pgConn, nil
			}
			newSQLite = func(reposqlite.Params) (dbconn.DBConn, error) {
				sqliteOpened = true
				if tt.sqErr != nil {
					return nil, tt.sqErr
				}
				return sqConn, nil
			}
			got, err := openDatabase(nil)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("got %v, want %q", err, tt.wantErr)
				}
			} else if err != nil || got != tt.want {
				t.Fatalf("got %v, %v", got, err)
			}
			if sqliteOpened != tt.sqlite {
				t.Errorf("sqlite opened: %v, want %v", sqliteOpened, tt.sqlite)
			}
		})
	}
}

// New stops when the database cannot be opened.
func TestNewStopsWhenTheDatabaseCannotBeOpened(t *testing.T) {
	old := newPostgres
	t.Cleanup(func() { newPostgres = old })
	newPostgres = func(repopg.Params) (dbconn.DBConn, error) { return nil, errors.New("connection refused") }
	var cfg Config
	cfg.Auth.JWTSecret = "database-test-secret"
	if _, err := New(cfg); err == nil || !strings.Contains(err.Error(), "postgres: connection refused") {
		t.Errorf("New: %v, want the database error", err)
	}
}
