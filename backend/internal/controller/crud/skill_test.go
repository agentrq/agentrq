// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package crud

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/skill"
	"github.com/agentrq/agentrq/backend/internal/service/skillimport"
	"github.com/agentrq/agentrq/backend/internal/service/storage"
	"github.com/glebarez/sqlite"
	"github.com/mustafaturan/monoflake"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// The skill controller is tested against a real database and a real storage
// directory: its promises — that no row points at a missing blob and no blob
// outlives its row — are about how the two move together, which a mocked
// repository cannot show.

const (
	skUser  = int64(482467435371298817)
	skOther = int64(482467435371298818)
	skWS    = int64(1001)
	skWS2   = int64(1002)
	skWS3   = int64(1003)
	skForgn = int64(2001) // belongs to skOther
)

var skUserStr = monoflake.ID(skUser).String()

type fakeImporter struct {
	res  *skillimport.Result
	err  error
	url  string
	only []string
}

func (f *fakeImporter) Fetch(ctx context.Context, rawURL string, only []string) (*skillimport.Result, error) {
	f.url, f.only = rawURL, only
	return f.res, f.err
}

type skillEnv struct {
	c        *controller
	db       *gorm.DB
	dir      string
	importer *fakeImporter
	ctx      context.Context
}

func newSkillEnv(t *testing.T) *skillEnv {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Workspace{}, &model.Skill{}, &model.SkillFile{}, &model.SkillShare{}); err != nil {
		t.Fatal(err)
	}
	for _, ws := range []model.Workspace{
		{ID: skWS, UserID: skUser, Name: "alpha"},
		{ID: skWS2, UserID: skUser, Name: "beta"},
		{ID: skWS3, UserID: skUser, Name: "gamma"},
		{ID: skForgn, UserID: skOther, Name: "theirs"},
	} {
		if err := db.Create(&ws).Error; err != nil {
			t.Fatal(err)
		}
	}
	dir := t.TempDir()
	store, err := storage.NewNested(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Attachments get a store of their own, so a skill blob written to the
	// wrong one shows up in blobs as missing.
	attachments, err := storage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	imp := &fakeImporter{}
	c := &controller{
		repository:   base.New(&realDB{db: db}),
		idgen:        &seqIDGen{n: 5000},
		storage:      attachments,
		skillStorage: store,
		skillImport:  imp,
	}
	return &skillEnv{c: c, db: db, dir: dir, importer: imp, ctx: context.Background()}
}

func md(name, description string) string {
	return "---\nname: " + name + "\ndescription: " + description + "\n---\n# " + name + "\n"
}

func (e *skillEnv) save(t *testing.T, ws int64, name, path, content string) *entity.SaveSkillFileResponse {
	t.Helper()
	rs, err := e.c.SaveSkillFile(e.ctx, entity.SaveSkillFileRequest{WorkspaceID: ws, UserID: skUserStr, Name: name, Path: path, Content: content})
	if err != nil {
		t.Fatalf("save %s/%s: %v", name, path, err)
	}
	return rs
}

// blobs is how many files are in the storage directory: the check that
// nothing leaked, and nothing was lost.
func (e *skillEnv) blobs(t *testing.T) int {
	t.Helper()
	n := 0
	err := filepath.WalkDir(e.dir, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			n++
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func wantSkillErr(t *testing.T, err error, kind SkillErrorKind, contains string) {
	t.Helper()
	var se *SkillError
	if !errors.As(err, &se) || se.Kind != kind || !strings.Contains(se.Message, contains) || err.Error() != se.Message {
		t.Fatalf("want SkillError kind %d containing %q, got %v", kind, contains, err)
	}
}

func TestSkills_SaveListReadDelete(t *testing.T) {
	e := newSkillEnv(t)

	rs := e.save(t, skWS, "PR-Reviewer", "SKILL.md", md("pr-reviewer", "Reviews pull requests."))
	if rs.Skill.Name != "pr-reviewer" || rs.Skill.Description != "Reviews pull requests." || rs.Skill.SourceType != "manual" || rs.Skill.FileCount != 1 {
		t.Fatalf("created: %+v", rs.Skill)
	}
	e.save(t, skWS, "pr-reviewer", "references/checklist.md", "- tests pass")
	rs = e.save(t, skWS, "pr-reviewer", "references/checklist.md", "- tests pass\n- docs updated")
	if rs.Skill.FileCount != 2 || rs.Skill.TotalBytes != len(md("pr-reviewer", "Reviews pull requests."))+len("- tests pass\n- docs updated") {
		t.Errorf("aggregates after replacing a file: %+v", rs.Skill)
	}
	// Replacing a file drops its old blob.
	if n := e.blobs(t); n != 2 {
		t.Errorf("storage holds %d blobs, want 2", n)
	}
	// Each blob is filed under its workspace and skill.
	var sk model.Skill
	e.db.Where("name = ?", "pr-reviewer").First(&sk)
	var files []model.SkillFile
	e.db.Where("skill_id = ?", sk.ID).Find(&files)
	dir := "w-" + monoflake.ID(skWS).String() + "/skill-" + monoflake.ID(sk.ID).String() + "/"
	for _, f := range files {
		if !strings.HasPrefix(f.StorageID, dir) {
			t.Errorf("blob %q is not under %q", f.StorageID, dir)
		}
		if _, err := os.Stat(filepath.Join(e.dir, filepath.FromSlash(f.StorageID))); err != nil {
			t.Errorf("blob %q: %v", f.StorageID, err)
		}
	}

	list, err := e.c.SearchSkills(e.ctx, entity.SearchSkillsRequest{WorkspaceID: skWS, UserID: skUserStr})
	if err != nil || len(list.Skills) != 1 || list.Skills[0].SharedFromWorkspaceID != 0 {
		t.Fatalf("list: %+v, %v", list, err)
	}

	got, err := e.c.GetSkill(e.ctx, entity.GetSkillRequest{WorkspaceID: skWS, UserID: skUserStr, Name: "pr-reviewer"})
	if err != nil || len(got.Skill.Files) != 2 || got.Skill.Files[0].Path != "SKILL.md" || got.Skill.Files[1].Content != "" {
		t.Fatalf("get: %+v, %v", got, err)
	}

	file, err := e.c.GetSkillFile(e.ctx, entity.GetSkillFileRequest{WorkspaceID: skWS, UserID: skUserStr, Name: "pr-reviewer", Path: "references/checklist.md"})
	if err != nil || file.File.Content != "- tests pass\n- docs updated" {
		t.Fatalf("file: %+v, %v", file, err)
	}

	del, err := e.c.DeleteSkillFile(e.ctx, entity.DeleteSkillFileRequest{WorkspaceID: skWS, UserID: skUserStr, Name: "pr-reviewer", Path: "references/checklist.md"})
	if err != nil || del.Skill.FileCount != 1 {
		t.Fatalf("delete file: %+v, %v", del, err)
	}
	if n := e.blobs(t); n != 1 {
		t.Errorf("storage holds %d blobs after deleting a file, want 1", n)
	}

	if err := e.c.DeleteSkill(e.ctx, entity.DeleteSkillRequest{WorkspaceID: skWS, UserID: skUserStr, Name: "pr-reviewer"}); err != nil {
		t.Fatalf("delete skill: %v", err)
	}
	if n := e.blobs(t); n != 0 {
		t.Errorf("storage holds %d blobs after deleting the skill, want 0", n)
	}
	if entries, _ := os.ReadDir(e.dir); len(entries) != 0 {
		t.Errorf("deleting the skill left %v behind", entries)
	}
	if _, err := e.c.GetSkill(e.ctx, entity.GetSkillRequest{WorkspaceID: skWS, UserID: skUserStr, Name: "pr-reviewer"}); !errors.Is(err, base.ErrNotFound) {
		t.Errorf("deleted skill: %v", err)
	}
}

func TestSkills_SaveRefusals(t *testing.T) {
	e := newSkillEnv(t)
	e.save(t, skWS, "tdd", "SKILL.md", md("tdd", "Test first."))

	for _, tc := range []struct {
		name, skill, path, content, want string
	}{
		{name: "bad skill name", skill: "Not Valid", path: "SKILL.md", content: md("x", "d"), want: "not usable"},
		{name: "bad path", skill: "tdd", path: "../escape.md", content: "x", want: `".."`},
		{name: "bad frontmatter", skill: "tdd", path: "SKILL.md", content: "no frontmatter", want: "YAML frontmatter"},
		{name: "frontmatter names another skill", skill: "tdd", path: "SKILL.md", content: md("other", "d"), want: "make the two match"},
		{name: "sub file too large", skill: "tdd", path: "big.md", content: strings.Repeat("a", skill.MaxSubFileBytes+1), want: "64 KiB"},
		{name: "sub file before SKILL.md", skill: "missing", path: "notes.md", content: "x", want: "save its SKILL.md first"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := e.c.SaveSkillFile(e.ctx, entity.SaveSkillFileRequest{WorkspaceID: skWS, UserID: skUserStr, Name: tc.skill, Path: tc.path, Content: tc.content})
			wantSkillErr(t, err, SkillInvalid, tc.want)
		})
	}
	if n := e.blobs(t); n != 1 {
		t.Errorf("a refused save must store nothing; storage holds %d blobs", n)
	}
}

func TestSkills_FileLimit(t *testing.T) {
	e := newSkillEnv(t)
	e.save(t, skWS, "wide", "SKILL.md", md("wide", "Many files."))
	for i := 1; i < skill.MaxFiles; i++ {
		e.save(t, skWS, "wide", fmt.Sprintf("f%03d.md", i), "x")
	}
	_, err := e.c.SaveSkillFile(e.ctx, entity.SaveSkillFileRequest{WorkspaceID: skWS, UserID: skUserStr, Name: "wide", Path: "one-more.md", Content: "x"})
	wantSkillErr(t, err, SkillInvalid, "the limit")
	// Replacing a file that exists is not adding one.
	e.save(t, skWS, "wide", "f001.md", "y")
}

func TestSkills_DeleteFileRefusals(t *testing.T) {
	e := newSkillEnv(t)
	e.save(t, skWS, "tdd", "SKILL.md", md("tdd", "Test first."))
	req := entity.DeleteSkillFileRequest{WorkspaceID: skWS, UserID: skUserStr, Name: "tdd"}

	req.Path = "SKILL.md"
	_, err := e.c.DeleteSkillFile(e.ctx, req)
	wantSkillErr(t, err, SkillInvalid, "delete the whole skill")

	req.Path = "../x"
	_, err = e.c.DeleteSkillFile(e.ctx, req)
	wantSkillErr(t, err, SkillInvalid, `".."`)

	req.Path = "missing.md"
	if _, err := e.c.DeleteSkillFile(e.ctx, req); !errors.Is(err, base.ErrNotFound) {
		t.Errorf("missing file: %v", err)
	}
	req.Name = "missing"
	if _, err := e.c.DeleteSkillFile(e.ctx, req); !errors.Is(err, base.ErrNotFound) {
		t.Errorf("missing skill: %v", err)
	}
	req.Name = "Bad Name"
	_, err = e.c.DeleteSkillFile(e.ctx, req)
	wantSkillErr(t, err, SkillInvalid, "not usable")

	if err := e.c.DeleteSkill(e.ctx, entity.DeleteSkillRequest{WorkspaceID: skWS, UserID: skUserStr, Name: "missing"}); !errors.Is(err, base.ErrNotFound) {
		t.Errorf("delete missing skill: %v", err)
	}
	err = e.c.DeleteSkill(e.ctx, entity.DeleteSkillRequest{WorkspaceID: skWS, UserID: skUserStr, Name: "Bad Name"})
	wantSkillErr(t, err, SkillInvalid, "not usable")
}

func TestSkills_ReadRefusals(t *testing.T) {
	e := newSkillEnv(t)
	e.save(t, skWS, "tdd", "SKILL.md", md("tdd", "Test first."))

	_, err := e.c.GetSkill(e.ctx, entity.GetSkillRequest{WorkspaceID: skWS, UserID: skUserStr, Name: "Bad Name"})
	wantSkillErr(t, err, SkillInvalid, "not usable")
	_, err = e.c.GetSkillFile(e.ctx, entity.GetSkillFileRequest{WorkspaceID: skWS, UserID: skUserStr, Name: "Bad Name", Path: "SKILL.md"})
	wantSkillErr(t, err, SkillInvalid, "not usable")
	_, err = e.c.GetSkillFile(e.ctx, entity.GetSkillFileRequest{WorkspaceID: skWS, UserID: skUserStr, Name: "tdd", Path: "/abs"})
	wantSkillErr(t, err, SkillInvalid, "absolute")
	if _, err := e.c.GetSkillFile(e.ctx, entity.GetSkillFileRequest{WorkspaceID: skWS, UserID: skUserStr, Name: "missing", Path: "SKILL.md"}); !errors.Is(err, base.ErrNotFound) {
		t.Errorf("missing skill: %v", err)
	}
	if _, err := e.c.GetSkillFile(e.ctx, entity.GetSkillFileRequest{WorkspaceID: skWS, UserID: skUserStr, Name: "tdd", Path: "nope.md"}); !errors.Is(err, base.ErrNotFound) {
		t.Errorf("missing file: %v", err)
	}

	// A blob that went missing underneath its row is a server fault, not a miss.
	var f model.SkillFile
	e.db.First(&f)
	os.Remove(filepath.Join(e.dir, f.StorageID))
	if _, err := e.c.GetSkillFile(e.ctx, entity.GetSkillFileRequest{WorkspaceID: skWS, UserID: skUserStr, Name: "tdd", Path: "SKILL.md"}); err == nil || errors.Is(err, base.ErrNotFound) {
		t.Errorf("missing blob: %v", err)
	}
}

// Skills are the workspace owner's, exactly like memories: another account
// cannot read, write, import into or share into a workspace it does not own,
// and learns nothing about whether it exists.
func TestSkills_AccountIsolation(t *testing.T) {
	e := newSkillEnv(t)
	e.save(t, skWS, "tdd", "SKILL.md", md("tdd", "Test first."))
	other := monoflake.ID(skOther).String()

	calls := map[string]func() error{
		"list": func() error {
			_, err := e.c.SearchSkills(e.ctx, entity.SearchSkillsRequest{WorkspaceID: skWS, UserID: other})
			return err
		},
		"get": func() error {
			_, err := e.c.GetSkill(e.ctx, entity.GetSkillRequest{WorkspaceID: skWS, UserID: other, Name: "tdd"})
			return err
		},
		"file": func() error {
			_, err := e.c.GetSkillFile(e.ctx, entity.GetSkillFileRequest{WorkspaceID: skWS, UserID: other, Name: "tdd", Path: "SKILL.md"})
			return err
		},
		"save": func() error {
			_, err := e.c.SaveSkillFile(e.ctx, entity.SaveSkillFileRequest{WorkspaceID: skWS, UserID: other, Name: "tdd", Path: "SKILL.md", Content: md("tdd", "x")})
			return err
		},
		"delete": func() error {
			return e.c.DeleteSkill(e.ctx, entity.DeleteSkillRequest{WorkspaceID: skWS, UserID: other, Name: "tdd"})
		},
		"turn off": func() error {
			_, err := e.c.SetSkillEnabled(e.ctx, entity.SetSkillEnabledRequest{WorkspaceID: skWS, UserID: other, Name: "tdd"})
			return err
		},
		"delete file": func() error {
			_, err := e.c.DeleteSkillFile(e.ctx, entity.DeleteSkillFileRequest{WorkspaceID: skWS, UserID: other, Name: "tdd", Path: "x.md"})
			return err
		},
		"import": func() error {
			_, err := e.c.ImportSkills(e.ctx, entity.ImportSkillsRequest{WorkspaceID: skWS, UserID: other, URL: "https://github.com/a/b"})
			return err
		},
		"shares": func() error {
			_, err := e.c.ListSkillShares(e.ctx, entity.ListSkillSharesRequest{WorkspaceID: skWS, UserID: other, Name: "tdd"})
			return err
		},
		"share": func() error {
			return e.c.ShareSkill(e.ctx, entity.ShareSkillRequest{WorkspaceID: skWS, UserID: other, Name: "tdd", TargetWorkspaceID: skForgn})
		},
		"share into a foreign workspace": func() error {
			return e.c.ShareSkill(e.ctx, entity.ShareSkillRequest{WorkspaceID: skWS, UserID: skUserStr, Name: "tdd", TargetWorkspaceID: skForgn})
		},
		"unshare": func() error {
			return e.c.UnshareSkill(e.ctx, entity.ShareSkillRequest{WorkspaceID: skWS, UserID: other, Name: "tdd", TargetWorkspaceID: skWS2})
		},
	}
	for name, call := range calls {
		if err := call(); !errors.Is(err, base.ErrNotFound) {
			t.Errorf("%s: want ErrNotFound, got %v", name, err)
		}
	}
	if _, err := e.c.SearchSkills(e.ctx, entity.SearchSkillsRequest{WorkspaceID: 0, UserID: skUserStr}); err == nil {
		t.Error("workspace 0 must be refused")
	}
}

func TestSkills_Sharing(t *testing.T) {
	e := newSkillEnv(t)
	e.save(t, skWS, "tdd", "SKILL.md", md("tdd", "Test first."))
	e.save(t, skWS, "tdd", "notes.md", "red, green, refactor")
	share := entity.ShareSkillRequest{WorkspaceID: skWS, UserID: skUserStr, Name: "tdd", TargetWorkspaceID: skWS2}
	if err := e.c.ShareSkill(e.ctx, share); err != nil {
		t.Fatalf("share: %v", err)
	}
	// Sharing twice is the state already asked for.
	if err := e.c.ShareSkill(e.ctx, share); err != nil {
		t.Fatalf("share again: %v", err)
	}

	list, err := e.c.SearchSkills(e.ctx, entity.SearchSkillsRequest{WorkspaceID: skWS2, UserID: skUserStr})
	if err != nil || len(list.Skills) != 1 || list.Skills[0].SharedFromWorkspaceID != skWS {
		t.Fatalf("target list: %+v, %v", list, err)
	}
	// A live reference: a change at the source is what the target reads.
	e.save(t, skWS, "tdd", "notes.md", "updated")
	file, err := e.c.GetSkillFile(e.ctx, entity.GetSkillFileRequest{WorkspaceID: skWS2, UserID: skUserStr, Name: "tdd", Path: "notes.md"})
	if err != nil || file.File.Content != "updated" || file.Skill.SharedFromWorkspaceID != skWS {
		t.Fatalf("target read: %+v, %v", file, err)
	}
	got, err := e.c.GetSkill(e.ctx, entity.GetSkillRequest{WorkspaceID: skWS2, UserID: skUserStr, Name: "tdd"})
	if err != nil || got.Skill.SharedFromWorkspaceID != skWS || len(got.Skill.Files) != 2 {
		t.Fatalf("target get: %+v, %v", got, err)
	}

	// Read-only in the target, whichever way a write comes in.
	_, err = e.c.SaveSkillFile(e.ctx, entity.SaveSkillFileRequest{WorkspaceID: skWS2, UserID: skUserStr, Name: "tdd", Path: "notes.md", Content: "x"})
	wantSkillErr(t, err, SkillReadOnly, `workspace "alpha"`)
	_, err = e.c.SaveSkillFile(e.ctx, entity.SaveSkillFileRequest{WorkspaceID: skWS2, UserID: skUserStr, Name: "tdd", Path: "SKILL.md", Content: md("tdd", "mine now")})
	wantSkillErr(t, err, SkillReadOnly, "read-only")
	err = e.c.DeleteSkill(e.ctx, entity.DeleteSkillRequest{WorkspaceID: skWS2, UserID: skUserStr, Name: "tdd"})
	wantSkillErr(t, err, SkillReadOnly, "read-only")
	_, err = e.c.DeleteSkillFile(e.ctx, entity.DeleteSkillFileRequest{WorkspaceID: skWS2, UserID: skUserStr, Name: "tdd", Path: "notes.md"})
	wantSkillErr(t, err, SkillReadOnly, "read-only")
	err = e.c.ShareSkill(e.ctx, entity.ShareSkillRequest{WorkspaceID: skWS2, UserID: skUserStr, Name: "tdd", TargetWorkspaceID: skWS3})
	wantSkillErr(t, err, SkillReadOnly, "read-only")

	shares, err := e.c.ListSkillShares(e.ctx, entity.ListSkillSharesRequest{WorkspaceID: skWS, UserID: skUserStr, Name: "tdd"})
	if err != nil || len(shares.Shares) != 1 || shares.Shares[0].TargetWorkspaceID != skWS2 {
		t.Fatalf("shares: %+v, %v", shares, err)
	}

	if err := e.c.UnshareSkill(e.ctx, share); err != nil {
		t.Fatalf("unshare: %v", err)
	}
	if err := e.c.UnshareSkill(e.ctx, share); !errors.Is(err, base.ErrNotFound) {
		t.Errorf("unshare again: %v", err)
	}
	list, _ = e.c.SearchSkills(e.ctx, entity.SearchSkillsRequest{WorkspaceID: skWS2, UserID: skUserStr})
	if len(list.Skills) != 0 {
		t.Errorf("after unsharing the target still lists %+v", list.Skills)
	}
}

func TestSkills_ShareRefusals(t *testing.T) {
	e := newSkillEnv(t)
	e.save(t, skWS, "tdd", "SKILL.md", md("tdd", "Test first."))
	e.save(t, skWS2, "tdd", "SKILL.md", md("tdd", "Beta's own."))
	e.save(t, skWS3, "tdd", "SKILL.md", md("tdd", "Gamma's own."))

	err := e.c.ShareSkill(e.ctx, entity.ShareSkillRequest{WorkspaceID: skWS, UserID: skUserStr, Name: "tdd", TargetWorkspaceID: skWS})
	wantSkillErr(t, err, SkillInvalid, "already available")

	err = e.c.ShareSkill(e.ctx, entity.ShareSkillRequest{WorkspaceID: skWS, UserID: skUserStr, Name: "tdd", TargetWorkspaceID: skWS2})
	wantSkillErr(t, err, SkillConflict, "its own skill")

	// Two skills of one name cannot both be shared into the same workspace.
	e.save(t, skWS, "lint", "SKILL.md", md("lint", "Alpha lint."))
	e.save(t, skWS3, "lint", "SKILL.md", md("lint", "Gamma lint."))
	if err := e.c.ShareSkill(e.ctx, entity.ShareSkillRequest{WorkspaceID: skWS, UserID: skUserStr, Name: "lint", TargetWorkspaceID: skWS2}); err != nil {
		t.Fatal(err)
	}
	err = e.c.ShareSkill(e.ctx, entity.ShareSkillRequest{WorkspaceID: skWS3, UserID: skUserStr, Name: "lint", TargetWorkspaceID: skWS2})
	wantSkillErr(t, err, SkillConflict, "already shared into")

	// And a workspace cannot then create its own skill under a name it
	// already sees shared in.
	_, err = e.c.SaveSkillFile(e.ctx, entity.SaveSkillFileRequest{WorkspaceID: skWS2, UserID: skUserStr, Name: "lint", Path: "SKILL.md", Content: md("lint", "Mine.")})
	wantSkillErr(t, err, SkillReadOnly, "read-only")

	for _, name := range []string{"Bad Name", "missing"} {
		err := e.c.ShareSkill(e.ctx, entity.ShareSkillRequest{WorkspaceID: skWS, UserID: skUserStr, Name: name, TargetWorkspaceID: skWS3})
		if err == nil {
			t.Errorf("share %q: want an error", name)
		}
	}
	if _, err := e.c.ListSkillShares(e.ctx, entity.ListSkillSharesRequest{WorkspaceID: skWS, UserID: skUserStr, Name: "missing"}); !errors.Is(err, base.ErrNotFound) {
		t.Errorf("shares of a missing skill: %v", err)
	}
}

// Deleting a skill takes its shares with it, so the target stops seeing it.
func TestSkills_DeleteRemovesShares(t *testing.T) {
	e := newSkillEnv(t)
	e.save(t, skWS, "tdd", "SKILL.md", md("tdd", "Test first."))
	if err := e.c.ShareSkill(e.ctx, entity.ShareSkillRequest{WorkspaceID: skWS, UserID: skUserStr, Name: "tdd", TargetWorkspaceID: skWS2}); err != nil {
		t.Fatal(err)
	}
	if err := e.c.DeleteSkill(e.ctx, entity.DeleteSkillRequest{WorkspaceID: skWS, UserID: skUserStr, Name: "tdd"}); err != nil {
		t.Fatal(err)
	}
	var n int64
	e.db.Model(&model.SkillShare{}).Count(&n)
	if n != 0 {
		t.Errorf("%d shares outlived their skill", n)
	}
}

func githubResult(skills ...skillimport.Skill) *skillimport.Result {
	return &skillimport.Result{
		Repo: "obra/superpowers", Ref: "main", Commit: strings.Repeat("a", 40),
		Skills:  skills,
		Skipped: []skillimport.Skip{{Name: "brainstorming", Path: "skills/brainstorming", Reason: "SKILL.md is 100000 bytes; the limit is 98304 bytes (96 KiB)"}},
	}
}

func importedSkill(name, description string, extra ...skillimport.File) skillimport.Skill {
	files := append([]skillimport.File{{Path: "SKILL.md", Content: []byte(md(name, description))}}, extra...)
	return skillimport.Skill{Name: name, Description: description, Dir: "skills/" + name, Files: files}
}

func TestSkills_Import(t *testing.T) {
	e := newSkillEnv(t)
	e.save(t, skWS, "tdd", "SKILL.md", md("tdd", "Our own."))
	e.save(t, skWS2, "lint", "SKILL.md", md("lint", "Shared in."))
	if err := e.c.ShareSkill(e.ctx, entity.ShareSkillRequest{WorkspaceID: skWS2, UserID: skUserStr, Name: "lint", TargetWorkspaceID: skWS}); err != nil {
		t.Fatal(err)
	}
	e.importer.res = githubResult(
		importedSkill("systematic-debugging", "Debug.", skillimport.File{Path: "root-cause-tracing.md", Content: []byte("trace")}),
		importedSkill("tdd", "Theirs."),
		importedSkill("lint", "Theirs too."),
	)

	rs, err := e.c.ImportSkills(e.ctx, entity.ImportSkillsRequest{WorkspaceID: skWS, UserID: skUserStr, URL: "https://github.com/obra/superpowers"})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if e.importer.url != "https://github.com/obra/superpowers" {
		t.Errorf("importer got %q", e.importer.url)
	}
	if len(rs.Imported) != 1 || rs.Imported[0].Name != "systematic-debugging" || rs.Imported[0].FileCount != 2 ||
		rs.Imported[0].SourceType != "github" || rs.Imported[0].SourceRepo != "obra/superpowers" || rs.Imported[0].SourcePath != "skills/systematic-debugging" {
		t.Fatalf("imported: %+v", rs.Imported)
	}
	reasons := map[string]string{}
	for _, s := range rs.Skipped {
		reasons[s.Name] = s.Reason
	}
	for name, want := range map[string]string{"brainstorming": "96 KiB", "tdd": "overwrite", "lint": "shared into this workspace"} {
		if !strings.Contains(reasons[name], want) {
			t.Errorf("skipped %s: %q, want %q", name, reasons[name], want)
		}
	}

	// Overwrite replaces the workspace's own skill, keeping its identity, but
	// still never a shared-in one.
	before, _ := e.c.GetSkill(e.ctx, entity.GetSkillRequest{WorkspaceID: skWS, UserID: skUserStr, Name: "tdd"})
	rs, err = e.c.ImportSkills(e.ctx, entity.ImportSkillsRequest{WorkspaceID: skWS, UserID: skUserStr, URL: "u", Overwrite: true})
	if err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	if len(rs.Imported) != 2 {
		t.Fatalf("overwrite imported %+v", rs.Imported)
	}
	after, _ := e.c.GetSkill(e.ctx, entity.GetSkillRequest{WorkspaceID: skWS, UserID: skUserStr, Name: "tdd"})
	if after.Skill.ID != before.Skill.ID || after.Skill.Description != "Theirs." || after.Skill.SourceType != "github" {
		t.Errorf("overwritten: before %+v after %+v", before.Skill, after.Skill)
	}
	// Two skills, three files, and nothing left over from what was replaced.
	if n := e.blobs(t); n != 4 { // tdd, systematic-debugging ×2, and lint in skWS2
		t.Errorf("storage holds %d blobs, want 4", n)
	}

	// Editing an imported skill marks it, so a later re-sync knows.
	saved := e.save(t, skWS, "tdd", "notes.md", "ours")
	if !saved.Skill.LocallyModified {
		t.Error("editing an imported skill must mark it locally modified")
	}
	del, err := e.c.DeleteSkillFile(e.ctx, entity.DeleteSkillFileRequest{WorkspaceID: skWS, UserID: skUserStr, Name: "tdd", Path: "notes.md"})
	if err != nil || !del.Skill.LocallyModified {
		t.Errorf("delete file of an imported skill: %+v, %v", del, err)
	}
}

func TestSkills_ImportErrors(t *testing.T) {
	e := newSkillEnv(t)
	req := entity.ImportSkillsRequest{WorkspaceID: skWS, UserID: skUserStr, URL: "u"}

	e.importer.err = fmt.Errorf("%w: use a link like …", skillimport.ErrInvalidURL)
	_, err := e.c.ImportSkills(e.ctx, req)
	wantSkillErr(t, err, SkillInvalid, "invalid GitHub URL")

	e.importer.err = fmt.Errorf("%w: repository a/b does not exist", skillimport.ErrNotFound)
	_, err = e.c.ImportSkills(e.ctx, req)
	wantSkillErr(t, err, SkillInvalid, "does not exist")

	e.importer.err = errors.New("GitHub answered 502 downloading a/b")
	_, err = e.c.ImportSkills(e.ctx, req)
	wantSkillErr(t, err, SkillUpstream, "502")

	e.c.skillImport = nil
	_, err = e.c.ImportSkills(e.ctx, req)
	wantSkillErr(t, err, SkillUpstream, "not available")
}

// A repository too large to import whole imports nothing and passes on the
// skills it offers; the choice made from them reaches the importer.
func TestSkills_ImportOffersAndChooses(t *testing.T) {
	e := newSkillEnv(t)
	e.importer.res = &skillimport.Result{
		Repo: "garrytan/gstack", Ref: "main", Commit: "abc",
		Candidates: []skillimport.Candidate{{Name: "ship", Path: "ship", SkillBytes: 77710, Reason: "SKILL.md is too big"}, {Name: "careful", Path: "careful", SkillBytes: 3516}},
	}
	rs, err := e.c.ImportSkills(e.ctx, entity.ImportSkillsRequest{WorkspaceID: skWS, UserID: skUserStr, URL: "u"})
	if err != nil {
		t.Fatal(err)
	}
	want := []entity.SkillImportCandidate{{Name: "ship", Path: "ship", SkillBytes: 77710, Reason: "SKILL.md is too big"}, {Name: "careful", Path: "careful", SkillBytes: 3516}}
	if len(rs.Imported) != 0 || !slices.Equal(rs.Candidates, want) || rs.SourceRepo != "garrytan/gstack" {
		t.Fatalf("got %+v", rs)
	}

	e.importer.res = githubResult(importedSkill("careful", "Careful."))
	rs, err = e.c.ImportSkills(e.ctx, entity.ImportSkillsRequest{WorkspaceID: skWS, UserID: skUserStr, URL: "u", Skills: []string{"careful"}})
	if err != nil || len(rs.Imported) != 1 || len(rs.Candidates) != 0 || !slices.Equal(e.importer.only, []string{"careful"}) {
		t.Fatalf("got %+v, %v; importer saw %v", rs, err, e.importer.only)
	}

	_, err = e.c.ImportSkills(e.ctx, entity.ImportSkillsRequest{WorkspaceID: skWS, UserID: skUserStr, URL: "u", Skills: make([]string, skillimport.MaxSelected+1)})
	wantSkillErr(t, err, SkillInvalid, "at most 256 skills")
}

// A storage failure part-way through an import leaves no blob behind.
func TestSkills_ImportStorageFailureLeavesNothing(t *testing.T) {
	e := newSkillEnv(t)
	e.importer.res = githubResult(importedSkill("a", "A.", skillimport.File{Path: "b.md", Content: []byte("b")}))
	e.c.skillStorage = &failingStorage{Service: e.c.skillStorage, failAfter: 1}
	rs, err := e.c.ImportSkills(e.ctx, entity.ImportSkillsRequest{WorkspaceID: skWS, UserID: skUserStr, URL: "u"})
	if err != nil || len(rs.Imported) != 0 || !strings.Contains(rs.Skipped[len(rs.Skipped)-1].Reason, "could not be saved") {
		t.Fatalf("got %+v, %v", rs, err)
	}
	if n := e.blobs(t); n != 0 {
		t.Errorf("%d blobs leaked", n)
	}
}

type failingStorage struct {
	storage.Service
	failAfter int
	saves     int
}

func (f *failingStorage) Save(id, data string) error {
	f.saves++
	if f.saves > f.failAfter {
		return errors.New("disk full")
	}
	return f.Service.Save(id, data)
}

func TestSkills_SaveStorageFailure(t *testing.T) {
	e := newSkillEnv(t)
	e.c.skillStorage = &failingStorage{Service: e.c.skillStorage}
	_, err := e.c.SaveSkillFile(e.ctx, entity.SaveSkillFileRequest{WorkspaceID: skWS, UserID: skUserStr, Name: "tdd", Path: "SKILL.md", Content: md("tdd", "d")})
	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("got %v", err)
	}
	var n int64
	e.db.Model(&model.Skill{}).Count(&n)
	if n != 0 {
		t.Error("a skill row was written although its file never was")
	}
}

// failOn makes the next statement against table fail, for the paths where
// the database itself goes wrong.
func failOn(db *gorm.DB, op, table string) {
	fail := func(tx *gorm.DB) {
		if tx.Statement.Table == table {
			_ = tx.AddError(errors.New("injected " + op + " failure on " + table))
		}
	}
	name := "test:fail:" + op + ":" + table + fmt.Sprint(time.Now().UnixNano())
	switch op {
	case "query":
		db.Callback().Query().Before("gorm:query").Register(name, fail)
	case "subquery":
		// A table read only inside a subquery never names the statement, so
		// fail the query once its SQL is built and mentions the table.
		db.Callback().Query().After("gorm:query").Register(name, func(tx *gorm.DB) {
			if strings.Contains(tx.Statement.SQL.String(), "`"+table+"`") {
				_ = tx.AddError(errors.New("injected query failure on " + table))
			}
		})
	case "target":
		// Fail only the lookups made about the share target, skWS3, so the
		// checks on the source skill before them still pass.
		db.Callback().Query().After("gorm:query").Register(name, func(tx *gorm.DB) {
			for _, v := range tx.Statement.Vars {
				if v == skWS3 && strings.Contains(tx.Statement.SQL.String(), "`"+table+"`") {
					_ = tx.AddError(errors.New("injected query failure on " + table))
					return
				}
			}
		})
	case "create":
		db.Callback().Create().Before("gorm:create").Register(name, fail)
	case "update":
		db.Callback().Update().Before("gorm:update").Register(name, fail)
	case "delete":
		db.Callback().Delete().Before("gorm:delete").Register(name, fail)
	}
}

func TestSkills_DatabaseFailures(t *testing.T) {
	type call func(e *skillEnv) error
	list := func(e *skillEnv) error {
		_, err := e.c.SearchSkills(e.ctx, entity.SearchSkillsRequest{WorkspaceID: skWS, UserID: skUserStr})
		return err
	}
	get := func(e *skillEnv) error {
		_, err := e.c.GetSkill(e.ctx, entity.GetSkillRequest{WorkspaceID: skWS, UserID: skUserStr, Name: "tdd"})
		return err
	}
	getShared := func(e *skillEnv) error {
		_, err := e.c.GetSkill(e.ctx, entity.GetSkillRequest{WorkspaceID: skWS, UserID: skUserStr, Name: "not-here"})
		return err
	}
	file := func(e *skillEnv) error {
		_, err := e.c.GetSkillFile(e.ctx, entity.GetSkillFileRequest{WorkspaceID: skWS, UserID: skUserStr, Name: "tdd", Path: "SKILL.md"})
		return err
	}
	saveSub := func(e *skillEnv) error {
		_, err := e.c.SaveSkillFile(e.ctx, entity.SaveSkillFileRequest{WorkspaceID: skWS, UserID: skUserStr, Name: "tdd", Path: "n.md", Content: "x"})
		return err
	}
	saveNew := func(e *skillEnv) error {
		_, err := e.c.SaveSkillFile(e.ctx, entity.SaveSkillFileRequest{WorkspaceID: skWS, UserID: skUserStr, Name: "fresh", Path: "SKILL.md", Content: md("fresh", "d")})
		return err
	}
	del := func(e *skillEnv) error {
		return e.c.DeleteSkill(e.ctx, entity.DeleteSkillRequest{WorkspaceID: skWS, UserID: skUserStr, Name: "tdd"})
	}
	delFile := func(e *skillEnv) error {
		_, err := e.c.DeleteSkillFile(e.ctx, entity.DeleteSkillFileRequest{WorkspaceID: skWS, UserID: skUserStr, Name: "tdd", Path: "notes.md"})
		return err
	}
	imp := func(e *skillEnv) error {
		e.importer.res = githubResult(importedSkill("new-one", "New."))
		_, err := e.c.ImportSkills(e.ctx, entity.ImportSkillsRequest{WorkspaceID: skWS, UserID: skUserStr, URL: "u"})
		return err
	}
	shares := func(e *skillEnv) error {
		_, err := e.c.ListSkillShares(e.ctx, entity.ListSkillSharesRequest{WorkspaceID: skWS, UserID: skUserStr, Name: "tdd"})
		return err
	}
	turnOff := func(e *skillEnv) error {
		_, err := e.c.SetSkillEnabled(e.ctx, entity.SetSkillEnabledRequest{WorkspaceID: skWS, UserID: skUserStr, Name: "tdd"})
		return err
	}
	share := func(e *skillEnv) error {
		return e.c.ShareSkill(e.ctx, entity.ShareSkillRequest{WorkspaceID: skWS, UserID: skUserStr, Name: "tdd", TargetWorkspaceID: skWS3})
	}

	for _, tc := range []struct {
		name      string
		op, table string
		call      call
	}{
		{"list: own", "query", "skills", list},
		{"list: shared", "subquery", "skill_shares", list},
		{"access check", "query", "workspaces", list},
		{"get: skill", "query", "skills", get},
		{"get: shared", "subquery", "skill_shares", getShared},
		{"get: files", "query", "skill_files", get},
		{"file: row", "query", "skill_files", file},
		{"save: file lookup", "query", "skill_files", saveSub},
		{"save: write", "create", "skill_files", saveSub},
		{"save: new skill", "create", "skills", saveNew},
		{"save: resolve", "query", "skills", saveSub},
		{"delete", "delete", "skill_files", del},
		{"delete file", "delete", "skill_files", delFile},
		{"import: own", "query", "skills", imp},
		{"import: shared", "subquery", "skill_shares", imp},
		{"shares", "query", "skill_shares", shares},
		{"turn off: resolve", "query", "skills", turnOff},
		{"turn off: write", "update", "skills", turnOff},
		{"share: target access", "target", "workspaces", share},
		{"share: target's own skill", "target", "skills", share},
		{"share: target's shared skills", "subquery", "skill_shares", share},
		{"share: write", "create", "skill_shares", share},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newSkillEnv(t)
			e.save(t, skWS, "tdd", "SKILL.md", md("tdd", "Test first."))
			e.save(t, skWS, "tdd", "notes.md", "n")
			blobs := e.blobs(t)
			failOn(e.db, tc.op, tc.table)
			if err := tc.call(e); err == nil {
				t.Fatal("want the injected failure")
			}
			if n := e.blobs(t); n != blobs {
				t.Errorf("storage went from %d to %d blobs across a failed call", blobs, n)
			}
		})
	}
}

func TestSkills_ListIsSortedByName(t *testing.T) {
	e := newSkillEnv(t)
	e.save(t, skWS, "zeta", "SKILL.md", md("zeta", "Z."))
	e.save(t, skWS, "alpha", "SKILL.md", md("alpha", "A."))
	e.save(t, skWS2, "mid", "SKILL.md", md("mid", "M."))
	if err := e.c.ShareSkill(e.ctx, entity.ShareSkillRequest{WorkspaceID: skWS2, UserID: skUserStr, Name: "mid", TargetWorkspaceID: skWS}); err != nil {
		t.Fatalf("share: %v", err)
	}
	list, err := e.c.SearchSkills(e.ctx, entity.SearchSkillsRequest{WorkspaceID: skWS, UserID: skUserStr})
	if err != nil {
		t.Fatalf("SearchSkills: %v", err)
	}
	var names []string
	for _, s := range list.Skills {
		names = append(names, s.Name)
	}
	if strings.Join(names, ",") != "alpha,mid,zeta" {
		t.Errorf("got %v, want [alpha mid zeta]", names)
	}
}

// Two creators racing for one name: the loser hears it as a conflict, and
// its blob is cleaned up.
func TestSkills_SaveRaceIsAConflict(t *testing.T) {
	e := newSkillEnv(t)
	e.save(t, skWS, "tdd", "SKILL.md", md("tdd", "First."))
	// Pretend this caller looked before the other one wrote: the controller
	// believes the name is free and tries to insert.
	e.db.Callback().Query().Before("gorm:query").Register("test:hide-skills", func(tx *gorm.DB) {
		if tx.Statement.Table == "skills" && tx.Statement.SQL.Len() == 0 {
			tx.Statement.AddClause(clause.Where{Exprs: []clause.Expression{clause.Expr{SQL: "1 = 0"}}})
		}
	})
	_, err := e.c.SaveSkillFile(e.ctx, entity.SaveSkillFileRequest{WorkspaceID: skWS, UserID: skUserStr, Name: "tdd", Path: "SKILL.md", Content: md("tdd", "Second.")})
	wantSkillErr(t, err, SkillConflict, "a moment ago")
	if n := e.blobs(t); n != 1 {
		t.Errorf("storage holds %d blobs, want 1", n)
	}
}

// Review regressions.

// A skill that cannot be saved is reported with the rest, not by failing the
// import after the ones before it were already kept.
func TestSkills_ImportReportsASkillThatCouldNotBeSaved(t *testing.T) {
	e := newSkillEnv(t)
	e.importer.res = githubResult(importedSkill("first", "One."), importedSkill("second", "Two."))
	e.db.Callback().Create().Before("gorm:create").Register("test:fail-second", func(tx *gorm.DB) {
		if s, ok := tx.Statement.Dest.(*model.Skill); ok && s.Name == "second" {
			_ = tx.AddError(errors.New("disk full"))
		}
	})
	blobs := e.blobs(t)
	rs, err := e.c.ImportSkills(e.ctx, entity.ImportSkillsRequest{WorkspaceID: skWS, UserID: skUserStr, URL: "u"})
	if err != nil {
		t.Fatalf("ImportSkills: %v", err)
	}
	if len(rs.Imported) != 1 || rs.Imported[0].Name != "first" {
		t.Errorf("imported: %+v", rs.Imported)
	}
	var reason string
	for _, s := range rs.Skipped {
		if s.Name == "second" {
			reason = s.Reason
		}
	}
	if !strings.Contains(reason, "could not be saved") {
		t.Errorf("skipped: %+v", rs.Skipped)
	}
	// The failed skill's blobs are gone again; only the first skill's stay.
	if n := e.blobs(t); n != blobs+1 {
		t.Errorf("storage holds %d blobs, want %d", n, blobs+1)
	}
}

func TestSkills_ImportIsRateLimited(t *testing.T) {
	e := newSkillEnv(t)
	e.c.limiter = &stubLimiter{}
	e.importer.res = githubResult()
	if _, err := e.c.ImportSkills(e.ctx, entity.ImportSkillsRequest{WorkspaceID: skWS, UserID: skUserStr, URL: "u"}); err == nil || err.Error() != "rate limit exceeded" {
		t.Fatalf("got %v", err)
	}
	if e.importer.url != "" {
		t.Error("a refused import still reached GitHub")
	}
}

func TestSkills_Search(t *testing.T) {
	e := newSkillEnv(t)
	e.save(t, skWS, "pr-reviewer", "SKILL.md", md("pr-reviewer", "Reviews Pull Requests."))
	e.save(t, skWS, "tdd", "SKILL.md", md("tdd", "Write the failing test first."))
	e.save(t, skWS, "percent", "SKILL.md", md("percent", "Handles 100% of cases."))
	e.save(t, skWS2, "reviewed-by-ops", "SKILL.md", md("reviewed-by-ops", "Shared in from beta."))
	e.save(t, skWS3, "unshared-review", "SKILL.md", md("unshared-review", "Not visible to alpha."))
	if err := e.c.ShareSkill(e.ctx, entity.ShareSkillRequest{WorkspaceID: skWS2, UserID: skUserStr, Name: "reviewed-by-ops", TargetWorkspaceID: skWS}); err != nil {
		t.Fatal(err)
	}
	search := func(q string, limit, offset int) *entity.SearchSkillsResponse {
		t.Helper()
		rs, err := e.c.SearchSkills(e.ctx, entity.SearchSkillsRequest{WorkspaceID: skWS, UserID: skUserStr, Query: q, Limit: limit, Offset: offset})
		if err != nil {
			t.Fatalf("SearchSkills(%q): %v", q, err)
		}
		return rs
	}
	names := func(rs *entity.SearchSkillsResponse) string {
		var out []string
		for _, s := range rs.Skills {
			out = append(out, s.Name)
		}
		return strings.Join(out, ",")
	}

	for _, tc := range []struct{ q, want string }{
		{"", "percent,pr-reviewer,reviewed-by-ops,tdd"},
		{"review", "pr-reviewer,reviewed-by-ops"}, // name, own and shared-in
		{"PULL requests", "pr-reviewer"},          // description, any case
		{"  failing  ", "tdd"},                    // trimmed
		{"100%", "percent"},                       // % matches itself
		{"r_v", ""},                               // so does _
		{"nothing-here", ""},
	} {
		rs := search(tc.q, 0, 0)
		if got := names(rs); got != tc.want || rs.Total != len(rs.Skills) {
			t.Errorf("q %q: got %q (total %d), want %q", tc.q, got, rs.Total, tc.want)
		}
	}

	// A shared-in skill says where it came from.
	if rs := search("reviewed", 0, 0); rs.Skills[0].SharedFromWorkspaceID != skWS2 {
		t.Errorf("shared from: %+v", rs.Skills[0])
	}

	// Pages by name, with the total of every match.
	if rs := search("", 2, 1); names(rs) != "pr-reviewer,reviewed-by-ops" || rs.Total != 4 {
		t.Errorf("page: %q total %d", names(rs), rs.Total)
	}
	if rs := search("", 2, 10); len(rs.Skills) != 0 || rs.Total != 4 {
		t.Errorf("past the end: %+v", rs)
	}
	if rs := search("", 1000, 0); len(rs.Skills) != 4 {
		t.Errorf("a large limit is capped, not refused: %d", len(rs.Skills))
	}
}

func TestSkills_SearchRefusesBadInput(t *testing.T) {
	e := newSkillEnv(t)
	for _, tc := range []struct {
		rq   entity.SearchSkillsRequest
		want string
	}{
		{entity.SearchSkillsRequest{Query: "ab"}, "at least 3 characters"},
		{entity.SearchSkillsRequest{Query: " ab "}, "at least 3 characters"},
		{entity.SearchSkillsRequest{Query: strings.Repeat("q", MaxSkillQueryLength+1)}, "the limit is 256"},
		{entity.SearchSkillsRequest{Limit: -1}, "cannot be negative"},
		{entity.SearchSkillsRequest{Offset: -1}, "cannot be negative"},
	} {
		tc.rq.WorkspaceID, tc.rq.UserID = skWS, skUserStr
		_, err := e.c.SearchSkills(e.ctx, tc.rq)
		wantSkillErr(t, err, SkillInvalid, tc.want)
	}
	// Exactly the minimum is a search.
	if _, err := e.c.SearchSkills(e.ctx, entity.SearchSkillsRequest{WorkspaceID: skWS, UserID: skUserStr, Query: "tdd"}); err != nil {
		t.Errorf("three characters: %v", err)
	}
}

func TestNew_SkillStorage(t *testing.T) {
	local, _ := storage.New(t.TempDir())
	skills, _ := storage.New(t.TempDir())
	if c := New(Params{Storage: local}).(*controller); c.skillStorage != local {
		t.Error("with no SkillStorage, skills should use Storage")
	}
	if c := New(Params{Storage: local, SkillStorage: skills}).(*controller); c.skillStorage != skills || c.storage != local {
		t.Error("SkillStorage was not used for skills alone")
	}
}

// A skill turned off stays in the workspace and in the interface; only the
// agent's view of the list leaves it out.
func TestSkills_TurnOffAndOn(t *testing.T) {
	e := newSkillEnv(t)
	e.save(t, skWS, "tdd", "SKILL.md", md("tdd", "Test first."))
	e.save(t, skWS, "debugging", "SKILL.md", md("debugging", "Find the cause."))
	if err := e.c.ShareSkill(e.ctx, entity.ShareSkillRequest{WorkspaceID: skWS, UserID: skUserStr, Name: "tdd", TargetWorkspaceID: skWS2}); err != nil {
		t.Fatal(err)
	}
	set := func(ws int64, name string, enabled bool) (*entity.SetSkillEnabledResponse, error) {
		return e.c.SetSkillEnabled(e.ctx, entity.SetSkillEnabledRequest{WorkspaceID: ws, UserID: skUserStr, Name: name, Enabled: enabled})
	}
	names := func(ws int64, q string, enabledOnly bool) []string {
		t.Helper()
		rs, err := e.c.SearchSkills(e.ctx, entity.SearchSkillsRequest{WorkspaceID: ws, UserID: skUserStr, Query: q, EnabledOnly: enabledOnly})
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, s := range rs.Skills {
			out = append(out, fmt.Sprintf("%s:%t", s.Name, s.Enabled))
		}
		if rs.Total != len(out) {
			t.Errorf("total %d for %d skills", rs.Total, len(out))
		}
		return out
	}

	if got := names(skWS, "", true); !slices.Equal(got, []string{"debugging:true", "tdd:true"}) {
		t.Fatalf("a new skill is on: %v", got)
	}
	rs, err := set(skWS, "TDD", false)
	if err != nil || rs.Skill.Name != "tdd" || rs.Skill.Enabled {
		t.Fatalf("turn off: %+v, %v", rs, err)
	}
	if got := names(skWS, "", false); !slices.Equal(got, []string{"debugging:true", "tdd:false"}) {
		t.Errorf("the interface still lists it, as off: %v", got)
	}
	if got := names(skWS, "", true); !slices.Equal(got, []string{"debugging:true"}) {
		t.Errorf("an agent's list leaves it out: %v", got)
	}
	if got := names(skWS, "test", true); len(got) != 0 {
		t.Errorf("a search does not find it either: %v", got)
	}
	// Off wherever it is shared, and only its owner can turn it back on.
	if got := names(skWS2, "", true); len(got) != 0 {
		t.Errorf("the share target's agent still sees %v", got)
	}
	_, err = set(skWS2, "tdd", true)
	wantSkillErr(t, err, SkillReadOnly, `workspace "alpha"`)

	// Turning it off again is the state already asked for; editing it, or
	// importing over it, keeps it off.
	if rs, err := set(skWS, "tdd", false); err != nil || rs.Skill.Enabled {
		t.Fatalf("turn off again: %+v, %v", rs, err)
	}
	if saved := e.save(t, skWS, "tdd", "notes.md", "n"); saved.Skill.Enabled {
		t.Error("saving a file turned the skill back on")
	}
	e.importer.res = githubResult(importedSkill("tdd", "Theirs."))
	if imp, err := e.c.ImportSkills(e.ctx, entity.ImportSkillsRequest{WorkspaceID: skWS, UserID: skUserStr, URL: "u", Overwrite: true}); err != nil || len(imp.Imported) != 1 || imp.Imported[0].Enabled {
		t.Fatalf("an import over it turned it back on: %+v, %v", imp, err)
	}
	got, err := e.c.GetSkill(e.ctx, entity.GetSkillRequest{WorkspaceID: skWS, UserID: skUserStr, Name: "tdd"})
	if err != nil || got.Skill.Enabled || got.Skill.Description != "Theirs." {
		t.Fatalf("get: %+v, %v", got, err)
	}

	if rs, err := set(skWS, "tdd", true); err != nil || !rs.Skill.Enabled {
		t.Fatalf("turn on: %+v, %v", rs, err)
	}
	if got := names(skWS2, "", true); !slices.Equal(got, []string{"tdd:true"}) {
		t.Errorf("back on in the share target: %v", got)
	}
}

func TestSkills_TurnOffRefusals(t *testing.T) {
	e := newSkillEnv(t)
	_, err := e.c.SetSkillEnabled(e.ctx, entity.SetSkillEnabledRequest{WorkspaceID: skWS, UserID: skUserStr, Name: "Bad Name"})
	wantSkillErr(t, err, SkillInvalid, "not usable")
	if _, err := e.c.SetSkillEnabled(e.ctx, entity.SetSkillEnabledRequest{WorkspaceID: skWS, UserID: skUserStr, Name: "missing"}); !errors.Is(err, base.ErrNotFound) {
		t.Errorf("missing skill: %v", err)
	}
}
