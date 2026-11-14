// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package crud

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/mustafaturan/monoflake"
	"gorm.io/datatypes"
)

// followUpMetadata marks a message as a follow-up. Not a "type": a typed
// message leaves the conversation the supervisor and the page read.
var followUpMetadata = datatypes.JSON(`{"followUp":true}`)

// Why FollowUpTask refuses.
var (
	errFollowUpText     = errors.New("text is required")
	errFollowUpAssignee = errors.New("the task is assigned to the human: a follow-up is for the agent's tasks; reply instead")
	errFollowUpCron     = errors.New("the task is a schedule: follow up one of its runs instead")
	errRateLimited      = errors.New("rate limit exceeded")
)

// IsFollowUp says whether a message is a follow-up FollowUpTask posted.
func IsFollowUp(m model.Message) bool {
	var meta struct {
		FollowUp bool `json:"followUp"`
	}
	return len(m.Metadata) > 0 && json.Unmarshal(m.Metadata, &meta) == nil && meta.FollowUp
}

// FollowUpTask posts text to an agent's task as a follow-up and hands the
// task back to the agent: notstarted, so the poller offers it again with the
// follow-up before its body, unless the agent is on it, ongoing.
func (c *controller) FollowUpTask(ctx context.Context, req entity.FollowUpTaskRequest) (*entity.FollowUpTaskResponse, error) {
	if strings.TrimSpace(req.Text) == "" {
		return nil, errFollowUpText
	}
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	if c.limiter != nil && !c.limiter.AllowMessage(uid) {
		return nil, errRateLimited
	}
	if _, err := c.ensureActiveWorkspace(ctx, req.WorkspaceID, req.UserID); err != nil {
		return nil, err
	}
	m, err := c.repository.GetTaskRow(ctx, req.WorkspaceID, req.TaskID, uid)
	if err != nil {
		return nil, err
	}
	if m.Assignee != "agent" {
		return nil, errFollowUpAssignee
	}
	if m.Status == "cron" {
		return nil, errFollowUpCron
	}
	if ctx, _, err = c.asSupervisor(ctx, req.BySupervisor, uid); err != nil {
		return nil, err
	}

	msg := model.Message{
		ID:        c.idgen.NextID(),
		CreatedAt: time.Now(),
		TaskID:    m.ID,
		UserID:    uid,
		Sender:    "human",
		Text:      req.Text,
		Metadata:  followUpMetadata,
	}
	if err := c.repository.CreateMessage(ctx, msg); err != nil {
		return nil, err
	}
	c.emitEvent(ctx, entity.CRUDEvent{
		Action:       entity.ActionMessageCreate,
		WorkspaceID:  req.WorkspaceID,
		UserID:       uid,
		ResourceType: entity.ResourceMessage,
		ResourceID:   msg.ID,
		Actor:        entity.ActorHuman,
	})

	if m.Status != "ongoing" {
		m.Status = "notstarted"
	}
	m.UpdatedAt = time.Now()
	updated, err := c.repository.UpdateTask(ctx, m, "Status")
	if err != nil {
		return nil, err
	}
	c.emitEvent(ctx, entity.CRUDEvent{
		Action:       entity.ActionTaskUpdate,
		WorkspaceID:  updated.WorkspaceID,
		UserID:       updated.UserID,
		ResourceType: entity.ResourceTask,
		ResourceID:   updated.ID,
		Actor:        entity.ActorHuman,
	})
	return &entity.FollowUpTaskResponse{Task: c.fromModelTaskToEntity(updated), MessageID: msg.ID}, nil
}
