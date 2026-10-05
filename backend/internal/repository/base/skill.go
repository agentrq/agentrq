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

func (r *repository) GetSkill(ctx context.Context, userID, workspaceID int64, name string) (model.Skill, error) {
	var s model.Skill
	err := r.conn(ctx).
		Where("user_id = ? AND workspace_id = ? AND name = ?", userID, workspaceID, name).
		First(&s).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.Skill{}, ErrNotFound
	}
	return s, err
}

// ListSkillsByWorkspace returns the skills a workspace owns, by name.
func (r *repository) ListSkillsByWorkspace(ctx context.Context, userID, workspaceID int64) ([]model.Skill, error) {
	var skills []model.Skill
	err := r.conn(ctx).
		Where("user_id = ? AND workspace_id = ?", userID, workspaceID).
		Order("name asc").
		Find(&skills).Error
	return skills, err
}

// ListSkillsSharedInto returns the skills other workspaces of the same owner
// have shared into this one, by name.
func (r *repository) ListSkillsSharedInto(ctx context.Context, userID, workspaceID int64) ([]model.Skill, error) {
	var skills []model.Skill
	shared := r.conn(ctx).Model(&model.SkillShare{}).Select("skill_id").
		Where("user_id = ? AND target_workspace_id = ?", userID, workspaceID)
	err := r.conn(ctx).
		Where("user_id = ? AND id IN (?)", userID, shared).
		Order("name asc").
		Find(&skills).Error
	return skills, err
}

// SearchSkills returns the skills a workspace can use — its own and those
// shared into it — whose name or description contains q, ignoring case, by
// name; and how many match in all. enabledOnly leaves out the skills turned
// off. A limit of 0 returns every match.
func (r *repository) SearchSkills(ctx context.Context, userID, workspaceID int64, q string, enabledOnly bool, limit, offset int) ([]model.Skill, int64, error) {
	shared := r.conn(ctx).Model(&model.SkillShare{}).Select("skill_id").
		Where("user_id = ? AND target_workspace_id = ?", userID, workspaceID)
	query := r.conn(ctx).Model(&model.Skill{}).
		Where("user_id = ? AND (workspace_id = ? OR id IN (?))", userID, workspaceID, shared)
	if q != "" {
		// LIKE's own wildcards in q are matched literally.
		pattern := "%" + likeEscaper.Replace(strings.ToLower(q)) + "%"
		query = query.Where(`(LOWER(name) LIKE ? ESCAPE '\' OR LOWER(description) LIKE ? ESCAPE '\')`, pattern, pattern)
	}
	if enabledOnly {
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
// when s.ID is not stored yet. It returns the storage ids of the files it
// dropped, for the caller to purge after the commit.
func (r *repository) ReplaceSkill(ctx context.Context, s model.Skill, files []model.SkillFile) (model.Skill, []string, error) {
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
		return recount(tx, &s)
	})
	if err != nil {
		return model.Skill{}, nil, err
	}
	return s, dropped, nil
}

// UpsertSkillFile writes one file of a skill, creating the skill when s.ID is
// not stored yet, and returns the storage id of the content it replaced ("" for
// a new file).
func (r *repository) UpsertSkillFile(ctx context.Context, s model.Skill, f model.SkillFile) (model.Skill, string, error) {
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
		return recount(tx, &s)
	})
	if err != nil {
		return model.Skill{}, "", err
	}
	return s, replaced, nil
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

// DeleteSkill removes a skill, its files and its shares, and returns the
// storage ids of the files' content.
func (r *repository) DeleteSkill(ctx context.Context, skillID int64) ([]string, error) {
	var storageIDs []string
	err := r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.SkillFile{}).Where("skill_id = ?", skillID).Pluck("storage_id", &storageIDs).Error; err != nil {
			return err
		}
		if err := tx.Where("skill_id = ?", skillID).Delete(&model.SkillFile{}).Error; err != nil {
			return err
		}
		if err := tx.Where("skill_id = ?", skillID).Delete(&model.SkillShare{}).Error; err != nil {
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

// SetSkillDisabled turns a skill off for agents, or back on. It leaves
// updated_at alone: the skill's content did not change.
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

// CreateSkillShare shares a skill into a workspace. Sharing it again is not an
// error: the state the caller asked for already holds.
func (r *repository) CreateSkillShare(ctx context.Context, sh model.SkillShare) error {
	return r.conn(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&sh).Error
}

func (r *repository) DeleteSkillShare(ctx context.Context, skillID, targetWorkspaceID int64) error {
	res := r.conn(ctx).
		Where("skill_id = ? AND target_workspace_id = ?", skillID, targetWorkspaceID).
		Delete(&model.SkillShare{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *repository) ListSkillShares(ctx context.Context, skillID int64) ([]model.SkillShare, error) {
	var shares []model.SkillShare
	err := r.conn(ctx).Where("skill_id = ?", skillID).Order("created_at asc").Find(&shares).Error
	return shares, err
}

// GetWorkspaceSkillStorageIDs lists the storage ids of every skill file a
// workspace owns, so deleting the workspace can purge them.
func (r *repository) GetWorkspaceSkillStorageIDs(ctx context.Context, workspaceID int64) ([]string, error) {
	var ids []string
	skillIDs := r.conn(ctx).Model(&model.Skill{}).Select("id").Where("workspace_id = ?", workspaceID)
	err := r.conn(ctx).Model(&model.SkillFile{}).Where("skill_id IN (?)", skillIDs).Pluck("storage_id", &ids).Error
	return ids, err
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
