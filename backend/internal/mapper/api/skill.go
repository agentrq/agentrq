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

// skillParams reads the workspace and the percent-decoded skill name from the
// path; a name is required only when the route has one.
func skillParams(c *fiber.Ctx, wantName bool) (int64, string, bool) {
	workspaceID := monoflake.IDFromBase62(c.Params("id")).Int64()
	if workspaceID == 0 {
		return 0, "", false
	}
	if !wantName {
		return workspaceID, "", true
	}
	name, err := url.PathUnescape(c.Params("name"))
	if err != nil {
		return 0, "", false
	}
	return workspaceID, name, true
}

// FromHTTPRequestToSearchSkillsRequestEntity reads ?q=&limit=&offset=; all are
// optional. A limit or offset that is not a number is left for the controller
// to refuse, as a negative one.
func FromHTTPRequestToSearchSkillsRequestEntity(c *fiber.Ctx) *entity.SearchSkillsRequest {
	workspaceID, _, ok := skillParams(c, false)
	if !ok {
		return nil
	}
	return &entity.SearchSkillsRequest{
		WorkspaceID: workspaceID,
		Query:       c.Query("q"),
		Limit:       c.QueryInt("limit", 0),
		Offset:      c.QueryInt("offset", 0),
	}
}

func FromHTTPRequestToGetSkillRequestEntity(c *fiber.Ctx) *entity.GetSkillRequest {
	workspaceID, name, ok := skillParams(c, true)
	if !ok {
		return nil
	}
	return &entity.GetSkillRequest{WorkspaceID: workspaceID, Name: name}
}

func FromHTTPRequestToGetSkillFileRequestEntity(c *fiber.Ctx) *entity.GetSkillFileRequest {
	workspaceID, name, ok := skillParams(c, true)
	if !ok {
		return nil
	}
	// The file's path is the rest of the URL, and arrives percent-encoded.
	p, err := url.PathUnescape(c.Params("*"))
	if err != nil {
		return nil
	}
	return &entity.GetSkillFileRequest{WorkspaceID: workspaceID, Name: name, Path: p}
}

func FromHTTPRequestToDeleteSkillRequestEntity(c *fiber.Ctx) *entity.DeleteSkillRequest {
	workspaceID, name, ok := skillParams(c, true)
	if !ok {
		return nil
	}
	return &entity.DeleteSkillRequest{WorkspaceID: workspaceID, Name: name}
}

// FromHTTPRequestToSetSkillEnabledRequestEntity reads {"enabled": bool}, which
// must be given: an empty body is refused rather than read as off.
func FromHTTPRequestToSetSkillEnabledRequestEntity(c *fiber.Ctx) *entity.SetSkillEnabledRequest {
	workspaceID, name, ok := skillParams(c, true)
	if !ok {
		return nil
	}
	var rq view.SetSkillEnabledRequest
	if err := c.BodyParser(&rq); err != nil || rq.Enabled == nil {
		return nil
	}
	return &entity.SetSkillEnabledRequest{WorkspaceID: workspaceID, Name: name, Enabled: *rq.Enabled}
}

func FromHTTPRequestToImportSkillsRequestEntity(c *fiber.Ctx) *entity.ImportSkillsRequest {
	workspaceID, _, ok := skillParams(c, false)
	if !ok {
		return nil
	}
	var rq view.ImportSkillsRequest
	if err := c.BodyParser(&rq); err != nil || rq.URL == "" {
		return nil
	}
	return &entity.ImportSkillsRequest{WorkspaceID: workspaceID, URL: rq.URL, Overwrite: rq.Overwrite, Skills: rq.Skills}
}

func FromHTTPRequestToListSkillSharesRequestEntity(c *fiber.Ctx) *entity.ListSkillSharesRequest {
	workspaceID, name, ok := skillParams(c, true)
	if !ok {
		return nil
	}
	return &entity.ListSkillSharesRequest{WorkspaceID: workspaceID, Name: name}
}

// FromHTTPRequestToShareSkillRequestEntity serves both sharing and
// unsharing, which name the same three things.
func FromHTTPRequestToShareSkillRequestEntity(c *fiber.Ctx) *entity.ShareSkillRequest {
	workspaceID, name, ok := skillParams(c, true)
	if !ok {
		return nil
	}
	target := monoflake.IDFromBase62(c.Params("targetWorkspaceId")).Int64()
	if target == 0 {
		return nil
	}
	return &entity.ShareSkillRequest{WorkspaceID: workspaceID, Name: name, TargetWorkspaceID: target}
}

func FromSearchSkillsResponseEntityToHTTPResponse(rs *entity.SearchSkillsResponse) []byte {
	skills := make([]view.Skill, len(rs.Skills))
	for i, s := range rs.Skills {
		skills[i] = fromEntitySkillToView(s)
	}
	payload, _ := json.Marshal(view.SearchSkillsResponse{Skills: skills, Total: rs.Total})
	return payload
}

func FromGetSkillResponseEntityToHTTPResponse(rs *entity.GetSkillResponse) []byte {
	payload, _ := json.Marshal(view.GetSkillResponse{Skill: fromEntitySkillToView(rs.Skill)})
	return payload
}

func FromSetSkillEnabledResponseEntityToHTTPResponse(rs *entity.SetSkillEnabledResponse) []byte {
	payload, _ := json.Marshal(view.GetSkillResponse{Skill: fromEntitySkillToView(rs.Skill)})
	return payload
}

func FromGetSkillFileResponseEntityToHTTPResponse(rs *entity.GetSkillFileResponse) []byte {
	payload, _ := json.Marshal(view.GetSkillFileResponse{
		Skill: fromEntitySkillToView(rs.Skill),
		File:  fromEntitySkillFileToView(rs.File),
	})
	return payload
}

func FromImportSkillsResponseEntityToHTTPResponse(rs *entity.ImportSkillsResponse) []byte {
	out := view.ImportSkillsResponse{
		Imported:     make([]view.ImportedSkill, len(rs.Imported)),
		Skipped:      make([]view.SkippedSkillEntry, len(rs.Skipped)),
		SourceRepo:   rs.SourceRepo,
		SourceRef:    rs.SourceRef,
		SourceCommit: rs.SourceCommit,
	}
	for i, s := range rs.Imported {
		out.Imported[i] = view.ImportedSkill{Name: s.Name, FileCount: s.FileCount, TotalBytes: s.TotalBytes}
	}
	for i, s := range rs.Skipped {
		out.Skipped[i] = view.SkippedSkillEntry{Name: s.Name, Path: s.Path, Reason: s.Reason}
	}
	for _, s := range rs.Candidates {
		out.Candidates = append(out.Candidates, view.SkillImportCandidate{Name: s.Name, Path: s.Path, SizeBytes: s.SkillBytes, Reason: s.Reason})
	}
	payload, _ := json.Marshal(out)
	return payload
}

func FromListSkillSharesResponseEntityToHTTPResponse(rs *entity.ListSkillSharesResponse) []byte {
	shares := make([]view.SkillShare, len(rs.Shares))
	for i, s := range rs.Shares {
		shares[i] = view.SkillShare{TargetWorkspaceID: monoflake.ID(s.TargetWorkspaceID).String(), CreatedAt: s.CreatedAt}
	}
	payload, _ := json.Marshal(view.ListSkillSharesResponse{Shares: shares})
	return payload
}

func fromEntitySkillToView(s entity.Skill) view.Skill {
	v := view.Skill{
		ID:              monoflake.ID(s.ID).String(),
		CreatedAt:       s.CreatedAt,
		UpdatedAt:       s.UpdatedAt,
		WorkspaceID:     monoflake.ID(s.WorkspaceID).String(),
		Name:            s.Name,
		Description:     s.Description,
		SourceType:      s.SourceType,
		SourceRepo:      s.SourceRepo,
		SourceRef:       s.SourceRef,
		SourceCommit:    s.SourceCommit,
		SourcePath:      s.SourcePath,
		LocallyModified: s.LocallyModified,
		FileCount:       s.FileCount,
		TotalBytes:      s.TotalBytes,
		Enabled:         s.Enabled,
	}
	if s.SharedFromWorkspaceID != 0 {
		v.SharedFromWorkspaceID = monoflake.ID(s.SharedFromWorkspaceID).String()
	}
	for _, f := range s.Files {
		v.Files = append(v.Files, fromEntitySkillFileToView(f))
	}
	return v
}

func fromEntitySkillFileToView(f entity.SkillFile) view.SkillFile {
	return view.SkillFile{Path: f.Path, SizeBytes: f.SizeBytes, UpdatedAt: f.UpdatedAt, Content: f.Content, URL: f.URL}
}
