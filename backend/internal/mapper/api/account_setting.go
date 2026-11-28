// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package api

import (
	"encoding/json"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	view "github.com/agentrq/agentrq/backend/internal/data/view/api"
	"github.com/gofiber/fiber/v3"
)

func FromHTTPRequestToUpdateAccountSettingsRequestEntity(c fiber.Ctx) *entity.UpdateAccountSettingsRequest {
	var payload view.UpdateAccountSettingsRequest
	if err := json.Unmarshal(c.Body(), &payload); err != nil {
		return nil
	}
	settings := entity.AccountSettings{}
	if d := payload.Display; d != nil {
		display := map[string]string{}
		if d.TimeFormat != nil {
			display[entity.SettingTimeFormat] = *d.TimeFormat
		}
		settings[entity.SettingsDisplay] = display
	}
	return &entity.UpdateAccountSettingsRequest{Settings: settings}
}

func FromAccountSettingsResponseEntityToHTTPResponse(rs *entity.AccountSettingsResponse) []byte {
	display := rs.Settings[entity.SettingsDisplay]
	payload, _ := json.Marshal(view.AccountSettingsResponse{Settings: view.AccountSettings{
		Display: view.DisplaySettings{TimeFormat: display[entity.SettingTimeFormat]},
	}})
	return payload
}
