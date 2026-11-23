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

// FromAppConfigurationEntitiesToHTTPResponse renders the stored config blocks.
func FromAppConfigurationEntitiesToHTTPResponse(es []entity.AppConfiguration) []byte {
	out := view.ListAppConfigurationsResponse{Configurations: make([]view.AppConfiguration, len(es))}
	for i, e := range es {
		out.Configurations[i] = view.AppConfiguration{Key: e.Key, Value: e.Value}
	}
	b, _ := json.Marshal(out)
	return b
}

// FromAppConfigurationEntityToHTTPResponse renders one config block.
func FromAppConfigurationEntityToHTTPResponse(e *entity.AppConfiguration) []byte {
	b, _ := json.Marshal(view.AppConfiguration{Key: e.Key, Value: e.Value})
	return b
}

// FromHTTPRequestToUpdateAppConfigurationRequestEntity reads the block named
// by :key from the body; nil when the body is not JSON or has no value.
func FromHTTPRequestToUpdateAppConfigurationRequestEntity(c fiber.Ctx) *entity.UpdateAppConfigurationRequest {
	var rq view.UpdateAppConfigurationRequest
	if err := json.Unmarshal(c.Body(), &rq); err != nil || len(rq.Value) == 0 || string(rq.Value) == "null" {
		return nil
	}
	return &entity.UpdateAppConfigurationRequest{Key: c.Params("key"), Value: rq.Value}
}

// FromHTTPRequestToUploadLoginIconRequestEntity reads the image the body
// carries; nil when it is empty.
func FromHTTPRequestToUploadLoginIconRequestEntity(c fiber.Ctx) *entity.UploadLoginIconRequest {
	if len(c.Body()) == 0 {
		return nil
	}
	return &entity.UploadLoginIconRequest{Data: append([]byte(nil), c.Body()...)}
}

// FromUploadLoginIconResponseEntityToHTTPResponse renders the icon's link.
func FromUploadLoginIconResponseEntityToHTTPResponse(rs *entity.UploadLoginIconResponse) []byte {
	b, _ := json.Marshal(view.UploadLoginIconResponse{URL: rs.URL})
	return b
}
