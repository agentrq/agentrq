// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package app

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	"github.com/agentrq/agentrq/backend/internal/controller/mcp"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
)

func teamSkill(on bool) *entity.TeamSkill {
	return &entity.TeamSkill{Skill: entity.Skill{ID: 50, Name: "tdd", Files: []entity.SkillFile{{Path: "SKILL.md"}, {Path: "b.md"}}},
		TeamWorkspaceID: 40, TeamWorkspaceName: "Backend", On: on}
}

// teamOnly is a workspace whose account has no skill of the name, and whose
// team workspace has one.
func teamOnly(on bool) *fakeSkillCrud {
	return &fakeSkillCrud{getErr: base.ErrNotFound, fileErr: base.ErrNotFound, team: teamSkill(on)}
}

func refusalText(err error) string {
	var r *mcp.SkillRefusal
	if errors.As(err, &r) {
		return r.Message
	}
	return ""
}

// The agent sees its own skills and the team's it is opted into as one list
// by name, paged after merging; a team skill sharing a name with its own is
// left out, as the agent's own wins it.
func TestSkillStore_SearchMergesTheTeamsSkills(t *testing.T) {
	ctx := context.Background()
	f := &fakeSkillCrud{
		skills: []entity.Skill{{Name: "review", Description: "own review"}, {Name: "tdd", Description: "own tdd"}},
		teamSkills: []entity.TeamSkill{
			{Skill: entity.Skill{Name: "tdd", Description: "team tdd"}, TeamWorkspaceName: "Backend"},
			{Skill: entity.Skill{Name: "deploy", Description: "team deploy"}, TeamWorkspaceName: "Backend"},
		},
	}
	got, total, err := newSkillStore(f).SearchSkills(ctx, " de ", 0, 0)
	want := []mcp.SkillSummary{{Name: "deploy", Description: "team deploy", TeamWorkspace: "Backend"}, {Name: "review", Description: "own review"}, {Name: "tdd", Description: "own tdd"}}
	if err != nil || total != 3 || !reflect.DeepEqual(got, want) {
		t.Errorf("the search gave %+v of %d (%v), want %+v of 3", got, total, err, want)
	}
	if !reflect.DeepEqual(f.teamCalls, []string{"list de true"}) || f.search.Limit != 0 || f.search.Offset != 0 {
		t.Errorf("asked the team list %v and the account's %+v, want the opted-in team skills and every own match", f.teamCalls, f.search)
	}
	if got, total, _ := newSkillStore(f).SearchSkills(ctx, "", 1, 1); total != 3 || !reflect.DeepEqual(got, want[1:2]) {
		t.Errorf("the second page of one is %+v of %d, want %+v", got, total, want[1:2])
	}
	if got, total, _ := newSkillStore(f).SearchSkills(ctx, "", 2, 9); total != 3 || len(got) != 0 {
		t.Errorf("a page past the end is %+v of %d, want none of 3", got, total)
	}
	f.skills = make([]entity.Skill, crud.MaxSkillSearchLimit+5)
	for i := range f.skills {
		f.skills[i].Name = strings.Repeat("a", i+1)
	}
	if got, total, _ := newSkillStore(f).SearchSkills(ctx, "", 500, 0); len(got) != crud.MaxSkillSearchLimit || total != crud.MaxSkillSearchLimit+7 {
		t.Errorf("a limit over the most gave %d of %d, want %d", len(got), total, crud.MaxSkillSearchLimit)
	}
	g := &fakeSkillCrud{teamSkills: []entity.TeamSkill{{Skill: entity.Skill{Name: "x"}}}}
	calls := 0
	failing := &secondSearchFails{fakeSkillCrud: g, calls: &calls}
	if _, _, err := (&skillStore{crud: failing, workspaceID: 7, userID: "owner"}).SearchSkills(ctx, "", 0, 0); refusalText(err) != "second" {
		t.Errorf("a failure listing every own match gave %v", err)
	}
}

type secondSearchFails struct {
	*fakeSkillCrud
	calls *int
}

func (s *secondSearchFails) SearchSkills(ctx context.Context, req entity.SearchSkillsRequest) (*entity.SearchSkillsResponse, error) {
	if *s.calls++; *s.calls == 2 {
		return nil, &crud.SkillError{Message: "second"}
	}
	return s.fakeSkillCrud.SearchSkills(ctx, req)
}

// A team skill the workspace is opted into reads as the agent's own would,
// with its version and team workspace, and its files listed with SKILL.md.
func TestSkillStore_LoadsATeamSkill(t *testing.T) {
	ctx := context.Background()
	f := teamOnly(true)
	file, found, err := newSkillStore(f).LoadSkillFile(ctx, "tdd", "SKILL.md")
	want := mcp.SkillFile{Content: "team SKILL.md", Files: []string{"SKILL.md", "b.md"}, Version: 5, TeamWorkspace: "Backend"}
	if err != nil || !found || !reflect.DeepEqual(file, want) {
		t.Errorf("loading the team skill gave %+v %v %v, want %+v", file, found, err, want)
	}
	if file, found, _ := newSkillStore(teamOnly(true)).LoadSkillFile(ctx, "tdd", "b.md"); !found || file.Files != nil || file.Content != "team b.md" {
		t.Errorf("a team skill's other file read %+v %v", file, found)
	}
	// An own skill hidden here gives way to the team's: the human chose it.
	hidden := teamOnly(true)
	hidden.getErr, hidden.notHere = nil, true
	if file, found, _ := newSkillStore(hidden).LoadSkillFile(ctx, "tdd", "SKILL.md"); !found || file.TeamWorkspace != "Backend" {
		t.Errorf("an own skill hidden here hid the team's: %+v %v", file, found)
	}
	// One opted out of is not there for the agent, nor a missing file.
	if _, found, err := newSkillStore(teamOnly(false)).LoadSkillFile(ctx, "tdd", "SKILL.md"); found || err != nil {
		t.Errorf("a team skill opted out of was found (%v, %v)", found, err)
	}
	missing := teamOnly(true)
	missing.teamOpErr = base.ErrNotFound
	if _, found, err := newSkillStore(missing).LoadSkillFile(ctx, "tdd", "c.md"); found || err != nil {
		t.Errorf("a missing team file was found (%v, %v)", found, err)
	}
	failing := teamOnly(true)
	failing.teamOpErr = &crud.SkillError{Message: "bad path"}
	if _, _, err := newSkillStore(failing).LoadSkillFile(ctx, "tdd", "../x"); refusalText(err) != "bad path" {
		t.Errorf("a refused team read gave %v", err)
	}
}

// A write to a team skill goes to its team workspace on the version read; one
// the workspace is opted out of is refused, naming the switch to ask for.
func TestSkillStore_WritesATeamSkill(t *testing.T) {
	ctx := context.Background()
	f := teamOnly(true)
	s := newSkillStore(f)
	saved, err := s.SaveSkillFile(ctx, "tdd", "b.md", "x", 5, mcp.SkillTarget{TeamWorkspace: "ignored"})
	if err != nil || saved != (mcp.SkillSaved{Version: 6, TeamWorkspace: "Backend"}) {
		t.Errorf("saving a team file gave %+v, %v", saved, err)
	}
	if len(f.teamSaved) != 1 || f.teamSaved[0].TeamWorkspaceID != 40 || *f.teamSaved[0].Version != 5 || len(f.saved) != 0 {
		t.Errorf("the save was sent as %+v and %+v", f.teamSaved, f.saved)
	}
	if ok, err := s.DeleteSkillFile(ctx, "tdd", "b.md"); !ok || err != nil {
		t.Errorf("deleting a team file gave %v, %v", ok, err)
	}
	f.gone = true
	if d, err := s.DeleteSkill(ctx, "tdd"); err != nil || d != (mcp.SkillDeleted{Deleted: true, TeamWorkspace: "Backend", Gone: true}) {
		t.Errorf("deleting the team skill gave %+v, %v", d, err)
	}
	if !reflect.DeepEqual(f.teamCalls[len(f.teamCalls)-2:], []string{"get tdd", "optOut tdd"}) {
		t.Errorf("the delete asked %v, want an opt-out", f.teamCalls)
	}

	f.teamOpErr = &crud.SkillError{Message: "conflict"}
	if _, err := s.SaveSkillFile(ctx, "tdd", "b.md", "x", 1, mcp.SkillTarget{}); refusalText(err) != "conflict" {
		t.Errorf("a refused team save gave %v", err)
	}
	if d, err := s.DeleteSkill(ctx, "tdd"); d.Deleted || refusalText(err) != "conflict" {
		t.Errorf("a refused opt-out gave %+v, %v", d, err)
	}

	off := newSkillStore(teamOnly(false))
	want := `skill "tdd" is the team's, in team workspace "Backend", and this workspace is opted out of it, so this agent cannot use or change it; ask the human to opt this workspace in from its Skills tab, or choose another name`
	if _, err := off.SaveSkillFile(ctx, "tdd", "SKILL.md", "x", 0, mcp.SkillTarget{}); refusalText(err) != want {
		t.Errorf("a save to a team skill opted out of gave %v", err)
	}
	if _, err := off.DeleteSkill(ctx, "tdd"); refusalText(err) != want {
		t.Errorf("a delete of a team skill opted out of gave %v", err)
	}

	none := &fakeSkillCrud{getErr: base.ErrNotFound}
	if d, err := newSkillStore(none).DeleteSkill(ctx, "tdd"); d.Deleted || err != nil {
		t.Errorf("deleting no skill gave %+v, %v", d, err)
	}
	if ok, err := newSkillStore(none).DeleteSkillFile(ctx, "tdd", "a.md"); ok || err != nil {
		t.Errorf("deleting a file of no skill gave %v, %v", ok, err)
	}
	if len(none.deleted) != 0 {
		t.Errorf("nothing to delete still asked the controller: %+v", none.deleted)
	}
}

// A new skill goes to the team when the workspace is in one team workspace,
// unless the agent keeps it; in two, the agent must name one.
func TestSkillStore_ANewSkillsHome(t *testing.T) {
	ctx := context.Background()
	no, yes := false, true
	one := []entity.TeamWorkspaceRef{{ID: 40, Name: "Backend", TeamName: "core"}}
	two := append([]entity.TeamWorkspaceRef{{ID: 41, Name: "Infra", TeamName: "ops"}}, one...)
	for name, c := range map[string]struct {
		tws     []entity.TeamWorkspaceRef
		to      mcp.SkillTarget
		team    int64
		refusal string
	}{
		"no team workspace keeps it":       {to: mcp.SkillTarget{}},
		"one team workspace takes it":      {tws: one, team: 40},
		"team false keeps it":              {tws: one, to: mcp.SkillTarget{Team: &no}},
		"team true with one takes it":      {tws: one, to: mcp.SkillTarget{Team: &yes}, team: 40},
		"a name picks one, in any case":    {tws: two, to: mcp.SkillTarget{TeamWorkspace: " backend "}, team: 40},
		"team false and a name conflict":   {tws: one, to: mcp.SkillTarget{Team: &no, TeamWorkspace: "Backend"}, refusal: "team is false, which keeps the skill the account's, but teamWorkspace names a team workspace; send one or the other"},
		"a name with no team workspace":    {to: mcp.SkillTarget{TeamWorkspace: "Backend"}, refusal: `this workspace is not shared into a team workspace, so there is no "Backend" to save skill "new" to; leave teamWorkspace out to keep it the account's`},
		"a name it is not shared into":     {tws: two, to: mcp.SkillTarget{TeamWorkspace: "Web"}, refusal: `this workspace is not shared into a team workspace named "Web"; it is shared into "Infra" (team ops), "Backend" (team core)`},
		"team true with no team workspace": {to: mcp.SkillTarget{Team: &yes}, refusal: `this workspace is not shared into a team workspace, so skill "new" cannot be the team's; leave team out to keep it the account's`},
		"two team workspaces and no name":  {tws: two, refusal: `this workspace is shared into 2 team workspaces: "Infra" (team ops), "Backend" (team core). Send teamWorkspace with the one skill "new" belongs to, or team false to keep it the account's`},
	} {
		t.Run(name, func(t *testing.T) {
			f := &fakeSkillCrud{getErr: base.ErrNotFound, teamWorkspaces: c.tws}
			saved, err := newSkillStore(f).SaveSkillFile(ctx, "new", "SKILL.md", "x", 0, c.to)
			switch {
			case c.refusal != "":
				if refusalText(err) != c.refusal || len(f.saved)+len(f.teamSaved) != 0 {
					t.Errorf("gave %v, saving %+v %+v; want refused with %q", err, f.saved, f.teamSaved, c.refusal)
				}
			case c.team != 0:
				if err != nil || len(f.teamSaved) != 1 || f.teamSaved[0].TeamWorkspaceID != c.team || saved.TeamWorkspace != "Backend" || saved.Version != 6 {
					t.Errorf("gave %+v %v, saving %+v; want team workspace %d", saved, err, f.teamSaved, c.team)
				}
			default:
				if err != nil || len(f.saved) != 1 || len(f.teamSaved) != 0 || saved != (mcp.SkillSaved{Version: 9}) {
					t.Errorf("gave %+v %v, saving %+v %+v; want it the account's", saved, err, f.saved, f.teamSaved)
				}
			}
		})
	}
	failing := &fakeSkillCrud{getErr: base.ErrNotFound, teamListErr: &crud.SkillError{Message: "down"}}
	if _, err := newSkillStore(failing).SaveSkillFile(ctx, "new", "SKILL.md", "x", 0, mcp.SkillTarget{}); refusalText(err) != "down" {
		t.Errorf("a failed team workspace list gave %v", err)
	}
}
