// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package base

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/agentrq/agentrq/backend/internal/data/model"
)

// CreateHook stores a hook whose module is already in storage, and reports
// false, storing nothing, when the account already has a hook of that name.
func (r *repository) CreateHook(ctx context.Context, h model.Hook) (bool, error) {
	res := r.conn(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&h)
	return res.RowsAffected > 0, res.Error
}

// GetHook returns one of an account's hooks, or ErrNotFound. UserID 0 is a
// built-in.
func (r *repository) GetHook(ctx context.Context, id, userID int64) (model.Hook, error) {
	var h model.Hook
	err := r.conn(ctx).Where("id = ? AND user_id = ?", id, userID).First(&h).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.Hook{}, ErrNotFound
	}
	return h, err
}

// ListHooks returns an account's hooks by name.
func (r *repository) ListHooks(ctx context.Context, userID int64) ([]model.Hook, error) {
	var hooks []model.Hook
	err := r.conn(ctx).Where("user_id = ?", userID).Order("name asc").Find(&hooks).Error
	return hooks, err
}

// ReplaceHookModule points a hook at a new module, already in storage, and
// returns ErrNotFound when the hook is gone.
func (r *repository) ReplaceHookModule(ctx context.Context, h model.Hook) error {
	res := r.conn(ctx).Model(&model.Hook{}).Where("id = ? AND user_id = ?", h.ID, h.UserID).
		Updates(map[string]any{
			"version": h.Version, "blob_key": h.BlobKey, "sha256": h.SHA256, "size_bytes": h.SizeBytes,
			"abi_version": h.ABIVersion, "manifest": h.Manifest,
		})
	if res.Error == nil && res.RowsAffected == 0 {
		return ErrNotFound
	}
	return res.Error
}

// SetHookEnabled turns a hook on or off, and returns ErrNotFound when there is
// no such hook.
func (r *repository) SetHookEnabled(ctx context.Context, id, userID int64, enabled bool) error {
	res := r.conn(ctx).Model(&model.Hook{}).Where("id = ? AND user_id = ?", id, userID).
		Update("enabled", enabled)
	if res.Error == nil && res.RowsAffected == 0 {
		return ErrNotFound
	}
	return res.Error
}

// DeleteHook removes a hook nothing registers, and reports false, removing
// nothing, while something does. The check is in the DELETE itself, so a
// registration made meanwhile cannot be left pointing at no hook.
func (r *repository) DeleteHook(ctx context.Context, id, userID int64) (bool, error) {
	registered := r.conn(ctx).Model(&model.HookRegistration{}).Select("1").Where("hook_id = ?", id)
	res := r.conn(ctx).Where("id = ? AND user_id = ? AND NOT EXISTS (?)", id, userID, registered).
		Delete(&model.Hook{})
	return res.RowsAffected > 0, res.Error
}

// ListHookRegistrationRoutes returns the routes of every registration of a
// hook, in any account: what an upgrade of the hook must keep working.
func (r *repository) ListHookRegistrationRoutes(ctx context.Context, hookID int64) ([]string, error) {
	var routes []string
	err := r.conn(ctx).Model(&model.HookRegistration{}).Where("hook_id = ?", hookID).
		Order("id asc").Pluck("routes", &routes).Error
	return routes, err
}
