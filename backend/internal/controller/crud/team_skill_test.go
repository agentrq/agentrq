// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package crud

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
	"github.com/agentrq/agentrq/backend/internal/service/skill"
	"github.com/agentrq/agentrq/backend/internal/service/skillimport"
	"gorm.io/gorm"
)

// The team skill fixture adds to the skill one: team 1 "core" holds skUser
// (Alice) and skOther (Bob); team workspace 50 "backend" is the one Alice's
// alpha (skWS) and Bob's theirs (skForgn) are shared into, and 52 "docs" the
// one Alice's beta (skWS2) is. Team 2 holds nobody of theirs.
const (
	tkTeam, tkOtherTeam = int64(1), int64(2)
	tkBackend, tkDocs   = int64(50), int64(52)
	tkInfra             = int64(60) // team 2's
)

type recordingPubSub struct {
	pubsub.Service
	events []entity.CRUDEvent
}

func (p *recordingPubSub) Publish(_ context.Context, req pubsub.PublishRequest) (*pubsub.PublishResponse, error) {
	if e, ok := req.Event.(entity.CRUDEvent); ok && e.ResourceType == entity.ResourceTeamSkill {
		p.events = append(p.events, e)
	}
	return nil, nil
}

// line is the events as "action actor team", in order.
func (p *recordingPubSub) line() string {
	out := make([]string, len(p.events))
	for i, e := range p.events {
		actor := "human"
		if e.Actor == entity.ActorAgent {
			actor = "agent"
		}
		out[i] = fmt.Sprintf("%s %s %d", e.Action, actor, e.TeamID)
	}
	return strings.Join(out, ", ")
}

type teamSkillEnv struct {
	*skillEnv
	events   *recordingPubSub
	deployID int64
}

func newTeamSkillEnv(t *testing.T) *teamSkillEnv {
	t.Helper()
	e := newSkillEnv(t)
	if err := e.db.AutoMigrate(&model.User{}, &model.Team{}, &model.TeamWorkspace{}, &model.TeamManagementActivity{}); err != nil {
		t.Fatal(err)
	}
	for _, row := range []any{
		&model.Team{ID: tkTeam, Name: "core", Version: 1}, &model.Team{ID: tkOtherTeam, Name: "ops", Version: 1},
		&model.TeamMember{ID: 1, TeamID: tkTeam, UserID: skUser}, &model.TeamMember{ID: 2, TeamID: tkTeam, UserID: skOther},
		&model.TeamWorkspace{ID: tkBackend, TeamID: tkTeam, Name: "backend", NameKey: "backend", Version: 1},
		&model.TeamWorkspace{ID: tkDocs, TeamID: tkTeam, Name: "docs", NameKey: "docs", Version: 1},
		&model.TeamWorkspace{ID: tkInfra, TeamID: tkOtherTeam, Name: "infra", NameKey: "infra", Version: 1},
		&model.WorkspaceShare{ID: 1, TeamID: tkTeam, WorkspaceID: skWS, UserID: skUser, TeamWorkspaceID: tkBackend},
		&model.WorkspaceShare{ID: 2, TeamID: tkTeam, WorkspaceID: skForgn, UserID: skOther, TeamWorkspaceID: tkBackend},
		&model.WorkspaceShare{ID: 3, TeamID: tkTeam, WorkspaceID: skWS2, UserID: skUser, TeamWorkspaceID: tkDocs},
	} {
		if err := e.db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	ps := &recordingPubSub{}
	e.c.pubsub = ps
	t.Cleanup(func() { TeamSkillsPerWorkspace = DefaultTeamSkillsPerWorkspace })
	return &teamSkillEnv{skillEnv: e, events: ps}
}

func (e *teamSkillEnv) member(user int64) entity.TeamSkillsRequest {
	return entity.TeamSkillsRequest{UserID: user, TeamID: tkTeam, TeamWorkspaceID: tkBackend}
}

// agentSave saves a file from a workspace, as its agent's skills tool does.
func (e *teamSkillEnv) agentSave(ws int64, user, name, path, content string, version *int64) (*entity.TeamSkillFileResponse, error) {
	return e.c.SaveWorkspaceTeamSkillFile(entity.WithOrigin(e.ctx, entity.OriginMCP), entity.SaveWorkspaceTeamSkillFileRequest{
		WorkspaceID: ws, UserID: user, Name: name, Path: path, Content: content, Version: version, TeamWorkspaceID: tkBackend})
}

func (e *teamSkillEnv) mustCreate(t *testing.T, name string) *entity.TeamSkillFileResponse {
	t.Helper()
	rs, err := e.agentSave(skWS, skUserStr, name, "", md(name, "About "+name+"."), nil)
	if err != nil {
		t.Fatalf("creating %s: %v", name, err)
	}
	return rs
}

// teamBlobs is the paths of the team skills' blobs, under t-.
func (e *teamSkillEnv) teamBlobs(t *testing.T) []string {
	t.Helper()
	var out []string
	_ = filepath.WalkDir(e.dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(e.dir, p)
			out = append(out, rel)
		}
		return nil
	})
	return out
}

func ptr(v int64) *int64 { return &v }

// An agent's save of a new skill in a shared workspace makes it the team's:
// every workspace shared into the team workspace has it, it has no public
// links, and the change is sent to the team as the agent's.
func TestTeamSkills_AnAgentMakesOne(t *testing.T) {
	e := newTeamSkillEnv(t)
	rs := e.mustCreate(t, "deploy")
	if rs.Skill.TeamWorkspaceID != tkBackend || rs.Skill.TeamID != tkTeam || !rs.Skill.On || rs.File.Version != 1 || rs.File.Path != skill.FileName {
		t.Errorf("the new skill reads %+v", rs)
	}
	if e.events.line() != "team_skill_create agent 1" {
		t.Errorf("events: %s", e.events.line())
	}
	for _, b := range e.teamBlobs(t) {
		if !strings.HasPrefix(b, "t-") {
			t.Errorf("a team skill's blob is at %s, outside t-", b)
		}
	}
	for _, c := range []struct {
		ws   int64
		user string
	}{{skWS, skUserStr}, {skForgn, skOtherStr}} {
		got, err := e.c.ListWorkspaceTeamSkills(e.ctx, entity.WorkspaceTeamSkillsRequest{WorkspaceID: c.ws, UserID: c.user})
		if err != nil || len(got.Skills) != 1 || !got.Skills[0].On || got.Skills[0].Clash || got.Skills[0].TeamWorkspaceName != "backend" ||
			len(got.TeamWorkspaces) != 1 || got.TeamWorkspaces[0] != (entity.TeamWorkspaceRef{ID: tkBackend, Name: "backend", TeamID: tkTeam, TeamName: "core"}) {
			t.Errorf("%d lists %+v (%v)", c.ws, got, err)
		}
	}
	f, err := e.c.GetWorkspaceTeamSkillFile(e.ctx, entity.WorkspaceTeamSkillRequest{WorkspaceID: skForgn, UserID: skOtherStr, Name: "deploy"})
	if err != nil || f.File.Content != md("deploy", "About deploy.") || f.File.Version != 1 || len(f.Skill.Files) != 1 || f.Skill.Files[0].URL != "" {
		t.Errorf("Bob reads %+v (%v)", f, err)
	}
	if got, err := e.c.GetWorkspaceTeamSkill(e.ctx, entity.WorkspaceTeamSkillRequest{WorkspaceID: skForgn, UserID: skOtherStr, Name: "deploy"}); err != nil || len(got.Files) != 1 {
		t.Errorf("Bob reads the skill as %+v (%v)", got, err)
	}
	// One of the account's own of the name, on here, wins it.
	e.save(t, skWS, "deploy", skill.FileName, md("deploy", "Mine."))
	got, _ := e.c.ListWorkspaceTeamSkills(e.ctx, entity.WorkspaceTeamSkillsRequest{WorkspaceID: skWS, UserID: skUserStr})
	if !got.Skills[0].Clash {
		t.Error("an own skill of the same name is not flagged")
	}
}

// Writes to a team skill follow the version read, and every rule a skill
// file has; a refusal is written for the agent.
func TestTeamSkills_WritesAndTheirRefusals(t *testing.T) {
	e := newTeamSkillEnv(t)
	e.mustCreate(t, "deploy")
	if rs, err := e.agentSave(skForgn, skOtherStr, "deploy", "notes.md", "n", nil); err != nil || rs.File.Version != 1 {
		t.Fatalf("Bob adding a file: %+v %v", rs, err)
	}
	_, err := e.agentSave(skWS, skUserStr, "deploy", "notes.md", "m", nil)
	wantSkillErr(t, err, SkillConflict, "is shared with your team, so replacing skill://deploy/notes.md needs the version you read; it is now version 1")
	if rs, err := e.agentSave(skWS, skUserStr, "deploy", "notes.md", "m", ptr(1)); err != nil || rs.File.Version != 2 || rs.Skill.TeamWorkspaceID != tkBackend {
		t.Fatalf("Alice replacing on version 1: %+v %v", rs, err)
	}
	_, err = e.agentSave(skForgn, skOtherStr, "deploy", "notes.md", "o", ptr(1))
	wantSkillErr(t, err, SkillConflict, "was saved by someone else after version 1; it is now version 2")
	if rs, err := e.agentSave(skWS, skUserStr, "deploy", "", md("deploy", "Changed."), ptr(1)); err != nil || rs.Skill.Description != "Changed." {
		t.Errorf("rewriting SKILL.md: %+v %v", rs, err)
	}
	if e.events.line() != "team_skill_create agent 1" {
		t.Errorf("file writes sent events: %s", e.events.line())
	}

	for name, c := range map[string]struct {
		ws          int64
		user        string
		skill, path string
		content     string
		kind        SkillErrorKind
		want        string
	}{
		"a bad name":                     {skWS, skUserStr, "Bad Name", "", "x", SkillInvalid, "name"},
		"a bad path":                     {skWS, skUserStr, "deploy", "../x", "x", SkillInvalid, "path"},
		"a SKILL.md naming another":      {skWS, skUserStr, "deploy", "", md("other", "d"), SkillInvalid, `names the skill "other", but it is being saved as "deploy"`},
		"a SKILL.md with no front":       {skWS, skUserStr, "deploy", "", "no frontmatter", SkillInvalid, "frontmatter"},
		"a binary file":                  {skWS, skUserStr, "deploy", "bin.md", "a\x00b", SkillInvalid, "not UTF-8 text"},
		"a file of a skill not yet made": {skWS, skUserStr, "fresh", "a.md", "x", SkillInvalid, `skill "fresh" does not exist yet; save its SKILL.md first`},
	} {
		_, err := e.agentSave(c.ws, c.user, c.skill, c.path, c.content, nil)
		var se *SkillError
		if !errors.As(err, &se) || se.Kind != c.kind || !strings.Contains(se.Message, c.want) {
			t.Errorf("%s: %v, want %q", name, err, c.want)
		}
	}

	old := skill.MaxFiles
	skill.MaxFiles = 2
	t.Cleanup(func() { skill.MaxFiles = old })
	_, err = e.agentSave(skWS, skUserStr, "deploy", "third.md", "x", nil)
	wantSkillErr(t, err, SkillInvalid, `skill "deploy" already has 2 files, the limit`)

	// A workspace in no team workspace with the skill, or someone else's,
	// does not reach it.
	if _, err := e.agentSave(skWS2, skUserStr, "deploy", "x.md", "x", nil); err == nil {
		t.Error("beta, shared into docs, wrote backend's deploy")
	}
	if _, err := e.agentSave(skForgn, skUserStr, "deploy", "x.md", "x", nil); err == nil {
		t.Error("Alice wrote through Bob's workspace")
	}
}

// Past the cap, an agent is told to improve a skill instead; a name the team
// workspace has is a conflict.
func TestTeamSkills_TheCapAndTheName(t *testing.T) {
	e := newTeamSkillEnv(t)
	TeamSkillsPerWorkspace = 1
	e.mustCreate(t, "deploy")
	_, err := e.agentSave(skWS, skUserStr, "lint", "", md("lint", "d"), nil)
	wantSkillErr(t, err, SkillInvalid, "already holds 1 skills, its limit: improve an existing skill instead of adding \"lint\", or ask the human to make room")
	TeamSkillsPerWorkspace = 0
	_, err = e.c.SaveTeamSkillFile(e.ctx, entity.SaveTeamSkillFileRequest{TeamSkillsRequest: e.member(skOther), Content: md("deploy", "again")})
	wantSkillErr(t, err, SkillConflict, `the team workspace already has a skill named "deploy"`)
	if n := len(e.teamBlobs(t)); n != 1 {
		t.Errorf("refused creates left %d blobs, want deploy's alone", n)
	}
}

// Opting out is per workspace and sticks; an agent's delete opts out, and
// deletes the skill only when nobody is opted in.
func TestTeamSkills_OptingOutAndAgentsDeleting(t *testing.T) {
	e := newTeamSkillEnv(t)
	id := e.mustCreate(t, "deploy").Skill.ID
	e.agentSave(skWS, skUserStr, "deploy", "notes.md", "n", nil)

	off, err := e.c.SetWorkspaceTeamSkill(e.ctx, entity.SetWorkspaceTeamSkillRequest{WorkspaceID: skWS, UserID: skUserStr, SkillID: id})
	if err != nil || off.On || off.Name != "deploy" {
		t.Fatalf("opting alpha out: %+v %v", off, err)
	}
	_, err = e.agentSave(skWS, skUserStr, "deploy", "notes.md", "x", ptr(1))
	wantSkillErr(t, err, SkillInvalid, `skill "deploy" is the team's, and this workspace is opted out of it`)
	_, err = e.c.DeleteWorkspaceTeamSkillFile(e.ctx, entity.WorkspaceTeamSkillRequest{WorkspaceID: skWS, UserID: skUserStr, Name: "deploy", Path: "notes.md"})
	wantSkillErr(t, err, SkillInvalid, "opted out")
	_, err = e.c.DeleteWorkspaceTeamSkill(e.ctx, entity.WorkspaceTeamSkillRequest{WorkspaceID: skWS, UserID: skUserStr, Name: "deploy"})
	wantSkillErr(t, err, SkillInvalid, "opted out")
	on, err := e.c.SetWorkspaceTeamSkill(e.ctx, entity.SetWorkspaceTeamSkillRequest{WorkspaceID: skWS, UserID: skUserStr, SkillID: id, On: true})
	if err != nil || !on.On {
		t.Fatalf("opting back in: %+v %v", on, err)
	}
	if _, err := e.c.SetWorkspaceTeamSkill(e.ctx, entity.SetWorkspaceTeamSkillRequest{WorkspaceID: skWS2, UserID: skUserStr, SkillID: id, On: true}); !errors.Is(err, base.ErrNotFound) {
		t.Errorf("opting beta into backend's skill: %v", err)
	}

	if ts, err := e.c.DeleteWorkspaceTeamSkillFile(e.ctx, entity.WorkspaceTeamSkillRequest{WorkspaceID: skWS, UserID: skUserStr, Name: "deploy", Path: "notes.md"}); err != nil || ts.FileCount != 1 {
		t.Errorf("deleting notes.md: %+v %v", ts, err)
	}
	_, err = e.c.DeleteWorkspaceTeamSkillFile(e.ctx, entity.WorkspaceTeamSkillRequest{WorkspaceID: skWS, UserID: skUserStr, Name: "deploy", Path: skill.FileName})
	wantSkillErr(t, err, SkillInvalid, "a skill cannot lose its SKILL.md")
	_, err = e.c.DeleteWorkspaceTeamSkillFile(e.ctx, entity.WorkspaceTeamSkillRequest{WorkspaceID: skWS, UserID: skUserStr, Name: "deploy", Path: "../x"})
	wantSkillErr(t, err, SkillInvalid, "path")

	rs, err := e.c.DeleteWorkspaceTeamSkill(entity.WithOrigin(e.ctx, entity.OriginMCP), entity.WorkspaceTeamSkillRequest{WorkspaceID: skWS, UserID: skUserStr, Name: "deploy"})
	if err != nil || rs.Deleted {
		t.Fatalf("Alice's agent deleting deploy, with Bob opted in: %+v %v", rs, err)
	}
	rs, err = e.c.DeleteWorkspaceTeamSkill(entity.WithOrigin(e.ctx, entity.OriginMCP), entity.WorkspaceTeamSkillRequest{WorkspaceID: skForgn, UserID: skOtherStr, Name: "deploy"})
	if err != nil || !rs.Deleted || len(e.teamBlobs(t)) != 0 {
		t.Errorf("Bob's agent deleting deploy last: %+v %v, blobs %v", rs, err, e.teamBlobs(t))
	}
	if want := "team_skill_create agent 1, team_skill_opt_out human 1, team_skill_opt_in human 1, team_skill_opt_out agent 1, team_skill_opt_out agent 1, team_skill_delete agent 1"; e.events.line() != want {
		t.Errorf("events\n %s\nwant\n %s", e.events.line(), want)
	}
	if _, err := e.c.DeleteWorkspaceTeamSkill(e.ctx, entity.WorkspaceTeamSkillRequest{WorkspaceID: skWS, UserID: skUserStr, Name: "deploy"}); !errors.Is(err, base.ErrNotFound) {
		t.Errorf("deleting it again: %v", err)
	}
}

// Giving a skill moves it, with its content, to the team workspace: its
// account blobs go, and it keeps its id.
func TestTeamSkills_GivingOne(t *testing.T) {
	e := newTeamSkillEnv(t)
	own := e.save(t, skWS, "lint", skill.FileName, md("lint", "Lint it.")).Skill
	e.save(t, skWS, "lint", "rules.md", "no tabs")
	ts, err := e.c.GiveSkillToTeam(e.ctx, entity.GiveSkillToTeamRequest{WorkspaceID: skWS, UserID: skUserStr, Name: "lint", TeamWorkspaceID: tkBackend})
	if err != nil || ts.ID != own.ID || ts.TeamWorkspaceID != tkBackend || !ts.On {
		t.Fatalf("giving lint: %+v %v", ts, err)
	}
	blobs := e.teamBlobs(t)
	if len(blobs) != 2 || !strings.HasPrefix(blobs[0], "t-") || !strings.HasPrefix(blobs[1], "t-") {
		t.Errorf("after the gift the blobs are %v, want lint's two under t-", blobs)
	}
	f, err := e.c.GetWorkspaceTeamSkillFile(e.ctx, entity.WorkspaceTeamSkillRequest{WorkspaceID: skForgn, UserID: skOtherStr, Name: "lint", Path: "rules.md"})
	if err != nil || f.File.Content != "no tabs" {
		t.Errorf("Bob reads the gift's rules as %+v (%v)", f, err)
	}
	if e.events.line() != "team_skill_give human 1" {
		t.Errorf("events: %s", e.events.line())
	}

	e.save(t, skWS, "deploy", skill.FileName, md("deploy", "Mine."))
	e.mustCreate(t, "build")
	e.save(t, skWS, "build", skill.FileName, md("build", "Mine too."))
	e.db.Create(&model.Skill{ID: 9100, UserID: skUser, Name: "linked", LinkedSkillID: own.ID, SourceType: "team"})
	for name, c := range map[string]struct {
		skill string
		tw    int64
		cap   int
		kind  SkillErrorKind
		want  string
	}{
		"a link to a teammate's skill":  {"linked", tkBackend, 0, SkillInvalid, "only a skill of your own can be given"},
		"a name the team workspace has": {"build", tkBackend, 0, SkillConflict, `already has a skill named "build"`},
		"over the cap":                  {"deploy", tkBackend, 2, SkillInvalid, "already holds 2 skills"},
	} {
		TeamSkillsPerWorkspace = c.cap
		_, err := e.c.GiveSkillToTeam(e.ctx, entity.GiveSkillToTeamRequest{WorkspaceID: skWS, UserID: skUserStr, Name: c.skill, TeamWorkspaceID: c.tw})
		var se *SkillError
		if !errors.As(err, &se) || se.Kind != c.kind || !strings.Contains(se.Message, c.want) {
			t.Errorf("%s: %v, want %q", name, err, c.want)
		}
	}
	TeamSkillsPerWorkspace = 0
	for name, req := range map[string]entity.GiveSkillToTeamRequest{
		"no such skill":                          {WorkspaceID: skWS, UserID: skUserStr, Name: "nope", TeamWorkspaceID: tkBackend},
		"a team workspace it is not shared into": {WorkspaceID: skWS, UserID: skUserStr, Name: "deploy", TeamWorkspaceID: tkDocs},
		"someone else's workspace":               {WorkspaceID: skForgn, UserID: skUserStr, Name: "deploy", TeamWorkspaceID: tkBackend},
	} {
		if _, err := e.c.GiveSkillToTeam(e.ctx, req); err == nil {
			t.Errorf("%s was given", name)
		}
	}
	if _, err := e.c.GiveSkillToTeam(e.ctx, entity.GiveSkillToTeamRequest{WorkspaceID: skWS, UserID: skUserStr, Name: "Bad Name", TeamWorkspaceID: tkBackend}); err == nil {
		t.Error("a bad name was given")
	}
	// Refused gifts leave no copies behind: deploy's and build's own blobs,
	// and the team's lint and build.
	if n := e.blobs(t); n != 5 {
		t.Errorf("after the refusals there are %d blobs, want 5: %v", n, e.teamBlobs(t))
	}
}

// From the team's page a member reads and writes the team's skills, and
// deletes one for everyone; nobody outside the team, and no other team
// workspace's path, reaches them.
func TestTeamSkills_TheTeamsPage(t *testing.T) {
	e := newTeamSkillEnv(t)
	created, err := e.c.SaveTeamSkillFile(e.ctx, entity.SaveTeamSkillFileRequest{TeamSkillsRequest: e.member(skOther), Content: md("deploy", "Ship.")})
	if err != nil || created.Skill.Name != "deploy" || created.Skill.TeamID != tkTeam {
		t.Fatalf("Bob creating deploy: %+v %v", created, err)
	}
	id := created.Skill.ID
	at := e.member(skUser)
	at.SkillID = id
	list, err := e.c.ListTeamSkills(e.ctx, e.member(skUser))
	if err != nil || len(list) != 1 || list[0].ID != id || !list[0].On {
		t.Errorf("Alice lists %+v (%v)", list, err)
	}
	at.Path = "guide.md"
	if rs, err := e.c.SaveTeamSkillFile(e.ctx, entity.SaveTeamSkillFileRequest{TeamSkillsRequest: at, Content: "g"}); err != nil || rs.File.Version != 1 {
		t.Fatalf("Alice adding guide.md: %+v %v", rs, err)
	}
	got, err := e.c.GetTeamSkill(e.ctx, at)
	if err != nil || len(got.Files) != 2 || got.Files[1].Version != 1 {
		t.Errorf("Alice reads %+v (%v)", got, err)
	}
	f, err := e.c.GetTeamSkillFile(e.ctx, at)
	if err != nil || f.File.Content != "g" {
		t.Errorf("Alice reads guide.md as %+v (%v)", f, err)
	}
	at.Path = ""
	if f, err := e.c.GetTeamSkillFile(e.ctx, at); err != nil || f.File.Path != skill.FileName {
		t.Errorf("no path reads %+v (%v), want SKILL.md", f, err)
	}
	at.Path = "guide.md"
	if ts, err := e.c.DeleteTeamSkillFile(e.ctx, at); err != nil || ts.FileCount != 1 {
		t.Errorf("deleting guide.md: %+v %v", ts, err)
	}

	for name, req := range map[string]entity.SaveTeamSkillFileRequest{
		"a new skill from another file":   {TeamSkillsRequest: entity.TeamSkillsRequest{UserID: skUser, TeamID: tkTeam, TeamWorkspaceID: tkBackend, Path: "a.md"}, Content: "x"},
		"a new skill with no frontmatter": {TeamSkillsRequest: e.member(skUser), Content: "x"},
	} {
		if _, err := e.c.SaveTeamSkillFile(e.ctx, req); err == nil || !errors.As(err, new(*SkillError)) {
			t.Errorf("%s: %v", name, err)
		}
	}

	outsider := at
	outsider.UserID = skUser + 99
	wrongTW := at
	wrongTW.TeamWorkspaceID = tkDocs
	for name, req := range map[string]entity.TeamSkillsRequest{"someone outside the team": outsider, "another team workspace's path": wrongTW} {
		if _, err := e.c.GetTeamSkill(e.ctx, req); !errors.Is(err, base.ErrNotFound) {
			t.Errorf("%s reads it: %v", name, err)
		}
		if _, err := e.c.GetTeamSkillFile(e.ctx, req); !errors.Is(err, base.ErrNotFound) {
			t.Errorf("%s reads a file: %v", name, err)
		}
		if _, err := e.c.SaveTeamSkillFile(e.ctx, entity.SaveTeamSkillFileRequest{TeamSkillsRequest: req, Content: "x"}); !errors.Is(err, base.ErrNotFound) {
			t.Errorf("%s writes a file: %v", name, err)
		}
		if _, err := e.c.DeleteTeamSkillFile(e.ctx, req); !errors.Is(err, base.ErrNotFound) {
			t.Errorf("%s deletes a file: %v", name, err)
		}
		if err := e.c.DeleteTeamSkill(e.ctx, req); !errors.Is(err, base.ErrNotFound) {
			t.Errorf("%s deletes it: %v", name, err)
		}
	}
	if _, err := e.c.ListTeamSkills(e.ctx, outsider); !errors.Is(err, base.ErrNotFound) {
		t.Errorf("an outsider lists them: %v", err)
	}

	if err := e.c.DeleteTeamSkill(e.ctx, at); err != nil || e.blobs(t) != 0 {
		t.Errorf("Alice deleting deploy: %v, %d blobs left", err, e.blobs(t))
	}
	if want := "team_skill_create human 1, team_skill_delete human 1"; e.events.line() != want {
		t.Errorf("events %s, want %s", e.events.line(), want)
	}
}

// A member imports into a team workspace as into an account: a name it has
// is skipped unless overwritten, and one that cannot be saved is reported.
func TestTeamSkills_Import(t *testing.T) {
	e := newTeamSkillEnv(t)
	e.mustCreate(t, "deploy")
	res := githubResult(importedSkill("deploy", "Imported deploy."), importedSkill("review", "Review it.", skillimport.File{Path: "checklist.md", Content: []byte("c")}))
	res.Candidates = []skillimport.Candidate{{Name: "big", Path: "skills/big", SkillBytes: 9}}
	e.importer.res = res
	req := entity.ImportTeamSkillsRequest{TeamSkillsRequest: e.member(skOther), URL: "https://github.com/obra/superpowers"}
	rs, err := e.c.ImportTeamSkills(e.ctx, req)
	if err != nil || len(rs.Imported) != 1 || rs.Imported[0].Name != "review" || rs.Imported[0].TeamWorkspaceID != tkBackend || rs.Imported[0].FileCount != 2 ||
		len(rs.Skipped) != 2 || !strings.Contains(rs.Skipped[1].Reason, "already has a skill with this name") || len(rs.Candidates) != 1 || rs.SourceRepo != "obra/superpowers" {
		t.Fatalf("the import gave %+v (%v)", rs, err)
	}
	req.Overwrite = true
	rs, err = e.c.ImportTeamSkills(e.ctx, req)
	if err != nil || len(rs.Imported) != 2 {
		t.Fatalf("the overwrite gave %+v (%v)", rs, err)
	}
	f, _ := e.c.GetWorkspaceTeamSkillFile(e.ctx, entity.WorkspaceTeamSkillRequest{WorkspaceID: skWS, UserID: skUserStr, Name: "deploy"})
	if f.Skill.SourceType != skillSourceGitHub || f.File.Content != md("deploy", "Imported deploy.") {
		t.Errorf("the overwritten deploy reads %+v", f)
	}
	if want := "team_skill_create agent 1, team_skill_create human 1"; e.events.line() != want {
		t.Errorf("events %s, want %s", e.events.line(), want)
	}

	// One that cannot be saved is reported; the rest go on.
	TeamSkillsPerWorkspace = 2
	e.importer.res = githubResult(importedSkill("lint", "Lint."))
	req.Overwrite = false
	rs, err = e.c.ImportTeamSkills(e.ctx, req)
	if err != nil || len(rs.Imported) != 0 || !strings.Contains(rs.Skipped[len(rs.Skipped)-1].Reason, "could not be saved; try the import again: the team workspace already holds 2 skills") {
		t.Errorf("an import over the cap gave %+v (%v)", rs, err)
	}

	for _, c := range []struct {
		name  string
		setup func()
		req   entity.ImportTeamSkillsRequest
		check func(error) bool
	}{
		{"too many chosen", func() {}, entity.ImportTeamSkillsRequest{TeamSkillsRequest: e.member(skUser), URL: "u", Skills: make([]string, skillimport.MaxSelected+1)},
			func(err error) bool { return errors.As(err, new(*SkillError)) }},
		{"someone outside the team", func() {}, entity.ImportTeamSkillsRequest{TeamSkillsRequest: entity.TeamSkillsRequest{UserID: 7, TeamID: tkTeam, TeamWorkspaceID: tkBackend}, URL: "u"},
			func(err error) bool { return errors.Is(err, base.ErrNotFound) }},
		{"a link that is not a repository", func() { e.importer.err = skillimport.ErrInvalidURL }, entity.ImportTeamSkillsRequest{TeamSkillsRequest: e.member(skUser), URL: "u"},
			func(err error) bool { var se *SkillError; return errors.As(err, &se) && se.Kind == SkillInvalid }},
		{"GitHub down", func() { e.importer.err = errors.New("timeout") }, entity.ImportTeamSkillsRequest{TeamSkillsRequest: e.member(skUser), URL: "u"},
			func(err error) bool { var se *SkillError; return errors.As(err, &se) && se.Kind == SkillUpstream }},
		{"rate limited", func() { e.importer.err = nil; e.c.limiter = &stubLimiter{} }, entity.ImportTeamSkillsRequest{TeamSkillsRequest: e.member(skUser), URL: "u"},
			func(err error) bool { return err != nil && err.Error() == "rate limit exceeded" }},
		{"no importer", func() { e.c.skillImport = nil }, entity.ImportTeamSkillsRequest{TeamSkillsRequest: e.member(skUser), URL: "u"},
			func(err error) bool { var se *SkillError; return errors.As(err, &se) && se.Kind == SkillUpstream }},
	} {
		c.setup()
		if _, err := e.c.ImportTeamSkills(e.ctx, c.req); !c.check(err) {
			t.Errorf("%s: %v", c.name, err)
		}
	}
}

// A blob that cannot be written stops the write before any row, and one
// written for a refused change is removed.
func TestTeamSkills_StorageFailures(t *testing.T) {
	e := newTeamSkillEnv(t)
	e.mustCreate(t, "deploy")
	e.save(t, skWS, "lint", skill.FileName, md("lint", "Lint."))
	before := e.blobs(t)
	e.c.skillStorage = &failingStorage{Service: e.c.skillStorage}
	if _, err := e.agentSave(skWS, skUserStr, "fresh", "", md("fresh", "d"), nil); err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Errorf("creating with storage failing: %v", err)
	}
	if _, err := e.agentSave(skWS, skUserStr, "deploy", "a.md", "x", nil); err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Errorf("adding a file with storage failing: %v", err)
	}
	if _, err := e.c.GiveSkillToTeam(e.ctx, entity.GiveSkillToTeamRequest{WorkspaceID: skWS, UserID: skUserStr, Name: "lint", TeamWorkspaceID: tkBackend}); err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Errorf("giving with storage failing: %v", err)
	}
	e.importer.res = githubResult(importedSkill("review", "R.", skillimport.File{Path: "b.md", Content: []byte("b")}))
	e.c.skillStorage = &failingStorage{Service: e.c.skillStorage.(*failingStorage).Service, failAfter: 1}
	if rs, err := e.c.ImportTeamSkills(e.ctx, entity.ImportTeamSkillsRequest{TeamSkillsRequest: e.member(skUser), URL: "u"}); err != nil || len(rs.Imported) != 0 {
		t.Errorf("importing with storage failing on the second file: %+v %v", rs, err)
	}
	if n := e.blobs(t); n != before {
		t.Errorf("failed writes left %d blobs, want %d", n, before)
	}

	// A file whose blob is gone fails its read, and its gift.
	e.c.skillStorage = e.c.skillStorage.(*failingStorage).Service
	var f model.SkillFile
	e.db.Where("path = ?", skill.FileName).Joins("JOIN skills ON skills.id = skill_files.skill_id AND skills.name = ?", "deploy").First(&f)
	if err := e.c.skillStorage.Delete(f.StorageID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.c.GetWorkspaceTeamSkillFile(e.ctx, entity.WorkspaceTeamSkillRequest{WorkspaceID: skWS, UserID: skUserStr, Name: "deploy"}); err == nil {
		t.Error("a file with no blob was read")
	}
	f = model.SkillFile{}
	e.db.Where("skill_files.path = ?", skill.FileName).Joins("JOIN skills ON skills.id = skill_files.skill_id AND skills.name = ?", "lint").First(&f)
	_ = e.c.skillStorage.Delete(f.StorageID)
	if _, err := e.c.GiveSkillToTeam(e.ctx, entity.GiveSkillToTeamRequest{WorkspaceID: skWS, UserID: skUserStr, Name: "lint", TeamWorkspaceID: tkBackend}); err == nil || !strings.Contains(err.Error(), "copy skill file") {
		t.Errorf("giving a skill with a blob gone: %v", err)
	}
}

// failTeamSQL makes the statements of kind ("query" or "row") whose SQL
// holds text fail, after letting the first skip of them through.
func failTeamSQL(db *gorm.DB, kind, text string, skip int) {
	seen := 0
	fail := func(tx *gorm.DB) {
		if strings.Contains(tx.Statement.SQL.String(), text) {
			if seen++; seen > skip {
				_ = tx.AddError(errors.New("injected failure on " + text))
			}
		}
	}
	name := fmt.Sprintf("test:team-sql:%s:%s:%d", kind, text, time.Now().UnixNano())
	if kind == "row" {
		_ = db.Callback().Row().After("gorm:row").Register(name, fail)
		return
	}
	_ = db.Callback().Query().After("gorm:query").Register(name, fail)
}

// Each read and write fails with the statement under it, and a workspace
// that is not the caller's, or a name that is not one, is refused first.
func TestTeamSkills_Failures(t *testing.T) {
	type call func(e *teamSkillEnv) error
	alpha := func(name, path string) entity.WorkspaceTeamSkillRequest {
		return entity.WorkspaceTeamSkillRequest{WorkspaceID: skWS, UserID: skUserStr, Name: name, Path: path}
	}
	theirs := entity.WorkspaceTeamSkillRequest{WorkspaceID: skForgn, UserID: skUserStr, Name: "deploy"}
	list := func(e *teamSkillEnv) error {
		_, err := e.c.ListWorkspaceTeamSkills(e.ctx, entity.WorkspaceTeamSkillsRequest{WorkspaceID: skWS, UserID: skUserStr})
		return err
	}
	getSkill := func(req entity.WorkspaceTeamSkillRequest) call {
		return func(e *teamSkillEnv) error { _, err := e.c.GetWorkspaceTeamSkill(e.ctx, req); return err }
	}
	getFile := func(req entity.WorkspaceTeamSkillRequest) call {
		return func(e *teamSkillEnv) error { _, err := e.c.GetWorkspaceTeamSkillFile(e.ctx, req); return err }
	}
	set := func(e *teamSkillEnv) error {
		_, err := e.c.SetWorkspaceTeamSkill(e.ctx, entity.SetWorkspaceTeamSkillRequest{WorkspaceID: skWS, UserID: skUserStr, SkillID: e.deployID, On: true})
		return err
	}
	give := func(e *teamSkillEnv) error {
		_, err := e.c.GiveSkillToTeam(e.ctx, entity.GiveSkillToTeamRequest{WorkspaceID: skWS, UserID: skUserStr, Name: "lint", TeamWorkspaceID: tkBackend})
		return err
	}
	member := func(e *teamSkillEnv) entity.TeamSkillsRequest {
		r := e.member(skUser)
		r.SkillID = e.deployID
		return r
	}
	injected := func(err error) bool { return err != nil && strings.Contains(err.Error(), "injected") }
	notFound := func(err error) bool {
		return errors.Is(err, base.ErrNotFound) || (err != nil && strings.Contains(err.Error(), "not found"))
	}
	invalid := func(err error) bool { var se *SkillError; return errors.As(err, &se) && se.Kind == SkillInvalid }
	for _, c := range []struct {
		name   string
		fail   func(e *teamSkillEnv)
		run    call
		expect func(error) bool
	}{
		{"listing from someone else's workspace", nil, func(e *teamSkillEnv) error {
			_, err := e.c.ListWorkspaceTeamSkills(e.ctx, entity.WorkspaceTeamSkillsRequest{WorkspaceID: skForgn, UserID: skUserStr})
			return err
		}, notFound},
		{"listing the team skills", func(e *teamSkillEnv) { failTeamSQL(e.db, "row", "team_skill_choices", 0) }, list, injected},
		{"listing the account's own", func(e *teamSkillEnv) { failTeamSQL(e.db, "query", "workspace_skills", 0) }, list, injected},
		{"listing the team workspaces", func(e *teamSkillEnv) { failTeamSQL(e.db, "row", "team_name", 0) }, list, injected},
		{"reading through someone else's workspace", nil, getSkill(theirs), notFound},
		{"reading a bad name", nil, getSkill(alpha("Bad Name", "")), invalid},
		{"reading no such skill", nil, getSkill(alpha("nope", "")), notFound},
		{"listing a skill's files", func(e *teamSkillEnv) { failTeamSQL(e.db, "query", "skill_files", 0) }, getSkill(alpha("deploy", "")), injected},
		{"reading a file of no such skill", nil, getFile(alpha("nope", "")), notFound},
		{"reading a bad path", nil, getFile(alpha("deploy", "../x")), invalid},
		{"reading no such file", nil, getFile(alpha("deploy", "nope.md")), notFound},
		{"listing the files read with", func(e *teamSkillEnv) { failTeamSQL(e.db, "query", "skill_files", 1) }, getFile(alpha("deploy", "")), injected},
		{"saving through someone else's workspace", nil, func(e *teamSkillEnv) error {
			_, err := e.agentSave(skForgn, skUserStr, "deploy", "", "x", nil)
			return err
		}, notFound},
		{"saving a bad name", nil, func(e *teamSkillEnv) error {
			_, err := e.agentSave(skWS, skUserStr, "Bad Name", "", "x", nil)
			return err
		}, invalid},
		{"finding the skill to save", func(e *teamSkillEnv) { failTeamSQL(e.db, "query", "team_skill_choices", 0) }, func(e *teamSkillEnv) error {
			_, err := e.agentSave(skWS, skUserStr, "deploy", "a.md", "x", nil)
			return err
		}, injected},
		{"finding the file to save", func(e *teamSkillEnv) { failTeamSQL(e.db, "query", "skill_files", 0) }, func(e *teamSkillEnv) error {
			_, err := e.agentSave(skWS, skUserStr, "deploy", "a.md", "x", nil)
			return err
		}, injected},
		{"deleting a file of no such skill", nil, func(e *teamSkillEnv) error {
			_, err := e.c.DeleteWorkspaceTeamSkillFile(e.ctx, alpha("nope", "a.md"))
			return err
		}, notFound},
		{"setting a choice in someone else's workspace", nil, func(e *teamSkillEnv) error {
			_, err := e.c.SetWorkspaceTeamSkill(e.ctx, entity.SetWorkspaceTeamSkillRequest{WorkspaceID: skForgn, UserID: skUserStr, SkillID: e.deployID})
			return err
		}, notFound},
		{"reading the choice back", func(e *teamSkillEnv) { failTeamSQL(e.db, "row", "team_skill_choices", 0) }, set, injected},
		{"reading the account's own back", func(e *teamSkillEnv) { failTeamSQL(e.db, "query", "workspace_skills", 0) }, set, injected},
		{"a skill deleted as it is set", func(e *teamSkillEnv) {
			done := false
			_ = e.db.Callback().Row().Before("gorm:row").Register("test:team-race", func(tx *gorm.DB) {
				if !done && tx.Statement.Table == "skills" {
					done = true
					e.db.Exec("DELETE FROM skills WHERE id = ?", e.deployID)
				}
			})
		}, set, notFound},
		{"giving from someone else's workspace", nil, func(e *teamSkillEnv) error {
			_, err := e.c.GiveSkillToTeam(e.ctx, entity.GiveSkillToTeamRequest{WorkspaceID: skForgn, UserID: skUserStr, Name: "lint", TeamWorkspaceID: tkBackend})
			return err
		}, notFound},
		{"listing the files given", func(e *teamSkillEnv) { failTeamSQL(e.db, "query", "skill_files", 0) }, give, injected},
		{"reading the gift back", func(e *teamSkillEnv) { failTeamSQL(e.db, "query", "team_skill_choices", 0) }, give, injected},
		{"listing a team skill's files", func(e *teamSkillEnv) { failTeamSQL(e.db, "query", "skill_files", 0) }, func(e *teamSkillEnv) error {
			_, err := e.c.GetTeamSkill(e.ctx, member(e))
			return err
		}, injected},
		{"deleting a team skill", func(e *teamSkillEnv) { failOn(e.db, "delete", "skill_files") }, func(e *teamSkillEnv) error {
			return e.c.DeleteTeamSkill(e.ctx, member(e))
		}, injected},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newTeamSkillEnv(t)
			e.deployID = e.mustCreate(t, "deploy").Skill.ID
			e.save(t, skWS, "lint", skill.FileName, md("lint", "Lint."))
			if c.fail != nil {
				c.fail(e)
			}
			if err := c.run(e); !c.expect(err) {
				t.Errorf("got %v", err)
			}
		})
	}
}

// A change to an imported team skill marks it changed since.
func TestTeamSkills_AChangedImportIsMarked(t *testing.T) {
	e := newTeamSkillEnv(t)
	e.importer.res = githubResult(importedSkill("review", "Review it."))
	if _, err := e.c.ImportTeamSkills(e.ctx, entity.ImportTeamSkillsRequest{TeamSkillsRequest: e.member(skUser), URL: "u"}); err != nil {
		t.Fatal(err)
	}
	rs, err := e.agentSave(skWS, skUserStr, "review", "notes.md", "n", nil)
	if err != nil || !rs.Skill.LocallyModified || rs.Skill.SourceType != skillSourceGitHub {
		t.Errorf("a change to an import reads %+v (%v)", rs.Skill, err)
	}
}
