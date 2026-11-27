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

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
)

type gitlabCrud struct {
	crud.Controller
	err error
	saw any
}

func gitlabConnection() *entity.GitLabConnectionResponse {
	return &entity.GitLabConnectionResponse{Host: "gitlab.example", Enabled: true, AuthorizeURL: "https://gitlab.example/oauth/authorize?state=s", Username: "ann"}
}

func (g *gitlabCrud) GetGitLabConnection(_ context.Context, rq entity.GetGitLabConnectionRequest) (*entity.GitLabConnectionResponse, error) {
	g.saw = rq
	return gitlabConnection(), g.err
}

func (g *gitlabCrud) ConnectGitLab(_ context.Context, rq entity.ConnectGitLabRequest) (*entity.GitLabConnectionResponse, error) {
	g.saw = rq
	return gitlabConnection(), g.err
}

func (g *gitlabCrud) DisconnectGitLab(_ context.Context, rq entity.DisconnectGitLabRequest) error {
	g.saw = rq
	return g.err
}

func TestGitLabRoutes(t *testing.T) {
	for _, tc := range []struct {
		name, method, path, body string
		status                   int
		want                     any
		contains                 string
	}{
		{"the connection", http.MethodGet, "/gitlab", "", 200, entity.GetGitLabConnectionRequest{UserID: "user-1"},
			`{"host":"gitlab.example","enabled":true,"authorizeUrl":"https://gitlab.example/oauth/authorize?state=s","username":"ann"}`},
		{"a connect", http.MethodPost, "/gitlab/connection", `{"code":"c","state":"s"}`, 200, entity.ConnectGitLabRequest{UserID: "user-1", Code: "c", State: "s"}, `"username":"ann"`},
		{"a connect with no state", http.MethodPost, "/gitlab/connection", `{"code":"c"}`, 422, nil, "invalid request payload"},
		{"a disconnect", http.MethodDelete, "/gitlab/connection", "", 204, entity.DisconnectGitLabRequest{UserID: "user-1"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := &gitlabCrud{}
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

func TestGitLabRoutes_Errors(t *testing.T) {
	for _, tc := range []struct {
		method, path, body string
		err                error
		status             int
		contains           string
	}{
		{http.MethodGet, "/gitlab", "", errors.New("db down"), 500, ""},
		{http.MethodPost, "/gitlab/connection", `{"code":"c","state":"s"}`, &crud.SkillError{Kind: crud.SkillInvalid, Message: "this GitLab connection link has expired"}, 422, "expired"},
		{http.MethodPost, "/gitlab/connection", `{"code":"c","state":"s"}`, &crud.SkillError{Kind: crud.SkillUpstream, Message: "GitLab answered 500"}, 502, "GitLab answered 500"},
		{http.MethodDelete, "/gitlab/connection", "", base.ErrNotFound, 404, ""},
	} {
		status, body := doSkill(t, &gitlabCrud{err: tc.err}, tc.method, tc.path, tc.body)
		if status != tc.status || !strings.Contains(body, tc.contains) {
			t.Errorf("%s %s: got %d %s", tc.method, tc.path, status, body)
		}
	}
}
