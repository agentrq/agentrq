// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package crud

import (
	"context"
	"errors"
	"fmt"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/skill"
	"github.com/agentrq/agentrq/backend/internal/service/skillimport"
	"gorm.io/gorm"
)

// TeamSkillController is a team workspace's skills: read and written by the
// agents of the workspaces shared into it that are opted in, opted into or
// out of by those workspaces' owners, and managed by the team's members from
// the team's page. Every check is the repository's, made in the same
// transaction as the read or write, at every call.
type TeamSkillController interface {
	ListWorkspaceTeamSkills(ctx context.Context, req entity.WorkspaceTeamSkillsRequest) (*entity.WorkspaceTeamSkillsResponse, error)
	GetWorkspaceTeamSkill(ctx context.Context, req entity.WorkspaceTeamSkillRequest) (*entity.TeamSkill, error)
	GetWorkspaceTeamSkillFile(ctx context.Context, req entity.WorkspaceTeamSkillRequest) (*entity.TeamSkillFileResponse, error)
	SaveWorkspaceTeamSkillFile(ctx context.Context, req entity.SaveWorkspaceTeamSkillFileRequest) (*entity.TeamSkillFileResponse, error)
	DeleteWorkspaceTeamSkill(ctx context.Context, req entity.WorkspaceTeamSkillRequest) (*entity.OptOutTeamSkillResponse, error)
	DeleteWorkspaceTeamSkillFile(ctx context.Context, req entity.WorkspaceTeamSkillRequest) (*entity.TeamSkill, error)
	SetWorkspaceTeamSkill(ctx context.Context, req entity.SetWorkspaceTeamSkillRequest) (*entity.TeamSkill, error)
	GiveSkillToTeam(ctx context.Context, req entity.GiveSkillToTeamRequest) (*entity.TeamSkill, error)

	ListTeamSkills(ctx context.Context, req entity.TeamSkillsRequest) ([]entity.TeamSkill, error)
	GetTeamSkill(ctx context.Context, req entity.TeamSkillsRequest) (*entity.TeamSkill, error)
	GetTeamSkillFile(ctx context.Context, req entity.TeamSkillsRequest) (*entity.TeamSkillFileResponse, error)
	SaveTeamSkillFile(ctx context.Context, req entity.SaveTeamSkillFileRequest) (*entity.TeamSkillFileResponse, error)
	DeleteTeamSkillFile(ctx context.Context, req entity.TeamSkillsRequest) (*entity.TeamSkill, error)
	DeleteTeamSkill(ctx context.Context, req entity.TeamSkillsRequest) error
	ImportTeamSkills(ctx context.Context, req entity.ImportTeamSkillsRequest) (*entity.ImportTeamSkillsResponse, error)
}

// DefaultTeamSkillsPerWorkspace is teams.maxSkillsPerWorkspace when it is
// not set.
const DefaultTeamSkillsPerWorkspace = 100

// TeamSkillsPerWorkspace is the most skills one team workspace may hold.
var TeamSkillsPerWorkspace = DefaultTeamSkillsPerWorkspace

// teamSkillErr turns a team skill refusal into one written for the caller;
// any other error is left as it is.
func teamSkillErr(err error, name string) error {
	switch {
	case errors.Is(err, base.ErrTeamSkillCap):
		return skillErr(SkillInvalid, "the team workspace already holds %d skills, its limit: improve an existing skill instead of adding %q, or ask the human to make room", TeamSkillsPerWorkspace, name)
	case errors.Is(err, base.ErrTeamSkillOff):
		return skillErr(SkillInvalid, "skill %q is the team's, and this workspace is opted out of it, so this agent cannot use or change it; ask the human to opt this workspace in from its Skills tab", name)
	case errors.Is(err, gorm.ErrDuplicatedKey):
		return skillErr(SkillConflict, "the team workspace already has a skill named %q", name)
	}
	return err
}

func fromTeamSkillRow(r base.TeamSkillRow, clash bool) entity.TeamSkill {
	return entity.TeamSkill{
		Skill:  fromModelSkill(r.Skill, nil, 0),
		TeamID: r.TeamID, TeamWorkspaceID: r.Skill.TeamWorkspaceID, TeamWorkspaceName: r.TeamWorkspaceName,
		On: r.On, Clash: clash,
	}
}

// withFiles is t with its files listed, without content.
func (c *controller) withFiles(ctx context.Context, t entity.TeamSkill) (entity.TeamSkill, error) {
	files, err := c.repository.ListSkillFiles(ctx, t.ID)
	if err != nil {
		return entity.TeamSkill{}, err
	}
	t.Files = make([]entity.SkillFile, len(files))
	for i, f := range files {
		t.Files[i] = entity.SkillFile{Path: f.Path, SizeBytes: f.SizeBytes, UpdatedAt: f.UpdatedAt, Version: f.Version}
	}
	return t, nil
}

// teamSkillFile is one file of team skill t, with its content.
func (c *controller) teamSkillFile(ctx context.Context, t entity.TeamSkill, path string) (*entity.TeamSkillFileResponse, error) {
	path, err := skill.CleanPath(path)
	if err != nil {
		return nil, skillErr(SkillInvalid, "%v", err)
	}
	f, err := c.repository.GetSkillFile(ctx, t.ID, path)
	if err != nil {
		return nil, err
	}
	content, err := c.skillStorage.LoadRaw(f.StorageID)
	if err != nil {
		return nil, fmt.Errorf("load skill file content: %w", err)
	}
	if t, err = c.withFiles(ctx, t); err != nil {
		return nil, err
	}
	return &entity.TeamSkillFileResponse{Skill: t, File: entity.SkillFile{
		Path: f.Path, SizeBytes: f.SizeBytes, UpdatedAt: f.UpdatedAt, Content: string(content), Version: f.Version,
	}}, nil
}

// emitTeamSkillEvent sends a change to a team skill to the team's pages, and
// counts it unless an agent made it (its tool call is counted).
func (c *controller) emitTeamSkillEvent(ctx context.Context, action entity.Action, uid, workspaceID, teamID, skillID int64) {
	actor := entity.ActorHuman
	if entity.GetOrigin(ctx) == entity.OriginMCP {
		actor = entity.ActorAgent
	}
	c.emitEvent(ctx, entity.CRUDEvent{
		Action: action, UserID: uid, WorkspaceID: workspaceID, ResourceType: entity.ResourceTeamSkill,
		ResourceID: skillID, Actor: actor, TeamID: teamID,
	})
}

// workspaceAccess resolves a workspace request: the account, the content
// workspace (the parent, for a fork) and its access to team skills.
func (c *controller) workspaceAccess(ctx context.Context, workspaceID int64, userID string) (int64, base.TeamSkillAccess, error) {
	uid, scope, err := c.memoryOwner(ctx, workspaceID, userID)
	if err != nil {
		return 0, base.TeamSkillAccess{}, err
	}
	return uid, base.TeamSkillAccess{WorkspaceID: scope, NewID: c.idgen.NextID, At: time.Now()}, nil
}

func (c *controller) memberAccess(req entity.TeamSkillsRequest) base.TeamSkillAccess {
	return base.TeamSkillAccess{UserID: req.UserID, TeamID: req.TeamID, NewID: c.idgen.NextID, At: time.Now()}
}

// ownOnNames is the names of the account's skills its agent sees in the
// workspace, which win a clash with a team skill.
func (c *controller) ownOnNames(ctx context.Context, uid, scope int64) (map[string]bool, error) {
	own, _, err := c.repository.SearchSkills(ctx, uid, "", base.SkillFilter{InWorkspace: scope, EnabledOnly: true}, 0, 0)
	if err != nil {
		return nil, err
	}
	names := make(map[string]bool, len(own))
	for _, s := range own {
		names[s.Name] = true
	}
	return names, nil
}

func (c *controller) ListWorkspaceTeamSkills(ctx context.Context, req entity.WorkspaceTeamSkillsRequest) (*entity.WorkspaceTeamSkillsResponse, error) {
	uid, a, err := c.workspaceAccess(ctx, req.WorkspaceID, req.UserID)
	if err != nil {
		return nil, err
	}
	rows, err := c.repository.ListWorkspaceTeamSkills(ctx, a.WorkspaceID, req.Query, req.OnlyOn)
	if err != nil {
		return nil, err
	}
	own, err := c.ownOnNames(ctx, uid, a.WorkspaceID)
	if err != nil {
		return nil, err
	}
	tws, err := c.repository.ListWorkspaceTeamWorkspaces(ctx, a.WorkspaceID)
	if err != nil {
		return nil, err
	}
	out := &entity.WorkspaceTeamSkillsResponse{Skills: make([]entity.TeamSkill, len(rows)), TeamWorkspaces: make([]entity.TeamWorkspaceRef, len(tws))}
	for i, r := range rows {
		out.Skills[i] = fromTeamSkillRow(r, own[r.Name])
	}
	for i, t := range tws {
		out.TeamWorkspaces[i] = entity.TeamWorkspaceRef{ID: t.ID, Name: t.Name, TeamID: t.TeamID, TeamName: t.TeamName}
	}
	return out, nil
}

// workspaceTeamSkill is the team skill of that name the workspace reaches,
// opted in or not.
func (c *controller) workspaceTeamSkill(ctx context.Context, req entity.WorkspaceTeamSkillRequest) (int64, base.TeamSkillAccess, entity.TeamSkill, error) {
	uid, a, err := c.workspaceAccess(ctx, req.WorkspaceID, req.UserID)
	if err != nil {
		return 0, a, entity.TeamSkill{}, err
	}
	name, err := skillName(req.Name)
	if err != nil {
		return 0, a, entity.TeamSkill{}, err
	}
	r, err := c.repository.GetWorkspaceTeamSkill(ctx, a.WorkspaceID, name)
	if err != nil {
		return 0, a, entity.TeamSkill{}, err
	}
	return uid, a, fromTeamSkillRow(r, false), nil
}

// GetWorkspaceTeamSkill is the team skill of that name the workspace
// reaches, with its files listed; On says whether it is opted in.
func (c *controller) GetWorkspaceTeamSkill(ctx context.Context, req entity.WorkspaceTeamSkillRequest) (*entity.TeamSkill, error) {
	_, _, t, err := c.workspaceTeamSkill(ctx, req)
	if err != nil {
		return nil, err
	}
	if t, err = c.withFiles(ctx, t); err != nil {
		return nil, err
	}
	return &t, nil
}

// GetWorkspaceTeamSkillFile reads a file of a team skill the workspace
// reaches; On says whether it is opted in, which the agent's tools need.
func (c *controller) GetWorkspaceTeamSkillFile(ctx context.Context, req entity.WorkspaceTeamSkillRequest) (*entity.TeamSkillFileResponse, error) {
	_, _, t, err := c.workspaceTeamSkill(ctx, req)
	if err != nil {
		return nil, err
	}
	return c.teamSkillFile(ctx, t, cmpPath(req.Path))
}

func cmpPath(p string) string {
	if p == "" {
		return skill.FileName
	}
	return p
}

// SaveWorkspaceTeamSkillFile writes a file of the team skill of that name
// the workspace is opted into, or with no such skill and a SKILL.md, creates
// one in req.TeamWorkspaceID.
func (c *controller) SaveWorkspaceTeamSkillFile(ctx context.Context, req entity.SaveWorkspaceTeamSkillFileRequest) (*entity.TeamSkillFileResponse, error) {
	uid, a, err := c.workspaceAccess(ctx, req.WorkspaceID, req.UserID)
	if err != nil {
		return nil, err
	}
	name, err := skillName(req.Name)
	if err != nil {
		return nil, err
	}
	t := entity.TeamSkill{TeamWorkspaceID: req.TeamWorkspaceID}
	r, err := c.repository.GetWorkspaceTeamSkill(ctx, a.WorkspaceID, name)
	switch {
	case err == nil:
		t = fromTeamSkillRow(r, false)
	case !errors.Is(err, base.ErrNotFound):
		return nil, err
	}
	rs, teamID, created, err := c.writeTeamSkillFile(ctx, a, t, req.Name, cmpPath(req.Path), []byte(req.Content), req.Version)
	if err != nil {
		return nil, err
	}
	if created {
		c.emitTeamSkillEvent(ctx, entity.ActionTeamSkillCreate, uid, a.WorkspaceID, teamID, rs.Skill.ID)
	}
	return rs, nil
}

// writeTeamSkillFile writes one file of team skill t, or creates it from a
// SKILL.md when t.ID is 0, in t.TeamWorkspaceID. name is canonical: every
// caller has it from skillName, a SKILL.md or the skill. It returns the
// file, the team and whether the skill was created.
func (c *controller) writeTeamSkillFile(ctx context.Context, a base.TeamSkillAccess, t entity.TeamSkill, name, path string, content []byte, version *int64) (*entity.TeamSkillFileResponse, int64, bool, error) {
	path, err := skill.CleanPath(path)
	if err != nil {
		return nil, 0, false, skillErr(SkillInvalid, "%v", err)
	}
	now := time.Now()
	s := model.Skill{ID: t.ID, CreatedAt: t.CreatedAt, UpdatedAt: now, Name: name, Description: t.Description,
		SourceType: t.SourceType, SourceRepo: t.SourceRepo, SourceRef: t.SourceRef, SourceCommit: t.SourceCommit,
		SourcePath: t.SourcePath, LocallyModified: t.LocallyModified, TeamWorkspaceID: t.TeamWorkspaceID}
	if path == skill.FileName {
		fm, err := skill.ParseSkillFile(content, name)
		if err != nil {
			return nil, 0, false, skillErr(SkillInvalid, "%v", err)
		}
		if fm.Name != name {
			return nil, 0, false, skillErr(SkillInvalid, "%s names the skill %q, but it is being saved as %q; make the two match", skill.FileName, fm.Name, name)
		}
		s.Description = fm.Description
	} else {
		if err := skill.CheckContent(path, content); err != nil {
			return nil, 0, false, skillErr(SkillInvalid, "%v", err)
		}
		if t.ID == 0 {
			return nil, 0, false, skillErr(SkillInvalid, "skill %q does not exist yet; save its %s first", name, skill.FileName)
		}
		if _, err := c.repository.GetSkillFile(ctx, t.ID, path); errors.Is(err, base.ErrNotFound) {
			if t.FileCount >= skill.MaxFiles {
				return nil, 0, false, skillErr(SkillInvalid, "skill %q already has %d files, the limit; delete one first", name, t.FileCount)
			}
		} else if err != nil {
			return nil, 0, false, err
		}
	}
	if imported(s.SourceType) {
		s.LocallyModified = true
	}
	if t.ID == 0 {
		s.ID, s.CreatedAt, s.SourceType = c.idgen.NextID(), now, skillSourceManual
		f, err := c.storeSkillFile(s, path, content, now)
		if err != nil {
			return nil, 0, false, err
		}
		saved, teamID, err := c.repository.CreateTeamSkill(ctx, a, s, []model.SkillFile{f}, TeamSkillsPerWorkspace)
		if err != nil {
			_ = c.skillStorage.Delete(f.StorageID)
			return nil, 0, false, teamSkillErr(err, name)
		}
		t = entity.TeamSkill{Skill: fromModelSkill(saved, nil, 0), TeamID: teamID, TeamWorkspaceID: saved.TeamWorkspaceID, TeamWorkspaceName: t.TeamWorkspaceName, On: true}
		return &entity.TeamSkillFileResponse{Skill: t, File: entity.SkillFile{Path: path, SizeBytes: len(content), UpdatedAt: now, Version: 1}}, teamID, true, nil
	}
	expect := version
	if expect == nil {
		// Without the version it read, a writer may only add a file.
		expect = new(int64)
	}
	f, err := c.storeSkillFile(s, path, content, now)
	if err != nil {
		return nil, 0, false, err
	}
	saved, stored, replaced, err := c.repository.UpsertTeamSkillFile(ctx, a, s, f, expect)
	if err != nil {
		_ = c.skillStorage.Delete(f.StorageID)
		if errors.Is(err, base.ErrVersionConflict) {
			return nil, 0, false, c.skillVersionConflict(ctx, t.ID, name, path, version, true)
		}
		return nil, 0, false, teamSkillErr(err, name)
	}
	c.purge([]string{replaced})
	t.Skill = fromModelSkill(saved, nil, 0)
	return &entity.TeamSkillFileResponse{Skill: t, File: entity.SkillFile{Path: path, SizeBytes: len(content), UpdatedAt: now, Version: stored.Version}}, t.TeamID, false, nil
}

// DeleteWorkspaceTeamSkill is an agent deleting a team skill: its workspace
// is opted out, and the skill is deleted when no workspace is opted in.
func (c *controller) DeleteWorkspaceTeamSkill(ctx context.Context, req entity.WorkspaceTeamSkillRequest) (*entity.OptOutTeamSkillResponse, error) {
	uid, a, t, err := c.workspaceTeamSkill(ctx, req)
	if err != nil {
		return nil, err
	}
	teamID, gone, storageIDs, err := c.repository.OptOutTeamSkill(ctx, a, model.TeamSkillChoice{ID: c.idgen.NextID(), CreatedAt: a.At, SkillID: t.ID, UserID: uid})
	if err != nil {
		return nil, teamSkillErr(err, t.Name)
	}
	c.purge(storageIDs)
	c.emitTeamSkillEvent(ctx, entity.ActionTeamSkillOptOut, uid, a.WorkspaceID, teamID, t.ID)
	if gone {
		c.emitTeamSkillEvent(ctx, entity.ActionTeamSkillDelete, uid, a.WorkspaceID, teamID, t.ID)
	}
	return &entity.OptOutTeamSkillResponse{Deleted: gone}, nil
}

func (c *controller) DeleteWorkspaceTeamSkillFile(ctx context.Context, req entity.WorkspaceTeamSkillRequest) (*entity.TeamSkill, error) {
	_, a, t, err := c.workspaceTeamSkill(ctx, entity.WorkspaceTeamSkillRequest{WorkspaceID: req.WorkspaceID, UserID: req.UserID, Name: req.Name})
	if err != nil {
		return nil, err
	}
	return c.deleteTeamSkillFile(ctx, a, t, req.Path)
}

func (c *controller) deleteTeamSkillFile(ctx context.Context, a base.TeamSkillAccess, t entity.TeamSkill, path string) (*entity.TeamSkill, error) {
	path, err := skill.CleanPath(path)
	if err != nil {
		return nil, skillErr(SkillInvalid, "%v", err)
	}
	if path == skill.FileName {
		return nil, skillErr(SkillInvalid, "a skill cannot lose its %s; delete the whole skill instead", skill.FileName)
	}
	s := model.Skill{ID: t.ID, CreatedAt: t.CreatedAt, UpdatedAt: time.Now(), Name: t.Name, Description: t.Description,
		SourceType: t.SourceType, SourceRepo: t.SourceRepo, SourceRef: t.SourceRef, SourceCommit: t.SourceCommit,
		SourcePath: t.SourcePath, LocallyModified: t.LocallyModified || imported(t.SourceType)}
	saved, storageID, err := c.repository.DeleteTeamSkillFile(ctx, a, s, path)
	if err != nil {
		return nil, teamSkillErr(err, t.Name)
	}
	c.purge([]string{storageID})
	t.Skill = fromModelSkill(saved, nil, 0)
	return &t, nil
}

// SetWorkspaceTeamSkill opts the workspace into, or out of, a team skill,
// as its owner chose.
func (c *controller) SetWorkspaceTeamSkill(ctx context.Context, req entity.SetWorkspaceTeamSkillRequest) (*entity.TeamSkill, error) {
	uid, a, err := c.workspaceAccess(ctx, req.WorkspaceID, req.UserID)
	if err != nil {
		return nil, err
	}
	teamID, err := c.repository.SetTeamSkillChoice(ctx, a, model.TeamSkillChoice{ID: c.idgen.NextID(), CreatedAt: a.At, SkillID: req.SkillID, UserID: uid, Off: !req.On})
	if err != nil {
		return nil, err
	}
	action := entity.ActionTeamSkillOptIn
	if !req.On {
		action = entity.ActionTeamSkillOptOut
	}
	c.emitTeamSkillEvent(ctx, action, uid, a.WorkspaceID, teamID, req.SkillID)
	rows, err := c.repository.ListWorkspaceTeamSkills(ctx, a.WorkspaceID, "", false)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		if r.ID == req.SkillID {
			own, err := c.ownOnNames(ctx, uid, a.WorkspaceID)
			if err != nil {
				return nil, err
			}
			t := fromTeamSkillRow(r, own[r.Name])
			return &t, nil
		}
	}
	return nil, base.ErrNotFound
}

// GiveSkillToTeam moves the account's own skill into a team workspace the
// workspace is shared into, keeping its id: its files are copied to the team
// workspace's keys first, and the account's copies purged after the commit,
// so the public links to them stop working.
func (c *controller) GiveSkillToTeam(ctx context.Context, req entity.GiveSkillToTeamRequest) (*entity.TeamSkill, error) {
	uid, a, err := c.workspaceAccess(ctx, req.WorkspaceID, req.UserID)
	if err != nil {
		return nil, err
	}
	name, err := skillName(req.Name)
	if err != nil {
		return nil, err
	}
	s, err := c.repository.GetSkill(ctx, uid, name)
	if err != nil {
		return nil, err
	}
	if s.LinkedSkillID != 0 {
		return nil, skillErr(SkillInvalid, "skill %q is a teammate's, linked into your account; only a skill of your own can be given to a team", name)
	}
	files, err := c.repository.ListSkillFiles(ctx, s.ID)
	if err != nil {
		return nil, err
	}
	team := s
	team.TeamWorkspaceID = req.TeamWorkspaceID
	var written []string
	for i, f := range files {
		content, err := c.skillStorage.LoadRaw(f.StorageID)
		if err == nil {
			err = c.storeSkillBlob(team, &files[i], content)
		}
		if err != nil {
			c.purge(written)
			return nil, fmt.Errorf("copy skill file %s: %w", f.Path, err)
		}
		written = append(written, files[i].StorageID)
	}
	teamID, replaced, err := c.repository.GiveSkillToTeam(ctx, a, uid, s.ID, req.TeamWorkspaceID, files, TeamSkillsPerWorkspace)
	if err != nil {
		c.purge(written)
		return nil, teamSkillErr(err, name)
	}
	c.purge(replaced)
	c.emitTeamSkillEvent(ctx, entity.ActionTeamSkillGive, uid, a.WorkspaceID, teamID, s.ID)
	r, err := c.repository.GetWorkspaceTeamSkill(ctx, a.WorkspaceID, name)
	if err != nil {
		return nil, err
	}
	t := fromTeamSkillRow(r, false)
	return &t, nil
}

func (c *controller) ListTeamSkills(ctx context.Context, req entity.TeamSkillsRequest) ([]entity.TeamSkill, error) {
	rows, err := c.repository.ListTeamWorkspaceSkills(ctx, req.UserID, req.TeamID, req.TeamWorkspaceID)
	if err != nil {
		return nil, err
	}
	out := make([]entity.TeamSkill, len(rows))
	for i, r := range rows {
		out[i] = fromTeamSkillRow(r, false)
	}
	return out, nil
}

// teamSkill is one of the team's skills, for a member.
func (c *controller) teamSkill(ctx context.Context, req entity.TeamSkillsRequest) (entity.TeamSkill, error) {
	r, err := c.repository.GetTeamSkill(ctx, req.UserID, req.TeamID, req.SkillID)
	if err != nil {
		return entity.TeamSkill{}, err
	}
	if r.Skill.TeamWorkspaceID != req.TeamWorkspaceID {
		// Another team workspace's: the path names a skill not in it.
		return entity.TeamSkill{}, base.ErrNotFound
	}
	return fromTeamSkillRow(r, false), nil
}

func (c *controller) GetTeamSkill(ctx context.Context, req entity.TeamSkillsRequest) (*entity.TeamSkill, error) {
	t, err := c.teamSkill(ctx, req)
	if err != nil {
		return nil, err
	}
	if t, err = c.withFiles(ctx, t); err != nil {
		return nil, err
	}
	return &t, nil
}

func (c *controller) GetTeamSkillFile(ctx context.Context, req entity.TeamSkillsRequest) (*entity.TeamSkillFileResponse, error) {
	t, err := c.teamSkill(ctx, req)
	if err != nil {
		return nil, err
	}
	return c.teamSkillFile(ctx, t, cmpPath(req.Path))
}

// SaveTeamSkillFile writes a file of one of the team's skills, or with no
// SkillID creates one in req.TeamWorkspaceID from its SKILL.md, named by it.
func (c *controller) SaveTeamSkillFile(ctx context.Context, req entity.SaveTeamSkillFileRequest) (*entity.TeamSkillFileResponse, error) {
	a := c.memberAccess(req.TeamSkillsRequest)
	t := entity.TeamSkill{TeamWorkspaceID: req.TeamWorkspaceID}
	name := ""
	if req.SkillID != 0 {
		var err error
		if t, err = c.teamSkill(ctx, req.TeamSkillsRequest); err != nil {
			return nil, err
		}
		name = t.Name
	} else {
		if cmpPath(req.Path) != skill.FileName {
			return nil, skillErr(SkillInvalid, "a new skill starts with its %s", skill.FileName)
		}
		fm, err := skill.ParseSkillFile([]byte(req.Content), "")
		if err != nil {
			return nil, skillErr(SkillInvalid, "%v", err)
		}
		name = fm.Name
	}
	rs, teamID, created, err := c.writeTeamSkillFile(ctx, a, t, name, cmpPath(req.Path), []byte(req.Content), req.Version)
	if err != nil {
		return nil, err
	}
	if created {
		c.emitTeamSkillEvent(ctx, entity.ActionTeamSkillCreate, req.UserID, 0, teamID, rs.Skill.ID)
	}
	return rs, nil
}

func (c *controller) DeleteTeamSkillFile(ctx context.Context, req entity.TeamSkillsRequest) (*entity.TeamSkill, error) {
	t, err := c.teamSkill(ctx, req)
	if err != nil {
		return nil, err
	}
	return c.deleteTeamSkillFile(ctx, c.memberAccess(req), t, req.Path)
}

// DeleteTeamSkill deletes one of the team's skills for every workspace.
func (c *controller) DeleteTeamSkill(ctx context.Context, req entity.TeamSkillsRequest) error {
	if _, err := c.teamSkill(ctx, req); err != nil {
		return err
	}
	teamID, storageIDs, err := c.repository.DeleteTeamSkill(ctx, c.memberAccess(req), req.SkillID)
	if err != nil {
		return err
	}
	c.purge(storageIDs)
	c.emitTeamSkillEvent(ctx, entity.ActionTeamSkillDelete, req.UserID, 0, teamID, req.SkillID)
	return nil
}

// ImportTeamSkills imports skills from GitHub or GitLab into one of the team's team
// workspaces, as ImportSkills does into an account: a name the team
// workspace has already is skipped unless Overwrite replaces it.
func (c *controller) ImportTeamSkills(ctx context.Context, req entity.ImportTeamSkillsRequest) (*entity.ImportTeamSkillsResponse, error) {
	if c.skillImport == nil {
		return nil, skillErr(SkillUpstream, "importing skills is not available on this server")
	}
	if len(req.Skills) > skillimport.MaxSelected {
		return nil, skillErr(SkillInvalid, "choose at most %d skills in one import", skillimport.MaxSelected)
	}
	rows, err := c.repository.ListTeamWorkspaceSkills(ctx, req.UserID, req.TeamID, req.TeamWorkspaceID)
	if err != nil {
		return nil, err
	}
	if c.limiter != nil && !c.limiter.AllowSkillImport(req.UserID) {
		return nil, fmt.Errorf("rate limit exceeded")
	}
	token, gitlab := c.sourceToken(ctx, req.UserID, req.URL)
	res, err := c.fetchImport(ctx, req.URL, req.Skills, token, req.List)
	if err != nil {
		return nil, c.importFetchErr(err, token, gitlab)
	}
	owned := make(map[string]base.TeamSkillRow, len(rows))
	for _, r := range rows {
		owned[r.Name] = r
	}
	out := &entity.ImportTeamSkillsResponse{
		Imported: []entity.TeamSkill{}, Skipped: []entity.SkillImportSkip{},
		SourceRepo: res.Repo, SourceRef: res.Ref, SourceCommit: res.Commit, TooLarge: res.TooLarge,
	}
	for _, sk := range res.Skipped {
		out.Skipped = append(out.Skipped, entity.SkillImportSkip{Name: sk.Name, Path: sk.Path, Reason: sk.Reason})
	}
	for _, cd := range res.Candidates {
		out.Candidates = append(out.Candidates, entity.SkillImportCandidate{Name: cd.Name, Path: cd.Path, SkillBytes: cd.SkillBytes, Reason: cd.Reason})
	}
	a := c.memberAccess(req.TeamSkillsRequest)
	for _, sk := range res.Skills {
		existing, taken := owned[sk.Name]
		if taken && !req.Overwrite {
			out.Skipped = append(out.Skipped, entity.SkillImportSkip{Name: sk.Name, Path: sk.Dir, Reason: "the team workspace already has a skill with this name; import again with overwrite to replace it"})
			continue
		}
		saved, err := c.importTeamSkill(ctx, a, req.TeamWorkspaceID, existing, taken, sk, res)
		if err != nil {
			out.Skipped = append(out.Skipped, entity.SkillImportSkip{Name: sk.Name, Path: sk.Dir, Reason: "could not be saved; try the import again: " + teamSkillErr(err, sk.Name).Error()})
			continue
		}
		out.Imported = append(out.Imported, saved)
	}
	return out, nil
}

func (c *controller) importTeamSkill(ctx context.Context, a base.TeamSkillAccess, teamWorkspaceID int64, existing base.TeamSkillRow, taken bool, sk skillimport.Skill, res *skillimport.Result) (entity.TeamSkill, error) {
	now := time.Now()
	s := model.Skill{ID: c.idgen.NextID(), CreatedAt: now, UpdatedAt: now, TeamWorkspaceID: teamWorkspaceID, Name: sk.Name, Description: sk.Description,
		SourceType: importSource(res), SourceRepo: res.Repo, SourceRef: res.Ref, SourceCommit: res.Commit, SourcePath: sk.Dir}
	if taken {
		s.ID = existing.ID
	}
	files := make([]model.SkillFile, 0, len(sk.Files))
	var written []string
	for _, f := range sk.Files {
		row, err := c.storeSkillFile(s, f.Path, f.Content, now)
		if err != nil {
			c.purge(written)
			return entity.TeamSkill{}, err
		}
		written = append(written, row.StorageID)
		files = append(files, row)
	}
	var teamID int64
	var dropped []string
	var err error
	if taken {
		s, dropped, err = c.repository.ReplaceTeamSkill(ctx, a, s, files)
		teamID = existing.TeamID
	} else {
		s, teamID, err = c.repository.CreateTeamSkill(ctx, a, s, files, TeamSkillsPerWorkspace)
	}
	if err != nil {
		c.purge(written)
		return entity.TeamSkill{}, err
	}
	c.purge(dropped)
	if !taken {
		c.emitTeamSkillEvent(ctx, entity.ActionTeamSkillCreate, a.UserID, 0, teamID, s.ID)
	}
	return entity.TeamSkill{Skill: fromModelSkill(s, nil, 0), TeamID: teamID, TeamWorkspaceID: teamWorkspaceID, TeamWorkspaceName: existing.TeamWorkspaceName, On: true}, nil
}
