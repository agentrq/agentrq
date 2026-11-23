// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package app

import (
	"context"
	"errors"
	"testing"

	"github.com/golang/mock/gomock"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	mock_repository "github.com/agentrq/agentrq/backend/internal/service/mocks/repository"
	"github.com/agentrq/agentrq/backend/internal/service/server"
)

func TestStartTaskAgents(t *testing.T) {
	ctx := context.Background()

	t.Run("loads the names before the first request needs them", func(t *testing.T) {
		repo := mock_repository.NewMockRepository(gomock.NewController(t))
		repo.EXPECT().ListAgents(ctx, nil).Return([]model.Agent{{ID: 1, Name: "claude-code"}}, nil)
		repo.EXPECT().ListAgentModels(ctx, nil).Return(nil, nil)
		ctrl, err := startTaskAgents(ctx, repo)
		if err != nil {
			t.Fatalf("startTaskAgents: %v", err)
		}
		defer ctrl.Close()
		// Served from kv: the mock fails the test on another read.
		if agents, _, err := ctrl.Names(ctx, []int64{1}, nil); err != nil || agents[1] != "claude-code" {
			t.Errorf("Names = %v, %v; want claude-code from kv", agents, err)
		}
	})

	t.Run("fails the startup when the names cannot be read", func(t *testing.T) {
		repo := mock_repository.NewMockRepository(gomock.NewController(t))
		repo.EXPECT().ListAgents(ctx, nil).Return(nil, errors.New("database unavailable"))
		if _, err := startTaskAgents(ctx, repo); err == nil {
			t.Fatal("startTaskAgents succeeded, want the database error")
		}
	})
}

type stubServer struct{ shutdown bool }

func (s *stubServer) Run() error                         { return nil }
func (s *stubServer) Shutdown(ctx context.Context) error { s.shutdown = true; return nil }

var _ server.Service = (*stubServer)(nil)

// Shutdown writes the agent names still pending before the server stops.
func TestShutdownClosesTaskAgents(t *testing.T) {
	ctx := context.Background()
	repo := mock_repository.NewMockRepository(gomock.NewController(t))
	repo.EXPECT().ListAgents(ctx, nil).Return(nil, nil)
	repo.EXPECT().ListAgentModels(ctx, nil).Return(nil, nil)
	ctrl, err := startTaskAgents(ctx, repo)
	if err != nil {
		t.Fatalf("startTaskAgents: %v", err)
	}
	codex := ctrl.Register(ctx, entity.TaskAgent{Name: "codex"})
	repo.EXPECT().CreateAgents(gomock.Any(), []model.Agent{{ID: codex.ID, Name: "codex"}}).Return(nil)

	srv := &stubServer{}
	a := &App{server: srv, taskAgents: ctrl, cancel: func() {}}
	if err := a.Shutdown(ctx); err != nil || !srv.shutdown {
		t.Fatalf("Shutdown = %v and server stopped = %v; want no error and the server stopped", err, srv.shutdown)
	}
}
