// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package crud

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"slices"
	"strings"
	"testing"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/mustafaturan/monoflake"
	"gorm.io/gorm"
)

// legacySkill stores a skill the way it was kept before skills belonged to the
// account: filed under ws, its blobs under the workspace's folder, and shared
// into each of sharedInto. files maps each path to its content.
func (e *skillEnv) legacySkill(t *testing.T, id, ws int64, name string, disabled bool, files map[string]string, sharedInto ...int64) {
	t.Helper()
	s := model.Skill{ID: id, UserID: skUser, WorkspaceID: ws, Name: name, Description: "d", SourceType: skillSourceManual, Disabled: disabled}
	if err := e.db.Create(&s).Error; err != nil {
		t.Fatalf("seed legacy skill %s: %v", name, err)
	}
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	slices.Sort(paths)
	for i, p := range paths {
		f := model.SkillFile{ID: id*100 + int64(i), SkillID: id, Path: p}
		key := "w-" + monoflake.ID(ws).String() + "/skill-" + monoflake.ID(id).String() + "/" + monoflake.ID(f.ID).String()
		if err := e.c.skillStorage.Save(key, base64.StdEncoding.EncodeToString([]byte(files[p]))); err != nil {
			t.Fatal(err)
		}
		f.StorageID = key
		f.SizeBytes = len(files[p])
		f.SHA256 = sha256Hex(files[p])
		if err := e.db.Create(&f).Error; err != nil {
			t.Fatal(err)
		}
	}
	for i, target := range sharedInto {
		if err := e.db.Create(&model.SkillShare{ID: id*100 + 50 + int64(i), UserID: skUser, SkillID: id, TargetWorkspaceID: target}).Error; err != nil {
			t.Fatal(err)
		}
	}
}

func sha256Hex(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// accountSkill reads one of the account's skills and its file's content.
func (e *skillEnv) accountSkill(t *testing.T, name, path string) (entity.Skill, string) {
	t.Helper()
	rs, err := e.c.GetSkillFile(e.ctx, entity.GetSkillFileRequest{UserID: skUserStr, Name: name, Path: path})
	if err != nil {
		t.Fatalf("read %s/%s from the account: %v", name, path, err)
	}
	return rs.Skill, rs.File.Content
}

func (e *skillEnv) legacyCount(t *testing.T) int64 {
	t.Helper()
	var n int64
	e.db.Model(&model.Skill{}).Where("workspace_id <> 0").Count(&n)
	return n
}

func TestMoveLegacySkills_MovesASkillAndItsShares(t *testing.T) {
	e := newSkillEnv(t)
	e.legacySkill(t, 10, skWS, "tdd", false, map[string]string{"SKILL.md": md("tdd", "Test first."), "notes.md": "red, green"}, skWS2)

	if err := e.c.MoveLegacySkills(e.ctx); err != nil {
		t.Fatalf("MoveLegacySkills: %v", err)
	}
	s, content := e.accountSkill(t, "tdd", "notes.md")
	if content != "red, green" || !slices.Equal(s.WorkspaceIDs, []int64{skWS, skWS2}) || !s.Enabled || s.FileCount != 2 {
		t.Fatalf("the moved skill reads %+v with notes %q; want it on in alpha and beta with its two files", s, content)
	}
	// Its files now live under the account's folder, and only there.
	var files []model.SkillFile
	e.db.Where("skill_id = ?", 10).Find(&files)
	for _, f := range files {
		if !strings.HasPrefix(f.StorageID, "u-"+monoflake.ID(skUser).String()+"/skill-"+monoflake.ID(10).String()+"/") {
			t.Errorf("file %s is stored at %q, want it under the account's folder", f.Path, f.StorageID)
		}
	}
	if n := e.blobs(t); n != 2 {
		t.Errorf("storage holds %d blobs, want the 2 moved ones", n)
	}
	var shares int64
	e.db.Model(&model.SkillShare{}).Count(&shares)
	if shares != 0 {
		t.Errorf("%d shares left after the move, want 0", shares)
	}

	// A second start finds nothing left to move.
	if err := e.c.MoveLegacySkills(e.ctx); err != nil || e.blobs(t) != 2 {
		t.Errorf("a second run returned %v and left %d blobs, want no error and 2", err, e.blobs(t))
	}
}

func TestMoveLegacySkills_MergesTheSameSkill(t *testing.T) {
	e := newSkillEnv(t)
	files := map[string]string{"SKILL.md": md("tdd", "Test first.")}
	e.legacySkill(t, 10, skWS, "tdd", false, files)
	e.legacySkill(t, 11, skWS2, "tdd", false, files, skWS3)

	if err := e.c.MoveLegacySkills(e.ctx); err != nil {
		t.Fatalf("MoveLegacySkills: %v", err)
	}
	s, _ := e.accountSkill(t, "tdd", "SKILL.md")
	if s.ID != 10 || !slices.Equal(s.WorkspaceIDs, []int64{skWS, skWS2, skWS3}) {
		t.Errorf("the merged skill reads %+v, want skill 10 on in all three workspaces", s)
	}
	var n int64
	e.db.Model(&model.Skill{}).Count(&n)
	if n != 1 || e.blobs(t) != 1 {
		t.Errorf("the account has %d skills and %d blobs, want 1 of each", n, e.blobs(t))
	}
}

// A clash that is not the same skill — other files, or one switched off and
// the other on — is renamed after its workspace, its SKILL.md with it.
func TestMoveLegacySkills_RenamesAClash(t *testing.T) {
	e := newSkillEnv(t)
	e.legacySkill(t, 10, skWS, "tdd", false, map[string]string{"SKILL.md": md("tdd", "Alpha's.")})
	e.legacySkill(t, 11, skWS2, "tdd", false, map[string]string{"SKILL.md": md("tdd", "Beta's.")})
	e.legacySkill(t, 12, skWS3, "tdd", true, map[string]string{"SKILL.md": md("tdd", "Alpha's.")})

	if err := e.c.MoveLegacySkills(e.ctx); err != nil {
		t.Fatalf("MoveLegacySkills: %v", err)
	}
	for _, want := range []struct {
		name string
		ws   int64
		on   bool
	}{{"tdd", skWS, true}, {"tdd-beta", skWS2, true}, {"tdd-gamma", skWS3, false}} {
		s, content := e.accountSkill(t, want.name, "SKILL.md")
		if !slices.Equal(s.WorkspaceIDs, []int64{want.ws}) || s.Enabled != want.on || !strings.Contains(content, "name: "+want.name+"\n") {
			t.Errorf("%s reads %+v with SKILL.md %q; want it on in workspace %d, enabled %t, and named %s inside", want.name, s, content, want.ws, want.on, want.name)
		}
	}
	if n := e.blobs(t); n != 3 {
		t.Errorf("storage holds %d blobs, want 3", n)
	}
}

func TestFreeSkillName(t *testing.T) {
	e := newSkillEnv(t)
	for _, ws := range []model.Workspace{{ID: 3001, UserID: skUser, Name: "!!!"}, {ID: 3002, UserID: skUser, Name: "The very long name of a workspace"}} {
		if err := e.db.Create(&ws).Error; err != nil {
			t.Fatal(err)
		}
	}
	long := strings.Repeat("a", 60)
	for _, tc := range []struct {
		name  string
		skill model.Skill
		taken []string
		want  string
	}{
		{"after its workspace", model.Skill{Name: "tdd", WorkspaceID: skWS2}, []string{"tdd"}, "tdd-beta"},
		{"numbered when that is taken too", model.Skill{Name: "tdd", WorkspaceID: skWS2}, []string{"tdd", "tdd-beta", "tdd-beta-2"}, "tdd-beta-3"},
		{"numbered when the workspace has no usable name", model.Skill{Name: "tdd", WorkspaceID: 3001}, []string{"tdd"}, "tdd-2"},
		{"numbered when the workspace is gone", model.Skill{Name: "tdd", WorkspaceID: 9999}, []string{"tdd", "tdd-2"}, "tdd-3"},
		{"cut to the longest name, keeping the suffix", model.Skill{Name: long, WorkspaceID: skWS2}, []string{long}, long[:59] + "-beta"},
		{"with no more than the start of a long workspace name", model.Skill{Name: "tdd", WorkspaceID: 3002}, []string{"tdd"}, "tdd-the-very-long-name-of-a"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			taken := map[string]model.Skill{}
			for _, n := range tc.taken {
				taken[n] = model.Skill{}
			}
			if got := e.c.freeSkillName(e.ctx, tc.skill, taken); got != tc.want {
				t.Errorf("freeSkillName returned %q, want %q", got, tc.want)
			}
		})
	}
}

func TestNameSlug(t *testing.T) {
	for in, want := range map[string]string{"Agent RQ — Code!": "agent-rq-code", "  ": "", "v2.1": "v2-1"} {
		if got := nameSlug(in); got != want {
			t.Errorf("nameSlug(%q) returned %q, want %q", in, got, want)
		}
	}
}

// A skill that cannot be moved is left where it was, for the next start, and
// the others still move.
func TestMoveLegacySkills_LeavesASkillThatFails(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(e *skillEnv)
	}{
		{"its blob is missing", func(e *skillEnv) {
			var f model.SkillFile
			e.db.Where("skill_id = ?", 11).First(&f)
			_ = e.c.skillStorage.Delete(f.StorageID)
		}},
		{"its SKILL.md has no frontmatter to rename it in", func(e *skillEnv) {
			var f model.SkillFile
			e.db.Where("skill_id = ?", 11).First(&f)
			_ = e.c.skillStorage.Save(f.StorageID, base64.StdEncoding.EncodeToString([]byte("no frontmatter")))
		}},
		{"its shares cannot be read", func(e *skillEnv) { failOn(e.db, "query", "skill_shares") }},
		{"its files cannot be read", func(e *skillEnv) { failOn(e.db, "query", "skill_files") }},
		{"the move cannot be written", func(e *skillEnv) { failOn(e.db, "update", "skills") }},
		{"the storage is full", func(e *skillEnv) { e.c.skillStorage = &failingStorage{Service: e.c.skillStorage, failAfter: 0} }},
		{"the storage fills on the second file", func(e *skillEnv) { e.c.skillStorage = &failingStorage{Service: e.c.skillStorage, failAfter: 1} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newSkillEnv(t)
			e.legacySkill(t, 10, skWS, "tdd", false, map[string]string{"SKILL.md": md("tdd", "Alpha's.")})
			if err := e.c.MoveLegacySkills(e.ctx); err != nil {
				t.Fatal(err)
			}
			e.legacySkill(t, 11, skWS2, "tdd", false, map[string]string{"SKILL.md": md("tdd", "Beta's."), "zz.md": "z"})
			blobs := e.blobs(t)
			tc.setup(e)
			if err := e.c.MoveLegacySkills(e.ctx); err != nil {
				t.Fatalf("one skill failing failed the whole run: %v", err)
			}
			if n := e.legacyCount(t); n != 1 {
				t.Errorf("%d skills still filed under a workspace, want the failed one", n)
			}
			if n := e.blobs(t); n > blobs {
				t.Errorf("storage went from %d to %d blobs; a failed move left its copies behind", blobs, n)
			}
		})
	}
}

func TestMoveLegacySkills_LeavesAMergeThatFails(t *testing.T) {
	for _, tc := range []struct {
		name, op, table string
	}{
		{"the files to compare cannot be read", "compare", "skill_files"},
		{"the merge cannot be written", "delete", "skills"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newSkillEnv(t)
			files := map[string]string{"SKILL.md": md("tdd", "Same.")}
			e.legacySkill(t, 10, skWS, "tdd", false, files)
			if err := e.c.MoveLegacySkills(e.ctx); err != nil {
				t.Fatal(err)
			}
			e.legacySkill(t, 11, skWS2, "tdd", false, files)
			if tc.op == "compare" {
				// Only the read of the account's tdd, skill 10, fails.
				e.db.Callback().Query().After("gorm:query").Register("test:fail-compare", func(tx *gorm.DB) {
					if tx.Statement.Table == "skill_files" && slices.Contains(tx.Statement.Vars, any(int64(10))) {
						_ = tx.AddError(errors.New("database unavailable"))
					}
				})
			} else {
				failOn(e.db, tc.op, tc.table)
			}
			if err := e.c.MoveLegacySkills(e.ctx); err != nil {
				t.Fatal(err)
			}
			if n := e.legacyCount(t); n != 1 {
				t.Errorf("%d skills still filed under a workspace, want the one that failed to merge", n)
			}
		})
	}
}

// Another instance moving the same skill first: this one's copies go, and
// its own move changes nothing.
func TestMoveLegacySkills_LosingARaceLeavesNoCopies(t *testing.T) {
	e := newSkillEnv(t)
	e.legacySkill(t, 10, skWS, "tdd", false, map[string]string{"SKILL.md": md("tdd", "Test first.")})
	// The other instance moves it between this one reading it and writing.
	moved := false
	e.db.Callback().Query().After("gorm:query").Register("test:moved-elsewhere", func(tx *gorm.DB) {
		if tx.Statement.Table == "skill_files" && !moved {
			moved = true
			e.db.Exec("UPDATE skills SET workspace_id = 0 WHERE id = 10")
		}
	})
	blobs := e.blobs(t)
	if err := e.c.MoveLegacySkills(e.ctx); err != nil {
		t.Fatal(err)
	}
	if n := e.blobs(t); n != blobs {
		t.Errorf("storage went from %d to %d blobs after losing the race", blobs, n)
	}
}

func TestMoveLegacySkills_ReportsWhatStopsTheRun(t *testing.T) {
	for _, tc := range []struct {
		name     string
		failRead int
	}{
		{"the legacy skills cannot be listed", 1},
		{"the account's skills cannot be listed", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newSkillEnv(t)
			e.legacySkill(t, 10, skWS, "tdd", false, map[string]string{"SKILL.md": md("tdd", "Test first.")})
			// The legacy list is the first read of skills, the account's the next.
			reads := 0
			e.db.Callback().Query().Before("gorm:query").Register("test:fail-skills-read", func(tx *gorm.DB) {
				if tx.Statement.Table == "skills" {
					reads++
					if reads == tc.failRead {
						_ = tx.AddError(errors.New("database unavailable"))
					}
				}
			})
			if err := e.c.MoveLegacySkills(e.ctx); err == nil || !strings.Contains(err.Error(), "database unavailable") {
				t.Errorf("MoveLegacySkills returned %v, want the database failure", err)
			}
		})
	}
}
