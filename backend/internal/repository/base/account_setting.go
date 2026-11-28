// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package base

import (
	"context"
	"time"

	"github.com/agentrq/agentrq/backend/internal/data/model"
	"gorm.io/gorm/clause"
)

// ListAccountSettings returns the groups of settings the account saved,
// ordered by name.
func (r *repository) ListAccountSettings(ctx context.Context, userID int64) ([]model.AccountSetting, error) {
	settings := []model.AccountSetting{}
	err := r.conn(ctx).Where("user_id = ?", userID).Order("name asc").Find(&settings).Error
	return settings, err
}

// SaveAccountSetting stores s as the account's group of settings by its
// name, replacing the one it had.
func (r *repository) SaveAccountSetting(ctx context.Context, s model.AccountSetting) error {
	s.UpdatedAt = time.Now()
	return r.conn(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}, {Name: "name"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "updated_at"}),
	}).Create(&s).Error
}
