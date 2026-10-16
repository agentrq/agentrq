// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"context"
	"errors"
	"slices"

	"github.com/mustafaturan/monoflake"
	zlog "github.com/rs/zerolog/log"

	"github.com/agentrq/agentrq/backend/internal/controller/agentlink"
	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
)

// Reasons a reasoning level cannot be selected.
var (
	ErrEffortSelectUnsupported = errors.New("the connected agent does not support choosing a reasoning level")
	ErrEffortNotOffered        = errors.New("the connected agent does not offer that reasoning level")
	ErrEffortSetNotDelivered   = errors.New("the connected agent could not be reached")
)

// HandleAgentEffort records a session's reasoning level, keyed by transport
// session like HandleAgentModels, and publishes it when it changed.
func (ps *WorkspaceServer) HandleAgentEffort(_ context.Context, sessionID string, p agentlink.EffortReport) {
	snapshot := agentlink.EffortSnapshot{
		SessionID:     p.SessionID,
		ConfigID:      p.ConfigID,
		CurrentEffort: p.CurrentEffort,
		Efforts:       p.Efforts,
		CanSet:        p.CanSet,
	}

	ps.agentEffortMu.Lock()
	previous, had := ps.agentEffort[sessionID]
	changed := !had || !sameAgentEffort(previous, snapshot)
	ps.agentEffort[sessionID] = snapshot
	pruneAgentEffort(ps.agentEffort, ps.liveSessionIDs())
	ps.agentEffortMu.Unlock()

	if changed {
		ps.publishAgentEffort(snapshot)
	}
}

func sameAgentEffort(a, b agentlink.EffortSnapshot) bool {
	return a.ConfigID == b.ConfigID &&
		a.CurrentEffort == b.CurrentEffort &&
		a.CanSet == b.CanSet &&
		slices.Equal(a.Efforts, b.Efforts)
}

// publishAgentEffort tells the open pages what the workspace now offers. A
// withdrawal falls back to another live session's offer, as models do.
func (ps *WorkspaceServer) publishAgentEffort(reported agentlink.EffortSnapshot) {
	published := reported
	if len(published.Efforts) == 0 {
		published = agentlink.EffortSnapshot{}
		if snapshot := ps.AgentEffort(); snapshot != nil {
			published = *snapshot
		}
	}
	efforts := published.Efforts
	if efforts == nil {
		efforts = []agentlink.Effort{}
	}
	ps.bus.Publish(ps.workspaceID, ps.userID, eventbus.Event{
		Type: "agent.effort",
		Payload: map[string]any{
			"configId":      published.ConfigID,
			"currentEffort": published.CurrentEffort,
			"canSet":        published.Selectable(),
			"efforts":       efforts,
			"workspaceId":   monoflake.ID(ps.workspaceID).String(),
		},
	})
}

// AgentEffort reports the reasoning levels the connected agent offers, or nil
// when no streaming session offers any.
func (ps *WorkspaceServer) AgentEffort() *agentlink.EffortSnapshot {
	ps.agentEffortMu.RLock()
	defer ps.agentEffortMu.RUnlock()
	_, snapshot := pickAgentEffortSession(ps.agentEffort, ps.streamingSessionIDs())
	return snapshot
}

// pickAgentEffortSession is pickAgentModelsSession for reasoning levels: the
// first live session that can switch, else the first that offers any.
func pickAgentEffortSession(snapshots map[string]agentlink.EffortSnapshot, live []string) (string, *agentlink.EffortSnapshot) {
	var fallbackID string
	var fallback *agentlink.EffortSnapshot
	for _, id := range live {
		snapshot, ok := snapshots[id]
		if !ok || len(snapshot.Efforts) == 0 {
			continue
		}
		if snapshot.Selectable() {
			return id, &snapshot
		}
		if fallback == nil {
			fallbackID, fallback = id, &snapshot
		}
	}
	return fallbackID, fallback
}

// SetAgentEffort asks the one session that offered the choice to switch its
// reasoning level. Only a native link reports reasoning levels, so only a link
// is asked, by its own call. The snapshot is not updated here: the agent's next
// report says whether it did.
func (ps *WorkspaceServer) SetAgentEffort(ctx context.Context, effortID string) error {
	ps.agentEffortMu.RLock()
	sessID, snapshot := pickAgentEffortSession(ps.agentEffort, ps.streamingSessionIDs())
	ps.agentEffortMu.RUnlock()

	if snapshot == nil || !snapshot.Selectable() {
		return ErrEffortSelectUnsupported
	}
	if !slices.ContainsFunc(snapshot.Efforts, func(e agentlink.Effort) bool { return e.ID == effortID }) {
		return ErrEffortNotOffered
	}
	link, ok := ps.agentLink(sessID)
	if !ok || !link.SetEffort(ctx, snapshot.SessionID, snapshot.ConfigID, effortID) {
		return ErrEffortSetNotDelivered
	}
	zlog.Info().
		Int64("workspace_id", ps.workspaceID).
		Str("session_id", sessID).
		Str("effort", effortID).
		Msg("asked the connected agent to switch reasoning level")
	return nil
}

// pruneAgentEffort drops the snapshots of sessions that have gone away, under
// the same rule as pruneAgentModels. The caller holds the write lock.
func pruneAgentEffort(snapshots map[string]agentlink.EffortSnapshot, live []string) {
	if len(live) == 0 {
		return
	}
	keep := make(map[string]bool, len(live))
	for _, id := range live {
		keep[id] = true
	}
	for id := range snapshots {
		if !keep[id] {
			delete(snapshots, id)
		}
	}
}
