// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package crud

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/skill"
	zlog "github.com/rs/zerolog/log"
)

// MoveLegacySkills makes every skill still filed under a workspace, from
// before skills belonged to the account, a skill of its account: on in the
// workspace that had it and in every workspace it was shared into, with its
// files moved to the account's folder in storage.
//
// Names are unique per account, so a skill whose name the account already
// has is merged into that one when their files are the same and their
// account switches agree, and otherwise renamed <name>-<workspace>, with its
// SKILL.md changed to match.
//
// It runs at every start and finds nothing once the move is done. Each skill
// moves in its own transaction, which re-checks the skill is still where it
// was, so two instances starting together each move a skill at most once. A
// skill that fails is logged and left for the next start.
func (c *controller) MoveLegacySkills(ctx context.Context) error {
	legacy, err := c.repository.ListLegacySkills(ctx)
	if err != nil {
		return fmt.Errorf("list legacy skills: %w", err)
	}
	if len(legacy) == 0 {
		return nil
	}
	accounts := map[int64]map[string]model.Skill{}
	moved := 0
	for _, s := range legacy {
		taken, ok := accounts[s.UserID]
		if !ok {
			own, _, err := c.repository.SearchSkills(ctx, s.UserID, "", base.SkillFilter{}, 0, 0)
			if err != nil {
				return fmt.Errorf("list account skills: %w", err)
			}
			taken = make(map[string]model.Skill, len(own))
			for _, o := range own {
				taken[o.Name] = o
			}
			accounts[s.UserID] = taken
		}
		name, err := c.moveLegacySkill(ctx, s, taken)
		if err != nil {
			zlog.Error().Err(err).Str("skill", s.Name).Int64("skillId", s.ID).Msg("skills: could not move a workspace skill to its account; the next start tries again")
			continue
		}
		if name != "" {
			s.Name, s.WorkspaceID = name, 0
			taken[name] = s
		}
		moved++
	}
	zlog.Info().Int("skills", moved).Int("of", len(legacy)).Msg("skills: moved workspace skills to their accounts")
	return nil
}

// moveLegacySkill moves or merges one legacy skill, and returns the name it
// now has in the account, or "" when it was merged into another.
func (c *controller) moveLegacySkill(ctx context.Context, s model.Skill, taken map[string]model.Skill) (string, error) {
	shares, err := c.repository.ListLegacySkillShares(ctx, s.ID)
	if err != nil {
		return "", err
	}
	now := time.Now()
	workspaces := append([]int64{s.WorkspaceID}, shares...)
	on := make([]model.WorkspaceSkill, len(workspaces))
	for i, w := range workspaces {
		on[i] = model.WorkspaceSkill{ID: c.idgen.NextID(), CreatedAt: now, UserID: s.UserID, WorkspaceID: w}
	}
	files, err := c.repository.ListSkillFiles(ctx, s.ID)
	if err != nil {
		return "", err
	}

	name := s.Name
	if existing, clash := taken[s.Name]; clash {
		same, err := c.sameSkillFiles(ctx, existing, files)
		if err != nil {
			return "", err
		}
		if same && existing.Disabled == s.Disabled {
			dropped, err := c.repository.MergeLegacySkill(ctx, s, existing.ID, on)
			if err != nil {
				return "", err
			}
			c.purge(dropped)
			return "", nil
		}
		name = c.freeSkillName(ctx, s, taken)
	}

	// Every blob is written afresh under the account's folder before the
	// rows point at it; the old ones go only once the move has committed.
	target := s
	target.Name, target.WorkspaceID = name, 0
	var written, old []string
	for i, f := range files {
		content, err := c.skillStorage.LoadRaw(f.StorageID)
		if err != nil {
			c.purge(written)
			return "", fmt.Errorf("read %s: %w", f.Path, err)
		}
		if name != s.Name && f.Path == skill.FileName {
			if content, err = skill.WithName(content, name); err != nil {
				c.purge(written)
				return "", err
			}
		}
		old = append(old, f.StorageID)
		if err := c.storeSkillBlob(target, &files[i], content); err != nil {
			c.purge(written)
			return "", err
		}
		written = append(written, files[i].StorageID)
	}
	ok, err := c.repository.MoveLegacySkill(ctx, s.WorkspaceID, target, files, on)
	if err != nil || !ok {
		c.purge(written)
		return "", err
	}
	c.purge(old)
	return name, nil
}

// sameSkillFiles says whether skill a has exactly the files given, by path
// and content.
func (c *controller) sameSkillFiles(ctx context.Context, a model.Skill, files []model.SkillFile) (bool, error) {
	theirs, err := c.repository.ListSkillFiles(ctx, a.ID)
	if err != nil {
		return false, err
	}
	if len(theirs) != len(files) {
		return false, nil
	}
	sums := make(map[string]string, len(theirs))
	for _, f := range theirs {
		sums[f.Path] = f.SHA256
	}
	for _, f := range files {
		if sum, ok := sums[f.Path]; !ok || sum != f.SHA256 {
			return false, nil
		}
	}
	return true, nil
}

// freeSkillName is a name the account does not have yet for a legacy skill
// whose own name it does: <name>-<workspace>, or with a number after it.
func (c *controller) freeSkillName(ctx context.Context, s model.Skill, taken map[string]model.Skill) string {
	var slug string
	if ws, err := c.repository.SystemGetWorkspace(ctx, s.WorkspaceID); err == nil {
		slug = nameSlug(ws.Name)
		slug = strings.TrimRight(slug[:min(len(slug), maxSlugLength)], "-")
	}
	for n := 1; ; n++ {
		suffix := ""
		switch {
		case slug == "":
			suffix = fmt.Sprintf("-%d", n+1)
		case n == 1:
			suffix = "-" + slug
		default:
			suffix = fmt.Sprintf("-%s-%d", slug, n)
		}
		// Cut the name rather than the suffix, which is what makes it new.
		cut := s.Name[:min(len(s.Name), skill.MaxNameLength-len(suffix))]
		candidate := strings.TrimRight(cut, "-") + suffix
		if _, clash := taken[candidate]; !clash {
			return candidate
		}
	}
}

// maxSlugLength is the most of a workspace's name a renamed skill carries.
const maxSlugLength = 24

// nameSlug folds a workspace name into the letters, digits and single
// hyphens a skill name allows.
func nameSlug(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case b.Len() > 0 && !strings.HasSuffix(b.String(), "-"):
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}
