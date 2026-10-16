// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/agentrq/agentrq/backend/internal/data/model"
	mapper "github.com/agentrq/agentrq/backend/internal/mapper/api"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
	"github.com/mustafaturan/monoflake"
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

// create records a tool call on a task of this workspace, in one statement.
func (r toolCallRecorder) create(ctx context.Context, tc model.ToolCall) (model.ToolCall, error) {
	tc.WorkspaceID = r.workspaceID
	created, err := r.repo.CreateToolCall(ctx, tc, monoflake.IDFromBase62(r.owner).Int64())
	if errors.Is(err, base.ErrNotFound) {
		return model.ToolCall{}, fmt.Errorf("task %s is not in this workspace", monoflake.ID(tc.TaskID).String())
	}
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
