// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package crud

type (
	// SendA2uiActionRequest is a click on a Button of an agent's card.
	SendA2uiActionRequest struct {
		WorkspaceID int64
		TaskID      int64
		UserID      string
		// MessageID is the agent's message that carries the card.
		MessageID         int64
		SurfaceID         string
		Name              string
		SourceComponentID string
		Context           map[string]any
		// UserMessage is the reply's text as the page resolved it; empty
		// falls back to the Button's own.
		UserMessage string
	}

	// SendA2uiActionResponse is the task with the click posted as the
	// person's reply.
	SendA2uiActionResponse struct {
		Task      Task
		MessageID int64
		// Text is the reply's text, which the agent is told.
		Text string
		// Reopened says the reply put a completed task back to ongoing.
		Reopened bool
	}
)

// A2uiActionError is a click the server refuses, with the sentence to show
// its caller.
type A2uiActionError struct {
	Message string
	// Conflict is a card that was already answered.
	Conflict bool
}

func (e *A2uiActionError) Error() string { return e.Message }
