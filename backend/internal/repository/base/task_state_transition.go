// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package base

import (
	"context"
	"encoding/json"
	"errors"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ListTaskStateTransitions returns a task's status changes, oldest first.
func (r *repository) ListTaskStateTransitions(ctx context.Context, taskID int64) ([]model.TaskStateTransition, error) {
	var rows []model.TaskStateTransition
	err := r.conn(ctx).
		Where("task_id = ?", taskID).
		Order("created_at asc, id asc").
		Find(&rows).Error
	return rows, err
}

// CreateAgents files agents' names in one statement. A name already filed is
// left as it is: the ID is the name's hash, so the row can only ever say the
// same thing.
func (r *repository) CreateAgents(ctx context.Context, agents []model.Agent) error {
	return r.conn(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&agents).Error
}

// CreateAgentModels files models' names, as CreateAgents files agents'.
func (r *repository) CreateAgentModels(ctx context.Context, models []model.AgentModel) error {
	return r.conn(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&models).Error
}

// ListAgents returns the agents with the given IDs, or every agent for nil.
func (r *repository) ListAgents(ctx context.Context, ids []int64) ([]model.Agent, error) {
	var rows []model.Agent
	err := byIDs(r.conn(ctx), ids).Find(&rows).Error
	return rows, err
}

// ListAgentModels returns the models with the given IDs, or every model for
// nil.
func (r *repository) ListAgentModels(ctx context.Context, ids []int64) ([]model.AgentModel, error) {
	var rows []model.AgentModel
	err := byIDs(r.conn(ctx), ids).Find(&rows).Error
	return rows, err
}

func byIDs(db *gorm.DB, ids []int64) *gorm.DB {
	if ids == nil {
		return db
	}
	return db.Where("id IN ?", ids)
}

// recordTaskStateTransition is the only writer of a task's history. A move
// into a closed state also records the task's latency, here rather than in
// the callers, so no path can close a task without it. The agent that made
// the change, if any, comes from the context, already registered.
func recordTaskStateTransition(tx *gorm.DB, t model.Task, from, to model.TaskState) error {
	agent := entity.GetTaskAgent(tx.Statement.Context)
	if err := tx.Create(&model.TaskStateTransition{
		UserID:       t.UserID,
		WorkspaceID:  t.WorkspaceID,
		TaskID:       t.ID,
		FromState:    from,
		ToState:      to,
		AgentID:      agent.ID,
		AgentModelID: agent.ModelID,
	}).Error; err != nil {
		return err
	}
	if !isClosedTaskState(to) {
		return nil
	}
	return recordTaskLatency(tx, t)
}

// currentTaskState is the state the task's history last moved it to, or, for a
// task with no history yet, the one its status names. The two differ while the
// task needs input, which is a state of the history only.
func currentTaskState(tx *gorm.DB, taskID int64, status string) (model.TaskState, error) {
	var last model.TaskStateTransition
	err := tx.Where("task_id = ?", taskID).Order("created_at desc, id desc").Take(&last).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.TaskStateFromStatus(status), nil
	}
	return last.ToState, err
}

// hasRequestStatus reports whether a message's metadata carries a top-level
// status — the mark of a question to the human (a permission request, an
// elicitation) — so only those writes pay for syncNeedsInput.
func hasRequestStatus(metadata []byte) bool {
	if len(metadata) == 0 {
		return false
	}
	var m struct {
		Status *string `json:"status"`
	}
	return json.Unmarshal(metadata, &m) == nil && m.Status != nil
}

// syncNeedsInput records the task starting or ending a wait on the human: it
// needs input while any message in its thread is a question still pending.
// The task's status is never touched — needing input is a state of the history
// only, so nothing that reads the status behaves differently for it.
func syncNeedsInput(tx *gorm.DB, taskID int64) error {
	var t model.Task
	err := tx.Select("id, user_id, workspace_id, status").Where("id = ?", taskID).Take(&t).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	current, err := currentTaskState(tx, taskID, t.Status)
	if err != nil {
		return err
	}

	var pending int64
	if err := tx.Model(&model.Message{}).
		Where("task_id = ?", taskID).
		Where(pendingRequestClause(tx)).
		Count(&pending).Error; err != nil {
		return err
	}

	switch {
	case pending > 0 && current != model.TaskStateNeedsInput && !isClosedTaskState(current):
		return recordTaskStateTransition(tx, t, current, model.TaskStateNeedsInput)
	case pending == 0 && current == model.TaskStateNeedsInput:
		return recordTaskStateTransition(tx, t, current, model.TaskStateFromStatus(t.Status))
	}
	return nil
}

// pendingRequestClause matches a message whose metadata's top-level status is
// "pending". Top-level on purpose: a plan's entries carry a status of their
// own, and a plan item still to do is not a question to anyone.
func pendingRequestClause(tx *gorm.DB) string {
	if tx.Dialector.Name() == "postgres" {
		return pgPendingRequest
	}
	return sqlitePendingRequest
}

const (
	pgPendingRequest     = "metadata->>'status' = 'pending'"
	sqlitePendingRequest = "json_valid(metadata) AND json_extract(metadata, '$.status') = 'pending'"
)

func isClosedTaskState(s model.TaskState) bool {
	return s == model.TaskStateCompleted || s == model.TaskStateRejected
}
