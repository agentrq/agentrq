// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package coremcp

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	"github.com/agentrq/agentrq/backend/internal/service/auth"
)

// fakeRevocations answers ids, or err, counting its loads.
type fakeRevocations struct {
	mu    sync.Mutex
	ids   []int64
	err   error
	loads atomic.Int32
	since time.Time
	gate  chan struct{}
}

func (f *fakeRevocations) load(_ context.Context, since time.Time) ([]int64, error) {
	f.loads.Add(1)
	if f.gate != nil {
		<-f.gate
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.since = since
	return f.ids, f.err
}

func (f *fakeRevocations) set(ids []int64, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ids, f.err = ids, err
}

// clock is a now that a test moves by hand.
type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newTestRevoked(f *fakeRevocations) (*revokedWorkspaces, *clock) {
	c := &clock{t: time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)}
	r := newRevokedWorkspaces(f.load)
	r.now = c.now
	return r, c
}

// One load answers every call for 15 minutes, and only the ids it returned
// are revoked; it asks for a launch token's lifetime of revocations.
func TestRevokedWorkspaces_OneLoadServesFifteenMinutes(t *testing.T) {
	f := &fakeRevocations{ids: []int64{7}}
	r, c := newTestRevoked(f)

	if !r.has(t.Context(), 7) || r.has(t.Context(), 8) {
		t.Fatal("want 7 revoked and 8 not")
	}
	if want := c.t.Add(-auth.SupervisorLaunchTokenTTL - clockSkew); !f.since.Equal(want) {
		t.Errorf("loaded since %v, want %v", f.since, want)
	}
	c.t = c.t.Add(revokedRefresh - time.Second)
	f.set([]int64{8}, nil)
	if !r.has(t.Context(), 7) || f.loads.Load() != 1 {
		t.Errorf("loads = %d, want the first load still trusted", f.loads.Load())
	}

	c.t = c.t.Add(time.Second)
	if !r.has(t.Context(), 8) || r.has(t.Context(), 7) || f.loads.Load() != 2 {
		t.Errorf("loads = %d, want a reload after 15 minutes", f.loads.Load())
	}
}

// Callers that find the set stale together share one load.
func TestRevokedWorkspaces_ConcurrentCallersShareOneLoad(t *testing.T) {
	f := &fakeRevocations{ids: []int64{7}, gate: make(chan struct{})}
	r, _ := newTestRevoked(f)

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if !r.has(context.Background(), 7) {
				t.Error("want 7 revoked")
			}
		}()
	}
	for f.loads.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(10 * time.Millisecond)
	close(f.gate)
	wg.Wait()
	if n := f.loads.Load(); n != 1 {
		t.Errorf("loads = %d, want 1", n)
	}
}

// With nothing loaded yet a failed load refuses every token; once a set is
// loaded, a failed reload keeps it and the next call tries again.
func TestRevokedWorkspaces_AFailedLoad(t *testing.T) {
	f := &fakeRevocations{err: errors.New("database unavailable")}
	r, c := newTestRevoked(f)
	if !r.has(t.Context(), 8) {
		t.Error("nothing loaded: want every id treated as revoked")
	}

	f.set([]int64{7}, nil)
	if r.has(t.Context(), 8) {
		t.Fatal("want 8 not revoked once loaded")
	}
	c.t = c.t.Add(revokedRefresh)
	f.set(nil, errors.New("database unavailable"))
	if !r.has(t.Context(), 7) || r.has(t.Context(), 8) {
		t.Error("a failed reload: want the last set kept")
	}
	if n := f.loads.Load(); n != 4 {
		t.Errorf("loads = %d, want a retry on each call after a failure", n)
	}
}

// A caller whose context ends before the load is refused; the load still
// finishes for the next one.
func TestRevokedWorkspaces_ACallerThatGivesUpIsRefused(t *testing.T) {
	f := &fakeRevocations{gate: make(chan struct{})}
	r, _ := newTestRevoked(f)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if !r.has(ctx, 8) {
		t.Error("want a caller that gave up refused")
	}
	close(f.gate)
	if r.has(t.Context(), 8) {
		t.Error("want the load to finish and answer the next caller")
	}
}

// revokedCrud answers the revoked ids.
type revokedCrud struct {
	crud.Controller
	ids []int64
}

func (c revokedCrud) ListRevokedWorkspaceIDs(context.Context, time.Time) ([]int64, error) {
	return c.ids, nil
}

// New loads the revoked ids from the database it is given.
func TestNew_LoadsTheRevokedWorkspacesFromItsCrud(t *testing.T) {
	h, err := New(Params{Mux: http.NewServeMux(), Crud: revokedCrud{ids: []int64{7}}})
	if err != nil {
		t.Fatal(err)
	}
	if r := h.(*handler).revoked; !r.has(t.Context(), 7) || r.has(t.Context(), 8) {
		t.Error("want 7 revoked and 8 not")
	}
}
