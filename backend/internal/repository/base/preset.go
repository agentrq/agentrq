// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package base

import (
	"context"
	"errors"

	"github.com/agentrq/agentrq/backend/internal/data/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ErrPresetExists is a preset saved under a name the workspace already has.
var ErrPresetExists = errors.New("preset exists")

// ListPresets returns a workspace's presets, ordered by name.
func (r *repository) ListPresets(ctx context.Context, userID, workspaceID int64) ([]model.Preset, error) {
	presets := []model.Preset{}
	err := r.conn(ctx).
		Where("user_id = ? AND workspace_id = ?", userID, workspaceID).
		Order("name asc").
		Find(&presets).Error
	return presets, err
}

// CreatePreset stores p, or returns ErrPresetExists when the workspace has a
// preset by that name. The unique index decides, so two saves at once cannot
// both get in.
func (r *repository) CreatePreset(ctx context.Context, p model.Preset) (model.Preset, error) {
	res := r.conn(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&p)
	if res.Error != nil {
		return model.Preset{}, res.Error
	}
	if res.RowsAffected == 0 {
		return model.Preset{}, ErrPresetExists
	}
	return p, nil
}

// UpdatePreset gives the workspace's preset called name p's name and body,
// and returns it as stored. It returns ErrNotFound when the workspace has no
// preset by that name, and ErrPresetExists when the new name is taken; the
// unique index decides that, as it does for CreatePreset.
func (r *repository) UpdatePreset(ctx context.Context, userID, workspaceID int64, name string, p model.Preset) (model.Preset, error) {
	var saved model.Preset
	err := r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&model.Preset{}).
			Where("user_id = ? AND workspace_id = ? AND name = ?", userID, workspaceID, name).
			Updates(map[string]any{"name": p.Name, "body": p.Body})
		if errors.Is(res.Error, gorm.ErrDuplicatedKey) {
			return ErrPresetExists
		}
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		return tx.Where("user_id = ? AND workspace_id = ? AND name = ?", userID, workspaceID, p.Name).First(&saved).Error
	})
	if err != nil {
		return model.Preset{}, err
	}
	return saved, nil
}

// DeletePreset removes a workspace's preset by name, or returns ErrNotFound
// when it has none by that name.
func (r *repository) DeletePreset(ctx context.Context, userID, workspaceID int64, name string) error {
	res := r.conn(ctx).
		Where("user_id = ? AND workspace_id = ? AND name = ?", userID, workspaceID, name).
		Delete(&model.Preset{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
