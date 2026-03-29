package postgres

import (
	"context"
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
)

func New(p Params) (dbconn.DBConn, error) {
	var cfg postgresConfig

	err := p.Config.Populate(_cfgKey, &cfg)
	if err != nil {
		return nil, err
	}

	if !cfg.Enabled {
		zlog.Info().Msg(_logPrefix + "postgres repository is not enabled, skipping")
		return nil, fmt.Errorf("postgres repository is not enabled")
	}

	db, err := gorm.Open(postgres.New(postgres.Config{
		DSN: cfg.DSN,
	}), &gorm.Config{TranslateError: true})
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
