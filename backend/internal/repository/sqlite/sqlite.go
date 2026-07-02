package sqlite

import (
	"context"
	"fmt"
	"strings"
	"time"

	zlog "github.com/rs/zerolog/log"

	"github.com/agentrq/agentrq/backend/internal/repository/dbconn"
	"github.com/agentrq/agentrq/backend/internal/service/config"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

type (
	Params struct {
		Config config.Service
	}

	repository struct {
		db *gorm.DB
	}

	sqliteConfig struct {
		Enabled         bool          `yaml:"enabled"`
		DSN             string        `yaml:"dsn"`
		MaxIdleConns    int           `yaml:"maxIdleConns"`
		MaxOpenConns    int           `yaml:"maxOpenConns"`
		MaxConnLifetime time.Duration `yaml:"maxConnLifetime"`
	}
)

const (
	_cfgKey = "sqlite"

	_logPrefix = "[sqlite] "
)

func New(p Params) (dbconn.DBConn, error) {
	var cfg sqliteConfig

	err := p.Config.Populate(_cfgKey, &cfg)
	if err != nil {
		return nil, err
	}

	if !cfg.Enabled {
		zlog.Info().Msg(_logPrefix + "sqlite repository is not enabled, skipping")
		return nil, nil
	}

	db, err := gorm.Open(sqlite.Open(withConcurrencyPragmas(cfg.DSN)), &gorm.Config{TranslateError: true})
	if err != nil {
		return nil, fmt.Errorf(_logPrefix+"failed to connect: %w", err)
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

// withConcurrencyPragmas enables WAL journaling and a busy timeout on a file-backed SQLite
// DSN. WAL lets readers and the single writer proceed concurrently (a bare DSN defaults to
// "delete" journaling, where any reader blocks the writer and vice versa), and the busy
// timeout makes contending writers wait rather than fail. It respects an operator that has
// already configured pragmas, and leaves in-memory databases untouched.
func withConcurrencyPragmas(dsn string) string {
	if dsn == "" || strings.Contains(dsn, "_pragma=") {
		return dsn // operator configured pragmas explicitly; do not override
	}
	if strings.Contains(dsn, ":memory:") {
		return dsn // WAL is not meaningful for in-memory databases
	}

	base := dsn
	if !strings.HasPrefix(base, "file:") {
		// A file: scheme is required for the driver to parse query-string pragmas.
		base = "file:" + base
	}
	sep := "?"
	if strings.Contains(base, "?") {
		sep = "&"
	}
	return base + sep + "_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
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
