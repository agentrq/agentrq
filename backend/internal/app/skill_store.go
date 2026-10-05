// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	"github.com/agentrq/agentrq/backend/internal/controller/mcp"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/skill"
)

// skillStore gives a workspace's MCP server its skills, through the same
// controller the REST API uses, so the rules hold whichever way a skill is
// written. It is bound to one workspace and its owner.
type skillStore struct {
	crud        crud.Controller
	workspaceID int64
	userID      string
}

// refusal marks a controller refusal as one to hand the agent word for word;
// any other error is left as it is.
func refusal(err error) error {
	var se *crud.SkillError
	if errors.As(err, &se) {
		return &mcp.SkillRefusal{Message: se.Message}
	}
	return err
}

func (s *skillStore) SearchSkills(ctx context.Context, q string, limit, offset int) ([]mcp.SkillSummary, int, error) {
	ctx = entity.WithOrigin(ctx, entity.OriginMCP)
	rs, err := s.crud.SearchSkills(ctx, entity.SearchSkillsRequest{WorkspaceID: s.workspaceID, UserID: s.userID, Query: q, Limit: limit, Offset: offset, EnabledOnly: true})
	if err != nil {
		return nil, 0, refusal(err)
	}
	owners := map[int64]string{}
	out := make([]mcp.SkillSummary, len(rs.Skills))
	for i, sk := range rs.Skills {
		out[i] = mcp.SkillSummary{Name: sk.Name, Description: sk.Description}
		if sk.SharedFromWorkspaceID == 0 {
			continue
		}
		name, ok := owners[sk.SharedFromWorkspaceID]
		if !ok {
			name = "another workspace"
			if ws, err := s.crud.GetWorkspace(ctx, entity.GetWorkspaceRequest{ID: sk.SharedFromWorkspaceID, UserID: s.userID}); err == nil {
				name = fmt.Sprintf("workspace %q", ws.Workspace.Name)
			}
			owners[sk.SharedFromWorkspaceID] = name
		}
		out[i].SharedFrom = name
	}
	return out, rs.Total, nil
}

// LoadSkillFile reads one file, and for a SKILL.md the paths of its skill's
// files as well, which is what loadSkill lists after it. A skill turned off
// is not found, as searchSkills does not list it.
func (s *skillStore) LoadSkillFile(ctx context.Context, name, path string) (string, []string, bool, error) {
	ctx = entity.WithOrigin(ctx, entity.OriginMCP)
	file, err := s.crud.GetSkillFile(ctx, entity.GetSkillFileRequest{WorkspaceID: s.workspaceID, UserID: s.userID, Name: name, Path: path})
	if errors.Is(err, base.ErrNotFound) {
		return "", nil, false, nil
	}
	if err != nil {
		return "", nil, false, refusal(err)
	}
	if !file.Skill.Enabled {
		return "", nil, false, nil
	}
	if path != skill.FileName {
		return file.File.Content, nil, true, nil
	}
	rs, err := s.crud.GetSkill(ctx, entity.GetSkillRequest{WorkspaceID: s.workspaceID, UserID: s.userID, Name: name})
	if errors.Is(err, base.ErrNotFound) {
		return "", nil, false, nil
	}
	if err != nil {
		return "", nil, false, refusal(err)
	}
	files := make([]string, len(rs.Skill.Files))
	for i, f := range rs.Skill.Files {
		files[i] = f.Path
	}
	return file.File.Content, files, true, nil
}

// turnedOff refuses a change to a skill the human turned off: the agent
// cannot see it, so it should not be rewriting it either.
func (s *skillStore) turnedOff(ctx context.Context, name string) error {
	rs, err := s.crud.GetSkill(entity.WithOrigin(ctx, entity.OriginMCP), entity.GetSkillRequest{WorkspaceID: s.workspaceID, UserID: s.userID, Name: name})
	if err != nil || rs.Skill.Enabled {
		// A miss or a refusal is left to the call itself, which reports it.
		return nil
	}
	return &mcp.SkillRefusal{Message: fmt.Sprintf("skill %q is turned off in this workspace, so agents cannot use or change it; ask the human to turn it on in the Skills tab", rs.Skill.Name)}
}

func (s *skillStore) SaveSkillFile(ctx context.Context, name, path, content string) error {
	if err := s.turnedOff(ctx, name); err != nil {
		return err
	}
	_, err := s.crud.SaveSkillFile(ctx, entity.SaveSkillFileRequest{WorkspaceID: s.workspaceID, UserID: s.userID, Name: name, Path: path, Content: content})
	return refusal(err)
}

func (s *skillStore) DeleteSkill(ctx context.Context, name string) (bool, error) {
	if err := s.turnedOff(ctx, name); err != nil {
		return false, err
	}
	err := s.crud.DeleteSkill(ctx, entity.DeleteSkillRequest{WorkspaceID: s.workspaceID, UserID: s.userID, Name: name})
	return deleted(err)
}

func (s *skillStore) DeleteSkillFile(ctx context.Context, name, path string) (bool, error) {
	if err := s.turnedOff(ctx, name); err != nil {
		return false, err
	}
	_, err := s.crud.DeleteSkillFile(ctx, entity.DeleteSkillFileRequest{WorkspaceID: s.workspaceID, UserID: s.userID, Name: name, Path: path})
	return deleted(err)
}

func deleted(err error) (bool, error) {
	if errors.Is(err, base.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, refusal(err)
	}
	return true, nil
}
