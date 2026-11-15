// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

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

// teamSkills is the team skills this workspace reaches; with onlyOn, those
// it is opted into, which are the ones its agent sees.
func (s *skillStore) teamSkills(ctx context.Context, q string, onlyOn bool) (*entity.WorkspaceTeamSkillsResponse, error) {
	return s.crud.ListWorkspaceTeamSkills(ctx, entity.WorkspaceTeamSkillsRequest{WorkspaceID: s.workspaceID, UserID: s.userID, Query: q, OnlyOn: onlyOn})
}

// SearchSkills finds the account's skills on here and the team skills this
// workspace is opted into, by name: of two with one name, the account's
// wins, as it does on every other call.
func (s *skillStore) SearchSkills(ctx context.Context, q string, limit, offset int) ([]mcp.SkillSummary, int, error) {
	ctx = entity.WithOrigin(ctx, entity.OriginMCP)
	rs, err := s.crud.SearchSkills(ctx, entity.SearchSkillsRequest{WorkspaceID: s.workspaceID, UserID: s.userID, Query: q, Offset: offset, Limit: limit, EnabledOnly: true})
	if err != nil {
		return nil, 0, refusal(err)
	}
	team, err := s.teamSkills(ctx, strings.TrimSpace(q), true)
	if err != nil {
		return nil, 0, refusal(err)
	}
	if len(team.Skills) == 0 {
		out := make([]mcp.SkillSummary, len(rs.Skills))
		for i, sk := range rs.Skills {
			out[i] = mcp.SkillSummary{Name: sk.Name, Description: sk.Description}
		}
		return out, rs.Total, nil
	}
	// Merging needs every match of both, to page through them by name.
	if rs, err = s.crud.SearchSkills(ctx, entity.SearchSkillsRequest{WorkspaceID: s.workspaceID, UserID: s.userID, Query: q, EnabledOnly: true}); err != nil {
		return nil, 0, refusal(err)
	}
	all := make([]mcp.SkillSummary, 0, len(rs.Skills)+len(team.Skills))
	own := make(map[string]bool, len(rs.Skills))
	for _, sk := range rs.Skills {
		own[sk.Name] = true
		all = append(all, mcp.SkillSummary{Name: sk.Name, Description: sk.Description})
	}
	for _, sk := range team.Skills {
		if !own[sk.Name] {
			all = append(all, mcp.SkillSummary{Name: sk.Name, Description: sk.Description, TeamWorkspace: sk.TeamWorkspaceName})
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Name < all[j].Name })
	total := len(all)
	all = all[min(offset, total):]
	if limit > 0 {
		all = all[:min(limit, crud.MaxSkillSearchLimit, len(all))]
	}
	return all, total, nil
}

// visible reports whether the agent sees a skill: on for the account and on
// in this workspace.
func visible(sk entity.Skill) bool {
	return sk.Enabled && sk.WorkspaceEnabled
}

// skillOf is which skill a name is, to this workspace's agent: the
// account's, when it sees it, or else a team skill of that name; own is nil
// for a team skill, and both are nil when there is neither. A team skill the
// workspace is opted out of, or an account skill hidden here with no team
// skill to stand for it, is hidden: a refusal for a write, and a miss for a
// read, as searchSkills does not list it.
func (s *skillStore) skillOf(ctx context.Context, name string) (own *entity.Skill, team *entity.TeamSkill, hidden, err error) {
	rs, err := s.crud.GetSkill(ctx, entity.GetSkillRequest{WorkspaceID: s.workspaceID, UserID: s.userID, Name: name})
	switch {
	case err == nil && visible(rs.Skill):
		return &rs.Skill, nil, nil, nil
	case err != nil && !errors.Is(err, base.ErrNotFound):
		return nil, nil, nil, refusal(err)
	}
	t, terr := s.crud.GetWorkspaceTeamSkill(ctx, entity.WorkspaceTeamSkillRequest{WorkspaceID: s.workspaceID, UserID: s.userID, Name: name})
	switch {
	case terr == nil && t.On:
		return nil, t, nil, nil
	case terr == nil:
		return nil, nil, &mcp.SkillRefusal{Message: fmt.Sprintf("skill %q is the team's, in team workspace %q, and this workspace is opted out of it, so this agent cannot use or change it; ask the human to opt this workspace in from its Skills tab, or choose another name", name, t.TeamWorkspaceName)}, nil
	case !errors.Is(terr, base.ErrNotFound):
		return nil, nil, nil, refusal(terr)
	case err != nil:
		return nil, nil, nil, nil
	case !rs.Skill.Enabled:
		return nil, nil, &mcp.SkillRefusal{Message: fmt.Sprintf("skill %q is turned off for every workspace, so agents cannot use or change it; ask the human to turn it on in Skills", rs.Skill.Name)}, nil
	}
	return nil, nil, &mcp.SkillRefusal{Message: fmt.Sprintf("skill %q belongs to the account but is not on in this workspace, so this agent cannot use or change it; ask the human to turn it on in this workspace's Skills tab, or choose another name", rs.Skill.Name)}, nil
}

// writable is skillOf for a write, which a hidden skill refuses.
func (s *skillStore) writable(ctx context.Context, name string) (*entity.Skill, *entity.TeamSkill, error) {
	own, team, hidden, err := s.skillOf(ctx, name)
	if err == nil {
		err = hidden
	}
	return own, team, err
}

// LoadSkillFile reads one file, and for a SKILL.md the paths of its skill's
// files as well, which is what loadSkill lists after it, and the file's
// version for a team skill, which others write too. A skill the agent does
// not see is not found, as searchSkills does not list it. A file of a skill of the account's the agent sees is one lookup;
// anything else asks which skill the name is.
func (s *skillStore) LoadSkillFile(ctx context.Context, name, path string) (mcp.SkillFile, bool, error) {
	ctx = entity.WithOrigin(ctx, entity.OriginMCP)
	file, err := s.crud.GetSkillFile(ctx, entity.GetSkillFileRequest{WorkspaceID: s.workspaceID, UserID: s.userID, Name: name, Path: path})
	if err != nil && !errors.Is(err, base.ErrNotFound) {
		return mcp.SkillFile{}, false, refusal(err)
	}
	if err == nil && visible(file.Skill) {
		out := mcp.SkillFile{Content: file.File.Content}
		if path != skill.FileName {
			return out, true, nil
		}
		rs, err := s.crud.GetSkill(ctx, entity.GetSkillRequest{WorkspaceID: s.workspaceID, UserID: s.userID, Name: name})
		if errors.Is(err, base.ErrNotFound) {
			return mcp.SkillFile{}, false, nil
		}
		if err != nil {
			return mcp.SkillFile{}, false, refusal(err)
		}
		for _, f := range rs.Skill.Files {
			out.Files = append(out.Files, f.Path)
		}
		return out, true, nil
	}
	_, team, _, err := s.skillOf(ctx, name)
	if err != nil || team == nil {
		return mcp.SkillFile{}, false, err
	}
	tf, err := s.crud.GetWorkspaceTeamSkillFile(ctx, entity.WorkspaceTeamSkillRequest{WorkspaceID: s.workspaceID, UserID: s.userID, Name: name, Path: path})
	if errors.Is(err, base.ErrNotFound) {
		return mcp.SkillFile{}, false, nil
	}
	if err != nil {
		return mcp.SkillFile{}, false, refusal(err)
	}
	out := mcp.SkillFile{Content: tf.File.Content, Version: tf.File.Version, TeamWorkspace: team.TeamWorkspaceName}
	if path == skill.FileName {
		for _, f := range team.Files {
			out.Files = append(out.Files, f.Path)
		}
	}
	return out, true, nil
}

// newSkillHome is the team workspace a new skill goes to, 0 for the
// account. It goes to the team when the workspace is shared into a team
// workspace, unless the agent asked to keep it; into two or more, the agent
// must name one.
func (s *skillStore) newSkillHome(ctx context.Context, name string, to mcp.SkillTarget) (int64, string, error) {
	if to.Team != nil && !*to.Team {
		if to.TeamWorkspace != "" {
			return 0, "", &mcp.SkillRefusal{Message: "team is false, which keeps the skill the account's, but teamWorkspace names a team workspace; send one or the other"}
		}
		return 0, "", nil
	}
	team, err := s.teamSkills(ctx, "", false)
	if err != nil {
		return 0, "", refusal(err)
	}
	names := make([]string, len(team.TeamWorkspaces))
	for i, tw := range team.TeamWorkspaces {
		names[i] = fmt.Sprintf("%q (team %s)", tw.Name, tw.TeamName)
		if to.TeamWorkspace != "" && strings.EqualFold(strings.TrimSpace(tw.Name), strings.TrimSpace(to.TeamWorkspace)) {
			return tw.ID, tw.Name, nil
		}
	}
	switch {
	case to.TeamWorkspace != "" && len(names) == 0:
		return 0, "", &mcp.SkillRefusal{Message: fmt.Sprintf("this workspace is not shared into a team workspace, so there is no %q to save skill %q to; leave teamWorkspace out to keep it the account's", to.TeamWorkspace, name)}
	case to.TeamWorkspace != "":
		return 0, "", &mcp.SkillRefusal{Message: fmt.Sprintf("this workspace is not shared into a team workspace named %q; it is shared into %s", to.TeamWorkspace, strings.Join(names, ", "))}
	case len(names) == 0 && to.Team != nil:
		return 0, "", &mcp.SkillRefusal{Message: fmt.Sprintf("this workspace is not shared into a team workspace, so skill %q cannot be the team's; leave team out to keep it the account's", name)}
	case len(names) == 0:
		return 0, "", nil
	case len(names) == 1:
		return team.TeamWorkspaces[0].ID, team.TeamWorkspaces[0].Name, nil
	}
	return 0, "", &mcp.SkillRefusal{Message: fmt.Sprintf("this workspace is shared into %d team workspaces: %s. Send teamWorkspace with the one skill %q belongs to, or team false to keep it the account's", len(names), strings.Join(names, ", "), name)}
}

// SaveSkillFile writes on version, the one the agent read; 0 names none. A
// file of a skill the agent sees is written where the skill is; a new skill
// goes where newSkillHome says.
func (s *skillStore) SaveSkillFile(ctx context.Context, name, path, content string, version int64, to mcp.SkillTarget) (mcp.SkillSaved, error) {
	ctx = entity.WithOrigin(ctx, entity.OriginMCP)
	own, team, err := s.writable(ctx, name)
	if err != nil {
		return mcp.SkillSaved{}, err
	}
	var v *int64
	if version != 0 {
		v = &version
	}
	var teamWorkspaceID int64
	var where string
	switch {
	case team != nil:
		teamWorkspaceID, where = team.TeamWorkspaceID, team.TeamWorkspaceName
	case own == nil:
		if teamWorkspaceID, where, err = s.newSkillHome(ctx, name, to); err != nil {
			return mcp.SkillSaved{}, err
		}
	}
	if teamWorkspaceID != 0 {
		rs, err := s.crud.SaveWorkspaceTeamSkillFile(ctx, entity.SaveWorkspaceTeamSkillFileRequest{WorkspaceID: s.workspaceID, UserID: s.userID,
			Name: name, Path: path, Content: content, Version: v, TeamWorkspaceID: teamWorkspaceID})
		if err != nil {
			return mcp.SkillSaved{}, refusal(err)
		}
		return mcp.SkillSaved{Version: rs.File.Version, TeamWorkspace: where}, nil
	}
	rs, err := s.crud.SaveSkillFile(ctx, entity.SaveSkillFileRequest{WorkspaceID: s.workspaceID, UserID: s.userID, Name: name, Path: path, Content: content, Version: v})
	if err != nil {
		return mcp.SkillSaved{}, refusal(err)
	}
	return mcp.SkillSaved{Version: rs.File.Version}, nil
}

// DeleteSkill takes the skill out of this workspace. The controller deletes
// an account's skill only when no other workspace has it on, and a team
// skill only when no workspace is opted into it.
func (s *skillStore) DeleteSkill(ctx context.Context, name string) (mcp.SkillDeleted, error) {
	ctx = entity.WithOrigin(ctx, entity.OriginMCP)
	own, team, err := s.writable(ctx, name)
	switch {
	case err != nil:
		return mcp.SkillDeleted{}, err
	case team != nil:
		rs, err := s.crud.DeleteWorkspaceTeamSkill(ctx, entity.WorkspaceTeamSkillRequest{WorkspaceID: s.workspaceID, UserID: s.userID, Name: name})
		if err != nil {
			ok, err := deleted(err)
			return mcp.SkillDeleted{Deleted: ok}, err
		}
		return mcp.SkillDeleted{Deleted: true, TeamWorkspace: team.TeamWorkspaceName, Gone: rs.Deleted}, nil
	case own == nil:
		return mcp.SkillDeleted{}, nil
	}
	ok, err := deleted(s.crud.DeleteSkill(ctx, entity.DeleteSkillRequest{WorkspaceID: s.workspaceID, UserID: s.userID, Name: name}))
	return mcp.SkillDeleted{Deleted: ok}, err
}

func (s *skillStore) DeleteSkillFile(ctx context.Context, name, path string) (bool, error) {
	ctx = entity.WithOrigin(ctx, entity.OriginMCP)
	own, team, err := s.writable(ctx, name)
	switch {
	case err != nil:
		return false, err
	case team != nil:
		_, err = s.crud.DeleteWorkspaceTeamSkillFile(ctx, entity.WorkspaceTeamSkillRequest{WorkspaceID: s.workspaceID, UserID: s.userID, Name: name, Path: path})
	case own == nil:
		return false, nil
	default:
		_, err = s.crud.DeleteSkillFile(ctx, entity.DeleteSkillFileRequest{WorkspaceID: s.workspaceID, UserID: s.userID, Name: name, Path: path})
	}
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
// they belonged to the account move to it here, and links to teammates'
// skills, from before team workspaces owned skills, become skills of their
// own; every later start finds both done. A failure is logged rather than
// stopping the server, which serves every other skill; the next start tries
// again.
func moveLegacySkills(ctx context.Context, skills interface {
	MoveLegacySkills(context.Context) error
	ConvertSkillLinks(context.Context) error
}) {
	if err := skills.MoveLegacySkills(ctx); err != nil {
		zlog.Error().Err(err).Msg("skills: could not move workspace skills to their accounts; the next start tries again")
	}
	if err := skills.ConvertSkillLinks(ctx); err != nil {
		zlog.Error().Err(err).Msg("skills: could not convert links to teammates' skills; the next start tries again")
	}
}
