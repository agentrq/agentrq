// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/mustafaturan/monoflake"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/storage"
)

// panoCrud answers every Pano call with err, or with a fixed response, and
// records the request it was given.
type panoCrud struct {
	crud.Controller
	err error
	saw any
}

var panoNow = time.Date(2026, 10, 13, 8, 0, 0, 0, time.UTC)

func testPanoDoc() entity.PanoDoc {
	return entity.PanoDoc{
		ID: 7, CreatedAt: panoNow, UpdatedAt: panoNow, Title: "Plan", Excerpt: "# Plan", Markdown: "# Plan\n![chart](chart.png)", Version: 4,
		CreatedByWorkspaceID: 4242, TTLDays: 30, ExpiresAt: panoNow.AddDate(0, 0, 30), Color: entity.PanoColorPink,
		Files:       []entity.PanoFile{{ID: 11, Filename: "chart.png", MimeType: "image/png", SizeBytes: 3, URL: "https://l/chart.png", WorkspaceID: 4242, CreatedAt: panoNow}},
		Attachments: []entity.PanoFile{{ID: 12, Filename: "notes.txt", MimeType: "text/plain", SizeBytes: 5, CreatedAt: panoNow}},
		Comments:    []entity.PanoComment{{ID: 13, WorkspaceID: 4242, Body: "Looks right.", CreatedAt: panoNow}, {ID: 14, Body: "Agreed.", CreatedAt: panoNow}},
		Tasks:       []entity.PanoDocTask{{TaskID: 15, WorkspaceID: 4242, Status: "ongoing"}},
	}
}

func (p *panoCrud) ListPanoDocs(_ context.Context, rq entity.ListPanoDocsRequest) (*entity.ListPanoDocsResponse, error) {
	p.saw = rq
	return &entity.ListPanoDocsResponse{Docs: []entity.PanoDoc{{ID: 7, Title: "Plan", Excerpt: "# Plan", CreatedAt: panoNow, UpdatedAt: panoNow}}}, p.err
}

func (p *panoCrud) GetPanoDoc(_ context.Context, rq entity.GetPanoDocRequest) (*entity.GetPanoDocResponse, error) {
	p.saw = rq
	return &entity.GetPanoDocResponse{Doc: testPanoDoc()}, p.err
}

func (p *panoCrud) SavePanoDoc(_ context.Context, rq entity.SavePanoDocRequest) (*entity.SavePanoDocResponse, error) {
	p.saw = rq
	return &entity.SavePanoDocResponse{Doc: entity.PanoDoc{ID: 7, Title: rq.Title, Markdown: rq.Markdown, Files: []entity.PanoFile{}}}, p.err
}

func (p *panoCrud) DeletePanoDoc(_ context.Context, rq entity.DeletePanoDocRequest) error {
	p.saw = rq
	return p.err
}

func (p *panoCrud) AddPanoComment(_ context.Context, rq entity.AddPanoCommentRequest) (*entity.AddPanoCommentResponse, error) {
	p.saw = rq
	return &entity.AddPanoCommentResponse{Comment: entity.PanoComment{ID: 13, Body: rq.Body, CreatedAt: panoNow}}, p.err
}

func (p *panoCrud) AttachPanoFiles(_ context.Context, rq entity.AttachPanoFilesRequest) (*entity.AttachPanoFilesResponse, error) {
	p.saw = rq
	return &entity.AttachPanoFilesResponse{Attachments: []entity.PanoFile{{ID: 12, Filename: "notes.txt", MimeType: "text/plain", SizeBytes: 5, WorkspaceID: 4242, CreatedAt: panoNow}}}, p.err
}

func doPano(t *testing.T, c crud.Controller, method, path, body string) (int, string) {
	t.Helper()
	app := fiber.New()
	app.Use(func(ctx *fiber.Ctx) error {
		ctx.Locals("user_id", "user-1")
		return ctx.Next()
	})
	h := &handler{crud: c, router: app}
	h.registerPanoRoutes()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, r)
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

var (
	panoDoc = monoflake.ID(7).String()
	panoWS  = monoflake.ID(4242).String()
)

func TestPanoRoutes(t *testing.T) {
	data := base64.StdEncoding.EncodeToString([]byte("abc"))
	for _, tc := range []struct {
		name, method, path, body string
		status                   int
		want                     any
		contains                 []string
	}{
		{"the account's docs are listed without their markdown", http.MethodGet, "/pano", "", 200,
			entity.ListPanoDocsRequest{UserID: "user-1"},
			[]string{`{"docs":[{"id":"` + panoDoc + `","title":"Plan","excerpt":"# Plan","createdAt":"2026-10-13T08:00:00Z"`}},
		{"a doc is read with its files, attachments and comments", http.MethodGet, "/pano/" + panoDoc, "", 200,
			entity.GetPanoDocRequest{UserID: "user-1", ID: 7, IncludeComments: true, IncludeAttachments: true},
			[]string{
				`"markdown":"# Plan\n![chart](chart.png)"`,
				`"version":4`, `"ttlDays":30,"expiresAt":"2026-11-12T08:00:00Z"`, `"color":"pink"`,
				`"createdByWorkspaceId":"` + panoWS + `"`,
				`"files":[{"id":"` + monoflake.ID(11).String() + `","filename":"chart.png","mimeType":"image/png","size":3,"url":"https://l/chart.png","workspaceId":"` + panoWS + `"`,
				`"attachments":[{"id":"` + monoflake.ID(12).String() + `","filename":"notes.txt","mimeType":"text/plain","size":5,"createdAt"`,
				`"comments":[{"id":"` + monoflake.ID(13).String() + `","workspaceId":"` + panoWS + `","body":"Looks right."`,
				`{"id":"` + monoflake.ID(14).String() + `","body":"Agreed."`,
				`"tasks":[{"taskId":"` + monoflake.ID(15).String() + `","workspaceId":"` + panoWS + `","status":"ongoing"}]`,
			}},
		{"the person creates a doc with a linked file", http.MethodPost, "/pano",
			`{"title":"Plan","markdown":"![c](c.png)","files":[{"filename":"c.png","mimeType":"image/png","data":"` + data + `"}]}`, 201,
			entity.SavePanoDocRequest{UserID: "user-1", Title: "Plan", Markdown: "![c](c.png)", Files: []entity.PanoUpload{{Filename: "c.png", MimeType: "image/png", Data: data}}},
			[]string{`"doc":{"id":"` + panoDoc + `","title":"Plan"`, `"markdown":"![c](c.png)"`}},
		{"the person replaces a doc", http.MethodPut, "/pano/" + panoDoc, `{"version":3,"title":"Plan 2","markdown":"text","ttlDays":7,"color":"blue"}`, 200,
			entity.SavePanoDocRequest{UserID: "user-1", ID: 7, Version: 3, Title: "Plan 2", Markdown: "text", TTLDays: 7, Color: "blue", Files: []entity.PanoUpload{}},
			[]string{`"title":"Plan 2"`}},
		{"the person deletes a doc", http.MethodDelete, "/pano/" + panoDoc, "", 204,
			entity.DeletePanoDocRequest{UserID: "user-1", ID: 7}, nil},
		{"the person comments on a doc", http.MethodPost, "/pano/" + panoDoc + "/comments", `{"body":"Agreed."}`, 201,
			entity.AddPanoCommentRequest{UserID: "user-1", ID: 7, Body: "Agreed."},
			[]string{`{"comment":{"id":"` + monoflake.ID(13).String() + `","body":"Agreed.","createdAt":"2026-10-13T08:00:00Z"}}`}},
		{"the person attaches a file to a doc", http.MethodPost, "/pano/" + panoDoc + "/attachments", `{"files":[{"filename":"notes.txt","data":"` + data + `"}]}`, 201,
			entity.AttachPanoFilesRequest{UserID: "user-1", ID: 7, Files: []entity.PanoUpload{{Filename: "notes.txt", Data: data}}},
			[]string{`{"attachments":[{"id":"` + monoflake.ID(12).String() + `","filename":"notes.txt","mimeType":"text/plain","size":5,"workspaceId":"` + panoWS + `"`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &panoCrud{}
			status, body := doPano(t, c, tc.method, tc.path, tc.body)
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

// A path id that is not a doc's, or a body that is not JSON, never reaches
// the controller.
func TestPanoRoutes_RefuseAnInvalidRequest(t *testing.T) {
	for _, tc := range []struct{ name, method, path, body string }{
		{"a doc is read by the zero id", http.MethodGet, "/pano/00000000000", ""},
		{"a doc is created from a body that is not JSON", http.MethodPost, "/pano", "{"},
		{"a doc is replaced by the zero id", http.MethodPut, "/pano/00000000000", `{"title":"t","markdown":"m"}`},
		{"a doc is deleted by the zero id", http.MethodDelete, "/pano/00000000000", ""},
		{"a comment goes to the zero id", http.MethodPost, "/pano/00000000000/comments", `{"body":"b"}`},
		{"a comment's body is not JSON", http.MethodPost, "/pano/" + panoDoc + "/comments", "{"},
		{"an attachment goes to the zero id", http.MethodPost, "/pano/00000000000/attachments", `{"files":[]}`},
		{"an attachment's body is not JSON", http.MethodPost, "/pano/" + panoDoc + "/attachments", "{"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &panoCrud{}
			status, body := doPano(t, c, tc.method, tc.path, tc.body)
			if status != http.StatusUnprocessableEntity || !strings.Contains(body, "invalid request payload") {
				t.Errorf("%s %s answered %d %s, want 422 invalid request payload", tc.method, tc.path, status, body)
			}
			if c.saw != nil {
				t.Errorf("the controller was asked %+v, want no call", c.saw)
			}
		})
	}
}

// A refusal answers 422 with its own sentence, a missing doc 404, and any
// other failure 500.
func TestPanoRoutes_PassOnTheControllersError(t *testing.T) {
	refusal := &entity.PanoError{Message: "a Pano doc needs a title"}
	stale := &entity.PanoError{Message: "the Pano doc changed", Conflict: true}
	for _, tc := range []struct {
		name, method, path, body string
	}{
		{"listing", http.MethodGet, "/pano", ""},
		{"reading", http.MethodGet, "/pano/" + panoDoc, ""},
		{"saving", http.MethodPut, "/pano/" + panoDoc, `{"title":"","markdown":"m"}`},
		{"deleting", http.MethodDelete, "/pano/" + panoDoc, ""},
		{"commenting", http.MethodPost, "/pano/" + panoDoc + "/comments", `{"body":"b"}`},
		{"attaching", http.MethodPost, "/pano/" + panoDoc + "/attachments", `{"files":[]}`},
	} {
		t.Run(tc.name+" passes on a refusal, a missing doc and a failure", func(t *testing.T) {
			for _, e := range []struct {
				err    error
				status int
				msg    string
			}{
				{refusal, http.StatusUnprocessableEntity, "a Pano doc needs a title"},
				{stale, http.StatusConflict, "the Pano doc changed"},
				{base.ErrNotFound, http.StatusNotFound, "not found"},
				{errors.New("database unavailable"), http.StatusInternalServerError, ""},
			} {
				status, body := doPano(t, &panoCrud{err: e.err}, tc.method, tc.path, tc.body)
				if status != e.status || !strings.Contains(body, e.msg) {
					t.Errorf("for %v the route answered %d %s, want %d %q", e.err, status, body, e.status, e.msg)
				}
			}
		})
	}
}

// A Pano file is served by its public link, its type sniffed from the bytes
// so HTML downloads, and a Pano key under another kind is not found.
func TestPublicFile_ServesAPanoFile(t *testing.T) {
	dir := t.TempDir()
	pano, err := storage.NewNestedPublic(filepath.Join(dir, "pano"), "u")
	if err != nil {
		t.Fatal(err)
	}
	pngKey := "u-00000000001/pano-00000000002/" + monoflake.ID(3).String()
	htmlKey := "u-00000000001/pano-00000000002/" + monoflake.ID(4).String()
	for key, content := range map[string]string{pngKey: "\x89PNG\r\n\x1a\n image", htmlKey: "<html><script>alert(1)</script></html>"} {
		if err := pano.Save(key, base64.StdEncoding.EncodeToString([]byte(content))); err != nil {
			t.Fatal(err)
		}
	}
	app := fiber.New()
	h := &handler{crud: crud.New(crud.Params{Storage: mustFlat(t, dir), PanoStorage: pano})}
	h.registerPublicFileRoutes(app)

	status, hdr, body := get(t, app, "/storage/pano/"+pngKey)
	if status != http.StatusOK || !strings.HasSuffix(body, " image") || hdr.Get("Content-Type") != "image/png" {
		t.Errorf("the image was served as %d %q %q, want 200 image/png", status, hdr.Get("Content-Type"), body)
	}
	status, hdr, _ = get(t, app, "/storage/pano/"+htmlKey)
	if status != http.StatusOK || hdr.Get("Content-Type") != "application/octet-stream" || hdr.Get("Content-Disposition") != "attachment" {
		t.Errorf("the HTML was served as %d %q %q, want a 200 download", status, hdr.Get("Content-Type"), hdr.Get("Content-Disposition"))
	}
	for _, path := range []string{
		"/storage/skills/" + pngKey,
		"/storage/artifacts/" + pngKey,
		"/storage/pano/u-00000000001/skill-00000000002/" + monoflake.ID(3).String(),
		"/storage/pano/" + pngKey + "x",
	} {
		if status, _, body := get(t, app, path); status != http.StatusNotFound {
			t.Errorf("%s answered %d %q, want 404", path, status, body)
		}
	}
}
