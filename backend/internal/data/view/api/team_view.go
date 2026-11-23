// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

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
	// TeamWorkspaceID is the team workspace it is shared into, whose
	// skills are at /teams/:teamId/team-workspaces/:teamWorkspaceId/skills.
	TeamWorkspaceID string `json:"teamWorkspaceId,omitempty"`
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
