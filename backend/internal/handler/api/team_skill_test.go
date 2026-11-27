// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/gofiber/fiber/v3"
	"github.com/mustafaturan/monoflake"
)

// teamSkillCrud answers the team skill calls with one skill, and records the
// request each was given.
type teamSkillCrud struct {
	crud.Controller
	saw any
	err error
}

var tsAt = time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)

func aTeamSkill() entity.TeamSkill {
	return entity.TeamSkill{
		Skill:  entity.Skill{ID: 9, Name: "deploy", Description: "Ship.", SourceType: "manual", FileCount: 1, CreatedAt: tsAt, UpdatedAt: tsAt, Files: []entity.SkillFile{{Path: "SKILL.md", SizeBytes: 3, UpdatedAt: tsAt, Version: 4}}},
		TeamID: 1, TeamWorkspaceID: 50, TeamWorkspaceName: "backend", On: true, Clash: true,
	}
}

func (s *teamSkillCrud) ListWorkspaceTeamSkills(_ context.Context, rq entity.WorkspaceTeamSkillsRequest) (*entity.WorkspaceTeamSkillsResponse, error) {
	s.saw = rq
	return &entity.WorkspaceTeamSkillsResponse{Skills: []entity.TeamSkill{aTeamSkill()}, TeamWorkspaces: []entity.TeamWorkspaceRef{{ID: 50, Name: "backend", TeamID: 1, TeamName: "core"}}}, s.err
}

func (s *teamSkillCrud) SetWorkspaceTeamSkill(_ context.Context, rq entity.SetWorkspaceTeamSkillRequest) (*entity.TeamSkill, error) {
	s.saw = rq
	t := aTeamSkill()
	return &t, s.err
}

func (s *teamSkillCrud) GiveSkillToTeam(_ context.Context, rq entity.GiveSkillToTeamRequest) (*entity.TeamSkill, error) {
	s.saw = rq
	t := aTeamSkill()
	return &t, s.err
}

func (s *teamSkillCrud) ListTeamSkills(_ context.Context, rq entity.TeamSkillsRequest) ([]entity.TeamSkill, error) {
	s.saw = rq
	return []entity.TeamSkill{aTeamSkill()}, s.err
}

func (s *teamSkillCrud) GetTeamSkill(_ context.Context, rq entity.TeamSkillsRequest) (*entity.TeamSkill, error) {
	s.saw = rq
	t := aTeamSkill()
	return &t, s.err
}

func (s *teamSkillCrud) GetTeamSkillFile(_ context.Context, rq entity.TeamSkillsRequest) (*entity.TeamSkillFileResponse, error) {
	s.saw = rq
	return &entity.TeamSkillFileResponse{Skill: aTeamSkill(), File: entity.SkillFile{Path: rq.Path, Content: "body", Version: 4, UpdatedAt: tsAt}}, s.err
}

func (s *teamSkillCrud) SaveTeamSkillFile(_ context.Context, rq entity.SaveTeamSkillFileRequest) (*entity.TeamSkillFileResponse, error) {
	s.saw = rq
	return &entity.TeamSkillFileResponse{Skill: aTeamSkill(), File: entity.SkillFile{Path: "SKILL.md", Version: 5, UpdatedAt: tsAt}}, s.err
}

func (s *teamSkillCrud) DeleteTeamSkillFile(_ context.Context, rq entity.TeamSkillsRequest) (*entity.TeamSkill, error) {
	s.saw = rq
	t := aTeamSkill()
	return &t, s.err
}

func (s *teamSkillCrud) DeleteTeamSkill(_ context.Context, rq entity.TeamSkillsRequest) error {
	s.saw = rq
	return s.err
}

func (s *teamSkillCrud) ImportTeamSkills(_ context.Context, rq entity.ImportTeamSkillsRequest) (*entity.ImportTeamSkillsResponse, error) {
	s.saw = rq
	return &entity.ImportTeamSkillsResponse{
		Imported: []entity.TeamSkill{aTeamSkill()}, Skipped: []entity.SkillImportSkip{{Name: "big", Reason: "too big"}},
		Candidates: []entity.SkillImportCandidate{{Name: "ship", Path: "ship", SkillBytes: 7}}, SourceRepo: "o/r", SourceRef: "main",
		TooLarge: rq.List,
	}, s.err
}

var tsUser = monoflake.ID(77).String()

func doTeamSkill(t *testing.T, c crud.Controller, method, path, body string) (int, string) {
	t.Helper()
	app := fiber.New()
	app.Use(func(ctx fiber.Ctx) error {
		ctx.Locals("user_id", tsUser)
		return ctx.Next()
	})
	h := &handler{crud: c, router: app}
	h.registerTeamSkillRoutes(app.Group("/workspaces"))
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, "/placeholder", r)
	req.URL.Opaque = path // Fiber's Test writes an opaque URL as it stands
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := app.Test(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(b)
}

func TestTeamSkillRoutes(t *testing.T) {
	id := func(n int64) string { return monoflake.ID(n).String() }
	ws := "/workspaces/" + id(4242)
	tw := "/teams/" + id(1) + "/team-workspaces/" + id(50) + "/skills"
	member := entity.TeamSkillsRequest{UserID: 77, TeamID: 1, TeamWorkspaceID: 50}
	withSkill := member
	withSkill.SkillID = 9
	withFile := withSkill
	withFile.Path = "docs/a b.md"
	skillJSON := `"id":"` + id(9) + `","createdAt":"2026-11-15T12:00:00Z","updatedAt":"2026-11-15T12:00:00Z","name":"deploy","description":"Ship.","sourceType":"manual","locallyModified":false,"fileCount":1,"totalBytes":0,"enabled":false,"workspaceIds":[],` +
		`"files":[{"path":"SKILL.md","sizeBytes":3,"updatedAt":"2026-11-15T12:00:00Z","version":4}],"teamId":"` + id(1) + `","teamWorkspaceId":"` + id(50) + `","teamWorkspaceName":"backend","on":true,"clash":true`
	v := int64(4)
	for _, tc := range []struct {
		name, method, path, body string
		status                   int
		want                     any
		body200                  string
	}{
		{"a workspace's team skills", http.MethodGet, ws + "/team-skills?q=dep", "", 200,
			entity.WorkspaceTeamSkillsRequest{WorkspaceID: 4242, UserID: tsUser, Query: "dep"},
			`{"skills":[{` + skillJSON + `}],"teamWorkspaces":[{"id":"` + id(50) + `","name":"backend","teamId":"` + id(1) + `","teamName":"core"}]}`},
		{"opting out", http.MethodPut, ws + "/team-skills/" + id(9), `{"on":false}`, 200,
			entity.SetWorkspaceTeamSkillRequest{WorkspaceID: 4242, UserID: tsUser, SkillID: 9}, `{"skill":{` + skillJSON + `}}`},
		{"giving a skill", http.MethodPost, ws + "/skills/lint/give", `{"teamWorkspaceId":"` + id(50) + `"}`, 200,
			entity.GiveSkillToTeamRequest{WorkspaceID: 4242, UserID: tsUser, Name: "lint", TeamWorkspaceID: 50}, `{"skill":{` + skillJSON + `}}`},
		{"a team workspace's skills", http.MethodGet, tw, "", 200, member, `{"skills":[{` + skillJSON + `}]}`},
		{"one team skill", http.MethodGet, tw + "/" + id(9), "", 200, withSkill, `{"skill":{` + skillJSON + `}}`},
		{"one of its files", http.MethodGet, tw + "/" + id(9) + "/files/docs/a%20b.md", "", 200, withFile,
			`{"skill":{` + skillJSON + `},"file":{"path":"docs/a b.md","sizeBytes":0,"updatedAt":"2026-11-15T12:00:00Z","content":"body","version":4}}`},
		{"writing a file", http.MethodPut, tw + "/" + id(9) + "/files/docs/a%20b.md", `{"content":"x","version":4}`, 200,
			entity.SaveTeamSkillFileRequest{TeamSkillsRequest: withFile, Content: "x", Version: &v}, `"file":{"path":"SKILL.md","sizeBytes":0,"updatedAt":"2026-11-15T12:00:00Z","version":5}`},
		{"creating a skill", http.MethodPost, tw, `{"content":"---"}`, 200,
			entity.SaveTeamSkillFileRequest{TeamSkillsRequest: member, Content: "---"}, `"version":5`},
		{"deleting a file", http.MethodDelete, tw + "/" + id(9) + "/files/docs/a%20b.md", "", 200, withFile, `{"skill":{`},
		{"deleting a skill", http.MethodDelete, tw + "/" + id(9), "", 204, withSkill, ""},
		{"importing", http.MethodPost, tw + "/import", `{"url":"https://github.com/o/r","overwrite":true,"skills":["a"]}`, 200,
			entity.ImportTeamSkillsRequest{TeamSkillsRequest: member, URL: "https://github.com/o/r", Overwrite: true, Skills: []string{"a"}},
			`"skipped":[{"name":"big","reason":"too big"}],"candidates":[{"name":"ship","path":"ship","sizeBytes":7}],"sourceRepo":"o/r","sourceRef":"main","sourceCommit":""}`},
		{"listing", http.MethodPost, tw + "/import", `{"url":"https://github.com/o/r","list":true}`, 200,
			entity.ImportTeamSkillsRequest{TeamSkillsRequest: member, URL: "https://github.com/o/r", List: true},
			`"candidates":[{"name":"ship","path":"ship","sizeBytes":7}],"tooLarge":true,"sourceRepo":"o/r"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &teamSkillCrud{}
			status, body := doTeamSkill(t, c, tc.method, tc.path, tc.body)
			if status != tc.status || !strings.Contains(body, tc.body200) {
				t.Errorf("%d %s, want %d with %s", status, body, tc.status, tc.body200)
			}
			if !reflect.DeepEqual(c.saw, tc.want) {
				t.Errorf("the controller was asked\n %+v\nwant\n %+v", c.saw, tc.want)
			}
		})
	}
}

// A request that does not parse is 400 before the controller; a refusal is
// answered as the controller wrote it.
func TestTeamSkillRoutesRefuse(t *testing.T) {
	id := func(n int64) string { return monoflake.ID(n).String() }
	ws := "/workspaces/" + id(4242)
	tw := "/teams/" + id(1) + "/team-workspaces/" + id(50) + "/skills"
	for _, tc := range []struct {
		name, method, path, body string
	}{
		{"a workspace that is not an id", http.MethodGet, "/workspaces/!/team-skills", ""},
		{"a choice with no on", http.MethodPut, ws + "/team-skills/" + id(9), `{}`},
		{"a choice of no skill", http.MethodPut, ws + "/team-skills/!", `{"on":true}`},
		{"a choice that is not JSON", http.MethodPut, ws + "/team-skills/" + id(9), `{`},
		{"a gift to no team workspace", http.MethodPost, ws + "/skills/lint/give", `{"teamWorkspaceId":""}`},
		{"a gift that is not JSON", http.MethodPost, ws + "/skills/lint/give", `{`},
		{"a gift of a badly escaped name", http.MethodPost, ws + "/skills/%zz/give", `{"teamWorkspaceId":"` + id(50) + `"}`},
		{"a team that is not an id", http.MethodGet, "/teams/!/team-workspaces/" + id(50) + "/skills", ""},
		{"a skill that is not an id", http.MethodGet, tw + "/!", ""},
		{"a badly escaped path", http.MethodGet, tw + "/" + id(9) + "/files/%zz", ""},
		{"a skill read with a bad team", http.MethodGet, "/teams/!/team-workspaces/" + id(50) + "/skills/" + id(9), ""},
		{"a file read with a bad team", http.MethodGet, "/teams/!/team-workspaces/" + id(50) + "/skills/" + id(9) + "/files/a.md", ""},
		{"a save that is not JSON", http.MethodPut, tw + "/" + id(9) + "/files/a.md", `{`},
		{"a file deleted with a bad team", http.MethodDelete, "/teams/!/team-workspaces/" + id(50) + "/skills/" + id(9) + "/files/a.md", ""},
		{"a skill deleted with a bad team", http.MethodDelete, "/teams/!/team-workspaces/" + id(50) + "/skills/" + id(9), ""},
		{"an import with no url", http.MethodPost, tw + "/import", `{}`},
	} {
		c := &teamSkillCrud{}
		if status, body := doTeamSkill(t, c, tc.method, tc.path, tc.body); status != http.StatusBadRequest || c.saw != nil {
			t.Errorf("%s: %d %s, asked %+v; want 400 before the controller", tc.name, status, body, c.saw)
		}
	}

	refused := &crud.SkillError{Kind: crud.SkillConflict, Message: "taken"}
	for _, tc := range []struct {
		name, method, path, body string
		err                      error
		status                   int
	}{
		{"listing", http.MethodGet, ws + "/team-skills", "", base.ErrNotFound, 404},
		{"a choice", http.MethodPut, ws + "/team-skills/" + id(9), `{"on":true}`, base.ErrNotFound, 404},
		{"a gift", http.MethodPost, ws + "/skills/lint/give", `{"teamWorkspaceId":"` + id(50) + `"}`, refused, 409},
		{"the team's list", http.MethodGet, tw, "", base.ErrNotFound, 404},
		{"one skill", http.MethodGet, tw + "/" + id(9), "", base.ErrNotFound, 404},
		{"one file", http.MethodGet, tw + "/" + id(9) + "/files/a.md", "", base.ErrNotFound, 404},
		{"a save", http.MethodPut, tw + "/" + id(9) + "/files/a.md", `{"content":"x"}`, refused, 409},
		{"a file deleted", http.MethodDelete, tw + "/" + id(9) + "/files/a.md", "", base.ErrNotFound, 404},
		{"a skill deleted", http.MethodDelete, tw + "/" + id(9), "", base.ErrNotFound, 404},
		{"an import", http.MethodPost, tw + "/import", `{"url":"u"}`, errors.New("boom"), 500},
	} {
		status, body := doTeamSkill(t, &teamSkillCrud{err: tc.err}, tc.method, tc.path, tc.body)
		if status != tc.status || (tc.err == refused && !strings.Contains(body, `"taken"`)) {
			t.Errorf("%s: %d %s, want %d", tc.name, status, body, tc.status)
		}
	}
}
