// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package machine

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/agentrq/agentrq/daemon/wire"
)

var agentsOn11 = LookupKey{MachineID: 11, Op: wire.OpListAcpAgents}

// fakeClock is a settable now for a cache under test.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (f *fakeClock) now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.t
}

func (f *fakeClock) advance(d time.Duration) {
	f.mu.Lock()
	f.t = f.t.Add(d)
	f.mu.Unlock()
}

func cacheAt(ttl time.Duration) (*LookupCache, *fakeClock) {
	clock := &fakeClock{t: time.Unix(1000, 0)}
	c := NewLookupCache(ttl)
	c.now = clock.now
	return c, clock
}

// counting answers body every time, keeping it, and counts how often it ran.
func counting(n *atomic.Int32, body string) LookupFetch {
	return func(context.Context) ([]byte, bool, error) {
		n.Add(1)
		return []byte(body), true, nil
	}
}

func TestLookupCacheKeepsAnAnswerUntilItExpires(t *testing.T) {
	c, clock := cacheAt(10 * time.Minute)
	var n atomic.Int32
	load := func(key LookupKey) string {
		t.Helper()
		body, err := c.Load(context.Background(), key, counting(&n, "a"))
		if err != nil {
			t.Fatal(err)
		}
		return string(body)
	}

	if got := load(agentsOn11); got != "a" {
		t.Fatalf("Load = %q, want a", got)
	}
	load(agentsOn11)
	if n.Load() != 1 {
		t.Fatalf("fetched %d times, want 1", n.Load())
	}

	load(LookupKey{MachineID: 12, Op: wire.OpListAcpAgents})
	load(LookupKey{MachineID: 11, Op: wire.OpListAcpModels, Agent: "claude"})
	if n.Load() != 3 {
		t.Fatalf("fetched %d times for three keys, want 3", n.Load())
	}

	clock.advance(10*time.Minute - time.Nanosecond)
	load(agentsOn11)
	if n.Load() != 3 {
		t.Error("answer gone before its ttl")
	}
	clock.advance(time.Nanosecond)
	load(agentsOn11)
	if n.Load() != 4 {
		t.Error("answer still served at its ttl")
	}
}

func TestLookupCacheDoesNotKeepAnUnwantedAnswerOrAnError(t *testing.T) {
	tests := map[string]LookupFetch{
		"not worth keeping": func(context.Context) ([]byte, bool, error) { return []byte(`{}`), false, nil },
		"error":             func(context.Context) ([]byte, bool, error) { return nil, true, errors.New("daemon unreachable") },
	}
	for name, fetch := range tests {
		t.Run(name, func(t *testing.T) {
			c, _ := cacheAt(time.Minute)
			var n atomic.Int32
			counted := func(ctx context.Context) ([]byte, bool, error) {
				n.Add(1)
				return fetch(ctx)
			}
			_, _ = c.Load(context.Background(), agentsOn11, counted)
			_, _ = c.Load(context.Background(), agentsOn11, counted)
			if n.Load() != 2 {
				t.Errorf("fetched %d times, want 2", n.Load())
			}
		})
	}
}

// Expired entries leave from the head of the queue as later ones are stored,
// the queue is compacted once mostly spent, and a key stored again keeps the
// later of its expiries.
func TestLookupCacheStoreDropsOnlyTheExpired(t *testing.T) {
	c, clock := cacheAt(time.Minute)
	a := LookupKey{MachineID: 1, Op: wire.OpListAcpAgents}
	b := LookupKey{MachineID: 2, Op: wire.OpListAcpAgents}
	d := LookupKey{MachineID: 3, Op: wire.OpListAcpAgents}

	c.store(a, []byte("a")) // expires 60s
	clock.advance(30 * time.Second)
	c.store(a, []byte("a2")) // expires 90s; the 60s item is stale
	c.store(b, []byte("b"))  // expires 90s
	clock.advance(30 * time.Second)
	c.store(d, []byte("d")) // pops a's 60s item, which must not drop a2
	if e, ok := c.entries[a]; !ok || string(e.body) != "a2" {
		t.Fatalf("a = %+v, %v; want a2 kept", e, ok)
	}
	clock.advance(30 * time.Second)
	c.store(a, []byte("a3")) // pops a2 and b

	if len(c.entries) != 2 {
		t.Errorf("entries = %d, want 2 (d and a3)", len(c.entries))
	}
	if live := len(c.expiry) - c.head; live != 2 {
		t.Errorf("live queue = %d, want 2", live)
	}
	if c.head != 0 {
		t.Errorf("head = %d, want the spent queue compacted", c.head)
	}
}

// An answer stored between Load's read-locked check and its write lock is
// served rather than fetched again. The clock steps back between the two
// checks to stand in for that store landing in between.
func TestLookupCacheRechecksUnderTheWriteLock(t *testing.T) {
	c, _ := cacheAt(time.Minute)
	base := time.Unix(1000, 0)
	c.entries[agentsOn11] = lookupEntry{body: []byte("a"), expires: base.Add(time.Minute)}
	calls := 0
	c.now = func() time.Time {
		calls++
		if calls == 1 {
			return base.Add(time.Minute) // expired at the first look
		}
		return base // fresh at the second
	}
	var n atomic.Int32
	if body, err := c.Load(context.Background(), agentsOn11, counting(&n, "b")); err != nil || string(body) != "a" {
		t.Fatalf("Load = %q, %v; want the stored a", body, err)
	}
	if n.Load() != 0 {
		t.Errorf("fetched %d times, want 0", n.Load())
	}
}

// Concurrent misses on one key ask the machine once and all get its answer.
func TestLookupCacheSharesOneFetchBetweenConcurrentMisses(t *testing.T) {
	c, _ := cacheAt(time.Minute)
	var n atomic.Int32
	release := make(chan struct{})
	fetch := func(context.Context) ([]byte, bool, error) {
		n.Add(1)
		<-release
		return []byte("a"), true, nil
	}

	const callers = 8
	var wg sync.WaitGroup
	got := make([]string, callers)
	for i := range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			body, _ := c.Load(context.Background(), agentsOn11, fetch)
			got[i] = string(body)
		}()
	}
	for {
		c.mu.RLock()
		_, started := c.inflight[agentsOn11]
		c.mu.RUnlock()
		if started {
			break
		}
		time.Sleep(time.Millisecond)
	}
	time.Sleep(10 * time.Millisecond) // let the rest join the call
	close(release)
	wg.Wait()

	if n.Load() != 1 {
		t.Errorf("fetched %d times, want 1", n.Load())
	}
	for i, g := range got {
		if g != "a" {
			t.Errorf("caller %d got %q, want a", i, g)
		}
	}
}

// A caller that gives up stops waiting, but the fetch it started runs on and
// is kept for the next one.
func TestLookupCacheCallerGivingUpDoesNotCancelTheFetch(t *testing.T) {
	c, _ := cacheAt(time.Minute)
	var n atomic.Int32
	release := make(chan struct{})
	finished := make(chan error, 1)
	fetch := func(ctx context.Context) ([]byte, bool, error) {
		n.Add(1)
		<-release
		finished <- ctx.Err()
		return []byte("a"), true, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Load(ctx, agentsOn11, fetch); !errors.Is(err, context.Canceled) {
		t.Fatalf("Load err = %v, want context.Canceled", err)
	}
	close(release)
	if err := <-finished; err != nil {
		t.Errorf("fetch saw ctx err %v, want none", err)
	}
	for {
		c.mu.RLock()
		_, running := c.inflight[agentsOn11]
		c.mu.RUnlock()
		if !running {
			break
		}
		time.Sleep(time.Millisecond)
	}

	if body, err := c.Load(context.Background(), agentsOn11, fetch); err != nil || string(body) != "a" {
		t.Errorf("Load = %q, %v; want the kept answer", body, err)
	}
	if n.Load() != 1 {
		t.Errorf("fetched %d times, want 1", n.Load())
	}
}

func TestNilLookupCacheFetchesEveryTime(t *testing.T) {
	var c *LookupCache
	var n atomic.Int32
	for range 2 {
		if body, err := c.Load(context.Background(), agentsOn11, counting(&n, "a")); err != nil || string(body) != "a" {
			t.Fatalf("Load = %q, %v", body, err)
		}
	}
	if n.Load() != 2 {
		t.Errorf("fetched %d times, want 2", n.Load())
	}
}
