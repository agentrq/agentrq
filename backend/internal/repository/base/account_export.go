// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package base

import (
	"context"
	"time"

	"github.com/agentrq/agentrq/backend/internal/data/model"
)

// The reads an account's data export is made of: everything the account
// owns, in an order that does not change between two exports.

// ExportPresets returns every preset of the account, its own and its
// workspaces'.
func (r *repository) ExportPresets(ctx context.Context, userID int64) ([]model.Preset, error) {
	presets := []model.Preset{}
	err := r.conn(ctx).Where("user_id = ?", userID).Order("workspace_id asc, name asc").Find(&presets).Error
	return presets, err
}

// ExportMemories returns every memory of the account's workspaces.
func (r *repository) ExportMemories(ctx context.Context, userID int64) ([]model.Memory, error) {
	memories := []model.Memory{}
	err := r.conn(ctx).Where("user_id = ?", userID).Order("workspace_id asc, name asc").Find(&memories).Error
	return memories, err
}

// ExportPanoDocs returns the account's Pano docs that have not expired by
// now, with their markdown.
func (r *repository) ExportPanoDocs(ctx context.Context, userID int64, now time.Time) ([]model.PanoDoc, error) {
	docs := []model.PanoDoc{}
	err := r.conn(ctx).Where("user_id = ? AND expires_at > ?", userID, now).Order("id asc").Find(&docs).Error
	return docs, err
}

// ExportTasks returns up to limit of the account's tasks after afterID, by
// id, so a caller reads them all a page at a time and none twice.
func (r *repository) ExportTasks(ctx context.Context, userID, afterID int64, limit int) ([]model.Task, error) {
	tasks := []model.Task{}
	err := r.conn(ctx).Where("user_id = ? AND id > ?", userID, afterID).Order("id asc").Limit(limit).Find(&tasks).Error
	return tasks, err
}

// ExportMessages returns the messages of the given tasks, oldest first within
// each task. The caller has read the tasks as the account's own.
func (r *repository) ExportMessages(ctx context.Context, taskIDs []int64) ([]model.Message, error) {
	messages := []model.Message{}
	if len(taskIDs) == 0 {
		return messages, nil
	}
	err := r.conn(ctx).Where("task_id IN ?", taskIDs).Order("task_id asc, created_at asc, id asc").Find(&messages).Error
	return messages, err
}
