// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

// Package kv is a process-local key-value store for values that are looked up
// far more often than they change.
package kv

import (
	"context"
	"sync"
)

type (
	Service interface {
		// Get returns the value stored under key, and whether there was one.
		Get(ctx context.Context, key string) (string, bool)
		// Set stores value under key, replacing any value already there.
		Set(ctx context.Context, key, value string)
	}

	service struct {
		m sync.Map
	}
)

func New() Service {
	return &service{}
}

func (s *service) Get(_ context.Context, key string) (string, bool) {
	v, ok := s.m.Load(key)
	if !ok {
		return "", false
	}
	return v.(string), true
}

func (s *service) Set(_ context.Context, key, value string) {
	s.m.Store(key, value)
}
