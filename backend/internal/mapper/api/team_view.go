// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"time"

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
	v := view.TeamWorkspace{ID: id62(w.ID), Name: w.Name, Group: w.Group, Owner: FromTeamWorkspaceEntityToOwnerView(w)}
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
	return view.TeamTask{Task: teamTaskFiles(t, teamID), Owner: FromTeamWorkspaceEntityToOwnerView(w), Group: w.Group}
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

func fromDailyStats(ds []entity.DailyStat) []view.StatsCount {
	out := make([]view.StatsCount, len(ds))
	for i, d := range ds {
		out[i] = view.StatsCount{Date: d.Date, Count: d.Count}
	}
	return out
}

func fromHeatmapStats(hs []entity.HeatmapStat) []view.StatsCount {
	out := make([]view.StatsCount, len(hs))
	for i, h := range hs {
		out[i] = view.StatsCount{Bucket: h.Bucket, Count: h.Count}
	}
	return out
}

// FromTeamStatsEntityToHTTPResponse renders a team's statistics.
func FromTeamStatsEntityToHTTPResponse(rs *entity.GetStatsOfWorkspacesResponse) []byte {
	s := rs.Stats
	v := view.TeamStatsResponse{
		Summary: view.StatsSummary{
			TasksCompleted: s.Summary.TasksCompleted, TasksScheduled: s.Summary.TasksScheduled, Messages: s.Summary.Messages,
			ManualApprovals: s.Summary.ManualApprovals, AutoApprovals: s.Summary.AutoApprovals, Denies: s.Summary.Denies,
		},
		Timeseries: view.StatsTimeseries{
			TasksCompleted: fromDailyStats(s.Timeseries.TasksCompleted),
			Messages:       fromDailyStats(s.Timeseries.Messages),
		},
		Heatmap: view.StatsHeatmap{
			Granularity: s.Heatmap.Granularity, RangeStart: s.Heatmap.RangeStart, RangeEnd: s.Heatmap.RangeEnd,
			TasksCompleted: fromHeatmapStats(s.Heatmap.TasksCompleted),
			Messages:       fromHeatmapStats(s.Heatmap.Messages),
		},
		ByMember: make([]view.TeamMemberStats, len(rs.ByMember)),
	}
	for i, m := range rs.ByMember {
		v.ByMember[i] = view.TeamMemberStats{Owner: fromTeamOwner(m.UserID, m.Name, m.Picture), TasksCompleted: m.TasksCompleted, Messages: m.Messages}
	}
	b, _ := json.Marshal(v)
	return b
}

// FromHTTPRequestToSaveTeamMemoryRequestEntity reads a team memory save; nil
// when baseUpdatedAt is given but is not a time.
func FromHTTPRequestToSaveTeamMemoryRequestEntity(rq view.SaveTeamMemoryRequest) *entity.SaveTeamMemoryRequest {
	out := &entity.SaveTeamMemoryRequest{Content: rq.Content}
	if rq.BaseUpdatedAt != "" {
		t, err := time.Parse(time.RFC3339Nano, rq.BaseUpdatedAt)
		if err != nil {
			return nil
		}
		out.BaseUpdatedAt = t
	}
	return out
}

// FromTeamMemoryEntityToHTTPResponse renders a team's memory.
func FromTeamMemoryEntityToHTTPResponse(m entity.TeamMemory) []byte {
	v := view.TeamMemory{Content: m.Content, Conflict: m.Conflict}
	if !m.UpdatedAt.IsZero() {
		at := m.UpdatedAt
		by := fromTeamOwner(m.UpdatedBy, m.UpdatedByName, m.UpdatedByPicture)
		v.UpdatedAt, v.UpdatedBy = &at, &by
	}
	b, _ := json.Marshal(v)
	return b
}

// FromTeamMemoryIndexEntitiesToHTTPResponse renders the shared workspaces'
// memories.
func FromTeamMemoryIndexEntitiesToHTTPResponse(es []entity.TeamMemoryIndexEntry) []byte {
	out := make([]view.TeamMemoryIndexEntry, len(es))
	for i, e := range es {
		out[i] = view.TeamMemoryIndexEntry{
			WorkspaceID: id62(e.Workspace.ID), WorkspaceName: e.Workspace.Name, Group: e.Workspace.Group,
			Owner: FromTeamWorkspaceEntityToOwnerView(e.Workspace), Names: e.Names, UpdatedAt: e.UpdatedAt,
		}
	}
	b, _ := json.Marshal(view.TeamMemoryIndexResponse{Memories: out})
	return b
}

// FromMemoryEntityToHTTPResponse renders one memory on its own.
func FromMemoryEntityToHTTPResponse(m entity.Memory) []byte {
	b, _ := json.Marshal(fromEntityMemoryToView(m))
	return b
}

func fromTeamSkillEntity(s entity.TeamSkill) view.TeamSkill {
	owners := make([]view.TeamOwner, len(s.Owners))
	for i, o := range s.Owners {
		owners[i] = fromTeamOwner(o.UserID, o.Name, o.Picture)
	}
	return view.TeamSkill{Name: s.Name, Description: s.Description, UpdatedAt: s.UpdatedAt, Owners: owners, Content: s.Content}
}

// FromTeamSkillEntitiesToHTTPResponse renders a team's skills.
func FromTeamSkillEntitiesToHTTPResponse(ss []entity.TeamSkill) []byte {
	out := make([]view.TeamSkill, len(ss))
	for i, s := range ss {
		out[i] = fromTeamSkillEntity(s)
	}
	b, _ := json.Marshal(view.ListTeamSkillsResponse{Skills: out})
	return b
}

// FromTeamSkillEntityToHTTPResponse renders one of a team's skills.
func FromTeamSkillEntityToHTTPResponse(s entity.TeamSkill) []byte {
	b, _ := json.Marshal(view.GetTeamSkillResponse{Skill: fromTeamSkillEntity(s)})
	return b
}

// FromAddTeamSkillResponseEntityToHTTPResponse renders the skill added.
func FromAddTeamSkillResponseEntityToHTTPResponse(s entity.Skill) []byte {
	b, _ := json.Marshal(view.AddTeamSkillResponse{Skill: fromEntitySkillToView(s)})
	return b
}
