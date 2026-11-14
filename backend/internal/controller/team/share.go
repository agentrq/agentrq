// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package team

import (
	"context"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
)

func shareEntity(s base.WorkspaceShareWithTeam) entity.WorkspaceShare {
	return entity.WorkspaceShare{
		TeamID: s.TeamID, TeamName: s.TeamName, WorkspaceID: s.WorkspaceID,
		CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt,
	}
}

func (c *controller) ListShares(ctx context.Context, userID, workspaceID int64) ([]entity.WorkspaceShare, error) {
	if _, err := c.repo.GetWorkspace(ctx, workspaceID, userID); err != nil {
		return nil, err
	}
	rows, err := c.repo.ListWorkspaceShares(ctx, workspaceID, userID)
	if err != nil {
		return nil, err
	}
	out := make([]entity.WorkspaceShare, len(rows))
	for i, s := range rows {
		out[i] = shareEntity(s)
	}
	return out, nil
}

// workspaceActivity is a new activity that names the workspace as it was
// called then, which the log keeps after it is renamed or deleted.
func (c *controller) workspaceActivity(ws model.Workspace) model.TeamManagementActivity {
	act := c.activity()
	act.Details = base.TeamActivityDetails(map[string]string{"workspaceName": ws.Name})
	return act
}

// ShareWorkspace shares the account's workspace with a team it is in, where
// it shows by its own name. The repository checks the account is in the
// team, under the team's lock.
func (c *controller) ShareWorkspace(ctx context.Context, ch entity.TeamChange, workspaceID int64) (entity.WorkspaceShare, error) {
	ws, err := c.repo.GetWorkspace(ctx, workspaceID, ch.UserID)
	if err != nil {
		return entity.WorkspaceShare{}, err
	}
	if ws.ForkOfID != 0 {
		return entity.WorkspaceShare{}, teamErr(entity.TeamInvalid, "a fork follows its parent: share the parent workspace instead")
	}
	if ws.ArchivedAt != nil {
		return entity.WorkspaceShare{}, teamErr(entity.TeamInvalid, "an archived workspace cannot be shared")
	}
	now := c.now()
	s, next, err := c.repo.SetWorkspaceShare(ctx, model.WorkspaceShare{
		ID: c.ids.NextID(), TeamID: ch.TeamID, WorkspaceID: workspaceID, UserID: ch.UserID, CreatedAt: now, UpdatedAt: now,
	}, ch.Version, c.workspaceActivity(ws))
	if err != nil {
		return entity.WorkspaceShare{}, fromRepo(err)
	}
	c.emit(ctx, entity.ActionWorkspaceShare, ch.UserID, workspaceID, ch.TeamID)
	out := shareEntity(s)
	out.Version = next
	return out, nil
}

func (c *controller) UnshareWorkspace(ctx context.Context, ch entity.TeamChange, workspaceID int64) (int64, error) {
	ws, err := c.repo.GetWorkspace(ctx, workspaceID, ch.UserID)
	if err != nil {
		return 0, err
	}
	next, err := c.repo.DeleteWorkspaceShare(ctx, workspaceID, ch.TeamID, ch.UserID, ch.Version, c.workspaceActivity(ws))
	return next, fromRepo(err)
}

func (c *controller) CanReadWorkspaceInTeam(ctx context.Context, userID, teamID, workspaceID int64) (bool, error) {
	return c.repo.CanReadWorkspaceInTeam(ctx, userID, teamID, workspaceID)
}

func (c *controller) ListTeamWorkspaces(ctx context.Context, userID, teamID int64) ([]entity.TeamWorkspace, error) {
	if err := c.member(ctx, userID, teamID); err != nil {
		return nil, err
	}
	rows, err := c.repo.ListTeamWorkspaces(ctx, teamID)
	if err != nil {
		return nil, err
	}
	out := make([]entity.TeamWorkspace, len(rows))
	for i, w := range rows {
		out[i] = entity.TeamWorkspace{
			ID: w.ID, Name: w.Name, ForkOfID: w.ForkOfID, SharedName: w.SharedName,
			OwnerID: w.UserID, OwnerName: w.OwnerName, OwnerPicture: w.OwnerPicture,
		}
	}
	return out, nil
}
