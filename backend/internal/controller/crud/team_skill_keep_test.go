// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package crud

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/skill"
)

// keepEnv is a team skill env where Alice's alpha (skWS) is opted into the
// team's deploy, lint and fresh but out of gone, and her beta (skWS2) into
// docs' notes. Her own account has deploy with the team's files, and a lint
// of its own.
func keepEnv(t *testing.T) (*teamSkillEnv, map[string]int64) {
	t.Helper()
	e := newTeamSkillEnv(t)
	if err := e.db.AutoMigrate(&model.PanoDocShare{}, &model.MemoryExclusion{}, &model.TeamInvitation{}); err != nil {
		t.Fatal(err)
	}
	ids := map[string]int64{}
	for _, name := range []string{"deploy", "lint", "fresh", "gone"} {
		ids[name] = e.mustCreate(t, name).Skill.ID
	}
	notes, err := e.c.SaveWorkspaceTeamSkillFile(entity.WithOrigin(e.ctx, entity.OriginMCP), entity.SaveWorkspaceTeamSkillFileRequest{
		WorkspaceID: skWS2, UserID: skUserStr, Name: "notes", Content: md("notes", "About notes."), TeamWorkspaceID: tkDocs})
	if err != nil {
		t.Fatal(err)
	}
	ids["notes"] = notes.Skill.ID
	if _, err := e.c.SetWorkspaceTeamSkill(e.ctx, entity.SetWorkspaceTeamSkillRequest{WorkspaceID: skWS, UserID: skUserStr, SkillID: ids["gone"]}); err != nil {
		t.Fatal(err)
	}
	ids["own deploy"] = e.save(t, skWS3, "deploy", skill.FileName, md("deploy", "About deploy.")).Skill.ID
	ids["own lint"] = e.save(t, skWS3, "lint", skill.FileName, md("lint", "Ours.")).Skill.ID
	return e, ids
}

func (e *teamSkillEnv) leave(user int64, keep base.TeamSkillCopies) ([]string, error) {
	var version int64
	e.db.Model(&model.Team{}).Where("id = ?", tkTeam).Pluck("version", &version)
	_, _, blobs, err := e.c.repository.RemoveTeamMember(e.ctx, tkTeam, user, user, version, model.TeamManagementActivity{ID: e.c.idgen.NextID(), CreatedAt: time.Now()}, e.c.idgen.NextID(), keep)
	return blobs, err
}

// accountSkills is the user's own skills, by name, with the workspaces each
// is on in.
func (e *teamSkillEnv) accountSkills(t *testing.T, user int64) map[string][]int64 {
	t.Helper()
	var rows []model.Skill
	e.db.Where("user_id = ? AND workspace_id = 0 AND team_workspace_id = 0", user).Find(&rows)
	out := map[string][]int64{}
	for _, s := range rows {
		var on []int64
		e.db.Model(&model.WorkspaceSkill{}).Where("skill_id = ?", s.ID).Order("workspace_id").Pluck("workspace_id", &on)
		out[s.Name] = on
	}
	return out
}

func (e *teamSkillEnv) accountFile(t *testing.T, user int64, name string) string {
	t.Helper()
	var f model.SkillFile
	if err := e.db.Joins("JOIN skills ON skills.id = skill_files.skill_id").
		Where("skills.user_id = ? AND skills.team_workspace_id = 0 AND skills.name = ? AND skill_files.path = ?", user, name, skill.FileName).
		First(&f).Error; err != nil {
		t.Fatalf("%s's SKILL.md: %v", name, err)
	}
	b, err := e.c.skillStorage.LoadRaw(f.StorageID)
	if err != nil {
		t.Fatalf("%s's blob: %v", name, err)
	}
	return string(b)
}

// A member who leaves keeps every team skill a workspace of theirs was
// opted into, on in it: their own skill when it has the same files, a copy
// under <name>-<team workspace> when theirs differs, and a copy of its own
// name otherwise. The last member's copies come before the team's skills go.
func TestKeepTeamSkills_LeavingAndTheLastMember(t *testing.T) {
	e, ids := keepEnv(t)
	keep, written, err := e.c.KeepTeamSkills(e.ctx, base.TeamSkillsLeaving{TeamID: tkTeam, UserID: skUser})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, s := range keep.Skills {
		names = append(names, s.Name)
	}
	slices.Sort(names)
	if !slices.Equal(names, []string{"fresh", "lint-backend", "notes"}) || len(written) != 3 || len(keep.Files) != 3 {
		t.Fatalf("the copies are %v, %d blobs, %d files; want fresh, lint-backend and notes", names, len(written), len(keep.Files))
	}
	if len(keep.Kept) != 4 || len(keep.On) != 4 {
		t.Errorf("kept %v, on %v; want four of Alice's workspaces' skills", keep.Kept, keep.On)
	}
	if _, err := e.leave(skUser, keep); err != nil {
		t.Fatal(err)
	}
	got := e.accountSkills(t, skUser)
	want := map[string][]int64{"deploy": {skWS, skWS3}, "lint": {skWS3}, "lint-backend": {skWS}, "fresh": {skWS}, "notes": {skWS2}}
	if len(got) != len(want) {
		t.Errorf("Alice's skills are %v, want %v", got, want)
	}
	for name, on := range want {
		if !slices.Equal(got[name], on) {
			t.Errorf("Alice's %s is on in %v, want %v", name, got[name], on)
		}
	}
	if body := e.accountFile(t, skUser, "lint-backend"); !strings.Contains(body, "name: lint-backend\n") || !strings.Contains(body, "About lint.") {
		t.Errorf("the renamed copy's SKILL.md is %q", body)
	}
	var choices int64
	e.db.Model(&model.TeamSkillChoice{}).Where("workspace_id = ?", skWS).Count(&choices)
	if choices != 0 {
		t.Errorf("alpha keeps %d choices of a team it left", choices)
	}

	// Bob, the last, keeps all four; the team's skills go with the team.
	keep, _, err = e.c.KeepTeamSkills(e.ctx, base.TeamSkillsLeaving{TeamID: tkTeam, UserID: skOther})
	if err != nil {
		t.Fatal(err)
	}
	blobs, err := e.leave(skOther, keep)
	if err != nil {
		t.Fatal(err)
	}
	if got := e.accountSkills(t, skOther); len(got) != 4 || got["gone"] == nil {
		t.Errorf("Bob kept %v, want deploy, lint, fresh and gone", got)
	}
	var left int64
	e.db.Model(&model.Skill{}).Where("team_workspace_id <> 0").Count(&left)
	if left != 0 || len(blobs) != 5 {
		t.Errorf("%d team skills stay, and %d blobs are to purge; want none and the five team files", left, len(blobs))
	}
	e.c.PurgeSkillBlobs(blobs)
	for _, b := range blobs {
		if _, err := e.c.skillStorage.LoadRaw(b); err == nil {
			t.Errorf("team blob %s was not purged", b)
		}
	}
	if body := e.accountFile(t, skOther, "deploy"); !strings.Contains(body, "About deploy.") {
		t.Errorf("Bob's deploy reads %q after the team's blobs went", body)
	}
	_ = ids
}

// A plan is refused when a team skill or a name it relied on changed
// before the change ran; a name the account holds as a link, or a name a
// copy already took, is never free.
func TestKeepTeamSkills_ChangesAndNames(t *testing.T) {
	e, ids := keepEnv(t)
	sel := base.TeamSkillsLeaving{TeamID: tkTeam, UserID: skUser}
	keep, _, err := e.c.KeepTeamSkills(e.ctx, sel)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.agentSave(skWS, skUserStr, "fresh", "", md("fresh", "Changed."), ptr(1)); err != nil {
		t.Fatal(err)
	}
	if _, err := e.leave(skUser, keep); !errors.Is(err, base.ErrTeamSkillsChanged) {
		t.Errorf("leaving on a plan from before a team skill changed: %v", err)
	}
	keep, _, _ = e.c.KeepTeamSkills(e.ctx, sel)
	e.save(t, skWS3, "fresh", skill.FileName, md("fresh", "Mine now."))
	if _, err := e.leave(skUser, keep); !errors.Is(err, base.ErrTeamSkillsChanged) {
		t.Errorf("leaving on a plan from before a name was taken: %v", err)
	}

	// fresh is Alice's own now, and notes a link: both copies are renamed,
	// and lint-backend taken already pushes lint to lint-backend-2.
	if err := e.db.Create(&model.Skill{ID: 7001, UserID: skUser, Name: "notes", LinkedSkillID: ids["deploy"]}).Error; err != nil {
		t.Fatal(err)
	}
	e.save(t, skWS3, "lint-backend", skill.FileName, md("lint-backend", "Taken."))
	keep, _, err = e.c.KeepTeamSkills(e.ctx, sel)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, s := range keep.Skills {
		names = append(names, s.Name)
	}
	slices.Sort(names)
	if !slices.Equal(names, []string{"fresh-backend", "lint-backend-2", "notes-docs"}) {
		t.Errorf("the copies are %v", names)
	}
	if _, err := e.leave(skUser, keep); err != nil {
		t.Errorf("leaving on a fresh plan: %v", err)
	}

	// Nothing to keep is an empty plan.
	keep, written, err := e.c.KeepTeamSkills(e.ctx, sel)
	if err != nil || len(keep.Kept)+len(keep.Skills)+len(written) != 0 {
		t.Errorf("a plan for nobody: %+v %v %v", keep, written, err)
	}
}

// A plan that fails leaves no blob behind.
func TestKeepTeamSkills_Failures(t *testing.T) {
	sel := base.TeamSkillsLeaving{TeamID: tkTeam, UserID: skUser}
	for _, tc := range []struct {
		name   string
		break_ func(e *teamSkillEnv)
	}{
		{"listing the skills", func(e *teamSkillEnv) { failTeamSQL(e.db, "row", "keep_workspace_id", 0) }},
		{"listing a team skill's files", func(e *teamSkillEnv) { failTeamSQL(e.db, "query", "skill_files", 0) }},
		{"reading an own name", func(e *teamSkillEnv) { failTeamSQL(e.db, "query", "`skills`", 0) }},
		{"listing an own skill's files", func(e *teamSkillEnv) { failTeamSQL(e.db, "query", "skill_files", 2) }},
		{"reading a blob", func(e *teamSkillEnv) {
			var f model.SkillFile
			e.db.Joins("JOIN skills ON skills.id = skill_files.skill_id AND skills.name = ? AND skills.team_workspace_id <> 0", "notes").First(&f)
			_ = e.c.skillStorage.Delete(f.StorageID)
		}},
		{"renaming a copy", func(e *teamSkillEnv) {
			var f model.SkillFile
			e.db.Joins("JOIN skills ON skills.id = skill_files.skill_id AND skills.name = ? AND skills.team_workspace_id <> 0", "lint").First(&f)
			_ = e.c.skillStorage.Save(f.StorageID, "bm8gZnJvbnRtYXR0ZXI=") // "no frontmatter"
		}},
		{"writing a blob", func(e *teamSkillEnv) { e.c.skillStorage = &failingStorage{Service: e.c.skillStorage, failAfter: 1} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, _ := keepEnv(t)
			before := e.blobs(t)
			tc.break_(e)
			if _, _, err := e.c.KeepTeamSkills(e.ctx, sel); err == nil {
				t.Error("the plan did not fail")
			}
			if fs, ok := e.c.skillStorage.(*failingStorage); ok {
				e.c.skillStorage = fs.Service
			}
			if n := e.blobs(t); n > before {
				t.Errorf("a failed plan left %d blobs, %d before", n, before)
			}
		})
	}
}
