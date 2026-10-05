// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"encoding/json"
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
	"github.com/gofiber/fiber/v2"
	"github.com/mustafaturan/monoflake"
)

// skillCrud answers every skill call with err, or with a fixed response, and
// records the request it was given.
type skillCrud struct {
	crud.Controller
	err error
	saw any
}

var skillNow = time.Date(2026, 9, 23, 8, 0, 0, 0, time.UTC)

func testSkill() entity.Skill {
	return entity.Skill{
		ID: 7, CreatedAt: skillNow, UpdatedAt: skillNow, WorkspaceID: 4242, Name: "tdd", Description: "Test first.",
		SourceType: "github", SourceRepo: "obra/superpowers", FileCount: 2, TotalBytes: 30,
		SharedFromWorkspaceID: 4243,
		Files:                 []entity.SkillFile{{Path: "SKILL.md", SizeBytes: 20, UpdatedAt: skillNow}},
	}
}

func (s *skillCrud) SearchSkills(_ context.Context, rq entity.SearchSkillsRequest) (*entity.SearchSkillsResponse, error) {
	s.saw = rq
	return &entity.SearchSkillsResponse{Skills: []entity.Skill{testSkill(), {ID: 8, Name: "own"}}, Total: 2}, s.err
}

func (s *skillCrud) GetSkill(_ context.Context, rq entity.GetSkillRequest) (*entity.GetSkillResponse, error) {
	s.saw = rq
	return &entity.GetSkillResponse{Skill: testSkill()}, s.err
}

func (s *skillCrud) SetSkillEnabled(_ context.Context, rq entity.SetSkillEnabledRequest) (*entity.SetSkillEnabledResponse, error) {
	s.saw = rq
	sk := testSkill()
	sk.Enabled = rq.Enabled
	return &entity.SetSkillEnabledResponse{Skill: sk}, s.err
}

func (s *skillCrud) GetSkillFile(_ context.Context, rq entity.GetSkillFileRequest) (*entity.GetSkillFileResponse, error) {
	s.saw = rq
	return &entity.GetSkillFileResponse{Skill: testSkill(), File: entity.SkillFile{Path: rq.Path, Content: "body"}}, s.err
}

func (s *skillCrud) DeleteSkill(_ context.Context, rq entity.DeleteSkillRequest) error {
	s.saw = rq
	return s.err
}

func (s *skillCrud) ImportSkills(_ context.Context, rq entity.ImportSkillsRequest) (*entity.ImportSkillsResponse, error) {
	s.saw = rq
	return &entity.ImportSkillsResponse{
		Imported:   []entity.Skill{{Name: "tdd", FileCount: 2, TotalBytes: 30}},
		Skipped:    []entity.SkillImportSkip{{Path: "skills/big", Reason: "too big"}},
		Candidates: []entity.SkillImportCandidate{{Name: "ship", Path: "ship", SkillBytes: 77710, Reason: "SKILL.md is too big"}, {Name: "careful", Path: "careful", SkillBytes: 3516}},
		SourceRepo: "obra/superpowers", SourceRef: "main",
	}, s.err
}

func (s *skillCrud) ListSkillShares(_ context.Context, rq entity.ListSkillSharesRequest) (*entity.ListSkillSharesResponse, error) {
	s.saw = rq
	return &entity.ListSkillSharesResponse{Shares: []entity.SkillShare{{TargetWorkspaceID: 4243, CreatedAt: skillNow}}}, s.err
}

func (s *skillCrud) ShareSkill(_ context.Context, rq entity.ShareSkillRequest) error {
	s.saw = rq
	return s.err
}

func (s *skillCrud) UnshareSkill(_ context.Context, rq entity.ShareSkillRequest) error {
	s.saw = rq
	return s.err
}

func skillApp(c crud.Controller) *fiber.App {
	h := &handler{crud: c}
	app := fiber.New()
	app.Use(func(ctx *fiber.Ctx) error {
		ctx.Locals("user_id", "user-1")
		return ctx.Next()
	})
	h.registerSkillRoutes(app.Group("/workspaces"))
	return app
}

var (
	skillWS     = monoflake.ID(4242).String()
	skillTarget = monoflake.ID(4243).String()
)

func doSkill(t *testing.T, c crud.Controller, method, path, body string) (int, string) {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	// httptest.NewRequest refuses a malformed escape, so the raw request line
	// is set by hand, as the memory tests do.
	req := httptest.NewRequest(method, "/placeholder", r)
	req.RequestURI, req.URL.Path, req.URL.RawPath = path, path, path
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := skillApp(c).Test(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(b)
}

func TestSkillRoutes(t *testing.T) {
	prefix := "/workspaces/" + skillWS + "/skills"
	for _, tc := range []struct {
		name, method, path, body string
		status                   int
		want                     any
		contains                 []string
	}{
		{"list", http.MethodGet, prefix, "", 200, entity.SearchSkillsRequest{WorkspaceID: 4242, UserID: "user-1"},
			[]string{`"name":"tdd"`, `"sharedFromWorkspaceId":"` + skillTarget + `"`, `"sourceRepo":"obra/superpowers"`, `"files":[{"path":"SKILL.md","sizeBytes":20`}},
		{"search", http.MethodGet, prefix + "?q=review&limit=20&offset=40", "", 200,
			entity.SearchSkillsRequest{WorkspaceID: 4242, UserID: "user-1", Query: "review", Limit: 20, Offset: 40},
			[]string{`"total":2`}},
		{"get", http.MethodGet, prefix + "/tdd", "", 200, entity.GetSkillRequest{WorkspaceID: 4242, UserID: "user-1", Name: "tdd"},
			[]string{`"skill":{`, `"fileCount":2`}},
		{"file, with a nested and encoded path", http.MethodGet, prefix + "/tdd/files/scripts/run%20me.sh", "", 200,
			entity.GetSkillFileRequest{WorkspaceID: 4242, UserID: "user-1", Name: "tdd", Path: "scripts/run me.sh"},
			[]string{`"file":{"path":"scripts/run me.sh"`, `"content":"body"`}},
		{"import", http.MethodPost, prefix + "/import", `{"url":"https://github.com/obra/superpowers","overwrite":true}`, 200,
			entity.ImportSkillsRequest{WorkspaceID: 4242, UserID: "user-1", URL: "https://github.com/obra/superpowers", Overwrite: true},
			[]string{`"imported":[{"name":"tdd","fileCount":2,"totalBytes":30}]`, `"skipped":[{"path":"skills/big","reason":"too big"}]`, `"sourceRef":"main"`}},
		{"import, choosing skills", http.MethodPost, prefix + "/import", `{"url":"https://github.com/garrytan/gstack","skills":["ship","careful"]}`, 200,
			entity.ImportSkillsRequest{WorkspaceID: 4242, UserID: "user-1", URL: "https://github.com/garrytan/gstack", Skills: []string{"ship", "careful"}},
			[]string{`"candidates":[{"name":"ship","path":"ship","sizeBytes":77710,"reason":"SKILL.md is too big"},{"name":"careful","path":"careful","sizeBytes":3516}]`}},
		{"turn off", http.MethodPatch, prefix + "/tdd", `{"enabled":false}`, 200,
			entity.SetSkillEnabledRequest{WorkspaceID: 4242, UserID: "user-1", Name: "tdd", Enabled: false},
			[]string{`"skill":{`, `"enabled":false`}},
		{"turn on", http.MethodPatch, prefix + "/tdd", `{"enabled":true}`, 200,
			entity.SetSkillEnabledRequest{WorkspaceID: 4242, UserID: "user-1", Name: "tdd", Enabled: true},
			[]string{`"enabled":true`}},
		{"delete", http.MethodDelete, prefix + "/tdd", "", 204, entity.DeleteSkillRequest{WorkspaceID: 4242, UserID: "user-1", Name: "tdd"}, nil},
		{"shares", http.MethodGet, prefix + "/tdd/shares", "", 200, entity.ListSkillSharesRequest{WorkspaceID: 4242, UserID: "user-1", Name: "tdd"},
			[]string{`"shares":[{"targetWorkspaceId":"` + skillTarget + `"`}},
		{"share", http.MethodPut, prefix + "/tdd/shares/" + skillTarget, "", 204,
			entity.ShareSkillRequest{WorkspaceID: 4242, UserID: "user-1", Name: "tdd", TargetWorkspaceID: 4243}, nil},
		{"unshare", http.MethodDelete, prefix + "/tdd/shares/" + skillTarget, "", 204,
			entity.ShareSkillRequest{WorkspaceID: 4242, UserID: "user-1", Name: "tdd", TargetWorkspaceID: 4243}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &skillCrud{}
			status, body := doSkill(t, c, tc.method, tc.path, tc.body)
			if status != tc.status {
				t.Fatalf("status %d, want %d: %s", status, tc.status, body)
			}
			if !reflect.DeepEqual(c.saw, tc.want) {
				t.Errorf("controller saw %+v, want %+v", c.saw, tc.want)
			}
			for _, s := range tc.contains {
				if !strings.Contains(body, s) {
					t.Errorf("body %s lacks %s", body, s)
				}
			}
		})
	}
}

// A request the path cannot be read from never reaches the controller.
func TestSkillRoutes_Unparseable(t *testing.T) {
	bad := "/workspaces/" + skillWS + "/skills"
	for _, tc := range []struct{ name, method, path, body string }{
		{"list: no workspace", http.MethodGet, "/workspaces/0/skills", ""},
		{"get: bad escape", http.MethodGet, bad + "/%zz", ""},
		{"file: no workspace", http.MethodGet, "/workspaces/0/skills/tdd/files/SKILL.md", ""},
		{"file: bad path escape", http.MethodGet, bad + "/tdd/files/%zz", ""},
		{"import: no url", http.MethodPost, bad + "/import", `{"overwrite":true}`},
		{"import: not json", http.MethodPost, bad + "/import", `{`},
		{"import: no workspace", http.MethodPost, "/workspaces/0/skills/import", `{"url":"u"}`},
		{"delete: no workspace", http.MethodDelete, "/workspaces/0/skills/tdd", ""},
		{"turn off: enabled not given", http.MethodPatch, bad + "/tdd", `{}`},
		{"turn off: not json", http.MethodPatch, bad + "/tdd", `{`},
		{"turn off: no workspace", http.MethodPatch, "/workspaces/0/skills/tdd", `{"enabled":false}`},
		{"shares: no workspace", http.MethodGet, "/workspaces/0/skills/tdd/shares", ""},
		{"share: no target", http.MethodPut, bad + "/tdd/shares/0", ""},
		{"share: bad name", http.MethodPut, bad + "/%zz/shares/" + skillTarget, ""},
		{"unshare: no workspace", http.MethodDelete, "/workspaces/0/skills/tdd/shares/" + skillTarget, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &skillCrud{}
			status, _ := doSkill(t, c, tc.method, tc.path, tc.body)
			if status != http.StatusUnprocessableEntity || c.saw != nil {
				t.Errorf("status %d, controller saw %+v; want 422 and no call", status, c.saw)
			}
		})
	}
}

// A skill refusal is passed on word for word with its own status; any other
// error is not, since it may carry internals.
func TestSkillRoutes_Errors(t *testing.T) {
	prefix := "/workspaces/" + skillWS + "/skills"
	routes := []struct{ method, path, body string }{
		{http.MethodGet, prefix, ""},
		{http.MethodGet, prefix + "/tdd", ""},
		{http.MethodGet, prefix + "/tdd/files/SKILL.md", ""},
		{http.MethodPost, prefix + "/import", `{"url":"u"}`},
		{http.MethodDelete, prefix + "/tdd", ""},
		{http.MethodPatch, prefix + "/tdd", `{"enabled":false}`},
		{http.MethodGet, prefix + "/tdd/shares", ""},
		{http.MethodPut, prefix + "/tdd/shares/" + skillTarget, ""},
		{http.MethodDelete, prefix + "/tdd/shares/" + skillTarget, ""},
	}
	for _, tc := range []struct {
		name    string
		err     error
		status  int
		message string
	}{
		{"invalid", &crud.SkillError{Kind: crud.SkillInvalid, Message: "name it in lowercase"}, 422, "name it in lowercase"},
		{"read-only", &crud.SkillError{Kind: crud.SkillReadOnly, Message: "change it there"}, 403, "change it there"},
		{"conflict", &crud.SkillError{Kind: crud.SkillConflict, Message: "already taken"}, 409, "already taken"},
		{"upstream", &crud.SkillError{Kind: crud.SkillUpstream, Message: "GitHub answered 500"}, 502, "GitHub answered 500"},
		{"not found", base.ErrNotFound, 404, "not found"},
		{"internal", errors.New("disk failure at /var/lib"), 500, "internal server error"},
	} {
		for _, r := range routes {
			t.Run(tc.name+" "+r.method+" "+r.path, func(t *testing.T) {
				status, body := doSkill(t, &skillCrud{err: tc.err}, r.method, r.path, r.body)
				var e struct {
					Error struct {
						Message string `json:"message"`
						Code    int    `json:"code"`
					} `json:"error"`
				}
				if err := json.Unmarshal([]byte(body), &e); err != nil {
					t.Fatalf("decode %q: %v", body, err)
				}
				if status != tc.status || e.Error.Code != tc.status || e.Error.Message != tc.message {
					t.Errorf("got %d %+v, want %d %q", status, e.Error, tc.status, tc.message)
				}
			})
		}
	}
}
