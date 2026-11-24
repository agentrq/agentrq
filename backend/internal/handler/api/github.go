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

// registerGitHubRoutes connects the account to the server's GitHub App, for
// importing skills from private repositories. GitHub returns the person to
// the web app, never to these routes: the app posts what GitHub sent with the
// session cookie, so a connect is always finished by the account it is for.
func (h *handler) registerGitHubRoutes() {
	r := h.router.Group("/github")
	r.Get("", h.getGitHubConnection())
	r.Post("/installations", h.connectGitHub())
	r.Delete("/installations/:id", h.disconnectGitHub())
}

func (h *handler) getGitHubConnection() fiber.Handler {
	return func(c fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		ctx, cancel := newContext(c)
		defer cancel()
		rs, err := h.crud.GetGitHubConnection(ctx, entity.GetGitHubConnectionRequest{UserID: c.Locals("user_id").(string)})
		if err != nil {
			return sendSkillError(c, err)
		}
		return c.Send(mapper.FromGitHubConnectionResponseEntityToHTTPResponse(rs))
	}
}

func (h *handler) connectGitHub() fiber.Handler {
	return func(c fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		rq := mapper.FromHTTPRequestToConnectGitHubRequestEntity(c)
		if rq == nil {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}
		rq.UserID = c.Locals("user_id").(string)
		ctx, cancel := newContext(c)
		defer cancel()
		rs, err := h.crud.ConnectGitHub(ctx, *rq)
		if err != nil {
			return sendSkillError(c, err)
		}
		return c.Send(mapper.FromGitHubConnectionResponseEntityToHTTPResponse(rs))
	}
}

func (h *handler) disconnectGitHub() fiber.Handler {
	return func(c fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		rq := mapper.FromHTTPRequestToDisconnectGitHubRequestEntity(c)
		if rq == nil {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}
		rq.UserID = c.Locals("user_id").(string)
		ctx, cancel := newContext(c)
		defer cancel()
		if err := h.crud.DisconnectGitHub(ctx, *rq); err != nil {
			return sendSkillError(c, err)
		}
		c.Status(http.StatusNoContent)
		return nil
	}
}
