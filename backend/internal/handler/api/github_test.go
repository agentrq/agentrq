// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package api

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
)

type githubCrud struct {
	crud.Controller
	err error
	saw any
}

func githubConnection() *entity.GitHubConnectionResponse {
	return &entity.GitHubConnectionResponse{
		Enabled: true, InstallURL: "https://github.com/apps/x/installations/new?state=s",
		Installations: []entity.GitHubInstallation{{InstallationID: 99, Account: "acme", AccountType: "Organization", CreatedAt: time.Date(2026, 11, 24, 0, 0, 0, 0, time.UTC)}},
	}
}

func (g *githubCrud) GetGitHubConnection(_ context.Context, rq entity.GetGitHubConnectionRequest) (*entity.GitHubConnectionResponse, error) {
	g.saw = rq
	return githubConnection(), g.err
}

func (g *githubCrud) ConnectGitHub(_ context.Context, rq entity.ConnectGitHubRequest) (*entity.GitHubConnectionResponse, error) {
	g.saw = rq
	return githubConnection(), g.err
}

func (g *githubCrud) DisconnectGitHub(_ context.Context, rq entity.DisconnectGitHubRequest) error {
	g.saw = rq
	return g.err
}

func TestGitHubRoutes(t *testing.T) {
	for _, tc := range []struct {
		name, method, path, body string
		status                   int
		want                     any
		contains                 string
	}{
		{"the connection", http.MethodGet, "/github", "", 200, entity.GetGitHubConnectionRequest{UserID: "user-1"},
			`{"enabled":true,"installUrl":"https://github.com/apps/x/installations/new?state=s","installations":[{"installationId":99,"account":"acme","accountType":"Organization","createdAt":"2026-11-24T00:00:00Z"}]}`},
		{"a connect", http.MethodPost, "/github/installations", `{"code":"c","state":"s"}`, 200, entity.ConnectGitHubRequest{UserID: "user-1", Code: "c", State: "s"}, `"account":"acme"`},
		{"a connect with no state", http.MethodPost, "/github/installations", `{"code":"c"}`, 422, nil, "invalid request payload"},
		{"a disconnect", http.MethodDelete, "/github/installations/99", "", 204, entity.DisconnectGitHubRequest{UserID: "user-1", InstallationID: 99}, ""},
		{"a disconnect of no number", http.MethodDelete, "/github/installations/abc", "", 422, nil, "invalid request payload"},
		{"a disconnect of zero", http.MethodDelete, "/github/installations/0", "", 422, nil, "invalid request payload"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := &githubCrud{}
			status, body := doSkill(t, g, tc.method, tc.path, tc.body)
			if status != tc.status || !strings.Contains(body, tc.contains) {
				t.Fatalf("got %d %s", status, body)
			}
			if !reflect.DeepEqual(g.saw, tc.want) {
				t.Fatalf("controller saw %#v, want %#v", g.saw, tc.want)
			}
		})
	}
}

func TestGitHubRoutes_Errors(t *testing.T) {
	for _, tc := range []struct {
		method, path, body string
		err                error
		status             int
		contains           string
	}{
		{http.MethodGet, "/github", "", errors.New("db down"), 500, ""},
		{http.MethodPost, "/github/installations", `{"code":"c","state":"s"}`, &crud.SkillError{Kind: crud.SkillInvalid, Message: "this GitHub connection link has expired"}, 422, "expired"},
		{http.MethodPost, "/github/installations", `{"code":"c","state":"s"}`, &crud.SkillError{Kind: crud.SkillUpstream, Message: "GitHub answered 500"}, 502, "GitHub answered 500"},
		{http.MethodDelete, "/github/installations/5", "", base.ErrNotFound, 404, ""},
	} {
		status, body := doSkill(t, &githubCrud{err: tc.err}, tc.method, tc.path, tc.body)
		if status != tc.status || !strings.Contains(body, tc.contains) {
			t.Errorf("%s %s: got %d %s", tc.method, tc.path, status, body)
		}
	}
}
