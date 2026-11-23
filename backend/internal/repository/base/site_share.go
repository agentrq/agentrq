// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package base

import (
	"context"
	"encoding/json"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/agentrq/agentrq/backend/internal/data/model"
)

// UpsertSiteShare stores the share of s.Origin for s.UserID, replacing any
// earlier one. The always-allowed tools survive only while the workspace stays
// the same: approvals given for one workspace do not carry to another.
func (r *repository) UpsertSiteShare(ctx context.Context, s model.SiteShare) (model.SiteShare, error) {
	err := r.conn(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "user_id"}, {Name: "origin"}},
		DoUpdates: append(clause.AssignmentColumns([]string{"workspace_id", "browser_id", "instance_id", "last_url", "tools", "updated_at"}),
			clause.Assignment{Column: clause.Column{Name: "always_allow"}, Value: gorm.Expr(
				"CASE WHEN site_shares.workspace_id = excluded.workspace_id THEN site_shares.always_allow ELSE '[]' END")}),
	}).Create(&s).Error
	if err != nil {
		return model.SiteShare{}, err
	}
	var got model.SiteShare
	err = r.conn(ctx).Where("user_id = ? AND origin = ?", s.UserID, s.Origin).First(&got).Error
	return got, err
}

// DeleteSiteShare removes the user's share of origin, and reports whether
// there was one.
func (r *repository) DeleteSiteShare(ctx context.Context, userID int64, origin string) (bool, error) {
	res := r.conn(ctx).Where("user_id = ? AND origin = ?", userID, origin).Delete(&model.SiteShare{})
	return res.RowsAffected > 0, res.Error
}

// ListSiteSharesForWorkspace returns the sites shared into a workspace, by origin.
func (r *repository) ListSiteSharesForWorkspace(ctx context.Context, workspaceID, userID int64) ([]model.SiteShare, error) {
	var shares []model.SiteShare
	err := r.conn(ctx).Where("workspace_id = ? AND user_id = ?", workspaceID, userID).
		Order("origin asc").Find(&shares).Error
	return shares, err
}

// ListSiteSharesForUser returns every site the user has shared, by origin.
func (r *repository) ListSiteSharesForUser(ctx context.Context, userID int64) ([]model.SiteShare, error) {
	var shares []model.SiteShare
	err := r.conn(ctx).Where("user_id = ?", userID).Order("origin asc").Find(&shares).Error
	return shares, err
}

// GetSiteShare returns the share of origin in a workspace, or
// gorm.ErrRecordNotFound.
func (r *repository) GetSiteShare(ctx context.Context, workspaceID, userID int64, origin string) (model.SiteShare, error) {
	var s model.SiteShare
	err := r.conn(ctx).Where("workspace_id = ? AND user_id = ? AND origin = ?", workspaceID, userID, origin).
		First(&s).Error
	return s, err
}

// SetSiteShareAlwaysAllow replaces the tools a share may call without asking.
func (r *repository) SetSiteShareAlwaysAllow(ctx context.Context, id int64, names []string) error {
	if names == nil {
		names = []string{}
	}
	b, _ := json.Marshal(names)
	return r.conn(ctx).Model(&model.SiteShare{}).Where("id = ?", id).Update("always_allow", string(b)).Error
}
