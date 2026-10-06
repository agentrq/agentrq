// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package coremcp

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
)

// A hand-written fake rather than a generated mock: one CI job runs this
// package's tests without generating mocks.
type mockSkillCrud struct {
	crud.Controller

	list    entity.SearchSkillsRequest
	getFile entity.GetSkillFileRequest
	get     entity.GetSkillRequest

	listErr, fileErr, getErr error
	origins                  []entity.Origin
}

func (m *mockSkillCrud) SearchSkills(ctx context.Context, req entity.SearchSkillsRequest) (*entity.SearchSkillsResponse, error) {
	m.origins = append(m.origins, entity.GetOrigin(ctx))
	m.list = req
	return &entity.SearchSkillsResponse{Skills: []entity.Skill{{Name: "tdd", Description: "Test first.", Enabled: true, WorkspaceIDs: []int64{testWorkspace, 301}, WorkspaceEnabled: true}}, Total: 1}, m.listErr
}

func (m *mockSkillCrud) GetSkillFile(ctx context.Context, req entity.GetSkillFileRequest) (*entity.GetSkillFileResponse, error) {
	m.origins = append(m.origins, entity.GetOrigin(ctx))
	m.getFile = req
	return &entity.GetSkillFileResponse{Skill: entity.Skill{Name: req.Name}, File: entity.SkillFile{Path: req.Path, Content: "body of " + req.Path}}, m.fileErr
}

func (m *mockSkillCrud) GetSkill(_ context.Context, req entity.GetSkillRequest) (*entity.GetSkillResponse, error) {
	m.get = req
	return &entity.GetSkillResponse{Skill: entity.Skill{Name: req.Name, Files: []entity.SkillFile{{Path: "SKILL.md"}, {Path: "refs/a.md"}}}}, m.getErr
}

func TestSearchSkills_ScopesToTheAuthenticatedUserAndWorkspace(t *testing.T) {
	ctrl := &mockSkillCrud{}
	s := &WorkspaceServer{crud: ctrl}
	body := textOf(t, toolResult(s.handleSearchSkills(authedContext(), nil, SearchSkillsParams{WorkspaceID: base62(testWorkspace), Q: "test", Limit: 5, Offset: 10})))

	if ctrl.list.UserID != testUserID || ctrl.list.WorkspaceID != testWorkspace || ctrl.list.Query != "test" || ctrl.list.Limit != 5 || ctrl.list.Offset != 10 {
		t.Errorf("the controller was asked %+v, want user %s, workspace %d, query test, limit 5 and offset 10", ctrl.list, testUserID, testWorkspace)
	}
	// Both switches reach the supervisor, so it can tell a skill turned off
	// for the account from one not on in this workspace.
	for _, want := range []string{`"total":1`, `"name":"tdd"`, `"description":"Test first."`, `"enabled":true`,
		`"workspaceIds":["` + base62(testWorkspace) + `","` + base62(301) + `"]`, `"workspaceEnabled":true`} {
		if !strings.Contains(body, want) {
			t.Errorf("the body %s does not contain %s", body, want)
		}
	}
}

func TestGetSkill(t *testing.T) {
	for _, tc := range []struct {
		uri, path string
		files     bool
	}{
		{"skill://TDD", "SKILL.md", true},
		{"skill://tdd/SKILL.md", "SKILL.md", true},
		{"skill://tdd/refs/a.md", "refs/a.md", false},
	} {
		ctrl := &mockSkillCrud{}
		s := &WorkspaceServer{crud: ctrl}
		body := textOf(t, toolResult(s.handleGetSkill(authedContext(), nil, GetSkillParams{WorkspaceID: base62(testWorkspace), URI: tc.uri})))

		want := entity.GetSkillFileRequest{UserID: testUserID, WorkspaceID: testWorkspace, Name: "tdd", Path: tc.path}
		if ctrl.getFile != want {
			t.Errorf("%s: request = %+v, want %+v", tc.uri, ctrl.getFile, want)
		}
		if !strings.Contains(body, `"content":"body of `+tc.path+`"`) {
			t.Errorf("%s: body %s", tc.uri, body)
		}
		// A SKILL.md comes with the skill's list of files; a sub file does not.
		if got := strings.Contains(body, `"files":[{"path":"SKILL.md"`); got != tc.files {
			t.Errorf("%s: lists files = %v, want %v: %s", tc.uri, got, tc.files, body)
		}
	}
}

func TestSkillTools_ReportFailures(t *testing.T) {
	refusal := &crud.SkillError{Kind: crud.SkillInvalid, Message: "not a valid skill name"}
	for _, tc := range []struct {
		name string
		ctrl *mockSkillCrud
		run  func(s *WorkspaceServer) callResult
		want string
	}{
		{"list", &mockSkillCrud{listErr: errors.New("database unavailable")}, func(s *WorkspaceServer) callResult {
			return toolResult(s.handleSearchSkills(authedContext(), nil, SearchSkillsParams{WorkspaceID: base62(testWorkspace), Q: "test", Limit: 5, Offset: 10}))
		}, "database unavailable"},
		{"bad uri", &mockSkillCrud{}, func(s *WorkspaceServer) callResult {
			return toolResult(s.handleGetSkill(authedContext(), nil, GetSkillParams{WorkspaceID: base62(testWorkspace), URI: "memory://x.md"}))
		}, "not a skill URI"},
		{"file", &mockSkillCrud{fileErr: refusal}, func(s *WorkspaceServer) callResult {
			return toolResult(s.handleGetSkill(authedContext(), nil, GetSkillParams{WorkspaceID: base62(testWorkspace), URI: "skill://tdd"}))
		}, "not a valid skill name"},
		{"file list", &mockSkillCrud{getErr: errors.New("database unavailable")}, func(s *WorkspaceServer) callResult {
			return toolResult(s.handleGetSkill(authedContext(), nil, GetSkillParams{WorkspaceID: base62(testWorkspace), URI: "skill://tdd"}))
		}, "database unavailable"},
	} {
		res := tc.run(&WorkspaceServer{crud: tc.ctrl})
		if !res.isError || !strings.Contains(res.text, tc.want) {
			t.Errorf("%s: got %+v, want an error containing %q", tc.name, res, tc.want)
		}
	}
}

// A supervisor's skill reads are counted as its tool calls, so the controller
// must be told they came over MCP or it counts them again as the interface's.
func TestSkillReadsAreMCPOrigin(t *testing.T) {
	ctrl := &mockSkillCrud{}
	s := &WorkspaceServer{crud: ctrl}
	toolResult(s.handleSearchSkills(authedContext(), nil, SearchSkillsParams{WorkspaceID: base62(testWorkspace)}))
	toolResult(s.handleGetSkill(authedContext(), nil, GetSkillParams{WorkspaceID: base62(testWorkspace), URI: "skill://tdd"}))
	if len(ctrl.origins) != 2 || ctrl.origins[0] != entity.OriginMCP || ctrl.origins[1] != entity.OriginMCP {
		t.Errorf("origins = %v, want both MCP", ctrl.origins)
	}
}
