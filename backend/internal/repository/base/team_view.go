// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package base

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
)

// Team mode reads: what a team's members see of the workspaces shared with
// it. Who may see a workspace is decided by CanReadWorkspaceInTeam and
// ListTeamWorkspaces alone; the reads below take the workspaces those
// allowed, and never decide it again.

// TeamMemberStatsRow is one owner's share of a team's statistics.
type TeamMemberStatsRow struct {
	UserID         int64
	Name           string
	Picture        string
	TasksCompleted int64
	Messages       int64
}

// WorkspaceMemoryRow names one memory of a workspace.
type WorkspaceMemoryRow struct {
	WorkspaceID int64
	Name        string
	UpdatedAt   time.Time
}

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

// TeamWorkspaceInfo is the workspace with its owner, and its group in the
// team: the share of it, or of the parent it is a fork of. It decides
// nothing: the caller has already checked the workspace with
// CanReadWorkspaceInTeam.
func (r *repository) TeamWorkspaceInfo(ctx context.Context, teamID, workspaceID int64) (TeamWorkspaceRow, error) {
	return first[TeamWorkspaceRow](r.conn(ctx).Model(&model.Workspace{}).
		Select("workspaces.id, workspaces.name, workspaces.fork_of_id, workspaces.user_id, "+
			"COALESCE(workspace_shares.group_name, '') AS group_name, users.name AS owner_name, users.picture AS owner_picture").
		Joins("LEFT JOIN workspace_shares ON workspace_shares.team_id = ? AND "+
			"(workspace_shares.workspace_id = workspaces.id OR workspace_shares.workspace_id = workspaces.fork_of_id)", teamID).
		Joins("LEFT JOIN users ON users.id = workspaces.user_id").
		Where("workspaces.id = ?", workspaceID))
}

// GetDetailedStatsOfWorkspaces is GetDetailedUserStats over the given
// workspaces, whoever owns them, with the breakdown by owner.
func (r *repository) GetDetailedStatsOfWorkspaces(ctx context.Context, workspaceIDs []int64, startTime, endTime int64) (entity.GetDetailedWorkspaceStatsResponse, []TeamMemberStatsRow, error) {
	res, err := r.detailedStats(ctx, "workspace_id IN ?", workspaceIDs, startTime, endTime)
	if err != nil {
		return res, nil, err
	}
	members := make([]TeamMemberStatsRow, 0)
	err = r.conn(ctx).Model(&model.Telemetry{}).
		Select(
			"telemetries.user_id AS user_id, COALESCE(users.name, '') AS name, COALESCE(users.picture, '') AS picture,"+
				" SUM(CASE WHEN telemetries.action = ? THEN 1 ELSE 0 END) AS tasks_completed,"+
				" SUM(CASE WHEN telemetries.action = ? THEN 1 ELSE 0 END) AS messages",
			model.ActionIDTaskComplete, model.ActionIDMessageCreate).
		Joins("LEFT JOIN users ON users.id = telemetries.user_id").
		Where("telemetries.workspace_id IN ?", workspaceIDs).
		Where("telemetries.occurred_at >= ? AND telemetries.occurred_at <= ?", startTime, endTime).
		Group("telemetries.user_id, users.name, users.picture").
		Having(
			"SUM(CASE WHEN telemetries.action = ? THEN 1 ELSE 0 END) > 0"+
				" OR SUM(CASE WHEN telemetries.action = ? THEN 1 ELSE 0 END) > 0",
			model.ActionIDTaskComplete, model.ActionIDMessageCreate).
		Order("tasks_completed DESC, messages DESC, telemetries.user_id ASC").
		Scan(&members).Error
	return res, members, err
}

// GetTeamMemory is a team's memory; ErrNotFound when none was saved.
func (r *repository) GetTeamMemory(ctx context.Context, teamID int64, name string) (model.TeamMemory, error) {
	return first[model.TeamMemory](r.conn(ctx).Where("team_id = ? AND name = ?", teamID, name))
}

// SaveTeamMemory writes a team's memory if mem.UpdatedBy is still in the
// team, read under the team's lock (ErrNotFound when not), and returns the
// one it replaced, if any (found).
func (r *repository) SaveTeamMemory(ctx context.Context, mem model.TeamMemory) (prev model.TeamMemory, found bool, err error) {
	err = r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := lockedMember(tx, mem.TeamID, mem.UpdatedBy); err != nil {
			return err
		}
		switch p, err := first[model.TeamMemory](tx.Where("team_id = ? AND name = ?", mem.TeamID, mem.Name)); err {
		case nil:
			prev, found = p, true
		case ErrNotFound:
		default:
			return err
		}
		return tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "team_id"}, {Name: "name"}},
			DoUpdates: clause.AssignmentColumns([]string{"content", "updated_at", "updated_by"}),
		}).Create(&mem).Error
	})
	return prev, found, err
}

// ListWorkspaceMemoryNames names the memories of the given workspaces, filed
// under each one's owner, by name.
func (r *repository) ListWorkspaceMemoryNames(ctx context.Context, workspaceIDs []int64) ([]WorkspaceMemoryRow, error) {
	rows := []WorkspaceMemoryRow{}
	if len(workspaceIDs) == 0 {
		return rows, nil
	}
	err := r.conn(ctx).Model(&model.Memory{}).
		Select("memories.workspace_id, memories.name, memories.updated_at").
		Joins("JOIN workspaces ON workspaces.id = memories.workspace_id AND workspaces.user_id = memories.user_id").
		Where("memories.workspace_id IN ?", workspaceIDs).
		Order("memories.workspace_id ASC, memories.name ASC").
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
