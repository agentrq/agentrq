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

// AddTeamSkillRequest adds a teammate's skill to your account, on in your
// workspaces given.
type AddTeamSkillRequest struct {
	WorkspaceIDs []string `json:"workspaceIds"`
}

// AddTeamSkillResponse is the skill now in your account.
type AddTeamSkillResponse struct {
	Skill Skill `json:"skill"`
}
