// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package crud

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/skill"
)

// TeamSkillKeeper copies the team skills people are about to lose to their
// accounts, for the change that ends their shares to commit.
type TeamSkillKeeper interface {
	// KeepTeamSkills plans the copies, with their blobs written, and returns
	// those blobs too, to purge if the change does not commit.
	KeepTeamSkills(ctx context.Context, sel base.TeamSkillsLeaving) (base.TeamSkillCopies, []string, error)
	// PurgeSkillBlobs deletes skill blobs nothing points at any more.
	PurgeSkillBlobs(storageIDs []string)
}

// keepPlan is one KeepTeamSkills call's working state.
type keepPlan struct {
	c       *controller
	ctx     context.Context
	now     time.Time
	out     base.TeamSkillCopies
	written []string
	// files is each team skill's files; became the account skill each
	// owner's copy of a team skill is.
	files  map[int64][]model.SkillFile
	became map[[2]int64]int64
	// planned is the names of the new copies; owned the ids of the account
	// skills read into out.Names.
	planned map[base.OwnSkillName]bool
	owned   map[base.OwnSkillName]int64
}

// KeepTeamSkills plans, for each team skill a workspace of the ending shares
// is opted into, what its owner keeps, on in that workspace: their own skill
// of its name when its files are the same, or else a copy, named
// <name>-<team workspace> when the account has that name already, with its
// SKILL.md changed to match.
func (c *controller) KeepTeamSkills(ctx context.Context, sel base.TeamSkillsLeaving) (base.TeamSkillCopies, []string, error) {
	rows, err := c.repository.ListTeamSkillsToKeep(ctx, sel)
	if err != nil {
		return base.TeamSkillCopies{}, nil, err
	}
	p := &keepPlan{
		c: c, ctx: ctx, now: time.Now(),
		out:   base.TeamSkillCopies{Kept: map[base.KeptTeamSkill]string{}, Names: map[base.OwnSkillName]string{}},
		files: map[int64][]model.SkillFile{}, became: map[[2]int64]int64{}, planned: map[base.OwnSkillName]bool{}, owned: map[base.OwnSkillName]int64{},
	}
	for _, r := range rows {
		if err := p.keep(r); err != nil {
			c.purge(p.written)
			return base.TeamSkillCopies{}, nil, err
		}
	}
	return p.out, p.written, nil
}

// PurgeSkillBlobs deletes the blobs.
func (c *controller) PurgeSkillBlobs(storageIDs []string) {
	c.purge(storageIDs)
}

// keep plans one workspace's team skill.
func (p *keepPlan) keep(r base.TeamSkillToKeep) error {
	files, ok := p.files[r.ID]
	if !ok {
		var err error
		if files, err = p.c.repository.ListSkillFiles(p.ctx, r.ID); err != nil {
			return err
		}
		p.files[r.ID] = files
	}
	p.out.Kept[base.KeptTeamSkill{WorkspaceID: r.KeepWorkspaceID, SkillID: r.ID}] = base.SkillFileSums(files)
	key := [2]int64{r.KeepUserID, r.ID}
	id, ok := p.became[key]
	if !ok {
		var err error
		if id, err = p.become(r, files); err != nil {
			return err
		}
		p.became[key] = id
	}
	p.out.On = append(p.out.On, model.WorkspaceSkill{ID: p.c.idgen.NextID(), CreatedAt: p.now, UserID: r.KeepUserID, SkillID: id, WorkspaceID: r.KeepWorkspaceID})
	return nil
}

// own is the sums of the owner's skill of that name, as
// TeamSkillCopies.Names holds them, and its id; read once.
func (p *keepPlan) own(userID int64, name string) (string, int64, error) {
	key := base.OwnSkillName{UserID: userID, Name: name}
	if sums, ok := p.out.Names[key]; ok {
		return sums, p.owned[key], nil
	}
	s, err := p.c.repository.GetSkill(p.ctx, userID, name)
	if errors.Is(err, base.ErrNotFound) {
		p.out.Names[key] = ""
		return "", 0, nil
	}
	if err != nil {
		return "", 0, err
	}
	files, err := p.c.repository.ListSkillFiles(p.ctx, s.ID)
	if err != nil {
		return "", 0, err
	}
	sums := base.OwnSkillSums(s, files)
	p.out.Names[key], p.owned[key] = sums, s.ID
	return sums, s.ID, nil
}

// free says whether the owner has no skill of that name, and none is
// planned.
func (p *keepPlan) free(userID int64, name string) (bool, error) {
	if p.planned[base.OwnSkillName{UserID: userID, Name: name}] {
		return false, nil
	}
	sums, _, err := p.own(userID, name)
	return sums == "", err
}

// become is the account skill the owner keeps for team skill r: their own
// of its name and files, or a new copy.
func (p *keepPlan) become(r base.TeamSkillToKeep, files []model.SkillFile) (int64, error) {
	sums, id, err := p.own(r.KeepUserID, r.Name)
	if err != nil {
		return 0, err
	}
	if sums == "+"+base.SkillFileSums(files) {
		return id, nil
	}
	name := r.Name
	if free, err := p.free(r.KeepUserID, name); err != nil || !free {
		if name, err = freeName(r.Name, r.TeamWorkspaceName, func(n string) (bool, error) { return p.free(r.KeepUserID, n) }); err != nil {
			return 0, err
		}
	}
	p.planned[base.OwnSkillName{UserID: r.KeepUserID, Name: name}] = true

	target := r.Skill
	target.ID, target.CreatedAt, target.UpdatedAt = p.c.idgen.NextID(), p.now, p.now
	target.UserID, target.WorkspaceID, target.TeamWorkspaceID = r.KeepUserID, 0, 0
	target.Name, target.LinkedSkillID, target.Disabled, target.TotalBytes = name, 0, false, 0
	for _, f := range files {
		content, err := p.c.skillStorage.LoadRaw(f.StorageID)
		if err != nil {
			return 0, fmt.Errorf("read %s: %w", f.Path, err)
		}
		if name != r.Name && f.Path == skill.FileName {
			if content, err = skill.WithName(content, name); err != nil {
				return 0, err
			}
		}
		copied := model.SkillFile{ID: p.c.idgen.NextID(), CreatedAt: p.now, UpdatedAt: p.now, SkillID: target.ID, Path: f.Path, Version: 1}
		if err := p.c.storeSkillBlob(target, &copied, content); err != nil {
			return 0, err
		}
		p.written = append(p.written, copied.StorageID)
		target.TotalBytes += copied.SizeBytes
		p.out.Files = append(p.out.Files, copied)
	}
	p.out.Skills = append(p.out.Skills, target)
	return target.ID, nil
}
