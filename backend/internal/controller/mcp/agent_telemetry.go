package mcp

import (
	"context"
	"fmt"
	"strings"

	zlog "github.com/rs/zerolog/log"

	"github.com/mustafaturan/monoflake"
)

// AgentTelemetryNotificationMethod is the channel notification carrying the
// parts of an agent's turn that are not its answer: the reasoning behind it,
// the plan it is working to, and what the turn is costing.
//
// Named "agent telemetry" throughout to keep it apart from emitTelemetry, which
// is this product's own usage analytics and has nothing to do with it.
const AgentTelemetryNotificationMethod = "notifications/claude/channel/telemetry"

// The kinds of telemetry a gateway sends.
const (
	agentTelemetryKindThought = "thought"
	agentTelemetryKindPlan    = "plan"
	agentTelemetryKindUsage   = "usage"
)

// The message metadata types the frontend renders these as.
const (
	MessageTypeAgentThought = "agent_thought"
	MessageTypeAgentPlan    = "agent_plan"
	MessageTypeAgentUsage   = "agent_usage"
)

// AgentTelemetryParams is the payload of an agent telemetry notification.
//
// Text is a rendered, ready-to-show form of the same thing Data holds
// structurally, so a client that does not understand a given kind still has
// something to show.
type AgentTelemetryParams struct {
	TaskID    string         `json:"task_id"`
	SessionID string         `json:"session_id"`
	Kind      string         `json:"kind"`
	Text      string         `json:"text"`
	Data      map[string]any `json:"data"`
}

// planID is the plan a payload refers to, or "" if it names none.
func (p AgentTelemetryParams) planID() string {
	id, _ := p.Data["planId"].(string)
	return id
}

// removed reports whether the agent withdrew the plan this payload names.
func (p AgentTelemetryParams) removed() bool {
	gone, _ := p.Data["removed"].(bool)
	return gone
}

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
// Reasoning is appended: each block explains a different moment of the turn.
// Plans and usage counters are revised in place, because a new message every
// time a checkbox ticks or a token counter moves buries the conversation.
func (ps *WorkspaceServer) HandleAgentTelemetry(ctx context.Context, sessionID string, p AgentTelemetryParams) {
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
	case agentTelemetryKindPlan:
		ps.reviseAgentTelemetryMessage(ctx, taskID, p, MessageTypeAgentPlan)
	case agentTelemetryKindUsage:
		ps.reviseAgentTelemetryMessage(ctx, taskID, p, MessageTypeAgentUsage)
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
func agentTelemetryMetadata(messageType string, p AgentTelemetryParams) map[string]any {
	metadata := map[string]any{"type": messageType, "text": p.Text}
	for k, v := range p.Data {
		// The payload describes the telemetry; it does not get to say what
		// kind of message this is or to replace the rendered text.
		if k == "type" || k == "text" {
			continue
		}
		metadata[k] = v
	}
	return metadata
}

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
// plan or usage counter, and posts a new one only when there is none.
func (ps *WorkspaceServer) reviseAgentTelemetryMessage(ctx context.Context, taskID int64, p AgentTelemetryParams, messageType string) {
	key := agentTelemetryMessageKey(taskID, p.Kind, p.planID())
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
		return
	}

	// A withdrawal notice for a plan that was never shown says nothing to the
	// human, and posting one would put an orphaned card in the conversation.
	if p.removed() {
		return
	}

	if msgID = ps.postAgentTelemetryMessage(ctx, taskID, p.Text, metadata); msgID == 0 {
		return
	}
	ps.agentTelemetryMessagesMu.Lock()
	ps.agentTelemetryMessages[key] = msgID
	ps.agentTelemetryMessagesMu.Unlock()
}

// forgetRemovedPlan releases a withdrawn plan's message, so that an agent
// reusing the same plan ID later starts a new card rather than reviving the
// one marked withdrawn.
func (ps *WorkspaceServer) forgetRemovedPlan(key string, p AgentTelemetryParams) {
	if !p.removed() {
		return
	}
	ps.agentTelemetryMessagesMu.Lock()
	delete(ps.agentTelemetryMessages, key)
	ps.agentTelemetryMessagesMu.Unlock()
}
