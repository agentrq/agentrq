// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"github.com/mustafaturan/monoflake"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
)

// workspacesChangedEvent tells the web app to read its workspace list again.
const workspacesChangedEvent = "workspaces.changed"

// publishWorkspaceListChange puts a workspace being added to or removed from
// the account on its owner's stream, and reports whether ev was a workspace
// event at all. Without it a workspace created, forked or merged anywhere but
// the open tab — by the supervisor's MCP, say — shows only after a reload.
//
// The owner comes from the event, not the row: a deleted workspace has none.
// A fork emits a create as well as its fork create, so only the create is sent.
func publishWorkspaceListChange(bus *eventbus.Bus, ev entity.CRUDEvent) bool {
	if ev.ResourceType != entity.ResourceWorkspace {
		return false
	}
	switch ev.Action {
	case entity.ActionWorkspaceCreate, entity.ActionWorkspaceDelete, entity.ActionWorkspaceForkMerge:
		bus.Publish(0, monoflake.ID(ev.UserID).String(), eventbus.Event{
			Type:    workspacesChangedEvent,
			Payload: map[string]string{"workspaceId": monoflake.ID(ev.ResourceID).String()},
		})
	}
	return true
}
