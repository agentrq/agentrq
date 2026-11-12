// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Package hook turns requests from outside services into the account's events,
// through WebAssembly hooks. See docs/agents/hooks.md.
package hook

import (
	"fmt"
	"time"

	"github.com/agentrq/agentrq/backend/internal/service/cleanup"
)

const (
	// MaxRequestBodySize is the largest request a hook URL accepts.
	MaxRequestBodySize = 1 << 20
	// MinPayloadSize is the smallest payload limit an operator may set.
	MinPayloadSize = 1024

	defaultDeliveryRetention = 7 * 24 * time.Hour
	minDeliveryRetention     = 24 * time.Hour
	defaultMaxPayloadSize    = 64 << 10
)

// Config is the server's hooks block. Zero values take the defaults.
type Config struct {
	// AllowUserHooks lets an account upload its own hooks; built-in hooks,
	// registered by the root user, are always available.
	AllowUserHooks bool `yaml:"allowUserHooks"`
	// DeliveryRetention is how long a delivery is kept, counted from its
	// arrival: in the log, for replay when it failed, and as a dedup key.
	DeliveryRetention string `yaml:"deliveryRetention"`
	// MaxPayloadSize is the most bytes one event payload written by a hook
	// may hold.
	MaxPayloadSize int `yaml:"maxPayloadSize"`
}

// Settings is a Config checked and parsed.
type Settings struct {
	AllowUserHooks    bool
	DeliveryRetention time.Duration
	MaxPayloadSize    int
}

// Settings checks the config, refusing a value the server could not honour
// rather than quietly using another.
func (c Config) Settings() (Settings, error) {
	s := Settings{
		AllowUserHooks:    c.AllowUserHooks,
		DeliveryRetention: defaultDeliveryRetention,
		MaxPayloadSize:    defaultMaxPayloadSize,
	}
	if c.DeliveryRetention != "" {
		d, err := cleanup.ParseDuration(c.DeliveryRetention)
		if err != nil {
			return Settings{}, fmt.Errorf("hooks.deliveryRetention: %w", err)
		}
		// A failed delivery is retried for hours; a shorter retention would
		// delete a request that is still being retried.
		if d < minDeliveryRetention {
			return Settings{}, fmt.Errorf("hooks.deliveryRetention: %q must be at least 1d", c.DeliveryRetention)
		}
		s.DeliveryRetention = d
	}
	if c.MaxPayloadSize != 0 {
		// A payload larger than the request it came from is a hook bug.
		if c.MaxPayloadSize < MinPayloadSize || c.MaxPayloadSize > MaxRequestBodySize {
			return Settings{}, fmt.Errorf("hooks.maxPayloadSize: %d bytes is not between %d and %d", c.MaxPayloadSize, MinPayloadSize, MaxRequestBodySize)
		}
		s.MaxPayloadSize = c.MaxPayloadSize
	}
	return s, nil
}
