// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package base

import (
	"context"

	"github.com/agentrq/agentrq/backend/internal/data/model"
	"gorm.io/gorm/clause"
)

// GetGitLabConnection returns the account's GitLab connection, or
// ErrNotFound when it has none.
func (r *repository) GetGitLabConnection(ctx context.Context, userID int64) (model.GitLabConnection, error) {
	var out model.GitLabConnection
	res := r.conn(ctx).Where("user_id = ?", userID).Limit(1).Find(&out)
	if res.Error != nil {
		return out, res.Error
	}
	if res.RowsAffected == 0 {
		return out, ErrNotFound
	}
	return out, nil
}

// SaveGitLabConnection makes row the account's connection, in place of any
// it had.
func (r *repository) SaveGitLabConnection(ctx context.Context, row model.GitLabConnection) error {
	return r.conn(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"updated_at", "host", "username", "access_token", "access_nonce", "refresh_token", "refresh_nonce", "expires_at"}),
	}).Create(&row).Error
}

// RenewGitLabConnection stores a renewed token, but only over the refresh
// token it was renewed from: false when another renewal already replaced it.
func (r *repository) RenewGitLabConnection(ctx context.Context, row model.GitLabConnection, oldRefresh string) (bool, error) {
	res := r.conn(ctx).Model(&model.GitLabConnection{}).Where("user_id = ? AND refresh_token = ?", row.UserID, oldRefresh).Updates(map[string]any{
		"updated_at": row.UpdatedAt, "access_token": row.AccessToken, "access_nonce": row.AccessNonce,
		"refresh_token": row.RefreshToken, "refresh_nonce": row.RefreshNonce, "expires_at": row.ExpiresAt,
	})
	return res.RowsAffected == 1, res.Error
}

// DeleteGitLabConnection forgets the account's connection, or returns
// ErrNotFound when it has none.
func (r *repository) DeleteGitLabConnection(ctx context.Context, userID int64) error {
	res := r.conn(ctx).Where("user_id = ?", userID).Delete(&model.GitLabConnection{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
