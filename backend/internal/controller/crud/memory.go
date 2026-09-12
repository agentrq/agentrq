// Copyright 2026 Contextual, Inc. https://agentrq.com

package crud

import (
	"context"
	"fmt"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/mustafaturan/monoflake"
)

// MemoryController reads a workspace's memories.
//
// Read-only on purpose. Agents own the writing, through the MCP memory tools;
// there is no endpoint here that writes one, because an agent reading back a
// memory a human had quietly rewritten is a confusing failure to debug from
// either side.
type MemoryController interface {
	ListMemories(ctx context.Context, req entity.ListMemoriesRequest) (*entity.ListMemoriesResponse, error)
	GetMemory(ctx context.Context, req entity.GetMemoryRequest) (*entity.GetMemoryResponse, error)
}

// memoryOwner resolves the account a workspace's memories are filed under, and
// refuses a caller who does not own the workspace.
//
// Memories are keyed to the workspace's owner, so this is also what stops one
// account reading another's notes by guessing a workspace ID.
func (c *controller) memoryOwner(ctx context.Context, workspaceID int64, userID string) (int64, error) {
	uid := monoflake.IDFromBase62(userID).Int64()
	if uid == 0 || workspaceID == 0 {
		return 0, fmt.Errorf("invalid request")
	}
	ok, err := c.CheckWorkspaceAccess(ctx, workspaceID, userID)
	if err != nil {
		return 0, fmt.Errorf("check workspace access: %w", err)
	}
	if !ok {
		// Not found rather than forbidden, matching every other route under
		// /workspaces/:id: those scope their query by user, so a workspace you
		// do not own simply is not there. Answering "forbidden" here would
		// confirm the workspace exists to someone who cannot see it.
		return 0, base.ErrNotFound
	}
	return uid, nil
}

func (c *controller) ListMemories(ctx context.Context, req entity.ListMemoriesRequest) (*entity.ListMemoriesResponse, error) {
	uid, err := c.memoryOwner(ctx, req.WorkspaceID, req.UserID)
	if err != nil {
		return nil, err
	}

	models, err := c.repository.ListMemoriesByWorkspace(ctx, uid, req.WorkspaceID)
	if err != nil {
		return nil, err
	}

	memories := make([]entity.Memory, len(models))
	for i, m := range models {
		e := fromModelMemory(m)
		// The list says how big each memory is without carrying it: that is
		// what the settings screen needs to draw an index.
		e.Content = ""
		memories[i] = e
	}
	return &entity.ListMemoriesResponse{Memories: memories}, nil
}

func (c *controller) GetMemory(ctx context.Context, req entity.GetMemoryRequest) (*entity.GetMemoryResponse, error) {
	uid, err := c.memoryOwner(ctx, req.WorkspaceID, req.UserID)
	if err != nil {
		return nil, err
	}
	if req.Name == "" {
		return nil, fmt.Errorf("invalid request")
	}

	m, err := c.repository.GetMemory(ctx, uid, req.WorkspaceID, req.Name)
	if err != nil {
		return nil, err
	}
	return &entity.GetMemoryResponse{Memory: fromModelMemory(m)}, nil
}

func fromModelMemory(m model.Memory) entity.Memory {
	return entity.Memory{
		ID:          m.ID,
		CreatedAt:   m.CreatedAt,
		UpdatedAt:   m.UpdatedAt,
		WorkspaceID: m.WorkspaceID,
		Name:        m.Name,
		Content:     m.Content,
		SizeBytes:   len(m.Content),
	}
}
