// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

// Package taskagent names the agents, and the models they run, that change a
// task's status.
//
// A name is stored once, in the agents or agent_models table, under the hash
// of the name, and a task's history carries only the ID. The names live in kv,
// which is filled from the tables at startup and on every miss, so a lookup
// reaches the database only for a name it has never seen.
//
// A new name is written behind: into kv at once, and into its table with the
// rest of the minute's in one batch. The names are not critical — a name lost
// to a failed batch leaves its changes unnamed, nothing more — so a batch that
// fails is dropped rather than retried, and a minute takes at most maxPending
// new names per table. A client inventing names can therefore neither grow the
// queue nor turn its calls into database writes.
package taskagent

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cespare/xxhash/v2"
	zlog "github.com/rs/zerolog/log"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/kv"
)

type (
	Controller interface {
		// Start puts every agent and model name in kv, and starts writing
		// new ones to the tables.
		Start(ctx context.Context) error
		// Register files the agent's name and its model's, and returns the
		// agent with the IDs a status change records. An agent that named
		// nobody comes back with neither.
		Register(ctx context.Context, a entity.TaskAgent) entity.TaskAgent
		// Names returns the names filed under the given IDs. An ID nothing is
		// filed under is left out.
		Names(ctx context.Context, agentIDs, modelIDs []int64) (agents, models map[int64]string, err error)
		// Close writes the names still pending and stops.
		Close()
	}

	Params struct {
		Repository base.Repository
		KV         kv.Service
		// Interval is how often new names are written; a minute when zero.
		Interval time.Duration
	}

	controller struct {
		repository base.Repository
		kv         kv.Service
		interval   time.Duration

		mu            sync.Mutex
		pendingAgents map[int64]string
		pendingModels map[int64]string

		stop      chan struct{}
		closeOnce sync.Once
		wg        sync.WaitGroup
	}
)

const (
	// maxAgentName and maxModelName are the widths, in characters, of the
	// name columns of agents and agent_models: room for the names agents
	// actually give, and no more.
	maxAgentName = 32
	maxModelName = 32
	// maxPending is how many new names per table one batch takes.
	maxPending = 1000

	agentKeyPrefix = "agent:"
	modelKeyPrefix = "agent_model:"
)

func New(p Params) Controller {
	if p.Interval == 0 {
		p.Interval = time.Minute
	}
	return &controller{
		repository:    p.Repository,
		kv:            p.KV,
		interval:      p.Interval,
		pendingAgents: make(map[int64]string),
		pendingModels: make(map[int64]string),
		stop:          make(chan struct{}),
	}
}

func (c *controller) Start(ctx context.Context) error {
	agents, err := c.repository.ListAgents(ctx, nil)
	if err != nil {
		return err
	}
	for _, a := range agents {
		c.kv.Set(ctx, agentKey(a.ID), a.Name)
	}
	models, err := c.repository.ListAgentModels(ctx, nil)
	if err != nil {
		return err
	}
	for _, m := range models {
		c.kv.Set(ctx, modelKey(m.ID), m.Name)
	}

	c.wg.Add(1)
	go c.worker()
	return nil
}

func (c *controller) Register(ctx context.Context, a entity.TaskAgent) entity.TaskAgent {
	name := truncateName(a.Name, maxAgentName)
	if name == "" {
		return entity.TaskAgent{}
	}
	registered := entity.TaskAgent{Name: name, ID: nameID(name)}
	c.cacheAndQueue(ctx, agentKey(registered.ID), c.pendingAgents, registered.ID, name)
	if registered.Model = truncateName(a.Model, maxModelName); registered.Model != "" {
		registered.ModelID = nameID(registered.Model)
		c.cacheAndQueue(ctx, modelKey(registered.ModelID), c.pendingModels, registered.ModelID, registered.Model)
	}
	return registered
}

// cacheAndQueue puts a name kv does not have yet in kv, and queues it for the
// next batch while the batch has room.
func (c *controller) cacheAndQueue(ctx context.Context, key string, pending map[int64]string, id int64, name string) {
	if _, ok := c.kv.Get(ctx, key); ok {
		return
	}
	c.kv.Set(ctx, key, name)
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(pending) < maxPending {
		pending[id] = name
	}
}

func (c *controller) Names(ctx context.Context, agentIDs, modelIDs []int64) (map[int64]string, map[int64]string, error) {
	agents, missing := c.cachedNames(ctx, agentKey, agentIDs)
	if len(missing) > 0 {
		rows, err := c.repository.ListAgents(ctx, missing)
		if err != nil {
			return nil, nil, err
		}
		for _, r := range rows {
			c.kv.Set(ctx, agentKey(r.ID), r.Name)
			agents[r.ID] = r.Name
		}
	}
	models, missing := c.cachedNames(ctx, modelKey, modelIDs)
	if len(missing) > 0 {
		rows, err := c.repository.ListAgentModels(ctx, missing)
		if err != nil {
			return nil, nil, err
		}
		for _, r := range rows {
			c.kv.Set(ctx, modelKey(r.ID), r.Name)
			models[r.ID] = r.Name
		}
	}
	return agents, models, nil
}

func (c *controller) Close() {
	c.closeOnce.Do(func() { close(c.stop) })
	c.wg.Wait()
}

func (c *controller) worker() {
	defer c.wg.Done()
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			c.writePending()
		case <-c.stop:
			c.writePending()
			return
		}
	}
}

// writePending writes the pending names in one batch per table, and forgets
// them whether or not the write succeeded.
func (c *controller) writePending() {
	c.mu.Lock()
	pendingAgents, pendingModels := c.pendingAgents, c.pendingModels
	c.pendingAgents, c.pendingModels = make(map[int64]string), make(map[int64]string)
	c.mu.Unlock()

	// Not the app's context: this also runs at shutdown, after it is cancelled.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if len(pendingAgents) > 0 {
		agents := make([]model.Agent, 0, len(pendingAgents))
		for id, name := range pendingAgents {
			agents = append(agents, model.Agent{ID: id, Name: name})
		}
		if err := c.repository.CreateAgents(ctx, agents); err != nil {
			zlog.Warn().Err(err).Int("count", len(agents)).Msg("[taskagent] could not write new agent names; dropped them, they stay named in memory until restart")
		}
	}
	if len(pendingModels) > 0 {
		models := make([]model.AgentModel, 0, len(pendingModels))
		for id, name := range pendingModels {
			models = append(models, model.AgentModel{ID: id, Name: name})
		}
		if err := c.repository.CreateAgentModels(ctx, models); err != nil {
			zlog.Warn().Err(err).Int("count", len(models)).Msg("[taskagent] could not write new model names; dropped them, they stay named in memory until restart")
		}
	}
}

// cachedNames returns the names kv holds for ids, and the IDs it does not.
func (c *controller) cachedNames(ctx context.Context, key func(int64) string, ids []int64) (map[int64]string, []int64) {
	names := make(map[int64]string, len(ids))
	var missing []int64
	for _, id := range ids {
		if id == 0 {
			continue
		}
		if name, ok := c.kv.Get(ctx, key(id)); ok {
			names[id] = name
		} else {
			missing = append(missing, id)
		}
	}
	return names, missing
}

func agentKey(id int64) string { return agentKeyPrefix + strconv.FormatInt(id, 10) }
func modelKey(id int64) string { return modelKeyPrefix + strconv.FormatInt(id, 10) }

// truncateName trims a name and cuts it to max characters.
func truncateName(name string, max int) string {
	name = strings.TrimSpace(name)
	if r := []rune(name); len(r) > max {
		name = strings.TrimSpace(string(r[:max]))
	}
	return name
}

// nameID is the row a name is filed under: its xxhash64, reinterpreted as
// int64 since Postgres has no unsigned bigint.
func nameID(name string) int64 {
	return int64(xxhash.Sum64String(name))
}
