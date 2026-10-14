// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package acpv1

import (
	"context"

	mcpctrl "github.com/agentrq/agentrq/backend/internal/controller/mcp"
)

// link is the engine as the workspace server sees it. Every method returns
// promptly: whatever waits on the agent runs on a goroutine of its own.
type link struct{ e *engineV1 }

var _ mcpctrl.AgentLink = (*link)(nil)

func (l *link) ID() string { return l.e.linkID }

func (l *link) DeliverTask(_ context.Context, taskID int64, content string) {
	l.e.q.deliver(taskID, content)
}

// CancelTask is Stop: anything queued for the task is skipped, and a running
// turn is told to stop, which ends it with the cancelled note.
func (l *link) CancelTask(_ context.Context, taskID int64) bool {
	had := l.e.q.cancel(taskID)
	if s := l.e.busySession(taskID); s != nil {
		_ = l.e.peer.notify(methodSessionCancel, sessionIDParams{SessionID: s.id})
		l.e.settlePermissions(s.id)
		had = true
	}
	return had
}

func (l *link) DeliverVerdict(_ context.Context, requestID, behavior string) bool {
	return l.e.verdict(requestID, behavior)
}

func (l *link) SetModel(_ context.Context, acpSessionID, _ /* configID: the session knows its own */, modelID string) bool {
	return l.e.setModel(acpSessionID, modelID)
}

func (l *link) SetConcurrency(_ context.Context, limit int) bool {
	l.e.q.setLimit(limit)
	return true
}

func (l *link) Dispatch(taskID int64) (bool, int) {
	if l.e.q.owns(taskID) || l.e.ownsTask(taskID) {
		return true, 0
	}
	limit, active, queued := l.e.q.numbers()
	return false, max(limit-active-queued, 0)
}
