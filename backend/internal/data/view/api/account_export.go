// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package api

import "time"

// AccountExport is the file an account's data is downloaded as. Ids are the
// API's; a workspaceId of "" is the account's own.
type (
	AccountExport struct {
		Version    int                      `json:"version"`
		ExportedAt time.Time                `json:"exportedAt"`
		Profile    AccountExportProfile     `json:"profile"`
		Settings   AccountSettings          `json:"settings"`
		Presets    []AccountExportPreset    `json:"presets"`
		Workspaces []AccountExportWorkspace `json:"workspaces"`
		Memories   []AccountExportMemory    `json:"memories"`
		PanoDocs   []AccountExportPanoDoc   `json:"panoDocs"`
		Tasks      []AccountExportTask      `json:"tasks"`
	}

	AccountExportProfile struct {
		ID    string `json:"id"`
		Email string `json:"email"`
		Name  string `json:"name"`
	}

	AccountExportPreset struct {
		WorkspaceID string    `json:"workspaceId"`
		Name        string    `json:"name"`
		Body        string    `json:"body"`
		CreatedAt   time.Time `json:"createdAt"`
	}

	AccountExportWorkspace struct {
		ID                   string     `json:"id"`
		Name                 string     `json:"name"`
		Description          string     `json:"description"`
		Icon                 string     `json:"icon"`
		WorkingDirectory     string     `json:"workingDirectory"`
		SelfLearningLoopNote string     `json:"selfLearningLoopNote"`
		ForkOfID             string     `json:"forkOfId"`
		CreatedAt            time.Time  `json:"createdAt"`
		ArchivedAt           *time.Time `json:"archivedAt"`
	}

	AccountExportMemory struct {
		WorkspaceID string    `json:"workspaceId"`
		Name        string    `json:"name"`
		Content     string    `json:"content"`
		UpdatedAt   time.Time `json:"updatedAt"`
	}

	AccountExportPanoDoc struct {
		ID        string    `json:"id"`
		Title     string    `json:"title"`
		Markdown  string    `json:"markdown"`
		CreatedAt time.Time `json:"createdAt"`
		UpdatedAt time.Time `json:"updatedAt"`
	}

	AccountExportTask struct {
		ID           string                 `json:"id"`
		WorkspaceID  string                 `json:"workspaceId"`
		ParentID     string                 `json:"parentId"`
		Title        string                 `json:"title"`
		Body         string                 `json:"body"`
		Status       string                 `json:"status"`
		CreatedBy    string                 `json:"createdBy"`
		Assignee     string                 `json:"assignee"`
		CronSchedule string                 `json:"cronSchedule"`
		CreatedAt    time.Time              `json:"createdAt"`
		UpdatedAt    time.Time              `json:"updatedAt"`
		Messages     []AccountExportMessage `json:"messages"`
	}

	AccountExportMessage struct {
		Sender    string    `json:"sender"`
		Text      string    `json:"text"`
		CreatedAt time.Time `json:"createdAt"`
	}
)
