// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"context"
	"errors"
	"time"

	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/idgen"
	"github.com/mustafaturan/monoflake"
)

// mcpWorkspace reads the workspace an MCP server is being built for, and
// returns it with the account it belongs to and the workspace whose memory,
// skills, site shares and attachment files it uses — its parent, for a fork.
// A fork's parent never changes, so this is resolved once per server. A
// workspace that cannot be read is taken to be userID's, and its own.
func mcpWorkspace(ctx context.Context, repo base.Repository, workspaceID int64, userID string) (model.Workspace, string, int64) {
	w, err := repo.SystemGetWorkspace(ctx, workspaceID)
	if err != nil {
		return w, userID, workspaceID
	}
	return w, monoflake.ID(w.UserID).String(), w.ContentID()
}

// workspaceMemory is a workspace's memory for its MCP server. Scoped to the
// workspace and its owner here, so the tools themselves only ever name a
// memory — an agent cannot reach another workspace's notes by asking for them.
type workspaceMemory struct {
	repo        base.Repository
	ids         idgen.Service
	workspaceID int64
	userID      string
}

func (m workspaceMemory) uid() int64 { return monoflake.IDFromBase62(m.userID).Int64() }

func (m workspaceMemory) load(ctx context.Context, name string) (string, int64, bool, error) {
	mem, err := m.repo.GetMemory(ctx, m.uid(), m.workspaceID, name)
	if errors.Is(err, base.ErrNotFound) {
		// A memory nobody has written yet: the ordinary state of a
		// fresh workspace, and not a failure to report.
		return "", 0, false, nil
	}
	if err != nil {
		return "", 0, false, err
	}
	return mem.Content, mem.Version, true, nil
}

func (m workspaceMemory) save(ctx context.Context, name string, content string, version int64) (int64, error) {
	now := time.Now()
	mem, err := m.repo.SaveMemory(ctx, model.Memory{
		ID:          m.ids.NextID(),
		CreatedAt:   now,
		UpdatedAt:   now,
		UserID:      m.uid(),
		WorkspaceID: m.workspaceID,
		Name:        name,
		Content:     content,
		Version:     version,
	})
	return mem.Version, err
}

func (m workspaceMemory) delete(ctx context.Context, name string) (bool, error) {
	err := m.repo.DeleteMemory(ctx, m.uid(), m.workspaceID, name)
	if errors.Is(err, base.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
