// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"context"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
)

// WorkspaceNotesFunc reads the workspace's standing notes for its agent, as
// they are now.
type WorkspaceNotesFunc func(ctx context.Context) string

// SetWorkspaceNotes gives the server where to read the workspace's notes.
// Called before the server is handed out, never after.
func (ps *WorkspaceServer) SetWorkspaceNotes(fn WorkspaceNotesFunc) {
	ps.workspaceNotes = fn
}

// AgentTaskBody is a task's body as the agent is handed it, with the
// workspace's notes after it. They are added here, at the hand-off, and never
// stored with the task.
func (ps *WorkspaceServer) AgentTaskBody(ctx context.Context, body string) string {
	if ps.workspaceNotes == nil {
		return body
	}
	return crud.AppendSelfLearningNote(body, ps.workspaceNotes(ctx))
}

// getTaskBody is the body task get answers: with the notes while the task is
// still being handed over, and without them once the agent has taken it, so a
// re-read does not repeat them.
func (ps *WorkspaceServer) getTaskBody(ctx context.Context, t model.Task) string {
	if t.Status == "notstarted" {
		return ps.AgentTaskBody(ctx, t.Body)
	}
	return t.Body
}
