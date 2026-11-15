// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Package appconfig keeps the server's config in the database, where the
// operator changes it from the admin pages. Each block of the yaml config is
// one row, seeded from yaml and env vars the first time the server starts
// without it; after that the row wins over both. A change takes effect when
// the server next starts.
package appconfig

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	zlog "github.com/rs/zerolog/log"
	"gorm.io/gorm/clause"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/repository/dbconn"
	"github.com/agentrq/agentrq/backend/internal/service/config"
	"github.com/agentrq/agentrq/backend/internal/service/security"
)

var (
	// envOnlyBlocks never go in the database: it takes the database's own
	// settings to read it, and idgen's node must differ on every instance
	// while the database is shared.
	envOnlyBlocks = []string{"idgen", "postgres", "sqlite"}
	// envOnlyFields stay in env vars too: the key the rows are encrypted
	// with, and the root token the admin pages are signed in with.
	envOnlyFields = map[string][]string{"auth": {"rootAccessToken", "workspaceTokenKey"}}
)

type (
	// Controller reads and writes the stored config.
	Controller interface {
		// Load seeds every block the database lacks from the yaml config,
		// and returns that config with the stored blocks laid over it.
		Load(ctx context.Context) (config.Service, error)
		List(ctx context.Context) ([]entity.AppConfiguration, error)
		// Update replaces a block, once the config it makes passes Check.
		Update(ctx context.Context, rq entity.UpdateAppConfigurationRequest) (*entity.AppConfiguration, error)
	}

	Params struct {
		DB dbconn.DBConn
		// Base is the config read from yaml and env vars.
		Base config.Service
		// Key encrypts the rows: auth.workspaceTokenKey, 32 bytes.
		Key string
		// Check refuses a config the server could not start with.
		Check func(config.Service) error
	}

	controller struct {
		db    dbconn.DBConn
		base  config.Service
		key   string
		check func(config.Service) error
	}
)

// New returns the controller, refusing a key AES-256 cannot use.
func New(p Params) (Controller, error) {
	if len(p.Key) != 32 {
		return nil, fmt.Errorf("auth.workspaceTokenKey: the config is encrypted with it, so it must be 32 bytes, not %d", len(p.Key))
	}
	return &controller{db: p.DB, base: p.Base, key: p.Key, check: p.Check}, nil
}

// Keys lists the blocks kept in the database.
func (c *controller) keys() []string {
	return slices.DeleteFunc(c.base.Keys(), func(k string) bool { return slices.Contains(envOnlyBlocks, k) })
}

func (c *controller) Load(ctx context.Context) (config.Service, error) {
	stored, err := c.stored(ctx)
	if err != nil {
		return nil, err
	}
	for _, key := range c.keys() {
		if _, ok := stored[key]; ok {
			continue
		}
		block := map[string]any{}
		if err := c.base.Populate(key, &block); err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
		row := c.row(key, strip(key, block))
		// Instances starting together seed alike; the first row stays.
		if err := c.db.Conn(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
			return nil, fmt.Errorf("seed config %s: %w", key, err)
		}
		zlog.Info().Str("block", key).Msg("config: stored the block from yaml and env vars")
	}
	if stored, err = c.stored(ctx); err != nil {
		return nil, err
	}
	return config.Overlay(c.base, stored)
}

func (c *controller) List(ctx context.Context) ([]entity.AppConfiguration, error) {
	stored, err := c.stored(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]entity.AppConfiguration, 0, len(stored))
	for _, key := range c.keys() {
		if val, ok := stored[key]; ok {
			out = append(out, entity.AppConfiguration{Key: key, Value: val})
		}
	}
	return out, nil
}

func (c *controller) Update(ctx context.Context, rq entity.UpdateAppConfigurationRequest) (*entity.AppConfiguration, error) {
	if !slices.Contains(c.keys(), rq.Key) {
		return nil, base.ErrNotFound
	}
	block, err := decodeObject(rq.Value)
	if err != nil {
		return nil, &entity.AppConfigurationError{Message: rq.Key + ": the value must be a JSON object"}
	}
	val, _ := json.Marshal(strip(rq.Key, block))

	stored, err := c.stored(ctx)
	if err != nil {
		return nil, err
	}
	stored[rq.Key] = val
	next, err := config.Overlay(c.base, stored)
	if err == nil {
		err = c.check(next)
	}
	if err != nil {
		return nil, &entity.AppConfigurationError{Message: err.Error()}
	}

	row := c.row(rq.Key, block)
	if err := c.db.Conn(ctx).Clauses(clause.OnConflict{UpdateAll: true}).Create(&row).Error; err != nil {
		return nil, err
	}
	return &entity.AppConfiguration{Key: rq.Key, Value: val}, nil
}

// stored reads every row, decrypted, without the fields kept in env vars.
func (c *controller) stored(ctx context.Context) (map[string][]byte, error) {
	var rows []model.AppConfiguration
	if err := c.db.Conn(ctx).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	out := make(map[string][]byte, len(rows))
	for _, r := range rows {
		plain, err := security.Decrypt(r.Val, c.key, r.Nonce)
		if err != nil {
			return nil, fmt.Errorf("config %s: cannot decrypt it with auth.workspaceTokenKey: %w", r.ConfigKey, err)
		}
		block, err := decodeObject([]byte(plain))
		if err != nil {
			return nil, fmt.Errorf("config %s: %w", r.ConfigKey, err)
		}
		out[r.ConfigKey], _ = json.Marshal(strip(r.ConfigKey, block))
	}
	return out, nil
}

// row encrypts a block for the table. A block read from yaml or JSON always
// encodes, and with the key's length checked in New, Encrypt fails only when
// crypto/rand does, which since Go 1.24 crashes the process instead.
func (c *controller) row(key string, block map[string]any) model.AppConfiguration {
	val, _ := json.Marshal(block)
	cipher, nonce, _ := security.Encrypt(string(val), c.key)
	return model.AppConfiguration{ConfigKey: key, Val: cipher, Nonce: nonce}
}

// decodeObject reads a JSON object, keeping its numbers exact.
func decodeObject(raw []byte) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var block map[string]any
	if err := dec.Decode(&block); err != nil {
		return nil, err
	}
	if block == nil {
		return nil, errors.New("not a JSON object")
	}
	return block, nil
}

// strip drops the block's fields kept in env vars.
func strip(key string, block map[string]any) map[string]any {
	for _, f := range envOnlyFields[key] {
		delete(block, f)
	}
	return block
}
