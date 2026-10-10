// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Package acpregistry keeps the official ACP registry's agent list, so the
// launch forms can show which agent ids acp-gateway accepts.
package acpregistry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	zlog "github.com/rs/zerolog/log"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
)

const (
	// URL is the registry acp-gateway itself resolves agents from. It sends
	// no CORS header, which is why the browser asks us rather than it.
	URL = "https://cdn.agentclientprotocol.com/registry/v1/latest/registry.json"

	// TTL is how long one fetch is served before the next refresh.
	TTL = time.Hour

	// RetryAfter is how long a failed fetch holds off the next one.
	RetryAfter = time.Minute

	fetchTimeout = 20 * time.Second

	// maxBody is far above the registry's size (~60 KiB for 41 agents); a
	// larger answer is refused, not cut.
	maxBody = 4 << 20
)

// ErrUnavailable is a load with nothing to serve: the first fetch failed, or
// one is held off after a failure.
var ErrUnavailable = errors.New("acp registry unavailable")

// Encode turns the agents into the response a load serves.
type Encode func([]entity.AcpRegistryAgent) []byte

// Controller answers the registry's agents, encoded.
type Controller interface {
	Load(ctx context.Context) ([]byte, error)
}

// Catalogue is the registry's agents as one encoded response, refetched once
// it is TTL old. An expired response is still served while a single refresh
// runs behind it; only the very first load waits for a fetch. Concurrent
// loads share one fetch, and a failure is never kept: the previous response
// stays, and the next fetch waits RetryAfter.
type Catalogue struct {
	url    string
	client *http.Client
	encode Encode
	ttl    time.Duration
	retry  time.Duration
	now    func() time.Time

	mu         sync.RWMutex
	body       []byte
	freshUntil time.Time
	retryAt    time.Time
	inflight   chan struct{} // closed when the running fetch ends; nil when none runs
}

// New makes an empty catalogue of the registry at url.
func New(url string, client *http.Client, encode Encode) *Catalogue {
	return &Catalogue{
		url:    url,
		client: client,
		encode: encode,
		ttl:    TTL,
		retry:  RetryAfter,
		now:    time.Now,
	}
}

// Load answers the encoded agent list.
func (c *Catalogue) Load(ctx context.Context) ([]byte, error) {
	c.mu.RLock()
	if c.body != nil && c.now().Before(c.freshUntil) {
		body := c.body
		c.mu.RUnlock()
		return body, nil
	}
	c.mu.RUnlock()

	c.mu.Lock()
	now := c.now()
	if c.body != nil && now.Before(c.freshUntil) {
		body := c.body
		c.mu.Unlock()
		return body, nil
	}
	if c.inflight == nil && !now.Before(c.retryAt) {
		c.inflight = make(chan struct{})
		go c.refresh(context.WithoutCancel(ctx), c.inflight)
	}
	body, wait := c.body, c.inflight
	c.mu.Unlock()

	if body != nil {
		return body, nil
	}
	if wait == nil {
		return nil, ErrUnavailable
	}
	select {
	case <-wait:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	c.mu.RLock()
	body = c.body
	c.mu.RUnlock()
	if body == nil {
		return nil, ErrUnavailable
	}
	return body, nil
}

func (c *Catalogue) refresh(ctx context.Context, done chan struct{}) {
	agents, err := c.fetch(ctx)
	var body []byte
	if err == nil {
		body = c.encode(agents)
	}

	c.mu.Lock()
	if err == nil {
		c.body, c.freshUntil = body, c.now().Add(c.ttl)
	} else {
		c.retryAt = c.now().Add(c.retry)
	}
	c.inflight = nil
	c.mu.Unlock()
	close(done)

	if err != nil {
		zlog.Warn().Err(err).Str("url", c.url).Msg("acp registry: fetch failed")
	}
}

// registry is the part of the registry's document the forms use.
type registry struct {
	Agents []struct {
		ID           string                     `json:"id"`
		Name         string                     `json:"name"`
		Version      string                     `json:"version"`
		Description  string                     `json:"description"`
		Distribution map[string]json.RawMessage `json:"distribution"`
	} `json:"agents"`
}

func (c *Catalogue) fetch(ctx context.Context) ([]entity.AcpRegistryAgent, error) {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()

	rq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return nil, err
	}
	rq.Header.Set("Accept", "application/json")
	rs, err := c.client.Do(rq)
	if err != nil {
		return nil, err
	}
	defer rs.Body.Close()
	if rs.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("registry answered %s", rs.Status)
	}
	raw, err := io.ReadAll(io.LimitReader(rs.Body, maxBody+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maxBody {
		return nil, fmt.Errorf("registry is larger than %d bytes", maxBody)
	}
	return parse(raw)
}

// parse reads the registry's agents, sorted by name. Agents without an id
// are left out, since there is nothing to launch them by; a registry with
// none left is an error, never an empty list worth keeping.
func parse(raw []byte) ([]entity.AcpRegistryAgent, error) {
	var doc registry
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("decode registry: %w", err)
	}
	agents := make([]entity.AcpRegistryAgent, 0, len(doc.Agents))
	for _, a := range doc.Agents {
		if strings.TrimSpace(a.ID) == "" {
			continue
		}
		runtimes := make([]string, 0, len(a.Distribution))
		for r := range a.Distribution {
			runtimes = append(runtimes, r)
		}
		sort.Strings(runtimes)
		name := a.Name
		if strings.TrimSpace(name) == "" {
			name = a.ID
		}
		agents = append(agents, entity.AcpRegistryAgent{
			ID:          a.ID,
			Name:        name,
			Version:     a.Version,
			Description: a.Description,
			Runtimes:    runtimes,
		})
	}
	if len(agents) == 0 {
		return nil, errors.New("registry lists no agents")
	}
	sort.SliceStable(agents, func(i, j int) bool {
		return strings.ToLower(agents[i].Name) < strings.ToLower(agents[j].Name)
	})
	return agents, nil
}
