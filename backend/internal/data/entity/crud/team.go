// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package crud

import "time"

// Team is a team as one of its members sees it, with their role there.
type Team struct {
	ID          int64
	Name        string
	Role        string
	MemberCount int64
	CreatedAt   time.Time
}

// TeamMember is an account in a team.
type TeamMember struct {
	UserID   int64
	Name     string
	Email    string
	Picture  string
	Role     string
	JoinedAt time.Time
}

// TeamInvitation is a pending invitation to a team.
type TeamInvitation struct {
	ID            int64
	TeamID        int64
	TeamName      string
	Email         string
	Role          string
	InvitedBy     int64
	InvitedByName string
	CreatedAt     time.Time
	ExpiresAt     time.Time
}

// CreateTeamInvitationResponse is a new invitation. Link, the only time the
// token is shown, is set when no email was sent.
type CreateTeamInvitationResponse struct {
	Invitation TeamInvitation
	EmailSent  bool
	Link       string
}

// TeamInvitationPreview is what an invitation link's page shows before it is
// accepted. Email is the invited address, masked.
type TeamInvitationPreview struct {
	TeamID        int64
	TeamName      string
	Role          string
	InvitedByName string
	Email         string
	EmailMatches  bool
	ExpiresAt     time.Time
}

// WorkspaceShare is a workspace shared with a team, shown there in Group.
type WorkspaceShare struct {
	TeamID      int64
	TeamName    string
	WorkspaceID int64
	Group       string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// TeamWorkspace is a workspace a team's members may read: one shared with
// it, or a fork of one, which shows in its parent's group.
type TeamWorkspace struct {
	ID           int64
	Name         string
	ForkOfID     int64
	Group        string
	OwnerID      int64
	OwnerName    string
	OwnerPicture string
}

// TeamErrorKind says how a team request was refused.
type TeamErrorKind int

const (
	// TeamInvalid is a bad value, such as an empty name.
	TeamInvalid TeamErrorKind = iota
	// TeamForbidden is a change the caller's role in the team does not allow,
	// or one the server turned off.
	TeamForbidden
	// TeamConflict is a change the current state refuses: the last owner
	// leaving, a pending invitation for the address already.
	TeamConflict
	// TeamGone is an invitation accepted, revoked or expired.
	TeamGone
	// TeamThrottled is a team past its daily invitation cap.
	TeamThrottled
)

// TeamError is a refused team request, with a message for the person.
type TeamError struct {
	Kind    TeamErrorKind
	Message string
}

func (e *TeamError) Error() string { return e.Message }

// GetStatsOfWorkspacesRequest asks for the workspace statistics summed over
// WorkspaceIDs, over the window Range (and From/To for "custom") names.
type GetStatsOfWorkspacesRequest struct {
	WorkspaceIDs []int64
	Range        string
	From         int64
	To           int64
}

// GetStatsOfWorkspacesResponse is the statistics, and each owner's share of
// the completions and messages in them.
type GetStatsOfWorkspacesResponse struct {
	Stats    GetDetailedWorkspaceStatsResponse
	ByMember []MemberStats
}

// MemberStats is one account's completions and messages.
type MemberStats struct {
	UserID         int64
	Name           string
	Picture        string
	TasksCompleted int64
	Messages       int64
}

// SystemGetSkillFileRequest reads a file of UserID's skill.
type SystemGetSkillFileRequest struct {
	UserID int64
	Name   string
	Path   string
}

// CopySkillRequest copies FromUserID's skill Name into UserID's account, on
// in WorkspaceIDs.
type CopySkillRequest struct {
	FromUserID   int64
	Name         string
	UserID       string
	WorkspaceIDs []int64
}

// CopySkillResponse is the copy.
type CopySkillResponse struct {
	Skill Skill
}

// TeamTask is a task as a team sees it: in which of the team's workspaces,
// and whose that is.
type TeamTask struct {
	Task      Task
	Workspace TeamWorkspace
}

// ListTeamTasksRequest lists the tasks of the workspaces a team's member
// reads there, narrowed to a group (by name, ignoring case and spaces) and
// to one owner's workspaces when Group or Member are set.
type ListTeamTasksRequest struct {
	UserID int64
	TeamID int64
	Status []string
	Group  string
	Member int64
	Limit  int
	Offset int
}

// TeamTaskDetail is a teammate's task as its owner's task page reads it, and
// its plan when it has one.
type TeamTaskDetail struct {
	Detail    GetTaskResponse
	Plan      *TaskPlan
	Workspace TeamWorkspace
}

// GetTeamStatsRequest asks for the statistics of a team's workspaces, or of
// one group of them.
type GetTeamStatsRequest struct {
	UserID int64
	TeamID int64
	Group  string
	Range  string
	From   int64
	To     int64
}

// CloneTeamTaskRequest copies a teammate's task into WorkspaceID, one of the
// caller's, starting it as Status: notstarted or ongoing.
type CloneTeamTaskRequest struct {
	UserID      int64
	TeamID      int64
	TaskID      int64
	WorkspaceID int64
	Status      string
}

// TeamMemory is a team's memory file. UpdatedAt is zero, and UpdatedBy 0,
// before anyone saved it. Conflict says a save replaced one saved after the
// version its writer had read.
type TeamMemory struct {
	Content          string
	UpdatedAt        time.Time
	UpdatedBy        int64
	UpdatedByName    string
	UpdatedByPicture string
	Conflict         bool
}

// SaveTeamMemoryRequest saves a team's memory. BaseUpdatedAt is the version
// the writer read; zero when they read none.
type SaveTeamMemoryRequest struct {
	UserID        int64
	TeamID        int64
	Content       string
	BaseUpdatedAt time.Time
}

// TeamMemoryIndexEntry is one shared workspace's memories, by name, and when
// the latest of them was saved.
type TeamMemoryIndexEntry struct {
	Workspace TeamWorkspace
	Names     []string
	UpdatedAt time.Time
}

// TeamOwner is an account with a shared workspace.
type TeamOwner struct {
	UserID  int64
	Name    string
	Picture string
}

// TeamSkill is a skill on in a team's workspaces, and who has it on. One read
// on its own carries its SKILL.md, from the owner who changed it last.
type TeamSkill struct {
	Name        string
	Description string
	UpdatedAt   time.Time
	Owners      []TeamOwner
	Content     string
}
