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
	view "github.com/agentrq/agentrq/backend/internal/data/view/api"
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
	if evt, ok := publishTaskDelete(bus, ev, ownerID); ok {
		publishToTeams(ctx, repo, bus, ev.WorkspaceID, evt)
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
			evt := eventbus.Event{Type: eventType, Payload: v}
			bus.Publish(ev.WorkspaceID, ownerID, evt)
			publishToTeams(ctx, repo, bus, ev.WorkspaceID, evt)
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

// publishTaskDelete puts a deleted task on the stream, and returns the event
// when ev was a delete. It is sent from here, not by the REST handler, so a
// delete by the supervisor's MCP removes the task from open pages too. The
// task row is gone, so the event carries only its id.
func publishTaskDelete(bus *eventbus.Bus, ev entity.CRUDEvent, ownerID string) (eventbus.Event, bool) {
	if ev.ResourceType != entity.ResourceTask || ev.Action != entity.ActionTaskDelete {
		return eventbus.Event{}, false
	}
	evt := eventbus.Event{
		Type:    "task.deleted",
		Payload: map[string]string{"id": monoflake.ID(ev.ResourceID).String()},
	}
	bus.Publish(ev.WorkspaceID, ownerID, evt)
	return evt, true
}

// teamEventPrefix starts the type of a task event a team member is sent for
// a workspace shared with the team: "team.task.created" and so on. A type of
// its own keeps it out of the pages that list the account's own tasks.
const teamEventPrefix = "team."

// teamReaders is what publishToTeams reads.
type teamReaders interface {
	ListTeamReaders(ctx context.Context, workspaceID int64) ([]base.TeamReader, error)
	TeamWorkspaceInfo(ctx context.Context, teamID, workspaceID int64) (base.TeamWorkspaceRow, error)
}

// publishToTeams sends a task event of the workspace to every member of a
// team who reads it there and has their account stream open, tagged with the
// team. Who reads it is asked at every event, so a share withdrawn, a member
// gone or a workspace archived stops the events at once. A task is sent as
// the team's task list shows it, with its owner and group.
func publishToTeams(ctx context.Context, repo teamReaders, bus *eventbus.Bus, workspaceID int64, evt eventbus.Event) {
	if !bus.AnyUserSubscribed() {
		return
	}
	readers, err := repo.ListTeamReaders(ctx, workspaceID)
	if err != nil {
		zlog.Warn().Err(err).Int64("workspace", workspaceID).Msg("[teams] failed to list a workspace's team readers")
		return
	}
	shown := map[int64]entity.TeamWorkspace{}
	for _, rd := range readers {
		userID := monoflake.ID(rd.UserID).String()
		if !bus.UserSubscribed(userID) {
			continue
		}
		payload := evt.Payload
		if task, ok := payload.(view.Task); ok {
			w, ok := shown[rd.TeamID]
			if !ok {
				row, err := repo.TeamWorkspaceInfo(ctx, rd.TeamID, workspaceID)
				if err != nil {
					continue
				}
				w = entity.TeamWorkspace{ID: row.ID, Name: row.Name, ForkOfID: row.ForkOfID, Group: row.GroupName,
					OwnerID: row.UserID, OwnerName: row.OwnerName, OwnerPicture: row.OwnerPicture}
				shown[rd.TeamID] = w
			}
			payload = mapper.FromTaskViewToTeamTaskView(task, w)
		}
		bus.Publish(0, userID, eventbus.Event{Type: teamEventPrefix + evt.Type, Payload: payload, TeamID: monoflake.ID(rd.TeamID).String()})
	}
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
