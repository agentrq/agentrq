// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

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
	Email      string    `gorm:"type:varchar(255);not null;index:idx_team_invitations_email;uniqueIndex:uk_team_invitations_pending_email,priority:2"`
	InvitedBy  int64     `gorm:"not null;index:idx_team_invitations_invited_by_created_at,priority:1"`
	TokenHash  string    `gorm:"type:char(64);not null;uniqueIndex:uk_team_invitations_token_hash"`
	ExpiresAt  time.Time `gorm:"index:idx_team_invitations_expiry_counted_at_expires_at,priority:2"`
	AcceptedAt *time.Time
	AcceptedBy int64 `gorm:"not null;default:0"`
	RevokedAt  *time.Time
	CreatedAt  time.Time `gorm:"index:idx_team_invitations_team_id_created_at,priority:2;index:idx_team_invitations_invited_by_created_at,priority:2"`
	// ExpiryCountedAt is when the expiry sweep counted it, so it is counted
	// once. Invitations already expired when the column arrived were marked
	// uncounted, so the first sweep is not a burst of old expiries.
	ExpiryCountedAt *time.Time `gorm:"index:idx_team_invitations_expiry_counted_at_expires_at,priority:1"`
}

// WorkspaceShare lets a team's members read a workspace, and its forks.
// UserID is the workspace's owner, who alone shares and unshares it. In the
// team it shows in a team workspace, the one of its name when it was
// shared. The Hide flags hold its tasks, skills
// or memory back from the team; stored as what is held back, so a share made
// before they existed reads as sharing all three.
type WorkspaceShare struct {
	ID          int64 `gorm:"primaryKey;autoIncrement:false"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
	TeamID      int64 `gorm:"not null;uniqueIndex:uk_workspace_shares_team_id_workspace_id,priority:1;index:idx_workspace_shares_team_id_user_id,priority:1"`
	WorkspaceID int64 `gorm:"not null;uniqueIndex:uk_workspace_shares_team_id_workspace_id,priority:2;index:idx_workspace_shares_workspace_id"`
	UserID      int64 `gorm:"not null;index:idx_workspace_shares_team_id_user_id,priority:2"`
	HideTasks   bool  `gorm:"not null;default:false"`
	HideSkills  bool  `gorm:"not null;default:false"`
	HideMemory  bool  `gorm:"not null;default:false"`
	// TeamWorkspaceID is the team workspace it shows in, picked by its name
	// when it was shared; a rename later does not move it.
	TeamWorkspaceID int64 `gorm:"not null;default:0;index:idx_workspace_shares_team_workspace_id"`
	// ManualTeamSkills turns auto opt-in off: a team skill with no choice
	// for the workspace is off rather than on. Stored negated, as the Hide
	// flags are, so a share starts opted in.
	ManualTeamSkills bool `gorm:"not null;default:false"`
}

// TeamSkillChoice is whether a workspace (a fork's parent, for a fork) uses
// one team skill, set by its owner or by its agent's delete. No row follows
// the share's auto opt-in, so nothing is written per member when a skill is
// added; a row outlives any change to the skill.
type TeamSkillChoice struct {
	ID          int64 `gorm:"primaryKey;autoIncrement:false"`
	CreatedAt   time.Time
	SkillID     int64 `gorm:"not null;uniqueIndex:uk_team_skill_choices_skill_id_workspace_id,priority:1"`
	WorkspaceID int64 `gorm:"not null;index:idx_team_skill_choices_workspace_id;uniqueIndex:uk_team_skill_choices_skill_id_workspace_id,priority:2"`
	UserID      int64 `gorm:"not null;index:idx_team_skill_choices_user_id"`
	Off         bool  `gorm:"not null;default:false"`
}

// TeamWorkspace is the shares of one team under one name, which owns what
// they hold in common so it outlives any member. NameKey is
// teamview.WorkspaceKey(Name), computed in Go: SQLite's LOWER folds ASCII
// only. It stays after its last share goes, and goes with the team.
type TeamWorkspace struct {
	ID        int64 `gorm:"primaryKey;autoIncrement:false"`
	CreatedAt time.Time
	UpdatedAt time.Time
	TeamID    int64  `gorm:"not null;index:idx_team_workspaces_team_id;uniqueIndex:uk_team_workspaces_team_id_name_key,priority:1"`
	Name      string `gorm:"type:varchar(128);not null"`
	NameKey   string `gorm:"type:varchar(128);not null;uniqueIndex:uk_team_workspaces_team_id_name_key,priority:2"`
	Version   int64  `gorm:"not null;default:1"`
}

// PanoDocShare lets a team's members read one of UserID's Pano docs, and
// its files. UserID, the doc's owner, alone shares and unshares it, and the
// share goes when they leave the team or the doc is deleted.
type PanoDocShare struct {
	ID        int64 `gorm:"primaryKey;autoIncrement:false"`
	CreatedAt time.Time
	TeamID    int64 `gorm:"not null;uniqueIndex:uk_pano_doc_shares_team_id_doc_id,priority:1;index:idx_pano_doc_shares_team_id_user_id,priority:1"`
	DocID     int64 `gorm:"not null;uniqueIndex:uk_pano_doc_shares_team_id_doc_id,priority:2;index:idx_pano_doc_shares_doc_id"`
	UserID    int64 `gorm:"not null;index:idx_pano_doc_shares_team_id_user_id,priority:2"`
}

// MemoryExclusion leaves a teammate's memory out of the memory index the
// agent of one of UserID's own shared workspaces reads. No row means
// included, so a new teammate starts included. It changes nothing for the
// teammate, and a row for someone no longer a teammate is ignored.
type MemoryExclusion struct {
	WorkspaceID    int64 `gorm:"primaryKey;autoIncrement:false"`
	UserID         int64 `gorm:"primaryKey;autoIncrement:false"`
	ExcludedUserID int64 `gorm:"primaryKey;autoIncrement:false"`
	CreatedAt      time.Time
}

// What a team management activity records.
const (
	TeamActivityTeamCreated          = "team.created"
	TeamActivityTeamRenamed          = "team.renamed"
	TeamActivityMemberInvited        = "member.invited"
	TeamActivityInvitationRevoked    = "invitation.revoked"
	TeamActivityInvitationAccepted   = "invitation.accepted"
	TeamActivityInvitationDeclined   = "invitation.declined"
	TeamActivityMemberRemoved        = "member.removed"
	TeamActivityMemberLeft           = "member.left"
	TeamActivityWorkspaceShared      = "workspace.shared"
	TeamActivityWorkspaceUnshared    = "workspace.unshared"
	TeamActivityShareChanged         = "workspace.shareChanged"
	TeamActivityTeamWorkspaceCreated = "teamWorkspace.created"
	TeamActivitySkillCreated         = "teamSkill.created"
	TeamActivitySkillDeleted         = "teamSkill.deleted"
	TeamActivitySkillGiven           = "teamSkill.given"
	TeamActivitySkillOptedIn         = "teamSkill.optedIn"
	TeamActivitySkillOptedOut        = "teamSkill.optedOut"
	TeamActivityPanoShared           = "pano.shared"
	TeamActivityPanoUnshared         = "pano.unshared"
	TeamActivityTeamDeleted          = "team.deleted"
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
