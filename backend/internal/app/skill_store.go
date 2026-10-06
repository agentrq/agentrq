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
	zlog "github.com/rs/zerolog/log"
)

// skillStore gives a workspace's MCP server its skills, through the same
// controller the REST API uses, so the rules hold whichever way a skill is
// written. It is bound to one workspace and its owner, and shows the agent
// only the skills on for the account and on in that workspace.
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
	out := make([]mcp.SkillSummary, len(rs.Skills))
	for i, sk := range rs.Skills {
		out[i] = mcp.SkillSummary{Name: sk.Name, Description: sk.Description}
	}
	return out, rs.Total, nil
}

// visible reports whether the agent sees a skill: on for the account and on
// in this workspace.
func visible(sk entity.Skill) bool {
	return sk.Enabled && sk.WorkspaceEnabled
}

// LoadSkillFile reads one file, and for a SKILL.md the paths of its skill's
// files as well, which is what loadSkill lists after it. A skill the agent
// does not see is not found, as searchSkills does not list it.
func (s *skillStore) LoadSkillFile(ctx context.Context, name, path string) (string, []string, bool, error) {
	ctx = entity.WithOrigin(ctx, entity.OriginMCP)
	file, err := s.crud.GetSkillFile(ctx, entity.GetSkillFileRequest{WorkspaceID: s.workspaceID, UserID: s.userID, Name: name, Path: path})
	if errors.Is(err, base.ErrNotFound) {
		return "", nil, false, nil
	}
	if err != nil {
		return "", nil, false, refusal(err)
	}
	if !visible(file.Skill) {
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

// hidden refuses a change to a skill of the account the agent does not see:
// one turned off, or one not on in this workspace. A skill that does not
// exist is left to the call, which creates it or reports the miss.
func (s *skillStore) hidden(ctx context.Context, name string) error {
	rs, err := s.crud.GetSkill(entity.WithOrigin(ctx, entity.OriginMCP), entity.GetSkillRequest{WorkspaceID: s.workspaceID, UserID: s.userID, Name: name})
	if err != nil || visible(rs.Skill) {
		// A miss or a refusal is left to the call itself, which reports it.
		return nil
	}
	if !rs.Skill.Enabled {
		return &mcp.SkillRefusal{Message: fmt.Sprintf("skill %q is turned off for every workspace, so agents cannot use or change it; ask the human to turn it on in Skills", rs.Skill.Name)}
	}
	return &mcp.SkillRefusal{Message: fmt.Sprintf("skill %q belongs to the account but is not on in this workspace, so this agent cannot use or change it; ask the human to turn it on in this workspace's Skills tab, or choose another name", rs.Skill.Name)}
}

func (s *skillStore) SaveSkillFile(ctx context.Context, name, path, content string) error {
	if err := s.hidden(ctx, name); err != nil {
		return err
	}
	_, err := s.crud.SaveSkillFile(ctx, entity.SaveSkillFileRequest{WorkspaceID: s.workspaceID, UserID: s.userID, Name: name, Path: path, Content: content})
	return refusal(err)
}

// DeleteSkill takes the skill out of this workspace; the controller deletes
// it only when no other workspace has it on.
func (s *skillStore) DeleteSkill(ctx context.Context, name string) (bool, error) {
	if err := s.hidden(ctx, name); err != nil {
		return false, err
	}
	err := s.crud.DeleteSkill(ctx, entity.DeleteSkillRequest{WorkspaceID: s.workspaceID, UserID: s.userID, Name: name})
	return deleted(err)
}

func (s *skillStore) DeleteSkillFile(ctx context.Context, name, path string) (bool, error) {
	if err := s.hidden(ctx, name); err != nil {
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

// moveLegacySkills runs before anything serves a skill: skills from before
// they belonged to the account move to it here, and are found already moved
// at every later start. A failure is logged rather than stopping the server,
// which serves every other skill; the next start tries again.
func moveLegacySkills(ctx context.Context, skills interface {
	MoveLegacySkills(context.Context) error
}) {
	if err := skills.MoveLegacySkills(ctx); err != nil {
		zlog.Error().Err(err).Msg("skills: could not move workspace skills to their accounts; the next start tries again")
	}
}
