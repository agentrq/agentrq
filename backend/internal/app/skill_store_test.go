// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

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
	return &entity.GetSkillFileResponse{Skill: entity.Skill{Enabled: !f.off, WorkspaceEnabled: !f.notHere}, File: entity.SkillFile{Content: "content of " + req.Path}}, f.fileErr
}

func (f *fakeSkillCrud) SaveSkillFile(_ context.Context, req entity.SaveSkillFileRequest) (*entity.SaveSkillFileResponse, error) {
	f.scope(req.WorkspaceID, req.UserID)
	return nil, f.err
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
	content, files, found, err := newSkillStore(f).LoadSkillFile(context.Background(), "tdd", "SKILL.md")
	if err != nil || !found || content != "content of SKILL.md" || !reflect.DeepEqual(files, []string{"SKILL.md", "a.md"}) {
		t.Errorf("got %q %v %v %v", content, files, found, err)
	}
	if !reflect.DeepEqual(f.scopes, []string{"owner@7", "owner@7"}) {
		t.Errorf("scopes: %v", f.scopes)
	}
}

// Only a SKILL.md comes with the list of files, so a sub file is one lookup.
func TestSkillStore_LoadSubFileIsOneLookup(t *testing.T) {
	f := &fakeSkillCrud{}
	content, files, found, err := newSkillStore(f).LoadSkillFile(context.Background(), "tdd", "a.md")
	if err != nil || !found || content != "content of a.md" || files != nil {
		t.Errorf("got %q %v %v %v", content, files, found, err)
	}
	if len(f.scopes) != 1 {
		t.Errorf("calls: %v", f.scopes)
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

	// A miss is not an error, from either lookup a load makes.
	for _, f := range []*fakeSkillCrud{{getErr: base.ErrNotFound}, {fileErr: base.ErrNotFound}} {
		if _, _, found, err := newSkillStore(f).LoadSkillFile(ctx, "tdd", "SKILL.md"); found || err != nil {
			t.Errorf("miss: found %v err %v", found, err)
		}
	}
	for _, f := range []*fakeSkillCrud{{getErr: refusal}, {fileErr: refusal}} {
		if _, _, _, err := newSkillStore(f).LoadSkillFile(ctx, "tdd", "SKILL.md"); !isRefusal(err) {
			t.Errorf("load refusal: %v", err)
		}
	}
	if _, _, err := newSkillStore(&fakeSkillCrud{listErr: refusal}).SearchSkills(ctx, "", 0, 0); !isRefusal(err) {
		t.Errorf("list refusal: %v", err)
	}
	if err := newSkillStore(&fakeSkillCrud{err: refusal}).SaveSkillFile(ctx, "tdd", "SKILL.md", "x"); !isRefusal(err) {
		t.Errorf("save refusal: %v", err)
	}
	if err := newSkillStore(&fakeSkillCrud{err: other}).SaveSkillFile(ctx, "tdd", "SKILL.md", "x"); err != other {
		t.Errorf("other errors pass unchanged: %v", err)
	}
	if err := newSkillStore(&fakeSkillCrud{}).SaveSkillFile(ctx, "tdd", "SKILL.md", "x"); err != nil {
		t.Errorf("save: %v", err)
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
			"skill": func() (bool, error) { return s.DeleteSkill(ctx, "tdd") },
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
	if _, _, _, err := s.LoadSkillFile(context.Background(), "tdd", "a.md"); err != nil {
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
				if _, _, found, err := newSkillStore(tc.fake()).LoadSkillFile(ctx, "tdd", p); found || err != nil {
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
			if got := refusedWith(s.SaveSkillFile(ctx, "tdd", "SKILL.md", "x")); got != tc.message {
				t.Errorf("saving was refused with %q, want %q", got, tc.message)
			}
			deleted, err := s.DeleteSkill(ctx, "tdd")
			if got := refusedWith(err); deleted || got != tc.message {
				t.Errorf("deleting the skill returned deleted %v and %q, want not deleted and %q", deleted, got, tc.message)
			}
			deleted, err = s.DeleteSkillFile(ctx, "tdd", "a.md")
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
	if err := newSkillStore(&fakeSkillCrud{getErr: base.ErrNotFound}).SaveSkillFile(ctx, "new", "SKILL.md", "x"); err != nil {
		t.Errorf("saving a new skill failed with %v, want it created", err)
	}
}

// Deleting a skill from an agent takes it out of the agent's own workspace:
// the controller is asked with that workspace, and deletes the skill itself
// only when no other workspace has it on.
func TestSkillStore_DeleteGoesThroughTheWorkspace(t *testing.T) {
	f := &fakeSkillCrud{}
	deleted, err := newSkillStore(f).DeleteSkill(context.Background(), "tdd")
	if !deleted || err != nil {
		t.Fatalf("deleting returned deleted %v and error %v, want deleted and no error", deleted, err)
	}
	want := []entity.DeleteSkillRequest{{WorkspaceID: 7, UserID: "owner", Name: "tdd"}}
	if !reflect.DeepEqual(f.deleted, want) {
		t.Errorf("the controller was asked to delete %+v, want %+v", f.deleted, want)
	}
}

type legacyMover struct {
	err   error
	calls int
}

func (m *legacyMover) MoveLegacySkills(context.Context) error {
	m.calls++
	return m.err
}

// The move runs once at start, and a failure does not stop the server.
func TestMoveLegacySkills_LogsAFailureAndCarriesOn(t *testing.T) {
	for _, err := range []error{nil, errors.New("database unavailable")} {
		m := &legacyMover{err: err}
		moveLegacySkills(context.Background(), m)
		if m.calls != 1 {
			t.Errorf("with %v the move ran %d times, want once", err, m.calls)
		}
	}
}
