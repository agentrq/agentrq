// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package api

import (
	"net/http"

	mapper "github.com/agentrq/agentrq/backend/internal/mapper/api"
	"github.com/gofiber/fiber/v3"
	zlog "github.com/rs/zerolog/log"
)

// getTaskLatencyStats serves how long closed tasks took, for one workspace
// (/workspaces/:id/stats/latency) or the whole account (/stats/latency).
func (h *handler) getTaskLatencyStats(workspaceScoped bool) fiber.Handler {
	return func(c fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		rq := mapper.FromHTTPRequestToGetTaskLatencyStatsRequestEntity(c, workspaceScoped)
		if rq == nil {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}
		rq.UserID = c.Locals("user_id").(string)

		ctx, cancel := newContext(c)
		defer cancel()
		rs, err := h.crud.GetTaskLatencyStats(ctx, *rq)
		if err != nil {
			zlog.Error().Err(err).Msg("Failed to get task latency stats")
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}
		return c.Status(http.StatusOK).Send(mapper.FromGetTaskLatencyStatsResponseEntityToHTTPResponse(rs))
	}
}
