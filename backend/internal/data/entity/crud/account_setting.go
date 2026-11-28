// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package crud

// An account's settings come in groups, each stored by its name as one JSON
// object of fields, named in camelCase as the API names them.
const (
	// SettingsDisplay is how the app shows things.
	SettingsDisplay = "display_settings"
	// SettingTimeFormat, in SettingsDisplay, is the clock every time in the
	// app is shown with.
	SettingTimeFormat = "timeFormat"
)

// The values of SettingTimeFormat.
const (
	TimeFormatBrowser = "browser"
	TimeFormat12h     = "12h"
	TimeFormat24h     = "24h"
)

// AccountSettings are an account's settings: group name to field name to
// value.
type AccountSettings map[string]map[string]string

type (
	GetAccountSettingsRequest struct {
		UserID string
	}

	// UpdateAccountSettingsRequest saves the fields in Settings; every other
	// field keeps its value.
	UpdateAccountSettingsRequest struct {
		UserID   string
		Settings AccountSettings
	}

	// AccountSettingsResponse holds every field of every group, each with
	// the account's value or its default.
	AccountSettingsResponse struct {
		Settings AccountSettings
	}
)

// AccountSettingsError is a setting the server refuses, with the sentence to
// show its caller.
type AccountSettingsError struct {
	Message string
}

func (e *AccountSettingsError) Error() string { return e.Message }
