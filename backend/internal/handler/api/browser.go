// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package api

import (
	"github.com/gofiber/fiber/v3"

	mapper "github.com/agentrq/agentrq/backend/internal/mapper/api"
	"github.com/agentrq/agentrq/backend/internal/service/auth"
)

// A child of the socket's path, so the stdlib mux's exact match on
// /browser/connect leaves it to Fiber.
const _routePathBrowserTicket = "/browser/ticket"

func (h *handler) registerBrowserRoutes() {
	h.router.Post(_routePathBrowserTicket, h.browserTicket())
}

// browserTicket mints the credential the Chrome extension presents to the
// browser socket: the extension's origin never receives the `at` cookie, and
// this cookie-authenticated POST is how it trades the cookie for one.
func (h *handler) browserTicket() fiber.Handler {
	return func(c fiber.Ctx) error {
		ticket, err := h.tokenSvc.CreateBrowserTicket(c.Locals("user_id").(string))
		if err != nil {
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}
		c.Set(_headerContentType, _mimeJSON)
		return c.Send(mapper.FromBrowserTicketToHTTPResponse(ticket, int(auth.BrowserTicketTTL.Seconds())))
	}
}
