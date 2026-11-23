// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package api

import (
	"encoding/json"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	view "github.com/agentrq/agentrq/backend/internal/data/view/api"
)

func fromTeamOwner(userID int64, name, picture string) view.TeamOwner {
	return view.TeamOwner{UserID: id62(userID), Name: name, Picture: picture}
}

// FromTeamWorkspaceEntityToOwnerView is the owner of a team's workspace.
func FromTeamWorkspaceEntityToOwnerView(w entity.TeamWorkspace) view.TeamOwner {
	return fromTeamOwner(w.OwnerID, w.OwnerName, w.OwnerPicture)
}

// FromTeamWorkspaceEntityToView is a workspace as a team sees it.
func FromTeamWorkspaceEntityToView(w entity.TeamWorkspace) view.TeamWorkspace {
	v := view.TeamWorkspace{ID: id62(w.ID), Name: w.Name, SharedName: w.SharedName, Owner: FromTeamWorkspaceEntityToOwnerView(w)}
	if w.TeamWorkspaceID != 0 {
		v.TeamWorkspaceID = id62(w.TeamWorkspaceID)
	}
	if w.ForkOfID != 0 {
		v.ForkOfID = id62(w.ForkOfID)
	}
	return v
}

// FromTeamWorkspaceEntitiesToHTTPResponse renders a team's workspaces.
func FromTeamWorkspaceEntitiesToHTTPResponse(ws []entity.TeamWorkspace) []byte {
	out := make([]view.TeamWorkspace, len(ws))
	for i, w := range ws {
		out[i] = FromTeamWorkspaceEntityToView(w)
	}
	b, _ := json.Marshal(view.ListTeamWorkspacesResponse{Workspaces: out})
	return b
}

// FromTaskViewToTeamTaskView is a task as team teamID sees it, in w.
func FromTaskViewToTeamTaskView(t view.Task, w entity.TeamWorkspace, teamID int64) view.TeamTask {
	return view.TeamTask{Task: teamTaskFiles(t, teamID), Owner: FromTeamWorkspaceEntityToOwnerView(w), SharedName: w.SharedName}
}

// teamTaskFiles is t with the link of every file, its own and its messages',
// replaced by team teamID's route to it. That route checks the reader's
// session and the share on every read, so a teammate loses the file with the
// share; the public link would keep working. t's own slices are left as they
// are: one event's task is sent to several teams.
func teamTaskFiles(t view.Task, teamID int64) view.Task {
	base := "/api/v1/teams/" + id62(teamID) + "/tasks/" + t.ID + "/attachments/"
	t.Attachments = teamFileLinks(t.Attachments, base)
	if len(t.Messages) > 0 {
		msgs := make([]view.Message, len(t.Messages))
		for i, m := range t.Messages {
			m.Attachments = teamFileLinks(m.Attachments, base)
			msgs[i] = m
		}
		t.Messages = msgs
	}
	return t
}

func teamFileLinks(as []view.Attachment, base string) []view.Attachment {
	if len(as) == 0 {
		return as
	}
	out := make([]view.Attachment, len(as))
	for i, a := range as {
		a.URL = base + a.ID
		out[i] = a
	}
	return out
}

// FromTeamTaskEntitiesToHTTPResponse renders a page of team teamID's tasks.
func FromTeamTaskEntitiesToHTTPResponse(ts []entity.TeamTask, teamID int64) []byte {
	out := make([]view.TeamTask, len(ts))
	for i, t := range ts {
		out[i] = FromTaskViewToTeamTaskView(FromEntityTaskToView(t.Task), t.Workspace, teamID)
	}
	b, _ := json.Marshal(view.ListTeamTasksResponse{Tasks: out})
	return b
}

// FromTeamTaskDetailEntityToHTTPResponse renders a teammate's task, as team
// teamID reads it.
func FromTeamTaskDetailEntityToHTTPResponse(d entity.TeamTaskDetail, teamID int64) []byte {
	v := view.GetTeamTaskResponse{
		GetTaskResponse: fromGetTaskResponseEntityToView(&d.Detail),
		Workspace:       FromTeamWorkspaceEntityToView(d.Workspace),
		Owner:           FromTeamWorkspaceEntityToOwnerView(d.Workspace),
	}
	v.Task = teamTaskFiles(v.Task, teamID)
	if p := d.Plan; p != nil {
		v.Plan = &view.TaskPlan{ID: id62(p.ID), Content: p.Content, Version: p.Version, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt}
	}
	b, _ := json.Marshal(v)
	return b
}

// FromTeamStatsEntityToHTTPResponse renders a team's statistics in the shape
// of one workspace's, so the same panels draw both.
func FromTeamStatsEntityToHTTPResponse(rs *entity.GetDetailedWorkspaceStatsResponse) []byte {
	b, _ := json.Marshal(rs)
	return b
}

func fromTeammateMemoryEntity(m entity.TeammateMemory) view.TeammateMemory {
	return view.TeammateMemory{
		UserID: id62(m.UserID), Name: m.Name, Picture: m.Picture,
		WorkspaceID: id62(m.WorkspaceID), UpdatedAt: m.UpdatedAt, Included: m.Included,
	}
}

// FromTeammateMemoryEntitiesToHTTPResponse renders the teammates of a
// workspace of the caller's.
func FromTeammateMemoryEntitiesToHTTPResponse(ms []entity.TeammateMemory) []byte {
	out := make([]view.TeammateMemory, len(ms))
	for i, m := range ms {
		out[i] = fromTeammateMemoryEntity(m)
	}
	b, _ := json.Marshal(view.ListTeammateMemoriesResponse{Teammates: out})
	return b
}

// FromTeammateMemoryEntityToHTTPResponse renders one teammate, as now set.
func FromTeammateMemoryEntityToHTTPResponse(m entity.TeammateMemory) []byte {
	b, _ := json.Marshal(view.SetTeammateMemoryResponse{Teammate: fromTeammateMemoryEntity(m)})
	return b
}
