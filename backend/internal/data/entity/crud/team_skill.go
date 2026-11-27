// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package crud

type (
	// TeamSkill is a team workspace's skill. Seen from a workspace, On is
	// whether the workspace is opted into it and Clash whether the
	// account's own skill of its name, on in that workspace, wins over it.
	// Seen from the team's page, On is true and Clash false. Its files carry
	// no public URL: it would outlive the share.
	TeamSkill struct {
		Skill
		TeamID            int64
		TeamWorkspaceID   int64
		TeamWorkspaceName string
		On                bool
		Clash             bool
	}

	// TeamWorkspaceRef is a team workspace a workspace is shared into.
	TeamWorkspaceRef struct {
		ID       int64
		Name     string
		TeamID   int64
		TeamName string
	}

	// WorkspaceTeamSkillsRequest lists the team skills a workspace of the
	// account's reaches; OnlyOn keeps the ones it is opted into, as its
	// agent sees them. Query, given, matches name or description.
	WorkspaceTeamSkillsRequest struct {
		WorkspaceID int64
		UserID      string
		Query       string
		OnlyOn      bool
	}

	WorkspaceTeamSkillsResponse struct {
		Skills         []TeamSkill
		TeamWorkspaces []TeamWorkspaceRef
	}

	// WorkspaceTeamSkillRequest names one team skill a workspace reaches,
	// by name, and a file of it (SKILL.md when Path is empty).
	WorkspaceTeamSkillRequest struct {
		WorkspaceID int64
		UserID      string
		Name        string
		Path        string
	}

	// TeamSkillFileResponse is a file of a team skill, with its version,
	// and the skill.
	TeamSkillFileResponse struct {
		Skill TeamSkill
		File  SkillFile
	}

	// SaveWorkspaceTeamSkillFileRequest writes a file of a team skill from a
	// workspace. Version is the one read, required to replace a file. A new
	// skill goes to TeamWorkspaceID, which must be one the workspace is
	// shared into.
	SaveWorkspaceTeamSkillFileRequest struct {
		WorkspaceID     int64
		UserID          string
		Name            string
		Path            string
		Content         string
		Version         *int64
		TeamWorkspaceID int64
	}

	// OptOutTeamSkillResponse says whether opting out deleted the skill,
	// no workspace being opted in any more.
	OptOutTeamSkillResponse struct {
		Deleted bool
	}

	// SetWorkspaceTeamSkillRequest opts a workspace into, or out of, a team
	// skill.
	SetWorkspaceTeamSkillRequest struct {
		WorkspaceID int64
		UserID      string
		SkillID     int64
		On          bool
	}

	// GiveSkillToTeamRequest moves the account's skill of that name into a
	// team workspace the workspace is shared into.
	GiveSkillToTeamRequest struct {
		WorkspaceID     int64
		UserID          string
		Name            string
		TeamWorkspaceID int64
	}

	// TeamSkillsRequest is a member reading or changing the skills of one
	// of the team's team workspaces, from the team's page. SkillID names
	// one; Path a file of it (SKILL.md when empty).
	TeamSkillsRequest struct {
		UserID          int64
		TeamID          int64
		TeamWorkspaceID int64
		SkillID         int64
		Path            string
	}

	// SaveTeamSkillFileRequest writes a file of a team workspace's skill
	// from the team's page; with no SkillID, a SKILL.md creates the skill.
	SaveTeamSkillFileRequest struct {
		TeamSkillsRequest
		Content string
		Version *int64
	}

	// ImportTeamSkillsRequest imports skills from GitHub or GitLab into a
	// team workspace, as ImportSkillsRequest does into an account.
	ImportTeamSkillsRequest struct {
		TeamSkillsRequest
		URL       string
		Overwrite bool
		Skills    []string
		// List offers the skills as Candidates and imports nothing.
		List bool
	}

	ImportTeamSkillsResponse struct {
		Imported     []TeamSkill
		Skipped      []SkillImportSkip
		Candidates   []SkillImportCandidate
		TooLarge     bool
		SourceRepo   string
		SourceRef    string
		SourceCommit string
	}
)
