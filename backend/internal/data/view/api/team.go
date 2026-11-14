// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import "time"

// Team is a team the signed-in account is in. version counts its changes:
// every change sends the version it was made from.
type Team struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Version     int64     `json:"version"`
	MemberCount int64     `json:"memberCount"`
	CreatedAt   time.Time `json:"createdAt"`
}

// ListTeamsResponse is the account's teams.
type ListTeamsResponse struct {
	Teams []Team `json:"teams"`
}

// TeamNameRequest creates or renames a team; a rename sends the version it
// was made from.
type TeamNameRequest struct {
	Name    string `json:"name"`
	Version int64  `json:"version"`
}

// TeamVersionRequest is a team change that sends only the version it was
// made from.
type TeamVersionRequest struct {
	Version int64 `json:"version"`
}

// TeamVersionResponse is the team's version after a change.
type TeamVersionResponse struct {
	Version int64 `json:"version"`
}

// TeamConflictResponse refuses a change made from a version another change
// replaced; version is the team's now.
type TeamConflictResponse struct {
	Error struct {
		Message string `json:"message"`
		Code    int    `json:"code"`
	} `json:"error"`
	Conflict bool  `json:"conflict"`
	Version  int64 `json:"version"`
}

// TeamMember is an account in a team.
type TeamMember struct {
	UserID   string    `json:"userId"`
	Name     string    `json:"name"`
	Email    string    `json:"email"`
	Picture  string    `json:"picture"`
	JoinedAt time.Time `json:"joinedAt"`
}

// ListTeamMembersResponse is a team's members, at the team's version.
type ListTeamMembersResponse struct {
	Members []TeamMember `json:"members"`
	Version int64        `json:"version"`
}

// TeamInvitation is a pending invitation to a team.
type TeamInvitation struct {
	ID            string    `json:"id"`
	TeamID        string    `json:"teamId"`
	TeamName      string    `json:"teamName"`
	Email         string    `json:"email"`
	InvitedBy     string    `json:"invitedBy"`
	InvitedByName string    `json:"invitedByName"`
	CreatedAt     time.Time `json:"createdAt"`
	ExpiresAt     time.Time `json:"expiresAt"`
}

// ListTeamInvitationsResponse is a list of invitations.
type ListTeamInvitationsResponse struct {
	Invitations []TeamInvitation `json:"invitations"`
}

// TeamInvitationsResponse is a team's open invitations, at its version.
type TeamInvitationsResponse struct {
	Invitations []TeamInvitation `json:"invitations"`
	Version     int64            `json:"version"`
}

// CreateTeamInvitationRequest invites an address, from the team's version.
type CreateTeamInvitationRequest struct {
	Email   string `json:"email"`
	Version int64  `json:"version"`
}

// CreateTeamInvitationResponse is a new invitation and the team's version
// after it. Link is set, the only time the token is shown, when no email was
// sent.
type CreateTeamInvitationResponse struct {
	Invitation TeamInvitation `json:"invitation"`
	EmailSent  bool           `json:"emailSent"`
	Link       string         `json:"link,omitempty"`
	Version    int64          `json:"version"`
}

// TeamInvitationTokenRequest names an invitation by its link's token.
type TeamInvitationTokenRequest struct {
	Token string `json:"token"`
}

// TeamInvitationPreview is what an invitation link's page shows; email is
// the invited address, masked.
type TeamInvitationPreview struct {
	TeamID        string    `json:"teamId"`
	TeamName      string    `json:"teamName"`
	InvitedByName string    `json:"invitedByName"`
	Email         string    `json:"email"`
	EmailMatches  bool      `json:"emailMatches"`
	ExpiresAt     time.Time `json:"expiresAt"`
}

// WorkspaceShare is a workspace shared with a team. version, the team's
// after the share, is set when the share was just made.
type WorkspaceShare struct {
	TeamID      string    `json:"teamId"`
	TeamName    string    `json:"teamName"`
	WorkspaceID string    `json:"workspaceId"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
	Version     int64     `json:"version,omitempty"`
}

// ListWorkspaceSharesResponse is a workspace's shares.
type ListWorkspaceSharesResponse struct {
	Shares []WorkspaceShare `json:"shares"`
}

// TeamActivity is one change to a team. targetUser, targetEmail and
// targetWorkspaceId are set when the change has one; details is a small
// object, such as a rename's from and to.
type TeamActivity struct {
	ID                string            `json:"id"`
	Action            string            `json:"action"`
	Actor             TeamOwner         `json:"actor"`
	TargetUser        *TeamOwner        `json:"targetUser,omitempty"`
	TargetEmail       string            `json:"targetEmail,omitempty"`
	TargetWorkspaceID string            `json:"targetWorkspaceId,omitempty"`
	Details           map[string]string `json:"details"`
	CreatedAt         time.Time         `json:"createdAt"`
}

// ListTeamActivitiesResponse is a page of a team's activities, newest
// first, at the team's version.
type ListTeamActivitiesResponse struct {
	Activities []TeamActivity `json:"activities"`
	Version    int64          `json:"version"`
}
