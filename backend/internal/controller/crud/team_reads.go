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
)

// TeamReadController is what a team's reads need beyond the owner-checked
// methods. The System ones check nothing: controller/teamview calls them only
// with workspaces and accounts its team check allowed.
type TeamReadController interface {
	// SystemListTasksInWorkspaces is ListTasks over the given workspaces,
	// whoever owns them, newest activity first.
	SystemListTasksInWorkspaces(ctx context.Context, req entity.ListTasksRequest, workspaceIDs []int64) (*entity.ListTasksResponse, error)
	// SystemGetSkillFile reads one file of an account's skill, counting
	// nothing: it is not the owner looking.
	SystemGetSkillFile(ctx context.Context, req entity.SystemGetSkillFileRequest) (*entity.GetSkillFileResponse, error)
	// CopySkill copies another account's skill, every file, into the
	// caller's account, on in the given workspaces of theirs. A workspace
	// they do not own is not found; a name they already have is a
	// SkillConflict.
	CopySkill(ctx context.Context, req entity.CopySkillRequest) (*entity.CopySkillResponse, error)
}

func (c *controller) SystemListTasksInWorkspaces(ctx context.Context, req entity.ListTasksRequest, workspaceIDs []int64) (*entity.ListTasksResponse, error) {
	ms, err := c.repository.ListTasksInWorkspaces(ctx, req, workspaceIDs)
	if err != nil {
		return nil, err
	}
	return c.taskListResponse(ctx, ms, req.PreloadAgents)
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

func (c *controller) CopySkill(ctx context.Context, req entity.CopySkillRequest) (*entity.CopySkillResponse, error) {
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	if uid == 0 || req.FromUserID == 0 {
		return nil, skillErr(SkillInvalid, "invalid request")
	}
	name, err := skillName(req.Name)
	if err != nil {
		return nil, err
	}
	workspaces, err := c.skillWorkspaces(ctx, req.UserID, req.WorkspaceIDs)
	if err != nil {
		return nil, err
	}
	switch _, err := c.repository.GetSkill(ctx, uid, name); {
	case err == nil:
		return nil, skillErr(SkillConflict, "you already have a skill named %s", name)
	case !errors.Is(err, base.ErrNotFound):
		return nil, err
	}
	src, err := c.repository.GetSkill(ctx, req.FromUserID, name)
	if err != nil {
		return nil, err
	}
	srcFiles, err := c.repository.ListSkillFiles(ctx, src.ID)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	s := model.Skill{
		ID: c.idgen.NextID(), CreatedAt: now, UpdatedAt: now, UserID: uid,
		Name: src.Name, Description: src.Description,
		SourceType: src.SourceType, SourceRepo: src.SourceRepo, SourceRef: src.SourceRef,
		SourceCommit: src.SourceCommit, SourcePath: src.SourcePath, LocallyModified: src.LocallyModified,
	}
	files := make([]model.SkillFile, 0, len(srcFiles))
	var written []string
	for _, sf := range srcFiles {
		content, err := c.skillStorage.LoadRaw(sf.StorageID)
		if err == nil {
			var f model.SkillFile
			if f, err = c.storeSkillFile(s, sf.Path, content, now); err == nil {
				written = append(written, f.StorageID)
				files = append(files, f)
				continue
			}
		}
		c.purge(written)
		return nil, err
	}
	on := make([]model.WorkspaceSkill, len(workspaces))
	for i, w := range workspaces {
		on[i] = model.WorkspaceSkill{ID: c.idgen.NextID(), CreatedAt: now, UserID: uid, WorkspaceID: w}
	}
	saved, _, err := c.repository.ReplaceSkill(ctx, s, files, on)
	if err != nil {
		c.purge(written)
		return nil, err
	}
	return &entity.CopySkillResponse{Skill: fromModelSkill(saved, workspaces, 0)}, nil
}
