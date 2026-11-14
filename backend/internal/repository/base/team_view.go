// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package base

import (
	"context"
	"time"

	"github.com/agentrq/agentrq/backend/internal/data/model"
)

// Team mode reads: what a team's members see of the workspaces shared with
// it. Who may see a workspace is decided by CanReadWorkspaceInTeam and
// ListTeamWorkspaces alone; the reads below take the workspaces those
// allowed, and never decide it again.

// WorkspaceSkillRow is a skill on in a workspace, as its owner has it.
type WorkspaceSkillRow struct {
	WorkspaceID int64
	UserID      int64
	SkillID     int64
	Name        string
	Description string
	UpdatedAt   time.Time
}

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
// the workspace is shared with, itself included, with its owner's name: the
// candidates for its team workspace, which the caller narrows by name.
func (r *repository) ListTeamPeerWorkspaces(ctx context.Context, workspaceID int64) ([]TeamWorkspacePeer, error) {
	rows := []TeamWorkspacePeer{}
	err := r.conn(ctx).Table("workspace_shares AS mine").
		Select("peer_shares.team_id, peers.id AS workspace_id, peers.user_id, peers.name, COALESCE(users.name, '') AS owner_name").
		Joins("JOIN workspaces AS me ON me.id = mine.workspace_id AND me.archived_at IS NULL").
		Joins("JOIN workspace_shares AS peer_shares ON peer_shares.team_id = mine.team_id").
		Joins("JOIN workspaces AS peers ON peers.id = peer_shares.workspace_id AND peers.archived_at IS NULL").
		Joins("LEFT JOIN users ON users.id = peers.user_id").
		Where("mine.workspace_id = ?", workspaceID).
		Order("peer_shares.team_id ASC, peers.id ASC").
		Scan(&rows).Error
	return rows, err
}

// ListSkillsOnInWorkspaces lists the skills on in the given workspaces that
// their owner has not turned off, by name.
func (r *repository) ListSkillsOnInWorkspaces(ctx context.Context, workspaceIDs []int64) ([]WorkspaceSkillRow, error) {
	rows := []WorkspaceSkillRow{}
	if len(workspaceIDs) == 0 {
		return rows, nil
	}
	err := r.conn(ctx).Model(&model.WorkspaceSkill{}).
		Select("workspace_skills.workspace_id, skills.user_id, skills.id AS skill_id, skills.name, skills.description, skills.updated_at").
		Joins("JOIN workspaces ON workspaces.id = workspace_skills.workspace_id").
		Joins("JOIN skills ON skills.id = workspace_skills.skill_id AND skills.user_id = workspaces.user_id").
		Where("workspace_skills.workspace_id IN ?", workspaceIDs).
		Where("skills.disabled IS NULL OR skills.disabled = ?", false).
		Order("skills.name ASC, skills.updated_at DESC, workspace_skills.workspace_id ASC").
		Scan(&rows).Error
	return rows, err
}

// ListTeamReaders is every member of every team who reads the workspace
// there: the rule of CanReadWorkspaceInTeam, for all teams at once.
func (r *repository) ListTeamReaders(ctx context.Context, workspaceID int64) ([]TeamReader, error) {
	rows := []TeamReader{}
	err := r.conn(ctx).Model(&model.Workspace{}).
		Distinct("workspace_shares.team_id", "team_members.user_id").
		Joins(sharedWorkspace).
		Joins("JOIN team_members ON team_members.team_id = workspace_shares.team_id").
		Where("workspaces.id = ? AND workspaces.archived_at IS NULL", workspaceID).
		Order("workspace_shares.team_id ASC, team_members.user_id ASC").
		Scan(&rows).Error
	return rows, err
}
