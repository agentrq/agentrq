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

// CreateHookRegistration stores a registration.
func (r *repository) CreateHookRegistration(ctx context.Context, reg model.HookRegistration) error {
	return r.conn(ctx).Create(&reg).Error
}

// GetHookRegistration returns one of an account's registrations, or
// ErrNotFound.
func (r *repository) GetHookRegistration(ctx context.Context, id, userID int64) (model.HookRegistration, error) {
	var reg model.HookRegistration
	err := r.conn(ctx).Where("id = ? AND user_id = ?", id, userID).First(&reg).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.HookRegistration{}, ErrNotFound
	}
	return reg, err
}

// ListHookRegistrations returns an account's registrations, newest first.
func (r *repository) ListHookRegistrations(ctx context.Context, userID int64) ([]model.HookRegistration, error) {
	var regs []model.HookRegistration
	err := r.conn(ctx).Where("user_id = ?", userID).Order("id desc").Find(&regs).Error
	return regs, err
}

// UpdateHookRegistration saves a registration's name, switch, inputs and
// routes, and returns ErrNotFound when it is gone. The token is changed only
// by SetHookRegistrationTokenHash.
func (r *repository) UpdateHookRegistration(ctx context.Context, reg model.HookRegistration) error {
	res := r.conn(ctx).Model(&model.HookRegistration{}).Where("id = ? AND user_id = ?", reg.ID, reg.UserID).
		Updates(map[string]any{"name": reg.Name, "enabled": reg.Enabled, "inputs": reg.Inputs, "routes": reg.Routes})
	if res.Error == nil && res.RowsAffected == 0 {
		return ErrNotFound
	}
	return res.Error
}

// SetHookRegistrationTokenHash replaces a registration's token, and returns
// ErrNotFound when it is gone.
func (r *repository) SetHookRegistrationTokenHash(ctx context.Context, id, userID int64, hash string) error {
	res := r.conn(ctx).Model(&model.HookRegistration{}).Where("id = ? AND user_id = ?", id, userID).
		Update("token_hash", hash)
	if res.Error == nil && res.RowsAffected == 0 {
		return ErrNotFound
	}
	return res.Error
}

// DeleteHookRegistration removes a registration with its deliveries and
// dedup keys, and returns ErrNotFound when there is none.
func (r *repository) DeleteHookRegistration(ctx context.Context, id, userID int64) error {
	return r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Where("id = ? AND user_id = ?", id, userID).Delete(&model.HookRegistration{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		if err := tx.Where("registration_id = ?", id).Delete(&model.HookDelivery{}).Error; err != nil {
			return err
		}
		return tx.Where("registration_id = ?", id).Delete(&model.HookDeliveryKey{}).Error
	})
}
