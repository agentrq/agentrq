// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	zlog "github.com/rs/zerolog/log"

	"github.com/agentrq/agentrq/backend/internal/repository/dbconn"
	"github.com/agentrq/agentrq/backend/internal/service/config"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type (
	Params struct {
		Config config.Service
	}

	repository struct {
		db *gorm.DB
	}

	postgresConfig struct {
		Enabled         bool          `yaml:"enabled"`
		DSN             string        `yaml:"dsn"`
		MaxIdleConns    int           `yaml:"maxIdleConns"`
		MaxOpenConns    int           `yaml:"maxOpenConns"`
		MaxConnLifetime time.Duration `yaml:"maxConnLifetime"`
	}
)

const (
	_cfgKey = "postgres"

	_logPrefix = "[postgres] "

	// Connecting is retried with exponential backoff, from connectRetryBase
	// to at most connectRetryMax between attempts, connectAttempts times in
	// all: about a minute and a half for a database that is still starting.
	connectAttempts  = 8
	connectRetryBase = time.Second
	connectRetryMax  = 30 * time.Second
)

// ErrDisabled is returned when postgres.enabled is false: the only case in
// which the app may run on SQLite instead.
var ErrDisabled = errors.New("postgres repository is not enabled")

var (
	openDB = func(dsn string) (*gorm.DB, error) {
		return gorm.Open(postgres.New(postgres.Config{DSN: dsn}), &gorm.Config{TranslateError: true, Logger: dbconn.Logger})
	}
	sleep = time.Sleep
)

func New(p Params) (dbconn.DBConn, error) {
	var cfg postgresConfig

	err := p.Config.Populate(_cfgKey, &cfg)
	if err != nil {
		return nil, err
	}

	if !cfg.Enabled {
		zlog.Info().Msg(_logPrefix + "postgres repository is not enabled, skipping")
		return nil, ErrDisabled
	}

	db, err := connect(cfg.DSN)
	if err != nil {
		return nil, err
	}

	zlog.Info().Msg(_logPrefix + "connected")

	dbi, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf(_logPrefix+"failed to get database connection: %w", err)
	}

	if cfg.MaxIdleConns > 0 {
		dbi.SetMaxIdleConns(cfg.MaxIdleConns)
	}
	if cfg.MaxOpenConns > 0 {
		dbi.SetMaxOpenConns(cfg.MaxOpenConns)
	}
	if cfg.MaxConnLifetime > 0 {
		dbi.SetConnMaxLifetime(cfg.MaxConnLifetime)
	}

	return &repository{db: db}, nil
}

// connect opens the database, retrying with exponential backoff, and gives
// up with the last error once every attempt has failed.
func connect(dsn string) (*gorm.DB, error) {
	wait := connectRetryBase
	for attempt := 1; ; attempt++ {
		db, err := openDB(dsn)
		if err == nil {
			return db, nil
		}
		if attempt == connectAttempts {
			return nil, fmt.Errorf(_logPrefix+"failed to connect after %d attempts: %w", attempt, err)
		}
		zlog.Warn().Err(err).Int("attempt", attempt).Dur("retryIn", wait).Msg(_logPrefix + "failed to connect, retrying")
		sleep(wait)
		wait = min(2*wait, connectRetryMax)
	}
}

func (r *repository) Conn(ctx context.Context) *gorm.DB {
	return r.db
}

func (r *repository) Close(ctx context.Context) {
	dbi, err := r.db.DB()
	if err != nil {
		return
	}
	dbi.Close()
}
