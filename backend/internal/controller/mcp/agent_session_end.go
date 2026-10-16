// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"context"
	"time"

	zlog "github.com/rs/zerolog/log"

	"github.com/agentrq/agentrq/backend/internal/controller/agentlink"
)

// MessageMetadataSessionEnded marks a usage message whose session has closed
// its stream. Its counters describe an agent that is no longer attached, so the
// composer stops drawing them.
const MessageMetadataSessionEnded = "sessionEnded"

// sessionEndedUpdateTimeout bounds the message writes made after a stream has
// closed, when no request is left to bound them.
const sessionEndedUpdateTimeout = 10 * time.Second

// usageMessageRef is a usage message one session wrote.
type usageMessageRef struct {
	taskID int64
	msgID  int64
}

// rememberUsageMessage records that a session wrote, or revised, a task's
// usage message.
func (ps *WorkspaceServer) rememberUsageMessage(sessionID string, taskID, msgID int64) {
	ps.agentUsageMessagesMu.Lock()
	defer ps.agentUsageMessagesMu.Unlock()
	if ps.agentUsageMessages == nil {
		ps.agentUsageMessages = make(map[string]map[string]usageMessageRef)
	}
	byKey := ps.agentUsageMessages[sessionID]
	if byKey == nil {
		byKey = make(map[string]usageMessageRef)
		ps.agentUsageMessages[sessionID] = byKey
	}
	byKey[agentTelemetryMessageKey(taskID, agentTelemetryKindUsage, "")] = usageMessageRef{taskID: taskID, msgID: msgID}
}

// agentSessionEnded tells the open pages that what a session reported no
// longer holds, once its last stream has closed.
//
// The REST payload already filters to streaming sessions, but a page that is
// open only hears events, and nothing else says that the gateway's limit,
// models and commands left with it. Without this, switching a workspace from
// acp-gateway to Claude Code left the gateway's concurrency control and context
// ring on screen for an agent that has neither.
func (ps *WorkspaceServer) agentSessionEnded(ctx context.Context, sessionID string) {
	ps.agentModelsMu.RLock()
	hadModels := len(ps.agentModels[sessionID].Models) > 0
	ps.agentModelsMu.RUnlock()
	if hadModels {
		ps.publishAgentModels(agentlink.ModelsSnapshot{})
	}

	ps.agentEffortMu.RLock()
	hadEffort := len(ps.agentEffort[sessionID].Efforts) > 0
	ps.agentEffortMu.RUnlock()
	if hadEffort {
		ps.publishAgentEffort(agentlink.EffortSnapshot{})
	}

	ps.agentCommandsMu.RLock()
	hadCommands := len(ps.agentCommands[sessionID].Commands) > 0
	ps.agentCommandsMu.RUnlock()
	if hadCommands {
		ps.publishAgentCommands(agentlink.CommandsSnapshot{})
	}

	ps.agentConcurrencyMu.RLock()
	hadConcurrency := ps.agentConcurrency[sessionID].Reported()
	ps.agentConcurrencyMu.RUnlock()
	if hadConcurrency {
		ps.publishAgentConcurrency(agentlink.ConcurrencySnapshot{})
	}

	ps.endUsageMessages(ctx, sessionID)
}

// endUsageMessages marks the session's usage messages as ended, and lets go of
// them so the next agent on the task starts a message of its own rather than
// revising one whose metadata says it has ended.
func (ps *WorkspaceServer) endUsageMessages(ctx context.Context, sessionID string) {
	ps.agentUsageMessagesMu.Lock()
	refs := ps.agentUsageMessages[sessionID]
	delete(ps.agentUsageMessages, sessionID)
	// A message another session has also written is still that session's, and
	// stays live with it.
	for key, mine := range refs {
		for _, other := range ps.agentUsageMessages {
			if other[key] == mine {
				delete(refs, key)
				break
			}
		}
	}
	ps.agentUsageMessagesMu.Unlock()
	if len(refs) == 0 {
		return
	}

	ps.agentTelemetryMessagesMu.Lock()
	for key, ref := range refs {
		if ps.agentTelemetryMessages[key] == ref.msgID {
			delete(ps.agentTelemetryMessages, key)
		}
	}
	ps.agentTelemetryMessagesMu.Unlock()

	if ps.updateMessageMetadata == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, sessionEndedUpdateTimeout)
	defer cancel()
	for _, ref := range refs {
		if err := ps.updateMessageMetadata(ctx, ref.taskID, ref.msgID, map[string]any{MessageMetadataSessionEnded: true}); err != nil {
			zlog.Warn().Err(err).Int64("task_id", ref.taskID).Int64("message_id", ref.msgID).
				Msg("could not mark the usage message of an ended session")
		}
	}
}
