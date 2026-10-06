// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package base

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/agentrq/agentrq/backend/internal/data/model"
)

// accountSkill is the workspace_id every skill of an account carries; a
// non-zero one is a legacy skill the backfill has yet to move.
const accountSkill = 0

// SkillFilter narrows a skill search.
type SkillFilter struct {
	// InWorkspace keeps only the skills turned on in that workspace; 0 keeps
	// every skill of the account.
	InWorkspace int64
	// EnabledOnly leaves out the skills turned off for the whole account.
	EnabledOnly bool
}

func (r *repository) GetSkill(ctx context.Context, userID int64, name string) (model.Skill, error) {
	var s model.Skill
	err := r.conn(ctx).
		Where("user_id = ? AND workspace_id = ? AND name = ?", userID, accountSkill, name).
		First(&s).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.Skill{}, ErrNotFound
	}
	return s, err
}

// SearchSkills returns an account's skills whose name or description contains
// q, ignoring case, by name; and how many match in all. A limit of 0 returns
// every match.
func (r *repository) SearchSkills(ctx context.Context, userID int64, q string, filter SkillFilter, limit, offset int) ([]model.Skill, int64, error) {
	query := r.conn(ctx).Model(&model.Skill{}).
		Where("user_id = ? AND workspace_id = ?", userID, accountSkill)
	if filter.InWorkspace != 0 {
		on := r.conn(ctx).Model(&model.WorkspaceSkill{}).Select("skill_id").
			Where("user_id = ? AND workspace_id = ?", userID, filter.InWorkspace)
		query = query.Where("id IN (?)", on)
	}
	if q != "" {
		// LIKE's own wildcards in q are matched literally.
		pattern := "%" + likeEscaper.Replace(strings.ToLower(q)) + "%"
		query = query.Where(`(LOWER(name) LIKE ? ESCAPE '\' OR LOWER(description) LIKE ? ESCAPE '\')`, pattern, pattern)
	}
	if filter.EnabledOnly {
		query = query.Where("disabled = ?", false)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	page := query.Order("name asc").Offset(offset)
	if limit > 0 {
		page = page.Limit(limit)
	}
	var skills []model.Skill
	if err := page.Find(&skills).Error; err != nil {
		return nil, 0, err
	}
	return skills, total, nil
}

var likeEscaper = strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`)

// ReplaceSkill writes a skill and exactly the given files, creating the skill
// when s.ID is not stored yet, and turns it on in the workspaces on names. It
// returns the storage ids of the files it dropped, for the caller to purge
// after the commit.
func (r *repository) ReplaceSkill(ctx context.Context, s model.Skill, files []model.SkillFile, on []model.WorkspaceSkill) (model.Skill, []string, error) {
	var dropped []string
	err := r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(&s).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.SkillFile{}).Where("skill_id = ?", s.ID).Pluck("storage_id", &dropped).Error; err != nil {
			return err
		}
		if err := tx.Where("skill_id = ?", s.ID).Delete(&model.SkillFile{}).Error; err != nil {
			return err
		}
		for i := range files {
			files[i].SkillID = s.ID
		}
		if len(files) > 0 {
			if err := tx.Create(&files).Error; err != nil {
				return err
			}
		}
		if err := turnOn(tx, s.ID, on); err != nil {
			return err
		}
		return recount(tx, &s)
	})
	if err != nil {
		return model.Skill{}, nil, err
	}
	return s, dropped, nil
}

// UpsertSkillFile writes one file of a skill, creating the skill when s.ID is
// not stored yet, turns it on in the workspaces on names, and returns the
// storage id of the content it replaced ("" for a new file).
func (r *repository) UpsertSkillFile(ctx context.Context, s model.Skill, f model.SkillFile, on []model.WorkspaceSkill) (model.Skill, string, error) {
	var replaced string
	err := r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(&s).Error; err != nil {
			return err
		}
		var old model.SkillFile
		err := tx.Where("skill_id = ? AND path = ?", s.ID, f.Path).First(&old).Error
		switch {
		case err == nil:
			replaced = old.StorageID
			f.ID, f.CreatedAt = old.ID, old.CreatedAt
		case !errors.Is(err, gorm.ErrRecordNotFound):
			return err
		}
		f.SkillID = s.ID
		if err := tx.Save(&f).Error; err != nil {
			return err
		}
		if err := turnOn(tx, s.ID, on); err != nil {
			return err
		}
		return recount(tx, &s)
	})
	if err != nil {
		return model.Skill{}, "", err
	}
	return s, replaced, nil
}

// turnOn turns a skill on in each workspace of on; one where it is on
// already is left as it is.
func turnOn(tx *gorm.DB, skillID int64, on []model.WorkspaceSkill) error {
	if len(on) == 0 {
		return nil
	}
	for i := range on {
		on[i].SkillID = skillID
	}
	return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&on).Error
}

// DeleteSkillFile removes one file and returns the storage id of its content.
func (r *repository) DeleteSkillFile(ctx context.Context, s model.Skill, path string) (model.Skill, string, error) {
	var storageID string
	err := r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		var f model.SkillFile
		err := tx.Where("skill_id = ? AND path = ?", s.ID, path).First(&f).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if err := tx.Delete(&f).Error; err != nil {
			return err
		}
		storageID = f.StorageID
		if err := tx.Save(&s).Error; err != nil {
			return err
		}
		return recount(tx, &s)
	})
	if err != nil {
		return model.Skill{}, "", err
	}
	return s, storageID, nil
}

// DeleteSkill removes a skill, its files and every workspace it is on in, and
// returns the storage ids of the files' content.
func (r *repository) DeleteSkill(ctx context.Context, skillID int64) ([]string, error) {
	var storageIDs []string
	err := r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.SkillFile{}).Where("skill_id = ?", skillID).Pluck("storage_id", &storageIDs).Error; err != nil {
			return err
		}
		if err := tx.Where("skill_id = ?", skillID).Delete(&model.SkillFile{}).Error; err != nil {
			return err
		}
		if err := tx.Where("skill_id = ?", skillID).Delete(&model.WorkspaceSkill{}).Error; err != nil {
			return err
		}
		res := tx.Where("id = ?", skillID).Delete(&model.Skill{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return storageIDs, nil
}

// SetSkillDisabled turns a skill off for agents in every workspace, or back
// on. It leaves updated_at alone: the skill's content did not change.
func (r *repository) SetSkillDisabled(ctx context.Context, skillID int64, disabled bool) error {
	res := r.conn(ctx).Model(&model.Skill{}).Where("id = ?", skillID).UpdateColumn("disabled", disabled)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *repository) GetSkillFile(ctx context.Context, skillID int64, path string) (model.SkillFile, error) {
	var f model.SkillFile
	err := r.conn(ctx).Where("skill_id = ? AND path = ?", skillID, path).First(&f).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.SkillFile{}, ErrNotFound
	}
	return f, err
}

// ListSkillFiles returns a skill's files, by path.
func (r *repository) ListSkillFiles(ctx context.Context, skillID int64) ([]model.SkillFile, error) {
	var files []model.SkillFile
	err := r.conn(ctx).Where("skill_id = ?", skillID).Order("path asc").Find(&files).Error
	return files, err
}

// ListSkillWorkspaces returns the workspaces each of the skills is on in, in
// the order they were turned on. A skill on in none is not in the map.
func (r *repository) ListSkillWorkspaces(ctx context.Context, skillIDs []int64) (map[int64][]int64, error) {
	out := map[int64][]int64{}
	if len(skillIDs) == 0 {
		return out, nil
	}
	var rows []model.WorkspaceSkill
	if err := r.conn(ctx).Where("skill_id IN ?", skillIDs).Order("created_at asc, id asc").Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.SkillID] = append(out[row.SkillID], row.WorkspaceID)
	}
	return out, nil
}

// TurnSkillOn turns a skill on in a workspace. Turning it on again is not an
// error: the state the caller asked for already holds.
func (r *repository) TurnSkillOn(ctx context.Context, on model.WorkspaceSkill) error {
	return r.conn(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&on).Error
}

// TurnSkillOff turns a skill off in a workspace, and says whether it was on.
func (r *repository) TurnSkillOff(ctx context.Context, skillID, workspaceID int64) (bool, error) {
	res := r.conn(ctx).
		Where("skill_id = ? AND workspace_id = ?", skillID, workspaceID).
		Delete(&model.WorkspaceSkill{})
	return res.RowsAffected > 0, res.Error
}

// ListLegacySkills returns every skill still filed under a workspace, from
// before skills belonged to the account, oldest first.
func (r *repository) ListLegacySkills(ctx context.Context) ([]model.Skill, error) {
	var skills []model.Skill
	err := r.conn(ctx).Where("workspace_id <> ?", accountSkill).Order("created_at asc, id asc").Find(&skills).Error
	return skills, err
}

// ListLegacySkillShares returns the workspaces a legacy skill was shared into.
func (r *repository) ListLegacySkillShares(ctx context.Context, skillID int64) ([]int64, error) {
	var targets []int64
	err := r.conn(ctx).Model(&model.SkillShare{}).Where("skill_id = ?", skillID).
		Order("created_at asc, id asc").Pluck("target_workspace_id", &targets).Error
	return targets, err
}

// MoveLegacySkill makes a legacy skill the account's: s as it is to be saved,
// with its files' new storage ids, turned on in the workspaces of on, and its
// shares gone. It reports false, changing nothing, when the skill is no
// longer filed under fromWorkspaceID, because another instance moved it first.
func (r *repository) MoveLegacySkill(ctx context.Context, fromWorkspaceID int64, s model.Skill, files []model.SkillFile, on []model.WorkspaceSkill) (bool, error) {
	moved := false
	err := r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&model.Skill{}).Where("id = ? AND workspace_id = ?", s.ID, fromWorkspaceID).
			Updates(map[string]any{"workspace_id": accountSkill, "name": s.Name, "description": s.Description})
		if res.Error != nil || res.RowsAffected == 0 {
			return res.Error
		}
		for _, f := range files {
			if err := tx.Model(&model.SkillFile{}).Where("id = ?", f.ID).
				Updates(map[string]any{"storage_id": f.StorageID, "size_bytes": f.SizeBytes, "sha256": f.SHA256}).Error; err != nil {
				return err
			}
		}
		if err := turnOn(tx, s.ID, on); err != nil {
			return err
		}
		if err := tx.Where("skill_id = ?", s.ID).Delete(&model.SkillShare{}).Error; err != nil {
			return err
		}
		if err := recount(tx, &s); err != nil {
			return err
		}
		moved = true
		return nil
	})
	return moved, err
}

// MergeLegacySkill folds a legacy skill into the account skill into, which
// has the same files: into is turned on in the workspaces of on, and from is
// deleted with its files and shares. It returns the storage ids of from's
// files, for the caller to purge after the commit.
func (r *repository) MergeLegacySkill(ctx context.Context, from model.Skill, into int64, on []model.WorkspaceSkill) ([]string, error) {
	var storageIDs []string
	err := r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.SkillFile{}).Where("skill_id = ?", from.ID).Pluck("storage_id", &storageIDs).Error; err != nil {
			return err
		}
		res := tx.Where("id = ? AND workspace_id = ?", from.ID, from.WorkspaceID).Delete(&model.Skill{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			// Another instance got here first.
			storageIDs = nil
			return nil
		}
		if err := tx.Where("skill_id = ?", from.ID).Delete(&model.SkillFile{}).Error; err != nil {
			return err
		}
		if err := tx.Where("skill_id = ?", from.ID).Delete(&model.SkillShare{}).Error; err != nil {
			return err
		}
		return turnOn(tx, into, on)
	})
	if err != nil {
		return nil, err
	}
	return storageIDs, nil
}

// recount keeps a skill's file count and size in step with its files.
func recount(tx *gorm.DB, s *model.Skill) error {
	var agg struct {
		N     int
		Bytes int
	}
	if err := tx.Model(&model.SkillFile{}).
		Select("COUNT(*) AS n, COALESCE(SUM(size_bytes), 0) AS bytes").
		Where("skill_id = ?", s.ID).
		Scan(&agg).Error; err != nil {
		return err
	}
	s.FileCount, s.TotalBytes = agg.N, agg.Bytes
	return tx.Model(&model.Skill{}).Where("id = ?", s.ID).
		Updates(map[string]any{"file_count": agg.N, "total_bytes": agg.Bytes}).Error
}
