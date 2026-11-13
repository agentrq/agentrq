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
