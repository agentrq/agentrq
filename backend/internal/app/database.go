// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"errors"
	"fmt"

	"github.com/agentrq/agentrq/backend/internal/repository/dbconn"
	repopg "github.com/agentrq/agentrq/backend/internal/repository/postgres"
	reposqlite "github.com/agentrq/agentrq/backend/internal/repository/sqlite"
	"github.com/agentrq/agentrq/backend/internal/service/config"
)

var (
	newPostgres = repopg.New
	newSQLite   = reposqlite.New
)

// openDatabase opens Postgres when it is enabled, and SQLite only when it is
// not. Postgres enabled but unreachable is an error, never a quiet switch to
// a local SQLite file other instances cannot see.
func openDatabase(c config.Service) (dbconn.DBConn, error) {
	pg, err := newPostgres(repopg.Params{Config: c})
	if err == nil {
		return pg, nil
	}
	if !errors.Is(err, repopg.ErrDisabled) {
		return nil, fmt.Errorf("postgres: %w", err)
	}
	sq, err := newSQLite(reposqlite.Params{Config: c})
	if err != nil {
		return nil, fmt.Errorf("sqlite: %w", err)
	}
	return sq, nil
}
