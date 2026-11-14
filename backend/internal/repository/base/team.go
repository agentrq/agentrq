// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package base

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/agentrq/agentrq/backend/internal/data/model"
)

var (
	// ErrLastTeamOwner is a change that would leave a team without an owner.
	ErrLastTeamOwner = errors.New("a team keeps at least one owner")
	// ErrInvitationCapReached is a team that created its most invitations in
	// the window.
	ErrInvitationCapReached = errors.New("the team's invitation cap is reached")
	// ErrInvitationPending is an address the team already has a pending
	// invitation for.
	ErrInvitationPending = errors.New("a pending invitation for the address exists")
	// ErrInvitationNotPending is an invitation already accepted, revoked or
	// expired, or one its inviter may no longer grant.
	ErrInvitationNotPending = errors.New("invitation no longer pending")
	// ErrTeamRoleForbidden is a change the caller's role in the team does
	// not allow.
	ErrTeamRoleForbidden = errors.New("the caller's team role does not allow the change")
)

// CanGrantTeamRole reports whether a member with role may give someone
// granted: an owner any role, an admin admin or member, a member none.
func CanGrantTeamRole(role, granted string) bool {
	return role == model.TeamRoleOwner || (role == model.TeamRoleAdmin && granted != model.TeamRoleOwner)
}

// TeamWithRole is a team, a member's role in it and how many members it has.
type TeamWithRole struct {
	model.Team
	Role        string
	MemberCount int64
}

// TeamMemberWithUser is a member with their account's name, email and
// picture.
type TeamMemberWithUser struct {
	model.TeamMember
	Name    string
	Email   string
	Picture string
}

// WorkspaceShareWithTeam is a share with its team's name.
type WorkspaceShareWithTeam struct {
	model.WorkspaceShare
	TeamName string
}

// TeamWorkspaceRow is a workspace a team may read, with its group in the
// team and its owner.
type TeamWorkspaceRow struct {
	ID           int64
	Name         string
	ForkOfID     int64
	UserID       int64
	GroupName    string
	OwnerName    string
	OwnerPicture string
}

// pendingInvitation is an invitation neither accepted nor revoked; it may
// have expired.
const pendingInvitation = "accepted_at IS NULL AND revoked_at IS NULL"

// lockTeam holds the team's row until the transaction ends, so two requests
// counting the team's owners or invitations run one after the other. It is a
// write, not SELECT ... FOR UPDATE: SQLite drops FOR UPDATE, but a write as
// the transaction's first statement takes its database lock before anything
// is read.
func lockTeam(tx *gorm.DB, teamID int64) error {
	res := tx.Exec("UPDATE teams SET id = id WHERE id = ?", teamID)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// first reads the one row tx finds into a T, or ErrNotFound.
func first[T any](tx *gorm.DB) (T, error) {
	var rows []T
	if err := tx.Limit(1).Find(&rows).Error; err != nil {
		var zero T
		return zero, err
	}
	if len(rows) == 0 {
		var zero T
		return zero, ErrNotFound
	}
	return rows[0], nil
}

// CreateTeam stores a team and its first owner.
func (r *repository) CreateTeam(ctx context.Context, team model.Team, owner model.TeamMember) error {
	return r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&team).Error; err != nil {
			return err
		}
		return tx.Create(&owner).Error
	})
}

func (r *repository) GetTeam(ctx context.Context, id int64) (model.Team, error) {
	return first[model.Team](r.conn(ctx).Where("id = ?", id))
}

// ListTeamsForUser returns the teams the user is in, by name.
func (r *repository) ListTeamsForUser(ctx context.Context, userID int64) ([]TeamWithRole, error) {
	rows := []TeamWithRole{}
	err := r.conn(ctx).Model(&model.Team{}).
		Select("teams.id, teams.created_at, teams.updated_at, teams.name, team_members.role, "+
			"(SELECT COUNT(*) FROM team_members AS all_members WHERE all_members.team_id = teams.id) AS member_count").
		Joins("JOIN team_members ON team_members.team_id = teams.id").
		Where("team_members.user_id = ?", userID).
		Order("teams.name ASC, teams.id ASC").
		Scan(&rows).Error
	return rows, err
}

func (r *repository) CountTeamMembers(ctx context.Context, teamID int64) (int64, error) {
	var n int64
	err := r.conn(ctx).Model(&model.TeamMember{}).Where("team_id = ?", teamID).Count(&n).Error
	return n, err
}

func (r *repository) RenameTeam(ctx context.Context, id int64, name string) error {
	res := r.conn(ctx).Model(&model.Team{}).Where("id = ?", id).Updates(map[string]any{"name": name})
	if res.Error == nil && res.RowsAffected == 0 {
		return ErrNotFound
	}
	return res.Error
}

// DeleteTeam deletes a team with its members, invitations, shares and
// memories, if the caller is one of its owners: ErrNotFound when they are
// not in it, ErrTeamRoleForbidden when they are not an owner. The team is
// locked first, so no invitation, member or share is added to it meanwhile.
func (r *repository) DeleteTeam(ctx context.Context, id, callerID int64) error {
	return r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		caller, err := lockedMember(tx, id, callerID)
		if err != nil {
			return err
		}
		if caller.Role != model.TeamRoleOwner {
			return ErrTeamRoleForbidden
		}
		for _, m := range []any{&model.TeamMember{}, &model.TeamInvitation{}, &model.WorkspaceShare{}, &model.TeamMemory{}} {
			if err := tx.Where("team_id = ?", id).Delete(m).Error; err != nil {
				return err
			}
		}
		return tx.Where("id = ?", id).Delete(&model.Team{}).Error
	})
}

func (r *repository) GetTeamMember(ctx context.Context, teamID, userID int64) (model.TeamMember, error) {
	return first[model.TeamMember](r.conn(ctx).Where("team_id = ? AND user_id = ?", teamID, userID))
}

// ListTeamMembers returns a team's members, the earliest to join first.
func (r *repository) ListTeamMembers(ctx context.Context, teamID int64) ([]TeamMemberWithUser, error) {
	rows := []TeamMemberWithUser{}
	err := r.conn(ctx).Model(&model.TeamMember{}).
		Select("team_members.id, team_members.created_at, team_members.updated_at, team_members.team_id, "+
			"team_members.user_id, team_members.role, users.name, users.email, users.picture").
		Joins("LEFT JOIN users ON users.id = team_members.user_id").
		Where("team_members.team_id = ?", teamID).
		Order("team_members.created_at ASC, team_members.id ASC").
		Scan(&rows).Error
	return rows, err
}

// TeamHasMemberWithEmail reports whether an account with the email, in any
// case, is in the team. email is lowercase.
func (r *repository) TeamHasMemberWithEmail(ctx context.Context, teamID int64, email string) (bool, error) {
	var n int64
	err := r.conn(ctx).Model(&model.TeamMember{}).
		Joins("JOIN users ON users.id = team_members.user_id").
		Where("team_members.team_id = ? AND lower(users.email) = ?", teamID, email).
		Count(&n).Error
	return n > 0, err
}

// member is the user's membership of the team; ErrNotFound when they are
// not in it.
func member(tx *gorm.DB, teamID, userID int64) (model.TeamMember, error) {
	return first[model.TeamMember](tx.Where("team_id = ? AND user_id = ?", teamID, userID))
}

// lockedMember locks the team and returns the user's membership of it, read
// under the lock; ErrNotFound when there is no team or they are not in it.
func lockedMember(tx *gorm.DB, teamID, userID int64) (model.TeamMember, error) {
	if err := lockTeam(tx, teamID); err != nil {
		return model.TeamMember{}, err
	}
	return member(tx, teamID, userID)
}

// keepsAnOwner is ErrLastTeamOwner when the team has one owner or none, so
// an owner may not step down or go.
func keepsAnOwner(tx *gorm.DB, teamID int64) error {
	var owners int64
	if err := tx.Model(&model.TeamMember{}).Where("team_id = ? AND role = ?", teamID, model.TeamRoleOwner).Count(&owners).Error; err != nil {
		return err
	}
	if owners <= 1 {
		return ErrLastTeamOwner
	}
	return nil
}

// managesTeam reports whether a role renames the team and changes its
// members: owners and admins.
func managesTeam(role string) bool {
	return role == model.TeamRoleOwner || role == model.TeamRoleAdmin
}

// SetTeamMemberRole is the caller changing a member's role. Both roles are
// read under the team's lock, so a change committed meanwhile is seen: the
// caller must be an owner or admin, and an owner to make or change an owner
// (ErrTeamRoleForbidden). ErrNotFound when the caller or the member is not
// in the team; ErrLastTeamOwner when it demotes the only owner.
func (r *repository) SetTeamMemberRole(ctx context.Context, teamID, callerID, userID int64, role string) error {
	return r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		caller, err := lockedMember(tx, teamID, callerID)
		if err != nil {
			return err
		}
		if !managesTeam(caller.Role) {
			return ErrTeamRoleForbidden
		}
		m, err := member(tx, teamID, userID)
		if err != nil {
			return err
		}
		if (role == model.TeamRoleOwner || m.Role == model.TeamRoleOwner) && caller.Role != model.TeamRoleOwner {
			return ErrTeamRoleForbidden
		}
		if m.Role == model.TeamRoleOwner && role != model.TeamRoleOwner {
			if err := keepsAnOwner(tx, teamID); err != nil {
				return err
			}
		}
		return tx.Model(&model.TeamMember{}).Where("id = ?", m.ID).Updates(map[string]any{"role": role}).Error
	})
}

// RemoveTeamMember is the caller removing a member, or leaving when the
// member is the caller, and unsharing the member's workspaces from the team.
// Anyone may leave. Removing someone else takes an owner or admin, and
// removing an owner takes an owner (ErrTeamRoleForbidden); both roles are
// read under the team's lock. ErrNotFound when the caller or the member is
// not in the team; ErrLastTeamOwner when the member is its only owner.
func (r *repository) RemoveTeamMember(ctx context.Context, teamID, callerID, userID int64) error {
	return r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		caller, err := lockedMember(tx, teamID, callerID)
		if err != nil {
			return err
		}
		m := caller
		if userID != callerID {
			if !managesTeam(caller.Role) {
				return ErrTeamRoleForbidden
			}
			if m, err = member(tx, teamID, userID); err != nil {
				return err
			}
			if m.Role == model.TeamRoleOwner && caller.Role != model.TeamRoleOwner {
				return ErrTeamRoleForbidden
			}
		}
		if m.Role == model.TeamRoleOwner {
			if err := keepsAnOwner(tx, teamID); err != nil {
				return err
			}
		}
		if err := tx.Where("id = ?", m.ID).Delete(&model.TeamMember{}).Error; err != nil {
			return err
		}
		return tx.Where("team_id = ? AND user_id = ?", teamID, userID).Delete(&model.WorkspaceShare{}).Error
	})
}

// CreateTeamInvitation stores an invitation unless the team created limit or
// more since `since` (ErrInvitationCapReached), counting revoked ones, or
// has a live pending one for the address (ErrInvitationPending). An expired
// pending one for the address is revoked first. The team stays locked from
// the count to the insert, so requests at once cannot pass the cap together.
func (r *repository) CreateTeamInvitation(ctx context.Context, inv model.TeamInvitation, since time.Time, limit int64) error {
	return r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockTeam(tx, inv.TeamID); err != nil {
			return err
		}
		var n int64
		if err := tx.Model(&model.TeamInvitation{}).Where("team_id = ? AND created_at > ?", inv.TeamID, since).Count(&n).Error; err != nil {
			return err
		}
		if n >= limit {
			return ErrInvitationCapReached
		}
		err := tx.Model(&model.TeamInvitation{}).
			Where("team_id = ? AND email = ? AND expires_at <= ? AND "+pendingInvitation, inv.TeamID, inv.Email, inv.CreatedAt).
			Update("revoked_at", inv.CreatedAt).Error
		if err != nil {
			return err
		}
		res := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&inv)
		if res.Error == nil && res.RowsAffected == 0 {
			return ErrInvitationPending
		}
		return res.Error
	})
}

// ListTeamInvitations returns a team's invitations still open at now, newest
// first.
func (r *repository) ListTeamInvitations(ctx context.Context, teamID int64, now time.Time) ([]model.TeamInvitation, error) {
	rows := []model.TeamInvitation{}
	err := r.conn(ctx).Where("team_id = ? AND expires_at > ? AND "+pendingInvitation, teamID, now).
		Order("created_at DESC, id DESC").Find(&rows).Error
	return rows, err
}

// ListInvitationsForEmail returns the invitations to the lowercase address
// still open at now, newest first.
func (r *repository) ListInvitationsForEmail(ctx context.Context, email string, now time.Time) ([]model.TeamInvitation, error) {
	rows := []model.TeamInvitation{}
	err := r.conn(ctx).Where("email = ? AND expires_at > ? AND "+pendingInvitation, email, now).
		Order("created_at DESC, id DESC").Find(&rows).Error
	return rows, err
}

func (r *repository) GetTeamInvitation(ctx context.Context, id int64) (model.TeamInvitation, error) {
	return first[model.TeamInvitation](r.conn(ctx).Where("id = ?", id))
}

func (r *repository) GetTeamInvitationByTokenHash(ctx context.Context, tokenHash string) (model.TeamInvitation, error) {
	return first[model.TeamInvitation](r.conn(ctx).Where("token_hash = ?", tokenHash))
}

// RevokeTeamInvitation revokes a pending invitation of the team; ErrNotFound
// when the team has no such pending invitation.
func (r *repository) RevokeTeamInvitation(ctx context.Context, teamID, id int64, now time.Time) error {
	res := r.conn(ctx).Model(&model.TeamInvitation{}).
		Where("id = ? AND team_id = ? AND "+pendingInvitation, id, teamID).
		Update("revoked_at", now)
	if res.Error == nil && res.RowsAffected == 0 {
		return ErrNotFound
	}
	return res.Error
}

// AcceptTeamInvitation marks the invitation accepted by the member's account
// and adds them to the member's team, keeping the role of one already there.
// The team is locked first: ErrNotFound when it is gone, or the invitation is
// not of it. ErrInvitationNotPending when the invitation was accepted,
// revoked or expired at now, by another accept meanwhile too, or its inviter
// is no longer in the team with a role that may grant the invited one.
func (r *repository) AcceptTeamInvitation(ctx context.Context, id int64, m model.TeamMember, now time.Time) error {
	return r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockTeam(tx, m.TeamID); err != nil {
			return err
		}
		inv, err := first[model.TeamInvitation](tx.Where("id = ? AND team_id = ?", id, m.TeamID))
		if err != nil {
			return err
		}
		if inv.AcceptedAt != nil || inv.RevokedAt != nil || !now.Before(inv.ExpiresAt) {
			return ErrInvitationNotPending
		}
		inviter, err := member(tx, inv.TeamID, inv.InvitedBy)
		if errors.Is(err, ErrNotFound) || (err == nil && !CanGrantTeamRole(inviter.Role, inv.Role)) {
			return ErrInvitationNotPending
		}
		if err != nil {
			return err
		}
		err = tx.Model(&model.TeamInvitation{}).Where("id = ?", id).
			Updates(map[string]any{"accepted_at": now, "accepted_by": m.UserID}).Error
		if err != nil {
			return err
		}
		return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&m).Error
	})
}

// sharesWithTeams selects shares with their team's name.
func sharesWithTeams(tx *gorm.DB) *gorm.DB {
	return tx.Model(&model.WorkspaceShare{}).
		Select("workspace_shares.id, workspace_shares.created_at, workspace_shares.updated_at, workspace_shares.team_id, " +
			"workspace_shares.workspace_id, workspace_shares.user_id, workspace_shares.group_name, teams.name AS team_name").
		Joins("JOIN teams ON teams.id = workspace_shares.team_id")
}

// SetWorkspaceShare shares a workspace with a team, or changes the group of
// its share, and returns the share. The team is locked and the sharer's
// membership read under the lock, so a removal committed meanwhile is seen
// and no share outlives it: ErrNotFound when the team is gone or the sharer
// is not in it.
func (r *repository) SetWorkspaceShare(ctx context.Context, s model.WorkspaceShare) (WorkspaceShareWithTeam, error) {
	var out WorkspaceShareWithTeam
	err := r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := lockedMember(tx, s.TeamID, s.UserID); err != nil {
			return err
		}
		err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "team_id"}, {Name: "workspace_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"group_name", "updated_at"}),
		}).Create(&s).Error
		if err != nil {
			return err
		}
		var rows []WorkspaceShareWithTeam
		if err := sharesWithTeams(tx).Where("workspace_shares.team_id = ? AND workspace_shares.workspace_id = ?", s.TeamID, s.WorkspaceID).Scan(&rows).Error; err != nil {
			return err
		}
		out = rows[0]
		return nil
	})
	return out, err
}

// ListWorkspaceShares returns the owner's shares of a workspace, by team name.
func (r *repository) ListWorkspaceShares(ctx context.Context, workspaceID, userID int64) ([]WorkspaceShareWithTeam, error) {
	rows := []WorkspaceShareWithTeam{}
	err := sharesWithTeams(r.conn(ctx)).
		Where("workspace_shares.workspace_id = ? AND workspace_shares.user_id = ?", workspaceID, userID).
		Order("teams.name ASC, teams.id ASC").
		Scan(&rows).Error
	return rows, err
}

// DeleteWorkspaceShare unshares the owner's workspace from a team;
// ErrNotFound when it was not shared there.
func (r *repository) DeleteWorkspaceShare(ctx context.Context, workspaceID, teamID, userID int64) error {
	res := r.conn(ctx).Where("workspace_id = ? AND team_id = ? AND user_id = ?", workspaceID, teamID, userID).
		Delete(&model.WorkspaceShare{})
	if res.Error == nil && res.RowsAffected == 0 {
		return ErrNotFound
	}
	return res.Error
}

// sharedInTeam joins a workspace to its team's share: its own, or its
// parent's for a fork, and only while the shared workspace is not archived.
const sharedInTeam = "JOIN workspace_shares ON workspace_shares.team_id = ? AND " +
	"(workspace_shares.workspace_id = workspaces.id OR workspace_shares.workspace_id = workspaces.fork_of_id) " +
	"JOIN workspaces AS shared ON shared.id = workspace_shares.workspace_id AND shared.archived_at IS NULL"

// CanReadWorkspaceInTeam reports whether the user is in the team and the
// workspace, not archived, is shared with it, itself or as a fork of a
// shared workspace.
func (r *repository) CanReadWorkspaceInTeam(ctx context.Context, userID, teamID, workspaceID int64) (bool, error) {
	var n int64
	err := r.conn(ctx).Model(&model.Workspace{}).
		Joins(sharedInTeam, teamID).
		Joins("JOIN team_members ON team_members.team_id = workspace_shares.team_id AND team_members.user_id = ?", userID).
		Where("workspaces.id = ? AND workspaces.archived_at IS NULL", workspaceID).
		Count(&n).Error
	return n > 0, err
}

// ListTeamWorkspaces returns the workspaces a team's members may read, by
// group, then name: those shared with it and their forks, none archived.
func (r *repository) ListTeamWorkspaces(ctx context.Context, teamID int64) ([]TeamWorkspaceRow, error) {
	rows := []TeamWorkspaceRow{}
	err := r.conn(ctx).Model(&model.Workspace{}).
		Select("workspaces.id, workspaces.name, workspaces.fork_of_id, workspaces.user_id, workspace_shares.group_name, "+
			"users.name AS owner_name, users.picture AS owner_picture").
		Joins(sharedInTeam, teamID).
		Joins("LEFT JOIN users ON users.id = workspaces.user_id").
		Where("workspaces.archived_at IS NULL").
		Order("workspace_shares.group_name ASC, workspaces.name ASC, workspaces.id ASC").
		Scan(&rows).Error
	return rows, err
}
