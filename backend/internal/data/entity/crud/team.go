// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package crud

import "time"

// Team is a team as one of its members sees it, at Version.
type Team struct {
	ID          int64
	Name        string
	Version     int64
	MemberCount int64
	CreatedAt   time.Time
}

// TeamMember is an account in a team.
type TeamMember struct {
	UserID   int64
	Name     string
	Email    string
	Picture  string
	JoinedAt time.Time
}

// TeamInvitation is a pending invitation to a team.
type TeamInvitation struct {
	ID            int64
	TeamID        int64
	TeamName      string
	Email         string
	InvitedBy     int64
	InvitedByName string
	CreatedAt     time.Time
	ExpiresAt     time.Time
}

// CreateTeamInvitationResponse is a new invitation and the team's version
// after it. Link, the only time the token is shown, is always set, so the
// inviter can share it; it is the only way in when no email was sent.
type CreateTeamInvitationResponse struct {
	Invitation TeamInvitation
	EmailSent  bool
	Link       string
	Version    int64
}

// TeamInvitationPreview is what an invitation link's page shows before it is
// accepted. Email is the invited address, masked.
type TeamInvitationPreview struct {
	TeamID        int64
	TeamName      string
	InvitedByName string
	Email         string
	EmailMatches  bool
	ExpiresAt     time.Time
}

// SharedParts is what a share lets the team read of the workspace.
type SharedParts struct {
	Tasks  bool
	Memory bool
	// AutoOptInSkills opts the workspace into every skill of its team
	// workspace it has made no choice about, later ones included.
	AutoOptInSkills bool
}

// WorkspaceShare is a workspace shared with a team. Version is the team's
// after the share was made, when it was just made.
type WorkspaceShare struct {
	TeamID      int64
	TeamName    string
	WorkspaceID int64
	SharedParts
	CreatedAt time.Time
	UpdatedAt time.Time
	Version   int64
}

// PanoDocShare is a Pano doc shared with a team. Version is the team's
// after the share was made, when it was just made.
type PanoDocShare struct {
	TeamID    int64
	TeamName  string
	DocID     int64
	CreatedAt time.Time
	Version   int64
}

// TeamPanoDoc is a Pano doc shared with a team, with its owner.
type TeamPanoDoc struct {
	PanoDoc
	OwnerID      int64
	OwnerName    string
	OwnerPicture string
}

// TeamWorkspace is a workspace a team's members may read: one shared with
// it, or a fork of one. TeamWorkspaceID is the team workspace its share (its
// parent's, for a fork) is in, and SharedName that team workspace's name.
type TeamWorkspace struct {
	ID              int64
	Name            string
	ForkOfID        int64
	TeamWorkspaceID int64
	SharedName      string
	OwnerID         int64
	OwnerName       string
	OwnerPicture    string
	// Tasks is whether its share lets the team read them.
	Tasks bool
}

// TeamMembers is a team's members at Version.
type TeamMembers struct {
	Members []TeamMember
	Version int64
}

// TeamInvitations is a team's open invitations at Version.
type TeamInvitations struct {
	Invitations []TeamInvitation
	Version     int64
}

// TeamChange is a change to a team from the version its maker loaded.
type TeamChange struct {
	UserID  int64
	TeamID  int64
	Version int64
}

// TeamActivity is one change to a team: who made it, what, and to whom or
// which workspace. Target fields are zero when the change has none.
type TeamActivity struct {
	ID                int64
	Action            string
	Actor             TeamOwner
	Target            TeamOwner
	TargetEmail       string
	TargetWorkspaceID int64
	Details           map[string]string
	CreatedAt         time.Time
}

// TeamActivities is a page of a team's activities, newest first, at the
// team's Version.
type TeamActivities struct {
	Activities []TeamActivity
	Version    int64
}

// ListTeamActivitiesRequest pages a team's activities: Limit of them, older
// than activity Before when it is set.
type ListTeamActivitiesRequest struct {
	UserID int64
	TeamID int64
	Limit  int
	Before int64
}

// TeamErrorKind says how a team request was refused.
type TeamErrorKind int

const (
	// TeamInvalid is a bad value, such as an empty name.
	TeamInvalid TeamErrorKind = iota
	// TeamForbidden is a change the server turned off, or an invitation for
	// another address.
	TeamForbidden
	// TeamConflict is a change the current state refuses: a pending
	// invitation for the address already, an invitee already in the team.
	TeamConflict
	// TeamGone is an invitation accepted, revoked or expired.
	TeamGone
	// TeamThrottled is a team, or an inviter, past its daily invitation cap.
	TeamThrottled
)

// TeamError is a refused team request, with a message for the person.
type TeamError struct {
	Kind    TeamErrorKind
	Message string
}

func (e *TeamError) Error() string { return e.Message }

// TeamVersionError is a team change made from a version another change has
// replaced; Version is the team's now.
type TeamVersionError struct {
	Version int64
}

func (e *TeamVersionError) Error() string {
	return "someone changed this team: reload it and try again"
}

// GetStatsOfWorkspacesRequest asks for the workspace statistics summed over
// WorkspaceIDs, over the window Range (and From/To for "custom") names.
type GetStatsOfWorkspacesRequest struct {
	WorkspaceIDs []int64
	Range        string
	From         int64
	To           int64
}

// TeamTask is a task as a team sees it: in which of the team's workspaces,
// and whose that is.
type TeamTask struct {
	Task      Task
	Workspace TeamWorkspace
}

// ListTeamTasksRequest lists the tasks of the workspaces a team's member
// reads there, narrowed to one team workspace (by its shared name, ignoring
// case and surrounding spaces) and to one owner's workspaces when Workspace
// or Member are set.
type ListTeamTasksRequest struct {
	UserID    int64
	TeamID    int64
	Status    []string
	Workspace string
	Member    int64
	Limit     int
	Offset    int
}

// TeamTaskDetail is a teammate's task as its owner's task page reads it, and
// its plan when it has one.
type TeamTaskDetail struct {
	Detail    GetTaskResponse
	Plan      *TaskPlan
	Workspace TeamWorkspace
}

// GetTeamStatsRequest asks for the statistics of a team's workspaces, or of
// one team workspace (by its shared name, as ListTeamTasksRequest) when
// Workspace is set.
type GetTeamStatsRequest struct {
	UserID    int64
	TeamID    int64
	Workspace string
	Range     string
	From      int64
	To        int64
}

// CloneTeamTaskRequest copies a teammate's task into WorkspaceID, one of the
// caller's, not started, for Assignee: human or agent.
type CloneTeamTaskRequest struct {
	UserID      int64
	TeamID      int64
	TaskID      int64
	WorkspaceID int64
	Assignee    string
}

// TeamOwner is an account with a shared workspace.
type TeamOwner struct {
	UserID  int64
	Name    string
	Picture string
}

// TeammateMemory is a teammate's workspace in the team workspace of one of
// the account's own, and whether that workspace's agent reads its memory.
// UpdatedAt is when a memory there was last written; nil when none was.
type TeammateMemory struct {
	UserID      int64
	Name        string
	Picture     string
	WorkspaceID int64
	UpdatedAt   *time.Time
	Included    bool
}
