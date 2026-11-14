// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package model

import "time"

// A member's role in a team. Stored as text.
const (
	TeamRoleOwner  = "owner"
	TeamRoleAdmin  = "admin"
	TeamRoleMember = "member"
)

// Team is a group of accounts that see the workspaces its members share with
// it. It has at least one owner while it exists.
type Team struct {
	ID        int64 `gorm:"primaryKey;autoIncrement:false"`
	CreatedAt time.Time
	UpdatedAt time.Time
	Name      string `gorm:"type:varchar(128);not null"`
}

// TeamMember is an account in a team, with its role there.
type TeamMember struct {
	ID        int64 `gorm:"primaryKey;autoIncrement:false"`
	CreatedAt time.Time
	UpdatedAt time.Time
	TeamID    int64  `gorm:"not null;uniqueIndex:uk_team_members_team_id_user_id,priority:1"`
	UserID    int64  `gorm:"not null;uniqueIndex:uk_team_members_team_id_user_id,priority:2;index:idx_team_members_user_id"`
	Role      string `gorm:"type:varchar(16);not null"`
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
	Role       string `gorm:"type:varchar(16);not null"`
	InvitedBy  int64  `gorm:"not null;index:idx_team_invitations_invited_by_created_at,priority:1"`
	TokenHash  string `gorm:"type:char(64);not null;uniqueIndex:uk_team_invitations_token_hash"`
	ExpiresAt  time.Time
	AcceptedAt *time.Time
	AcceptedBy int64 `gorm:"not null;default:0"`
	RevokedAt  *time.Time
	CreatedAt  time.Time `gorm:"index:idx_team_invitations_team_id_created_at,priority:2;index:idx_team_invitations_invited_by_created_at,priority:2"`
}

// WorkspaceShare lets a team's members read a workspace, and its forks,
// shown in the team under GroupName. UserID is the workspace's owner, who
// alone shares and unshares it.
type WorkspaceShare struct {
	ID          int64 `gorm:"primaryKey;autoIncrement:false"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
	TeamID      int64  `gorm:"not null;uniqueIndex:uk_workspace_shares_team_id_workspace_id,priority:1;index:idx_workspace_shares_team_id_user_id,priority:1"`
	WorkspaceID int64  `gorm:"not null;uniqueIndex:uk_workspace_shares_team_id_workspace_id,priority:2;index:idx_workspace_shares_workspace_id"`
	UserID      int64  `gorm:"not null;index:idx_workspace_shares_team_id_user_id,priority:2"`
	GroupName   string `gorm:"type:varchar(128);not null"`
}

// TeamMemory is a team's own memory file, which every member edits.
type TeamMemory struct {
	ID        int64 `gorm:"primaryKey;autoIncrement:false"`
	CreatedAt time.Time
	UpdatedAt time.Time
	TeamID    int64  `gorm:"not null;uniqueIndex:uk_team_memories_team_id_name,priority:1"`
	Name      string `gorm:"type:varchar(32);not null;uniqueIndex:uk_team_memories_team_id_name,priority:2"`
	Content   string `gorm:"type:text;not null"`
	UpdatedBy int64  `gorm:"not null"`
}
