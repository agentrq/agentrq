// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

type (
	// TeamWorkspaceSkill is a team workspace's skill. Read from a workspace,
	// `on` is whether the workspace is opted into it, and `clash` whether the
	// account's own skill of its name, on in that workspace, wins over it.
	// Read from the team's page, `on` is true and `clash` false. Its files
	// have no public `url`.
	TeamWorkspaceSkill struct {
		Skill
		TeamID            string `json:"teamId"`
		TeamWorkspaceID   string `json:"teamWorkspaceId"`
		TeamWorkspaceName string `json:"teamWorkspaceName"`
		On                bool   `json:"on"`
		Clash             bool   `json:"clash"`
	}

	// TeamWorkspaceRef is a team workspace a workspace is shared into.
	TeamWorkspaceRef struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		TeamID   string `json:"teamId"`
		TeamName string `json:"teamName"`
	}

	// WorkspaceTeamSkillsResponse is the team skills a workspace reaches,
	// and the team workspaces it is shared into, for "Give to team".
	WorkspaceTeamSkillsResponse struct {
		Skills         []TeamWorkspaceSkill `json:"skills"`
		TeamWorkspaces []TeamWorkspaceRef   `json:"teamWorkspaces"`
	}

	// SetWorkspaceTeamSkillRequest opts a workspace into, or out of, a team
	// skill; `on` must be given.
	SetWorkspaceTeamSkillRequest struct {
		On *bool `json:"on"`
	}

	// GiveSkillToTeamRequest moves the account's skill into a team
	// workspace the workspace is shared into.
	GiveSkillToTeamRequest struct {
		TeamWorkspaceID string `json:"teamWorkspaceId"`
	}

	TeamWorkspaceSkillResponse struct {
		Skill TeamWorkspaceSkill `json:"skill"`
	}

	TeamWorkspaceSkillsResponse struct {
		Skills []TeamWorkspaceSkill `json:"skills"`
	}

	// TeamWorkspaceSkillFileResponse is a file of a team skill, with its
	// version, which a save of it must send.
	TeamWorkspaceSkillFileResponse struct {
		Skill TeamWorkspaceSkill `json:"skill"`
		File  SkillFile          `json:"file"`
	}

	// SaveTeamSkillFileRequest writes a file of a team workspace's skill.
	// `version` is the one read, required to replace a file; leave it out
	// to add one. Posted to the skills list, a SKILL.md creates a skill,
	// named by its frontmatter.
	SaveTeamSkillFileRequest struct {
		Content string `json:"content"`
		Version *int64 `json:"version,omitempty"`
	}

	// ImportTeamSkillsRequest imports skills from GitHub into a team
	// workspace.
	ImportTeamSkillsRequest struct {
		URL       string   `json:"url"`
		Overwrite bool     `json:"overwrite"`
		Skills    []string `json:"skills,omitempty"`
	}

	// ImportTeamSkillsResponse is ImportSkillsResponse, with the skills
	// imported in full.
	ImportTeamSkillsResponse struct {
		Imported     []TeamWorkspaceSkill   `json:"imported"`
		Skipped      []SkippedSkillEntry    `json:"skipped"`
		Candidates   []SkillImportCandidate `json:"candidates,omitempty"`
		SourceRepo   string                 `json:"sourceRepo"`
		SourceRef    string                 `json:"sourceRef"`
		SourceCommit string                 `json:"sourceCommit"`
	}
)
