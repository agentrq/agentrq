// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"context"
	"errors"

	"github.com/mustafaturan/monoflake"
	zlog "github.com/rs/zerolog/log"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	mapper "github.com/agentrq/agentrq/backend/internal/mapper/api"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
)

// forwardCRUDEvent puts a CRUD event on the open pages' streams, as the event
// the web app reacts to. It is the one way a change made by the supervisor's
// MCP, which has no stream of its own, reaches the screen.
func forwardCRUDEvent(ctx context.Context, repo base.Repository, agents lastTaskAgents, bus *eventbus.Bus, ev entity.CRUDEvent) {
	if publishWorkspaceListChange(bus, ev) || publishPanoChange(bus, ev) {
		return
	}
	ws, err := repo.SystemGetWorkspace(ctx, ev.WorkspaceID)
	if err != nil {
		return
	}
	ownerID := monoflake.ID(ws.UserID).String()
	if publishTaskDelete(bus, ev, ownerID) {
		return
	}

	var taskID int64
	var eventType string

	switch ev.ResourceType {
	case entity.ResourceTask:
		taskID = ev.ResourceID
		switch ev.Action {
		case entity.ActionTaskCreate:
			eventType = "task.created"
		case entity.ActionTaskComplete:
			eventType = "status.updated"
		default:
			eventType = "task.updated"
		}
	case entity.ResourceMessage:
		if ev.Action == entity.ActionMessageCreate {
			msg, err := repo.SystemGetMessage(ctx, ev.ResourceID)
			if err == nil {
				taskID = msg.TaskID
				eventType = "reply.received"
			}
		}
	}

	if taskID != 0 && eventType != "" {
		t, err := taskEventPayload(ctx, repo, taskID)
		if err == nil {
			v := mapper.FromModelTaskToView(t)
			v.Agent = lastTaskAgent(ctx, agents, taskID)
			bus.Publish(ev.WorkspaceID, ownerID, eventbus.Event{
				Type:    eventType,
				Payload: v,
			})
		}
	}
}

// lastTaskAgents is the part of the crud controller that names the agent a
// task's card shows.
type lastTaskAgents interface {
	LastTaskAgents(ctx context.Context, taskIDs []int64) (map[int64]string, error)
}

// lastTaskAgent names the agent behind the task's latest change made by one,
// so the card names an agent as soon as one picks the task up. A failed
// lookup leaves it out, and the page keeps the name it already had.
func lastTaskAgent(ctx context.Context, agents lastTaskAgents, taskID int64) string {
	if agents == nil {
		return ""
	}
	names, err := agents.LastTaskAgents(ctx, []int64{taskID})
	if err != nil {
		return ""
	}
	return names[taskID]
}

// workspacesChangedEvent tells the web app to read its workspace list again.
const workspacesChangedEvent = "workspaces.changed"

// publishWorkspaceListChange puts a workspace being added, changed or removed
// on its owner's stream, and reports whether ev was a workspace
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
	// An update can archive or rename, which moves or removes the row too.
	case entity.ActionWorkspaceCreate, entity.ActionWorkspaceUpdate, entity.ActionWorkspaceDelete, entity.ActionWorkspaceForkMerge, entity.ActionWorkspaceResetID:
		bus.Publish(0, monoflake.ID(ev.UserID).String(), eventbus.Event{
			Type:    workspacesChangedEvent,
			Payload: map[string]string{"workspaceId": monoflake.ID(ev.ResourceID).String()},
		})
	}
	return true
}

// publishTaskDelete puts a deleted task on the stream, and reports whether ev
// was a delete. It is sent from here, not by the REST handler, so a delete by
// the supervisor's MCP removes the task from open pages too. The task row is
// gone, so the event carries only its id.
func publishTaskDelete(bus *eventbus.Bus, ev entity.CRUDEvent, ownerID string) bool {
	if ev.ResourceType != entity.ResourceTask || ev.Action != entity.ActionTaskDelete {
		return false
	}
	bus.Publish(ev.WorkspaceID, ownerID, eventbus.Event{
		Type:    "task.deleted",
		Payload: map[string]string{"id": monoflake.ID(ev.ResourceID).String()},
	})
	return true
}

// panoChangedEvent tells an open Pano board to read the Pano again.
const panoChangedEvent = "pano.changed"

// publishPanoChange puts a Pano doc written, deleted, commented on or given
// attachments on its owner's stream, and reports whether ev was a Pano event.
// Agents write the Pano from other workspaces, so without it the board shows
// their docs only after a reload.
func publishPanoChange(bus *eventbus.Bus, ev entity.CRUDEvent) bool {
	if ev.ResourceType != entity.ResourcePano {
		return false
	}
	bus.Publish(0, monoflake.ID(ev.UserID).String(), eventbus.Event{
		Type:    panoChangedEvent,
		Payload: map[string]string{"id": monoflake.ID(ev.ResourceID).String(), "action": ev.Action.String()},
	})
	return true
}

// panoTaskSyncer keeps the checklist items assigned to a task in step with it.
type panoTaskSyncer interface {
	SyncPanoTask(ctx context.Context, taskID int64) (int64, []int64, error)
}

// syncPanoAssignees recounts the checklists of the Pano docs linked to a task
// when its status changes — by the person, the agent or the supervisor — and
// tells open boards, which read the new status from the task. The pubsub is in-process, so
// only the instance that changed the task writes the docs.
func syncPanoAssignees(ctx context.Context, pano panoTaskSyncer, bus *eventbus.Bus, ev entity.CRUDEvent) {
	if ev.ResourceType != entity.ResourceTask || (ev.Action != entity.ActionTaskUpdate && ev.Action != entity.ActionTaskComplete) {
		return
	}
	uid, ids, err := pano.SyncPanoTask(ctx, ev.ResourceID)
	if err != nil && !errors.Is(err, base.ErrNotFound) {
		zlog.Warn().Err(err).Int64("task", ev.ResourceID).Msg("[pano] failed to sync an assigned task's status")
	}
	for _, id := range ids {
		bus.Publish(0, monoflake.ID(uid).String(), eventbus.Event{
			Type:    panoChangedEvent,
			Payload: map[string]string{"id": monoflake.ID(id).String(), "action": entity.ActionPanoUpdate.String()},
		})
	}
}

// taskListeners are the supervisor's listens to tasks (coremcp.TaskListeners).
type taskListeners interface {
	Listening() bool
	TaskChanged(workspaceID, taskID, delegatedByWorkspaceID int64)
}

// notifyTaskListeners tells the supervisor's listens that a task changed: the
// task itself, or a message in it, a question or permission request of its
// agent included. It reads nothing while nobody listens.
func notifyTaskListeners(ctx context.Context, repo base.Repository, listeners taskListeners, ev entity.CRUDEvent) {
	if !listeners.Listening() {
		return
	}
	var taskID int64
	switch ev.ResourceType {
	case entity.ResourceTask:
		taskID = ev.ResourceID
	case entity.ResourceMessage:
		msg, err := repo.SystemGetMessage(ctx, ev.ResourceID)
		if err != nil {
			return
		}
		taskID = msg.TaskID
	default:
		return
	}
	// A deleted task is gone: its workspace comes from the event, and the
	// workspace that delegated it is no longer known.
	workspaceID, delegatedBy := ev.WorkspaceID, int64(0)
	if t, err := repo.SystemGetTask(ctx, taskID); err == nil {
		workspaceID, delegatedBy = t.WorkspaceID, t.CreatedByWorkspaceID
	}
	listeners.TaskChanged(workspaceID, taskID, delegatedBy)
}
