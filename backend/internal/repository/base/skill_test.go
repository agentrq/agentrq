// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package base

import (
	"context"
	"errors"
	"sort"
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
	if err := db.AutoMigrate(&model.Skill{}, &model.SkillFile{}, &model.SkillShare{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

// seedSkill stores skill 1 in skWS with SKILL.md and notes.md, shared into
// skOther.
func seedSkill(t *testing.T, r Repository) model.Skill {
	t.Helper()
	ctx := context.Background()
	s, _, err := r.ReplaceSkill(ctx, model.Skill{ID: 1, UserID: skUser, WorkspaceID: skWS, Name: "tdd", Description: "d"}, []model.SkillFile{
		{ID: 11, Path: "SKILL.md", SizeBytes: 10, StorageID: "skill-11"},
		{ID: 12, Path: "notes.md", SizeBytes: 5, StorageID: "skill-12"},
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := r.CreateSkillShare(ctx, model.SkillShare{ID: 21, SkillID: 1, UserID: skUser, TargetWorkspaceID: skOther}); err != nil {
		t.Fatalf("seed share: %v", err)
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
	for _, tc := range []struct {
		name string
		call func(r Repository, s model.Skill) error
	}{
		{"replace", func(r Repository, s model.Skill) error {
			_, _, err := r.ReplaceSkill(ctx, s, []model.SkillFile{{ID: 13, Path: "SKILL.md", StorageID: "skill-13"}})
			return err
		}},
		{"upsert existing", func(r Repository, s model.Skill) error {
			_, _, err := r.UpsertSkillFile(ctx, s, model.SkillFile{ID: 14, Path: "notes.md", StorageID: "skill-14"})
			return err
		}},
		{"delete file", func(r Repository, s model.Skill) error {
			_, _, err := r.DeleteSkillFile(ctx, s, "notes.md")
			return err
		}},
		{"delete skill", func(r Repository, s model.Skill) error {
			_, err := r.DeleteSkill(ctx, s.ID)
			return err
		}},
		{"delete share", func(r Repository, s model.Skill) error {
			return r.DeleteSkillShare(ctx, s.ID, skOther)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for n := 1; ; n++ {
				db := skillDB(t)
				r := New(&mockDB{db: db})
				s := seedSkill(t, r)
				ran := failNth(db, n)
				err := tc.call(r, s)
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
					t.Fatalf("failing statement %d: got %v, want the injected failure", n, err)
				}
			}
		})
	}
}

func TestGetWorkspaceSkillStorageIDs_OnlyThatWorkspace(t *testing.T) {
	db := skillDB(t)
	r := New(&mockDB{db: db})
	ctx := context.Background()
	seedSkill(t, r)
	if _, _, err := r.ReplaceSkill(ctx, model.Skill{ID: 2, UserID: skUser, WorkspaceID: skOther, Name: "other", Description: "d"},
		[]model.SkillFile{{ID: 31, Path: "SKILL.md", StorageID: "skill-31"}}); err != nil {
		t.Fatalf("seed other: %v", err)
	}
	ids, err := r.GetWorkspaceSkillStorageIDs(ctx, skWS)
	if err != nil {
		t.Fatalf("GetWorkspaceSkillStorageIDs: %v", err)
	}
	sort.Strings(ids)
	if len(ids) != 2 || ids[0] != "skill-11" || ids[1] != "skill-12" {
		t.Errorf("got %v, want [skill-11 skill-12]", ids)
	}

	// Its subquery is built by a dry run whose statement never reaches the
	// database, so only the outer query can fail.
	failNth(db, 1)
	if _, err := r.GetWorkspaceSkillStorageIDs(ctx, skWS); !errors.Is(err, errInjected) {
		t.Errorf("got %v, want the injected failure", err)
	}
}

func TestDeleteSkill_MissingIsNotFound(t *testing.T) {
	r := New(&mockDB{db: skillDB(t)})
	if _, err := r.DeleteSkill(context.Background(), 999); !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

// Dropping the skills table would fail the share delete before it, so the
// skills delete itself is failed directly.
func TestDeleteWorkspace_FailsWhenSkillsDeleteFails(t *testing.T) {
	db := skillDB(t)
	if err := db.AutoMigrate(&model.Workspace{}, &model.Message{}, &model.ToolCall{}, &model.SlackTaskThread{}, &model.Task{}, &model.TaskStateTransition{}, &model.TaskLatency{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	r := New(&mockDB{db: db})
	seedSkill(t, r)
	if err := db.Create(&model.Workspace{ID: skWS, UserID: skUser, Name: "doomed"}).Error; err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	_ = db.Callback().Delete().Before("gorm:delete").Register("test:fail-skills", func(tx *gorm.DB) {
		if tx.Statement.Table == "skills" {
			_ = tx.AddError(errInjected)
		}
	})
	if err := r.DeleteWorkspace(context.Background(), skWS, skUser); !errors.Is(err, errInjected) {
		t.Fatalf("got %v, want the injected failure", err)
	}
	var files int64
	db.Model(&model.SkillFile{}).Count(&files)
	if files != 2 {
		t.Errorf("rollback failed: %d skill files left, want 2", files)
	}
}

// Both statements a search runs, the count and the page, report a failure.
// Statement 2 is the dry run that builds the page's shares subquery, whose own
// error is never returned, so it is skipped.
func TestSearchSkills_ReportsFailures(t *testing.T) {
	for _, n := range []int{1, 3} {
		db := skillDB(t)
		r := New(&mockDB{db: db})
		seedSkill(t, r)
		failNth(db, n)
		if _, _, err := r.SearchSkills(context.Background(), skUser, skWS, "tdd", false, 1, 0); !errors.Is(err, errInjected) {
			t.Errorf("statement %d: got %v", n, err)
		}
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
	if found, total, err := r.SearchSkills(ctx, skUser, skOther, "", true, 0, 0); err != nil || total != 0 || len(found) != 0 {
		t.Errorf("enabled only, in the share target: %v, %d, %v", found, total, err)
	}
	if found, total, err := r.SearchSkills(ctx, skUser, skWS, "tdd", false, 0, 0); err != nil || total != 1 || !found[0].Disabled {
		t.Errorf("every skill: %v, %d, %v", found, total, err)
	}
	if err := r.SetSkillDisabled(ctx, 2, true); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing skill: %v", err)
	}
	failNth(db, 1)
	if err := r.SetSkillDisabled(ctx, 1, false); !errors.Is(err, errInjected) {
		t.Errorf("failure: %v", err)
	}
}
