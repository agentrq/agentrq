// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package base

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"gorm.io/gorm"

	"github.com/agentrq/agentrq/backend/internal/data/model"
)

// keptOf is TeamSkillCopies.Kept as the rows are now.
func keptOf(t *testing.T, db *gorm.DB, r Repository, sel TeamSkillsLeaving) map[KeptTeamSkill]string {
	t.Helper()
	rows, err := r.ListTeamSkillsToKeep(context.Background(), sel)
	if err != nil {
		t.Fatal(err)
	}
	out := map[KeptTeamSkill]string{}
	for _, s := range rows {
		var files []model.SkillFile
		db.Where("skill_id = ?", s.ID).Find(&files)
		out[KeptTeamSkill{WorkspaceID: s.KeepWorkspaceID, SkillID: s.ID}] = SkillFileSums(files)
	}
	return out
}

// alicesCopy is Alice keeping deploy as a copy of her own, on in 100, and
// review as nothing: a plan as KeepTeamSkills would make it.
func alicesCopy(t *testing.T, db *gorm.DB, r Repository, sel TeamSkillsLeaving) TeamSkillCopies {
	return TeamSkillCopies{
		Kept:   keptOf(t, db, r, sel),
		Names:  map[OwnSkillName]string{{UserID: tsAlice, Name: "deploy"}: ""},
		Skills: []model.Skill{{ID: 900, UserID: tsAlice, Name: "deploy", SourceType: "manual", CreatedAt: teamT0, UpdatedAt: teamT0, FileCount: 1}},
		Files:  []model.SkillFile{{ID: 9001, SkillID: 900, Path: "SKILL.md", StorageID: "u-deploy", Version: 1, CreatedAt: teamT0, UpdatedAt: teamT0}},
		On:     []model.WorkspaceSkill{{ID: 9002, UserID: tsAlice, SkillID: 900, WorkspaceID: 100, CreatedAt: teamT0}},
	}
}

func keepNames(rows []TeamSkillToKeep) string {
	out := make([]string, len(rows))
	for i, s := range rows {
		out[i] = fmt.Sprintf("%s@%d/%d", s.Name, s.KeepWorkspaceID, s.KeepUserID)
	}
	return strings.Join(out, " ")
}

// The skills kept are those the ending shares' workspaces are opted into
// while not archived; each workspace's choices go with its share, and a
// plan that no longer holds is refused before anything is written.
func TestKeepingTeamSkills(t *testing.T) {
	ctx := context.Background()
	forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
		seedTeamSkills(t, db)
		db.Create(&model.TeamSkillChoice{ID: 90, SkillID: tsReview, WorkspaceID: 200, UserID: tsBob, Off: true})
		db.Create(&model.TeamSkillChoice{ID: 91, SkillID: tsStyle, WorkspaceID: 201, UserID: tsBob})
		for _, c := range []struct {
			sel  TeamSkillsLeaving
			want string
		}{
			{TeamSkillsLeaving{TeamID: 1, UserID: tsAlice}, "deploy@100/10 review@100/10"},
			{TeamSkillsLeaving{TeamID: 1, WorkspaceID: 200}, "deploy@200/11"},
			{TeamSkillsLeaving{TeamID: 1}, "deploy@100/10 review@100/10 deploy@200/11 style@201/11"},
			{TeamSkillsLeaving{TeamID: 2, UserID: tsAlice}, ""},
		} {
			rows, err := r.ListTeamSkillsToKeep(ctx, c.sel)
			if got := keepNames(rows); err != nil || got != c.want {
				t.Errorf("keeping %+v: %q %v, want %q", c.sel, got, err, c.want)
			}
		}

		sel := TeamSkillsLeaving{TeamID: 1, UserID: tsAlice}
		staleKept := alicesCopy(t, db, r, sel)
		staleKept.Kept[KeptTeamSkill{WorkspaceID: 100, SkillID: tsDeploy}] = "changed"
		staleName := alicesCopy(t, db, r, sel)
		staleName.Names[OwnSkillName{UserID: tsAlice, Name: "review"}] = "+"
		db.Create(&model.Skill{ID: 950, UserID: tsAlice, Name: "taken", SourceType: "manual"})
		unread := alicesCopy(t, db, r, sel)
		unread.Skills[0].Name = "taken"
		for name, plan := range map[string]TeamSkillCopies{"a team skill changed": staleKept, "a name changed": staleName, "a name the plan never read": unread} {
			if _, _, _, err := r.RemoveTeamMember(ctx, 1, tsAlice, tsAlice, teamVersion(db, 1), act(), 0, plan); !errors.Is(err, ErrTeamSkillsChanged) {
				t.Errorf("%s: %v, want ErrTeamSkillsChanged", name, err)
			}
		}
		if skillExists(db, 900) {
			t.Error("a refused plan left its copy")
		}

		// Alice opted out of review in 100: she leaves with deploy only.
		db.Create(&model.TeamSkillChoice{ID: 92, SkillID: tsReview, WorkspaceID: 100, UserID: tsAlice, Off: true})
		plan := alicesCopy(t, db, r, sel)
		if len(plan.Kept) != 1 {
			t.Fatalf("Alice keeps %v", plan.Kept)
		}
		if _, _, _, err := r.RemoveTeamMember(ctx, 1, tsAlice, tsAlice, teamVersion(db, 1), act(), 0, plan); err != nil {
			t.Fatal(err)
		}
		var on []int64
		db.Model(&model.WorkspaceSkill{}).Where("skill_id = ?", 900).Pluck("workspace_id", &on)
		if !skillExists(db, 900) || !slices.Equal(on, []int64{100}) {
			t.Errorf("Alice's copy exists %v, on in %v; want on in 100", skillExists(db, 900), on)
		}
		if got := choices(db); got != "501@200:false 520@201:true" {
			t.Errorf("choices after Alice left: %q", got)
		}

		// Bob unshares 201, then the team goes: its skills, files and
		// choices with it, and their blobs to purge; team 2's stay.
		bob := TeamSkillsLeaving{TeamID: 1, WorkspaceID: 201}
		if _, err := r.DeleteWorkspaceShare(ctx, 201, 1, tsBob, teamVersion(db, 1), act(), TeamSkillCopies{Kept: keptOf(t, db, r, bob)}); err != nil {
			t.Fatal(err)
		}
		if got := choices(db); got != "501@200:false" {
			t.Errorf("choices after Bob unshared 201: %q", got)
		}
		all := TeamSkillsLeaving{TeamID: 1}
		blobs, err := r.DeleteTeam(ctx, 1, tsBob, teamVersion(db, 1), act(), TeamSkillCopies{Kept: keptOf(t, db, r, all)})
		if err != nil {
			t.Fatal(err)
		}
		slices.Sort(blobs)
		if !slices.Equal(blobs, []string{"t-500-skill", "t-501-skill", "t-520-skill"}) {
			t.Errorf("the team's blobs to purge are %v", blobs)
		}
		var left []int64
		db.Model(&model.Skill{}).Where("team_workspace_id <> 0").Pluck("id", &left)
		if !slices.Equal(left, []int64{tsOps}) || choices(db) != "" {
			t.Errorf("after the team went, team skills %v and choices %q stay", left, choices(db))
		}
	})
}

func TestSkillSums(t *testing.T) {
	a := []model.SkillFile{{Path: "b.md", SHA256: "2"}, {Path: "SKILL.md", SHA256: "1"}}
	b := []model.SkillFile{{Path: "SKILL.md", SHA256: "1"}, {Path: "b.md", SHA256: "2"}}
	if SkillFileSums(a) != SkillFileSums(b) || SkillFileSums(a) == SkillFileSums(b[:1]) {
		t.Error("the sums depend on the files' order, or not on their set")
	}
	if OwnSkillSums(model.Skill{LinkedSkillID: 1}, nil) != "link" || OwnSkillSums(model.Skill{}, nil) != "+" {
		t.Error("a link, or a skill with no files, reads as another")
	}
}

// Each statement of a change that ends shares fails it, with nothing kept
// and nothing of the team's gone.
func TestKeepingTeamSkillsFailsWithItsStatements(t *testing.T) {
	ctx := context.Background()
	leave := func(t *testing.T, db *gorm.DB, r Repository, plan TeamSkillCopies) error {
		_, _, _, err := r.RemoveTeamMember(ctx, 1, tsAlice, tsAlice, teamVersion(db, 1), act(), 0, plan)
		return err
	}
	deleteTeam := func(t *testing.T, db *gorm.DB, r Repository, _ TeamSkillCopies) error {
		_, err := r.DeleteTeam(ctx, 1, tsAlice, teamVersion(db, 1), act(), TeamSkillCopies{Kept: keptOf(t, db, r, TeamSkillsLeaving{TeamID: 1})})
		return err
	}
	for _, c := range []struct {
		name, kind, table string
		skip              int
		run               func(t *testing.T, db *gorm.DB, r Repository, plan TeamSkillCopies) error
	}{
		{"holding the team workspaces", "raw", "", 1, leave},
		{"listing what is kept", "row", "sh", 0, leave},
		{"reading the kept files", "query", "skill_files", 0, leave},
		{"reading a name", "query", "skills", 0, leave},
		{"reading an own skill's files", "query", "skill_files", 1, func(t *testing.T, db *gorm.DB, r Repository, plan TeamSkillCopies) error {
			db.Create(&model.Skill{ID: 950, UserID: tsAlice, Name: "deploy", SourceType: "manual"})
			plan.Names[OwnSkillName{UserID: tsAlice, Name: "deploy"}] = "+"
			return leave(t, db, r, plan)
		}},
		{"writing the copies", "create", "skills", 0, leave},
		{"writing their files", "create", "skill_files", 0, leave},
		{"turning them on", "create", "workspace_skills", 0, leave},
		{"listing the shares that end", "query", "workspace_shares", 0, leave},
		{"dropping the choices", "delete", "team_skill_choices", 0, leave},
		{"dropping an unshared workspace's choices", "delete", "team_skill_choices", 0, func(t *testing.T, db *gorm.DB, r Repository, _ TeamSkillCopies) error {
			_, err := r.DeleteWorkspaceShare(ctx, 100, 1, tsAlice, teamVersion(db, 1), act(), TeamSkillCopies{Kept: keptOf(t, db, r, TeamSkillsLeaving{TeamID: 1, WorkspaceID: 100})})
			return err
		}},
		{"holding the team workspaces to unshare", "raw", "", 1, func(t *testing.T, db *gorm.DB, r Repository, _ TeamSkillCopies) error {
			_, err := r.DeleteWorkspaceShare(ctx, 100, 1, tsAlice, teamVersion(db, 1), act(), TeamSkillCopies{})
			return err
		}},
		{"holding the team workspaces to delete", "raw", "", 1, deleteTeam},
		{"listing the team's blobs", "query", "skill_files", 1, deleteTeam},
		{"deleting the team's choices", "delete", "team_skill_choices", 0, deleteTeam},
		{"deleting the team's files", "delete", "skill_files", 0, deleteTeam},
		{"deleting the team's skills", "delete", "skills", 0, deleteTeam},
	} {
		t.Run(c.name, func(t *testing.T) {
			forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
				seedTeamSkills(t, db)
				db.Create(&model.TeamSkillChoice{ID: 99, SkillID: tsStyle, WorkspaceID: 201, UserID: tsBob})
				db.Create(&model.TeamSkillChoice{ID: 98, SkillID: tsReview, WorkspaceID: 100, UserID: tsAlice})
				plan := alicesCopy(t, db, r, TeamSkillsLeaving{TeamID: 1, UserID: tsAlice})
				state := func() string {
					var skills, shares, choices int64
					db.Raw("SELECT COUNT(*) FROM skills").Scan(&skills)
					db.Raw("SELECT COUNT(*) FROM workspace_shares").Scan(&shares)
					db.Raw("SELECT COUNT(*) FROM team_skill_choices").Scan(&choices)
					return fmt.Sprintf("%d skills, %d shares, %d choices", skills, shares, choices)
				}
				before := state()
				failTeamStatement(t, db, c.kind, c.table, c.skip)
				if err := c.run(t, db, r, plan); !errors.Is(err, errTeamDB) {
					t.Errorf("failing %s %s gave %v", c.kind, c.table, err)
				}
				if after := state(); after != before && !strings.HasPrefix(c.name, "reading an own") {
					t.Errorf("a failed change left %s, was %s", after, before)
				}
			})
		})
	}
}
