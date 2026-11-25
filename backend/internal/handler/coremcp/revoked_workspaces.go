// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package coremcp

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/agentrq/agentrq/backend/internal/service/auth"
)

// revokedRefresh is how long this instance trusts the revoked ids it loaded:
// a deleted or reset supervisor workspace's launch tokens stop working within
// it, and a call costs a lookup only once in it.
const revokedRefresh = 15 * time.Minute

// revokedSnapshot is one load: never changed once published.
type revokedSnapshot struct {
	ids      map[int64]struct{}
	loadedAt time.Time
}

// revokedWorkspaces holds the supervisor workspace ids revoked within a launch
// token's lifetime. A load builds a new map and publishes it whole through an
// atomic pointer, so a check is one atomic load and one map lookup with no
// lock: the set only ever changes all at once, every 15 minutes, which suits
// a published snapshot better than a sync.Map. Concurrent reloads share one
// load. A failed load keeps the snapshot it had; with none loaded yet, every
// launch token is refused rather than trusted.
type revokedWorkspaces struct {
	load func(ctx context.Context, since time.Time) ([]int64, error)
	now  func() time.Time

	snapshot atomic.Pointer[revokedSnapshot]

	mu      sync.Mutex
	loading chan struct{}
}

func newRevokedWorkspaces(load func(ctx context.Context, since time.Time) ([]int64, error)) *revokedWorkspaces {
	return &revokedWorkspaces{load: load, now: time.Now}
}

// has reports whether id is revoked.
func (r *revokedWorkspaces) has(ctx context.Context, id int64) bool {
	if s := r.snapshot.Load(); s != nil && r.now().Sub(s.loadedAt) < revokedRefresh {
		_, hit := s.ids[id]
		return hit
	}

	select {
	case <-r.reload(ctx):
	case <-ctx.Done():
		return true
	}
	s := r.snapshot.Load()
	if s == nil {
		return true
	}
	_, hit := s.ids[id]
	return hit
}

// reload starts a load unless one is running, and returns its done channel.
// The load is detached, so one caller giving up does not fail the others.
func (r *revokedWorkspaces) reload(ctx context.Context) <-chan struct{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.loading != nil {
		return r.loading
	}
	done := make(chan struct{})
	r.loading = done
	go func() {
		now := r.now()
		ids, err := r.load(context.WithoutCancel(ctx), now.Add(-auth.SupervisorLaunchTokenTTL-clockSkew))
		if err == nil {
			set := make(map[int64]struct{}, len(ids))
			for _, id := range ids {
				set[id] = struct{}{}
			}
			r.snapshot.Store(&revokedSnapshot{ids: set, loadedAt: now})
		}
		r.mu.Lock()
		r.loading = nil
		r.mu.Unlock()
		close(done)
	}()
	return done
}
