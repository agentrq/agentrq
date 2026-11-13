// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	view "github.com/agentrq/agentrq/backend/internal/data/view/api"
	"github.com/mustafaturan/monoflake"
)

// FromHTTPRequestToTeamRequestView reads a team request's JSON body into v;
// false when it is not JSON.
func FromHTTPRequestToTeamRequestView(body []byte, v any) bool {
	return json.Unmarshal(body, v) == nil
}

func id62(id int64) string { return monoflake.ID(id).String() }

func fromTeamEntity(e entity.Team) view.Team {
	return view.Team{ID: id62(e.ID), Name: e.Name, Role: e.Role, MemberCount: e.MemberCount, CreatedAt: e.CreatedAt}
}

// FromTeamEntityToHTTPResponse renders a team.
func FromTeamEntityToHTTPResponse(e entity.Team) []byte {
	b, _ := json.Marshal(fromTeamEntity(e))
	return b
}

// FromTeamEntitiesToHTTPResponse renders the account's teams; none is an
// empty list.
func FromTeamEntitiesToHTTPResponse(es []entity.Team) []byte {
	out := view.ListTeamsResponse{Teams: make([]view.Team, len(es))}
	for i, e := range es {
		out.Teams[i] = fromTeamEntity(e)
	}
	b, _ := json.Marshal(out)
	return b
}

// FromTeamMemberEntitiesToHTTPResponse renders a team's members.
func FromTeamMemberEntitiesToHTTPResponse(es []entity.TeamMember) []byte {
	out := view.ListTeamMembersResponse{Members: make([]view.TeamMember, len(es))}
	for i, e := range es {
		out.Members[i] = view.TeamMember{UserID: id62(e.UserID), Name: e.Name, Email: e.Email, Picture: e.Picture, Role: e.Role, JoinedAt: e.JoinedAt}
	}
	b, _ := json.Marshal(out)
	return b
}

func fromTeamInvitationEntity(e entity.TeamInvitation) view.TeamInvitation {
	return view.TeamInvitation{
		ID: id62(e.ID), TeamID: id62(e.TeamID), TeamName: e.TeamName, Email: e.Email, Role: e.Role,
		InvitedBy: id62(e.InvitedBy), InvitedByName: e.InvitedByName, CreatedAt: e.CreatedAt, ExpiresAt: e.ExpiresAt,
	}
}

// FromTeamInvitationEntitiesToHTTPResponse renders a list of invitations.
func FromTeamInvitationEntitiesToHTTPResponse(es []entity.TeamInvitation) []byte {
	out := view.ListTeamInvitationsResponse{Invitations: make([]view.TeamInvitation, len(es))}
	for i, e := range es {
		out.Invitations[i] = fromTeamInvitationEntity(e)
	}
	b, _ := json.Marshal(out)
	return b
}

// FromCreateTeamInvitationEntityToHTTPResponse renders a new invitation.
func FromCreateTeamInvitationEntityToHTTPResponse(e entity.CreateTeamInvitationResponse) []byte {
	b, _ := json.Marshal(view.CreateTeamInvitationResponse{
		Invitation: fromTeamInvitationEntity(e.Invitation), EmailSent: e.EmailSent, Link: e.Link,
	})
	return b
}

// FromTeamInvitationPreviewEntityToHTTPResponse renders an invitation link's
// preview.
func FromTeamInvitationPreviewEntityToHTTPResponse(e entity.TeamInvitationPreview) []byte {
	b, _ := json.Marshal(view.TeamInvitationPreview{
		TeamID: id62(e.TeamID), TeamName: e.TeamName, Role: e.Role, InvitedByName: e.InvitedByName,
		Email: e.Email, EmailMatches: e.EmailMatches, ExpiresAt: e.ExpiresAt,
	})
	return b
}

func fromWorkspaceShareEntity(e entity.WorkspaceShare) view.WorkspaceShare {
	return view.WorkspaceShare{
		TeamID: id62(e.TeamID), TeamName: e.TeamName, WorkspaceID: id62(e.WorkspaceID), Group: e.Group,
		CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt,
	}
}

// FromWorkspaceShareEntityToHTTPResponse renders a share.
func FromWorkspaceShareEntityToHTTPResponse(e entity.WorkspaceShare) []byte {
	b, _ := json.Marshal(fromWorkspaceShareEntity(e))
	return b
}

// FromWorkspaceShareEntitiesToHTTPResponse renders a workspace's shares.
func FromWorkspaceShareEntitiesToHTTPResponse(es []entity.WorkspaceShare) []byte {
	out := view.ListWorkspaceSharesResponse{Shares: make([]view.WorkspaceShare, len(es))}
	for i, e := range es {
		out.Shares[i] = fromWorkspaceShareEntity(e)
	}
	b, _ := json.Marshal(out)
	return b
}
