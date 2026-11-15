// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package crud

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/storage"
	"github.com/mustafaturan/monoflake"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// A link is tested against the real database too: whether it reaches its
// skill is a join across the team tables.

var skOtherStr = monoflake.ID(skOther).String()

// linkEnv is skOther's skill deploy, on in skForgn, which a team of both
// accounts is shared, and skUser's link to it, on in skWS.
type linkEnv struct {
	*skillEnv
	src  int64 // skOther's deploy
	link int64 // skUser's link
}

func newLinkEnv(t *testing.T) *linkEnv {
	t.Helper()
	e := newSkillEnv(t)
	store, err := storage.NewNestedPublic(e.dir, "u")
	if err != nil {
		t.Fatal(err)
	}
	e.c.skillStorage = store
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
	ln, err := e.c.LinkSkill(e.ctx, entity.LinkSkillRequest{SkillID: rs.Skill.ID, UserID: skUserStr, WorkspaceIDs: []int64{skWS}})
	if err != nil {
		t.Fatal(err)
	}
	return &linkEnv{skillEnv: e, src: rs.Skill.ID, link: ln.Skill.ID}
}

func (e *linkEnv) saveAs(user string, ws int64, path, content string, version *int64) (*entity.SaveSkillFileResponse, error) {
	return e.c.SaveSkillFile(e.ctx, entity.SaveSkillFileRequest{WorkspaceID: ws, UserID: user, Name: "deploy", Path: path, Content: content, Version: version})
}

func (e *linkEnv) fileAs(t *testing.T, user string, ws int64, path string) *entity.GetSkillFileResponse {
	t.Helper()
	rs, err := e.c.GetSkillFile(e.ctx, entity.GetSkillFileRequest{WorkspaceID: ws, UserID: user, Name: "deploy", Path: path})
	if err != nil {
		t.Fatalf("read %s as %s: %v", path, user, err)
	}
	return rs
}

func (e *linkEnv) unshare(t *testing.T) {
	t.Helper()
	if err := e.db.Model(&model.WorkspaceShare{}).Where("id = ?", 3).UpdateColumn("hide_skills", true).Error; err != nil {
		t.Fatal(err)
	}
}

func version(n int64) *int64 { return &n }

// A link reads its teammate's skill: its files, their versions and content,
// shared, and with no public link to keep once the team stops sharing it.
func TestSkillLink_Reads(t *testing.T) {
	e := newLinkEnv(t)
	var link model.Skill
	if err := e.db.First(&link, e.link).Error; err != nil || link.LinkedSkillID != e.src || link.SourceType != skillSourceTeam ||
		link.Description != "Ship it." || link.FileCount != 2 {
		t.Fatalf("the link row is %+v (%v)", link, err)
	}
	var files int64
	e.db.Model(&model.SkillFile{}).Where("skill_id = ?", e.link).Count(&files)
	if files != 0 {
		t.Errorf("the link holds %d files of its own, want none", files)
	}

	got, err := e.c.GetSkill(e.ctx, entity.GetSkillRequest{WorkspaceID: skWS, UserID: skUserStr, Name: "deploy"})
	if err != nil || got.Skill.ID != e.link || len(got.Skill.Files) != 2 || !got.Skill.WorkspaceEnabled {
		t.Fatalf("GetSkill through the link = %+v, %v", got, err)
	}
	for _, f := range got.Skill.Files {
		if f.URL != "" || f.Version != 1 {
			t.Errorf("a linked file is %+v: want version 1 and no public link", f)
		}
	}

	f := e.fileAs(t, skUserStr, skWS, "SKILL.md")
	if !strings.Contains(f.File.Content, "Ship it.") || !f.Shared || f.File.URL != "" || f.File.Version != 1 || f.Skill.ID != e.link {
		t.Errorf("the linked SKILL.md is %+v", f)
	}
	own := e.fileAs(t, skOtherStr, skForgn, "SKILL.md")
	if !own.Shared || own.File.URL == "" {
		t.Errorf("the owner's file, linked to, is %+v: want shared, with its public link", own)
	}
	if _, err := e.c.SaveSkillFile(e.ctx, entity.SaveSkillFileRequest{WorkspaceID: skForgn, UserID: skOtherStr, Name: "solo", Path: "SKILL.md", Content: md("solo", "Alone.")}); err != nil {
		t.Fatal(err)
	}
	solo, err := e.c.GetSkillFile(e.ctx, entity.GetSkillFileRequest{WorkspaceID: skForgn, UserID: skOtherStr, Name: "solo", Path: "SKILL.md"})
	if err != nil || solo.Shared {
		t.Errorf("a skill nobody links to is %+v, %v: want it not shared", solo, err)
	}
}

// A write through a link lands in the teammate's skill, on the version it
// read; one without a version, or on a version since replaced, is refused
// with the version there is now.
func TestSkillLink_Writes(t *testing.T) {
	e := newLinkEnv(t)

	_, err := e.saveAs(skUserStr, skWS, "run.md", "mine", nil)
	wantSkillErr(t, err, SkillConflict, "shared with your team, so replacing skill://deploy/run.md needs the version you read; it is now version 1")
	_, err = e.saveAs(skOtherStr, skForgn, "run.md", "theirs", nil)
	wantSkillErr(t, err, SkillConflict, "it is now version 1")

	rs, err := e.saveAs(skUserStr, skWS, "run.md", "mine", version(1))
	if err != nil || rs.File.Version != 2 || rs.File.URL != "" || rs.Skill.ID != e.link || !rs.Skill.WorkspaceEnabled {
		t.Fatalf("a save through the link = %+v, %v", rs, err)
	}
	if f := e.fileAs(t, skOtherStr, skForgn, "run.md"); f.File.Content != "mine" || f.File.Version != 2 {
		t.Errorf("the owner reads %+v after the save through the link", f.File)
	}
	_, err = e.saveAs(skOtherStr, skForgn, "run.md", "theirs", version(1))
	wantSkillErr(t, err, SkillConflict, "skill://deploy/run.md was saved by someone else after version 1; it is now version 2")

	// A new file needs no version, and lands in the owner's skill and folder.
	rs, err = e.saveAs(skUserStr, skWS, "new.md", "n", nil)
	if err != nil || rs.File.Version != 1 || rs.Skill.FileCount != 3 {
		t.Fatalf("a new file through the link = %+v, %v", rs, err)
	}
	var nf model.SkillFile
	if err := e.db.Where("skill_id = ? AND path = ?", e.src, "new.md").First(&nf).Error; err != nil || !strings.HasPrefix(nf.StorageID, "u-"+skOtherStr+"/") {
		t.Errorf("the new file is %+v (%v): want it the owner's", nf, err)
	}
	_, err = e.saveAs(skUserStr, skWS, "gone.md", "g", version(3))
	wantSkillErr(t, err, SkillConflict, "skill://deploy/gone.md was deleted after you read it")

	// A new description shows on the link too, where a search finds it.
	rs, err = e.saveAs(skUserStr, skWS, "SKILL.md", md("deploy", "Ship it safely."), version(1))
	if err != nil || rs.Skill.Description != "Ship it safely." || rs.Skill.ID != e.link {
		t.Fatalf("SKILL.md through the link = %+v, %v", rs, err)
	}
	found, err := e.c.SearchSkills(e.ctx, entity.SearchSkillsRequest{WorkspaceID: skWS, UserID: skUserStr, Query: "safely", EnabledOnly: true})
	if err != nil || len(found.Skills) != 1 || found.Skills[0].ID != e.link {
		t.Errorf("searching the new words = %+v, %v", found, err)
	}

	del, err := e.c.DeleteSkillFile(e.ctx, entity.DeleteSkillFileRequest{WorkspaceID: skWS, UserID: skUserStr, Name: "deploy", Path: "new.md"})
	if err != nil || del.Skill.ID != e.link || del.Skill.FileCount != 2 {
		t.Fatalf("a file deleted through the link = %+v, %v", del, err)
	}
	if _, err := e.c.GetSkillFile(e.ctx, entity.GetSkillFileRequest{WorkspaceID: skForgn, UserID: skOtherStr, Name: "deploy", Path: "new.md"}); !errors.Is(err, base.ErrNotFound) {
		t.Errorf("the owner still reads the file deleted through the link: %v", err)
	}
}

// A write with no version, to a skill nobody else writes, that loses a race
// to another write is refused with the version it lost to.
func TestSkillLink_UnversionedRace(t *testing.T) {
	e := newSkillEnv(t)
	e.save(t, skWS, "tdd", "SKILL.md", md("tdd", "Test first."))
	e.save(t, skWS, "tdd", "notes.md", "n")
	e.db.Callback().Update().Before("gorm:update").Register("test:lose-race", func(tx *gorm.DB) {
		if tx.Statement.Table == "skill_files" {
			tx.Statement.AddClause(clause.Where{Exprs: []clause.Expression{clause.Expr{SQL: "1 = 0"}}})
		}
	})
	_, err := e.c.SaveSkillFile(e.ctx, entity.SaveSkillFileRequest{WorkspaceID: skWS, UserID: skUserStr, Name: "tdd", Path: "notes.md", Content: "m"})
	wantSkillErr(t, err, SkillConflict, "skill://tdd/notes.md was saved by someone else at the same time; it is now version 1")
}

// Once the team no longer shares the skill, the link reaches nothing: hidden
// from the agent's search, not found to every read and write, and still the
// account's to delete.
func TestSkillLink_Unshared(t *testing.T) {
	e := newLinkEnv(t)
	e.unshare(t)
	found, err := e.c.SearchSkills(e.ctx, entity.SearchSkillsRequest{WorkspaceID: skWS, UserID: skUserStr, EnabledOnly: true})
	if err != nil || found.Total != 0 {
		t.Errorf("the agent's search finds %+v, %v: want the unshared link hidden", found, err)
	}
	all, err := e.c.SearchSkills(e.ctx, entity.SearchSkillsRequest{UserID: skUserStr})
	if err != nil || all.Total != 1 {
		t.Errorf("the account's list is %+v, %v: want the link still listed", all, err)
	}
	for what, call := range map[string]func() error{
		"get": func() error {
			_, err := e.c.GetSkill(e.ctx, entity.GetSkillRequest{UserID: skUserStr, Name: "deploy"})
			return err
		},
		"file": func() error {
			_, err := e.c.GetSkillFile(e.ctx, entity.GetSkillFileRequest{UserID: skUserStr, Name: "deploy", Path: "SKILL.md"})
			return err
		},
		"save": func() error {
			_, err := e.saveAs(skUserStr, skWS, "run.md", "x", version(1))
			return err
		},
		"delete file": func() error {
			_, err := e.c.DeleteSkillFile(e.ctx, entity.DeleteSkillFileRequest{UserID: skUserStr, Name: "deploy", Path: "run.md"})
			return err
		},
	} {
		if err := call(); !errors.Is(err, base.ErrNotFound) || !strings.Contains(err.Error(), "no longer shared with you") {
			t.Errorf("%s through an unshared link: %v", what, err)
		}
	}
	if err := e.c.DeleteSkill(e.ctx, entity.DeleteSkillRequest{UserID: skUserStr, Name: "deploy"}); err != nil {
		t.Fatalf("deleting the link: %v", err)
	}
	if f := e.fileAs(t, skOtherStr, skForgn, "run.md"); f.File.Content != "run" {
		t.Errorf("deleting the link touched the skill: %+v", f.File)
	}
}

// The owner deleting the skill takes every link to it along.
func TestSkillLink_GoesWithItsSkill(t *testing.T) {
	e := newLinkEnv(t)
	if err := e.c.DeleteSkill(e.ctx, entity.DeleteSkillRequest{UserID: skOtherStr, Name: "deploy"}); err != nil {
		t.Fatal(err)
	}
	var rows, on int64
	e.db.Model(&model.Skill{}).Where("id = ?", e.link).Count(&rows)
	e.db.Model(&model.WorkspaceSkill{}).Where("skill_id = ?", e.link).Count(&on)
	if rows != 0 || on != 0 {
		t.Errorf("the link left %d rows and %d workspaces behind", rows, on)
	}
}

// failSQL fails every query whose SQL contains what, from the nth on.
func failSQL(db *gorm.DB, what string, nth int) {
	seen := 0
	db.Callback().Query().After("gorm:query").Register("test:fail-sql:"+what, func(tx *gorm.DB) {
		if strings.Contains(tx.Statement.SQL.String(), what) {
			if seen++; seen >= nth {
				_ = tx.AddError(errors.New("injected failure on " + what))
			}
		}
	})
}

// The checks a link and a shared skill add pass a failed read on.
func TestSkillLink_DatabaseFailures(t *testing.T) {
	t.Run("the team check", func(t *testing.T) {
		e := newLinkEnv(t)
		failSQL(e.db, "JOIN workspace_shares", 1)
		if _, err := e.c.GetSkill(e.ctx, entity.GetSkillRequest{UserID: skUserStr, Name: "deploy"}); err == nil || errors.Is(err, base.ErrNotFound) {
			t.Errorf("GetSkill = %v, want the injected failure", err)
		}
	})
	for _, tc := range []struct {
		name string
		call func(e *linkEnv) error
	}{
		{"read", func(e *linkEnv) error {
			_, err := e.c.GetSkillFile(e.ctx, entity.GetSkillFileRequest{UserID: skOtherStr, Name: "deploy", Path: "SKILL.md"})
			return err
		}},
		{"save", func(e *linkEnv) error {
			_, err := e.saveAs(skOtherStr, skForgn, "run.md", "x", version(1))
			return err
		}},
	} {
		t.Run("whether it is shared: "+tc.name, func(t *testing.T) {
			e := newLinkEnv(t)
			failSQL(e.db, "WHERE linked_skill_id =", 1)
			if err := tc.call(e); err == nil {
				t.Error("want the injected failure")
			}
		})
	}
	t.Run("the version there is now", func(t *testing.T) {
		e := newLinkEnv(t)
		// The first read of the file is the write's own; the second is the
		// refusal's, which fails.
		failSQL(e.db, "FROM `skill_files`", 2)
		_, err := e.saveAs(skUserStr, skWS, "SKILL.md", md("deploy", "x"), nil)
		var se *SkillError
		if err == nil || errors.As(err, &se) {
			t.Errorf("save = %v, want the injected failure", err)
		}
	})
}

// swapEnv is linkEnv with skUser's own deploy in place of the link, with
// files, on in skWS, shared with the team, and a third member, skThird,
// linking to it.
func newSwapEnv(t *testing.T) (*linkEnv, int64, int64) {
	t.Helper()
	e := newLinkEnv(t)
	if err := e.c.DeleteSkill(e.ctx, entity.DeleteSkillRequest{UserID: skUserStr, Name: "deploy"}); err != nil {
		t.Fatal(err)
	}
	mine, err := e.saveAs(skUserStr, skWS, "SKILL.md", md("deploy", "Mine."), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.saveAs(skUserStr, skWS, "extra.md", "extra", nil); err != nil {
		t.Fatal(err)
	}
	for _, row := range []any{
		&model.TeamMember{ID: 4, TeamID: 9, UserID: skThird},
		&model.WorkspaceShare{ID: 5, TeamID: 9, WorkspaceID: skWS, UserID: skUser},
	} {
		if err := e.db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	third, err := e.c.LinkSkill(e.ctx, entity.LinkSkillRequest{SkillID: mine.Skill.ID, UserID: monoflake.ID(skThird).String()})
	if err != nil {
		t.Fatal(err)
	}
	return e, mine.Skill.ID, third.Skill.ID
}

const skThird = int64(482467435371298819)

// Replace turns the caller's own skill into a link to the teammate's: its
// files go, its switches stay, and whoever linked to it now links there.
func TestLinkSkill_Replace(t *testing.T) {
	e, mine, third := newSwapEnv(t)
	req := entity.LinkSkillRequest{SkillID: e.src, UserID: skUserStr}
	if _, err := e.c.LinkSkill(e.ctx, req); err == nil || !strings.Contains(err.Error(), "already have") {
		t.Fatalf("a link over a name the caller has, without replace = %v", err)
	}
	req.Replace, req.WorkspaceIDs = true, []int64{skWS2}
	rs, err := e.c.LinkSkill(e.ctx, req)
	if err != nil || rs.Skill.ID != mine || rs.Skill.SourceType != skillSourceTeam || rs.Skill.Description != "Ship it." || fmt.Sprint(rs.Skill.WorkspaceIDs) != fmt.Sprint([]int64{skWS, skWS2}) {
		t.Fatalf("the swap = %+v, %v", rs, err)
	}
	var files int64
	e.db.Model(&model.SkillFile{}).Where("skill_id = ?", mine).Count(&files)
	var row, thirds model.Skill
	e.db.First(&row, mine)
	e.db.First(&thirds, third)
	if files != 0 || row.LinkedSkillID != e.src || thirds.LinkedSkillID != e.src || thirds.Description != "Ship it." {
		t.Errorf("after the swap: %d files left, the row %+v, the third's link %+v", files, row, thirds)
	}
	if f := e.fileAs(t, skUserStr, skWS, "SKILL.md"); !strings.Contains(f.File.Content, "Ship it.") {
		t.Errorf("the swapped skill reads %q, want the teammate's", f.File.Content)
	}
	if _, err := e.c.LinkSkill(e.ctx, entity.LinkSkillRequest{SkillID: e.src, UserID: skOtherStr, Replace: true}); err == nil || !strings.Contains(err.Error(), "your own") {
		t.Errorf("linking one's own skill = %v, want a refusal", err)
	}
}

// A swap for a skill that has itself just become a link is refused, so two
// owners swapping for each other's at once cannot leave two links and no
// skill; a failed read or write is passed on.
func TestLinkSkill_ReplaceRefusals(t *testing.T) {
	t.Run("the target became a link", func(t *testing.T) {
		e, _, _ := newSwapEnv(t)
		e.db.Model(&model.Skill{}).Where("id = ?", e.src).UpdateColumn("linked_skill_id", 1)
		var se *SkillError
		_, err := e.c.LinkSkill(e.ctx, entity.LinkSkillRequest{SkillID: e.src, UserID: skUserStr, Replace: true})
		if !errors.As(err, &se) || se.Kind != SkillConflict || !strings.Contains(se.Message, "a moment ago") {
			t.Errorf("swap = %v, want a conflict", err)
		}
	})
	t.Run("the switches unreadable", func(t *testing.T) {
		e, _, _ := newSwapEnv(t)
		failSQL(e.db, "FROM `workspace_skills` WHERE skill_id IN", 1)
		if _, err := e.c.LinkSkill(e.ctx, entity.LinkSkillRequest{SkillID: e.src, UserID: skUserStr, Replace: true}); err == nil {
			t.Error("want the injected failure")
		}
	})
	t.Run("the swap failing", func(t *testing.T) {
		e, _, _ := newSwapEnv(t)
		failSQL(e.db, "SELECT `storage_id` FROM `skill_files`", 1)
		var se *SkillError
		if _, err := e.c.LinkSkill(e.ctx, entity.LinkSkillRequest{SkillID: e.src, UserID: skUserStr, Replace: true}); err == nil || errors.As(err, &se) {
			t.Errorf("swap = %v, want the injected failure", err)
		}
	})
}
