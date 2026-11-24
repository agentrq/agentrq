// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package base

import (
	"context"

	"github.com/agentrq/agentrq/backend/internal/data/model"
	"gorm.io/gorm"
)

// ListGitHubInstallations returns the GitHub App installations an account
// reaches, ordered by the GitHub account they are on.
func (r *repository) ListGitHubInstallations(ctx context.Context, userID int64) ([]model.GitHubInstallation, error) {
	out := []model.GitHubInstallation{}
	err := r.conn(ctx).Where("user_id = ?", userID).Order("account asc, installation_id asc").Find(&out).Error
	return out, err
}

// ReplaceGitHubInstallations makes rows the account's installations, all of
// them: one GitHub no longer lists for the person is dropped.
func (r *repository) ReplaceGitHubInstallations(ctx context.Context, userID int64, rows []model.GitHubInstallation) error {
	return r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ?", userID).Delete(&model.GitHubInstallation{}).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		return tx.Create(&rows).Error
	})
}

// DeleteGitHubInstallation removes one installation from an account, or
// returns ErrNotFound when the account does not have it.
func (r *repository) DeleteGitHubInstallation(ctx context.Context, userID, installationID int64) error {
	res := r.conn(ctx).Where("user_id = ? AND installation_id = ?", userID, installationID).Delete(&model.GitHubInstallation{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
