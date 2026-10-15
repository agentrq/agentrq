// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"context"
	"errors"
	"fmt"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
	"github.com/mustafaturan/monoflake"
)

// workspaceTask reads a task an agent named, and refuses one outside the
// workspace its MCP server serves. Every write an agent makes against a task
// passes here first: the database only checks that the task exists, so without
// it one workspace's token could post into any task, in any workspace.
//
// The row only: it runs on every tool call, and a long run's task holds
// thousands of messages and tool calls (#921).
func workspaceTask(ctx context.Context, repo base.Repository, workspaceID int64, owner string, taskID int64) (model.Task, error) {
	t, err := repo.SystemGetTask(ctx, taskID)
	if err == nil && (t.WorkspaceID != workspaceID || t.UserID != monoflake.IDFromBase62(owner).Int64()) {
		err = base.ErrNotFound
	}
	if errors.Is(err, base.ErrNotFound) {
		return model.Task{}, fmt.Errorf("task %s is not in this workspace", monoflake.ID(taskID).String())
	}
	return t, err
}

// setAgentTaskStatus is an agent's status change. The row only, like
// workspaceTask: a save never needs the history (#921).
//
// No "Status updated to" message in the thread: the change is recorded as a
// state transition and shown in the task's timeline.
func setAgentTaskStatus(ctx context.Context, repo base.Repository, ps pubsub.Service, workspaceID int64, owner string, taskID int64, status string) (model.Task, error) {
	uid := monoflake.IDFromBase62(owner).Int64()
	m, err := repo.GetTaskRow(ctx, workspaceID, taskID, uid)
	if err != nil {
		return model.Task{}, err
	}
	if m.Status == status {
		return m, nil
	}
	m.Status = status

	updated, err := repo.UpdateTask(ctx, m, "Status")
	if err != nil {
		return updated, err
	}
	publish := func(action entity.Action) {
		ps.Publish(context.Background(), pubsub.PublishRequest{
			PubSubID: entity.PubSubTopicCRUD,
			Event: entity.CRUDEvent{
				Action:       action,
				WorkspaceID:  workspaceID,
				UserID:       uid,
				ResourceType: entity.ResourceTask,
				ResourceID:   updated.ID,
				Actor:        entity.ActorAgent,
				Origin:       entity.OriginMCP,
			},
		})
	}
	if updated.Status == "completed" || updated.Status == "done" {
		publish(entity.ActionTaskComplete)
	}
	publish(entity.ActionTaskUpdate)
	return updated, nil
}
