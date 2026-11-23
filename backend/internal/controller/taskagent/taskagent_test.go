// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package taskagent

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/cespare/xxhash/v2"
	"github.com/golang/mock/gomock"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/service/kv"
	mock_repo "github.com/agentrq/agentrq/backend/internal/service/mocks/repository"
)

// errDB is what the repository returns when the database refuses a call.
var errDB = errors.New("database unavailable")

// newController is a controller whose worker never writes on its own within a
// test: the tests write the pending names by calling writePending, or Close.
func newController(t *testing.T) (*controller, *mock_repo.MockRepository, kv.Service) {
	t.Helper()
	repo := mock_repo.NewMockRepository(gomock.NewController(t))
	store := kv.New()
	c := New(Params{Repository: repo, KV: store, Interval: time.Hour}).(*controller)
	return c, repo, store
}

// hashOf is the ID a name is filed under.
func hashOf(name string) int64 { return int64(xxhash.Sum64String(name)) }

func sortedNames(agents []model.Agent) []string {
	var names []string
	for _, a := range agents {
		names = append(names, a.Name)
	}
	sort.Strings(names)
	return names
}

func TestNew_WritesEveryMinuteByDefault(t *testing.T) {
	if c := New(Params{}).(*controller); c.interval != time.Minute {
		t.Errorf("write interval = %v, want one minute", c.interval)
	}
}

func TestStart(t *testing.T) {
	ctx := context.Background()

	t.Run("loads every name into kv, and writes pending names on Close", func(t *testing.T) {
		c, repo, store := newController(t)
		repo.EXPECT().ListAgents(ctx, nil).Return([]model.Agent{{ID: 1, Name: "claude-code"}}, nil)
		repo.EXPECT().ListAgentModels(ctx, nil).Return([]model.AgentModel{{ID: 2, Name: "Opus"}}, nil)
		if err := c.Start(ctx); err != nil {
			t.Fatalf("Start: %v", err)
		}
		if name, _ := store.Get(ctx, "agent:1"); name != "claude-code" {
			t.Errorf("kv agent:1 = %q, want claude-code", name)
		}
		if name, _ := store.Get(ctx, "agent_model:2"); name != "Opus" {
			t.Errorf("kv agent_model:2 = %q, want Opus", name)
		}

		c.Register(ctx, entity.TaskAgent{Name: "codex"})
		repo.EXPECT().CreateAgents(gomock.Any(), []model.Agent{{ID: hashOf("codex"), Name: "codex"}}).Return(nil)
		c.Close()
		c.Close() // a second Close must not panic or write again
	})

	t.Run("fails when either table cannot be read", func(t *testing.T) {
		c, repo, _ := newController(t)
		repo.EXPECT().ListAgents(ctx, nil).Return(nil, errDB)
		if err := c.Start(ctx); !errors.Is(err, errDB) {
			t.Errorf("Start = %v, want the agents table's error", err)
		}

		c, repo, _ = newController(t)
		repo.EXPECT().ListAgents(ctx, nil).Return(nil, nil)
		repo.EXPECT().ListAgentModels(ctx, nil).Return(nil, errDB)
		if err := c.Start(ctx); !errors.Is(err, errDB) {
			t.Errorf("Start = %v, want the agent_models table's error", err)
		}
	})
}

func TestWorker_WritesPendingNamesOnEachTick(t *testing.T) {
	repo := mock_repo.NewMockRepository(gomock.NewController(t))
	c := New(Params{Repository: repo, KV: kv.New(), Interval: 10 * time.Millisecond}).(*controller)
	repo.EXPECT().ListAgents(gomock.Any(), nil).Return(nil, nil)
	repo.EXPECT().ListAgentModels(gomock.Any(), nil).Return(nil, nil)
	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	written := make(chan struct{})
	repo.EXPECT().CreateAgents(gomock.Any(), gomock.Any()).DoAndReturn(func(context.Context, []model.Agent) error {
		close(written)
		return nil
	})

	c.Register(context.Background(), entity.TaskAgent{Name: "codex"})
	select {
	case <-written:
	case <-time.After(5 * time.Second):
		t.Fatal("no tick wrote the pending name within 5s")
	}
	c.Close()
}

func TestRegister(t *testing.T) {
	ctx := context.Background()

	t.Run("a new name goes into kv at once and into its table with the next batch", func(t *testing.T) {
		c, repo, store := newController(t)
		want := entity.TaskAgent{Name: "gemini", Model: "Gemini 3 Flash", ID: hashOf("gemini"), ModelID: hashOf("Gemini 3 Flash")}
		for range 2 {
			if got := c.Register(ctx, entity.TaskAgent{Name: " gemini ", Model: "Gemini 3 Flash"}); got != want {
				t.Fatalf("Register = %+v, want %+v", got, want)
			}
		}
		c.Register(ctx, entity.TaskAgent{Name: "claude-code"})
		if name, _ := store.Get(ctx, agentKey(want.ID)); name != "gemini" {
			t.Errorf("kv agent name = %q, want gemini", name)
		}
		if name, _ := store.Get(ctx, modelKey(want.ModelID)); name != "Gemini 3 Flash" {
			t.Errorf("kv model name = %q, want Gemini 3 Flash", name)
		}

		repo.EXPECT().CreateAgents(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, agents []model.Agent) error {
			if got := fmt.Sprint(sortedNames(agents)); got != "[claude-code gemini]" {
				t.Errorf("batch wrote agents %s, want claude-code and gemini once each", got)
			}
			return nil
		})
		repo.EXPECT().CreateAgentModels(gomock.Any(), []model.AgentModel{{ID: want.ModelID, Name: "Gemini 3 Flash"}}).Return(nil)
		c.writePending()
		c.writePending() // nothing is pending any more, so this must not write
	})

	t.Run("a name kv already has is not written again", func(t *testing.T) {
		c, _, store := newController(t)
		store.Set(ctx, agentKey(hashOf("claude-code")), "claude-code")
		c.Register(ctx, entity.TaskAgent{Name: "claude-code"})
		c.writePending() // the mock fails the test on any write
	})

	t.Run("an agent that names nobody is not registered, and neither is its model", func(t *testing.T) {
		c, _, _ := newController(t)
		if got := c.Register(ctx, entity.TaskAgent{Name: "  ", Model: "Opus"}); got != (entity.TaskAgent{}) {
			t.Errorf("Register = %+v, want an empty agent", got)
		}
		c.writePending() // the mock fails the test on any write
	})

	t.Run("long names are cut to the column widths, in characters", func(t *testing.T) {
		c, _, _ := newController(t)
		agent := strings.Repeat("é", maxAgentName-1) + "a"
		model := strings.Repeat("é", maxModelName-2) + " b"
		got := c.Register(ctx, entity.TaskAgent{Name: agent + "tail", Model: model + "tail"})
		if got.Name != agent || got.ID != hashOf(agent) || got.Model != model {
			t.Errorf("Register = %q on %q, want %q on %q", got.Name, got.Model, agent, model)
		}

		// A cut that ends on a space drops the space.
		kept := strings.Repeat("a", maxAgentName-1)
		if got := c.Register(ctx, entity.TaskAgent{Name: kept + " tail"}); got.Name != kept {
			t.Errorf("Register = %q, want %q with no trailing space", got.Name, kept)
		}
	})

	t.Run("a batch takes at most maxPending new names; the rest stay in kv only", func(t *testing.T) {
		c, repo, store := newController(t)
		for i := range maxPending + 5 {
			c.Register(ctx, entity.TaskAgent{Name: fmt.Sprintf("agent-%d", i)})
		}
		last := fmt.Sprintf("agent-%d", maxPending+4)
		if _, ok := store.Get(ctx, agentKey(hashOf(last))); !ok {
			t.Errorf("%s, past the batch limit, is not in kv", last)
		}
		repo.EXPECT().CreateAgents(gomock.Any(), gomock.Len(maxPending)).Return(nil)
		c.writePending()
	})

	t.Run("a batch that fails to write is dropped, not retried", func(t *testing.T) {
		c, repo, _ := newController(t)
		c.Register(ctx, entity.TaskAgent{Name: "codex", Model: "gpt-5"})
		repo.EXPECT().CreateAgents(gomock.Any(), gomock.Any()).Return(errDB)
		repo.EXPECT().CreateAgentModels(gomock.Any(), gomock.Any()).Return(errDB)
		c.writePending()
		c.writePending() // the mock fails the test on a second attempt
	})
}

func TestNames(t *testing.T) {
	ctx := context.Background()

	t.Run("reads kv first, the tables only for what kv lacks, then keeps those in kv", func(t *testing.T) {
		c, repo, store := newController(t)
		store.Set(ctx, agentKey(1), "claude-code")
		repo.EXPECT().ListAgents(ctx, []int64{2, 42}).Return([]model.Agent{{ID: 2, Name: "gemini"}}, nil)
		repo.EXPECT().ListAgentModels(ctx, []int64{3}).Return([]model.AgentModel{{ID: 3, Name: "Opus"}}, nil)

		agents, models, err := c.Names(ctx, []int64{1, 2, 0, 42}, []int64{3})
		if err != nil || len(agents) != 2 || agents[1] != "claude-code" || agents[2] != "gemini" || models[3] != "Opus" {
			t.Fatalf("Names = agents %v, models %v, error %v; want claude-code and gemini on Opus", agents, models, err)
		}

		// Everything is in kv now; the mock fails the test on another read.
		if agents, models, err = c.Names(ctx, []int64{1, 2}, []int64{3}); err != nil || len(agents) != 2 || models[3] != "Opus" {
			t.Errorf("Names = agents %v, models %v, error %v; want the same names from kv", agents, models, err)
		}
	})

	t.Run("fails when either table cannot be read", func(t *testing.T) {
		c, repo, _ := newController(t)
		repo.EXPECT().ListAgents(ctx, []int64{1}).Return(nil, errDB)
		if _, _, err := c.Names(ctx, []int64{1}, nil); !errors.Is(err, errDB) {
			t.Errorf("Names = %v, want the agents table's error", err)
		}
		repo.EXPECT().ListAgentModels(ctx, []int64{2}).Return(nil, errDB)
		if _, _, err := c.Names(ctx, nil, []int64{2}); !errors.Is(err, errDB) {
			t.Errorf("Names = %v, want the agent_models table's error", err)
		}
	})
}
