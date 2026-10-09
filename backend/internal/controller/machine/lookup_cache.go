// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package machine

import (
	"context"
	"sync"
	"time"

	"github.com/agentrq/agentrq/daemon/wire"
)

// LookupCacheTTL is how long a machine's answer to a lookup is reused.
const LookupCacheTTL = 10 * time.Minute

// LookupKey names one lookup on one machine. Agent is empty for a lookup that
// takes none.
type LookupKey struct {
	MachineID int64
	Op        wire.Op
	Agent     string
}

// LookupFetch asks the machine, returning the response to serve and whether
// it is worth keeping.
type LookupFetch func(context.Context) (body []byte, keep bool, err error)

// LookupCache keeps machines' answers to slow lookups — the acp-gateway's agent
// list, an agent's models — for a fixed ttl, and lets concurrent misses on one
// key share a single ask. Per process, like [Registry]; a nil cache keeps
// nothing and shares nothing.
//
// Every entry lives the same ttl, so entries expire in the order they were
// stored: expiry is a FIFO, and a store only pops its expired head.
type LookupCache struct {
	ttl time.Duration
	now func() time.Time

	mu       sync.RWMutex
	entries  map[LookupKey]lookupEntry
	expiry   []lookupExpiry // ascending by at; expiry[head:] is live
	head     int
	inflight map[LookupKey]*lookupCall
}

type lookupEntry struct {
	body    []byte
	expires time.Time
}

type lookupExpiry struct {
	key LookupKey
	at  time.Time
}

type lookupCall struct {
	done chan struct{}
	body []byte
	err  error
}

// NewLookupCache makes an empty cache whose entries live for ttl.
func NewLookupCache(ttl time.Duration) *LookupCache {
	return &LookupCache{
		ttl:      ttl,
		now:      time.Now,
		entries:  map[LookupKey]lookupEntry{},
		inflight: map[LookupKey]*lookupCall{},
	}
}

// Load answers key from the cache, or from fetch on a miss. Concurrent misses
// on one key share one fetch, run detached from any one caller's ctx so a
// caller that gives up does not fail the others; a caller stops waiting when
// its own ctx ends.
func (c *LookupCache) Load(ctx context.Context, key LookupKey, fetch LookupFetch) ([]byte, error) {
	if c == nil {
		body, _, err := fetch(ctx)
		return body, err
	}

	c.mu.RLock()
	e, ok := c.entries[key]
	c.mu.RUnlock()
	if ok && c.now().Before(e.expires) {
		return e.body, nil
	}

	c.mu.Lock()
	if e, ok := c.entries[key]; ok && c.now().Before(e.expires) {
		c.mu.Unlock()
		return e.body, nil
	}
	call, ok := c.inflight[key]
	if !ok {
		call = &lookupCall{done: make(chan struct{})}
		c.inflight[key] = call
		go c.run(context.WithoutCancel(ctx), key, call, fetch)
	}
	c.mu.Unlock()

	select {
	case <-call.done:
		return call.body, call.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (c *LookupCache) run(ctx context.Context, key LookupKey, call *lookupCall, fetch LookupFetch) {
	body, keep, err := fetch(ctx)
	call.body, call.err = body, err

	c.mu.Lock()
	delete(c.inflight, key)
	if err == nil && keep {
		c.store(key, body)
	}
	c.mu.Unlock()
	close(call.done)
}

// store keeps body under key and drops what has expired. Called with mu held.
func (c *LookupCache) store(key LookupKey, body []byte) {
	now := c.now()
	for c.head < len(c.expiry) && !now.Before(c.expiry[c.head].at) {
		x := c.expiry[c.head]
		c.expiry[c.head] = lookupExpiry{}
		c.head++
		// A key stored again since has a later expiry and stays.
		if e, ok := c.entries[x.key]; ok && e.expires.Equal(x.at) {
			delete(c.entries, x.key)
		}
	}
	if c.head > len(c.expiry)/2 {
		c.expiry = append(c.expiry[:0], c.expiry[c.head:]...)
		c.head = 0
	}

	at := now.Add(c.ttl)
	c.entries[key] = lookupEntry{body: body, expires: at}
	c.expiry = append(c.expiry, lookupExpiry{key: key, at: at})
}
