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

// registerGitLabRoutes name the GitLab skills are imported from, and connect
// the account to the server's GitLab application, for its private projects.
// As with GitHub, GitLab returns the person to the web app, which posts what
// GitLab sent with the session cookie.
func (h *handler) registerGitLabRoutes() {
	r := h.router.Group("/gitlab")
	r.Get("", h.getGitLabConnection())
	r.Post("/connection", h.connectGitLab())
	r.Delete("/connection", h.disconnectGitLab())
}

func (h *handler) getGitLabConnection() fiber.Handler {
	return func(c fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		ctx, cancel := newContext(c)
		defer cancel()
		rs, err := h.crud.GetGitLabConnection(ctx, entity.GetGitLabConnectionRequest{UserID: c.Locals("user_id").(string)})
		if err != nil {
			return sendSkillError(c, err)
		}
		return c.Send(mapper.FromGitLabConnectionResponseEntityToHTTPResponse(rs))
	}
}

func (h *handler) connectGitLab() fiber.Handler {
	return func(c fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		rq := mapper.FromHTTPRequestToConnectGitLabRequestEntity(c)
		if rq == nil {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}
		rq.UserID = c.Locals("user_id").(string)
		ctx, cancel := newContext(c)
		defer cancel()
		rs, err := h.crud.ConnectGitLab(ctx, *rq)
		if err != nil {
			return sendSkillError(c, err)
		}
		return c.Send(mapper.FromGitLabConnectionResponseEntityToHTTPResponse(rs))
	}
}

func (h *handler) disconnectGitLab() fiber.Handler {
	return func(c fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		ctx, cancel := newContext(c)
		defer cancel()
		if err := h.crud.DisconnectGitLab(ctx, entity.DisconnectGitLabRequest{UserID: c.Locals("user_id").(string)}); err != nil {
			return sendSkillError(c, err)
		}
		c.Status(http.StatusNoContent)
		return nil
	}
}
