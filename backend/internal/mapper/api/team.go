// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"net/http"

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
	return view.Team{ID: id62(e.ID), Name: e.Name, Version: e.Version, MemberCount: e.MemberCount, CreatedAt: e.CreatedAt}
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

// FromTeamMembersEntityToHTTPResponse renders a team's members.
func FromTeamMembersEntityToHTTPResponse(e entity.TeamMembers) []byte {
	out := view.ListTeamMembersResponse{Members: make([]view.TeamMember, len(e.Members)), Version: e.Version}
	for i, m := range e.Members {
		out.Members[i] = view.TeamMember{UserID: id62(m.UserID), Name: m.Name, Email: m.Email, Picture: m.Picture, JoinedAt: m.JoinedAt}
	}
	b, _ := json.Marshal(out)
	return b
}

func fromTeamInvitationEntity(e entity.TeamInvitation) view.TeamInvitation {
	return view.TeamInvitation{
		ID: id62(e.ID), TeamID: id62(e.TeamID), TeamName: e.TeamName, Email: e.Email,
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

// FromTeamInvitationsEntityToHTTPResponse renders a team's invitations.
func FromTeamInvitationsEntityToHTTPResponse(e entity.TeamInvitations) []byte {
	out := view.TeamInvitationsResponse{Invitations: make([]view.TeamInvitation, len(e.Invitations)), Version: e.Version}
	for i, inv := range e.Invitations {
		out.Invitations[i] = fromTeamInvitationEntity(inv)
	}
	b, _ := json.Marshal(out)
	return b
}

// FromCreateTeamInvitationEntityToHTTPResponse renders a new invitation.
func FromCreateTeamInvitationEntityToHTTPResponse(e entity.CreateTeamInvitationResponse) []byte {
	b, _ := json.Marshal(view.CreateTeamInvitationResponse{
		Invitation: fromTeamInvitationEntity(e.Invitation), EmailSent: e.EmailSent, Link: e.Link, Version: e.Version,
	})
	return b
}

// FromTeamVersionToHTTPResponse renders the team's version after a change.
func FromTeamVersionToHTTPResponse(version int64) []byte {
	b, _ := json.Marshal(view.TeamVersionResponse{Version: version})
	return b
}

// FromTeamVersionErrorToHTTPResponse renders a change refused because the
// team changed first: the error, and the version to reload at.
func FromTeamVersionErrorToHTTPResponse(e *entity.TeamVersionError) []byte {
	v := view.TeamConflictResponse{Conflict: true, Version: e.Version}
	v.Error.Message, v.Error.Code = e.Error(), http.StatusConflict
	b, _ := json.Marshal(v)
	return b
}

// FromTeamActivitiesEntityToHTTPResponse renders a page of a team's
// activities.
func FromTeamActivitiesEntityToHTTPResponse(e entity.TeamActivities) []byte {
	out := view.ListTeamActivitiesResponse{Activities: make([]view.TeamActivity, len(e.Activities)), Version: e.Version}
	for i, a := range e.Activities {
		v := view.TeamActivity{
			ID: id62(a.ID), Action: a.Action, Actor: fromTeamOwner(a.Actor.UserID, a.Actor.Name, a.Actor.Picture),
			TargetEmail: a.TargetEmail, Details: a.Details, CreatedAt: a.CreatedAt,
		}
		if a.Target.UserID != 0 {
			t := fromTeamOwner(a.Target.UserID, a.Target.Name, a.Target.Picture)
			v.TargetUser = &t
		}
		if a.TargetWorkspaceID != 0 {
			v.TargetWorkspaceID = id62(a.TargetWorkspaceID)
		}
		out.Activities[i] = v
	}
	b, _ := json.Marshal(out)
	return b
}

// FromTeamInvitationPreviewEntityToHTTPResponse renders an invitation link's
// preview.
func FromTeamInvitationPreviewEntityToHTTPResponse(e entity.TeamInvitationPreview) []byte {
	b, _ := json.Marshal(view.TeamInvitationPreview{
		TeamID: id62(e.TeamID), TeamName: e.TeamName, InvitedByName: e.InvitedByName,
		Email: e.Email, EmailMatches: e.EmailMatches, ExpiresAt: e.ExpiresAt,
	})
	return b
}

func fromWorkspaceShareEntity(e entity.WorkspaceShare) view.WorkspaceShare {
	return view.WorkspaceShare{
		TeamID: id62(e.TeamID), TeamName: e.TeamName, WorkspaceID: id62(e.WorkspaceID),
		CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt, Version: e.Version,
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
