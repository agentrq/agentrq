// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package crud

import (
	"context"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
)

// TeamReadController is what a team's reads need beyond the owner-checked
// methods. The System ones check nothing: controller/teamview calls them only
// with workspaces and accounts its team check allowed.
type TeamReadController interface {
	// SystemListTasksInWorkspaces is ListTasks over the given workspaces,
	// whoever owns them, newest activity first.
	SystemListTasksInWorkspaces(ctx context.Context, req entity.ListTasksRequest, workspaceIDs []int64) (*entity.ListTasksResponse, error)
	// SystemGetDetailedStatsOfWorkspaces is GetDetailedWorkspaceStats summed
	// over the given workspaces, whoever owns them.
	SystemGetDetailedStatsOfWorkspaces(ctx context.Context, req entity.GetStatsOfWorkspacesRequest) (*entity.GetDetailedWorkspaceStatsResponse, error)
}

func (c *controller) SystemListTasksInWorkspaces(ctx context.Context, req entity.ListTasksRequest, workspaceIDs []int64) (*entity.ListTasksResponse, error) {
	ms, err := c.repository.ListTasksInWorkspaces(ctx, req, workspaceIDs)
	if err != nil {
		return nil, err
	}
	return c.taskListResponse(ctx, ms, req.PreloadAgents)
}

func (c *controller) SystemGetDetailedStatsOfWorkspaces(ctx context.Context, req entity.GetStatsOfWorkspacesRequest) (*entity.GetDetailedWorkspaceStatsResponse, error) {
	startTime, endTime := statsWindow(req.Range, req.From, req.To, time.Now())
	res, err := c.repository.GetDetailedStatsOfWorkspaces(ctx, req.WorkspaceIDs, startTime, endTime)
	if err != nil {
		return nil, err
	}
	return &res, nil
}
