// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/agentrq/agentrq/backend/internal/data/model"
	mapper "github.com/agentrq/agentrq/backend/internal/mapper/api"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
	"github.com/mustafaturan/monoflake"
)

// taskPlanUpdatedEvent carries a task's plan, written, replaced or withdrawn.
const taskPlanUpdatedEvent = "taskPlan.updated"

// taskPlanStore keeps the plans a workspace's agent works to, one per task.
type taskPlanStore struct {
	repo        base.Repository
	bus         *eventbus.Bus
	workspaceID int64
	owner       string
}

// Save writes or replaces the plan of a task of this workspace, in one statement.
func (s taskPlanStore) Save(ctx context.Context, taskID int64, content string) error {
	p, err := s.repo.SaveTaskPlan(ctx, model.TaskPlan{ID: taskID, UpdatedAt: time.Now().UTC(), Content: content}, s.workspaceID, monoflake.IDFromBase62(s.owner).Int64())
	if errors.Is(err, base.ErrNotFound) {
		return fmt.Errorf("task %s is not in this workspace", monoflake.ID(taskID).String())
	}
	if err != nil {
		return err
	}
	s.publish(taskID, &p)
	return nil
}

// Delete removes the plan of a task of this workspace.
func (s taskPlanStore) Delete(ctx context.Context, taskID int64) error {
	if err := s.repo.DeleteTaskPlan(ctx, taskID, s.workspaceID, monoflake.IDFromBase62(s.owner).Int64()); err != nil {
		return err
	}
	s.publish(taskID, nil)
	return nil
}

// publish puts the plan on the stream, or its withdrawal when p is nil.
func (s taskPlanStore) publish(taskID int64, p *model.TaskPlan) {
	s.bus.Publish(s.workspaceID, s.owner, eventbus.Event{
		Type:    taskPlanUpdatedEvent,
		Payload: mapper.FromModelTaskPlanToUpdatedView(s.workspaceID, taskID, p),
	})
}
