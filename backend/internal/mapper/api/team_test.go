// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
)

func TestTeamErrorsAnswerTheirStatus(t *testing.T) {
	for kind, want := range map[entity.TeamErrorKind]int{
		entity.TeamInvalid:   http.StatusUnprocessableEntity,
		entity.TeamForbidden: http.StatusForbidden,
		entity.TeamConflict:  http.StatusConflict,
		entity.TeamGone:      http.StatusGone,
		entity.TeamThrottled: http.StatusTooManyRequests,
	} {
		b, status := FromErrorToHTTPResponse(&entity.TeamError{Kind: kind, Message: "why"})
		if status != want || string(b) != fmt.Sprintf(`{"error":{"message":"why","code":%d}}`, want) {
			t.Errorf("team error kind %d answered %d %s, want %d with its message", kind, status, b, want)
		}
	}
}

func TestTeamListsRenderEmptyNotNull(t *testing.T) {
	for name, got := range map[string]string{
		"teams":       string(FromTeamEntitiesToHTTPResponse(nil)),
		"members":     string(FromTeamMemberEntitiesToHTTPResponse(nil)),
		"invitations": string(FromTeamInvitationEntitiesToHTTPResponse(nil)),
		"shares":      string(FromWorkspaceShareEntitiesToHTTPResponse(nil)),
	} {
		if got != `{"`+name+`":[]}` {
			t.Errorf("no %s rendered %s, want an empty list", name, got)
		}
	}
}

func TestTeamEntitiesRender(t *testing.T) {
	at := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	for name, c := range map[string]struct{ got, want string }{
		"a team": {string(FromTeamEntityToHTTPResponse(entity.Team{ID: 1, Name: "core", Role: "owner", MemberCount: 2, CreatedAt: at})),
			`{"id":"00000000001","name":"core","role":"owner","memberCount":2,"createdAt":"2026-11-13T12:00:00Z"}`},
		"a member": {string(FromTeamMemberEntitiesToHTTPResponse([]entity.TeamMember{{UserID: 2, Name: "Bob", Email: "b@x.io", Picture: "p", Role: "admin", JoinedAt: at}})),
			`{"members":[{"userId":"00000000002","name":"Bob","email":"b@x.io","picture":"p","role":"admin","joinedAt":"2026-11-13T12:00:00Z"}]}`},
		"an emailed invitation": {string(FromCreateTeamInvitationEntityToHTTPResponse(entity.CreateTeamInvitationResponse{
			Invitation: entity.TeamInvitation{ID: 3, TeamID: 1, TeamName: "core", Email: "d@x.io", Role: "member", InvitedBy: 2, InvitedByName: "Bob", CreatedAt: at, ExpiresAt: at},
			EmailSent:  true,
		})), `{"invitation":{"id":"00000000003","teamId":"00000000001","teamName":"core","email":"d@x.io","role":"member","invitedBy":"00000000002",` +
			`"invitedByName":"Bob","createdAt":"2026-11-13T12:00:00Z","expiresAt":"2026-11-13T12:00:00Z"},"emailSent":true}`},
		"a preview": {string(FromTeamInvitationPreviewEntityToHTTPResponse(entity.TeamInvitationPreview{TeamName: "core", Role: "member", InvitedByName: "Bob", Email: "d***@x.io", EmailMatches: true, ExpiresAt: at})),
			`{"teamName":"core","role":"member","invitedByName":"Bob","email":"d***@x.io","emailMatches":true,"expiresAt":"2026-11-13T12:00:00Z"}`},
		"a share": {string(FromWorkspaceShareEntityToHTTPResponse(entity.WorkspaceShare{TeamID: 1, TeamName: "core", WorkspaceID: 4, Group: "g", CreatedAt: at, UpdatedAt: at})),
			`{"teamId":"00000000001","teamName":"core","workspaceId":"00000000004","group":"g","createdAt":"2026-11-13T12:00:00Z","updatedAt":"2026-11-13T12:00:00Z"}`},
	} {
		if c.got != c.want {
			t.Errorf("%s rendered\n %s\nwant\n %s", name, c.got, c.want)
		}
	}
	var v struct{ Name string }
	if !FromHTTPRequestToTeamRequestView([]byte(`{"name":"x"}`), &v) || v.Name != "x" || FromHTTPRequestToTeamRequestView([]byte(`{`), &v) {
		t.Error("a team request body is read wrong: JSON must parse and anything else must not")
	}
}
