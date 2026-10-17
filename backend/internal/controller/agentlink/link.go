// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package agentlink

import "context"

// Link is an agent attached to this workspace by a connection this
// process holds natively — a live ACP connection — rather than by an MCP
// session. The workspace server treats it as one more session: it is handed
// pushes, stops, verdicts, model and concurrency changes, and whatever it
// reports is keyed by ID exactly as an MCP session's reports are.
//
// Every method must return promptly. They are called from request handlers and
// from the poller, never from the link's own goroutines.
type Link interface {
	// ID keys the link's per-session state. It never collides with an MCP
	// session id: links are "acp1:<connection>" or "acp2:<connection>".
	ID() string
	// DeliverTask hands the agent a message for a task, with the files it
	// carries.
	DeliverTask(ctx context.Context, taskID int64, content string, attachments ...Attachment)
	// CancelTask stops the task's turn, reporting whether the link had one
	// running or waiting.
	CancelTask(ctx context.Context, taskID int64) bool
	// DeliverVerdict answers a permission request the link raised.
	DeliverVerdict(ctx context.Context, requestID, behavior string) bool
	// SetModel switches the model of one of the link's ACP sessions.
	SetModel(ctx context.Context, acpSessionID, configID, modelID string) bool
	// SetEffort switches the reasoning level of one of the link's ACP sessions.
	SetEffort(ctx context.Context, acpSessionID, configID, effort string) bool
	// SetConcurrency changes how many tasks the link runs at once.
	SetConcurrency(ctx context.Context, limit int) bool
	// Dispatch says whether the link already holds a session for the task,
	// and how many more turns it could start right now.
	Dispatch(taskID int64) (owns bool, free int)
}

// Attachment is a file a message carries, named by the public link anyone can
// read it at: the agent fetches it, so the file itself never travels with the
// push.
type Attachment struct {
	Name     string
	MimeType string
	URL      string
}
