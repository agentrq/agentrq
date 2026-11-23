// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package app

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	"github.com/agentrq/agentrq/backend/internal/controller/mcp"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
)

// fakeSkillCrud answers the skill calls the store makes, and records which
// workspace and user each one named.
type fakeSkillCrud struct {
	crud.Controller

	skills                        []entity.Skill
	listErr, getErr, fileErr, err error
	scopes                        []string
	search                        entity.SearchSkillsRequest
	deleted                       []entity.DeleteSkillRequest
	origins                       []entity.Origin
	// off is a skill the human turned off for the account, and notHere one
	// of the account's that is not on in this workspace.
	off, notHere bool
	saved        []entity.SaveSkillFileRequest

	// team is the team skill of every name, nil for none; teamWorkspaces
	// the team workspaces the workspace is shared into.
	team               *entity.TeamSkill
	teamWorkspaces     []entity.TeamWorkspaceRef
	teamSkills         []entity.TeamSkill
	teamErr, teamOpErr error
	teamListErr        error
	teamSaved          []entity.SaveWorkspaceTeamSkillFileRequest
	teamCalls          []string
	gone               bool
}

func (f *fakeSkillCrud) ListWorkspaceTeamSkills(_ context.Context, req entity.WorkspaceTeamSkillsRequest) (*entity.WorkspaceTeamSkillsResponse, error) {
	f.teamCalls = append(f.teamCalls, fmt.Sprintf("list %s %v", req.Query, req.OnlyOn))
	return &entity.WorkspaceTeamSkillsResponse{Skills: f.teamSkills, TeamWorkspaces: f.teamWorkspaces}, f.teamListErr
}

func (f *fakeSkillCrud) GetWorkspaceTeamSkill(_ context.Context, req entity.WorkspaceTeamSkillRequest) (*entity.TeamSkill, error) {
	f.teamCalls = append(f.teamCalls, "get "+req.Name)
	if f.teamErr != nil {
		return nil, f.teamErr
	}
	if f.team == nil {
		return nil, base.ErrNotFound
	}
	return f.team, nil
}

func (f *fakeSkillCrud) GetWorkspaceTeamSkillFile(_ context.Context, req entity.WorkspaceTeamSkillRequest) (*entity.TeamSkillFileResponse, error) {
	f.teamCalls = append(f.teamCalls, "file "+req.Name+"/"+req.Path)
	if f.teamOpErr != nil {
		return nil, f.teamOpErr
	}
	return &entity.TeamSkillFileResponse{Skill: *f.team, File: entity.SkillFile{Content: "team " + req.Path, Version: 5}}, nil
}

func (f *fakeSkillCrud) SaveWorkspaceTeamSkillFile(_ context.Context, req entity.SaveWorkspaceTeamSkillFileRequest) (*entity.TeamSkillFileResponse, error) {
	f.teamSaved = append(f.teamSaved, req)
	if f.teamOpErr != nil {
		return nil, f.teamOpErr
	}
	return &entity.TeamSkillFileResponse{File: entity.SkillFile{Version: 6}}, nil
}

func (f *fakeSkillCrud) DeleteWorkspaceTeamSkill(_ context.Context, req entity.WorkspaceTeamSkillRequest) (*entity.OptOutTeamSkillResponse, error) {
	f.teamCalls = append(f.teamCalls, "optOut "+req.Name)
	if f.teamOpErr != nil {
		return nil, f.teamOpErr
	}
	return &entity.OptOutTeamSkillResponse{Deleted: f.gone}, nil
}

func (f *fakeSkillCrud) DeleteWorkspaceTeamSkillFile(_ context.Context, req entity.WorkspaceTeamSkillRequest) (*entity.TeamSkill, error) {
	f.teamCalls = append(f.teamCalls, "deleteFile "+req.Name+"/"+req.Path)
	return f.team, f.teamOpErr
}

func (f *fakeSkillCrud) scope(workspaceID int64, userID string) {
	f.scopes = append(f.scopes, userID+"@"+string(rune('0'+workspaceID)))
}

func (f *fakeSkillCrud) SearchSkills(ctx context.Context, req entity.SearchSkillsRequest) (*entity.SearchSkillsResponse, error) {
	f.origins = append(f.origins, entity.GetOrigin(ctx))
	f.scope(req.WorkspaceID, req.UserID)
	f.search = req
	return &entity.SearchSkillsResponse{Skills: f.skills, Total: len(f.skills) + 10}, f.listErr
}

func (f *fakeSkillCrud) GetSkill(_ context.Context, req entity.GetSkillRequest) (*entity.GetSkillResponse, error) {
	f.scope(req.WorkspaceID, req.UserID)
	return &entity.GetSkillResponse{Skill: entity.Skill{Name: "tdd", Enabled: !f.off, WorkspaceEnabled: !f.notHere, Files: []entity.SkillFile{{Path: "SKILL.md"}, {Path: "a.md"}}}}, f.getErr
}

func (f *fakeSkillCrud) GetSkillFile(ctx context.Context, req entity.GetSkillFileRequest) (*entity.GetSkillFileResponse, error) {
	f.origins = append(f.origins, entity.GetOrigin(ctx))
	f.scope(req.WorkspaceID, req.UserID)
	return &entity.GetSkillFileResponse{Skill: entity.Skill{Enabled: !f.off, WorkspaceEnabled: !f.notHere}, File: entity.SkillFile{Content: "content of " + req.Path, Version: 4}}, f.fileErr
}

func (f *fakeSkillCrud) SaveSkillFile(_ context.Context, req entity.SaveSkillFileRequest) (*entity.SaveSkillFileResponse, error) {
	f.scope(req.WorkspaceID, req.UserID)
	f.saved = append(f.saved, req)
	if f.err != nil {
		return nil, f.err
	}
	return &entity.SaveSkillFileResponse{File: entity.SkillFile{Version: 9}}, nil
}

func (f *fakeSkillCrud) DeleteSkill(_ context.Context, req entity.DeleteSkillRequest) error {
	f.scope(req.WorkspaceID, req.UserID)
	f.deleted = append(f.deleted, req)
	return f.err
}

func (f *fakeSkillCrud) DeleteSkillFile(_ context.Context, req entity.DeleteSkillFileRequest) (*entity.DeleteSkillFileResponse, error) {
	f.scope(req.WorkspaceID, req.UserID)
	return nil, f.err
}

func newSkillStore(f *fakeSkillCrud) *skillStore {
	return &skillStore{crud: f, workspaceID: 7, userID: "owner"}
}

func TestSkillStore_List(t *testing.T) {
	f := &fakeSkillCrud{
		skills: []entity.Skill{
			{Name: "tdd", Description: "Test first.", Enabled: true, WorkspaceIDs: []int64{7, 8}, WorkspaceEnabled: true},
			{Name: "review", Description: "Review it.", Enabled: true, WorkspaceIDs: []int64{7}, WorkspaceEnabled: true},
		},
	}
	got, total, err := newSkillStore(f).SearchSkills(context.Background(), "tdd", 5, 2)
	if err != nil {
		t.Fatalf("searching skills failed: %v", err)
	}
	want := []mcp.SkillSummary{
		{Name: "tdd", Description: "Test first."},
		{Name: "review", Description: "Review it."},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the search returned %+v, want %+v", got, want)
	}
	// The controller keeps only what this workspace's agent sees: EnabledOnly
	// asks for both switches on.
	if total != 12 || f.search.Query != "tdd" || f.search.Limit != 5 || f.search.Offset != 2 || !f.search.EnabledOnly {
		t.Errorf("the search reported %d in all and asked %+v, want 12 and query tdd, limit 5, offset 2, enabled only", total, f.search)
	}
	if !reflect.DeepEqual(f.scopes, []string{"owner@7"}) {
		t.Errorf("the controller was asked as %v, want owner@7 once", f.scopes)
	}
}

func TestSkillStore_Load(t *testing.T) {
	f := &fakeSkillCrud{}
	file, found, err := newSkillStore(f).LoadSkillFile(context.Background(), "tdd", "SKILL.md")
	if err != nil || !found || file.Content != "content of SKILL.md" || !reflect.DeepEqual(file.Files, []string{"SKILL.md", "a.md"}) || file.Version != 0 {
		t.Errorf("got %+v %v %v, want no version for a skill nobody else writes", file, found, err)
	}
	if !reflect.DeepEqual(f.scopes, []string{"owner@7", "owner@7"}) {
		t.Errorf("scopes: %v", f.scopes)
	}
}

// Only a SKILL.md comes with the list of files, so a sub file is one lookup.
func TestSkillStore_LoadSubFileIsOneLookup(t *testing.T) {
	f := &fakeSkillCrud{}
	file, found, err := newSkillStore(f).LoadSkillFile(context.Background(), "tdd", "a.md")
	if err != nil || !found || file.Content != "content of a.md" || file.Files != nil {
		t.Errorf("got %+v %v %v", file, found, err)
	}
	if len(f.scopes) != 1 {
		t.Errorf("calls: %v", f.scopes)
	}
}

// A file of the account's own skill, which nobody else writes, comes with
// no version: only a team skill's does.
func TestSkillStore_LoadOwnHasNoVersion(t *testing.T) {
	for _, p := range []string{"SKILL.md", "a.md"} {
		file, found, err := newSkillStore(&fakeSkillCrud{}).LoadSkillFile(context.Background(), "tdd", p)
		if err != nil || !found || file.Version != 0 {
			t.Errorf("loading %s of an own skill gave version %d (found %v, %v), want none", p, file.Version, found, err)
		}
	}
}

// A save names the version the agent read only when it gave one, and
// reports the version the file is at after it.
func TestSkillStore_SaveVersion(t *testing.T) {
	ctx := context.Background()
	f := &fakeSkillCrud{}
	s := newSkillStore(f)
	if v, err := s.SaveSkillFile(ctx, "tdd", "a.md", "x", 0, mcp.SkillTarget{}); err != nil || v != (mcp.SkillSaved{Version: 9}) {
		t.Errorf("a save with no version returned %+v, %v; want 9", v, err)
	}
	if v, err := s.SaveSkillFile(ctx, "tdd", "a.md", "x", 3, mcp.SkillTarget{}); err != nil || v != (mcp.SkillSaved{Version: 9}) {
		t.Errorf("a save on version 3 returned %+v, %v; want 9", v, err)
	}
	if len(f.saved) != 2 || f.saved[0].Version != nil || f.saved[1].Version == nil || *f.saved[1].Version != 3 {
		t.Errorf("the controller was asked to save %+v, want no version, then version 3", f.saved)
	}
}

func TestSkillStore_Errors(t *testing.T) {
	refusal := &crud.SkillError{Kind: crud.SkillInvalid, Message: "not a valid skill name"}
	other := errors.New("database unavailable")
	ctx := context.Background()

	isRefusal := func(err error) bool {
		var r *mcp.SkillRefusal
		return errors.As(err, &r) && r.Message == "not a valid skill name"
	}

	// A miss is not an error, from any lookup a load makes.
	for _, f := range []*fakeSkillCrud{{getErr: base.ErrNotFound}, {fileErr: base.ErrNotFound}, {fileErr: base.ErrNotFound, getErr: base.ErrNotFound}} {
		if _, found, err := newSkillStore(f).LoadSkillFile(ctx, "tdd", "SKILL.md"); found || err != nil {
			t.Errorf("miss: found %v err %v", found, err)
		}
	}
	for _, f := range []*fakeSkillCrud{{getErr: refusal}, {fileErr: refusal}, {fileErr: base.ErrNotFound, getErr: refusal}, {fileErr: base.ErrNotFound, getErr: base.ErrNotFound, teamErr: refusal}} {
		if _, _, err := newSkillStore(f).LoadSkillFile(ctx, "tdd", "SKILL.md"); !isRefusal(err) {
			t.Errorf("load refusal: %v", err)
		}
	}
	if _, _, err := newSkillStore(&fakeSkillCrud{listErr: refusal}).SearchSkills(ctx, "", 0, 0); !isRefusal(err) {
		t.Errorf("list refusal: %v", err)
	}
	if _, _, err := newSkillStore(&fakeSkillCrud{teamListErr: refusal}).SearchSkills(ctx, "", 0, 0); !isRefusal(err) {
		t.Errorf("team list refusal: %v", err)
	}
	if _, err := newSkillStore(&fakeSkillCrud{err: refusal}).SaveSkillFile(ctx, "tdd", "SKILL.md", "x", 0, mcp.SkillTarget{}); !isRefusal(err) {
		t.Errorf("save refusal: %v", err)
	}
	if _, err := newSkillStore(&fakeSkillCrud{err: other}).SaveSkillFile(ctx, "tdd", "SKILL.md", "x", 0, mcp.SkillTarget{}); err != other {
		t.Errorf("other errors pass unchanged: %v", err)
	}
	if _, err := newSkillStore(&fakeSkillCrud{}).SaveSkillFile(ctx, "tdd", "SKILL.md", "x", 0, mcp.SkillTarget{}); err != nil {
		t.Errorf("save: %v", err)
	}
	for _, op := range []func(s *skillStore) error{
		func(s *skillStore) error {
			_, err := s.SaveSkillFile(ctx, "tdd", "SKILL.md", "x", 0, mcp.SkillTarget{})
			return err
		},
		func(s *skillStore) error { _, err := s.DeleteSkill(ctx, "tdd"); return err },
		func(s *skillStore) error { _, err := s.DeleteSkillFile(ctx, "tdd", "a.md"); return err },
	} {
		if err := op(newSkillStore(&fakeSkillCrud{getErr: refusal})); !isRefusal(err) {
			t.Errorf("a write after a refused lookup: %v", err)
		}
	}

	for _, tc := range []struct {
		err     error
		deleted bool
		refused bool
	}{
		{nil, true, false},
		{base.ErrNotFound, false, false},
		{refusal, false, true},
	} {
		s := newSkillStore(&fakeSkillCrud{err: tc.err})
		for name, run := range map[string]func() (bool, error){
			"skill": func() (bool, error) { d, err := s.DeleteSkill(ctx, "tdd"); return d.Deleted, err },
			"file":  func() (bool, error) { return s.DeleteSkillFile(ctx, "tdd", "a.md") },
		} {
			deleted, err := run()
			if deleted != tc.deleted || isRefusal(err) != tc.refused || (!tc.refused && err != nil) {
				t.Errorf("delete %s with %v: deleted %v err %v", name, tc.err, deleted, err)
			}
		}
	}
}

// An agent's skill reads are counted as its tool calls, so the controller must
// be told they came over MCP or it counts them a second time as the interface's.
func TestSkillStore_ReadsAreMCPOrigin(t *testing.T) {
	f := &fakeSkillCrud{}
	s := newSkillStore(f)
	if _, _, err := s.SearchSkills(context.Background(), "", 0, 0); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.LoadSkillFile(context.Background(), "tdd", "a.md"); err != nil {
		t.Fatal(err)
	}
	if want := []entity.Origin{entity.OriginMCP, entity.OriginMCP}; !reflect.DeepEqual(f.origins, want) {
		t.Errorf("origins = %v, want %v", f.origins, want)
	}
}

func TestVisible(t *testing.T) {
	for _, tc := range []struct {
		name                      string
		enabled, workspaceEnabled bool
		want                      bool
	}{
		{"a skill on for the account and on in the workspace is seen", true, true, true},
		{"a skill turned off for the account is not seen", false, true, false},
		{"a skill not on in the workspace is not seen", true, false, false},
		{"a skill off in both is not seen", false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := visible(entity.Skill{Enabled: tc.enabled, WorkspaceEnabled: tc.workspaceEnabled}); got != tc.want {
				t.Errorf("visible returned %v, want %v", got, tc.want)
			}
		})
	}
}

// A skill the agent does not see is invisible to it, whichever switch hides
// it: a load does not find it, and a write to it is refused, with a message
// that says which switch to ask the human for, rather than made blind.
func TestSkillStore_Hidden(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name    string
		fake    func() *fakeSkillCrud
		message string
	}{
		{"a skill turned off for the account", func() *fakeSkillCrud { return &fakeSkillCrud{off: true} },
			`skill "tdd" is turned off for every workspace, so agents cannot use or change it; ask the human to turn it on in Skills`},
		{"a skill turned off for the account and not on here", func() *fakeSkillCrud { return &fakeSkillCrud{off: true, notHere: true} },
			`skill "tdd" is turned off for every workspace, so agents cannot use or change it; ask the human to turn it on in Skills`},
		{"a skill of the account not on in this workspace", func() *fakeSkillCrud { return &fakeSkillCrud{notHere: true} },
			`skill "tdd" belongs to the account but is not on in this workspace, so this agent cannot use or change it; ask the human to turn it on in this workspace's Skills tab, or choose another name`},
	} {
		t.Run(tc.name+" is not found by a load", func(t *testing.T) {
			for _, p := range []string{"SKILL.md", "a.md"} {
				if _, found, err := newSkillStore(tc.fake()).LoadSkillFile(ctx, "tdd", p); found || err != nil {
					t.Errorf("loading %s returned found %v and error %v, want not found and no error", p, found, err)
				}
			}
		})
		t.Run(tc.name+" refuses every write", func(t *testing.T) {
			refusedWith := func(err error) string {
				var r *mcp.SkillRefusal
				if !errors.As(err, &r) {
					return fmt.Sprintf("no refusal but %v", err)
				}
				return r.Message
			}
			f := tc.fake()
			s := newSkillStore(f)
			_, err := s.SaveSkillFile(ctx, "tdd", "SKILL.md", "x", 0, mcp.SkillTarget{})
			if got := refusedWith(err); got != tc.message {
				t.Errorf("saving was refused with %q, want %q", got, tc.message)
			}
			d, err := s.DeleteSkill(ctx, "tdd")
			if got := refusedWith(err); d.Deleted || got != tc.message {
				t.Errorf("deleting the skill returned deleted %v and %q, want not deleted and %q", d.Deleted, got, tc.message)
			}
			deleted, err := s.DeleteSkillFile(ctx, "tdd", "a.md")
			if got := refusedWith(err); deleted || got != tc.message {
				t.Errorf("deleting a file returned deleted %v and %q, want not deleted and %q", deleted, got, tc.message)
			}
			// Only the lookups ran; nothing was written.
			if len(f.scopes) != 3 || len(f.deleted) != 0 {
				t.Errorf("the controller was called as %v and asked to delete %+v, want three lookups and no delete", f.scopes, f.deleted)
			}
		})
	}

	// A skill that is not there yet is the write's own business: saving its
	// SKILL.md creates it.
	if _, err := newSkillStore(&fakeSkillCrud{getErr: base.ErrNotFound}).SaveSkillFile(ctx, "new", "SKILL.md", "x", 0, mcp.SkillTarget{}); err != nil {
		t.Errorf("saving a new skill failed with %v, want it created", err)
	}
}

// Deleting a skill from an agent takes it out of the agent's own workspace:
// the controller is asked with that workspace, and deletes the skill itself
// only when no other workspace has it on.
func TestSkillStore_DeleteGoesThroughTheWorkspace(t *testing.T) {
	f := &fakeSkillCrud{}
	d, err := newSkillStore(f).DeleteSkill(context.Background(), "tdd")
	if d != (mcp.SkillDeleted{Deleted: true}) || err != nil {
		t.Fatalf("deleting returned %+v and error %v, want deleted and no error", d, err)
	}
	want := []entity.DeleteSkillRequest{{WorkspaceID: 7, UserID: "owner", Name: "tdd"}}
	if !reflect.DeepEqual(f.deleted, want) {
		t.Errorf("the controller was asked to delete %+v, want %+v", f.deleted, want)
	}
}

type legacyMover struct {
	err      error
	calls    int
	converts int
}

func (m *legacyMover) MoveLegacySkills(context.Context) error {
	m.calls++
	return m.err
}

func (m *legacyMover) ConvertSkillLinks(context.Context) error {
	m.converts++
	return m.err
}

// The move and the conversion of links run once at start, and a failure
// of either does not stop the server.
func TestMoveLegacySkills_LogsAFailureAndCarriesOn(t *testing.T) {
	for _, err := range []error{nil, errors.New("database unavailable")} {
		m := &legacyMover{err: err}
		moveLegacySkills(context.Background(), m)
		if m.calls != 1 || m.converts != 1 {
			t.Errorf("with %v the move ran %d times and the conversion %d, want once each", err, m.calls, m.converts)
		}
	}
}
