// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package mcp

import (
	"context"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
)

// WorkspaceNotesFunc reads the workspace's standing notes for its agent, as
// they are now.
type WorkspaceNotesFunc func(ctx context.Context) string

// AgentNoteRecorder keeps the note a task was handed to its agent with, so the
// task page shows what was sent. recorded is the task's AgentNoteID as read;
// a recorder writes nothing when it already names this note.
type AgentNoteRecorder func(ctx context.Context, taskID, recorded int64, note string)

// SetWorkspaceNotes gives the server where to read the workspace's notes, and
// where to record the ones a task is handed with. Called before the server is
// handed out, never after.
func (ps *WorkspaceServer) SetWorkspaceNotes(fn WorkspaceNotesFunc, record AgentNoteRecorder) {
	ps.workspaceNotes = fn
	ps.recordAgentNote = record
}

// AgentTaskBody is a task's body as the agent is handed it, with the
// workspace's notes after it. They are added here, at the hand-off, and never
// stored with the task; which notes it was handed is recorded instead.
// agentNoteID is the task's AgentNoteID as read.
func (ps *WorkspaceServer) AgentTaskBody(ctx context.Context, taskID, agentNoteID int64, body string) string {
	if ps.workspaceNotes == nil {
		return body
	}
	note := ps.workspaceNotes(ctx)
	if ps.recordAgentNote != nil {
		ps.recordAgentNote(ctx, taskID, agentNoteID, note)
	}
	return crud.AppendSelfLearningNote(body, note)
}

// getTaskBody is the body task get answers: with the notes while the task is
// still being handed over, and without them once the agent has taken it, so a
// re-read does not repeat them.
func (ps *WorkspaceServer) getTaskBody(ctx context.Context, t model.Task) string {
	if t.Status == "notstarted" {
		return ps.AgentTaskBody(ctx, t.ID, t.AgentNoteID, t.Body)
	}
	return t.Body
}
