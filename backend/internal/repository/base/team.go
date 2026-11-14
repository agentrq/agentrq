// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package base

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/agentrq/agentrq/backend/internal/data/model"
)

var (
	// ErrInvitationCapReached is a team that created its most invitations in
	// the window.
	ErrInvitationCapReached = errors.New("the team's invitation cap is reached")
	// ErrUserInvitationCapReached is an inviter who created their most
	// invitations in the window, across every team.
	ErrUserInvitationCapReached = errors.New("the inviter's invitation cap is reached")
	// ErrInvitationPending is an address the team already has a pending
	// invitation for.
	ErrInvitationPending = errors.New("a pending invitation for the address exists")
	// ErrInvitationNotPending is an invitation already accepted, revoked or
	// expired, or one whose inviter has left the team.
	ErrInvitationNotPending = errors.New("invitation no longer pending")
)

// TeamVersionConflict is a team change made from a version another change
// has replaced. Current is the team's version now.
type TeamVersionConflict struct {
	Current int64
}

func (e *TeamVersionConflict) Error() string {
	return fmt.Sprintf("the team changed: it is at version %d", e.Current)
}

// TeamWithCount is a team and how many members it has.
type TeamWithCount struct {
	model.Team
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

// TeamWorkspaceRow is a workspace a team may read, with its owner and the
// name of the shared workspace it shows under: its own, or its parent's for
// a fork.
type TeamWorkspaceRow struct {
	ID           int64
	Name         string
	ForkOfID     int64
	UserID       int64
	SharedName   string
	OwnerName    string
	OwnerPicture string
}

// TeamActivityRow is a team's activity with its actor's and its target's
// names and pictures, empty for an account that is gone.
type TeamActivityRow struct {
	model.TeamManagementActivity
	ActorName     string
	ActorPicture  string
	TargetName    string
	TargetPicture string
}

// TeamWorkspacePeer is a workspace shared with a team, and its owner's name.
type TeamWorkspacePeer struct {
	TeamID      int64
	WorkspaceID int64
	UserID      int64
	Name        string
	OwnerName   string
}

// pendingInvitation is an invitation neither accepted nor revoked; it may
// have expired.
const pendingInvitation = "accepted_at IS NULL AND revoked_at IS NULL"

// changeTeam moves the team from version to the next, as the first
// statement of a team change, and returns the new version. The UPDATE holds
// the team's row until the transaction ends (Postgres's row lock, SQLite's
// write lock), so whatever the change reads next, such as the caller's
// membership or the invitations to count, no other change alters meanwhile.
// ErrNotFound when the team is gone or the caller is not in it, whatever
// the version: a stranger learns nothing. *TeamVersionConflict when another
// change landed first.
func changeTeam(tx *gorm.DB, teamID, callerID, version int64, at time.Time) (int64, error) {
	res := tx.Exec("UPDATE teams SET version = version + 1, updated_at = ? WHERE id = ? AND version = ?", at, teamID, version)
	if res.Error != nil {
		return 0, res.Error
	}
	if _, err := member(tx, teamID, callerID); err != nil {
		return 0, err
	}
	if res.RowsAffected == 0 {
		t, err := first[model.Team](tx.Where("id = ?", teamID))
		if err != nil {
			return 0, err
		}
		return 0, &TeamVersionConflict{Current: t.Version}
	}
	return version + 1, nil
}

// bumpTeam moves the team to its next version, whichever it is, for a
// change made by someone who never loaded it: an invitee accepting or
// declining. It holds the team's row as changeTeam does. ErrNotFound when
// the team is gone.
func bumpTeam(tx *gorm.DB, teamID int64, at time.Time) error {
	res := tx.Exec("UPDATE teams SET version = version + 1, updated_at = ? WHERE id = ?", at, teamID)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// lockUser holds the account's row until the transaction ends, as
// changeTeam does a team's, so two requests counting the account's
// invitations across teams run one after the other.
func lockUser(tx *gorm.DB, userID int64) error {
	res := tx.Exec("UPDATE users SET id = id WHERE id = ?", userID)
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

// TeamActivityDetails is an activity's details as stored: a JSON object.
func TeamActivityDetails(v map[string]string) string {
	if len(v) == 0 {
		return "{}"
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// record writes the activity of a change, in the change's transaction.
func record(tx *gorm.DB, act model.TeamManagementActivity) error {
	if act.Details == "" {
		act.Details = "{}"
	}
	return tx.Create(&act).Error
}

// CreateTeam stores a team at version 1, its first member and the activity.
func (r *repository) CreateTeam(ctx context.Context, team model.Team, first model.TeamMember, act model.TeamManagementActivity) error {
	return r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		team.Version = 1
		if err := tx.Create(&team).Error; err != nil {
			return err
		}
		if err := tx.Create(&first).Error; err != nil {
			return err
		}
		act.TeamID, act.Action = team.ID, model.TeamActivityTeamCreated
		return record(tx, act)
	})
}

func (r *repository) GetTeam(ctx context.Context, id int64) (model.Team, error) {
	return first[model.Team](r.conn(ctx).Where("id = ?", id))
}

// ListTeamsForUser returns the teams the user is in, by name.
func (r *repository) ListTeamsForUser(ctx context.Context, userID int64) ([]TeamWithCount, error) {
	rows := []TeamWithCount{}
	err := r.conn(ctx).Model(&model.Team{}).
		Select("teams.id, teams.created_at, teams.updated_at, teams.name, teams.version, "+
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

// RenameTeam renames the team from version, as a member, recording the old
// and the new name, and returns the new version.
func (r *repository) RenameTeam(ctx context.Context, teamID, callerID, version int64, name string, act model.TeamManagementActivity) (int64, error) {
	var next int64
	err := r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		if next, err = changeTeam(tx, teamID, callerID, version, act.CreatedAt); err != nil {
			return err
		}
		t, err := first[model.Team](tx.Where("id = ?", teamID))
		if err != nil {
			return err
		}
		if err := tx.Model(&model.Team{}).Where("id = ?", teamID).Updates(map[string]any{"name": name}).Error; err != nil {
			return err
		}
		act.TeamID, act.ActorUserID, act.Action = teamID, callerID, model.TeamActivityTeamRenamed
		act.Details = TeamActivityDetails(map[string]string{"from": t.Name, "to": name})
		return record(tx, act)
	})
	return next, err
}

// deleteTeamRows deletes a team with its members, invitations and shares,
// and records it. Its activities stay.
func deleteTeamRows(tx *gorm.DB, teamID int64, act model.TeamManagementActivity) error {
	t, err := first[model.Team](tx.Where("id = ?", teamID))
	if err != nil {
		return err
	}
	for _, m := range []any{&model.TeamMember{}, &model.TeamInvitation{}, &model.WorkspaceShare{}} {
		if err := tx.Where("team_id = ?", teamID).Delete(m).Error; err != nil {
			return err
		}
	}
	if err := tx.Where("id = ?", teamID).Delete(&model.Team{}).Error; err != nil {
		return err
	}
	act.TeamID, act.Action = teamID, model.TeamActivityTeamDeleted
	act.Details = TeamActivityDetails(map[string]string{"name": t.Name})
	return record(tx, act)
}

// DeleteTeam deletes a team from version, as a member, with its members,
// invitations and shares.
func (r *repository) DeleteTeam(ctx context.Context, teamID, callerID, version int64, act model.TeamManagementActivity) error {
	return r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := changeTeam(tx, teamID, callerID, version, act.CreatedAt); err != nil {
			return err
		}
		act.ActorUserID = callerID
		return deleteTeamRows(tx, teamID, act)
	})
}

func (r *repository) GetTeamMember(ctx context.Context, teamID, userID int64) (model.TeamMember, error) {
	return member(r.conn(ctx), teamID, userID)
}

// ListTeamMembers returns a team's members, the earliest to join first.
func (r *repository) ListTeamMembers(ctx context.Context, teamID int64) ([]TeamMemberWithUser, error) {
	rows := []TeamMemberWithUser{}
	err := r.conn(ctx).Model(&model.TeamMember{}).
		Select("team_members.id, team_members.created_at, team_members.updated_at, team_members.team_id, "+
			"team_members.user_id, users.name, users.email, users.picture").
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

// RemoveTeamMember is the caller, from version, removing a member, or leaving
// when the member is the caller, and unsharing the member's workspaces from
// the team. The last member to go deletes the team (deleted), recorded as
// activity deletedID. ErrNotFound when the caller or the member is not in
// the team.
func (r *repository) RemoveTeamMember(ctx context.Context, teamID, callerID, userID, version int64, act model.TeamManagementActivity, deletedID int64) (next int64, deleted bool, err error) {
	err = r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		if next, err = changeTeam(tx, teamID, callerID, version, act.CreatedAt); err != nil {
			return err
		}
		m, err := member(tx, teamID, userID)
		if err != nil {
			return err
		}
		if err := tx.Where("id = ?", m.ID).Delete(&model.TeamMember{}).Error; err != nil {
			return err
		}
		if err := tx.Where("team_id = ? AND user_id = ?", teamID, userID).Delete(&model.WorkspaceShare{}).Error; err != nil {
			return err
		}
		act.TeamID, act.ActorUserID, act.Action = teamID, callerID, model.TeamActivityMemberLeft
		if userID != callerID {
			act.Action, act.TargetUserID = model.TeamActivityMemberRemoved, &userID
		}
		if err := record(tx, act); err != nil {
			return err
		}
		var left int64
		if err := tx.Model(&model.TeamMember{}).Where("team_id = ?", teamID).Count(&left).Error; err != nil {
			return err
		}
		if left > 0 {
			return nil
		}
		deleted = true
		act.ID, act.TargetUserID = deletedID, nil
		return deleteTeamRows(tx, teamID, act)
	})
	return next, deleted, err
}

// CreateTeamInvitation stores an invitation from version, as the inviter, a
// member, unless the team created teamLimit or more since `since`
// (ErrInvitationCapReached), or its inviter created userLimit or more in any
// team (ErrUserInvitationCapReached; 0 is no such cap), counting revoked
// ones, or the team has a live pending one for the address
// (ErrInvitationPending). An expired pending one for the address is revoked
// first. The team, and the inviter when userLimit is set, stay locked from
// the count to the insert, so requests at once cannot pass a cap together.
func (r *repository) CreateTeamInvitation(ctx context.Context, inv model.TeamInvitation, version int64, since time.Time, teamLimit, userLimit int64, act model.TeamManagementActivity) (int64, error) {
	var next int64
	err := r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		if next, err = changeTeam(tx, inv.TeamID, inv.InvitedBy, version, inv.CreatedAt); err != nil {
			return err
		}
		var n int64
		if err := tx.Model(&model.TeamInvitation{}).Where("team_id = ? AND created_at > ?", inv.TeamID, since).Count(&n).Error; err != nil {
			return err
		}
		if n >= teamLimit {
			return ErrInvitationCapReached
		}
		if userLimit > 0 {
			if err := lockUser(tx, inv.InvitedBy); err != nil {
				return err
			}
			if err := tx.Model(&model.TeamInvitation{}).Where("invited_by = ? AND created_at > ?", inv.InvitedBy, since).Count(&n).Error; err != nil {
				return err
			}
			if n >= userLimit {
				return ErrUserInvitationCapReached
			}
		}
		err = tx.Model(&model.TeamInvitation{}).
			Where("team_id = ? AND email = ? AND expires_at <= ? AND "+pendingInvitation, inv.TeamID, inv.Email, inv.CreatedAt).
			Update("revoked_at", inv.CreatedAt).Error
		if err != nil {
			return err
		}
		res := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&inv)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrInvitationPending
		}
		act.TeamID, act.ActorUserID, act.Action, act.TargetEmail = inv.TeamID, inv.InvitedBy, model.TeamActivityMemberInvited, &inv.Email
		return record(tx, act)
	})
	return next, err
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

// RevokeTeamInvitation revokes a pending invitation of the team, from
// version, as a member; ErrNotFound when the team has no such pending
// invitation.
func (r *repository) RevokeTeamInvitation(ctx context.Context, teamID, callerID, version, id int64, act model.TeamManagementActivity) (int64, error) {
	var next int64
	err := r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		if next, err = changeTeam(tx, teamID, callerID, version, act.CreatedAt); err != nil {
			return err
		}
		inv, err := first[model.TeamInvitation](tx.Where("id = ? AND team_id = ? AND "+pendingInvitation, id, teamID))
		if err != nil {
			return err
		}
		if err := tx.Model(&model.TeamInvitation{}).Where("id = ?", id).Update("revoked_at", act.CreatedAt).Error; err != nil {
			return err
		}
		act.TeamID, act.ActorUserID, act.Action, act.TargetEmail = teamID, callerID, model.TeamActivityInvitationRevoked, &inv.Email
		return record(tx, act)
	})
	return next, err
}

// pendingOf is the team's invitation, read under the team's lock;
// ErrInvitationNotPending when it was accepted or revoked.
func pendingOf(tx *gorm.DB, teamID, id int64) (model.TeamInvitation, error) {
	inv, err := first[model.TeamInvitation](tx.Where("id = ? AND team_id = ?", id, teamID))
	if err != nil {
		return inv, err
	}
	if inv.AcceptedAt != nil || inv.RevokedAt != nil {
		return inv, ErrInvitationNotPending
	}
	return inv, nil
}

// DeclineTeamInvitation is the invitee declining: the invitation is revoked,
// and recorded with them as its actor. ErrNotFound when the team is gone or
// the invitation is not of it; ErrInvitationNotPending when it was accepted
// or revoked meanwhile.
func (r *repository) DeclineTeamInvitation(ctx context.Context, teamID, id int64, act model.TeamManagementActivity) error {
	return r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		if err := bumpTeam(tx, teamID, act.CreatedAt); err != nil {
			return err
		}
		// An expired invitation may still be declined: it only tidies up.
		inv, err := pendingOf(tx, teamID, id)
		if err != nil {
			return err
		}
		if err := tx.Model(&model.TeamInvitation{}).Where("id = ?", id).Update("revoked_at", act.CreatedAt).Error; err != nil {
			return err
		}
		act.TeamID, act.Action, act.TargetEmail = teamID, model.TeamActivityInvitationDeclined, &inv.Email
		return record(tx, act)
	})
}

// AcceptTeamInvitation marks the invitation accepted by the member's account
// and adds them to the member's team, unless already in it. The team is
// locked first: ErrNotFound when it is gone, or the invitation is not of it.
// ErrInvitationNotPending when the invitation was accepted, revoked or
// expired at now, by another accept meanwhile too, or its inviter is no
// longer in the team.
func (r *repository) AcceptTeamInvitation(ctx context.Context, id int64, m model.TeamMember, now time.Time, act model.TeamManagementActivity) error {
	return r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		if err := bumpTeam(tx, m.TeamID, now); err != nil {
			return err
		}
		inv, err := pendingOf(tx, m.TeamID, id)
		if err != nil {
			return err
		}
		if !now.Before(inv.ExpiresAt) {
			return ErrInvitationNotPending
		}
		switch _, err := member(tx, inv.TeamID, inv.InvitedBy); {
		case errors.Is(err, ErrNotFound):
			return ErrInvitationNotPending
		case err != nil:
			return err
		}
		err = tx.Model(&model.TeamInvitation{}).Where("id = ?", id).
			Updates(map[string]any{"accepted_at": now, "accepted_by": m.UserID}).Error
		if err != nil {
			return err
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&m).Error; err != nil {
			return err
		}
		act.TeamID, act.ActorUserID, act.Action, act.TargetEmail = m.TeamID, m.UserID, model.TeamActivityInvitationAccepted, &inv.Email
		return record(tx, act)
	})
}

// sharesWithTeams selects shares with their team's name.
func sharesWithTeams(tx *gorm.DB) *gorm.DB {
	return tx.Model(&model.WorkspaceShare{}).
		Select("workspace_shares.id, workspace_shares.created_at, workspace_shares.updated_at, workspace_shares.team_id, " +
			"workspace_shares.workspace_id, workspace_shares.user_id, teams.name AS team_name").
		Joins("JOIN teams ON teams.id = workspace_shares.team_id")
}

// SetWorkspaceShare shares a workspace with a team from version, and returns
// the share and the new version. The sharer's membership is read under the
// team's lock, so a removal committed meanwhile is seen and no share
// outlives it: ErrNotFound when the team is gone or the sharer is not in it.
// Sharing a workspace already shared changes nothing and records nothing.
func (r *repository) SetWorkspaceShare(ctx context.Context, s model.WorkspaceShare, version int64, act model.TeamManagementActivity) (WorkspaceShareWithTeam, int64, error) {
	var out WorkspaceShareWithTeam
	var next int64
	err := r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		if next, err = changeTeam(tx, s.TeamID, s.UserID, version, s.CreatedAt); err != nil {
			return err
		}
		res := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&s)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected > 0 {
			act.TeamID, act.ActorUserID, act.Action, act.TargetWorkspaceID = s.TeamID, s.UserID, model.TeamActivityWorkspaceShared, &s.WorkspaceID
			if err := record(tx, act); err != nil {
				return err
			}
		}
		out, err = first[WorkspaceShareWithTeam](sharesWithTeams(tx).Where("workspace_shares.team_id = ? AND workspace_shares.workspace_id = ?", s.TeamID, s.WorkspaceID))
		return err
	})
	return out, next, err
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

// DeleteWorkspaceShare unshares the owner's workspace from a team, from
// version, and returns the new version; ErrNotFound when the owner is not
// in the team or it was not shared there.
func (r *repository) DeleteWorkspaceShare(ctx context.Context, workspaceID, teamID, userID, version int64, act model.TeamManagementActivity) (int64, error) {
	var next int64
	err := r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		if next, err = changeTeam(tx, teamID, userID, version, act.CreatedAt); err != nil {
			return err
		}
		res := tx.Where("workspace_id = ? AND team_id = ? AND user_id = ?", workspaceID, teamID, userID).Delete(&model.WorkspaceShare{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		act.TeamID, act.ActorUserID, act.Action, act.TargetWorkspaceID = teamID, userID, model.TeamActivityWorkspaceUnshared, &workspaceID
		return record(tx, act)
	})
	return next, err
}

// ListTeamActivities returns up to limit of the team's activities, newest
// first; with before, those older than that activity of the team, and none
// when the team has no such activity.
func (r *repository) ListTeamActivities(ctx context.Context, teamID int64, limit int, before int64) ([]TeamActivityRow, error) {
	rows := []TeamActivityRow{}
	q := r.conn(ctx).Model(&model.TeamManagementActivity{}).
		Select("team_management_activities.*, "+
			"COALESCE(actors.name, '') AS actor_name, COALESCE(actors.picture, '') AS actor_picture, "+
			"COALESCE(targets.name, '') AS target_name, COALESCE(targets.picture, '') AS target_picture").
		Joins("LEFT JOIN users AS actors ON actors.id = team_management_activities.actor_user_id").
		Joins("LEFT JOIN users AS targets ON targets.id = team_management_activities.target_user_id").
		Where("team_management_activities.team_id = ?", teamID)
	if before != 0 {
		b, err := first[model.TeamManagementActivity](r.conn(ctx).Where("id = ? AND team_id = ?", before, teamID))
		if errors.Is(err, ErrNotFound) {
			// No such activity of the team: nothing is older than it.
			return rows, nil
		}
		if err != nil {
			return nil, err
		}
		q = q.Where("(team_management_activities.created_at < ? OR (team_management_activities.created_at = ? AND team_management_activities.id < ?))",
			b.CreatedAt, b.CreatedAt, b.ID)
	}
	err := q.Order("team_management_activities.created_at DESC, team_management_activities.id DESC").Limit(limit).Scan(&rows).Error
	return rows, err
}

// sharedWorkspace joins a workspace to its shares with teams: its own, or
// its parent's for a fork, and only while the shared workspace is not
// archived. A team's reads add workspace_shares.team_id = ?.
const sharedWorkspace = "JOIN workspace_shares ON " +
	"(workspace_shares.workspace_id = workspaces.id OR workspace_shares.workspace_id = workspaces.fork_of_id) " +
	"JOIN workspaces AS shared ON shared.id = workspace_shares.workspace_id AND shared.archived_at IS NULL"

// CanReadWorkspaceInTeam reports whether the user is in the team and the
// workspace, not archived, is shared with it, itself or as a fork of a
// shared workspace.
func (r *repository) CanReadWorkspaceInTeam(ctx context.Context, userID, teamID, workspaceID int64) (bool, error) {
	var n int64
	err := r.conn(ctx).Model(&model.Workspace{}).
		Joins(sharedWorkspace).
		Where("workspace_shares.team_id = ?", teamID).
		Joins("JOIN team_members ON team_members.team_id = workspace_shares.team_id AND team_members.user_id = ?", userID).
		Where("workspaces.id = ? AND workspaces.archived_at IS NULL", workspaceID).
		Count(&n).Error
	return n > 0, err
}

// ListTeamWorkspaces returns the workspaces a team's members may read, by
// the name they are shared under, then name: those shared with it and their
// forks, none archived.
func (r *repository) ListTeamWorkspaces(ctx context.Context, teamID int64) ([]TeamWorkspaceRow, error) {
	rows := []TeamWorkspaceRow{}
	err := r.conn(ctx).Model(&model.Workspace{}).
		Select("workspaces.id, workspaces.name, workspaces.fork_of_id, workspaces.user_id, shared.name AS shared_name, "+
			"users.name AS owner_name, users.picture AS owner_picture").
		Joins(sharedWorkspace).
		Where("workspace_shares.team_id = ?", teamID).
		Joins("LEFT JOIN users ON users.id = workspaces.user_id").
		Where("workspaces.archived_at IS NULL").
		Order("LOWER(shared.name) ASC, workspaces.name ASC, workspaces.id ASC").
		Scan(&rows).Error
	return rows, err
}
