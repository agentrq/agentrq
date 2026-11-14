// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package team

import (
	"context"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
)

func shareEntity(s model.WorkspaceShare, team string) entity.WorkspaceShare {
	return entity.WorkspaceShare{
		TeamID: s.TeamID, TeamName: team, WorkspaceID: s.WorkspaceID, Group: s.GroupName,
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
		out[i] = shareEntity(s.WorkspaceShare, s.TeamName)
	}
	return out, nil
}

// ShareWorkspace shares the account's workspace with a team it is in, shown
// there in group, the workspace's name when empty. Sharing it again changes
// the group. The repository checks the account is in the team, under the
// team's lock.
func (c *controller) ShareWorkspace(ctx context.Context, userID, workspaceID, teamID int64, group string) (entity.WorkspaceShare, error) {
	ws, err := c.repo.GetWorkspace(ctx, workspaceID, userID)
	if err != nil {
		return entity.WorkspaceShare{}, err
	}
	if ws.ForkOfID != 0 {
		return entity.WorkspaceShare{}, teamErr(entity.TeamInvalid, "a fork follows its parent: share the parent workspace instead")
	}
	if ws.ArchivedAt != nil {
		return entity.WorkspaceShare{}, teamErr(entity.TeamInvalid, "an archived workspace cannot be shared")
	}
	if group == "" {
		group = ws.Name
	}
	if group, err = cleanName(group, "group"); err != nil {
		return entity.WorkspaceShare{}, err
	}
	now := c.now()
	s, err := c.repo.SetWorkspaceShare(ctx, model.WorkspaceShare{
		ID: c.ids.NextID(), TeamID: teamID, WorkspaceID: workspaceID, UserID: userID, GroupName: group, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return entity.WorkspaceShare{}, err
	}
	c.emit(ctx, entity.ActionWorkspaceShare, userID, workspaceID, teamID)
	return shareEntity(s.WorkspaceShare, s.TeamName), nil
}

func (c *controller) UnshareWorkspace(ctx context.Context, userID, workspaceID, teamID int64) error {
	if _, err := c.repo.GetWorkspace(ctx, workspaceID, userID); err != nil {
		return err
	}
	return c.repo.DeleteWorkspaceShare(ctx, workspaceID, teamID, userID)
}

func (c *controller) CanReadWorkspaceInTeam(ctx context.Context, userID, teamID, workspaceID int64) (bool, error) {
	return c.repo.CanReadWorkspaceInTeam(ctx, userID, teamID, workspaceID)
}

func (c *controller) ListTeamWorkspaces(ctx context.Context, userID, teamID int64) ([]entity.TeamWorkspace, error) {
	if _, err := c.role(ctx, userID, teamID); err != nil {
		return nil, err
	}
	rows, err := c.repo.ListTeamWorkspaces(ctx, teamID)
	if err != nil {
		return nil, err
	}
	out := make([]entity.TeamWorkspace, len(rows))
	for i, w := range rows {
		out[i] = entity.TeamWorkspace{
			ID: w.ID, Name: w.Name, ForkOfID: w.ForkOfID, Group: w.GroupName,
			OwnerID: w.UserID, OwnerName: w.OwnerName, OwnerPicture: w.OwnerPicture,
		}
	}
	return out, nil
}
