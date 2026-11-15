// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package base

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/agentrq/agentrq/backend/internal/data/model"
	"gorm.io/gorm"
)

func ptr(v int64) *int64 { return &v }

// fileVersion is the stored version of the file at path of skill 1.
func fileVersion(t *testing.T, r Repository, path string) int64 {
	t.Helper()
	f, err := r.GetSkillFile(context.Background(), 1, path)
	if err != nil {
		t.Fatalf("get %s: %v", path, err)
	}
	return f.Version
}

// Every write moves a file to its next version; a write naming a version
// that is no longer the file's, or naming one for a file that is not there,
// is refused and changes nothing.
func TestUpsertSkillFile_Versions(t *testing.T) {
	ctx := context.Background()
	r := New(&mockDB{db: skillDB(t)})
	s := seedSkill(t, r)
	if v := fileVersion(t, r, "notes.md"); v != 1 {
		t.Fatalf("a seeded file is version %d, want 1", v)
	}

	_, f, replaced, err := r.UpsertSkillFile(ctx, s, model.SkillFile{ID: 50, Path: "notes.md", StorageID: "skill-50"}, nil, nil)
	if err != nil || f.Version != 2 || f.ID != 12 || replaced != "skill-12" {
		t.Fatalf("a write with no version: %+v, %q, %v; want version 2 of file 12, replacing skill-12", f, replaced, err)
	}
	for name, expect := range map[string]*int64{"a stale version": ptr(1), "no file yet": ptr(0)} {
		if _, _, _, err := r.UpsertSkillFile(ctx, s, model.SkillFile{ID: 51, Path: "notes.md", StorageID: "skill-51"}, nil, expect); !errors.Is(err, ErrVersionConflict) {
			t.Errorf("a write on %s: %v, want ErrVersionConflict", name, err)
		}
	}
	if _, f, _, err = r.UpsertSkillFile(ctx, s, model.SkillFile{ID: 52, Path: "notes.md", StorageID: "skill-52"}, nil, ptr(2)); err != nil || f.Version != 3 {
		t.Errorf("a write on the version there is: %+v, %v; want version 3", f, err)
	}
	if v := fileVersion(t, r, "notes.md"); v != 3 {
		t.Errorf("the file is version %d after two writes and two refusals, want 3", v)
	}

	if _, _, _, err := r.UpsertSkillFile(ctx, s, model.SkillFile{ID: 53, Path: "new.md", StorageID: "skill-53"}, nil, ptr(4)); !errors.Is(err, ErrVersionConflict) {
		t.Errorf("a write naming a version of a file that is not there: %v, want ErrVersionConflict", err)
	}
	if _, err := r.GetSkillFile(ctx, 1, "new.md"); !errors.Is(err, ErrNotFound) {
		t.Errorf("the refused new file was stored: %v", err)
	}
	if _, f, replaced, err = r.UpsertSkillFile(ctx, s, model.SkillFile{ID: 54, Path: "new.md", StorageID: "skill-54"}, nil, ptr(0)); err != nil || f.Version != 1 || replaced != "" {
		t.Errorf("a new file on version 0: %+v, %q, %v; want version 1, replacing nothing", f, replaced, err)
	}
}

// A write that loses the race to another, between reading the file and
// writing it, is refused rather than written over the other.
func TestUpsertSkillFile_LosesARace(t *testing.T) {
	db := skillDB(t)
	r := New(&mockDB{db: db})
	s := seedSkill(t, r)
	// The file is read as version 1, but another write moves it on before
	// this one updates it.
	if err := db.Callback().Query().After("gorm:query").Register("test:race", func(tx *gorm.DB) {
		if f, ok := tx.Statement.Dest.(*model.SkillFile); ok && f.Path == "notes.md" {
			tx.Session(&gorm.Session{NewDB: true}).Model(&model.SkillFile{}).Where("id = ?", f.ID).UpdateColumn("version", f.Version+1)
		}
	}); err != nil {
		t.Fatal(err)
	}
	_, _, _, err := r.UpsertSkillFile(context.Background(), s, model.SkillFile{ID: 50, Path: "notes.md", StorageID: "skill-50"}, nil, nil)
	if !errors.Is(err, ErrVersionConflict) {
		t.Errorf("a write that lost the race: %v, want ErrVersionConflict", err)
	}
}

// A link left from before team workspaces owned skills is listed for
// ConvertSkillLinks, which turns it into a skill of its own, with files, or
// deletes it with where it was on; either only while it is still that link.
func TestSkillLinks_ConvertOrDelete(t *testing.T) {
	ctx := context.Background()
	db := skillDB(t)
	r := New(&mockDB{db: db})
	seedSkill(t, r)

	if got, err := r.GetSkillByID(ctx, 1); err != nil || got.Name != "tdd" {
		t.Errorf("GetSkillByID(1) = %+v, %v", got, err)
	}
	legacy := seedLegacySkill(t, db)
	for _, id := range []int64{legacy.ID, 99} {
		if _, err := r.GetSkillByID(ctx, id); !errors.Is(err, ErrNotFound) {
			t.Errorf("GetSkillByID(%d) = %v, want ErrNotFound: a legacy skill or none", id, err)
		}
	}
	if links, err := r.ListSkillLinks(ctx); err != nil || len(links) != 0 {
		t.Errorf("ListSkillLinks with none = %v, %v", links, err)
	}

	reader := skUser + 1
	for _, row := range []any{
		&model.Skill{ID: 8, UserID: reader, Name: "gone", LinkedSkillID: 99, SourceType: "team"},
		&model.Skill{ID: 7, UserID: reader, Name: "tdd", LinkedSkillID: 1, SourceType: "team"},
		&model.WorkspaceSkill{ID: 24, UserID: reader, SkillID: 7, WorkspaceID: skOther},
		&model.WorkspaceSkill{ID: 25, UserID: reader, SkillID: 8, WorkspaceID: skOther},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	links, err := r.ListSkillLinks(ctx)
	if err != nil || len(links) != 2 || links[0].ID != 7 || links[1].ID != 8 {
		t.Fatalf("ListSkillLinks = %+v, %v; want 7 and 8, by id", links, err)
	}

	own := model.Skill{ID: 7, Description: "copied", SourceType: "github", SourceRepo: "o/r", LocallyModified: true, FileCount: 1, TotalBytes: 4, UpdatedAt: time.Now()}
	files := []model.SkillFile{{ID: 70, SkillID: 7, Path: "SKILL.md", SizeBytes: 4, StorageID: "u-x/skill-7/70", Version: 1}}
	if ok, err := r.ConvertSkillLink(ctx, own, 3, files); err != nil || ok {
		t.Errorf("converting a link to another skill = %v, %v; want false", ok, err)
	}
	if ok, err := r.ConvertSkillLink(ctx, own, 1, files); err != nil || !ok {
		t.Fatalf("ConvertSkillLink = %v, %v", ok, err)
	}
	var got model.Skill
	if err := db.First(&got, 7).Error; err != nil {
		t.Fatal(err)
	}
	if got.LinkedSkillID != 0 || got.UserID != reader || got.Name != "tdd" || got.Description != "copied" || got.SourceType != "github" ||
		got.SourceRepo != "o/r" || !got.LocallyModified || got.FileCount != 1 || got.TotalBytes != 4 {
		t.Errorf("the converted link is %+v", got)
	}
	if f, err := r.GetSkillFile(ctx, 7, "SKILL.md"); err != nil || f.StorageID != "u-x/skill-7/70" {
		t.Errorf("the converted link's file = %+v, %v", f, err)
	}
	if on, err := r.ListSkillWorkspaces(ctx, []int64{7}); err != nil || fmt.Sprint(on[7]) != fmt.Sprint([]int64{skOther}) {
		t.Errorf("the converted link is on in %v (%v), want [%d]", on[7], err, skOther)
	}
	if ok, err := r.ConvertSkillLink(ctx, own, 1, nil); err != nil || ok {
		t.Errorf("converting it twice = %v, %v; want false", ok, err)
	}

	if ok, err := r.DeleteSkillLink(ctx, 8, 1); err != nil || ok {
		t.Errorf("deleting a link to another skill = %v, %v; want false", ok, err)
	}
	if ok, err := r.DeleteSkillLink(ctx, 7, 1); err != nil || ok {
		t.Errorf("deleting a converted link = %v, %v; want false", ok, err)
	}
	if ok, err := r.DeleteSkillLink(ctx, 8, 99); err != nil || !ok {
		t.Errorf("DeleteSkillLink = %v, %v", ok, err)
	}
	var rows, on int64
	db.Model(&model.Skill{}).Where("id = ?", 8).Count(&rows)
	db.Model(&model.WorkspaceSkill{}).Where("skill_id = ?", 8).Count(&on)
	if rows != 0 || on != 0 {
		t.Errorf("the deleted link left %d rows, on in %d workspaces", rows, on)
	}
	if links, err := r.ListSkillLinks(ctx); err != nil || len(links) != 0 {
		t.Errorf("ListSkillLinks after = %v, %v; want none", links, err)
	}
	if _, err := r.DeleteSkill(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := r.GetSkillByID(ctx, 7); err != nil {
		t.Errorf("deleting the teammate's skill took the converted copy: %v", err)
	}
}

// A failed write leaves the link as it was.
func TestSkillLinks_FailedWrites(t *testing.T) {
	ctx := context.Background()
	db := skillDB(t)
	r := New(&mockDB{db: db})
	seedSkill(t, r)
	db.Create(&model.Skill{ID: 7, UserID: skUser + 1, Name: "tdd", LinkedSkillID: 1})
	// A file whose path the skill already has breaks the unique index.
	dup := []model.SkillFile{{ID: 70, SkillID: 7, Path: "a.md"}, {ID: 71, SkillID: 7, Path: "a.md"}}
	if ok, err := r.ConvertSkillLink(ctx, model.Skill{ID: 7}, 1, dup); err == nil || ok {
		t.Errorf("a conversion whose files fail = %v, %v; want the error", ok, err)
	}
	if got, err := r.GetSkillByID(ctx, 7); err != nil || got.LinkedSkillID != 1 {
		t.Errorf("after the failed conversion the link is %+v, %v", got, err)
	}
	db.Callback().Delete().Before("gorm:delete").Register("test:fail-workspace-skills", func(tx *gorm.DB) {
		if tx.Statement.Table == "workspace_skills" {
			_ = tx.AddError(errors.New("database unavailable"))
		}
	})
	if ok, err := r.DeleteSkillLink(ctx, 7, 1); err == nil || ok {
		t.Errorf("a delete whose workspaces fail = %v, %v; want the error", ok, err)
	}
	if got, err := r.GetSkillByID(ctx, 7); err != nil || got.LinkedSkillID != 1 {
		t.Errorf("after the failed delete the link is %+v, %v", got, err)
	}
}

// A user reads a teammate's skill through a team only while it is the
// teammate's own, on, and on in an unarchived workspace shared with a team
// of the user's that does not hold its skills back: a link to it is
// converted only then. No agent sees a link not yet converted.
func TestLinkedSkills_ReadOnlyThroughASharingTeam(t *testing.T) {
	forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
		ctx := context.Background()
		seedTeamWorkspaces(t, db)
		if err := db.AutoMigrate(teamViewTables...); err != nil {
			t.Fatal(err)
		}
		db.Create(&model.Workspace{ID: 105, UserID: 10, Name: "held-back"})
		db.Create(&model.WorkspaceShare{ID: 5, TeamID: 1, WorkspaceID: 105, UserID: 10, HideSkills: true})
		for _, row := range []any{
			&model.Skill{ID: 1, UserID: 10, Name: "deploy", Description: "d"},
			&model.Skill{ID: 2, UserID: 10, Name: "private"},
			&model.Skill{ID: 3, UserID: 10, Name: "off", Disabled: true},
			&model.Skill{ID: 4, UserID: 10, Name: "archived"},
			&model.Skill{ID: 5, UserID: 10, Name: "held"},
			&model.Skill{ID: 6, UserID: 10, Name: "relinked", LinkedSkillID: 9},
			&model.Skill{ID: 7, UserID: 11, Name: "deploy", LinkedSkillID: 1},
			&model.Skill{ID: 8, UserID: 11, Name: "private", LinkedSkillID: 2},
			&model.Skill{ID: 9, UserID: 11, Name: "mine"},
			&model.WorkspaceSkill{ID: 1, UserID: 10, SkillID: 1, WorkspaceID: 100},
			&model.WorkspaceSkill{ID: 2, UserID: 10, SkillID: 2, WorkspaceID: 103},
			&model.WorkspaceSkill{ID: 3, UserID: 10, SkillID: 3, WorkspaceID: 100},
			&model.WorkspaceSkill{ID: 4, UserID: 10, SkillID: 4, WorkspaceID: 104},
			&model.WorkspaceSkill{ID: 5, UserID: 10, SkillID: 5, WorkspaceID: 105},
			&model.WorkspaceSkill{ID: 6, UserID: 10, SkillID: 6, WorkspaceID: 100},
			&model.WorkspaceSkill{ID: 7, UserID: 11, SkillID: 7, WorkspaceID: 200},
			&model.WorkspaceSkill{ID: 8, UserID: 11, SkillID: 8, WorkspaceID: 200},
			&model.WorkspaceSkill{ID: 9, UserID: 11, SkillID: 9, WorkspaceID: 200},
		} {
			if err := db.Create(row).Error; err != nil {
				t.Fatal(err)
			}
		}

		for _, c := range []struct {
			what          string
			user, skillID int64
			want          bool
		}{
			{"on in a shared workspace", 11, 1, true},
			{"on only in an unshared one", 11, 2, false},
			{"turned off by its owner", 11, 3, false},
			{"on only in an archived shared one", 11, 4, false},
			{"in a share that holds skills back", 11, 5, false},
			{"itself a link", 11, 6, false},
			{"read by someone in no team with it", 12, 1, false},
		} {
			if got, err := r.CanReadLinkedSkill(ctx, c.user, c.skillID); err != nil || got != c.want {
				t.Errorf("CanReadLinkedSkill, a skill %s = %v, %v; want %v", c.what, got, err, c.want)
			}
		}

		names := func(filter SkillFilter) string {
			t.Helper()
			found, total, err := r.SearchSkills(ctx, 11, "", filter, 0, 0)
			if err != nil {
				t.Fatal(err)
			}
			out := []string{}
			for _, s := range found {
				out = append(out, s.Name)
			}
			return fmt.Sprintf("%d %v", total, out)
		}
		if got := names(SkillFilter{InWorkspace: 200, EnabledOnly: true}); got != "1 [mine]" {
			t.Errorf("what the agent sees: %s; want its own skill alone", got)
		}
		if got := names(SkillFilter{}); got != "3 [deploy mine private]" {
			t.Errorf("what the account lists: %s; want every row, links no team shares included", got)
		}

	})
}
