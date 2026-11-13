// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import "time"

// TeamOwner is the account a shared workspace belongs to.
type TeamOwner struct {
	UserID  string `json:"userId"`
	Name    string `json:"name"`
	Picture string `json:"picture"`
}

// TeamWorkspace is a workspace a team reads: shared with it, or a fork of
// one, shown in the group it is shared as.
type TeamWorkspace struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Group    string    `json:"group"`
	Owner    TeamOwner `json:"owner"`
	ForkOfID string    `json:"forkOfId,omitempty"`
}

// ListTeamWorkspacesResponse is a team's workspaces, by group.
type ListTeamWorkspacesResponse struct {
	Workspaces []TeamWorkspace `json:"workspaces"`
}

// TeamTask is a task in team mode: the task, whose it is and its group.
type TeamTask struct {
	Task
	Owner TeamOwner `json:"owner"`
	Group string    `json:"group"`
}

// ListTeamTasksResponse is a page of a team's tasks, newest activity first.
type ListTeamTasksResponse struct {
	Tasks []TeamTask `json:"tasks"`
}

// GetTeamTaskResponse is a teammate's task as its own page reads it, with
// its workspace, owner and plan.
type GetTeamTaskResponse struct {
	GetTaskResponse
	Workspace TeamWorkspace `json:"workspace"`
	Owner     TeamOwner     `json:"owner"`
	Plan      *TaskPlan     `json:"plan"`
}

// TeamMemberStats is one member's completions and messages.
type TeamMemberStats struct {
	Owner          TeamOwner `json:"owner"`
	TasksCompleted int64     `json:"tasksCompleted"`
	Messages       int64     `json:"messages"`
}

// TeamStatsResponse is the workspace statistics summed over a team's
// workspaces, in the shape of a workspace's, and by member.
type TeamStatsResponse struct {
	Summary    StatsSummary      `json:"summary"`
	Timeseries StatsTimeseries   `json:"timeseries"`
	Heatmap    StatsHeatmap      `json:"heatmap"`
	ByMember   []TeamMemberStats `json:"byMember"`
}

// StatsSummary is the totals over a window.
type StatsSummary struct {
	TasksCompleted  int64 `json:"tasksCompleted"`
	TasksScheduled  int64 `json:"tasksScheduled"`
	Messages        int64 `json:"messages"`
	ManualApprovals int64 `json:"manualApprovals"`
	AutoApprovals   int64 `json:"autoApprovals"`
	Denies          int64 `json:"denies"`
}

// StatsCount is a count in one date or bucket.
type StatsCount struct {
	Date   string `json:"date,omitempty"`
	Bucket string `json:"bucket,omitempty"`
	Count  int64  `json:"count"`
}

// StatsTimeseries is completions and messages by day.
type StatsTimeseries struct {
	TasksCompleted []StatsCount `json:"tasksCompleted"`
	Messages       []StatsCount `json:"messages"`
}

// StatsHeatmap is completions and messages by hour, for a week or less, or
// by day.
type StatsHeatmap struct {
	Granularity    string       `json:"granularity"`
	RangeStart     int64        `json:"rangeStart"`
	RangeEnd       int64        `json:"rangeEnd"`
	TasksCompleted []StatsCount `json:"tasksCompleted"`
	Messages       []StatsCount `json:"messages"`
}

// CloneTeamTaskRequest clones a teammate's task into one of your workspaces.
type CloneTeamTaskRequest struct {
	WorkspaceID string `json:"workspaceId"`
	Status      string `json:"status"`
}

// TeamMemory is a team's memory.md. updatedAt and updatedBy are null before
// anyone saved it; conflict says a save replaced a newer one.
type TeamMemory struct {
	Content   string     `json:"content"`
	UpdatedAt *time.Time `json:"updatedAt"`
	UpdatedBy *TeamOwner `json:"updatedBy"`
	Conflict  bool       `json:"conflict"`
}

// SaveTeamMemoryRequest saves a team's memory; baseUpdatedAt is the
// updatedAt the writer read, empty when there was none.
type SaveTeamMemoryRequest struct {
	Content       string `json:"content"`
	BaseUpdatedAt string `json:"baseUpdatedAt"`
}

// TeamMemoryIndexEntry is one shared workspace's memories.
type TeamMemoryIndexEntry struct {
	WorkspaceID   string    `json:"workspaceId"`
	WorkspaceName string    `json:"workspaceName"`
	Group         string    `json:"group"`
	Owner         TeamOwner `json:"owner"`
	Names         []string  `json:"names"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

// TeamMemoryIndexResponse is the shared workspaces' memories, by group, then
// owner.
type TeamMemoryIndexResponse struct {
	Memories []TeamMemoryIndexEntry `json:"memories"`
}

// TeamSkill is a skill on in a team's workspaces, and who has it on.
// content, its SKILL.md, is read only for one skill.
type TeamSkill struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	UpdatedAt   time.Time   `json:"updatedAt"`
	Owners      []TeamOwner `json:"owners"`
	Content     string      `json:"content,omitempty"`
}

// ListTeamSkillsResponse is a team's skills, by name.
type ListTeamSkillsResponse struct {
	Skills []TeamSkill `json:"skills"`
}

// GetTeamSkillResponse is one of a team's skills, with its SKILL.md.
type GetTeamSkillResponse struct {
	Skill TeamSkill `json:"skill"`
}

// AddTeamSkillRequest adds a teammate's skill to your account, on in your
// workspaces given.
type AddTeamSkillRequest struct {
	WorkspaceIDs []string `json:"workspaceIds"`
}

// AddTeamSkillResponse is the skill now in your account.
type AddTeamSkillResponse struct {
	Skill Skill `json:"skill"`
}
