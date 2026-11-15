// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package crud

import (
	"context"
	"errors"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/mustafaturan/monoflake"
	"gorm.io/gorm"
)

// TeamReadController is what a team's reads need beyond the owner-checked
// methods. The System ones check nothing: controller/teamview calls them only
// with workspaces and accounts its team check allowed.
type TeamReadController interface {
	// SystemListTasksInWorkspaces is ListTasks over the given workspaces,
	// whoever owns them, newest activity first.
	SystemListTasksInWorkspaces(ctx context.Context, req entity.ListTasksRequest, workspaceIDs []int64) (*entity.ListTasksResponse, error)
	// SystemGetDetailedStatsOfWorkspaces is GetDetailedWorkspaceStats summed
	// over the given workspaces, whoever owns them.
	SystemGetDetailedStatsOfWorkspaces(ctx context.Context, req entity.GetStatsOfWorkspacesRequest) (*entity.GetDetailedWorkspaceStatsResponse, error)
	// SystemGetSkillFile reads one file of an account's skill, counting
	// nothing: it is not the owner looking.
	SystemGetSkillFile(ctx context.Context, req entity.SystemGetSkillFileRequest) (*entity.GetSkillFileResponse, error)
	// LinkSkill links another account's skill into the caller's account, on
	// in the given workspaces of theirs: no file is copied, and the link
	// reads and writes that skill while a team shares it with them. A
	// workspace they do not own is not found; a name they already have is a
	// SkillConflict.
	LinkSkill(ctx context.Context, req entity.LinkSkillRequest) (*entity.LinkSkillResponse, error)
}

func (c *controller) SystemListTasksInWorkspaces(ctx context.Context, req entity.ListTasksRequest, workspaceIDs []int64) (*entity.ListTasksResponse, error) {
	ms, err := c.repository.ListTasksInWorkspaces(ctx, req, workspaceIDs)
	if err != nil {
		return nil, err
	}
	return c.taskListResponse(ctx, ms, req.PreloadAgents)
}

func (c *controller) SystemGetDetailedStatsOfWorkspaces(ctx context.Context, req entity.GetStatsOfWorkspacesRequest) (*entity.GetDetailedWorkspaceStatsResponse, error) {
	startTime, endTime := statsWindow(req.Range, req.From, req.To, time.Now())
	res, err := c.repository.GetDetailedStatsOfWorkspaces(ctx, req.WorkspaceIDs, startTime, endTime)
	if err != nil {
		return nil, err
	}
	return &res, nil
}

func (c *controller) SystemGetSkillFile(ctx context.Context, req entity.SystemGetSkillFileRequest) (*entity.GetSkillFileResponse, error) {
	s, f, content, err := c.loadSkillFile(ctx, req.UserID, req.Name, req.Path)
	if err != nil {
		return nil, err
	}
	return &entity.GetSkillFileResponse{
		Skill: fromModelSkill(s, nil, 0),
		File:  entity.SkillFile{Path: f.Path, SizeBytes: f.SizeBytes, UpdatedAt: f.UpdatedAt, Content: string(content)},
	}, nil
}

func (c *controller) LinkSkill(ctx context.Context, req entity.LinkSkillRequest) (*entity.LinkSkillResponse, error) {
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	if uid == 0 || req.SkillID == 0 {
		return nil, skillErr(SkillInvalid, "invalid request")
	}
	workspaces, err := c.skillWorkspaces(ctx, req.UserID, req.WorkspaceIDs)
	if err != nil {
		return nil, err
	}
	src, err := c.repository.GetSkillByID(ctx, req.SkillID)
	if err != nil {
		return nil, err
	}
	if src.UserID == uid {
		return nil, skillErr(SkillConflict, "skill %s is your own; no teammate has one to use instead", src.Name)
	}
	switch existing, err := c.repository.GetSkill(ctx, uid, src.Name); {
	case err == nil && req.Replace:
		return c.swapSkillForLink(ctx, existing, src, workspaces)
	case err == nil:
		return nil, skillErr(SkillConflict, "you already have a skill named %s", src.Name)
	case !errors.Is(err, base.ErrNotFound):
		return nil, err
	}

	now := time.Now()
	link := model.Skill{
		ID: c.idgen.NextID(), CreatedAt: now, UpdatedAt: src.UpdatedAt, UserID: uid,
		Name: src.Name, Description: src.Description, SourceType: skillSourceTeam,
		FileCount: src.FileCount, TotalBytes: src.TotalBytes, LinkedSkillID: src.ID,
	}
	on := make([]model.WorkspaceSkill, len(workspaces))
	for i, w := range workspaces {
		on[i] = model.WorkspaceSkill{ID: c.idgen.NextID(), CreatedAt: now, UserID: uid, WorkspaceID: w}
	}
	saved, err := c.repository.LinkSkill(ctx, link, on)
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return nil, skillErr(SkillConflict, "you already have a skill named %s", src.Name)
	}
	if err != nil {
		return nil, err
	}
	return &entity.LinkSkillResponse{Skill: fromModelSkill(saved, workspaces, 0)}, nil
}

// swapSkillForLink turns the caller's skill mine into a link to src: its
// files go, its workspace switches stay, and those who linked to it now
// link to src.
func (c *controller) swapSkillForLink(ctx context.Context, mine, src model.Skill, workspaces []int64) (*entity.LinkSkillResponse, error) {
	on, err := c.repository.ListSkillWorkspaces(ctx, []int64{mine.ID})
	if err != nil {
		return nil, err
	}
	link := model.Skill{
		ID: mine.ID, CreatedAt: mine.CreatedAt, UpdatedAt: src.UpdatedAt, UserID: mine.UserID,
		Name: mine.Name, Description: src.Description, SourceType: skillSourceTeam,
		FileCount: src.FileCount, TotalBytes: src.TotalBytes, Disabled: mine.Disabled, LinkedSkillID: src.ID,
	}
	now := time.Now()
	rows := make([]model.WorkspaceSkill, len(workspaces))
	for i, w := range workspaces {
		rows[i] = model.WorkspaceSkill{ID: c.idgen.NextID(), CreatedAt: now, UserID: mine.UserID, WorkspaceID: w}
	}
	dropped, err := c.repository.SwapSkillForLink(ctx, link, rows)
	if errors.Is(err, base.ErrVersionConflict) {
		return nil, skillErr(SkillConflict, "the teammate's %s changed a moment ago; try again", mine.Name)
	}
	if err != nil {
		return nil, err
	}
	c.purge(dropped)
	return &entity.LinkSkillResponse{Skill: fromModelSkill(link, withWorkspaces(on[mine.ID], workspaces), 0)}, nil
}
