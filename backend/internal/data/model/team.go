// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package model

import "time"

// Team is a group of accounts that see the workspaces its members share with
// it. Every member is equal: who may do what is decided outside AgentRQ.
// Version counts its changes: a change names the version it was made from
// and is refused if another landed first.
type Team struct {
	ID        int64 `gorm:"primaryKey;autoIncrement:false"`
	CreatedAt time.Time
	UpdatedAt time.Time
	Name      string `gorm:"type:varchar(128);not null"`
	Version   int64  `gorm:"not null;default:1"`
}

// TeamMember is an account in a team.
type TeamMember struct {
	ID        int64 `gorm:"primaryKey;autoIncrement:false"`
	CreatedAt time.Time
	UpdatedAt time.Time
	TeamID    int64 `gorm:"not null;uniqueIndex:uk_team_members_team_id_user_id,priority:1"`
	UserID    int64 `gorm:"not null;uniqueIndex:uk_team_members_team_id_user_id,priority:2;index:idx_team_members_user_id"`
}

// TeamInvitation asks someone, by email, to join a team. Only the SHA-256 of
// its token is kept. It is pending until accepted or revoked (a decline
// revokes it), and can be accepted until ExpiresAt. A team holds one pending
// invitation per address; every one, revoked or not, counts toward the
// team's daily cap and its inviter's.
type TeamInvitation struct {
	ID     int64 `gorm:"primaryKey;autoIncrement:false"`
	TeamID int64 `gorm:"not null;index:idx_team_invitations_team_id_created_at,priority:1;uniqueIndex:uk_team_invitations_pending_email,priority:1,where:accepted_at IS NULL AND revoked_at IS NULL"`
	// Email is stored lowercased, so matching an account ignores case.
	Email      string `gorm:"type:varchar(255);not null;index:idx_team_invitations_email;uniqueIndex:uk_team_invitations_pending_email,priority:2"`
	InvitedBy  int64  `gorm:"not null;index:idx_team_invitations_invited_by_created_at,priority:1"`
	TokenHash  string `gorm:"type:char(64);not null;uniqueIndex:uk_team_invitations_token_hash"`
	ExpiresAt  time.Time
	AcceptedAt *time.Time
	AcceptedBy int64 `gorm:"not null;default:0"`
	RevokedAt  *time.Time
	CreatedAt  time.Time `gorm:"index:idx_team_invitations_team_id_created_at,priority:2;index:idx_team_invitations_invited_by_created_at,priority:2"`
}

// WorkspaceShare lets a team's members read a workspace, and its forks.
// UserID is the workspace's owner, who alone shares and unshares it. In the
// team it shows by its own name, as one team workspace with every other
// shared workspace of the same name.
type WorkspaceShare struct {
	ID          int64 `gorm:"primaryKey;autoIncrement:false"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
	TeamID      int64 `gorm:"not null;uniqueIndex:uk_workspace_shares_team_id_workspace_id,priority:1;index:idx_workspace_shares_team_id_user_id,priority:1"`
	WorkspaceID int64 `gorm:"not null;uniqueIndex:uk_workspace_shares_team_id_workspace_id,priority:2;index:idx_workspace_shares_workspace_id"`
	UserID      int64 `gorm:"not null;index:idx_workspace_shares_team_id_user_id,priority:2"`
}

// What a team management activity records.
const (
	TeamActivityTeamCreated        = "team.created"
	TeamActivityTeamRenamed        = "team.renamed"
	TeamActivityMemberInvited      = "member.invited"
	TeamActivityInvitationRevoked  = "invitation.revoked"
	TeamActivityInvitationAccepted = "invitation.accepted"
	TeamActivityInvitationDeclined = "invitation.declined"
	TeamActivityMemberRemoved      = "member.removed"
	TeamActivityMemberLeft         = "member.left"
	TeamActivityWorkspaceShared    = "workspace.shared"
	TeamActivityWorkspaceUnshared  = "workspace.unshared"
	TeamActivityTeamDeleted        = "team.deleted"
)

// TeamManagementActivity is one change to a team, written in the same
// transaction as the change. Rows outlive the team: they are the record of
// what happened to it. Details is a small JSON object, such as a rename's
// old and new name.
type TeamManagementActivity struct {
	ID                int64     `gorm:"primaryKey;autoIncrement:false"`
	TeamID            int64     `gorm:"not null;index:idx_team_management_activities_team_id_created_at,priority:1"`
	ActorUserID       int64     `gorm:"not null"`
	Action            string    `gorm:"type:varchar(32);not null"`
	TargetUserID      *int64    ``
	TargetEmail       *string   `gorm:"type:varchar(255)"`
	TargetWorkspaceID *int64    ``
	Details           string    `gorm:"type:text;not null"`
	CreatedAt         time.Time `gorm:"index:idx_team_management_activities_team_id_created_at,priority:2"`
}
