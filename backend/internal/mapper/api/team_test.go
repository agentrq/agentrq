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
	for name, c := range map[string]struct{ got, want string }{
		"teams":            {string(FromTeamEntitiesToHTTPResponse(nil)), `{"teams":[]}`},
		"members":          {string(FromTeamMembersEntityToHTTPResponse(entity.TeamMembers{Version: 2})), `{"members":[],"version":2}`},
		"invitations":      {string(FromTeamInvitationEntitiesToHTTPResponse(nil)), `{"invitations":[]}`},
		"team invitations": {string(FromTeamInvitationsEntityToHTTPResponse(entity.TeamInvitations{Version: 2})), `{"invitations":[],"version":2}`},
		"shares":           {string(FromWorkspaceShareEntitiesToHTTPResponse(nil)), `{"shares":[]}`},
		"activities":       {string(FromTeamActivitiesEntityToHTTPResponse(entity.TeamActivities{Version: 2})), `{"activities":[],"version":2}`},
	} {
		if c.got != c.want {
			t.Errorf("no %s rendered %s, want %s", name, c.got, c.want)
		}
	}
}

func TestTeamEntitiesRender(t *testing.T) {
	at := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	inv := entity.TeamInvitation{ID: 3, TeamID: 1, TeamName: "core", Email: "d@x.io", InvitedBy: 2, InvitedByName: "Bob", CreatedAt: at, ExpiresAt: at}
	invJSON := `{"id":"00000000003","teamId":"00000000001","teamName":"core","email":"d@x.io","invitedBy":"00000000002",` +
		`"invitedByName":"Bob","createdAt":"2026-11-13T12:00:00Z","expiresAt":"2026-11-13T12:00:00Z"}`
	for name, c := range map[string]struct{ got, want string }{
		"a team": {string(FromTeamEntityToHTTPResponse(entity.Team{ID: 1, Name: "core", Version: 4, MemberCount: 2, CreatedAt: at})),
			`{"id":"00000000001","name":"core","version":4,"memberCount":2,"createdAt":"2026-11-13T12:00:00Z"}`},
		"a member": {string(FromTeamMembersEntityToHTTPResponse(entity.TeamMembers{Members: []entity.TeamMember{{UserID: 2, Name: "Bob", Email: "b@x.io", Picture: "p", JoinedAt: at}}, Version: 3})),
			`{"members":[{"userId":"00000000002","name":"Bob","email":"b@x.io","picture":"p","joinedAt":"2026-11-13T12:00:00Z"}],"version":3}`},
		"an emailed invitation": {string(FromCreateTeamInvitationEntityToHTTPResponse(entity.CreateTeamInvitationResponse{Invitation: inv, EmailSent: true, Version: 5})),
			`{"invitation":` + invJSON + `,"emailSent":true,"version":5}`},
		"a team's invitations": {string(FromTeamInvitationsEntityToHTTPResponse(entity.TeamInvitations{Invitations: []entity.TeamInvitation{inv}, Version: 5})),
			`{"invitations":[` + invJSON + `],"version":5}`},
		"a preview": {string(FromTeamInvitationPreviewEntityToHTTPResponse(entity.TeamInvitationPreview{TeamID: 1, TeamName: "core", InvitedByName: "Bob", Email: "d***@x.io", EmailMatches: true, ExpiresAt: at})),
			`{"teamId":"00000000001","teamName":"core","invitedByName":"Bob","email":"d***@x.io","emailMatches":true,"expiresAt":"2026-11-13T12:00:00Z"}`},
		"a share": {string(FromWorkspaceShareEntityToHTTPResponse(entity.WorkspaceShare{TeamID: 1, TeamName: "core", WorkspaceID: 4, CreatedAt: at, UpdatedAt: at})),
			`{"teamId":"00000000001","teamName":"core","workspaceId":"00000000004","createdAt":"2026-11-13T12:00:00Z","updatedAt":"2026-11-13T12:00:00Z"}`},
		"a share just made": {string(FromWorkspaceShareEntityToHTTPResponse(entity.WorkspaceShare{TeamID: 1, WorkspaceID: 4, CreatedAt: at, UpdatedAt: at, Version: 6})),
			`{"teamId":"00000000001","teamName":"","workspaceId":"00000000004","createdAt":"2026-11-13T12:00:00Z","updatedAt":"2026-11-13T12:00:00Z","version":6}`},
		"a version": {string(FromTeamVersionToHTTPResponse(7)), `{"version":7}`},
		"a conflict": {string(FromTeamVersionErrorToHTTPResponse(&entity.TeamVersionError{Version: 8})),
			`{"error":{"message":"someone changed this team: reload it and try again","code":409},"conflict":true,"version":8}`},
		"activities": {string(FromTeamActivitiesEntityToHTTPResponse(entity.TeamActivities{Version: 9, Activities: []entity.TeamActivity{
			{ID: 5, Action: "member.removed", Actor: entity.TeamOwner{UserID: 1, Name: "Alice", Picture: "a"}, Target: entity.TeamOwner{UserID: 2, Name: "Bob"},
				Details: map[string]string{}, CreatedAt: at},
			{ID: 6, Action: "workspace.shared", Actor: entity.TeamOwner{UserID: 1, Name: "Alice"}, TargetWorkspaceID: 4,
				Details: map[string]string{"workspaceName": "api"}, CreatedAt: at},
			{ID: 7, Action: "member.invited", Actor: entity.TeamOwner{UserID: 1}, TargetEmail: "d@x.io", Details: map[string]string{}, CreatedAt: at},
		}})), `{"activities":[` +
			`{"id":"00000000005","action":"member.removed","actor":{"userId":"00000000001","name":"Alice","picture":"a"},` +
			`"targetUser":{"userId":"00000000002","name":"Bob","picture":""},"details":{},"createdAt":"2026-11-13T12:00:00Z"},` +
			`{"id":"00000000006","action":"workspace.shared","actor":{"userId":"00000000001","name":"Alice","picture":""},` +
			`"targetWorkspaceId":"00000000004","details":{"workspaceName":"api"},"createdAt":"2026-11-13T12:00:00Z"},` +
			`{"id":"00000000007","action":"member.invited","actor":{"userId":"00000000001","name":"","picture":""},` +
			`"targetEmail":"d@x.io","details":{},"createdAt":"2026-11-13T12:00:00Z"}],"version":9}`},
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
