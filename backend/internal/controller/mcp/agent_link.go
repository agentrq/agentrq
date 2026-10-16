// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"context"
	"slices"

	"github.com/agentrq/agentrq/backend/internal/controller/agentlink"
)

// AttachAgentLink adds a native agent connection to the workspace. Attaching
// the same link twice is a no-op.
func (ps *WorkspaceServer) AttachAgentLink(link agentlink.Link) {
	ps.linksMu.Lock()
	if slices.ContainsFunc(ps.links, func(l agentlink.Link) bool { return l.ID() == link.ID() }) {
		ps.linksMu.Unlock()
		return
	}
	ps.links = append(ps.links, link)
	ps.linksMu.Unlock()
	ps.refreshConnected()
}

// DetachAgentLink removes a link and tells the open pages that what it
// reported no longer holds — the same thing an MCP session's last stream
// closing does.
func (ps *WorkspaceServer) DetachAgentLink(ctx context.Context, id string) {
	ps.linksMu.Lock()
	i := slices.IndexFunc(ps.links, func(l agentlink.Link) bool { return l.ID() == id })
	if i >= 0 {
		ps.links = slices.Delete(ps.links, i, i+1)
	}
	ps.linksMu.Unlock()
	if i < 0 {
		return
	}
	ps.agentSessionEnded(context.WithoutCancel(ctx), id)
	ps.refreshConnected()
}

// agentLinks is a copy of the attached links, in attach order, so a link's
// methods are never called with linksMu held. A slice and a linear scan are
// deliberate: a workspace has a handful of links at most, where a map costs
// more than it saves and loses the order the live-session readers depend on.
func (ps *WorkspaceServer) agentLinks() []agentlink.Link {
	ps.linksMu.RLock()
	defer ps.linksMu.RUnlock()
	return slices.Clone(ps.links)
}

func (ps *WorkspaceServer) agentLink(id string) (agentlink.Link, bool) {
	ps.linksMu.RLock()
	defer ps.linksMu.RUnlock()
	for _, l := range ps.links {
		if l.ID() == id {
			return l, true
		}
	}
	return nil, false
}

func (ps *WorkspaceServer) linkIDs() []string {
	ps.linksMu.RLock()
	defer ps.linksMu.RUnlock()
	ids := make([]string, 0, len(ps.links))
	for _, l := range ps.links {
		ids = append(ids, l.ID())
	}
	return ids
}

func (ps *WorkspaceServer) hasAgentLinks() bool {
	ps.linksMu.RLock()
	defer ps.linksMu.RUnlock()
	return len(ps.links) > 0
}

// pickLinkFor chooses the one link a task goes to: the link already holding
// the task's session, else the one with the most free capacity (the first on a
// tie), else nil. MCP sessions are not chosen between — they are broadcast to,
// as they always were.
func (ps *WorkspaceServer) pickLinkFor(taskID int64) agentlink.Link {
	var best agentlink.Link
	bestFree := -1
	for _, l := range ps.agentLinks() {
		owns, free := l.Dispatch(taskID)
		if owns {
			return l
		}
		if free > bestFree {
			best, bestFree = l, free
		}
	}
	return best
}

// refreshConnected publishes agent.connected when the combined answer — any
// MCP stream, or any link — changes, and only then. Holding connectedMu while
// recomputing is what keeps a stream ending and a link attaching at the same
// moment from publishing their answers in the wrong order. Lock order is
// always connectedMu, then linksMu.
func (ps *WorkspaceServer) refreshConnected() {
	ps.connectedMu.Lock()
	defer ps.connectedMu.Unlock()
	now := ps.IsAgentConnected()
	if now == ps.connected {
		return
	}
	ps.connected = now
	ps.publishAgentConnected(now)
}
