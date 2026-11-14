// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import "time"

// Team is a team the signed-in account is in, with its role there.
type Team struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Role        string    `json:"role"`
	MemberCount int64     `json:"memberCount"`
	CreatedAt   time.Time `json:"createdAt"`
}

// ListTeamsResponse is the account's teams.
type ListTeamsResponse struct {
	Teams []Team `json:"teams"`
}

// TeamNameRequest creates or renames a team.
type TeamNameRequest struct {
	Name string `json:"name"`
}

// TeamMember is an account in a team.
type TeamMember struct {
	UserID   string    `json:"userId"`
	Name     string    `json:"name"`
	Email    string    `json:"email"`
	Picture  string    `json:"picture"`
	Role     string    `json:"role"`
	JoinedAt time.Time `json:"joinedAt"`
}

// ListTeamMembersResponse is a team's members.
type ListTeamMembersResponse struct {
	Members []TeamMember `json:"members"`
}

// TeamMemberRoleRequest changes a member's role.
type TeamMemberRoleRequest struct {
	Role string `json:"role"`
}

// TeamInvitation is a pending invitation to a team.
type TeamInvitation struct {
	ID            string    `json:"id"`
	TeamID        string    `json:"teamId"`
	TeamName      string    `json:"teamName"`
	Email         string    `json:"email"`
	Role          string    `json:"role"`
	InvitedBy     string    `json:"invitedBy"`
	InvitedByName string    `json:"invitedByName"`
	CreatedAt     time.Time `json:"createdAt"`
	ExpiresAt     time.Time `json:"expiresAt"`
}

// ListTeamInvitationsResponse is a list of invitations.
type ListTeamInvitationsResponse struct {
	Invitations []TeamInvitation `json:"invitations"`
}

// CreateTeamInvitationRequest invites an address; role is member when empty.
type CreateTeamInvitationRequest struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

// CreateTeamInvitationResponse is a new invitation. Link is set, the only
// time the token is shown, when no email was sent.
type CreateTeamInvitationResponse struct {
	Invitation TeamInvitation `json:"invitation"`
	EmailSent  bool           `json:"emailSent"`
	Link       string         `json:"link,omitempty"`
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
	Role          string    `json:"role"`
	InvitedByName string    `json:"invitedByName"`
	Email         string    `json:"email"`
	EmailMatches  bool      `json:"emailMatches"`
	ExpiresAt     time.Time `json:"expiresAt"`
}

// WorkspaceShare is a workspace shared with a team, shown there in group.
type WorkspaceShare struct {
	TeamID      string    `json:"teamId"`
	TeamName    string    `json:"teamName"`
	WorkspaceID string    `json:"workspaceId"`
	Group       string    `json:"group"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// ListWorkspaceSharesResponse is a workspace's shares.
type ListWorkspaceSharesResponse struct {
	Shares []WorkspaceShare `json:"shares"`
}

// WorkspaceShareRequest shares a workspace; group is its name when empty.
type WorkspaceShareRequest struct {
	Group string `json:"group"`
}
