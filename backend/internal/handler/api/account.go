// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package api

import (
	"net/http"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	mapper "github.com/agentrq/agentrq/backend/internal/mapper/api"
	"github.com/gofiber/fiber/v3"
)

// registerAccountRoutes serves what holds in every workspace of an account:
// its settings, and its own presets. The preset handlers are the workspace
// ones; with no :id in the path they work on the account's.
func (h *handler) registerAccountRoutes() {
	r := h.router.Group("/account")
	r.Get("/settings", h.getAccountSettings())
	r.Put("/settings", h.updateAccountSettings())
	r.Get("/presets", h.listWorkspacePresets())
	r.Post("/presets", h.createWorkspacePreset())
	r.Put("/presets/:name", h.updateWorkspacePreset())
	r.Delete("/presets/:name", h.deleteWorkspacePreset())
}

func (h *handler) getAccountSettings() fiber.Handler {
	return func(c fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		ctx, cancel := newContext(c)
		defer cancel()
		rs, err := h.crud.GetAccountSettings(ctx, entity.GetAccountSettingsRequest{UserID: c.Locals("user_id").(string)})
		if err != nil {
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}
		return c.Send(mapper.FromAccountSettingsResponseEntityToHTTPResponse(rs))
	}
}

func (h *handler) updateAccountSettings() fiber.Handler {
	return func(c fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		rq := mapper.FromHTTPRequestToUpdateAccountSettingsRequestEntity(c)
		if rq == nil {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}
		rq.UserID = c.Locals("user_id").(string)

		ctx, cancel := newContext(c)
		defer cancel()
		rs, err := h.crud.UpdateAccountSettings(ctx, *rq)
		if err != nil {
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}
		return c.Send(mapper.FromAccountSettingsResponseEntityToHTTPResponse(rs))
	}
}
