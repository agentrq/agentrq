// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"net/url"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	view "github.com/agentrq/agentrq/backend/internal/data/view/api"
	"github.com/gofiber/fiber/v2"
	"github.com/mustafaturan/monoflake"
)

// FromHTTPRequestToWorkspaceTeamSkillsRequestEntity reads the workspace and
// ?q=.
func FromHTTPRequestToWorkspaceTeamSkillsRequestEntity(c *fiber.Ctx) *entity.WorkspaceTeamSkillsRequest {
	workspaceID, _, ok := skillParams(c, false)
	if !ok || workspaceID == 0 {
		return nil
	}
	return &entity.WorkspaceTeamSkillsRequest{WorkspaceID: workspaceID, Query: c.Query("q")}
}

// FromHTTPRequestToSetWorkspaceTeamSkillRequestEntity reads {"on": bool},
// which must be given.
func FromHTTPRequestToSetWorkspaceTeamSkillRequestEntity(c *fiber.Ctx) *entity.SetWorkspaceTeamSkillRequest {
	workspaceID := monoflake.IDFromBase62(c.Params("id")).Int64()
	skillID := monoflake.IDFromBase62(c.Params("skillId")).Int64()
	var rq view.SetWorkspaceTeamSkillRequest
	if workspaceID == 0 || skillID == 0 || c.BodyParser(&rq) != nil || rq.On == nil {
		return nil
	}
	return &entity.SetWorkspaceTeamSkillRequest{WorkspaceID: workspaceID, SkillID: skillID, On: *rq.On}
}

// FromHTTPRequestToGiveSkillToTeamRequestEntity reads the skill from the
// path and the team workspace from the body.
func FromHTTPRequestToGiveSkillToTeamRequestEntity(c *fiber.Ctx) *entity.GiveSkillToTeamRequest {
	workspaceID, name, ok := skillParams(c, true)
	var rq view.GiveSkillToTeamRequest
	if !ok || workspaceID == 0 || c.BodyParser(&rq) != nil {
		return nil
	}
	tw := monoflake.IDFromBase62(rq.TeamWorkspaceID).Int64()
	if tw == 0 {
		return nil
	}
	return &entity.GiveSkillToTeamRequest{WorkspaceID: workspaceID, Name: name, TeamWorkspaceID: tw}
}

// FromHTTPRequestToTeamSkillsRequestEntity reads the team and team workspace
// from the path, and the skill and file path where the route has them.
func FromHTTPRequestToTeamSkillsRequestEntity(c *fiber.Ctx) *entity.TeamSkillsRequest {
	rq := &entity.TeamSkillsRequest{
		TeamID:          monoflake.IDFromBase62(c.Params("teamId")).Int64(),
		TeamWorkspaceID: monoflake.IDFromBase62(c.Params("teamWorkspaceId")).Int64(),
	}
	if rq.TeamID == 0 || rq.TeamWorkspaceID == 0 {
		return nil
	}
	if raw := c.Params("skillId"); raw != "" {
		if rq.SkillID = monoflake.IDFromBase62(raw).Int64(); rq.SkillID == 0 {
			return nil
		}
	}
	p, err := url.PathUnescape(c.Params("*"))
	if err != nil {
		return nil
	}
	rq.Path = p
	return rq
}

func FromHTTPRequestToSaveTeamSkillFileRequestEntity(c *fiber.Ctx) *entity.SaveTeamSkillFileRequest {
	at := FromHTTPRequestToTeamSkillsRequestEntity(c)
	var rq view.SaveTeamSkillFileRequest
	if at == nil || c.BodyParser(&rq) != nil {
		return nil
	}
	return &entity.SaveTeamSkillFileRequest{TeamSkillsRequest: *at, Content: rq.Content, Version: rq.Version}
}

func FromHTTPRequestToImportTeamSkillsRequestEntity(c *fiber.Ctx) *entity.ImportTeamSkillsRequest {
	at := FromHTTPRequestToTeamSkillsRequestEntity(c)
	var rq view.ImportTeamSkillsRequest
	if at == nil || c.BodyParser(&rq) != nil || rq.URL == "" {
		return nil
	}
	return &entity.ImportTeamSkillsRequest{TeamSkillsRequest: *at, URL: rq.URL, Overwrite: rq.Overwrite, Skills: rq.Skills}
}

func fromTeamSkillEntityToView(t entity.TeamSkill) view.TeamWorkspaceSkill {
	v := view.TeamWorkspaceSkill{Skill: fromEntitySkillToView(t.Skill), TeamID: id62(t.TeamID), TeamWorkspaceID: id62(t.TeamWorkspaceID),
		TeamWorkspaceName: t.TeamWorkspaceName, On: t.On, Clash: t.Clash}
	for i, f := range t.Files {
		v.Files[i].Version = f.Version
	}
	return v
}

func FromWorkspaceTeamSkillsResponseEntityToHTTPResponse(rs *entity.WorkspaceTeamSkillsResponse) []byte {
	out := view.WorkspaceTeamSkillsResponse{Skills: make([]view.TeamWorkspaceSkill, len(rs.Skills)), TeamWorkspaces: make([]view.TeamWorkspaceRef, len(rs.TeamWorkspaces))}
	for i, s := range rs.Skills {
		out.Skills[i] = fromTeamSkillEntityToView(s)
	}
	for i, t := range rs.TeamWorkspaces {
		out.TeamWorkspaces[i] = view.TeamWorkspaceRef{ID: id62(t.ID), Name: t.Name, TeamID: id62(t.TeamID), TeamName: t.TeamName}
	}
	b, _ := json.Marshal(out)
	return b
}

func FromTeamSkillEntityToHTTPResponse(t *entity.TeamSkill) []byte {
	b, _ := json.Marshal(view.TeamWorkspaceSkillResponse{Skill: fromTeamSkillEntityToView(*t)})
	return b
}

func FromTeamSkillEntitiesToHTTPResponse(ts []entity.TeamSkill) []byte {
	out := view.TeamWorkspaceSkillsResponse{Skills: make([]view.TeamWorkspaceSkill, len(ts))}
	for i, t := range ts {
		out.Skills[i] = fromTeamSkillEntityToView(t)
	}
	b, _ := json.Marshal(out)
	return b
}

func FromTeamSkillFileResponseEntityToHTTPResponse(rs *entity.TeamSkillFileResponse) []byte {
	f := fromEntitySkillFileToView(rs.File)
	f.Version = rs.File.Version
	b, _ := json.Marshal(view.TeamWorkspaceSkillFileResponse{Skill: fromTeamSkillEntityToView(rs.Skill), File: f})
	return b
}

func FromImportTeamSkillsResponseEntityToHTTPResponse(rs *entity.ImportTeamSkillsResponse) []byte {
	out := view.ImportTeamSkillsResponse{
		Imported: make([]view.TeamWorkspaceSkill, len(rs.Imported)), Skipped: make([]view.SkippedSkillEntry, len(rs.Skipped)),
		SourceRepo: rs.SourceRepo, SourceRef: rs.SourceRef, SourceCommit: rs.SourceCommit,
	}
	for i, s := range rs.Imported {
		out.Imported[i] = fromTeamSkillEntityToView(s)
	}
	for i, s := range rs.Skipped {
		out.Skipped[i] = view.SkippedSkillEntry{Name: s.Name, Path: s.Path, Reason: s.Reason}
	}
	for _, s := range rs.Candidates {
		out.Candidates = append(out.Candidates, view.SkillImportCandidate{Name: s.Name, Path: s.Path, SizeBytes: s.SkillBytes, Reason: s.Reason})
	}
	b, _ := json.Marshal(out)
	return b
}
