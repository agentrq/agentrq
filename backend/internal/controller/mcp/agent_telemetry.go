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

// The message metadata types the frontend renders these as. A plan is not a
// message: it is the task's plan, kept by plans.
const (
	MessageTypeAgentThought = "agent_thought"
	MessageTypeAgentUsage   = "agent_usage"
	MessageTypeAgentNotice  = "agent_notice"
)

// agentTelemetryMessageKey identifies the one chat message that stands for a
// task's usage counters, which are revised in place rather than appended to.
func agentTelemetryMessageKey(taskID int64, kind string) string {
	return fmt.Sprintf("%d:%s", taskID, kind)
}

// HandleAgentTelemetry relays one piece of agent telemetry into a task.
//
// Reasoning and notices are appended: each says something about a different
// moment of the turn. Usage counters are revised in place, because a new message every
// time a token counter moves buries the conversation. A plan replaces the
// task's plan.
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
		ps.saveAgentPlan(ctx, taskID, p)
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

// saveAgentPlan stores the plan as the task's, or removes it when the agent
// withdrew it.
func (ps *WorkspaceServer) saveAgentPlan(ctx context.Context, taskID int64, p agentlink.TelemetryReport) {
	if ps.plans == nil {
		return
	}
	if p.Removed() {
		if err := ps.plans.Delete(ctx, taskID); err != nil {
			zlog.Error().Err(err).Int64("task_id", taskID).Msg("failed to remove the agent's plan")
		}
		return
	}
	if err := ps.plans.Save(ctx, taskID, p.Text); err != nil {
		zlog.Error().Err(err).Int64("task_id", taskID).Msg("failed to save the agent's plan")
		return
	}
	ps.emitTelemetry(ctx, ActionMCPPlanUpdate, "plan", clientIdentity{})
}

// reviseAgentTelemetryMessage updates the message already standing for this
// usage counter, and posts a new one only when there is none. It returns that
// message's ID, or 0 when nothing stands for it.
func (ps *WorkspaceServer) reviseAgentTelemetryMessage(ctx context.Context, taskID int64, p agentlink.TelemetryReport, messageType string) int64 {
	key := agentTelemetryMessageKey(taskID, p.Kind)
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
		return msgID
	}

	if msgID = ps.postAgentTelemetryMessage(ctx, taskID, p.Text, metadata); msgID == 0 {
		return 0
	}
	ps.agentTelemetryMessagesMu.Lock()
	ps.agentTelemetryMessages[key] = msgID
	ps.agentTelemetryMessagesMu.Unlock()
	return msgID
}
