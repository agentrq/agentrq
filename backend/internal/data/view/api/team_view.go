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
// one. sharedName is the name of the shared workspace it shows under, its
// own or its parent's: the shared workspaces of one name, ignoring case and
// surrounding spaces, are one team workspace.
type TeamWorkspace struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	SharedName string    `json:"sharedName"`
	Owner      TeamOwner `json:"owner"`
	ForkOfID   string    `json:"forkOfId,omitempty"`
}

// ListTeamWorkspacesResponse is a team's workspaces, by shared name.
type ListTeamWorkspacesResponse struct {
	Workspaces []TeamWorkspace `json:"workspaces"`
}

// TeamTask is a task in team mode: the task, whose it is and the team
// workspace it is in.
type TeamTask struct {
	Task
	Owner      TeamOwner `json:"owner"`
	SharedName string    `json:"sharedName"`
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

// CloneTeamTaskRequest clones a teammate's task into one of your
// workspaces, not started, for assignee: human (when empty) or agent.
type CloneTeamTaskRequest struct {
	WorkspaceID string `json:"workspaceId"`
	Assignee    string `json:"assignee"`
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

// AddTeamSkillRequest links a teammate's skill into your account, on in your
// workspaces given. `replace` turns your own skill of that name into the
// link, its files dropped, rather than refusing the name.
type AddTeamSkillRequest struct {
	WorkspaceIDs []string `json:"workspaceIds"`
	Replace      bool     `json:"replace,omitempty"`
}

// AddTeamSkillResponse is the skill now in your account.
type AddTeamSkillResponse struct {
	Skill Skill `json:"skill"`
}

// TeammateMemory is a teammate in the team workspace of one of your
// workspaces, and whether its agent's memory index includes their memory.
// updatedAt is when a memory of theirs there was last written, absent when
// none was.
type TeammateMemory struct {
	UserID      string     `json:"userId"`
	Name        string     `json:"name"`
	Picture     string     `json:"picture"`
	WorkspaceID string     `json:"workspaceId"`
	UpdatedAt   *time.Time `json:"updatedAt,omitempty"`
	Included    bool       `json:"included"`
}

// ListTeammateMemoriesResponse is the teammates of a workspace of yours.
type ListTeammateMemoriesResponse struct {
	Teammates []TeammateMemory `json:"teammates"`
}

// SetTeammateMemoryRequest includes a teammate's memory, or leaves it out.
type SetTeammateMemoryRequest struct {
	Included *bool `json:"included"`
}

// SetTeammateMemoryResponse is the teammate, as now set.
type SetTeammateMemoryResponse struct {
	Teammate TeammateMemory `json:"teammate"`
}
