// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package base

import (
	"context"
	"errors"

	"github.com/agentrq/agentrq/backend/internal/data/model"
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
