// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package crud

import (
	"context"
	"errors"
	"fmt"

	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	zlog "github.com/rs/zerolog/log"
)

// ConvertSkillLinks makes every link to a teammate's skill, from before team
// workspaces owned skills, a skill of its own: a copy of the files it still
// reads, under the same name and id, on where it was. A link that reads
// nothing any more (its skill gone, given to a team, or no longer shared)
// is deleted, as it was invisible already.
//
// It runs at every start and finds nothing once done. Each link converts in
// its own transaction, which re-checks it is still that link, so two
// instances starting together convert it once. A link that fails is logged
// and left for the next start.
func (c *controller) ConvertSkillLinks(ctx context.Context) error {
	links, err := c.repository.ListSkillLinks(ctx)
	if err != nil {
		return fmt.Errorf("list skill links: %w", err)
	}
	if len(links) == 0 {
		return nil
	}
	converted := 0
	for _, l := range links {
		if err := c.convertSkillLink(ctx, l); err != nil {
			zlog.Error().Err(err).Str("skill", l.Name).Int64("skillId", l.ID).Msg("skills: could not convert a link to a teammate's skill; the next start tries again")
			continue
		}
		converted++
	}
	zlog.Info().Int("links", converted).Int("of", len(links)).Msg("skills: converted links to teammates' skills")
	return nil
}

func (c *controller) convertSkillLink(ctx context.Context, link model.Skill) error {
	to := link.LinkedSkillID
	readable, err := c.repository.CanReadLinkedSkill(ctx, link.UserID, to)
	if err != nil {
		return err
	}
	src, err := c.repository.GetSkillByID(ctx, to)
	if errors.Is(err, base.ErrNotFound) || (err == nil && !readable) {
		_, err = c.repository.DeleteSkillLink(ctx, link.ID, to)
		return err
	}
	if err != nil {
		return err
	}
	files, err := c.repository.ListSkillFiles(ctx, src.ID)
	if err != nil {
		return err
	}
	own := link
	own.LinkedSkillID, own.Description, own.UpdatedAt = 0, src.Description, src.UpdatedAt
	own.SourceType, own.SourceRepo, own.SourceRef, own.SourceCommit, own.SourcePath = src.SourceType, src.SourceRepo, src.SourceRef, src.SourceCommit, src.SourcePath
	own.LocallyModified, own.FileCount, own.TotalBytes = src.LocallyModified, src.FileCount, src.TotalBytes
	// Blobs are written under the account's folder before a row points at
	// them; the teammate's stay theirs.
	var written []string
	for i, f := range files {
		content, err := c.skillStorage.LoadRaw(f.StorageID)
		if err != nil {
			c.purge(written)
			return fmt.Errorf("read %s: %w", f.Path, err)
		}
		files[i].ID, files[i].SkillID, files[i].Version = c.idgen.NextID(), own.ID, 1
		if err := c.storeSkillBlob(own, &files[i], content); err != nil {
			c.purge(written)
			return err
		}
		written = append(written, files[i].StorageID)
	}
	ok, err := c.repository.ConvertSkillLink(ctx, own, to, files)
	if err != nil || !ok {
		c.purge(written)
	}
	return err
}
