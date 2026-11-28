// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package crud

import "time"

// AccountExportVersion is the shape of an export; it changes when a field
// changes meaning or goes, not when one is added.
const AccountExportVersion = 1

type (
	// ExportAccountDataRequest asks for everything the account owns. Email
	// and Name are the signed-in person's, as their session says.
	ExportAccountDataRequest struct {
		UserID string
		Email  string
		Name   string
	}

	// AccountExport is an account's data: what it is, its settings, and
	// what it made in every workspace, archived ones too.
	AccountExport struct {
		Version    int
		ExportedAt time.Time
		Profile    AccountExportProfile
		Settings   AccountSettings
		Presets    []AccountExportPreset
		Workspaces []AccountExportWorkspace
		Memories   []AccountExportMemory
		PanoDocs   []AccountExportPanoDoc
		Tasks      []AccountExportTask
	}

	AccountExportProfile struct {
		ID    int64
		Email string
		Name  string
	}

	// AccountExportPreset is one preset; WorkspaceID zero is the account's
	// own.
	AccountExportPreset struct {
		WorkspaceID int64
		Name        string
		Body        string
		CreatedAt   time.Time
	}

	AccountExportWorkspace struct {
		ID                   int64
		Name                 string
		Description          string
		Icon                 string
		WorkingDirectory     string
		SelfLearningLoopNote string
		ForkOfID             int64
		CreatedAt            time.Time
		ArchivedAt           *time.Time
	}

	AccountExportMemory struct {
		WorkspaceID int64
		Name        string
		Content     string
		UpdatedAt   time.Time
	}

	AccountExportPanoDoc struct {
		ID        int64
		Title     string
		Markdown  string
		CreatedAt time.Time
		UpdatedAt time.Time
	}

	AccountExportTask struct {
		ID           int64
		WorkspaceID  int64
		ParentID     int64
		Title        string
		Body         string
		Status       string
		CreatedBy    string
		Assignee     string
		CronSchedule string
		CreatedAt    time.Time
		UpdatedAt    time.Time
		Messages     []AccountExportMessage
	}

	AccountExportMessage struct {
		Sender    string
		Text      string
		CreatedAt time.Time
	}
)
