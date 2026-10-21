// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package crud

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/golang/mock/gomock"
	"github.com/mustafaturan/monoflake"
	"gorm.io/gorm"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	mock_repo "github.com/agentrq/agentrq/backend/internal/service/mocks/repository"
	"github.com/agentrq/agentrq/backend/internal/service/storage"
)

// The Pano controller is tested against a real database and a real storage
// directory, like skills: what it promises is that no row points at a missing
// blob and no blob outlives its row.

const panoURLBase = "https://agentrq.example/storage/pano"

type panoEnv struct {
	c      *controller
	db     *gorm.DB
	dir    string
	ctx    context.Context
	events *[]entity.CRUDEvent
}

func newPanoEnv(t *testing.T) *panoEnv {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Workspace{}, &model.Task{}, &model.PanoDoc{}, &model.PanoFile{}, &model.PanoComment{}, &model.PanoDocTask{}); err != nil {
		t.Fatal(err)
	}
	for _, ws := range []model.Workspace{{ID: skWS, UserID: skUser, Name: "alpha"}, {ID: skForgn, UserID: skOther, Name: "theirs"}} {
		if err := db.Create(&ws).Error; err != nil {
			t.Fatal(err)
		}
	}
	dir := t.TempDir()
	store, err := storage.NewNestedPublic(dir, panoURLBase)
	if err != nil {
		t.Fatal(err)
	}
	attachments, err := storage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e := &panoEnv{
		c:   &controller{repository: base.New(&realDB{db: db}), idgen: &seqIDGen{n: 5000}, storage: attachments, panoStorage: store},
		db:  db,
		dir: dir,
		ctx: context.Background(),
	}
	e.events = withSkillEvents(t, &skillEnv{c: e.c})
	return e
}

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

func (e *panoEnv) save(t *testing.T, req entity.SavePanoDocRequest) entity.PanoDoc {
	t.Helper()
	if req.UserID == "" {
		req.UserID = skUserStr
	}
	rs, err := e.c.SavePanoDoc(e.ctx, req)
	if err != nil {
		t.Fatalf("SavePanoDoc returned %v, want the doc saved", err)
	}
	return rs.Doc
}

func (e *panoEnv) get(t *testing.T, id int64) entity.PanoDoc {
	t.Helper()
	rs, err := e.c.GetPanoDoc(e.ctx, entity.GetPanoDocRequest{UserID: skUserStr, ID: id, IncludeComments: true, IncludeAttachments: true})
	if err != nil {
		t.Fatalf("GetPanoDoc returned %v, want the doc", err)
	}
	return rs.Doc
}

// blobs is every key in the storage directory.
func (e *panoEnv) blobs(t *testing.T) []string {
	t.Helper()
	var keys []string
	err := filepath.WalkDir(e.dir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(e.dir, path)
			keys = append(keys, filepath.ToSlash(rel))
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return keys
}

func (e *panoEnv) wantEvents(t *testing.T, want ...entity.CRUDEvent) {
	t.Helper()
	if len(*e.events) != len(want) {
		t.Fatalf("published %d events, want %d: %+v", len(*e.events), len(want), *e.events)
	}
	for i, w := range want {
		w.ResourceType, w.UserID, w.Origin = entity.ResourcePano, skUser, entity.OriginAPI
		if (*e.events)[i] != w {
			t.Errorf("event %d is %+v, want %+v", i, (*e.events)[i], w)
		}
	}
	*e.events = (*e.events)[:0]
}

func fileNames(files []entity.PanoFile) []string {
	out := []string{}
	for _, f := range files {
		out = append(out, f.Filename)
	}
	return out
}

func TestPanoLinks(t *testing.T) {
	for _, tc := range []struct {
		name, markdown string
		want           []string
	}{
		{"an inline link and an image name their targets", "See [the plan](plan.md) and ![chart](chart.png).", []string{"plan.md", "chart.png"}},
		{"an inline link in angle brackets, with a title, names its target", `[a](<a.txt> "title") [b]( b.txt "t")`, []string{"a.txt", "b.txt"}},
		{"a reference definition names its target", "[ref]\n\n   [ref]: ./data.csv \"Data\"\n[x]: <x.csv>", []string{"data.csv", "x.csv"}},
		{"HTML src and href name their targets", `<img src="./pic.png" width="2"> <A HREF='doc.pdf'>`, []string{"pic.png", "doc.pdf"}},
		{"a query and a fragment are not part of the name", "![a](a.png?v=2) [b](b.md#part)", []string{"a.png", "b.md"}},
		{"a percent-encoded name is decoded", "[r](my%20report.pdf)", []string{"my report.pdf"}},
		{"a name that is not valid percent-encoding is kept as written", "[r](bad%zz.txt)", []string{"bad%zz.txt"}},
		{"a doc with no links names nothing", "# Plain\ntext only", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := panoLinks(tc.markdown)
			want := map[string]bool{}
			for _, w := range tc.want {
				want[w] = true
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("panoLinks returned %v, want %v", got, want)
			}
		})
	}
}

func TestPanoExcerpt(t *testing.T) {
	if got := panoExcerpt("  short doc \n"); got != "short doc" {
		t.Errorf("panoExcerpt of a short doc returned %q, want it trimmed and whole", got)
	}
	long := strings.Repeat("ü", panoExcerptChars-1) + " " + strings.Repeat("x", 10)
	got := panoExcerpt(long)
	if want := strings.Repeat("ü", panoExcerptChars-1) + "…"; got != want {
		t.Errorf("panoExcerpt of a long doc returned %q, want the first %d characters, trimmed, and an ellipsis", got, panoExcerptChars)
	}
}

// The person writes a doc with a linked file, and reads it back: its markdown,
// its file with a public link, its defaults, and an event for the write.
func TestPano_CreateAndRead(t *testing.T) {
	e := newPanoEnv(t)
	doc := e.save(t, entity.SavePanoDocRequest{Title: "  Plan  ", Markdown: "# Plan\n![chart](chart.png)\n", Files: []entity.PanoUpload{{Filename: "chart.png", Data: b64("\x89PNG\r\n\x1a\n")}}})
	if doc.Title != "Plan" || doc.Markdown != "# Plan\n![chart](chart.png)\n" || doc.Excerpt != "# Plan\n![chart](chart.png)" {
		t.Errorf("SavePanoDoc answered %+v, want the trimmed title, the markdown and its excerpt", doc)
	}
	if doc.TTLDays != PanoDefaultTTLDays || !doc.ExpiresAt.Equal(doc.UpdatedAt.AddDate(0, 0, PanoDefaultTTLDays)) || doc.Color != entity.PanoColorYellow {
		t.Errorf("a new doc lives %d days until %v in %v, want %d days from its save, yellow", doc.TTLDays, doc.ExpiresAt, doc.Color, PanoDefaultTTLDays)
	}
	if doc.CreatedByWorkspaceID != 0 || doc.UpdatedByWorkspaceID != 0 {
		t.Errorf("the person's doc is by workspaces %d and %d, want 0", doc.CreatedByWorkspaceID, doc.UpdatedByWorkspaceID)
	}
	e.wantEvents(t, entity.CRUDEvent{Action: entity.ActionPanoCreate, ResourceID: doc.ID, Actor: entity.ActorHuman})

	got := e.get(t, doc.ID)
	if got.Markdown != doc.Markdown || got.Title != "Plan" {
		t.Errorf("GetPanoDoc returned %+v, want the saved markdown and title", got)
	}
	if len(got.Files) != 1 || got.Files[0].Filename != "chart.png" || got.Files[0].MimeType != "image/png" || got.Files[0].SizeBytes != 8 ||
		!strings.HasPrefix(got.Files[0].URL, panoURLBase+"/u-"+skUserStr+"/pano-"+monoflake.ID(doc.ID).String()+"/") {
		t.Errorf("the doc's files are %+v, want chart.png as image/png with a public link under the doc", got.Files)
	}
	if got.Attachments == nil || len(got.Attachments) != 0 || got.Comments == nil || len(got.Comments) != 0 {
		t.Errorf("asked for, the attachments %v and comments %v should be empty lists", got.Attachments, got.Comments)
	}
	if n := len(e.blobs(t)); n != 1 {
		t.Errorf("storage holds %d blobs, want the file alone: the markdown is in the database", n)
	}

	// Not asked for, attachments and comments are left out.
	rs, err := e.c.GetPanoDoc(e.ctx, entity.GetPanoDocRequest{UserID: skUserStr, ID: doc.ID})
	if err != nil || rs.Doc.Attachments != nil || rs.Doc.Comments != nil {
		t.Errorf("GetPanoDoc without includes returned %+v, %v, want no attachments or comments", rs, err)
	}

	list, err := e.c.ListPanoDocs(e.ctx, entity.ListPanoDocsRequest{UserID: skUserStr})
	if err != nil || len(list.Docs) != 1 || list.Docs[0].ID != doc.ID || list.Docs[0].Markdown != "" {
		t.Errorf("ListPanoDocs returned %+v, %v, want the one doc without its markdown", list, err)
	}
	// Reads publish nothing.
	e.wantEvents(t)
}

// An agent's update replaces a file sent under the same name, drops a linked
// file the markdown stops naming, keeps attachments, and leaves no old blob.
func TestPano_UpdateByAnAgent(t *testing.T) {
	e := newPanoEnv(t)
	doc := e.save(t, entity.SavePanoDocRequest{WorkspaceID: skWS, Title: "Plan", Markdown: "![a](a.png) [b](b.txt)", TTLDays: 7, Color: "blue", Files: []entity.PanoUpload{
		{Filename: "a.png", Data: b64("old a")}, {Filename: "b.txt", Data: b64("b")},
	}})
	if _, err := e.c.AttachPanoFiles(e.ctx, entity.AttachPanoFilesRequest{UserID: skUserStr, WorkspaceID: skWS, ID: doc.ID, Files: []entity.PanoUpload{{Filename: "notes.md", Data: b64("n")}}}); err != nil {
		t.Fatalf("AttachPanoFiles returned %v, want the attachment added", err)
	}
	e.wantEvents(t,
		entity.CRUDEvent{Action: entity.ActionPanoCreate, WorkspaceID: skWS, ResourceID: doc.ID, Actor: entity.ActorAgent},
		entity.CRUDEvent{Action: entity.ActionPanoAttach, WorkspaceID: skWS, ResourceID: doc.ID, Actor: entity.ActorAgent})

	// b.txt, still linked and not sent, stays.
	kept := e.save(t, entity.SavePanoDocRequest{WorkspaceID: skWS, ID: doc.ID, Version: doc.Version, Title: "Plan", Markdown: "![a](a.png) [b](b.txt) more"})
	if !reflect.DeepEqual(fileNames(kept.Files), []string{"a.png", "b.txt"}) {
		t.Errorf("the update answered with files %v, want the kept a.png and b.txt", fileNames(kept.Files))
	}
	if got := e.get(t, doc.ID); !reflect.DeepEqual(fileNames(got.Files), []string{"a.png", "b.txt"}) || kept.Excerpt != "![a](a.png) [b](b.txt) more" {
		t.Errorf("after an update that sent no files, the linked files are %v, want a.png and b.txt kept", fileNames(got.Files))
	}
	*e.events = (*e.events)[:0]

	updated := e.save(t, entity.SavePanoDocRequest{WorkspaceID: skWS, ID: doc.ID, Version: kept.Version, Title: "Plan 2", Markdown: "![a](./a.png)", Files: []entity.PanoUpload{{Filename: "a.png", MimeType: "image/png", Data: b64("new a")}}})
	if !reflect.DeepEqual(fileNames(updated.Files), []string{"a.png"}) || updated.Files[0].SizeBytes != 5 {
		t.Errorf("the update answered with files %+v, want only the new a.png", updated.Files)
	}
	if updated.TTLDays != 7 || updated.Color != entity.PanoColorBlue {
		t.Errorf("an update naming no TTL or color has %d days and %v, want the doc's own 7 and blue", updated.TTLDays, updated.Color)
	}
	if updated.CreatedByWorkspaceID != skWS || updated.UpdatedByWorkspaceID != skWS {
		t.Errorf("the doc is by workspaces %d and %d, want %d", updated.CreatedByWorkspaceID, updated.UpdatedByWorkspaceID, skWS)
	}
	e.wantEvents(t, entity.CRUDEvent{Action: entity.ActionPanoUpdate, WorkspaceID: skWS, ResourceID: doc.ID, Actor: entity.ActorAgent})

	got := e.get(t, doc.ID)
	if !reflect.DeepEqual(fileNames(got.Files), []string{"a.png"}) || got.Files[0].MimeType != "image/png" || got.Files[0].SizeBytes != 5 {
		t.Errorf("the linked files are %+v, want only the new a.png", got.Files)
	}
	if !reflect.DeepEqual(fileNames(got.Attachments), []string{"notes.md"}) {
		t.Errorf("the attachments are %v, want notes.md kept", fileNames(got.Attachments))
	}
	if got.Markdown != "![a](./a.png)" || got.Title != "Plan 2" {
		t.Errorf("GetPanoDoc returned %q %q, want the new title and markdown", got.Title, got.Markdown)
	}
	if n := len(e.blobs(t)); n != 2 {
		t.Errorf("storage holds %d blobs %v, want the new a.png and notes.md", n, e.blobs(t))
	}

	// A named TTL and color replace the doc's own.
	again := e.save(t, entity.SavePanoDocRequest{ID: doc.ID, Version: updated.Version, Title: "Plan", Markdown: "text", TTLDays: 90, Color: "yellow"})
	if again.TTLDays != 90 || again.Color != entity.PanoColorYellow || again.UpdatedByWorkspaceID != 0 {
		t.Errorf("an update naming 90 days and yellow by the person answered %+v", again)
	}
	e.wantEvents(t, entity.CRUDEvent{Action: entity.ActionPanoUpdate, ResourceID: doc.ID, Actor: entity.ActorHuman})
}

// A new doc lives no longer than the configured maximum, even by default.
func TestPano_TTLIsCappedByTheConfiguredMaximum(t *testing.T) {
	old := PanoMaxTTLDays
	defer func() { PanoMaxTTLDays = old }()
	PanoMaxTTLDays = 10
	e := newPanoEnv(t)
	if doc := e.save(t, entity.SavePanoDocRequest{Title: "t", Markdown: "m"}); doc.TTLDays != 10 {
		t.Errorf("a new doc under a 10-day maximum lives %d days, want 10", doc.TTLDays)
	}
	if _, err := e.c.SavePanoDoc(e.ctx, entity.SavePanoDocRequest{UserID: skUserStr, Title: "t", Markdown: "m", TTLDays: 11}); !strings.Contains(fmt.Sprint(err), "ttlDays must be between 1 and 10") {
		t.Errorf("SavePanoDoc with 11 days returned %v, want a refusal naming the 10-day maximum", err)
	}
}

func wantPanoRefusal(t *testing.T, err error, contains string) {
	t.Helper()
	var pe *entity.PanoError
	if !errors.As(err, &pe) || !strings.Contains(pe.Message, contains) {
		t.Errorf("the call returned %v, want a refusal containing %q", err, contains)
	}
}

func TestPano_SaveRefusals(t *testing.T) {
	big := b64(strings.Repeat("x", PanoMaxFileBytes+1))
	long := strings.Repeat("a", panoMaxFilenameBytes+1)
	for _, tc := range []struct {
		name string
		req  entity.SavePanoDocRequest
		want string
	}{
		{"a doc with no title is refused", entity.SavePanoDocRequest{Title: "  ", Markdown: "m"}, "needs a title"},
		{"a title over the limit is refused", entity.SavePanoDocRequest{Title: strings.Repeat("é", PanoMaxTitleChars+1), Markdown: "m"}, "longer than 200 characters"},
		{"a doc with no markdown is refused", entity.SavePanoDocRequest{Title: "t", Markdown: " \n"}, "needs markdown"},
		{"markdown over the limit is refused", entity.SavePanoDocRequest{Title: "t", Markdown: strings.Repeat("x", PanoMaxMarkdownBytes+1)}, "over the 262144 byte limit"},
		{"markdown that is not UTF-8 is refused", entity.SavePanoDocRequest{Title: "t", Markdown: "bad \xff"}, "not valid UTF-8"},
		{"a negative TTL is refused", entity.SavePanoDocRequest{Title: "t", Markdown: "m", TTLDays: -1}, "ttlDays must be between 1 and"},
		{"a TTL over the maximum is refused", entity.SavePanoDocRequest{Title: "t", Markdown: "m", TTLDays: PanoMaxTTLDays + 1}, "ttlDays must be between 1 and"},
		{"an unknown color is refused, naming the colors", entity.SavePanoDocRequest{Title: "t", Markdown: "m", Color: "red"}, "color must be one of yellow, pink, green, blue, orange, purple, amber"},
		{"a file with no name is refused", entity.SavePanoDocRequest{Title: "t", Markdown: "m", Files: []entity.PanoUpload{{Filename: " "}}}, "every file needs a filename"},
		{"a linked file whose name a link cannot name is refused", entity.SavePanoDocRequest{Title: "t", Markdown: "[a](a b.png)", Files: []entity.PanoUpload{{Filename: "a b.png"}}}, `"a b.png" cannot be linked`},
		{"a file that is not base64 is refused", entity.SavePanoDocRequest{Title: "t", Markdown: "[a](a.png)", Files: []entity.PanoUpload{{Filename: "a.png", Data: "!!"}}}, "a.png: data is not base64"},
		{"a file over the limit is refused", entity.SavePanoDocRequest{Title: "t", Markdown: "[a](a.png)", Files: []entity.PanoUpload{{Filename: "a.png", Data: big}}}, "over the 2097152 byte limit"},
		{"a file sent twice is refused", entity.SavePanoDocRequest{Title: "t", Markdown: "[a](a.png)", Files: []entity.PanoUpload{{Filename: "a.png"}, {Filename: "a.png"}}}, "a.png is sent twice"},
		{"a file the markdown does not link is refused, saying how to link it", entity.SavePanoDocRequest{Title: "t", Markdown: "no links", Files: []entity.PanoUpload{{Filename: "chart.png"}}}, "chart.png is not linked from the markdown: link it, as [chart.png](chart.png)"},
		{"an attachment-only name over the byte limit is refused as a linked file", entity.SavePanoDocRequest{Title: "t", Markdown: "m", Files: []entity.PanoUpload{{Filename: long}}}, "cannot be linked"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newPanoEnv(t)
			tc.req.UserID = skUserStr
			_, err := e.c.SavePanoDoc(e.ctx, tc.req)
			wantPanoRefusal(t, err, tc.want)
			if n := len(e.blobs(t)); n != 0 {
				t.Errorf("a refused save left %d blobs, want none", n)
			}
			e.wantEvents(t)
		})
	}
}

func TestPano_FileLimit(t *testing.T) {
	e := newPanoEnv(t)
	var files []entity.PanoUpload
	var md strings.Builder
	for i := range PanoMaxFiles + 1 {
		name := fmt.Sprintf("f%d.txt", i)
		files = append(files, entity.PanoUpload{Filename: name, Data: b64("x")})
		md.WriteString("[" + name + "](" + name + ")\n")
	}
	_, err := e.c.SavePanoDoc(e.ctx, entity.SavePanoDocRequest{UserID: skUserStr, Title: "t", Markdown: md.String(), Files: files})
	wantPanoRefusal(t, err, "holds at most 100 files")

	// Full, a doc takes no attachment.
	doc := e.save(t, entity.SavePanoDocRequest{Title: "t", Markdown: md.String(), Files: files[:PanoMaxFiles]})
	_, err = e.c.AttachPanoFiles(e.ctx, entity.AttachPanoFilesRequest{UserID: skUserStr, ID: doc.ID, Files: []entity.PanoUpload{{Filename: "one.more", Data: b64("x")}}})
	wantPanoRefusal(t, err, "holds at most 100 files")
	if n := len(e.blobs(t)); n != PanoMaxFiles {
		t.Errorf("storage holds %d blobs, want the %d files", n, PanoMaxFiles)
	}
}

func TestPano_CommentsAndAttachments(t *testing.T) {
	e := newPanoEnv(t)
	doc := e.save(t, entity.SavePanoDocRequest{Title: "t", Markdown: "m"})
	*e.events = (*e.events)[:0]

	rs, err := e.c.AddPanoComment(e.ctx, entity.AddPanoCommentRequest{UserID: skUserStr, WorkspaceID: skWS, ID: doc.ID, Body: "  Agreed. "})
	if err != nil || rs.Comment.Body != "Agreed." || rs.Comment.WorkspaceID != skWS || rs.Comment.ID == 0 {
		t.Fatalf("AddPanoComment returned %+v, %v, want the trimmed comment by the workspace", rs, err)
	}
	e.wantEvents(t, entity.CRUDEvent{Action: entity.ActionPanoComment, WorkspaceID: skWS, ResourceID: doc.ID, Actor: entity.ActorAgent})

	att, err := e.c.AttachPanoFiles(e.ctx, entity.AttachPanoFilesRequest{UserID: skUserStr, ID: doc.ID, Files: []entity.PanoUpload{
		{Filename: "report final.pdf", Data: b64("%PDF-1.4")},
		{Filename: "noext", Data: b64("plain words")},
		{Filename: "given.bin", MimeType: " text/csv ", Data: b64("a,b")},
	}})
	if err != nil {
		t.Fatalf("AttachPanoFiles returned %v, want the files attached", err)
	}
	var types []string
	for _, f := range att.Attachments {
		types = append(types, f.MimeType)
	}
	if want := []string{"application/pdf", "text/plain; charset=utf-8", "text/csv"}; !reflect.DeepEqual(types, want) {
		t.Errorf("the attachments' types are %v, want %v: by extension, sniffed, and as given", types, want)
	}
	e.wantEvents(t, entity.CRUDEvent{Action: entity.ActionPanoAttach, ResourceID: doc.ID, Actor: entity.ActorHuman})

	got := e.get(t, doc.ID)
	if len(got.Comments) != 1 || got.Comments[0].Body != "Agreed." || len(got.Attachments) != 3 || len(got.Files) != 0 {
		t.Errorf("GetPanoDoc returned comments %+v and attachments %+v, want the comment and three attachments", got.Comments, got.Attachments)
	}
}

func TestPano_CommentAndAttachRefusals(t *testing.T) {
	e := newPanoEnv(t)
	doc := e.save(t, entity.SavePanoDocRequest{Title: "t", Markdown: "m"})
	*e.events = (*e.events)[:0]
	comment := func(body string) error {
		_, err := e.c.AddPanoComment(e.ctx, entity.AddPanoCommentRequest{UserID: skUserStr, ID: doc.ID, Body: body})
		return err
	}
	attach := func(files ...entity.PanoUpload) error {
		_, err := e.c.AttachPanoFiles(e.ctx, entity.AttachPanoFilesRequest{UserID: skUserStr, ID: doc.ID, Files: files})
		return err
	}
	wantPanoRefusal(t, comment(" "), "a comment needs text")
	wantPanoRefusal(t, comment(strings.Repeat("x", PanoMaxCommentBytes+1)), "over the 16384 byte limit")
	wantPanoRefusal(t, comment("bad \xff"), "not valid UTF-8")
	wantPanoRefusal(t, attach(), "attach needs at least one file")
	wantPanoRefusal(t, attach(entity.PanoUpload{Filename: strings.Repeat("a", panoMaxFilenameBytes+1)}), "longer than 255 bytes")
	// An attachment's name need not be one a link can name.
	if err := attach(entity.PanoUpload{Filename: "my notes (v2).txt", Data: b64("n")}); err != nil {
		t.Errorf("attaching a file with spaces in its name returned %v, want it attached", err)
	}
	*e.events = (*e.events)[:0]

	// A doc that is not there takes neither.
	if _, err := e.c.AddPanoComment(e.ctx, entity.AddPanoCommentRequest{UserID: skUserStr, ID: 999, Body: "b"}); !errors.Is(err, base.ErrNotFound) {
		t.Errorf("commenting on a missing doc returned %v, want ErrNotFound", err)
	}
	if _, err := e.c.AttachPanoFiles(e.ctx, entity.AttachPanoFilesRequest{UserID: skUserStr, ID: 999, Files: []entity.PanoUpload{{Filename: "a"}}}); !errors.Is(err, base.ErrNotFound) {
		t.Errorf("attaching to a missing doc returned %v, want ErrNotFound", err)
	}
	e.wantEvents(t)
}

func TestPano_Delete(t *testing.T) {
	e := newPanoEnv(t)
	doc := e.save(t, entity.SavePanoDocRequest{Title: "t", Markdown: "![a](a.png)", Files: []entity.PanoUpload{{Filename: "a.png", Data: b64("a")}}})
	*e.events = (*e.events)[:0]
	if err := e.c.DeletePanoDoc(e.ctx, entity.DeletePanoDocRequest{UserID: skUserStr, WorkspaceID: skWS, ID: doc.ID}); err != nil {
		t.Fatalf("DeletePanoDoc returned %v, want the doc deleted", err)
	}
	if n := len(e.blobs(t)); n != 0 {
		t.Errorf("a deleted doc left %d blobs, want none", n)
	}
	e.wantEvents(t, entity.CRUDEvent{Action: entity.ActionPanoDelete, WorkspaceID: skWS, ResourceID: doc.ID, Actor: entity.ActorAgent})
	if err := e.c.DeletePanoDoc(e.ctx, entity.DeletePanoDocRequest{UserID: skUserStr, ID: doc.ID}); !errors.Is(err, base.ErrNotFound) {
		t.Errorf("deleting the doc again returned %v, want ErrNotFound", err)
	}
	if _, err := e.c.GetPanoDoc(e.ctx, entity.GetPanoDocRequest{UserID: skUserStr, ID: doc.ID}); !errors.Is(err, base.ErrNotFound) {
		t.Errorf("reading the deleted doc returned %v, want ErrNotFound", err)
	}
	if _, err := e.c.SavePanoDoc(e.ctx, entity.SavePanoDocRequest{UserID: skUserStr, ID: doc.ID, Version: 1, Title: "t", Markdown: "m"}); !errors.Is(err, base.ErrNotFound) {
		t.Errorf("updating the deleted doc returned %v, want ErrNotFound", err)
	}
}

// Only the account's own workspaces write its Pano, and only its own docs
// are reached.
func TestPano_AccountIsolation(t *testing.T) {
	e := newPanoEnv(t)
	doc := e.save(t, entity.SavePanoDocRequest{Title: "t", Markdown: "m"})
	*e.events = (*e.events)[:0]
	otherStr := monoflake.ID(skOther).String()

	calls := map[string]func(user string, ws int64) error{
		"save": func(user string, ws int64) error {
			_, err := e.c.SavePanoDoc(e.ctx, entity.SavePanoDocRequest{UserID: user, WorkspaceID: ws, Title: "t", Markdown: "m"})
			return err
		},
		"read": func(user string, ws int64) error {
			_, err := e.c.GetPanoDoc(e.ctx, entity.GetPanoDocRequest{UserID: user, WorkspaceID: ws, ID: doc.ID})
			return err
		},
		"delete": func(user string, ws int64) error {
			return e.c.DeletePanoDoc(e.ctx, entity.DeletePanoDocRequest{UserID: user, WorkspaceID: ws, ID: doc.ID})
		},
		"comment": func(user string, ws int64) error {
			_, err := e.c.AddPanoComment(e.ctx, entity.AddPanoCommentRequest{UserID: user, WorkspaceID: ws, ID: doc.ID, Body: "b"})
			return err
		},
		"attach": func(user string, ws int64) error {
			_, err := e.c.AttachPanoFiles(e.ctx, entity.AttachPanoFilesRequest{UserID: user, WorkspaceID: ws, ID: doc.ID, Files: []entity.PanoUpload{{Filename: "a"}}})
			return err
		},
	}
	for name, call := range calls {
		t.Run(name+" from another account's workspace is not found", func(t *testing.T) {
			if err := call(skUserStr, skForgn); !errors.Is(err, base.ErrNotFound) {
				t.Errorf("%s as workspace %d returned %v, want ErrNotFound", name, skForgn, err)
			}
		})
		t.Run(name+" with no user is an invalid request", func(t *testing.T) {
			if err := call("", 0); err == nil || err.Error() != "invalid request" {
				t.Errorf("%s with no user returned %v, want an invalid request", name, err)
			}
		})
	}
	if _, err := e.c.ListPanoDocs(e.ctx, entity.ListPanoDocsRequest{}); err == nil || err.Error() != "invalid request" {
		t.Errorf("ListPanoDocs with no user returned %v, want an invalid request", err)
	}
	// Another account's person does not reach the doc either.
	if err := calls["read"](otherStr, 0); !errors.Is(err, base.ErrNotFound) {
		t.Errorf("another account reading the doc returned %v, want ErrNotFound", err)
	}
	if list, err := e.c.ListPanoDocs(e.ctx, entity.ListPanoDocsRequest{UserID: otherStr}); err != nil || len(list.Docs) != 0 {
		t.Errorf("another account's list returned %+v, %v, want no docs", list, err)
	}
	e.wantEvents(t)
}

// panoFailingStore fails every Save after failAfter of them.
type panoFailingStore struct {
	storage.Service
	failAfter, saves int
}

func (f *panoFailingStore) Save(id, data string) error {
	f.saves++
	if f.saves > f.failAfter {
		return errors.New("S3 unavailable")
	}
	return f.Service.Save(id, data)
}

// A storage failure part-way through a write leaves no blob behind and no row
// pointing at a missing one.
func TestPano_StorageFailures(t *testing.T) {
	files := []entity.PanoUpload{{Filename: "a.png", Data: b64("a")}, {Filename: "b.png", Data: b64("b")}}
	for _, tc := range []struct {
		name      string
		failAfter int
		call      func(e *panoEnv, docID int64) error
	}{
		{"a markdown blob that cannot be saved fails the save", 0, func(e *panoEnv, _ int64) error {
			_, err := e.c.SavePanoDoc(e.ctx, entity.SavePanoDocRequest{UserID: skUserStr, Title: "t", Markdown: "![a](a.png) ![b](b.png)", Files: files})
			return err
		}},
		{"a linked file that cannot be saved fails the save", 1, func(e *panoEnv, _ int64) error {
			_, err := e.c.SavePanoDoc(e.ctx, entity.SavePanoDocRequest{UserID: skUserStr, Title: "t", Markdown: "![a](a.png) ![b](b.png)", Files: files})
			return err
		}},
		{"an attachment that cannot be saved fails the attach", 1, func(e *panoEnv, id int64) error {
			_, err := e.c.AttachPanoFiles(e.ctx, entity.AttachPanoFilesRequest{UserID: skUserStr, ID: id, Files: files})
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newPanoEnv(t)
			doc := e.save(t, entity.SavePanoDocRequest{Title: "t", Markdown: "m"})
			before := len(e.blobs(t))
			e.c.panoStorage = &panoFailingStore{Service: e.c.panoStorage, failAfter: tc.failAfter}
			if err := tc.call(e, doc.ID); err == nil || !strings.Contains(err.Error(), "S3 unavailable") {
				t.Fatalf("the call returned %v, want the storage failure", err)
			}
			if n := len(e.blobs(t)); n != before {
				t.Errorf("storage went from %d to %d blobs across a failed call", before, n)
			}
			var docs, rows int64
			e.db.Model(&model.PanoDoc{}).Count(&docs)
			e.db.Model(&model.PanoFile{}).Count(&rows)
			if docs != 1 || rows != 0 {
				t.Errorf("a failed call left %d docs and %d files, want only the first doc", docs, rows)
			}
		})
	}

}

// A database failure anywhere is passed on, and leaves storage as it was.
func TestPano_DatabaseFailures(t *testing.T) {
	type call func(e *panoEnv, docID int64) error
	list := func(e *panoEnv, _ int64) error {
		_, err := e.c.ListPanoDocs(e.ctx, entity.ListPanoDocsRequest{UserID: skUserStr})
		return err
	}
	get := func(e *panoEnv, id int64) error {
		_, err := e.c.GetPanoDoc(e.ctx, entity.GetPanoDocRequest{UserID: skUserStr, ID: id, IncludeComments: true})
		return err
	}
	update := func(e *panoEnv, id int64) error {
		_, err := e.c.SavePanoDoc(e.ctx, entity.SavePanoDocRequest{UserID: skUserStr, ID: id, Title: "t", Markdown: "![b](b.png)", Files: []entity.PanoUpload{{Filename: "b.png", Data: b64("b")}}})
		return err
	}
	create := func(e *panoEnv, _ int64) error {
		_, err := e.c.SavePanoDoc(e.ctx, entity.SavePanoDocRequest{UserID: skUserStr, WorkspaceID: skWS, Title: "t", Markdown: "m"})
		return err
	}
	del := func(e *panoEnv, id int64) error {
		return e.c.DeletePanoDoc(e.ctx, entity.DeletePanoDocRequest{UserID: skUserStr, ID: id})
	}
	comment := func(e *panoEnv, id int64) error {
		_, err := e.c.AddPanoComment(e.ctx, entity.AddPanoCommentRequest{UserID: skUserStr, ID: id, Body: "b"})
		return err
	}
	attach := func(e *panoEnv, id int64) error {
		_, err := e.c.AttachPanoFiles(e.ctx, entity.AttachPanoFilesRequest{UserID: skUserStr, ID: id, Files: []entity.PanoUpload{{Filename: "c.txt", Data: b64("c")}}})
		return err
	}
	for _, tc := range []struct {
		name      string
		op, table string
		call      call
	}{
		{"list", "query", "pano_docs", list},
		{"list: their tasks", "row", "pano_doc_tasks", list},
		{"read: the doc", "query", "pano_docs", get},
		{"read: its files", "query", "pano_files", get},
		{"read: its comments", "query", "pano_comments", get},
		{"read: its tasks", "row", "pano_doc_tasks", get},
		{"update: the doc", "query", "pano_docs", update},
		{"update: its files", "query", "pano_files", update},
		{"update: the write", "update", "pano_docs", update},
		{"create: the access check", "query", "workspaces", create},
		{"create: the write", "create", "pano_docs", create},
		{"delete", "delete", "pano_files", del},
		{"comment", "create", "pano_comments", comment},
		{"attach: the doc", "query", "pano_docs", attach},
		{"attach: its files", "query", "pano_files", attach},
		{"attach: the write", "create", "pano_files", attach},
	} {
		t.Run(tc.name+" passes on the failure", func(t *testing.T) {
			e := newPanoEnv(t)
			doc := e.save(t, entity.SavePanoDocRequest{Title: "t", Markdown: "![a](a.png)", Files: []entity.PanoUpload{{Filename: "a.png", Data: b64("a")}}})
			before := e.blobs(t)
			*e.events = (*e.events)[:0]
			failOn(e.db, tc.op, tc.table)
			if err := tc.call(e, doc.ID); err == nil {
				t.Fatal("the call returned no error, want the injected failure")
			}
			if after := e.blobs(t); !reflect.DeepEqual(after, before) {
				t.Errorf("storage went from %v to %v across a failed call", before, after)
			}
			e.wantEvents(t)
		})
	}
}

func TestPano_DeleteExpired(t *testing.T) {
	t.Run("every expired doc of any account goes, a batch at a time, and a live one stays", func(t *testing.T) {
		e := newPanoEnv(t)
		live := e.save(t, entity.SavePanoDocRequest{Title: "live", Markdown: "m"})
		past := time.Now().UTC().Add(-time.Hour)
		for i := range panoSweepBatch + 1 {
			user, uStr := skUser, skUserStr
			if i%2 == 1 {
				user, uStr = skOther, monoflake.ID(skOther).String()
			}
			doc := e.save(t, entity.SavePanoDocRequest{UserID: uStr, Title: "t", Markdown: "![a](a.png)", Files: []entity.PanoUpload{{Filename: "a.png", Data: b64("a")}}})
			if err := e.db.Model(&model.PanoDoc{}).Where("id = ? AND user_id = ?", doc.ID, user).Update("expires_at", past).Error; err != nil {
				t.Fatal(err)
			}
		}
		n, err := e.c.DeleteExpiredPanoDocs(e.ctx)
		if err != nil || n != panoSweepBatch+1 {
			t.Fatalf("DeleteExpiredPanoDocs returned %d, %v, want all %d expired docs deleted", n, err, panoSweepBatch+1)
		}
		if blobs := e.blobs(t); len(blobs) != 0 {
			t.Errorf("storage holds %d blobs, want none: only the expired docs had files", len(blobs))
		}
		e.get(t, live.ID)
		if n, err := e.c.DeleteExpiredPanoDocs(e.ctx); err != nil || n != 0 {
			t.Errorf("a second sweep returned %d, %v, want nothing to delete", n, err)
		}
	})

	t.Run("a failure to list or delete is passed on with what was deleted so far", func(t *testing.T) {
		for _, tc := range []struct{ op, table string }{{"query", "pano_docs"}, {"delete", "pano_files"}} {
			e := newPanoEnv(t)
			doc := e.save(t, entity.SavePanoDocRequest{Title: "t", Markdown: "m"})
			e.db.Model(&model.PanoDoc{}).Where("id = ?", doc.ID).Update("expires_at", time.Now().UTC().Add(-time.Hour))
			failOn(e.db, tc.op, tc.table)
			if n, err := e.c.DeleteExpiredPanoDocs(e.ctx); err == nil || n != 0 {
				t.Errorf("with a %s failure on %s DeleteExpiredPanoDocs returned %d, %v, want the failure and none deleted", tc.op, tc.table, n, err)
			}
		}
	})

	t.Run("a doc another instance deleted first is skipped", func(t *testing.T) {
		repo := mock_repo.NewMockRepository(gomock.NewController(t))
		e := newPanoEnv(t)
		e.c.repository = repo
		repo.EXPECT().ListExpiredPanoDocs(gomock.Any(), gomock.Any(), panoSweepBatch).Return([]model.PanoDoc{{ID: 1, UserID: skUser}, {ID: 2, UserID: skOther}}, nil)
		repo.EXPECT().DeletePanoDoc(gomock.Any(), skUser, int64(1)).Return(nil, base.ErrNotFound)
		repo.EXPECT().DeletePanoDoc(gomock.Any(), skOther, int64(2)).Return([]string{"", "u-x/pano-y/z"}, nil)
		if n, err := e.c.DeleteExpiredPanoDocs(e.ctx); err != nil || n != 1 {
			t.Errorf("DeleteExpiredPanoDocs returned %d, %v, want the one doc still there deleted", n, err)
		}
	})
}

// A Pano file's public link reads from the Pano storage, its type sniffed.
func TestGetPublicFile_Pano(t *testing.T) {
	e := newPanoEnv(t)
	doc := e.save(t, entity.SavePanoDocRequest{Title: "t", Markdown: "![a](a.png)", Files: []entity.PanoUpload{{Filename: "a.png", Data: b64("\x89PNG\r\n\x1a\n")}}})
	got := e.get(t, doc.ID)
	key := strings.TrimPrefix(got.Files[0].URL, panoURLBase+"/")
	rs, err := e.c.GetPublicFile(e.ctx, entity.GetPublicFileRequest{Kind: entity.PublicFilePano, Key: key})
	if err != nil || rs.ContentType != "image/png" || len(rs.Data) != 8 {
		t.Fatalf("GetPublicFile returned %+v, %v, want the PNG", rs, err)
	}
	if _, err := e.c.GetPublicFile(e.ctx, entity.GetPublicFileRequest{Kind: entity.PublicFileSkills, Key: key}); !errors.Is(err, base.ErrNotFound) {
		t.Errorf("a Pano key read as a skill file returned %v, want ErrNotFound", err)
	}
}

func TestNew_PanoStorage(t *testing.T) {
	local, _ := storage.New(t.TempDir())
	pano, _ := storage.New(t.TempDir())
	if c := New(Params{Storage: local}).(*controller); c.panoStorage != local {
		t.Error("with no PanoStorage, the Pano uses Storage, want it to")
	}
	if c := New(Params{Storage: local, PanoStorage: pano}).(*controller); c.panoStorage != pano || c.storage != local {
		t.Error("PanoStorage was not used for the Pano alone")
	}
}

func TestPanoChecklist(t *testing.T) {
	for _, tc := range []struct {
		name        string
		markdown    string
		done, total int
	}{
		{"none", "# Plan\n\n- a plain item", 0, 0},
		{"open and checked", "- [ ] a\n- [x] b\n* [X] c\n+ [ ] d", 2, 4},
		{"numbered and nested", "1. [x] a\n2) [ ] b\n    - [x] nested", 2, 3},
		{"an empty item", "- [ ]\n- [x]", 1, 2},
		{"not a checkbox", "- [y] a\n- [ ]a\n[ ] no marker\n-[ ] no space", 0, 0},
		{"fenced code is text", "```md\n- [x] in code\n```\n~~~\n- [ ] also\n~~~\n- [ ] real", 0, 1},
		{"an unclosed fence hides the rest", "```\n- [x] a", 0, 0},
	} {
		if done, total := panoChecklist(tc.markdown); done != tc.done || total != tc.total {
			t.Errorf("%s: panoChecklist = %d of %d, want %d of %d", tc.name, done, total, tc.done, tc.total)
		}
	}
}

// A save counts the doc's checklist, for the board's progress.
func TestPano_SaveCountsTheChecklist(t *testing.T) {
	e := newPanoEnv(t)
	doc := e.save(t, entity.SavePanoDocRequest{Title: "Launch", Markdown: "- [x] beta\n- [ ] docs\n- [ ] demo"})
	if doc.ChecklistDone != 1 || doc.ChecklistTotal != 3 {
		t.Errorf("a new doc's checklist is %d of %d, want 1 of 3", doc.ChecklistDone, doc.ChecklistTotal)
	}
	e.save(t, entity.SavePanoDocRequest{ID: doc.ID, Version: doc.Version, Title: "Launch", Markdown: "- [x] beta\n- [x] docs"})
	list, err := e.c.ListPanoDocs(e.ctx, entity.ListPanoDocsRequest{UserID: skUserStr})
	if err != nil || len(list.Docs) != 1 || list.Docs[0].ChecklistDone != 2 || list.Docs[0].ChecklistTotal != 2 {
		t.Errorf("after an update the list has %+v, %v, want the checklist 2 of 2", list, err)
	}
}

func TestPanoOverlay(t *testing.T) {
	const ws, task, other = int64(11), int64(21), int64(22)
	path := panoTaskPath(ws, task)
	tasks := func(status string) []base.PanoTaskStatus {
		return []base.PanoTaskStatus{{TaskID: task, WorkspaceID: ws, Status: status}}
	}
	for _, tc := range []struct {
		name, markdown, status, want string
	}{
		{"an assignee takes the status", "- [ ] ship → [@api](" + path + ")", "ongoing", "- [ ] ship → [@api](" + path + ` "ongoing")`},
		{"completed ticks the box", "- [ ] ship → [@api](" + path + ` "ongoing")`, "completed", "- [x] ship → [@api](" + path + ` "completed")`},
		{"done ticks it too", "* [ ] ship → [@api](" + path + ")", "done", "* [x] ship → [@api](" + path + ` "done")`},
		{"reopened unticks it", "1. [X] ship → [@api](" + path + ` "completed")`, "blocked", "1. [ ] ship → [@api](" + path + ` "blocked")`},
		{"every item of one task follows it", "- [ ] a → [@api](" + path + ")\n- [ ] b → [@api](" + path + ` "notstarted")`, "completed",
			"- [x] a → [@api](" + path + ` "completed")` + "\n- [x] b → [@api](" + path + ` "completed")`},
		{"an unassigned item is the writer's own", "- [x] mine\n- [ ] ship → [@api](" + path + ")", "ongoing", "- [x] mine\n- [ ] ship → [@api](" + path + ` "ongoing")`},
		{"another task's item loses its assignee", "- [x] ship → [@api](" + panoTaskPath(ws, other) + ` "notstarted")`, "completed", "- [x] ship"},
		{"the task under another workspace's path loses it too", "- [ ] ship → [@api](" + panoTaskPath(99, task) + ")  ", "completed", "- [ ] ship"},
		{"a plain link to another task is left", "- [ ] read [it](" + panoTaskPath(ws, other) + ")", "completed", "- [ ] read [it](" + panoTaskPath(ws, other) + ")"},
		{"a link that is not an item is left", "see [@api](" + path + ")", "completed", "see [@api](" + path + ")"},
		{"fenced code is text", "```\n- [ ] ship → [@api](" + path + ")\n```\n~~~\n~~~", "completed", "```\n- [ ] ship → [@api](" + path + ")\n```\n~~~\n~~~"},
	} {
		if got := panoOverlay(tc.markdown, tasks(tc.status)); got != tc.want {
			t.Errorf("%s: panoOverlay = %q, want %q", tc.name, got, tc.want)
		}
	}
	if got := panoOverlay("- [x] ship → [@api]("+path+` "completed")`, nil); got != "- [x] ship" {
		t.Errorf("with no tasks panoOverlay = %q, want the item without its assignee", got)
	}
}

func TestPanoAssignedTaskIDs(t *testing.T) {
	md := "- [ ] a → [@api](" + panoTaskPath(11, 21) + ")\n" +
		"- [ ] b → [@api](" + panoTaskPath(11, 21) + ` "ongoing")` + "\n" +
		"- [x] c → [@web](" + panoTaskPath(12, 22) + ")\n" +
		"- [ ] d → [@web](/workspaces/x/tasks/0)\n" +
		"see [@api](" + panoTaskPath(11, 23) + ")\n" +
		"```\n- [ ] e → [@api](" + panoTaskPath(11, 24) + ")\n```"
	if got := panoAssignedTaskIDs(md); !reflect.DeepEqual(got, []int64{21, 22}) {
		t.Errorf("panoAssignedTaskIDs = %v, want each assigned item's task once: 21, 22", got)
	}
}

// panoTasks makes tasks of the account in the env, one per status, in skWS.
func (e *panoEnv) panoTasks(t *testing.T, statuses ...string) []model.Task {
	t.Helper()
	out := make([]model.Task, len(statuses))
	for i, st := range statuses {
		out[i] = model.Task{ID: int64(7001 + i), WorkspaceID: skWS, UserID: skUser, Title: "task", Status: st, Assignee: "agent"}
		if err := e.db.Create(&out[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	return out
}

func (e *panoEnv) links(t *testing.T) []model.PanoDocTask {
	t.Helper()
	var links []model.PanoDocTask
	if err := e.db.Order("doc_id, task_id").Find(&links).Error; err != nil {
		t.Fatal(err)
	}
	return links
}

func taskItem(name string, task model.Task) string {
	return "- [ ] " + name + " → [@alpha](" + panoTaskPath(task.WorkspaceID, task.ID) + ` "notstarted")`
}

// A save links the doc to the tasks its items are assigned to, writes each
// item with its task's status now, and a read returns those tasks.
func TestPano_SaveLinksAssignedTasks(t *testing.T) {
	e := newPanoEnv(t)
	tasks := e.panoTasks(t, "ongoing", "completed")
	theirs := model.Task{ID: 7100, WorkspaceID: skForgn, UserID: skOther, Title: "theirs", Status: "completed"}
	if err := e.db.Create(&theirs).Error; err != nil {
		t.Fatal(err)
	}
	md := taskItem("a", tasks[0]) + "\n" + taskItem("b", tasks[1]) + "\n" + taskItem("c", tasks[1]) + "\n" + taskItem("x", theirs) + "\n- [x] mine"
	doc := e.save(t, entity.SavePanoDocRequest{Title: "Launch", Markdown: md})
	want := "- [ ] a → [@alpha](" + panoTaskPath(skWS, 7001) + ` "ongoing")` + "\n" +
		"- [x] b → [@alpha](" + panoTaskPath(skWS, 7002) + ` "completed")` + "\n" +
		"- [x] c → [@alpha](" + panoTaskPath(skWS, 7002) + ` "completed")` + "\n" +
		"- [ ] x\n- [x] mine"
	if doc.Markdown != want || doc.ChecklistDone != 3 || doc.ChecklistTotal != 5 {
		t.Errorf("the saved doc is %q, %d of %d, want %q, 3 of 5", doc.Markdown, doc.ChecklistDone, doc.ChecklistTotal, want)
	}
	if links := e.links(t); len(links) != 2 || links[0].TaskID != 7001 || links[1].TaskID != 7002 || links[0].WorkspaceID != skWS || links[0].UserID != skUser {
		t.Errorf("links = %+v, want the account's two tasks, and never another account's", links)
	}
	got := e.get(t, doc.ID)
	wantTasks := []entity.PanoDocTask{{TaskID: 7001, WorkspaceID: skWS, Status: "ongoing"}, {TaskID: 7002, WorkspaceID: skWS, Status: "completed"}}
	if !reflect.DeepEqual(got.Tasks, wantTasks) || got.Markdown != want {
		t.Errorf("a read returns %+v and %q, want %+v and the markdown as saved", got.Tasks, got.Markdown, wantTasks)
	}

	// Saving again adds no second link, and a deleted task drops out of the read.
	e.save(t, entity.SavePanoDocRequest{ID: doc.ID, Version: doc.Version, Title: "Launch", Markdown: md})
	if links := e.links(t); len(links) != 2 {
		t.Errorf("after a second save there are %d links, want 2", len(links))
	}
	e.db.Delete(&model.Task{}, 7002)
	wantLeft := "- [ ] a → [@alpha](" + panoTaskPath(skWS, 7001) + ` "ongoing")` + "\n- [x] b\n- [x] c\n- [ ] x\n- [x] mine"
	if got := e.get(t, doc.ID); len(got.Tasks) != 1 || got.Tasks[0].TaskID != 7001 || got.Markdown != wantLeft {
		t.Errorf("after deleting a task the read returns %+v and %q, want only the task left and %q", got.Tasks, got.Markdown, wantLeft)
	}

	// Deleting the doc deletes its links.
	if err := e.c.DeletePanoDoc(e.ctx, entity.DeletePanoDocRequest{UserID: skUserStr, ID: doc.ID}); err != nil {
		t.Fatal(err)
	}
	if links := e.links(t); len(links) != 0 {
		t.Errorf("after deleting the doc %d links are left, want none", len(links))
	}
}

// An agent's write links the doc to the one task its workspace has ongoing;
// with none, or several, it links nothing rather than guess.
func TestPano_AnAgentsWriteIsLinkedToItsTask(t *testing.T) {
	agent := func(e *panoEnv) context.Context { return entity.WithAgentWorkspace(e.ctx, skWS) }
	t.Run("one ongoing task: the save and an edit are linked, once", func(t *testing.T) {
		e := newPanoEnv(t)
		e.panoTasks(t, "ongoing", "completed", "notstarted")
		rs, err := e.c.SavePanoDoc(agent(e), entity.SavePanoDocRequest{UserID: skUserStr, WorkspaceID: skWS, Title: "Notes", Markdown: "- [ ] step"})
		if err != nil {
			t.Fatal(err)
		}
		e.wantEvents(t, entity.CRUDEvent{Action: entity.ActionPanoCreate, WorkspaceID: skWS, ResourceID: rs.Doc.ID, Actor: entity.ActorAgent},
			entity.CRUDEvent{Action: entity.ActionPanoLink, WorkspaceID: skWS, ResourceID: rs.Doc.ID, Actor: entity.ActorAgent})
		if _, err := e.c.EditPanoDoc(agent(e), entity.EditPanoDocRequest{UserID: skUserStr, WorkspaceID: skWS, ID: rs.Doc.ID, Version: rs.Doc.Version, Ops: []entity.PanoEditOp{{Op: "replace", OldString: "- [ ] step", NewString: "- [x] step"}}}); err != nil {
			t.Fatal(err)
		}
		e.wantEvents(t, entity.CRUDEvent{Action: entity.ActionPanoUpdate, WorkspaceID: skWS, ResourceID: rs.Doc.ID, Actor: entity.ActorAgent})
		if links := e.links(t); len(links) != 1 || links[0].TaskID != 7001 || links[0].DocID != rs.Doc.ID {
			t.Errorf("links = %+v, want the doc linked to the ongoing task once", links)
		}
		if got := e.get(t, rs.Doc.ID); got.Markdown != "- [x] step" {
			t.Errorf("the agent's own item reads %q, want it ticked as the agent left it", got.Markdown)
		}
	})
	for _, tc := range []struct {
		name     string
		statuses []string
	}{{"two ongoing tasks", []string{"ongoing", "ongoing"}}, {"no ongoing task", []string{"blocked"}}} {
		t.Run(tc.name+": no link", func(t *testing.T) {
			e := newPanoEnv(t)
			e.panoTasks(t, tc.statuses...)
			if _, err := e.c.SavePanoDoc(agent(e), entity.SavePanoDocRequest{UserID: skUserStr, WorkspaceID: skWS, Title: "Notes", Markdown: "m"}); err != nil {
				t.Fatal(err)
			}
			if links := e.links(t); len(links) != 0 {
				t.Errorf("links = %+v, want none", links)
			}
		})
	}
	t.Run("the person's write is never linked", func(t *testing.T) {
		e := newPanoEnv(t)
		e.panoTasks(t, "ongoing")
		e.save(t, entity.SavePanoDocRequest{Title: "Notes", Markdown: "m"})
		if links := e.links(t); len(links) != 0 {
			t.Errorf("links = %+v, want none", links)
		}
	})
	t.Run("a failing lookup still saves the doc", func(t *testing.T) {
		e := newPanoEnv(t)
		e.panoTasks(t, "ongoing")
		failOn(e.db, "create", "pano_doc_tasks")
		if _, err := e.c.SavePanoDoc(agent(e), entity.SavePanoDocRequest{UserID: skUserStr, WorkspaceID: skWS, Title: "Notes", Markdown: "m"}); err != nil {
			t.Errorf("SavePanoDoc returned %v, want the doc saved without its link", err)
		}
	})
}

// A task's status reaches the items assigned to it in every doc linked to it,
// on read, without the doc being written: only its count of ticked items is.
func TestPano_SyncTask(t *testing.T) {
	t.Run("only the count is written; the markdown and version stay", func(t *testing.T) {
		e := newPanoEnv(t)
		task := e.panoTasks(t, "ongoing")[0]
		doc := e.save(t, entity.SavePanoDocRequest{Title: "Launch", Markdown: taskItem("ship", task) + "\n" + taskItem("docs", task) + "\n- [ ] demo"})
		linkedOnly := e.save(t, entity.SavePanoDocRequest{Title: "Chat", Markdown: "no items"})
		if _, err := e.c.repository.LinkPanoDocTasks(e.ctx, []model.PanoDocTask{{ID: 9001, UserID: skUser, DocID: linkedOnly.ID, TaskID: task.ID, WorkspaceID: skWS}}); err != nil {
			t.Fatal(err)
		}
		other := e.save(t, entity.SavePanoDocRequest{Title: "Other", Markdown: "- [ ] nothing assigned"})
		var stored model.PanoDoc
		e.db.First(&stored, doc.ID)

		e.db.Model(&model.Task{}).Where("id = ?", task.ID).Update("status", "completed")
		uid, ids, err := e.c.SyncPanoTask(e.ctx, task.ID)
		if err != nil || uid != skUser || !reflect.DeepEqual(ids, []int64{doc.ID, linkedOnly.ID}) {
			t.Fatalf("SyncPanoTask = %d, %v, %v, want the owner and both linked docs", uid, ids, err)
		}
		var after model.PanoDoc
		e.db.First(&after, doc.ID)
		if after.ChecklistDone != 2 || after.Markdown != stored.Markdown || after.Version != stored.Version || !after.UpdatedAt.Equal(stored.UpdatedAt) {
			t.Errorf("after the sync the row is %d done, version %d, updated %v, want 2 done and markdown, version %d and updated %v kept",
				after.ChecklistDone, after.Version, after.UpdatedAt, stored.Version, stored.UpdatedAt)
		}
		if got := e.get(t, doc.ID); !strings.HasPrefix(got.Markdown, "- [x] ship") || !strings.Contains(got.Markdown, "- [x] docs") || got.Version != doc.Version {
			t.Errorf("a read is %q at version %d, want both items ticked at version %d", got.Markdown, got.Version, doc.Version)
		}
		if _, ids, _ := e.c.SyncPanoTask(e.ctx, task.ID); len(ids) != 2 {
			t.Errorf("a second sync reported %v, want both docs again", ids)
		}
		// The board's card shows the item ticked too, with its status.
		list, err := e.c.ListPanoDocs(e.ctx, entity.ListPanoDocsRequest{UserID: skUserStr})
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range list.Docs {
			if d.ID == doc.ID && !strings.HasPrefix(d.Excerpt, "- [x] ship → [@alpha]("+panoTaskPath(skWS, task.ID)+` "completed")`) {
				t.Errorf("the card's excerpt is %q, want the item ticked with its task completed", d.Excerpt)
			}
		}
		if got := e.get(t, other.ID); got.Markdown != "- [ ] nothing assigned" {
			t.Errorf("a doc without the task became %q", got.Markdown)
		}
	})

	t.Run("an expired doc is skipped", func(t *testing.T) {
		e := newPanoEnv(t)
		task := e.panoTasks(t, "ongoing")[0]
		doc := e.save(t, entity.SavePanoDocRequest{Title: "Launch", Markdown: taskItem("ship", task)})
		e.db.Model(&model.PanoDoc{}).Where("id = ?", doc.ID).UpdateColumn("expires_at", time.Now().Add(-time.Hour))
		if _, ids, err := e.c.SyncPanoTask(e.ctx, task.ID); err != nil || len(ids) != 0 {
			t.Errorf("SyncPanoTask = %v, %v, want no doc", ids, err)
		}
	})

	t.Run("a deleted task is not found", func(t *testing.T) {
		e := newPanoEnv(t)
		if _, _, err := e.c.SyncPanoTask(e.ctx, 404); !errors.Is(err, base.ErrNotFound) {
			t.Errorf("SyncPanoTask of a missing task returned %v, want ErrNotFound", err)
		}
	})

	for _, tc := range []struct{ name, op, table string }{
		{"the links", "query", "pano_doc_tasks"},
		{"the doc", "query", "pano_docs"},
		{"the write", "update", "pano_docs"},
	} {
		t.Run(tc.name+" failing is passed on", func(t *testing.T) {
			e := newPanoEnv(t)
			task := e.panoTasks(t, "ongoing")[0]
			e.save(t, entity.SavePanoDocRequest{Title: "Launch", Markdown: taskItem("ship", task)})
			e.db.Model(&model.Task{}).Where("id = ?", task.ID).Update("status", "completed")
			failOn(e.db, tc.op, tc.table)
			if _, _, err := e.c.SyncPanoTask(e.ctx, task.ID); err == nil {
				t.Error("SyncPanoTask returned no error, want the injected failure")
			}
		})
	}
	t.Run("the doc's tasks failing is passed on", func(t *testing.T) {
		e := newPanoEnv(t)
		task := e.panoTasks(t, "ongoing")[0]
		e.save(t, entity.SavePanoDocRequest{Title: "Launch", Markdown: taskItem("ship", task)})
		e.c.repository = &failingDocTasks{Repository: e.c.repository}
		if _, _, err := e.c.SyncPanoTask(e.ctx, task.ID); err == nil {
			t.Error("SyncPanoTask returned no error, want the injected failure")
		}
	})
}

// Deleting a task unassigns its items: they read as the writer's own, and
// their docs' counts follow, so a completed task deleted leaves no tick behind.
func TestPano_DeleteTaskUnassignsItems(t *testing.T) {
	e := newPanoEnv(t)
	if err := e.db.AutoMigrate(&model.Message{}, &model.ToolCall{}, &model.TaskPlan{}, &model.SlackTaskThread{}, &model.TaskStateTransition{}); err != nil {
		t.Fatal(err)
	}
	tasks := e.panoTasks(t, "ongoing", "ongoing")
	doc := e.save(t, entity.SavePanoDocRequest{Title: "Launch", Markdown: taskItem("ship", tasks[0]) + "\n" + taskItem("docs", tasks[1])})
	e.db.Model(&model.Task{}).Where("id = ?", tasks[0].ID).Update("status", "completed")
	if _, _, err := e.c.SyncPanoTask(e.ctx, tasks[0].ID); err != nil {
		t.Fatal(err)
	}

	if _, err := e.c.DeleteTask(e.ctx, entity.DeleteTaskRequest{WorkspaceID: skWS, TaskID: tasks[0].ID, UserID: skUserStr}); err != nil {
		t.Fatal(err)
	}
	want := "- [ ] ship\n- [ ] docs → [@alpha](" + panoTaskPath(skWS, tasks[1].ID) + ` "ongoing")`
	if got := e.get(t, doc.ID); got.Markdown != want || got.ChecklistDone != 0 || got.Version != doc.Version {
		t.Errorf("after deleting the task the doc is %q, %d done, version %d, want %q, none done, version %d", got.Markdown, got.ChecklistDone, got.Version, want, doc.Version)
	}
	if links := e.links(t); len(links) != 1 || links[0].TaskID != tasks[1].ID {
		t.Errorf("links = %+v, want only the task left", links)
	}
}

// failingDocTasks fails reading a doc's tasks, and nothing else.
type failingDocTasks struct{ base.Repository }

func (f *failingDocTasks) ListPanoDocTasks(context.Context, int64, []int64) ([]base.PanoTaskStatus, error) {
	return nil, errors.New("database unavailable")
}

// The edit's ops apply to the markdown as a read shows it, and reading its
// tasks failing refuses the edit.
func TestPano_EditSeesTheLiveStatus(t *testing.T) {
	e := newPanoEnv(t)
	task := e.panoTasks(t, "ongoing")[0]
	doc := e.save(t, entity.SavePanoDocRequest{Title: "Launch", Markdown: taskItem("ship", task)})
	e.db.Model(&model.Task{}).Where("id = ?", task.ID).Update("status", "completed")
	live := "- [x] ship → [@alpha](" + panoTaskPath(skWS, task.ID) + ` "completed")`
	rs, err := e.c.EditPanoDoc(e.ctx, entity.EditPanoDocRequest{UserID: skUserStr, ID: doc.ID, Version: doc.Version, Ops: []entity.PanoEditOp{{Op: "replace", OldString: live, NewString: live + " (shipped)"}}})
	if err != nil {
		t.Fatalf("an edit naming the item as a read shows it returned %v", err)
	}
	if rs.Doc.Markdown != live+" (shipped)" {
		t.Errorf("the edit wrote %q", rs.Doc.Markdown)
	}
	e.c.repository = &failingDocTasks{Repository: e.c.repository}
	if _, err := e.c.EditPanoDoc(e.ctx, entity.EditPanoDocRequest{UserID: skUserStr, ID: doc.ID, Version: rs.Doc.Version, Ops: []entity.PanoEditOp{{Op: "append", Text: "x"}}}); err == nil {
		t.Error("EditPanoDoc returned no error, want the injected failure")
	}
}

// A save whose assigned tasks cannot be read is refused, and stores nothing.
func TestPano_SaveFailsWhenItsTasksCannotBeRead(t *testing.T) {
	e := newPanoEnv(t)
	task := e.panoTasks(t, "ongoing")[0]
	failOn(e.db, "row", "tasks")
	if _, err := e.c.SavePanoDoc(e.ctx, entity.SavePanoDocRequest{UserID: skUserStr, Title: "Launch", Markdown: taskItem("ship", task)}); err == nil {
		t.Error("SavePanoDoc returned no error, want the injected failure")
	}
	var n int64
	e.db.Model(&model.PanoDoc{}).Count(&n)
	if n != 0 {
		t.Errorf("%d docs were stored, want none", n)
	}
}

// The backfill links every doc, of any account, to the tasks its items name,
// a page at a time, and running it again adds nothing.
func TestPano_BackfillDocTasks(t *testing.T) {
	e := newPanoEnv(t)
	tasks := e.panoTasks(t, "ongoing", "completed")
	theirs := model.Task{ID: 7100, WorkspaceID: skForgn, UserID: skOther, Title: "theirs", Status: "completed"}
	if err := e.db.Create(&theirs).Error; err != nil {
		t.Fatal(err)
	}
	// Docs written before links were stored: straight into the table.
	var docs []model.PanoDoc
	for i := 0; i < panoBackfillBatch+1; i++ {
		docs = append(docs, model.PanoDoc{ID: int64(100 + i), UserID: skUser, Title: "d", Markdown: taskItem("a", tasks[i%2]), ExpiresAt: time.Now().Add(time.Hour), Version: 1})
	}
	docs = append(docs, model.PanoDoc{ID: 900, UserID: skOther, Title: "theirs", Markdown: taskItem("a", theirs) + "\n" + taskItem("b", tasks[0]), ExpiresAt: time.Now().Add(time.Hour), Version: 1},
		model.PanoDoc{ID: 901, UserID: skUser, Title: "plain", Markdown: "no tasks", ExpiresAt: time.Now().Add(time.Hour), Version: 1})
	if err := e.db.Create(&docs).Error; err != nil {
		t.Fatal(err)
	}
	n, err := e.c.BackfillPanoDocTasks(e.ctx)
	if err != nil || n != int64(panoBackfillBatch+2) {
		t.Fatalf("BackfillPanoDocTasks = %d, %v, want %d links", n, err, panoBackfillBatch+2)
	}
	var theirLinks []model.PanoDocTask
	e.db.Where("doc_id = ?", 900).Find(&theirLinks)
	if len(theirLinks) != 1 || theirLinks[0].TaskID != theirs.ID || theirLinks[0].UserID != skOther {
		t.Errorf("the other account's doc has links %+v, want only its own task", theirLinks)
	}
	if n, err := e.c.BackfillPanoDocTasks(e.ctx); err != nil || n != 0 {
		t.Errorf("a second backfill = %d, %v, want nothing added", n, err)
	}
	for _, tc := range []struct{ op, table string }{{"query", "pano_docs"}, {"row", "tasks"}, {"create", "pano_doc_tasks"}} {
		t.Run(tc.op+" "+tc.table+" failing is passed on", func(t *testing.T) {
			e := newPanoEnv(t)
			task := e.panoTasks(t, "ongoing")[0]
			if err := e.db.Create(&model.PanoDoc{ID: 100, UserID: skUser, Title: "d", Markdown: taskItem("a", task), ExpiresAt: time.Now().Add(time.Hour)}).Error; err != nil {
				t.Fatal(err)
			}
			failOn(e.db, tc.op, tc.table)
			if _, err := e.c.BackfillPanoDocTasks(e.ctx); err == nil {
				t.Error("BackfillPanoDocTasks returned no error, want the injected failure")
			}
		})
	}
}

func (e *panoEnv) wantStale(t *testing.T, err error, contains ...string) {
	t.Helper()
	var refusal *entity.PanoError
	if !errors.As(err, &refusal) || !refusal.Conflict {
		t.Fatalf("got %v, want a stale-version refusal", err)
	}
	for _, s := range contains {
		if !strings.Contains(refusal.Message, s) {
			t.Errorf("the refusal %q does not contain %q", refusal.Message, s)
		}
	}
}

// A doc starts at version 1 and every write moves it on; an update must send
// the version it read, and the refusal names the current one.
func TestPano_VersionIsCheckedOnUpdate(t *testing.T) {
	e := newPanoEnv(t)
	doc := e.save(t, entity.SavePanoDocRequest{Title: "Plan", Markdown: "one"})
	if doc.Version != 1 || e.get(t, doc.ID).Version != 1 {
		t.Fatalf("a new doc is at version %d, want 1", doc.Version)
	}
	next := e.save(t, entity.SavePanoDocRequest{ID: doc.ID, Version: 1, Title: "Plan", Markdown: "two"})
	if next.Version != 2 || e.get(t, doc.ID).Version != 2 {
		t.Errorf("after a save with the right version the doc is at %d, want 2", next.Version)
	}
	*e.events = (*e.events)[:0]

	_, err := e.c.SavePanoDoc(e.ctx, entity.SavePanoDocRequest{UserID: skUserStr, ID: doc.ID, Version: 1, Title: "Plan", Markdown: "stale"})
	e.wantStale(t, err, "changed since you read it", "you sent version 1", "now 2", "read it again")
	_, err = e.c.SavePanoDoc(e.ctx, entity.SavePanoDocRequest{UserID: skUserStr, ID: doc.ID, Title: "Plan", Markdown: "no version"})
	e.wantStale(t, err, "needs the version you read", "now 2")
	if got := e.get(t, doc.ID); got.Markdown != "two" || got.Version != 2 {
		t.Errorf("after refused saves the doc is %q at %d, want it untouched", got.Markdown, got.Version)
	}
	e.wantEvents(t)
}

// A write that lands between the controller's read and its own is caught by
// the repository, and answered like any other stale write.
func TestPano_ALostRaceIsAStaleWrite(t *testing.T) {
	for _, tc := range []struct {
		name    string
		failGet bool
		want    string
	}{
		{"names the version the other writer left", false, "now 2"},
		{"falls back to the version it read when it cannot look", true, "now 1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newPanoEnv(t)
			doc := e.save(t, entity.SavePanoDocRequest{Title: "Plan", Markdown: "one"})
			rr := &racingRepo{Repository: e.c.repository, hook: func() {
				e.db.Model(&model.PanoDoc{}).Where("id = ?", doc.ID).Update("version", 2)
			}, failGet: tc.failGet}
			e.c.repository = rr
			_, err := e.c.SavePanoDoc(e.ctx, entity.SavePanoDocRequest{UserID: skUserStr, ID: doc.ID, Version: 1, Title: "Plan", Markdown: "mine"})
			e.wantStale(t, err, tc.want)
			e.c.repository = rr.Repository
			if got := e.get(t, doc.ID); got.Markdown != "one" {
				t.Errorf("the doc is %q, want the other writer's", got.Markdown)
			}
		})
	}
}

// racingRepo lets another writer in just before the save, optionally making
// the read that follows the conflict fail.
type racingRepo struct {
	base.Repository
	hook    func()
	failGet bool
	raced   bool
	saveErr error
}

func (r *racingRepo) SavePanoDoc(ctx context.Context, d model.PanoDoc, create bool, add []model.PanoFile, drop []int64, links []model.PanoDocTask) error {
	if r.saveErr != nil {
		return r.saveErr
	}
	r.hook()
	r.raced = true
	return r.Repository.SavePanoDoc(ctx, d, create, add, drop, links)
}

func (r *racingRepo) GetPanoDoc(ctx context.Context, userID, id int64, now time.Time) (model.PanoDoc, error) {
	if r.raced && r.failGet {
		return model.PanoDoc{}, errors.New("database unavailable")
	}
	return r.Repository.GetPanoDoc(ctx, userID, id, now)
}

func TestPano_Edit(t *testing.T) {
	const md = "# Plan\n\n- [ ] one\n- [ ] two\n![c](c.png)\n"
	setup := func(t *testing.T) (*panoEnv, entity.PanoDoc) {
		e := newPanoEnv(t)
		doc := e.save(t, entity.SavePanoDocRequest{Title: "Plan", Markdown: md, TTLDays: 7, Color: "blue", Files: []entity.PanoUpload{{Filename: "c.png", Data: b64("c")}}})
		*e.events = (*e.events)[:0]
		return e, doc
	}
	edit := func(e *panoEnv, doc entity.PanoDoc, ops ...entity.PanoEditOp) (*entity.SavePanoDocResponse, error) {
		return e.c.EditPanoDoc(e.ctx, entity.EditPanoDocRequest{UserID: skUserStr, WorkspaceID: skWS, ID: doc.ID, Version: doc.Version, Ops: ops})
	}
	tick := entity.PanoEditOp{Op: "replace", OldString: "- [ ] one", NewString: "- [x] one"}

	t.Run("ops apply in order, move the version and keep the rest", func(t *testing.T) {
		e, doc := setup(t)
		rs, err := edit(e, doc, tick, entity.PanoEditOp{Op: "append", Text: "- [ ] three\n"}, entity.PanoEditOp{Op: "replace", OldString: "- [ ] three", NewString: "- [ ] 3"})
		if err != nil {
			t.Fatalf("EditPanoDoc returned %v, want the edit applied", err)
		}
		want := "# Plan\n\n- [x] one\n- [ ] two\n![c](c.png)\n- [ ] 3\n"
		got := e.get(t, doc.ID)
		if got.Markdown != want || rs.Doc.Markdown != want || got.Version != 2 || rs.Doc.Version != 2 {
			t.Errorf("the doc is %q at %d, want %q at 2", got.Markdown, got.Version, want)
		}
		if got.ChecklistDone != 1 || got.ChecklistTotal != 3 || got.Title != "Plan" || got.TTLDays != 7 || got.Color != entity.PanoColorBlue || !reflect.DeepEqual(fileNames(got.Files), []string{"c.png"}) {
			t.Errorf("the edit left the doc %+v, want the checklist 1 of 3 and its title, TTL, color and file kept", got)
		}
		e.wantEvents(t, entity.CRUDEvent{Action: entity.ActionPanoUpdate, WorkspaceID: skWS, ResourceID: doc.ID, Actor: entity.ActorAgent})
	})

	t.Run("a stale or missing version is refused before the ops run", func(t *testing.T) {
		e, doc := setup(t)
		e.save(t, entity.SavePanoDocRequest{ID: doc.ID, Version: 1, Title: "Plan", Markdown: "- [x] nothing like it"})
		_, err := edit(e, doc, tick)
		e.wantStale(t, err, "you sent version 1", "now 2")
		doc.Version = 0
		_, err = edit(e, doc, tick)
		e.wantStale(t, err, "needs the version you read")
	})

	for _, tc := range []struct {
		name string
		ops  []entity.PanoEditOp
		want string
	}{
		{"no ops", nil, "at least one op"},
		{"an unknown op", []entity.PanoEditOp{{Op: "delete"}}, "op 1: op must be append or replace"},
		{"an append without text", []entity.PanoEditOp{{Op: "append"}}, "op 1: append needs text"},
		{"a replace without an oldString", []entity.PanoEditOp{{Op: "replace", NewString: "x"}}, "op 1: replace needs an oldString"},
		{"a replace that matches nothing", []entity.PanoEditOp{{Op: "replace", OldString: "- [ ] zero", NewString: "x"}}, "not in the markdown, nothing was changed"},
		{"a replace that matches twice", []entity.PanoEditOp{{Op: "replace", OldString: "- [ ]", NewString: "- [x]"}}, "occurs 2 times"},
		{"a later op failing undoes the earlier ones", []entity.PanoEditOp{tick, {Op: "append", Text: "more"}, {Op: "replace", OldString: "missing", NewString: "x"}}, "op 3:"},
		{"a result over the size limit", []entity.PanoEditOp{{Op: "append", Text: strings.Repeat("x", PanoMaxMarkdownBytes)}}, "over the"},
		{"a result that is not valid UTF-8", []entity.PanoEditOp{{Op: "append", Text: "\xff"}}, "not valid UTF-8"},
	} {
		t.Run("refuses "+tc.name+" and changes nothing", func(t *testing.T) {
			e, doc := setup(t)
			_, err := edit(e, doc, tc.ops...)
			var refusal *entity.PanoError
			if !errors.As(err, &refusal) || refusal.Conflict || !strings.Contains(refusal.Message, tc.want) {
				t.Fatalf("EditPanoDoc returned %v, want a refusal containing %q", err, tc.want)
			}
			if got := e.get(t, doc.ID); got.Markdown != md || got.Version != 1 {
				t.Errorf("the doc is %q at %d, want it untouched", got.Markdown, got.Version)
			}
			e.wantEvents(t)
		})
	}

	t.Run("a replace that stops linking a file drops it, as a save does", func(t *testing.T) {
		e, doc := setup(t)
		if _, err := edit(e, doc, entity.PanoEditOp{Op: "replace", OldString: "![c](c.png)", NewString: ""}); err != nil {
			t.Fatal(err)
		}
		if got := e.get(t, doc.ID); len(got.Files) != 0 || len(e.blobs(t)) != 0 {
			t.Errorf("the doc keeps files %v and blobs %v, want the unlinked file gone", fileNames(got.Files), e.blobs(t))
		}
	})

	t.Run("a doc that is missing, or another account's, is not found", func(t *testing.T) {
		e, doc := setup(t)
		if _, err := e.c.EditPanoDoc(e.ctx, entity.EditPanoDocRequest{UserID: skUserStr, ID: 9999, Version: 1, Ops: []entity.PanoEditOp{tick}}); !errors.Is(err, base.ErrNotFound) {
			t.Errorf("editing a missing doc returned %v, want ErrNotFound", err)
		}
		if _, err := e.c.EditPanoDoc(e.ctx, entity.EditPanoDocRequest{UserID: "", ID: doc.ID, Version: 1, Ops: []entity.PanoEditOp{tick}}); err == nil {
			t.Error("editing as an invalid user succeeded, want an error")
		}
	})
}

// Any other failure of the write is passed on, not taken for a stale version.
func TestPano_ASaveFailureIsNotAStaleWrite(t *testing.T) {
	e := newPanoEnv(t)
	doc := e.save(t, entity.SavePanoDocRequest{Title: "Plan", Markdown: "one"})
	e.c.repository = &racingRepo{Repository: e.c.repository, saveErr: errors.New("database unavailable")}
	_, err := e.c.SavePanoDoc(e.ctx, entity.SavePanoDocRequest{UserID: skUserStr, ID: doc.ID, Version: 1, Title: "Plan", Markdown: "two"})
	var refusal *entity.PanoError
	if err == nil || errors.As(err, &refusal) {
		t.Errorf("SavePanoDoc returned %v, want the database failure", err)
	}
}

// A task made about a doc is linked to it as it is created; for a doc that
// is not the account's, no task is made.
func TestCreateTask_LinkedToAPanoDoc(t *testing.T) {
	e := newPanoEnv(t)
	if err := e.db.AutoMigrate(&model.TaskStateTransition{}, &model.TaskLatency{}); err != nil {
		t.Fatal(err)
	}
	doc := e.save(t, entity.SavePanoDocRequest{Title: "Release plan", Markdown: "- [ ] tag"})
	*e.events = (*e.events)[:0]
	rs, err := e.c.CreateTask(e.ctx, entity.CreateTaskRequest{UserID: skUserStr, PanoDocID: doc.ID, Task: entity.Task{WorkspaceID: skWS, CreatedBy: "human", Assignee: "agent", Title: "Update the plan"}})
	if err != nil {
		t.Fatalf("CreateTask returned %v", err)
	}
	if links := e.links(t); len(links) != 1 || links[0].DocID != doc.ID || links[0].TaskID != rs.Task.ID || links[0].WorkspaceID != skWS || links[0].UserID != skUser {
		t.Errorf("links = %+v, want the doc linked to the new task", links)
	}
	if got := e.get(t, doc.ID); len(got.Tasks) != 1 || got.Tasks[0].TaskID != rs.Task.ID || got.Tasks[0].Status != "notstarted" {
		t.Errorf("the doc reads with tasks %+v, want the new task, not started", got.Tasks)
	}
	if n := len(*e.events); n != 2 || (*e.events)[0].Action != entity.ActionTaskCreate || (*e.events)[1].Action != entity.ActionPanoLink || (*e.events)[1].ResourceID != doc.ID {
		t.Errorf("events = %+v, want the task created, then the doc linked", *e.events)
	}

	theirs := e.save(t, entity.SavePanoDocRequest{UserID: monoflake.ID(skOther).String(), Title: "Theirs", Markdown: "m"})
	if _, err := e.c.CreateTask(e.ctx, entity.CreateTaskRequest{UserID: skUserStr, PanoDocID: theirs.ID, Task: entity.Task{WorkspaceID: skWS, CreatedBy: "human", Assignee: "agent", Title: "x"}}); !errors.Is(err, base.ErrNotFound) {
		t.Errorf("CreateTask for another account's doc returned %v, want ErrNotFound", err)
	}
	var n int64
	e.db.Model(&model.Task{}).Count(&n)
	if n != 1 {
		t.Errorf("%d tasks exist, want only the first: a refused link makes no task", n)
	}
}
