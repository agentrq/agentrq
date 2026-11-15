// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package crud

import (
	"errors"
	"strings"
	"testing"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/mustafaturan/monoflake"
	"gorm.io/gorm"
)

// Links are tested against the real database: whether one still reads its
// skill is a join across the team tables.

var skOtherStr = monoflake.ID(skOther).String()

// linkEnv is skOther's skill deploy, two files, on in skForgn, which a team
// of both accounts is shared, and skUser's link to it from before team
// workspaces owned skills, on in skWS.
type linkEnv struct {
	*skillEnv
	src  int64 // skOther's deploy
	link int64 // skUser's link
}

const skLink = 7001

func newLinkEnv(t *testing.T) *linkEnv {
	t.Helper()
	e := newSkillEnv(t)
	for _, row := range []any{
		&model.TeamMember{ID: 1, TeamID: 9, UserID: skUser},
		&model.TeamMember{ID: 2, TeamID: 9, UserID: skOther},
		&model.WorkspaceShare{ID: 3, TeamID: 9, WorkspaceID: skForgn, UserID: skOther},
	} {
		if err := e.db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	rs, err := e.c.SaveSkillFile(e.ctx, entity.SaveSkillFileRequest{WorkspaceID: skForgn, UserID: skOtherStr, Name: "deploy", Path: "SKILL.md", Content: md("deploy", "Ship it.")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.c.SaveSkillFile(e.ctx, entity.SaveSkillFileRequest{WorkspaceID: skForgn, UserID: skOtherStr, Name: "deploy", Path: "run.md", Content: "run"}); err != nil {
		t.Fatal(err)
	}
	for _, row := range []any{
		&model.Skill{ID: skLink, UserID: skUser, Name: "deploy", SourceType: "team", LinkedSkillID: rs.Skill.ID},
		&model.WorkspaceSkill{ID: 7002, UserID: skUser, SkillID: skLink, WorkspaceID: skWS},
	} {
		if err := e.db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	return &linkEnv{skillEnv: e, src: rs.Skill.ID, link: skLink}
}

func (e *linkEnv) linkRow(t *testing.T) (model.Skill, bool) {
	t.Helper()
	var s model.Skill
	err := e.db.First(&s, e.link).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.Skill{}, false
	}
	if err != nil {
		t.Fatal(err)
	}
	return s, true
}

// A link that still reads its skill becomes a skill of its own: a copy of
// every file, in the account's own storage, under its name, on where it was.
// The teammate's skill is untouched, and a second start finds nothing to do.
func TestConvertSkillLinks_CopiesWhatALinkReads(t *testing.T) {
	e := newLinkEnv(t)
	blobs := e.blobs(t)
	if err := e.c.ConvertSkillLinks(e.ctx); err != nil {
		t.Fatal(err)
	}
	own, ok := e.linkRow(t)
	if !ok || own.LinkedSkillID != 0 || own.SourceType != skillSourceManual || own.Description != "Ship it." || own.FileCount != 2 {
		t.Fatalf("the converted link is %+v (%v)", own, ok)
	}
	if n := e.blobs(t); n != blobs+2 {
		t.Errorf("storage went from %d to %d blobs, want a copy of both files", blobs, n)
	}
	got, err := e.c.GetSkill(e.ctx, entity.GetSkillRequest{WorkspaceID: skWS, UserID: skUserStr, Name: "deploy"})
	if err != nil || got.Skill.ID != e.link || len(got.Skill.Files) != 2 || !got.Skill.WorkspaceEnabled {
		t.Fatalf("the converted skill reads %+v, %v", got, err)
	}
	var copied []model.SkillFile
	e.db.Where("skill_id = ?", e.link).Find(&copied)
	for _, f := range copied {
		if f.Version != 1 || !strings.HasPrefix(f.StorageID, "u-"+skUserStr+"/skill-") {
			t.Errorf("a copied file is %+v: want version 1, in the account's own folder", f)
		}
	}
	if len(copied) != 2 {
		t.Errorf("the converted skill has %d files of its own, want 2", len(copied))
	}
	f, err := e.c.GetSkillFile(e.ctx, entity.GetSkillFileRequest{WorkspaceID: skWS, UserID: skUserStr, Name: "deploy", Path: "run.md"})
	if err != nil || f.File.Content != "run" {
		t.Errorf("the copied run.md = %+v, %v", f, err)
	}
	if _, err := e.c.SaveSkillFile(e.ctx, entity.SaveSkillFileRequest{WorkspaceID: skWS, UserID: skUserStr, Name: "deploy", Path: "run.md", Content: "mine"}); err != nil {
		t.Fatal(err)
	}
	theirs, err := e.c.GetSkillFile(e.ctx, entity.GetSkillFileRequest{WorkspaceID: skForgn, UserID: skOtherStr, Name: "deploy", Path: "run.md"})
	if err != nil || theirs.File.Content != "run" {
		t.Errorf("a write to the copy reached the teammate's skill: %+v, %v", theirs, err)
	}

	blobs = e.blobs(t)
	if err := e.c.ConvertSkillLinks(e.ctx); err != nil || e.blobs(t) != blobs {
		t.Errorf("a second run returned %v and went from %d to %d blobs", err, blobs, e.blobs(t))
	}
}

// A link that reads nothing any more was invisible already, and goes with
// where it was on: nothing of a skill no longer shared is copied.
func TestConvertSkillLinks_DeletesALinkThatReadsNothing(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(e *linkEnv)
	}{
		{"its team no longer shares the skill", func(e *linkEnv) {
			e.db.Model(&model.WorkspaceShare{}).Where("id = ?", 3).UpdateColumn("hide_skills", true)
		}},
		{"its skill is gone", func(e *linkEnv) {
			e.db.Exec("DELETE FROM skills WHERE id = ?", e.src)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newLinkEnv(t)
			tc.setup(e)
			blobs := e.blobs(t)
			if err := e.c.ConvertSkillLinks(e.ctx); err != nil {
				t.Fatal(err)
			}
			if _, ok := e.linkRow(t); ok {
				t.Error("the link is still there")
			}
			var on int64
			e.db.Model(&model.WorkspaceSkill{}).Where("skill_id = ?", e.link).Count(&on)
			if on != 0 || e.blobs(t) != blobs {
				t.Errorf("the deleted link is on in %d workspaces, and storage went from %d to %d blobs", on, blobs, e.blobs(t))
			}
		})
	}
}

// A link that cannot be converted is left as it was, for the next start,
// with no copies behind it; the run carries on.
func TestConvertSkillLinks_LeavesALinkThatFails(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(e *linkEnv)
	}{
		{"whether it reads its skill cannot be read", func(e *linkEnv) { failOn(e.db, "query", "workspace_skills") }},
		{"its skill cannot be read", failSourceRead},
		{"its files cannot be listed", func(e *linkEnv) { failOn(e.db, "query", "skill_files") }},
		{"a blob is missing", func(e *linkEnv) {
			var f model.SkillFile
			e.db.Where("skill_id = ? AND path = ?", e.src, "run.md").First(&f)
			_ = e.c.skillStorage.Delete(f.StorageID)
		}},
		{"the storage is full", func(e *linkEnv) { e.c.skillStorage = &failingStorage{Service: e.c.skillStorage, failAfter: 0} }},
		{"the storage fills on the second file", func(e *linkEnv) { e.c.skillStorage = &failingStorage{Service: e.c.skillStorage, failAfter: 1} }},
		{"the conversion cannot be written", func(e *linkEnv) { failOn(e.db, "update", "skills") }},
		{"the deletion cannot be written", func(e *linkEnv) {
			e.db.Model(&model.WorkspaceShare{}).Where("id = ?", 3).UpdateColumn("hide_skills", true)
			failOn(e.db, "delete", "skills")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newLinkEnv(t)
			blobs := e.blobs(t)
			tc.setup(e)
			if err := e.c.ConvertSkillLinks(e.ctx); err != nil {
				t.Fatalf("one link failing failed the whole run: %v", err)
			}
			if l, ok := e.linkRow(t); !ok || l.LinkedSkillID != e.src {
				t.Errorf("the link is now %+v (%v), want it as it was", l, ok)
			}
			if n := e.blobs(t); n > blobs {
				t.Errorf("storage went from %d to %d blobs; a failed conversion left its copies behind", blobs, n)
			}
		})
	}
}

// failSourceRead fails the read of the link's skill by id, after the check
// that it still reads it has passed.
func failSourceRead(e *linkEnv) {
	e.db.Callback().Query().After("gorm:query").Register("test:fail-source-read", func(tx *gorm.DB) {
		if tx.Statement.Table != "skills" {
			return
		}
		for _, v := range tx.Statement.Vars {
			if v == e.src {
				_ = tx.AddError(errors.New("database unavailable"))
				return
			}
		}
	})
}

// Another instance converting the same link first: this one's copies go,
// and its own conversion changes nothing.
func TestConvertSkillLinks_LosingARaceLeavesNoCopies(t *testing.T) {
	e := newLinkEnv(t)
	converted := false
	e.db.Callback().Query().After("gorm:query").Register("test:converted-elsewhere", func(tx *gorm.DB) {
		if tx.Statement.Table == "skill_files" && !converted {
			converted = true
			e.db.Exec("UPDATE skills SET linked_skill_id = 0 WHERE id = ?", e.link)
		}
	})
	blobs := e.blobs(t)
	if err := e.c.ConvertSkillLinks(e.ctx); err != nil {
		t.Fatal(err)
	}
	if n := e.blobs(t); n != blobs {
		t.Errorf("storage went from %d to %d blobs after losing the race", blobs, n)
	}
}

func TestConvertSkillLinks_ReportsWhenLinksCannotBeListed(t *testing.T) {
	e := newLinkEnv(t)
	failOn(e.db, "query", "skills")
	if err := e.c.ConvertSkillLinks(e.ctx); err == nil || !strings.Contains(err.Error(), "list skill links") {
		t.Errorf("ConvertSkillLinks = %v, want the listing failure", err)
	}
}

// A refused write says what the file is now, and what to do about it.
func TestSkillVersionConflict(t *testing.T) {
	e := newLinkEnv(t)
	read := int64(1)
	for _, tc := range []struct {
		what, path, want string
		read             *int64
		shared           bool
	}{
		{"a file deleted since", "gone.md", "was deleted after you read it", nil, false},
		{"a version read", "run.md", "saved by someone else after version 1", &read, false},
		{"a team skill's, with no version", "run.md", "is shared with your team", nil, true},
		{"a race with no version", "run.md", "saved by someone else at the same time; it is now version 1", nil, false},
	} {
		err := e.c.skillVersionConflict(e.ctx, e.src, "deploy", tc.path, tc.read, tc.shared)
		var se *SkillError
		if !errors.As(err, &se) || se.Kind != SkillConflict || !strings.Contains(se.Message, tc.want) {
			t.Errorf("%s: %v, want a conflict saying %q", tc.what, err, tc.want)
		}
	}
	failOn(e.db, "query", "skill_files")
	if err := e.c.skillVersionConflict(e.ctx, e.src, "deploy", "run.md", nil, false); err == nil || errors.As(err, new(*SkillError)) {
		t.Errorf("with the file unreadable: %v, want the failure itself", err)
	}
}

// A save of an own skill's file on a version it no longer is is refused
// with that version, and stores nothing.
func TestSaveSkillFile_StaleVersion(t *testing.T) {
	e := newLinkEnv(t)
	blobs := e.blobs(t)
	stale := int64(5)
	_, err := e.c.SaveSkillFile(e.ctx, entity.SaveSkillFileRequest{WorkspaceID: skForgn, UserID: skOtherStr, Name: "deploy", Path: "run.md", Content: "x", Version: &stale})
	wantSkillErr(t, err, SkillConflict, "saved by someone else after version 5; it is now version 1")
	if n := e.blobs(t); n != blobs {
		t.Errorf("storage went from %d to %d blobs after a refused save", blobs, n)
	}
}
