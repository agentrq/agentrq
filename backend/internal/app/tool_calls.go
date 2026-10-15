// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"context"

	"github.com/agentrq/agentrq/backend/internal/data/model"
	mapper "github.com/agentrq/agentrq/backend/internal/mapper/api"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
)

// toolCallUpdatedEvent carries one tool call that was made or answered.
const toolCallUpdatedEvent = "toolCall.updated"

// toolCallRecorder stores the tool calls a workspace's agent makes.
type toolCallRecorder struct {
	repo        base.Repository
	bus         *eventbus.Bus
	workspaceID int64
	owner       string
}

// create records a tool call on a task of this workspace.
func (r toolCallRecorder) create(ctx context.Context, tc model.ToolCall) (model.ToolCall, error) {
	if _, err := workspaceTask(ctx, r.repo, r.workspaceID, r.owner, tc.TaskID); err != nil {
		return model.ToolCall{}, err
	}
	created, err := r.repo.CreateToolCall(ctx, tc)
	if err == nil {
		r.publish(created)
	}
	return created, err
}

// answer records the permission decision on a tool call.
func (r toolCallRecorder) answer(ctx context.Context, id int64, status string) error {
	updated, err := r.repo.UpdateToolCallStatus(ctx, id, status)
	if err != nil {
		return err
	}
	r.publish(updated)
	return nil
}

// publish puts one tool call on the stream, never its whole task: a long run
// makes thousands, and resending all of them on each one grew as N² (#921).
func (r toolCallRecorder) publish(tc model.ToolCall) {
	r.bus.Publish(r.workspaceID, r.owner, eventbus.Event{
		Type:    toolCallUpdatedEvent,
		Payload: mapper.FromModelToolCallToUpdatedView(r.workspaceID, tc),
	})
}
