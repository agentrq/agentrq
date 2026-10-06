// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package base

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

const (
	skUser  = int64(482467435371298817)
	skWS    = int64(498041479541817345)
	skOther = int64(498041479541817346)
)

var errInjected = errors.New("injected failure")

func skillDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&model.Skill{}, &model.SkillFile{}, &model.SkillShare{}, &model.WorkspaceSkill{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

// seedSkill stores account skill 1 with SKILL.md and notes.md, on in skWS.
func seedSkill(t *testing.T, r Repository) model.Skill {
	t.Helper()
	s, _, err := r.ReplaceSkill(context.Background(), model.Skill{ID: 1, UserID: skUser, Name: "tdd", Description: "d"}, []model.SkillFile{
		{ID: 11, Path: "SKILL.md", SizeBytes: 10, StorageID: "skill-11"},
		{ID: 12, Path: "notes.md", SizeBytes: 5, StorageID: "skill-12"},
	}, []model.WorkspaceSkill{{ID: 21, UserID: skUser, WorkspaceID: skWS}})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	return s
}

// seedLegacySkill stores skill 2 the way it was kept before skills belonged to
// the account: filed under skWS, with one file, shared into skOther.
func seedLegacySkill(t *testing.T, db *gorm.DB) model.Skill {
	t.Helper()
	s := model.Skill{ID: 2, UserID: skUser, WorkspaceID: skWS, Name: "lint", Description: "d", FileCount: 1}
	if err := db.Create(&s).Error; err != nil {
		t.Fatalf("seed legacy skill: %v", err)
	}
	if err := db.Create(&model.SkillFile{ID: 31, SkillID: 2, Path: "SKILL.md", SizeBytes: 7, StorageID: "w-old/skill-2/31"}).Error; err != nil {
		t.Fatalf("seed legacy file: %v", err)
	}
	if err := db.Create(&model.SkillShare{ID: 41, UserID: skUser, SkillID: 2, TargetWorkspaceID: skOther}).Error; err != nil {
		t.Fatalf("seed legacy share: %v", err)
	}
	return s
}

// failNth fails the n-th statement run on db, whatever its kind, and reports
// how many statements ran.
func failNth(db *gorm.DB, n int) *int {
	count := 0
	fail := func(tx *gorm.DB) {
		count++
		if count == n {
			_ = tx.AddError(errInjected)
		}
	}
	cb := db.Callback()
	_ = cb.Create().Before("gorm:create").Register("test:nth", fail)
	_ = cb.Query().Before("gorm:query").Register("test:nth", fail)
	_ = cb.Update().Before("gorm:update").Register("test:nth", fail)
	_ = cb.Delete().Before("gorm:delete").Register("test:nth", fail)
	_ = cb.Row().Before("gorm:row").Register("test:nth", fail)
	return &count
}

// Every statement a skill write runs can fail, and each failure reaches the
// caller rather than being swallowed.
func TestSkillRepository_EveryStatementFailure(t *testing.T) {
	ctx := context.Background()
	on := func() []model.WorkspaceSkill {
		return []model.WorkspaceSkill{{ID: 22, UserID: skUser, WorkspaceID: skOther}}
	}
	for _, tc := range []struct {
		name string
		call func(r Repository, s, legacy model.Skill) error
	}{
		{"replace", func(r Repository, s, _ model.Skill) error {
			_, _, err := r.ReplaceSkill(ctx, s, []model.SkillFile{{ID: 13, Path: "SKILL.md", StorageID: "skill-13"}}, on())
			return err
		}},
		{"upsert existing", func(r Repository, s, _ model.Skill) error {
			_, _, err := r.UpsertSkillFile(ctx, s, model.SkillFile{ID: 14, Path: "notes.md", StorageID: "skill-14"}, on())
			return err
		}},
		{"delete file", func(r Repository, s, _ model.Skill) error {
			_, _, err := r.DeleteSkillFile(ctx, s, "notes.md")
			return err
		}},
		{"delete skill", func(r Repository, s, _ model.Skill) error {
			_, err := r.DeleteSkill(ctx, s.ID)
			return err
		}},
		{"move a legacy skill", func(r Repository, _, legacy model.Skill) error {
			legacy.WorkspaceID = 0
			_, err := r.MoveLegacySkill(ctx, skWS, legacy, []model.SkillFile{{ID: 31, StorageID: "u-new/skill-2/32"}}, on())
			return err
		}},
		{"merge a legacy skill", func(r Repository, s, legacy model.Skill) error {
			_, err := r.MergeLegacySkill(ctx, legacy, s.ID, on())
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for n := 1; ; n++ {
				db := skillDB(t)
				r := New(&mockDB{db: db})
				s := seedSkill(t, r)
				legacy := seedLegacySkill(t, db)
				ran := failNth(db, n)
				err := tc.call(r, s, legacy)
				if *ran < n {
					if err != nil {
						t.Fatalf("with no failure injected: %v", err)
					}
					if n == 1 {
						t.Fatal("the call ran no statements")
					}
					return
				}
				if !errors.Is(err, errInjected) {
					t.Fatalf("failing statement %d returned %v, want the injected failure", n, err)
				}
			}
		})
	}
}

func TestGetSkill_FindsOnlyAccountSkills(t *testing.T) {
	db := skillDB(t)
	r := New(&mockDB{db: db})
	ctx := context.Background()
	seedSkill(t, r)
	seedLegacySkill(t, db)

	if s, err := r.GetSkill(ctx, skUser, "tdd"); err != nil || s.ID != 1 {
		t.Errorf("GetSkill(tdd) returned %+v, %v; want skill 1", s, err)
	}
	if _, err := r.GetSkill(ctx, skUser, "lint"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetSkill of a skill still filed under a workspace returned %v, want ErrNotFound", err)
	}
	if _, err := r.GetSkill(ctx, skUser+1, "tdd"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetSkill from another account returned %v, want ErrNotFound", err)
	}
}

func TestSearchSkills_FiltersByWorkspaceAndSwitch(t *testing.T) {
	db := skillDB(t)
	r := New(&mockDB{db: db})
	ctx := context.Background()
	seedSkill(t, r)
	seedLegacySkill(t, db)
	if _, _, err := r.ReplaceSkill(ctx, model.Skill{ID: 3, UserID: skUser, Name: "review", Description: "100%_off"}, nil, nil); err != nil {
		t.Fatalf("seed review: %v", err)
	}

	names := func(skills []model.Skill) []string {
		out := []string{}
		for _, s := range skills {
			out = append(out, s.Name)
		}
		return out
	}
	for _, tc := range []struct {
		name   string
		q      string
		filter SkillFilter
		want   []string
	}{
		{"the account lists every account skill, by name", "", SkillFilter{}, []string{"review", "tdd"}},
		{"a workspace keeps only the skills on in it", "", SkillFilter{InWorkspace: skWS}, []string{"tdd"}},
		{"a workspace with none on lists nothing", "", SkillFilter{InWorkspace: skOther}, []string{}},
		{"a query matches LIKE wildcards literally", "0%_", SkillFilter{}, []string{"review"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			found, total, err := r.SearchSkills(ctx, skUser, tc.q, tc.filter, 0, 0)
			if err != nil {
				t.Fatalf("SearchSkills: %v", err)
			}
			if got := names(found); !reflect.DeepEqual(got, tc.want) || int(total) != len(tc.want) {
				t.Errorf("SearchSkills returned %v (total %d), want %v", got, total, tc.want)
			}
		})
	}

	if err := r.SetSkillDisabled(ctx, 1, true); err != nil {
		t.Fatal(err)
	}
	if found, total, err := r.SearchSkills(ctx, skUser, "", SkillFilter{InWorkspace: skWS, EnabledOnly: true}, 0, 0); err != nil || total != 0 || len(found) != 0 {
		t.Errorf("enabled only, after turning tdd off for the account, returned %v (total %d, %v), want nothing", names(found), total, err)
	}
	if found, _, err := r.SearchSkills(ctx, skUser, "", SkillFilter{}, 1, 1); err != nil || !reflect.DeepEqual(names(found), []string{"tdd"}) {
		t.Errorf("the second page of one returned %v (%v), want [tdd]", names(found), err)
	}
}

// Both statements a search runs, the count and the page, report a failure.
// Statement 2 is the dry run that builds the workspace subquery, whose own
// error is never returned, so it is skipped.
func TestSearchSkills_ReportsFailures(t *testing.T) {
	for _, n := range []int{1, 3} {
		db := skillDB(t)
		r := New(&mockDB{db: db})
		seedSkill(t, r)
		failNth(db, n)
		if _, _, err := r.SearchSkills(context.Background(), skUser, "tdd", SkillFilter{InWorkspace: skWS}, 1, 0); !errors.Is(err, errInjected) {
			t.Errorf("failing statement %d returned %v, want the injected failure", n, err)
		}
	}
}

func TestSkillWorkspaces_TurnOnAndOff(t *testing.T) {
	db := skillDB(t)
	r := New(&mockDB{db: db})
	ctx := context.Background()
	seedSkill(t, r)

	if err := r.TurnSkillOn(ctx, model.WorkspaceSkill{ID: 23, UserID: skUser, SkillID: 1, WorkspaceID: skOther}); err != nil {
		t.Fatalf("TurnSkillOn: %v", err)
	}
	if err := r.TurnSkillOn(ctx, model.WorkspaceSkill{ID: 24, UserID: skUser, SkillID: 1, WorkspaceID: skOther}); err != nil {
		t.Fatalf("turning a skill on where it is on already returned %v, want no error", err)
	}
	on, err := r.ListSkillWorkspaces(ctx, []int64{1, 99})
	if err != nil || !reflect.DeepEqual(on, map[int64][]int64{1: {skWS, skOther}}) {
		t.Fatalf("ListSkillWorkspaces returned %v, %v; want skill 1 on in both workspaces, in the order turned on", on, err)
	}

	if was, err := r.TurnSkillOff(ctx, 1, skWS); err != nil || !was {
		t.Errorf("TurnSkillOff of a skill that was on returned %v, %v; want true", was, err)
	}
	if was, err := r.TurnSkillOff(ctx, 1, skWS); err != nil || was {
		t.Errorf("TurnSkillOff of a skill already off returned %v, %v; want false", was, err)
	}

	if on, err := r.ListSkillWorkspaces(ctx, nil); err != nil || len(on) != 0 {
		t.Errorf("ListSkillWorkspaces of no skills returned %v, %v; want an empty map", on, err)
	}
	failNth(db, 1)
	if _, err := r.ListSkillWorkspaces(ctx, []int64{1}); !errors.Is(err, errInjected) {
		t.Errorf("ListSkillWorkspaces with the database failing returned %v, want the injected failure", err)
	}
}

func TestLegacySkills_ListedOldestFirstWithTheirShares(t *testing.T) {
	db := skillDB(t)
	r := New(&mockDB{db: db})
	ctx := context.Background()
	seedSkill(t, r)
	seedLegacySkill(t, db)

	legacy, err := r.ListLegacySkills(ctx)
	if err != nil || len(legacy) != 1 || legacy[0].ID != 2 {
		t.Fatalf("ListLegacySkills returned %v, %v; want only skill 2", legacy, err)
	}
	if targets, err := r.ListLegacySkillShares(ctx, 2); err != nil || !reflect.DeepEqual(targets, []int64{skOther}) {
		t.Errorf("ListLegacySkillShares returned %v, %v; want [%d]", targets, err, skOther)
	}
}

func TestMoveLegacySkill_MovesOnceToTheAccount(t *testing.T) {
	db := skillDB(t)
	r := New(&mockDB{db: db})
	ctx := context.Background()
	legacy := seedLegacySkill(t, db)

	target := legacy
	target.WorkspaceID, target.Name = 0, "lint-renamed"
	files := []model.SkillFile{{ID: 31, StorageID: "u-new/skill-2/32", SizeBytes: 9, SHA256: "sum"}}
	on := []model.WorkspaceSkill{{ID: 51, UserID: skUser, WorkspaceID: skWS}, {ID: 52, UserID: skUser, WorkspaceID: skOther}}
	moved, err := r.MoveLegacySkill(ctx, skWS, target, files, on)
	if err != nil || !moved {
		t.Fatalf("MoveLegacySkill returned %v, %v; want it moved", moved, err)
	}

	s, err := r.GetSkill(ctx, skUser, "lint-renamed")
	if err != nil || s.TotalBytes != 9 || s.FileCount != 1 {
		t.Errorf("the moved skill reads %+v, %v; want it under its new name with 9 bytes in 1 file", s, err)
	}
	f, _ := r.GetSkillFile(ctx, 2, "SKILL.md")
	if f.StorageID != "u-new/skill-2/32" || f.SHA256 != "sum" {
		t.Errorf("the moved file is %+v, want it pointing at the new blob", f)
	}
	if ws, _ := r.ListSkillWorkspaces(ctx, []int64{2}); !reflect.DeepEqual(ws[2], []int64{skWS, skOther}) {
		t.Errorf("the moved skill is on in %v, want both workspaces", ws[2])
	}
	var shares int64
	db.Model(&model.SkillShare{}).Count(&shares)
	if shares != 0 {
		t.Errorf("%d shares left after the move, want 0", shares)
	}

	moved, err = r.MoveLegacySkill(ctx, skWS, target, files, on)
	if err != nil || moved {
		t.Errorf("moving a skill moved already returned %v, %v; want false and no error", moved, err)
	}
}

func TestMergeLegacySkill_FoldsIntoTheAccountSkill(t *testing.T) {
	db := skillDB(t)
	r := New(&mockDB{db: db})
	ctx := context.Background()
	seedSkill(t, r)
	legacy := seedLegacySkill(t, db)

	on := []model.WorkspaceSkill{{ID: 53, UserID: skUser, WorkspaceID: skOther}}
	dropped, err := r.MergeLegacySkill(ctx, legacy, 1, on)
	if err != nil || !reflect.DeepEqual(dropped, []string{"w-old/skill-2/31"}) {
		t.Fatalf("MergeLegacySkill returned %v, %v; want the legacy skill's blob to purge", dropped, err)
	}
	if ws, _ := r.ListSkillWorkspaces(ctx, []int64{1}); !reflect.DeepEqual(ws[1], []int64{skWS, skOther}) {
		t.Errorf("the merged skill is on in %v, want both workspaces", ws[1])
	}
	for _, c := range []struct {
		what  string
		model any
	}{{"skills", &model.Skill{}}, {"skill files", &model.SkillFile{}}, {"skill shares", &model.SkillShare{}}} {
		var n int64
		db.Model(c.model).Where("id = ? OR id = ? OR id = ?", 2, 31, 41).Count(&n)
		if n != 0 {
			t.Errorf("%d of the legacy skill's %s left, want 0", n, c.what)
		}
	}

	dropped, err = r.MergeLegacySkill(ctx, legacy, 1, on)
	if err != nil || dropped != nil {
		t.Errorf("merging a skill merged already returned %v, %v; want nothing to purge and no error", dropped, err)
	}
}

func TestDeleteSkill_RemovesItsWorkspacesToo(t *testing.T) {
	db := skillDB(t)
	r := New(&mockDB{db: db})
	ctx := context.Background()
	seedSkill(t, r)
	ids, err := r.DeleteSkill(ctx, 1)
	if err != nil || len(ids) != 2 {
		t.Fatalf("DeleteSkill returned %v, %v; want both blobs to purge", ids, err)
	}
	var n int64
	db.Model(&model.WorkspaceSkill{}).Count(&n)
	if n != 0 {
		t.Errorf("%d workspace rows left after the delete, want 0", n)
	}
	if _, err := r.DeleteSkill(ctx, 999); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleting a missing skill returned %v, want ErrNotFound", err)
	}
}

// A workspace delete takes the skills out of the workspace, and leaves them in
// the account.
func TestDeleteWorkspace_KeepsTheAccountsSkills(t *testing.T) {
	db := skillDB(t)
	if err := db.AutoMigrate(&model.Workspace{}, &model.Message{}, &model.ToolCall{}, &model.SlackTaskThread{}, &model.Task{}, &model.TaskStateTransition{}, &model.TaskLatency{}, &model.SiteShare{}, &model.ForkFolder{}, &model.EventTrigger{}, &model.WorkflowStep{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	r := New(&mockDB{db: db})
	seedSkill(t, r)
	if err := db.Create(&model.Workspace{ID: skWS, UserID: skUser, Name: "doomed"}).Error; err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	if err := r.DeleteWorkspace(context.Background(), skWS, skUser); err != nil {
		t.Fatalf("DeleteWorkspace: %v", err)
	}
	if _, err := r.GetSkill(context.Background(), skUser, "tdd"); err != nil {
		t.Errorf("the skill after its workspace was deleted: %v, want it kept", err)
	}
	var n int64
	db.Model(&model.WorkspaceSkill{}).Count(&n)
	if n != 0 {
		t.Errorf("%d workspace rows left, want 0", n)
	}
}

func TestSetSkillDisabled(t *testing.T) {
	db := skillDB(t)
	r := New(&mockDB{db: db})
	ctx := context.Background()
	seedSkill(t, r)

	if err := r.SetSkillDisabled(ctx, 1, true); err != nil {
		t.Fatal(err)
	}
	if s, _ := r.GetSkill(ctx, skUser, "tdd"); !s.Disabled {
		t.Error("the skill after turning it off reads as on")
	}
	if err := r.SetSkillDisabled(ctx, 2, true); !errors.Is(err, ErrNotFound) {
		t.Errorf("turning off a missing skill returned %v, want ErrNotFound", err)
	}
	failNth(db, 1)
	if err := r.SetSkillDisabled(ctx, 1, false); !errors.Is(err, errInjected) {
		t.Errorf("turning a skill on with the database failing returned %v, want the injected failure", err)
	}
}
