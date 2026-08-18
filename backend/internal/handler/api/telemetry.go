package api

import (
	"errors"
	"net/http"

	_crud "github.com/agentrq/agentrq/backend/internal/controller/crud"
	mapper "github.com/agentrq/agentrq/backend/internal/mapper/api"
	"github.com/gofiber/fiber/v2"
)

const _routePathTelemetry = "/telemetry"

// registerTelemetryRoutes exposes the one route the browser may write to.
//
// It belongs on the Fiber router only: the stdlib mux in app.go wins exact
// path matches, so registering it there would shadow this handler.
func (h *handler) registerTelemetryRoutes() error {
	h.router.Post(_routePathTelemetry, h.recordTelemetry())
	return nil
}

func (h *handler) recordTelemetry() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)

		// A rejected mapping means an unknown action name or an unparseable
		// workspace, so nothing is recorded and nothing says which it was —
		// the allowlist is not something to help a caller probe.
		rq := mapper.FromHTTPRequestToRecordTelemetryRequestEntity(c)
		if rq == nil {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}
		rq.UserID = c.Locals("user_id").(string)

		ctx, cancel := newContext(c)
		defer cancel()

		if err := h.crud.RecordTelemetry(ctx, *rq); err != nil {
			if errors.Is(err, _crud.ErrForbidden) {
				c.Status(http.StatusForbidden)
				return c.Send(mapper.FromMessageToHTTPResponse("access denied", http.StatusForbidden))
			}
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}

		// Nothing to return: the caller reports and moves on, and the count is
		// not its business.
		c.Status(http.StatusNoContent)
		return nil
	}
}
