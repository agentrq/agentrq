// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package crud

import "time"

type (
	// Preset is a message kept for a workspace, put in the composer by
	// typing #name.
	Preset struct {
		ID          int64
		CreatedAt   time.Time
		WorkspaceID int64
		Name        string
		Body        string
	}

	ListPresetsRequest struct {
		WorkspaceID int64
		UserID      string
	}

	ListPresetsResponse struct {
		Presets []Preset
	}

	CreatePresetRequest struct {
		WorkspaceID int64
		UserID      string
		Name        string
		Body        string
	}

	CreatePresetResponse struct {
		Preset Preset
	}

	// UpdatePresetRequest replaces the body of the preset called Name, and
	// renames it to NewName when that is set and differs.
	UpdatePresetRequest struct {
		WorkspaceID int64
		UserID      string
		Name        string
		NewName     string
		Body        string
	}

	UpdatePresetResponse struct {
		Preset Preset
	}

	DeletePresetRequest struct {
		WorkspaceID int64
		UserID      string
		Name        string
	}
)

// PresetError is a preset the server refuses, with the sentence to show its
// caller.
type PresetError struct {
	Message string
	// Conflict is a name the workspace already has.
	Conflict bool
}

func (e *PresetError) Error() string { return e.Message }
