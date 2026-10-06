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
		ID: 7, CreatedAt: skillNow, UpdatedAt: skillNow, Name: "tdd", Description: "Test first.",
		SourceType: "github", SourceRepo: "obra/superpowers", FileCount: 2, TotalBytes: 30,
		Enabled: true, WorkspaceIDs: []int64{4242, 4243}, WorkspaceEnabled: true,
		Files: []entity.SkillFile{{Path: "SKILL.md", SizeBytes: 20, UpdatedAt: skillNow}},
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

func skillApp(c crud.Controller) *fiber.App {
	app := fiber.New()
	app.Use(func(ctx *fiber.Ctx) error {
		ctx.Locals("user_id", "user-1")
		return ctx.Next()
	})
	h := &handler{crud: c, router: app}
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
	account := "/skills"
	ws := "/workspaces/" + skillWS + "/skills"
	for _, tc := range []struct {
		name, method, path, body string
		status                   int
		want                     any
		contains                 []string
	}{
		{"the account's skills are listed with the workspaces each is on in", http.MethodGet, account, "", 200,
			entity.SearchSkillsRequest{UserID: "user-1"},
			[]string{`"name":"tdd"`, `"workspaceIds":["` + skillWS + `","` + skillTarget + `"]`, `"sourceRepo":"obra/superpowers"`, `"files":[{"path":"SKILL.md","sizeBytes":20`}},
		{"a skill on in no workspace lists an empty array of workspaces", http.MethodGet, account, "", 200,
			entity.SearchSkillsRequest{UserID: "user-1"},
			[]string{`{"id":"` + monoflake.ID(8).String() + `"`, `"name":"own"`, `"enabled":false,"workspaceIds":[]}`}},
		{"the account's skills are searched and paged", http.MethodGet, account + "?q=review&limit=20&offset=40", "", 200,
			entity.SearchSkillsRequest{UserID: "user-1", Query: "review", Limit: 20, Offset: 40},
			[]string{`"total":2`}},
		{"one of the account's skills is read", http.MethodGet, account + "/tdd", "", 200,
			entity.GetSkillRequest{UserID: "user-1", Name: "tdd"},
			[]string{`"skill":{`, `"fileCount":2`}},
		{"a file of the account's skill is read by a nested and encoded path", http.MethodGet, account + "/tdd/files/scripts/run%20me.sh", "", 200,
			entity.GetSkillFileRequest{UserID: "user-1", Name: "tdd", Path: "scripts/run me.sh"},
			[]string{`"file":{"path":"scripts/run me.sh"`, `"content":"body"`}},
		{"an import goes into the account", http.MethodPost, account + "/import", `{"url":"https://github.com/obra/superpowers","overwrite":true}`, 200,
			entity.ImportSkillsRequest{UserID: "user-1", URL: "https://github.com/obra/superpowers", Overwrite: true, WorkspaceIDs: []int64{}},
			[]string{`"imported":[{"name":"tdd","fileCount":2,"totalBytes":30}]`, `"skipped":[{"path":"skills/big","reason":"too big"}]`, `"sourceRef":"main"`}},
		{"an import names the skills to take and the workspaces to turn them on in", http.MethodPost, account + "/import",
			`{"url":"https://github.com/garrytan/gstack","skills":["ship","careful"],"workspaceIds":["` + skillWS + `","` + skillTarget + `"]}`, 200,
			entity.ImportSkillsRequest{UserID: "user-1", URL: "https://github.com/garrytan/gstack", Skills: []string{"ship", "careful"}, WorkspaceIDs: []int64{4242, 4243}},
			[]string{`"candidates":[{"name":"ship","path":"ship","sizeBytes":77710,"reason":"SKILL.md is too big"},{"name":"careful","path":"careful","sizeBytes":3516}]`}},
		{"a skill is turned off for the whole account", http.MethodPatch, account + "/tdd", `{"enabled":false}`, 200,
			entity.SetSkillEnabledRequest{UserID: "user-1", Name: "tdd", Enabled: false},
			[]string{`"skill":{`, `"enabled":false`}},
		{"a skill is turned on for the whole account", http.MethodPatch, account + "/tdd", `{"enabled":true}`, 200,
			entity.SetSkillEnabledRequest{UserID: "user-1", Name: "tdd", Enabled: true},
			[]string{`"enabled":true`}},
		{"a skill is deleted from the account", http.MethodDelete, account + "/tdd", "", 204,
			entity.DeleteSkillRequest{UserID: "user-1", Name: "tdd"}, nil},
		{"a workspace lists the account's skills as it sees them", http.MethodGet, ws, "", 200,
			entity.SearchSkillsRequest{WorkspaceID: 4242, UserID: "user-1"},
			[]string{`"name":"tdd"`, `"workspaceEnabled":true`}},
		{"a workspace searches and pages the account's skills", http.MethodGet, ws + "?q=review&limit=20&offset=40", "", 200,
			entity.SearchSkillsRequest{WorkspaceID: 4242, UserID: "user-1", Query: "review", Limit: 20, Offset: 40},
			[]string{`"total":2`}},
		{"a workspace reads one skill", http.MethodGet, ws + "/tdd", "", 200,
			entity.GetSkillRequest{WorkspaceID: 4242, UserID: "user-1", Name: "tdd"},
			[]string{`"skill":{`, `"workspaceEnabled":true`}},
		{"a workspace reads a skill's file by a nested and encoded path", http.MethodGet, ws + "/tdd/files/scripts/run%20me.sh", "", 200,
			entity.GetSkillFileRequest{WorkspaceID: 4242, UserID: "user-1", Name: "tdd", Path: "scripts/run me.sh"},
			[]string{`"file":{"path":"scripts/run me.sh"`, `"content":"body"`}},
		{"a skill is turned off in one workspace", http.MethodPatch, ws + "/tdd", `{"enabled":false}`, 200,
			entity.SetSkillEnabledRequest{WorkspaceID: 4242, UserID: "user-1", Name: "tdd", Enabled: false},
			[]string{`"skill":{`, `"enabled":false`}},
		{"a skill is turned on in one workspace", http.MethodPatch, ws + "/tdd", `{"enabled":true}`, 200,
			entity.SetSkillEnabledRequest{WorkspaceID: 4242, UserID: "user-1", Name: "tdd", Enabled: true},
			[]string{`"enabled":true`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &skillCrud{}
			status, body := doSkill(t, c, tc.method, tc.path, tc.body)
			if status != tc.status {
				t.Fatalf("%s %s answered %d, want %d: %s", tc.method, tc.path, status, tc.status, body)
			}
			if !reflect.DeepEqual(c.saw, tc.want) {
				t.Errorf("the controller was asked %+v, want %+v", c.saw, tc.want)
			}
			for _, s := range tc.contains {
				if !strings.Contains(body, s) {
					t.Errorf("the body %s does not contain %s", body, s)
				}
			}
		})
	}
}

// Importing, deleting and sharing are no longer done from a workspace: an
// import or a delete goes to the account, and sharing is gone.
func TestSkillRoutes_RemovedFromWorkspace(t *testing.T) {
	ws := "/workspaces/" + skillWS + "/skills"
	for _, tc := range []struct{ name, method, path, body string }{
		{"a workspace cannot import", http.MethodPost, ws + "/import", `{"url":"u"}`},
		{"a workspace cannot delete a skill", http.MethodDelete, ws + "/tdd", ""},
		{"a skill's shares cannot be listed", http.MethodGet, ws + "/tdd/shares", ""},
		{"a skill cannot be shared", http.MethodPut, ws + "/tdd/shares/" + skillTarget, ""},
		{"a skill cannot be unshared", http.MethodDelete, ws + "/tdd/shares/" + skillTarget, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &skillCrud{}
			status, _ := doSkill(t, c, tc.method, tc.path, tc.body)
			if status != http.StatusNotFound && status != http.StatusMethodNotAllowed {
				t.Errorf("%s %s answered %d, want 404 or 405", tc.method, tc.path, status)
			}
			if c.saw != nil {
				t.Errorf("the controller was asked %+v, want no call", c.saw)
			}
		})
	}
}

// A request the path or body cannot be read from never reaches the controller.
func TestSkillRoutes_Unparseable(t *testing.T) {
	ws := "/workspaces/" + skillWS + "/skills"
	for _, tc := range []struct{ name, method, path, body string }{
		{"a workspace list with no workspace", http.MethodGet, "/workspaces/0/skills", ""},
		{"a workspace read with a bad escape in the name", http.MethodGet, ws + "/%zz", ""},
		{"an account read with a bad escape in the name", http.MethodGet, "/skills/%zz", ""},
		{"a workspace file read with no workspace", http.MethodGet, "/workspaces/0/skills/tdd/files/SKILL.md", ""},
		{"a workspace file read with a bad escape in the path", http.MethodGet, ws + "/tdd/files/%zz", ""},
		{"an account file read with a bad escape in the path", http.MethodGet, "/skills/tdd/files/%zz", ""},
		{"an import with no url", http.MethodPost, "/skills/import", `{"overwrite":true}`},
		{"an import that is not JSON", http.MethodPost, "/skills/import", `{`},
		{"an import naming a workspace that is not an id", http.MethodPost, "/skills/import", `{"url":"u","workspaceIds":["` + skillWS + `","0"]}`},
		{"an account delete with a bad escape in the name", http.MethodDelete, "/skills/%zz", ""},
		{"a workspace switch with enabled not given", http.MethodPatch, ws + "/tdd", `{}`},
		{"a workspace switch that is not JSON", http.MethodPatch, ws + "/tdd", `{`},
		{"a workspace switch with no workspace", http.MethodPatch, "/workspaces/0/skills/tdd", `{"enabled":false}`},
		{"an account switch with enabled not given", http.MethodPatch, "/skills/tdd", `{}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &skillCrud{}
			status, _ := doSkill(t, c, tc.method, tc.path, tc.body)
			if status != http.StatusUnprocessableEntity || c.saw != nil {
				t.Errorf("%s %s answered %d and the controller was asked %+v, want 422 and no call", tc.method, tc.path, status, c.saw)
			}
		})
	}
}

// A skill refusal is passed on word for word with its own status; any other
// error is not, since it may carry internals.
func TestSkillRoutes_Errors(t *testing.T) {
	ws := "/workspaces/" + skillWS + "/skills"
	routes := []struct{ method, path, body string }{
		{http.MethodGet, "/skills", ""},
		{http.MethodGet, "/skills/tdd", ""},
		{http.MethodGet, "/skills/tdd/files/SKILL.md", ""},
		{http.MethodPost, "/skills/import", `{"url":"u"}`},
		{http.MethodDelete, "/skills/tdd", ""},
		{http.MethodPatch, "/skills/tdd", `{"enabled":false}`},
		{http.MethodGet, ws, ""},
		{http.MethodGet, ws + "/tdd", ""},
		{http.MethodGet, ws + "/tdd/files/SKILL.md", ""},
		{http.MethodPatch, ws + "/tdd", `{"enabled":false}`},
	}
	for _, tc := range []struct {
		name    string
		err     error
		status  int
		message string
	}{
		{"an invalid request", &crud.SkillError{Kind: crud.SkillInvalid, Message: "name it in lowercase"}, 422, "name it in lowercase"},
		{"a name already taken", &crud.SkillError{Kind: crud.SkillConflict, Message: "already taken"}, 409, "already taken"},
		{"GitHub failing", &crud.SkillError{Kind: crud.SkillUpstream, Message: "GitHub answered 500"}, 502, "GitHub answered 500"},
		{"a skill not found", base.ErrNotFound, 404, "not found"},
		{"an internal error", errors.New("database unavailable"), 500, "internal server error"},
	} {
		for _, r := range routes {
			t.Run(tc.name+" on "+r.method+" "+r.path, func(t *testing.T) {
				status, body := doSkill(t, &skillCrud{err: tc.err}, r.method, r.path, r.body)
				var e struct {
					Error struct {
						Message string `json:"message"`
						Code    int    `json:"code"`
					} `json:"error"`
				}
				if err := json.Unmarshal([]byte(body), &e); err != nil {
					t.Fatalf("the body %q is not a JSON error: %v", body, err)
				}
				if status != tc.status || e.Error.Code != tc.status || e.Error.Message != tc.message {
					t.Errorf("answered %d with %+v, want %d with %q", status, e.Error, tc.status, tc.message)
				}
			})
		}
	}
}
