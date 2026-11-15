// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	view "github.com/agentrq/agentrq/backend/internal/data/view/api"
	"github.com/gofiber/fiber/v2"
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
func FromHTTPRequestToUpdateAppConfigurationRequestEntity(c *fiber.Ctx) *entity.UpdateAppConfigurationRequest {
	var rq view.UpdateAppConfigurationRequest
	if err := json.Unmarshal(c.Body(), &rq); err != nil || len(rq.Value) == 0 || string(rq.Value) == "null" {
		return nil
	}
	return &entity.UpdateAppConfigurationRequest{Key: c.Params("key"), Value: rq.Value}
}
