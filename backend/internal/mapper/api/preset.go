// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package api

import (
	"encoding/json"
	"net/url"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	view "github.com/agentrq/agentrq/backend/internal/data/view/api"
	"github.com/gofiber/fiber/v3"
	"github.com/mustafaturan/monoflake"
)

func FromHTTPRequestToListPresetsRequestEntity(c fiber.Ctx) *entity.ListPresetsRequest {
	workspaceID := monoflake.IDFromBase62(c.Params("id")).Int64()
	if workspaceID == 0 {
		return nil
	}
	return &entity.ListPresetsRequest{WorkspaceID: workspaceID}
}

func FromHTTPRequestToCreatePresetRequestEntity(c fiber.Ctx) *entity.CreatePresetRequest {
	workspaceID := monoflake.IDFromBase62(c.Params("id")).Int64()
	if workspaceID == 0 {
		return nil
	}
	var payload view.CreatePresetRequest
	if err := json.Unmarshal(c.Body(), &payload); err != nil {
		return nil
	}
	return &entity.CreatePresetRequest{WorkspaceID: workspaceID, Name: payload.Name, Body: payload.Body}
}

func FromHTTPRequestToDeletePresetRequestEntity(c fiber.Ctx) *entity.DeletePresetRequest {
	workspaceID := monoflake.IDFromBase62(c.Params("id")).Int64()
	if workspaceID == 0 {
		return nil
	}
	// Fiber hands back the raw path segment.
	name, err := url.PathUnescape(c.Params("name"))
	if err != nil {
		return nil
	}
	return &entity.DeletePresetRequest{WorkspaceID: workspaceID, Name: name}
}

func FromHTTPRequestToUpdatePresetRequestEntity(c fiber.Ctx) *entity.UpdatePresetRequest {
	workspaceID := monoflake.IDFromBase62(c.Params("id")).Int64()
	if workspaceID == 0 {
		return nil
	}
	name, err := url.PathUnescape(c.Params("name"))
	if err != nil {
		return nil
	}
	var payload view.UpdatePresetRequest
	if err := json.Unmarshal(c.Body(), &payload); err != nil {
		return nil
	}
	return &entity.UpdatePresetRequest{WorkspaceID: workspaceID, Name: name, NewName: payload.Name, Body: payload.Body}
}

func FromListPresetsResponseEntityToHTTPResponse(rs *entity.ListPresetsResponse) []byte {
	presets := make([]view.Preset, len(rs.Presets))
	for i, p := range rs.Presets {
		presets[i] = fromEntityPresetToView(p)
	}
	payload, _ := json.Marshal(view.ListPresetsResponse{Presets: presets})
	return payload
}

func FromCreatePresetResponseEntityToHTTPResponse(rs *entity.CreatePresetResponse) []byte {
	payload, _ := json.Marshal(view.CreatePresetResponse{Preset: fromEntityPresetToView(rs.Preset)})
	return payload
}

func FromUpdatePresetResponseEntityToHTTPResponse(rs *entity.UpdatePresetResponse) []byte {
	payload, _ := json.Marshal(view.UpdatePresetResponse{Preset: fromEntityPresetToView(rs.Preset)})
	return payload
}

func fromEntityPresetToView(p entity.Preset) view.Preset {
	return view.Preset{
		ID:          monoflake.ID(p.ID).String(),
		CreatedAt:   p.CreatedAt,
		WorkspaceID: monoflake.ID(p.WorkspaceID).String(),
		Name:        p.Name,
		Body:        p.Body,
	}
}
