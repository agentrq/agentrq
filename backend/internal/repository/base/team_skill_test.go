// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package base

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"

	"gorm.io/gorm"

	"github.com/agentrq/agentrq/backend/internal/data/model"
)

// The team skill fixture, on forEachTeamDB's team 1 (Alice, 10):
//   - Bob (11) is in team 1 too; Carol (12) is in team 2 alone.
//   - Team workspace 50 "backend" holds deploy (500) and review (501); 52
//     "docs" holds style (520); 60 "infra", team 2's, holds ops (600).
//   - Alice's 100 is shared into 50, auto opt-in; Bob's 200 into 50 too, and
//     his 201 into 52 with auto opt-in off; Alice's archived 104 into 50;
//     Carol's 300 into 60.
const (
	tsAlice, tsBob, tsCarol            = int64(10), int64(11), int64(12)
	tsBackend, tsDocs, tsInfra         = int64(50), int64(52), int64(60)
	tsDeploy, tsReview, tsStyle, tsOps = int64(500), int64(501), int64(520), int64(600)
)

func seedTeamSkills(t *testing.T, db *gorm.DB) {
	t.Helper()
	archived := teamT0
	rows := []any{
		&model.TeamMember{ID: 2, TeamID: 1, UserID: tsBob, CreatedAt: teamT0},
		&model.Team{ID: 2, Name: "ops", Version: 1, CreatedAt: teamT0},
		&model.TeamMember{ID: 3, TeamID: 2, UserID: tsCarol, CreatedAt: teamT0},
		&model.Workspace{ID: 100, UserID: tsAlice, Name: "backend"},
		&model.Workspace{ID: 104, UserID: tsAlice, Name: "old", ArchivedAt: &archived},
		&model.Workspace{ID: 200, UserID: tsBob, Name: "backend"},
		&model.Workspace{ID: 201, UserID: tsBob, Name: "docs"},
		&model.Workspace{ID: 300, UserID: tsCarol, Name: "infra"},
		&model.TeamWorkspace{ID: tsBackend, TeamID: 1, Name: "backend", NameKey: "backend", Version: 1},
		&model.TeamWorkspace{ID: tsDocs, TeamID: 1, Name: "docs", NameKey: "docs", Version: 1},
		&model.TeamWorkspace{ID: tsInfra, TeamID: 2, Name: "infra", NameKey: "infra", Version: 1},
		&model.WorkspaceShare{ID: 1, TeamID: 1, WorkspaceID: 100, UserID: tsAlice, TeamWorkspaceID: tsBackend},
		&model.WorkspaceShare{ID: 2, TeamID: 1, WorkspaceID: 200, UserID: tsBob, TeamWorkspaceID: tsBackend},
		&model.WorkspaceShare{ID: 3, TeamID: 1, WorkspaceID: 201, UserID: tsBob, TeamWorkspaceID: tsDocs, ManualTeamSkills: true},
		&model.WorkspaceShare{ID: 4, TeamID: 1, WorkspaceID: 104, UserID: tsAlice, TeamWorkspaceID: tsBackend},
		&model.WorkspaceShare{ID: 5, TeamID: 2, WorkspaceID: 300, UserID: tsCarol, TeamWorkspaceID: tsInfra},
	}
	for _, s := range []struct {
		id, tw      int64
		name, about string
	}{
		{tsDeploy, tsBackend, "deploy", "Ship it."}, {tsReview, tsBackend, "review", "Read the diff."},
		{tsStyle, tsDocs, "style", "House style."}, {tsOps, tsInfra, "ops", "Run it."},
	} {
		rows = append(rows,
			&model.Skill{ID: s.id, Name: s.name, Description: s.about, TeamWorkspaceID: s.tw, SourceType: "manual", CreatedAt: teamT0, UpdatedAt: teamT0, FileCount: 1},
			&model.SkillFile{ID: s.id*10 + 1, SkillID: s.id, Path: "SKILL.md", StorageID: fmt.Sprintf("t-%d-skill", s.id), Version: 1, CreatedAt: teamT0, UpdatedAt: teamT0})
	}
	for _, row := range rows {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
}

func fromWorkspace(id int64) TeamSkillAccess {
	return TeamSkillAccess{WorkspaceID: id, NewID: testIDs, At: teamT0}
}

func asMember(userID, teamID int64) TeamSkillAccess {
	return TeamSkillAccess{UserID: userID, TeamID: teamID, NewID: testIDs, At: teamT0}
}

// teamSkillNames is the rows as "name:on", in order.
func teamSkillNames(rows []TeamSkillRow) string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = fmt.Sprintf("%s:%v", r.Name, r.On)
	}
	return strings.Join(out, " ")
}

func choices(db *gorm.DB) string {
	var cs []model.TeamSkillChoice
	db.Order("skill_id ASC, workspace_id ASC").Find(&cs)
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = fmt.Sprintf("%d@%d:%v", c.SkillID, c.WorkspaceID, !c.Off)
	}
	return strings.Join(out, " ")
}

func skillExists(db *gorm.DB, id int64) bool {
	var n int64
	db.Model(&model.Skill{}).Where("id = ?", id).Count(&n)
	return n == 1
}

// A workspace reaches the skills of the team workspaces it is shared into,
// on by its share's auto opt-in unless it chose otherwise, while it is not
// archived and its owner is in the team; nothing else.
func TestAWorkspaceReachesItsTeamWorkspacesSkills(t *testing.T) {
	forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
		seedTeamSkills(t, db)
		ctx := context.Background()
		list := func(ws int64, q string, onlyOn bool) string {
			rows, err := r.ListWorkspaceTeamSkills(ctx, ws, q, onlyOn)
			if err != nil {
				t.Fatal(err)
			}
			return teamSkillNames(rows)
		}
		for name, c := range map[string]struct {
			ws     int64
			q      string
			onlyOn bool
			want   string
		}{
			"auto opt-in":                       {ws: 100, want: "deploy:true review:true"},
			"a query on the description":        {ws: 100, q: "DIFF", want: "review:true"},
			"a query escapes its wildcards":     {ws: 100, q: "%", want: ""},
			"auto opt-in off":                   {ws: 201, want: "style:false"},
			"auto opt-in off, only the ones on": {ws: 201, onlyOn: true, want: ""},
			"an archived workspace":             {ws: 104, want: ""},
			"a workspace in another team":       {ws: 300, want: "ops:true"},
			"a workspace shared into no team":   {ws: 999, want: ""},
		} {
			if got := list(c.ws, c.q, c.onlyOn); got != c.want {
				t.Errorf("%s: %q, want %q", name, got, c.want)
			}
		}

		// A choice beats the share's setting, either way.
		db.Create(&model.TeamSkillChoice{ID: 1, SkillID: tsReview, WorkspaceID: 100, UserID: tsAlice, Off: true})
		db.Create(&model.TeamSkillChoice{ID: 2, SkillID: tsStyle, WorkspaceID: 201, UserID: tsBob})
		if got := list(100, "", true); got != "deploy:true" {
			t.Errorf("opted out of review, 100's agent sees %q", got)
		}
		if got := list(201, "", true); got != "style:true" {
			t.Errorf("opted into style, 201's agent sees %q", got)
		}

		row, err := r.GetWorkspaceTeamSkill(ctx, 100, "review")
		if err != nil || row.ID != tsReview || row.On || row.TeamID != 1 || row.TeamWorkspaceName != "backend" {
			t.Errorf("review from 100 reads %+v (%v)", row, err)
		}
		if _, err := r.GetWorkspaceTeamSkill(ctx, 100, "style"); !errors.Is(err, ErrNotFound) {
			t.Errorf("another team workspace's skill read %v, want not found", err)
		}

		// Alice leaving the team takes 100 out of it.
		db.Where("team_id = 1 AND user_id = ?", tsAlice).Delete(&model.TeamMember{})
		if got := list(100, "", false); got != "" {
			t.Errorf("after Alice left, 100 sees %q", got)
		}
	})
}

func TestTheTeamWorkspacesAWorkspaceIsSharedInto(t *testing.T) {
	forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
		seedTeamSkills(t, db)
		db.Create(&model.WorkspaceShare{ID: 6, TeamID: 1, WorkspaceID: 200, UserID: tsBob, TeamWorkspaceID: tsDocs})
		db.Model(&model.WorkspaceShare{}).Where("id = 6").Update("team_id", 3)
		got, err := r.ListWorkspaceTeamWorkspaces(context.Background(), 200)
		if err != nil || len(got) != 1 || got[0] != (WorkspaceTeamWorkspace{ID: tsBackend, Name: "backend", TeamID: 1, TeamName: "core"}) {
			t.Errorf("200 is shared into %+v (%v), want backend in core alone: Bob is in no team 3", got, err)
		}
		if got, _ := r.ListWorkspaceTeamWorkspaces(context.Background(), 104); len(got) != 0 {
			t.Errorf("an archived workspace is shared into %+v", got)
		}
	})
}

// A member reads every skill of the team's team workspaces, and nobody
// else does.
func TestAMemberReadsTheTeamsSkills(t *testing.T) {
	forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
		seedTeamSkills(t, db)
		ctx := context.Background()
		rows, err := r.ListTeamWorkspaceSkills(ctx, tsBob, 1, tsBackend)
		if err != nil || teamSkillNames(rows) != "deploy:true review:true" {
			t.Errorf("Bob lists %q (%v)", teamSkillNames(rows), err)
		}
		for name, c := range map[string]struct{ user, team, tw int64 }{
			"someone outside the team":        {tsCarol, 1, tsBackend},
			"another team's team workspace":   {tsBob, 1, tsInfra},
			"a team workspace not the team's": {tsCarol, 2, tsBackend},
		} {
			if _, err := r.ListTeamWorkspaceSkills(ctx, c.user, c.team, c.tw); !errors.Is(err, ErrNotFound) {
				t.Errorf("%s: %v, want not found", name, err)
			}
		}
		row, err := r.GetTeamSkill(ctx, tsBob, 1, tsDeploy)
		if err != nil || row.Name != "deploy" || !row.On || row.TeamWorkspaceName != "backend" {
			t.Errorf("Bob reads deploy as %+v (%v)", row, err)
		}
		if _, err := r.GetTeamSkill(ctx, tsCarol, 1, tsDeploy); !errors.Is(err, ErrNotFound) {
			t.Errorf("Carol reads deploy: %v", err)
		}
	})
}

func newTeamSkill(id, tw int64, name string) (model.Skill, []model.SkillFile) {
	return model.Skill{ID: id, Name: name, Description: "d", TeamWorkspaceID: tw, SourceType: "manual", CreatedAt: teamT0, UpdatedAt: teamT0},
		[]model.SkillFile{{ID: id*10 + 1, Path: "SKILL.md", StorageID: fmt.Sprintf("t-%d", id), SizeBytes: 5, Version: 1, CreatedAt: teamT0, UpdatedAt: teamT0}}
}

// A new team skill is made from a workspace shared into its team workspace
// or by a member, under the cap, and recorded; anyone else is refused.
func TestCreatingATeamSkill(t *testing.T) {
	forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
		seedTeamSkills(t, db)
		ctx := context.Background()
		s, files := newTeamSkill(700, tsBackend, "lint")
		got, teamID, err := r.CreateTeamSkill(ctx, fromWorkspace(200), s, files, 3)
		if err != nil || teamID != 1 || got.FileCount != 1 || got.TotalBytes != 5 {
			t.Fatalf("Bob's agent made %+v in team %d (%v)", got, teamID, err)
		}
		if want := `teamSkill.created 11 ws:200 {"skill":"lint","teamWorkspace":"backend"}`; lastActivity(db, 1) != want {
			t.Errorf("recorded %q, want %q", lastActivity(db, 1), want)
		}
		var tw model.TeamWorkspace
		db.First(&tw, tsBackend)
		if tw.Version != 2 {
			t.Errorf("the team workspace is at version %d, want it locked once", tw.Version)
		}

		s, files = newTeamSkill(701, tsBackend, "fmt")
		if _, _, err := r.CreateTeamSkill(ctx, asMember(tsAlice, 1), s, files, 3); !errors.Is(err, ErrTeamSkillCap) {
			t.Errorf("a fourth skill under a cap of 3: %v", err)
		}
		if _, _, err := r.CreateTeamSkill(ctx, asMember(tsAlice, 1), s, files, 0); err != nil {
			t.Errorf("no cap refused a skill: %v", err)
		}
		if want := `teamSkill.created 10 {"skill":"fmt","teamWorkspace":"backend"}`; lastActivity(db, 1) != want {
			t.Errorf("a member's create recorded %q", lastActivity(db, 1))
		}
		s, files = newTeamSkill(702, tsBackend, "deploy")
		if _, _, err := r.CreateTeamSkill(ctx, asMember(tsAlice, 1), s, files, 0); !errors.Is(err, gorm.ErrDuplicatedKey) {
			t.Errorf("a second deploy: %v", err)
		}
		for name, c := range map[string]struct {
			a  TeamSkillAccess
			tw int64
		}{
			"an archived workspace":        {fromWorkspace(104), tsBackend},
			"a workspace not shared there": {fromWorkspace(201), tsBackend},
			"someone outside the team":     {asMember(tsCarol, 1), tsBackend},
			"a team workspace not there":   {asMember(tsAlice, 1), 999},
		} {
			s, files = newTeamSkill(703, c.tw, "new")
			if _, _, err := r.CreateTeamSkill(ctx, c.a, s, files, 0); !errors.Is(err, ErrNotFound) {
				t.Errorf("%s: %v, want not found", name, err)
			}
		}
		if skillExists(db, 703) {
			t.Error("a refused skill was stored")
		}
	})
}

// A file of a team skill is written from a workspace opted into it, or by a
// member, on the version read; an opted-out workspace is refused.
func TestWritingATeamSkillsFiles(t *testing.T) {
	forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
		seedTeamSkills(t, db)
		ctx := context.Background()
		s := model.Skill{ID: tsDeploy, Name: "deploy", Description: "Ship it, carefully.", SourceType: "manual", UpdatedAt: teamT0}
		f := model.SkillFile{ID: 9001, Path: "SKILL.md", StorageID: "t-new", SizeBytes: 9, UpdatedAt: teamT0}
		one := int64(1)
		saved, stored, replaced, err := r.UpsertTeamSkillFile(ctx, fromWorkspace(200), s, f, &one)
		if err != nil || stored.Version != 2 || replaced != fmt.Sprintf("t-%d-skill", tsDeploy) || saved.TeamWorkspaceID != tsBackend || saved.Description != "Ship it, carefully." {
			t.Fatalf("Bob's agent wrote %+v %+v, replacing %q (%v)", saved, stored, replaced, err)
		}
		if _, _, _, err := r.UpsertTeamSkillFile(ctx, asMember(tsAlice, 1), s, f, &one); !errors.Is(err, ErrVersionConflict) {
			t.Errorf("a write on a stale version: %v", err)
		}
		g := model.SkillFile{ID: 9002, Path: "notes.md", StorageID: "t-notes", SizeBytes: 3, UpdatedAt: teamT0}
		if _, stored, _, err := r.UpsertTeamSkillFile(ctx, asMember(tsAlice, 1), s, g, nil); err != nil || stored.Version != 1 {
			t.Errorf("a member adding a file: %+v %v", stored, err)
		}
		saved, storageID, err := r.DeleteTeamSkillFile(ctx, fromWorkspace(100), s, "notes.md")
		if err != nil || storageID != "t-notes" || saved.FileCount != 1 {
			t.Errorf("deleting notes.md gave %+v %q (%v)", saved, storageID, err)
		}

		db.Create(&model.TeamSkillChoice{ID: 1, SkillID: tsDeploy, WorkspaceID: 100, UserID: tsAlice, Off: true})
		if _, _, _, err := r.UpsertTeamSkillFile(ctx, fromWorkspace(100), s, g, nil); !errors.Is(err, ErrTeamSkillOff) {
			t.Errorf("a write from a workspace opted out: %v", err)
		}
		if _, _, err := r.DeleteTeamSkillFile(ctx, fromWorkspace(100), s, "SKILL.md"); !errors.Is(err, ErrTeamSkillOff) {
			t.Errorf("a delete from a workspace opted out: %v", err)
		}
		if _, _, err := r.DeleteTeamSkillFile(ctx, fromWorkspace(201), s, "SKILL.md"); !errors.Is(err, ErrNotFound) {
			t.Errorf("a delete from a workspace in another team workspace: %v", err)
		}
		if _, _, _, err := r.UpsertTeamSkillFile(ctx, asMember(tsAlice, 1), model.Skill{ID: 4242}, g, nil); !errors.Is(err, ErrNotFound) {
			t.Errorf("a write to no skill: %v", err)
		}

		// A re-import replaces every file, keeping where the skill is.
		files := []model.SkillFile{{ID: 9003, Path: "SKILL.md", StorageID: "t-imported", SizeBytes: 4, CreatedAt: teamT0, UpdatedAt: teamT0}}
		re := model.Skill{ID: tsReview, Name: "review", Description: "Imported.", SourceType: "github", UpdatedAt: teamT0}
		got, dropped, err := r.ReplaceTeamSkill(ctx, asMember(tsBob, 1), re, files)
		if err != nil || got.TeamWorkspaceID != tsBackend || got.CreatedAt.IsZero() || len(dropped) != 1 || dropped[0] != fmt.Sprintf("t-%d-skill", tsReview) {
			t.Errorf("a re-import gave %+v, dropping %v (%v)", got, dropped, err)
		}
		if _, _, err := r.ReplaceTeamSkill(ctx, asMember(tsCarol, 1), re, files); !errors.Is(err, ErrNotFound) {
			t.Errorf("a re-import by someone outside the team: %v", err)
		}
	})
}

// Opting in and out sticks per workspace and is recorded; a member deletes
// a skill for everyone.
func TestOptingInAndOutAndDeleting(t *testing.T) {
	forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
		seedTeamSkills(t, db)
		ctx := context.Background()
		teamID, err := r.SetTeamSkillChoice(ctx, fromWorkspace(201), model.TeamSkillChoice{ID: 1, SkillID: tsStyle, UserID: tsBob})
		if err != nil || teamID != 1 || choices(db) != "520@201:true" {
			t.Fatalf("opting 201 into style: %d %v, choices %q", teamID, err, choices(db))
		}
		if want := `teamSkill.optedIn 11 ws:201 {"skill":"style","teamWorkspace":"docs"}`; lastActivity(db, 1) != want {
			t.Errorf("recorded %q", lastActivity(db, 1))
		}
		if _, err := r.SetTeamSkillChoice(ctx, fromWorkspace(201), model.TeamSkillChoice{ID: 2, SkillID: tsStyle, UserID: tsBob, Off: true}); err != nil || choices(db) != "520@201:false" {
			t.Errorf("opting back out: %v, choices %q", err, choices(db))
		}
		if !strings.HasPrefix(lastActivity(db, 1), "teamSkill.optedOut 11 ws:201") {
			t.Errorf("recorded %q", lastActivity(db, 1))
		}
		for name, c := range map[string]struct {
			ws, skill int64
		}{
			"a skill of a team workspace it is not shared into": {200, tsStyle},
			"an account's skill": {200, 4242},
		} {
			if _, err := r.SetTeamSkillChoice(ctx, fromWorkspace(c.ws), model.TeamSkillChoice{ID: 3, SkillID: c.skill}); !errors.Is(err, ErrNotFound) {
				t.Errorf("%s: %v", name, err)
			}
		}

		teamID, storageIDs, err := r.DeleteTeamSkill(ctx, asMember(tsBob, 1), tsStyle)
		if err != nil || teamID != 1 || len(storageIDs) != 1 || skillExists(db, tsStyle) || choices(db) != "" {
			t.Errorf("Bob deleting style: %d %v %v; still there %v, choices %q", teamID, storageIDs, err, skillExists(db, tsStyle), choices(db))
		}
		if want := `teamSkill.deleted 11 {"skill":"style","teamWorkspace":"docs"}`; lastActivity(db, 1) != want {
			t.Errorf("recorded %q", lastActivity(db, 1))
		}
		if _, _, err := r.DeleteTeamSkill(ctx, asMember(tsCarol, 1), tsDeploy); !errors.Is(err, ErrNotFound) {
			t.Errorf("Carol deleting deploy: %v", err)
		}
	})
}

// An agent's delete opts its workspace out; the skill goes when no
// workspace is opted in, an archived one with auto opt-in counting.
func TestAnAgentsDeleteOptsOut(t *testing.T) {
	forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
		seedTeamSkills(t, db)
		ctx := context.Background()
		optOut := func(ws, user int64) (bool, []string) {
			t.Helper()
			teamID, gone, ids, err := r.OptOutTeamSkill(ctx, fromWorkspace(ws), model.TeamSkillChoice{ID: testIDs(), SkillID: tsDeploy, UserID: user})
			if err != nil || teamID != 1 {
				t.Fatalf("%d opting out: %d %v", ws, teamID, err)
			}
			return gone, ids
		}
		if gone, _ := optOut(100, tsAlice); gone || !skillExists(db, tsDeploy) {
			t.Error("Alice's agent deleted deploy while Bob's 200 and the archived 104 are opted in")
		}
		if gone, _ := optOut(200, tsBob); gone {
			t.Error("deploy went while the archived 104 is still opted in")
		}
		if _, _, _, err := r.OptOutTeamSkill(ctx, fromWorkspace(100), model.TeamSkillChoice{ID: testIDs(), SkillID: tsDeploy}); !errors.Is(err, ErrTeamSkillOff) {
			t.Errorf("opting out twice: %v", err)
		}
		db.Model(&model.WorkspaceShare{}).Where("id = 4").Update("manual_team_skills", true)
		db.Model(&model.TeamSkillChoice{}).Where("workspace_id = 200").Update("off", false)
		gone, ids := optOut(200, tsBob)
		if !gone || skillExists(db, tsDeploy) || len(ids) != 1 || choices(db) != "" {
			t.Errorf("the last opt-out left the skill: gone %v, ids %v, choices %q", gone, ids, choices(db))
		}
		all := activities(db, 1)
		if got := strings.Join(all[len(all)-2:], "\n"); got != `teamSkill.optedOut 11 ws:200 {"skill":"deploy","teamWorkspace":"backend"}`+"\n"+`teamSkill.deleted 11 ws:200 {"skill":"deploy","teamWorkspace":"backend"}` {
			t.Errorf("the last opt-out recorded\n%s", got)
		}
	})
}

// Giving a skill moves the account's own skill into a team workspace,
// keeping its id and content, and takes it off the account's workspaces.
func TestGivingASkillToTheTeam(t *testing.T) {
	forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
		seedTeamSkills(t, db)
		ctx := context.Background()
		for _, row := range []any{
			&model.Skill{ID: 800, UserID: tsAlice, Name: "lint", SourceType: "manual", CreatedAt: teamT0, UpdatedAt: teamT0, Disabled: true},
			&model.SkillFile{ID: 8001, SkillID: 800, Path: "SKILL.md", StorageID: "u-lint", Version: 3},
			&model.WorkspaceSkill{SkillID: 800, WorkspaceID: 100},
			&model.Skill{ID: 801, UserID: tsAlice, Name: "deploy", SourceType: "manual", CreatedAt: teamT0, UpdatedAt: teamT0},
			&model.Skill{ID: 802, UserID: tsAlice, Name: "linked", LinkedSkillID: 800, SourceType: "team", CreatedAt: teamT0, UpdatedAt: teamT0},
		} {
			if err := db.Create(row).Error; err != nil {
				t.Fatal(err)
			}
		}
		files := []model.SkillFile{{ID: 8001, StorageID: "t-lint"}}
		teamID, replaced, err := r.GiveSkillToTeam(ctx, fromWorkspace(100), tsAlice, 800, tsBackend, files, 3)
		if err != nil || teamID != 1 || len(replaced) != 1 || replaced[0] != "u-lint" {
			t.Fatalf("giving lint: %d %v %v", teamID, replaced, err)
		}
		var s model.Skill
		db.First(&s, 800)
		var f model.SkillFile
		db.First(&f, 8001)
		var on int64
		db.Model(&model.WorkspaceSkill{}).Where("skill_id = 800").Count(&on)
		if s.UserID != 0 || s.TeamWorkspaceID != tsBackend || s.Disabled || f.StorageID != "t-lint" || f.Version != 3 || on != 0 {
			t.Errorf("lint is now %+v, its file %+v, on in %d workspaces", s, f, on)
		}
		if want := `teamSkill.given 10 ws:100 {"skill":"lint","teamWorkspace":"backend"}`; lastActivity(db, 1) != want {
			t.Errorf("recorded %q", lastActivity(db, 1))
		}
		for name, c := range map[string]struct {
			skill, tw int64
			cap       int
			want      error
		}{
			"over the cap":                           {801, tsBackend, 3, ErrTeamSkillCap},
			"a name the team workspace has":          {801, tsBackend, 0, gorm.ErrDuplicatedKey},
			"a link to a teammate's skill":           {802, tsBackend, 0, ErrNotFound},
			"a team skill already":                   {800, tsBackend, 0, ErrNotFound},
			"a team workspace it is not shared into": {801, tsDocs, 0, ErrNotFound},
			"no team workspace":                      {801, 999, 0, ErrNotFound},
		} {
			if _, _, err := r.GiveSkillToTeam(ctx, fromWorkspace(100), tsAlice, c.skill, c.tw, nil, c.cap); !errors.Is(err, c.want) {
				t.Errorf("%s: %v, want %v", name, err, c.want)
			}
		}
	})
}

// Turning a share's auto opt-in off keeps every team skill on now on: only
// later skills start off. Turning it back on changes no choice.
func TestTurningAutoOptInOffKeepsTodaysSkillsOn(t *testing.T) {
	forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
		seedTeamSkills(t, db)
		ctx := context.Background()
		db.Create(&model.TeamSkillChoice{ID: 1, SkillID: tsReview, WorkspaceID: 100, UserID: tsAlice, Off: true})
		tw := model.TeamWorkspace{ID: tsBackend, Name: "backend", NameKey: "backend"}
		share := model.WorkspaceShare{ID: 1, TeamID: 1, WorkspaceID: 100, UserID: tsAlice, ManualTeamSkills: true, CreatedAt: teamT0, UpdatedAt: teamT0}
		_, v, err := r.SetWorkspaceShare(ctx, share, tw, testIDs, 1, act())
		if err != nil || choices(db) != "500@100:true 501@100:false" {
			t.Fatalf("auto opt-in off: %v, choices %q", err, choices(db))
		}
		rows, _ := r.ListWorkspaceTeamSkills(ctx, 100, "", false)
		if teamSkillNames(rows) != "deploy:true review:false" {
			t.Errorf("after turning auto opt-in off, 100 reads %q", teamSkillNames(rows))
		}
		s, files := newTeamSkill(700, tsBackend, "lint")
		if _, _, err := r.CreateTeamSkill(ctx, asMember(tsBob, 1), s, files, 0); err != nil {
			t.Fatal(err)
		}
		rows, _ = r.ListWorkspaceTeamSkills(ctx, 100, "", false)
		if teamSkillNames(rows) != "deploy:true lint:false review:false" {
			t.Errorf("a later skill reads %q from 100, want it off", teamSkillNames(rows))
		}
		share.ManualTeamSkills = false
		if _, _, err := r.SetWorkspaceShare(ctx, share, tw, testIDs, v, act()); err != nil || choices(db) != "500@100:true 501@100:false" {
			t.Errorf("auto opt-in back on: %v, choices %q", err, choices(db))
		}
		rows, _ = r.ListWorkspaceTeamSkills(ctx, 100, "", false)
		if teamSkillNames(rows) != "deploy:true lint:true review:false" {
			t.Errorf("auto opt-in back on reads %q", teamSkillNames(rows))
		}
	})
}

// Deleting a workspace takes its choices; deleting a skill takes everyone's.
func TestChoicesGoWithTheirWorkspaceAndSkill(t *testing.T) {
	forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
		seedTeamSkills(t, db)
		for _, c := range []model.TeamSkillChoice{
			{ID: 1, SkillID: tsDeploy, WorkspaceID: 100, UserID: tsAlice},
			{ID: 2, SkillID: tsReview, WorkspaceID: 100, UserID: tsAlice},
			{ID: 3, SkillID: tsDeploy, WorkspaceID: 200, UserID: tsBob},
		} {
			db.Create(&c)
		}
		if err := db.AutoMigrate(&model.SiteShare{}, &model.ForkFolder{}, &model.AgentNote{}, &model.PanoDocTask{}, &model.Task{}, &model.Message{}, &model.ToolCall{}, &model.TaskPlan{},
			&model.SlackTaskThread{}, &model.EventTrigger{}, &model.WorkflowStep{}, &model.TaskStateTransition{}, &model.TaskLatency{}, &model.Agent{}, &model.AgentModel{}); err != nil {
			t.Fatal(err)
		}
		if err := r.DeleteWorkspace(context.Background(), 200, tsBob); err != nil {
			t.Fatal(err)
		}
		if choices(db) != "500@100:true 501@100:true" {
			t.Errorf("after deleting 200, choices are %q", choices(db))
		}
		if _, err := r.DeleteSkill(context.Background(), tsDeploy); err != nil || choices(db) != "501@100:true" {
			t.Errorf("after deleting deploy (%v), choices are %q", err, choices(db))
		}
	})
}

// Two members adding the last skill under the cap at once: one wins, as the
// team workspace's row is locked for the count.
func TestTheCapHoldsUnderParallelCreates(t *testing.T) {
	forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
		seedTeamSkills(t, db)
		errs := make(chan error, 8)
		for i := range 8 {
			go func() {
				s, files := newTeamSkill(int64(900+i), tsBackend, fmt.Sprintf("s%d", i))
				_, _, err := r.CreateTeamSkill(context.Background(), asMember(tsAlice, 1), s, files, 3)
				for err != nil && !errors.Is(err, ErrTeamSkillCap) && strings.Contains(strings.ToLower(err.Error()), "lock") {
					_, _, err = r.CreateTeamSkill(context.Background(), asMember(tsAlice, 1), s, files, 3)
				}
				errs <- err
			}()
		}
		var made, capped []string
		for range 8 {
			switch err := <-errs; {
			case err == nil:
				made = append(made, "ok")
			case errors.Is(err, ErrTeamSkillCap):
				capped = append(capped, "cap")
			default:
				t.Errorf("a parallel create failed: %v", err)
			}
		}
		var n int64
		db.Model(&model.Skill{}).Where("team_workspace_id = ?", tsBackend).Count(&n)
		sort.Strings(made)
		if len(made) != 1 || n != 3 {
			t.Errorf("%d creates won and the team workspace holds %d skills, want 1 and 3", len(made), n)
		}
	})
}

// Every statement of a team skill change that fails fails the change, and
// leaves nothing of it behind.
func TestTeamSkillChangesFailWithTheirStatements(t *testing.T) {
	ctx := context.Background()
	deploy := model.Skill{ID: tsDeploy, Name: "deploy", SourceType: "manual", UpdatedAt: teamT0}
	newFile := model.SkillFile{ID: 9002, Path: "notes.md", StorageID: "t-notes", UpdatedAt: teamT0}
	create := func(r Repository, a TeamSkillAccess, maxSkills int) error {
		s, files := newTeamSkill(700, tsBackend, "lint")
		_, _, err := r.CreateTeamSkill(ctx, a, s, files, maxSkills)
		return err
	}
	give := func(t *testing.T, db *gorm.DB, r Repository) error {
		db.Create(&model.Skill{ID: 800, UserID: tsAlice, Name: "lint", SourceType: "manual"})
		db.Create(&model.SkillFile{ID: 8001, SkillID: 800, Path: "SKILL.md", StorageID: "u-lint"})
		_, _, err := r.GiveSkillToTeam(ctx, fromWorkspace(100), tsAlice, 800, tsBackend, []model.SkillFile{{ID: 8001, StorageID: "t-lint"}}, 0)
		return err
	}
	manual := func(r Repository) error {
		share := model.WorkspaceShare{ID: 1, TeamID: 1, WorkspaceID: 100, UserID: tsAlice, ManualTeamSkills: true, CreatedAt: teamT0, UpdatedAt: teamT0}
		_, _, err := r.SetWorkspaceShare(ctx, share, model.TeamWorkspace{ID: tsBackend}, testIDs, 1, act())
		return err
	}
	for _, c := range []struct {
		name, kind, table string
		skip              int
		run               func(t *testing.T, db *gorm.DB, r Repository) error
	}{
		{"locking the team workspace", "raw", "", 0, func(t *testing.T, db *gorm.DB, r Repository) error { return create(r, fromWorkspace(200), 0) }},
		{"reaching the team workspace", "row", "sh", 0, func(t *testing.T, db *gorm.DB, r Repository) error { return create(r, fromWorkspace(200), 0) }},
		{"counting against the cap", "query", "skills", 0, func(t *testing.T, db *gorm.DB, r Repository) error { return create(r, asMember(tsAlice, 1), 9) }},
		{"storing the first files", "create", "skill_files", 0, func(t *testing.T, db *gorm.DB, r Repository) error { return create(r, asMember(tsAlice, 1), 0) }},
		{"checking the workspace is opted in", "query", "skills", 1, func(t *testing.T, db *gorm.DB, r Repository) error {
			_, _, _, err := r.UpsertTeamSkillFile(ctx, fromWorkspace(200), deploy, newFile, nil)
			return err
		}},
		{"deleting the skill", "delete", "skill_files", 0, func(t *testing.T, db *gorm.DB, r Repository) error {
			_, _, err := r.DeleteTeamSkill(ctx, asMember(tsAlice, 1), tsDeploy)
			return err
		}},
		{"storing a choice", "create", "team_skill_choices", 0, func(t *testing.T, db *gorm.DB, r Repository) error {
			_, err := r.SetTeamSkillChoice(ctx, fromWorkspace(100), model.TeamSkillChoice{ID: 1, SkillID: tsDeploy})
			return err
		}},
		{"opting out", "create", "team_skill_choices", 0, func(t *testing.T, db *gorm.DB, r Repository) error {
			_, _, _, err := r.OptOutTeamSkill(ctx, fromWorkspace(100), model.TeamSkillChoice{ID: 1, SkillID: tsDeploy})
			return err
		}},
		{"recording the opt-out", "create", "team_management_activities", 0, func(t *testing.T, db *gorm.DB, r Repository) error {
			_, _, _, err := r.OptOutTeamSkill(ctx, fromWorkspace(100), model.TeamSkillChoice{ID: 1, SkillID: tsDeploy})
			return err
		}},
		{"counting who is still opted in", "query", "sh", 0, func(t *testing.T, db *gorm.DB, r Repository) error {
			_, _, _, err := r.OptOutTeamSkill(ctx, fromWorkspace(100), model.TeamSkillChoice{ID: 1, SkillID: tsDeploy})
			return err
		}},
		{"deleting the skill nobody uses", "delete", "skill_files", 0, func(t *testing.T, db *gorm.DB, r Repository) error {
			_, _, _, err := r.OptOutTeamSkill(ctx, fromWorkspace(201), model.TeamSkillChoice{ID: 2, SkillID: tsStyle})
			return err
		}},
		{"reading the files given", "query", "skill_files", 0, give},
		{"moving the files given", "update", "skill_files", 0, give},
		{"taking the skill off the account's workspaces", "delete", "workspace_skills", 0, give},
		{"listing the skills on now", "query", "skills", 0, func(t *testing.T, db *gorm.DB, r Repository) error { return manual(r) }},
		{"keeping the skills on now", "create", "team_skill_choices", 0, func(t *testing.T, db *gorm.DB, r Repository) error { return manual(r) }},
	} {
		t.Run(c.name, func(t *testing.T) {
			forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
				seedTeamSkills(t, db)
				db.Create(&model.TeamSkillChoice{ID: 99, SkillID: tsStyle, WorkspaceID: 201, UserID: tsBob})
				// Read past the failing statements, which raw SQL is not.
				state := func() string {
					var n, choices int64
					db.Raw("SELECT COUNT(*) FROM skills WHERE team_workspace_id <> 0").Scan(&n)
					db.Raw("SELECT COUNT(*) FROM team_skill_choices WHERE off = ?", true).Scan(&choices)
					return fmt.Sprintf("%d team skills, %d opted out", n, choices)
				}
				before := state()
				failTeamStatement(t, db, c.kind, c.table, c.skip)
				if err := c.run(t, db, r); !errors.Is(err, errTeamDB) {
					t.Errorf("failing %s %s gave %v", c.kind, c.table, err)
				}
				if after := state(); after != before {
					t.Errorf("a failed change left %s, was %s", after, before)
				}
			})
		})
	}
}

func TestDeletingAMissingTeamSkillFile(t *testing.T) {
	forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
		seedTeamSkills(t, db)
		if _, _, err := r.DeleteTeamSkillFile(context.Background(), asMember(tsAlice, 1), model.Skill{ID: tsDeploy}, "nope.md"); !errors.Is(err, ErrNotFound) {
			t.Errorf("deleting a file not there: %v", err)
		}
	})
}

func TestDeletingAWorkspaceFailsWithItsChoices(t *testing.T) {
	forEachTeamDB(t, func(t *testing.T, db *gorm.DB, r Repository) {
		seedTeamSkills(t, db)
		if err := db.AutoMigrate(&model.SiteShare{}, &model.ForkFolder{}, &model.AgentNote{}, &model.PanoDocTask{}, &model.Task{}, &model.Message{}, &model.ToolCall{}, &model.TaskPlan{},
			&model.SlackTaskThread{}, &model.EventTrigger{}, &model.WorkflowStep{}, &model.TaskStateTransition{}, &model.TaskLatency{}, &model.Agent{}, &model.AgentModel{}); err != nil {
			t.Fatal(err)
		}
		failTeamStatement(t, db, "delete", "team_skill_choices", 0)
		if err := r.DeleteWorkspace(context.Background(), 200, tsBob); !errors.Is(err, errTeamDB) {
			t.Errorf("deleting 200 with its choices failing: %v", err)
		}
	})
}
