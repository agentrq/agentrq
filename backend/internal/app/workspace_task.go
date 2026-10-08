// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/mustafaturan/monoflake"
)

// workspaceTask reads a task an agent named, and refuses one outside the
// workspace its MCP server serves. Every write an agent makes against a task
// passes here first: the database only checks that the task exists, so without
// it one workspace's token could post into any task, in any workspace.
func workspaceTask(ctx context.Context, repo base.Repository, workspaceID int64, owner string, taskID int64) (model.Task, error) {
	t, err := repo.GetTask(ctx, workspaceID, taskID, monoflake.IDFromBase62(owner).Int64())
	if errors.Is(err, base.ErrNotFound) {
		return t, fmt.Errorf("task %s is not in this workspace", monoflake.ID(taskID).String())
	}
	return t, err
}
