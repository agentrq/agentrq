// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package base

import (
	"context"

	"github.com/agentrq/agentrq/backend/internal/data/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// RecordAgentNote files the note a task of userID was just handed to its agent
// with, once per text, and points the task at it.
func (r *repository) RecordAgentNote(ctx context.Context, userID, taskID int64, text string) error {
	note := model.AgentNote{ID: model.AgentNoteID(userID, text), UserID: userID, Text: text}
	return r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&note).Error; err != nil {
			return err
		}
		return tx.Model(&model.Task{}).Where("id = ? AND user_id = ?", taskID, userID).
			UpdateColumn("agent_note_id", note.ID).Error
	})
}

// GetAgentNote returns a note by id, ErrNotFound when there is none.
func (r *repository) GetAgentNote(ctx context.Context, id int64) (model.AgentNote, error) {
	// Find, not Take: Take logs every miss.
	var notes []model.AgentNote
	if err := r.conn(ctx).Where("id = ?", id).Limit(1).Find(&notes).Error; err != nil {
		return model.AgentNote{}, err
	}
	if len(notes) == 0 {
		return model.AgentNote{}, ErrNotFound
	}
	return notes[0], nil
}

// deleteUnusedAgentNotes deletes userID's notes that no task points at any
// more, once a workspace's tasks are gone.
func deleteUnusedAgentNotes(tx *gorm.DB, userID int64) error {
	// Rows from before the column are NULL, which would make NOT IN match
	// nothing: <> 0 leaves them out with the tasks never handed over.
	used := tx.Model(&model.Task{}).Select("agent_note_id").Where("user_id = ? AND agent_note_id <> 0", userID)
	return tx.Where("user_id = ? AND id NOT IN (?)", userID, used).Delete(&model.AgentNote{}).Error
}
