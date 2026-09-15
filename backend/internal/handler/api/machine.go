// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package api

import (
	"errors"
	"net/http"

	"github.com/gofiber/fiber/v2"

	_crud "github.com/agentrq/agentrq/backend/internal/controller/crud"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	mapper "github.com/agentrq/agentrq/backend/internal/mapper/api"
)

const (
	_routePathMachineEnrol = "/machines/enroll"
	_routePathMachineCodes = "/machines/codes"
)

// registerPublicMachineRoutes exposes the one machine route that cannot be
// authenticated.
//
// **It must be registered before h.router.Use(h.authMiddleware()).** Order is
// what makes a route public here, not a flag on the route — registering this
// after the middleware would put it behind a session cookie, and a daemon
// enrolling for the first time does not have one. That is the whole point of a
// code: it is the credential, and it is the only one the caller has.
func (h *handler) registerPublicMachineRoutes() {
	h.router.Post(_routePathMachineEnrol, h.enrolMachine())
}

// registerMachineRoutes exposes the routes a signed-in person uses.
func (h *handler) registerMachineRoutes() {
	h.router.Post(_routePathMachineCodes, h.createEnrolmentCode())
}

// createEnrolmentCode hands out a short code to type on a machine.
func (h *handler) createEnrolmentCode() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)

		ctx, cancel := newContext(c)
		defer cancel()

		rs, err := h.crud.CreateEnrolmentCode(ctx, entity.CreateEnrolmentCodeRequest{
			UserID: c.Locals("user_id").(string),
		})
		if err != nil {
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}

		// The code is in this response and nowhere else — only its hash is
		// stored, so this is the single moment it can be shown.
		c.Status(http.StatusCreated)
		return c.JSON(rs)
	}
}

// enrolMachine trades a code for a machine identity.
func (h *handler) enrolMachine() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)

		rq := mapper.FromHTTPRequestToEnrolMachineRequestEntity(c)
		if rq == nil {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}

		ctx, cancel := newContext(c)
		defer cancel()

		rs, err := h.crud.EnrolMachine(ctx, *rq)
		if err != nil {
			if errors.Is(err, _crud.ErrEnrolmentRejected) {
				// One answer for every reason. "Unknown", "expired" and
				// "already used" are different facts, and telling them apart
				// lets someone holding a stolen code learn whether it was ever
				// real. The person who just typed it fetches a new code in all
				// three cases.
				c.Status(http.StatusUnauthorized)
				return c.Send(mapper.FromMessageToHTTPResponse(
					"enrolment code rejected — fetch a new one from Machines → Add machine",
					http.StatusUnauthorized))
			}
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}

		c.Status(http.StatusCreated)
		return c.JSON(rs)
	}
}
