// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"context"
	"fmt"
	"strings"
	"time"

	zlog "github.com/rs/zerolog/log"

	"github.com/mustafaturan/monoflake"

	"github.com/agentrq/agentrq/backend/internal/controller/agentlink"
)

// The kinds of telemetry a gateway sends.
const (
	agentTelemetryKindThought = "thought"
	agentTelemetryKindPlan    = "plan"
	agentTelemetryKindUsage   = "usage"
	agentTelemetryKindNotice  = "notice"
)

// The message metadata types the frontend renders these as.
const (
	MessageTypeAgentThought = "agent_thought"
	MessageTypeAgentPlan    = "agent_plan"
	MessageTypeAgentUsage   = "agent_usage"
	MessageTypeAgentNotice  = "agent_notice"
)

// agentTelemetryMessageKey identifies the one chat message that stands for a
// plan, or for a task's usage counters — the things that are revised in place
// rather than appended to.
func agentTelemetryMessageKey(taskID int64, kind, planID string) string {
	if kind == agentTelemetryKindPlan {
		return fmt.Sprintf("%d:plan:%s", taskID, planID)
	}
	return fmt.Sprintf("%d:%s", taskID, kind)
}

// HandleAgentTelemetry relays one piece of agent telemetry into a task's chat.
//
// Reasoning and notices are appended: each says something about a different
// moment of the turn. Plans and usage counters are revised in place, because a new message every
// time a checkbox ticks or a token counter moves buries the conversation.
func (ps *WorkspaceServer) HandleAgentTelemetry(ctx context.Context, sessionID string, p agentlink.TelemetryReport) {
	taskID, ok := ps.resolveTaskID(ctx, sessionID, p.TaskID)
	if !ok {
		zlog.Warn().Str("session_id", sessionID).Str("kind", p.Kind).
			Msg("could not relay agent telemetry: no active task")
		return
	}

	switch p.Kind {
	case agentTelemetryKindThought:
		if strings.TrimSpace(p.Text) == "" {
			return
		}
		ps.postAgentTelemetryMessage(ctx, taskID, p.Text, agentTelemetryMetadata(MessageTypeAgentThought, p))
	case agentTelemetryKindNotice:
		ps.postAgentTelemetryMessage(ctx, taskID, p.Text, agentTelemetryMetadata(MessageTypeAgentNotice, p))
	case agentTelemetryKindPlan:
		ps.reviseAgentTelemetryMessage(ctx, taskID, p, MessageTypeAgentPlan)
	case agentTelemetryKindUsage:
		if msgID := ps.reviseAgentTelemetryMessage(ctx, taskID, p, MessageTypeAgentUsage); msgID != 0 {
			ps.rememberUsageMessage(sessionID, taskID, msgID)
		}
	default:
		zlog.Debug().Str("kind", p.Kind).Int64("task_id", taskID).
			Msg("ignoring agent telemetry of an unrecognised kind")
	}
}

// agentTelemetryMetadata builds the message metadata a client renders from.
//
// Text is carried in the metadata as well as in the message body: the body is
// written once and a revised plan or usage line would otherwise go stale, so
// the metadata is what a client should prefer.
func agentTelemetryMetadata(messageType string, p agentlink.TelemetryReport) map[string]any {
	metadata := map[string]any{"type": messageType, "text": p.Text}
	for k, v := range p.Data {
		// The payload describes the telemetry; it does not get to say what
		// kind of message this is, replace the rendered text, or choose when
		// the server received it.
		if k == "type" || k == "text" || k == MessageMetadataReportedAt {
			continue
		}
		metadata[k] = v
	}
	if messageType == MessageTypeAgentUsage {
		metadata[MessageMetadataReportedAt] = nowFunc().UTC().Format(time.RFC3339Nano)
	}
	return metadata
}

// MessageMetadataReportedAt is when a usage footer was last written. The
// footer is revised in place, so this, not its position, is what tells the
// composer that a turn ended after the person last spoke.
const MessageMetadataReportedAt = "reportedAt"

// nowFunc is the clock reportedAt reads; a var so a test can pin it.
var nowFunc = time.Now

// postAgentTelemetryMessage appends a new chat message and returns its ID, or
// 0 if it could not be delivered.
func (ps *WorkspaceServer) postAgentTelemetryMessage(ctx context.Context, taskID int64, text string, metadata map[string]any) int64 {
	msgID, err := ps.reply(ctx, monoflake.ID(taskID).String(), text, nil, metadata)
	if err != nil {
		zlog.Error().Err(err).Int64("task_id", taskID).
			Msg("failed to relay agent telemetry")
		return 0
	}
	return msgID
}

// reviseAgentTelemetryMessage updates the message already standing for this
// plan or usage counter, and posts a new one only when there is none. It
// returns that message's ID, or 0 when nothing stands for it.
func (ps *WorkspaceServer) reviseAgentTelemetryMessage(ctx context.Context, taskID int64, p agentlink.TelemetryReport, messageType string) int64 {
	key := agentTelemetryMessageKey(taskID, p.Kind, p.PlanID())
	metadata := agentTelemetryMetadata(messageType, p)

	ps.agentTelemetryMessagesMu.RLock()
	msgID, known := ps.agentTelemetryMessages[key]
	ps.agentTelemetryMessagesMu.RUnlock()

	revised := false
	if known && ps.updateMessageMetadata != nil {
		if err := ps.updateMessageMetadata(ctx, taskID, msgID, metadata); err != nil {
			// The message is gone, or was never written: fall through and post
			// a fresh one rather than lose the update.
			zlog.Warn().Err(err).Int64("task_id", taskID).Int64("message_id", msgID).
				Msg("could not revise agent telemetry message; posting a new one")
		} else {
			revised = true
		}
	}

	if revised {
		ps.forgetRemovedPlan(key, p)
		return msgID
	}

	// A withdrawal notice for a plan that was never shown says nothing to the
	// human, and posting one would put an orphaned card in the conversation.
	if p.Removed() {
		return 0
	}

	if msgID = ps.postAgentTelemetryMessage(ctx, taskID, p.Text, metadata); msgID == 0 {
		return 0
	}
	ps.agentTelemetryMessagesMu.Lock()
	ps.agentTelemetryMessages[key] = msgID
	ps.agentTelemetryMessagesMu.Unlock()
	return msgID
}

// forgetRemovedPlan releases a withdrawn plan's message, so that an agent
// reusing the same plan ID later starts a new card rather than reviving the
// one marked withdrawn.
func (ps *WorkspaceServer) forgetRemovedPlan(key string, p agentlink.TelemetryReport) {
	if !p.Removed() {
		return
	}
	ps.agentTelemetryMessagesMu.Lock()
	delete(ps.agentTelemetryMessages, key)
	ps.agentTelemetryMessagesMu.Unlock()
}
