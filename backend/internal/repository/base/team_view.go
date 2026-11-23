// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package base

import (
	"context"
	"time"

	"github.com/agentrq/agentrq/backend/internal/data/model"
	"gorm.io/gorm/clause"
)

// Team mode reads: what a team's members see of the workspaces shared with
// it. Who may see a workspace is decided by CanReadWorkspaceInTeam and
// ListTeamWorkspaces alone; the reads below take the workspaces those
// allowed, and never decide it again.

// TeamReader is a member who reads a workspace through a team.
type TeamReader struct {
	TeamID int64
	UserID int64
}

// TeamWorkspaceInfo is the workspace with its owner, and the name of the
// shared workspace it shows under: its own, or its parent's for a fork. It
// decides nothing: the caller has already checked the workspace with
// CanReadWorkspaceInTeam.
func (r *repository) TeamWorkspaceInfo(ctx context.Context, workspaceID int64) (TeamWorkspaceRow, error) {
	return first[TeamWorkspaceRow](r.conn(ctx).Model(&model.Workspace{}).
		Select("workspaces.id, workspaces.name, workspaces.fork_of_id, workspaces.user_id, "+
			"COALESCE(parent.name, workspaces.name) AS shared_name, users.name AS owner_name, users.picture AS owner_picture").
		Joins("LEFT JOIN workspaces AS parent ON parent.id = workspaces.fork_of_id").
		Joins("LEFT JOIN users ON users.id = workspaces.user_id").
		Where("workspaces.id = ?", workspaceID))
}

// ListTeamPeerWorkspaces is every workspace shared, unarchived, with a team
// the workspace is shared with, itself included, with its owner's name and
// its team workspace: the candidates for the workspace's own team
// workspaces, which the caller narrows by id. A share that holds its memory
// back from the team is left out, but for the workspace itself.
func (r *repository) ListTeamPeerWorkspaces(ctx context.Context, workspaceID int64) ([]TeamWorkspacePeer, error) {
	rows := []TeamWorkspacePeer{}
	err := r.conn(ctx).Table("workspace_shares AS mine").
		Select("peer_shares.team_id, peer_shares.team_workspace_id, peers.id AS workspace_id, peers.user_id, peers.name, "+
			"COALESCE(users.name, '') AS owner_name, COALESCE(users.picture, '') AS owner_picture").
		Joins("JOIN workspaces AS me ON me.id = mine.workspace_id AND me.archived_at IS NULL").
		Joins("JOIN workspace_shares AS peer_shares ON peer_shares.team_id = mine.team_id").
		Joins("JOIN workspaces AS peers ON peers.id = peer_shares.workspace_id AND peers.archived_at IS NULL").
		Joins("LEFT JOIN users ON users.id = peers.user_id").
		Where("mine.workspace_id = ?", workspaceID).
		Where("(peers.id = mine.workspace_id OR peer_shares.hide_memory = ?)", false).
		Order("peer_shares.team_id ASC, peers.id ASC").
		Scan(&rows).Error
	return rows, err
}

// ListTeamReaders is every member of every team who reads the workspace's
// tasks there: the rule of CanReadTasksInTeam, for all teams at once.
func (r *repository) ListTeamReaders(ctx context.Context, workspaceID int64) ([]TeamReader, error) {
	rows := []TeamReader{}
	err := r.conn(ctx).Model(&model.Workspace{}).
		Distinct("workspace_shares.team_id", "team_members.user_id").
		Joins(sharedWorkspace).
		Joins("JOIN team_members ON team_members.team_id = workspace_shares.team_id").
		Where("workspaces.id = ? AND workspaces.archived_at IS NULL", workspaceID).
		Where("workspace_shares.hide_tasks = ?", false).
		Order("workspace_shares.team_id ASC, team_members.user_id ASC").
		Scan(&rows).Error
	return rows, err
}

// ListMemoryExclusions is the teammates the user left out of the memory
// index of their workspace, by id.
func (r *repository) ListMemoryExclusions(ctx context.Context, userID, workspaceID int64) ([]int64, error) {
	ids := []int64{}
	err := r.conn(ctx).Model(&model.MemoryExclusion{}).
		Where("workspace_id = ? AND user_id = ?", workspaceID, userID).
		Order("excluded_user_id ASC").
		Pluck("excluded_user_id", &ids).Error
	return ids, err
}

// ExcludeTeammateMemory leaves a teammate out of the memory index of the
// user's workspace. Leaving them out again is not an error.
func (r *repository) ExcludeTeammateMemory(ctx context.Context, e model.MemoryExclusion) error {
	return r.conn(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&e).Error
}

// IncludeTeammateMemory takes a teammate back into the memory index of the
// user's workspace. Including them again is not an error.
func (r *repository) IncludeTeammateMemory(ctx context.Context, userID, workspaceID, teammateID int64) error {
	return r.conn(ctx).
		Where("workspace_id = ? AND user_id = ? AND excluded_user_id = ?", workspaceID, userID, teammateID).
		Delete(&model.MemoryExclusion{}).Error
}

// MemoryUpdates is when each of the workspaces last had a memory written,
// by workspace id; a workspace with no memory is not in it. The latest is
// taken here rather than with MAX in SQL, which SQLite answers as text.
func (r *repository) MemoryUpdates(ctx context.Context, workspaceIDs []int64) (map[int64]time.Time, error) {
	out := map[int64]time.Time{}
	if len(workspaceIDs) == 0 {
		return out, nil
	}
	rows := []model.Memory{}
	err := r.conn(ctx).Model(&model.Memory{}).Select("workspace_id, updated_at").
		Where("workspace_id IN ?", workspaceIDs).Find(&rows).Error
	for _, m := range rows {
		if m.UpdatedAt.After(out[m.WorkspaceID]) {
			out[m.WorkspaceID] = m.UpdatedAt
		}
	}
	return out, err
}
