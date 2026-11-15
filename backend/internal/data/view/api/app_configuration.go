// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import "encoding/json"

type (
	// AppConfiguration is one block of the server's config: Value is its
	// settings, as a JSON object.
	AppConfiguration struct {
		Key   string          `json:"key"`
		Value json.RawMessage `json:"value"`
	}

	ListAppConfigurationsResponse struct {
		Configurations []AppConfiguration `json:"configurations"`
	}

	UpdateAppConfigurationRequest struct {
		Value json.RawMessage `json:"value"`
	}
)

// UploadLoginIconResponse is the public link of an uploaded sign-in icon.
type UploadLoginIconResponse struct {
	URL string `json:"url"`
}

// OAuth2LoginProvider is a sign-in button the login page shows.
type OAuth2LoginProvider struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	IconURL string `json:"iconUrl"`
}
