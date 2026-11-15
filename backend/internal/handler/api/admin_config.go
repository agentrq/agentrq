// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"errors"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	mapper "github.com/agentrq/agentrq/backend/internal/mapper/api"
	"github.com/gofiber/fiber/v2"
	zlog "github.com/rs/zerolog/log"
)

// registerAdminConfigRoutes adds the stored config, one block per yaml
// namespace. A saved block takes effect when the server next starts.
func (h *handler) registerAdminConfigRoutes(r fiber.Router) {
	if h.appConfig == nil {
		return
	}
	r.Get("/configurations", h.listAppConfigurations())
	r.Put("/configurations/:key", h.updateAppConfiguration())
}

func (h *handler) listAppConfigurations() fiber.Handler {
	return func(c *fiber.Ctx) error {
		ctx, cancel := newContext(c)
		defer cancel()
		blocks, err := h.appConfig.List(ctx)
		if err != nil {
			return appConfigError(c, err)
		}
		c.Set(_headerContentType, _mimeJSON)
		return c.Send(mapper.FromAppConfigurationEntitiesToHTTPResponse(blocks))
	}
}

func (h *handler) updateAppConfiguration() fiber.Handler {
	return func(c *fiber.Ctx) error {
		rq := mapper.FromHTTPRequestToUpdateAppConfigurationRequestEntity(c)
		if rq == nil {
			return c.Status(fiber.StatusBadRequest).Send(_invalidPayload)
		}
		ctx, cancel := newContext(c)
		defer cancel()
		block, err := h.appConfig.Update(ctx, *rq)
		if err != nil {
			return appConfigError(c, err)
		}
		c.Set(_headerContentType, _mimeJSON)
		return c.Send(mapper.FromAppConfigurationEntityToHTTPResponse(block))
	}
}

// appConfigError answers a refused block with its reason, which names the
// setting at fault, and anything else as the generic error.
func appConfigError(c *fiber.Ctx, err error) error {
	c.Set(_headerContentType, _mimeJSON)
	var refused *entity.AppConfigurationError
	if errors.As(err, &refused) {
		return c.Status(fiber.StatusUnprocessableEntity).Send(mapper.FromMessageToHTTPResponse(refused.Message, fiber.StatusUnprocessableEntity))
	}
	zlog.Error().Err(err).Msg("admin config")
	e, status := mapper.FromErrorToHTTPResponse(err)
	return c.Status(status).Send(e)
}
