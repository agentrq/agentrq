// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package crud

import "encoding/json"

// AppConfiguration is one block of the server's config, as stored: Value is
// a JSON object.
type AppConfiguration struct {
	Key   string
	Value json.RawMessage
}

// UpdateAppConfigurationRequest replaces the block Key with Value, a JSON
// object.
type UpdateAppConfigurationRequest struct {
	Key   string
	Value json.RawMessage
}

// AppConfigurationError is a block refused, with a message for the operator.
type AppConfigurationError struct {
	Message string
}

func (e *AppConfigurationError) Error() string { return e.Message }

// UploadLoginIconRequest is the image a sign-in provider's button shows, as
// uploaded: a PNG, JPEG, GIF or WebP.
type UploadLoginIconRequest struct {
	Data []byte
}

// UploadLoginIconResponse is where the icon, resized, can be read.
type UploadLoginIconResponse struct {
	URL string
}
