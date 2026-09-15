// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gofiber/fiber/v2"
	"github.com/mustafaturan/monoflake"
	zlog "github.com/rs/zerolog/log"

	_crud "github.com/agentrq/agentrq/backend/internal/controller/crud"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	mapper "github.com/agentrq/agentrq/backend/internal/mapper/api"
	"github.com/agentrq/agentrq/daemon/wire"
)

const (
	_routePathMachines      = "/machines"
	_routePathMachineEnrol  = "/machines/enroll"
	_routePathMachineCodes  = "/machines/codes"
	_routePathMachineUpdate = "/machines/:id/update"
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
	h.router.Get(_routePathMachines, h.listMachines())
	h.router.Get(_routePathMachines+"/:id", h.getMachine())
	h.router.Patch(_routePathMachines+"/:id", h.updateMachine())
	h.router.Delete(_routePathMachines+"/:id", h.deleteMachine())
	h.router.Post(_routePathMachineUpdate, h.approveMachineUpdate())
}

// approveMachineUpdate tells a daemon to install the release it offered.
//
// This is the one request in the product that deliberately destroys work in
// progress: the daemon stops every session on that machine, replaces itself,
// and starts them again as new processes with empty terminals. The UI says so
// in those words before it gets here, and the version is required so that
// "yes" means yes to a particular release rather than to whatever the feed
// offers by the time the daemon looks.
func (h *handler) approveMachineUpdate() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)

		var payload struct {
			Version string `json:"version"`
		}
		if err := c.BodyParser(&payload); err != nil {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}

		ctx, cancel := newContext(c)
		defer cancel()

		approved, err := h.crud.ApproveMachineUpdate(ctx, entity.ApproveMachineUpdateRequest{
			UserID:    c.Locals("user_id").(string),
			MachineID: c.Params("id"),
			Version:   payload.Version,
		})
		if err != nil {
			if errors.Is(err, _crud.ErrNoUpdateOffered) {
				c.Status(http.StatusConflict)
				return c.Send(mapper.FromMessageToHTTPResponse(err.Error(), http.StatusConflict))
			}
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}

		if h.machineRegistry == nil {
			c.Status(http.StatusServiceUnavailable)
			return c.Send(mapper.FromMessageToHTTPResponse(
				"machine connections are not available on this server", http.StatusServiceUnavailable))
		}
		if _, err := h.machineRegistry.Get(approved.MachineID); err != nil {
			c.Status(http.StatusConflict)
			return c.Send(mapper.FromMessageToHTTPResponse(
				"that machine is not connected", http.StatusConflict))
		}

		body, err := json.Marshal(wire.UpdateNow{Version: approved.Version})
		if err != nil {
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}
		frame, err := wire.ControlFrame(wire.Control{Op: wire.OpUpdateNow, Body: body})
		if err != nil {
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}
		if err := h.machineRegistry.Send(approved.MachineID, frame); err != nil {
			zlog.Error().Err(err).Int64("machine_id", approved.MachineID).Msg("[machine] could not send the approval")
			c.Status(http.StatusBadGateway)
			return c.Send(mapper.FromMessageToHTTPResponse(
				"could not reach that machine", http.StatusBadGateway))
		}

		// Audited, because this destroys work that somebody else may be in the
		// middle of. Who approved it, for which machine, to which version.
		zlog.Info().
			Str("user_id", c.Locals("user_id").(string)).
			Int64("machine_id", approved.MachineID).
			Str("version", approved.Version).
			Msg("[audit] agentrqd update approved")

		c.Status(http.StatusAccepted)
		return nil
	}
}

func (h *handler) listMachines() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		ctx, cancel := newContext(c)
		defer cancel()

		rs, err := h.crud.ListMachines(ctx, entity.ListMachinesRequest{
			UserID: c.Locals("user_id").(string),
		})
		if err != nil {
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}
		return c.JSON(rs)
	}
}

func (h *handler) getMachine() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		ctx, cancel := newContext(c)
		defer cancel()

		rs, err := h.crud.GetMachine(ctx, entity.GetMachineRequest{
			UserID:    c.Locals("user_id").(string),
			MachineID: c.Params("id"),
		})
		if err != nil {
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}
		return c.JSON(rs)
	}
}

// updateMachine renames a machine or flips its kill switch.
func (h *handler) updateMachine() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)

		// Pointers, so "not mentioned" and "set to empty/false" stay different
		// requests. Without that, a rename would silently re-enable a machine
		// somebody had deliberately turned off.
		var payload struct {
			Name    *string `json:"name"`
			Enabled *bool   `json:"enabled"`
		}
		if err := c.BodyParser(&payload); err != nil {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}

		ctx, cancel := newContext(c)
		defer cancel()

		rs, err := h.crud.UpdateMachine(ctx, entity.UpdateMachineRequest{
			UserID:    c.Locals("user_id").(string),
			MachineID: c.Params("id"),
			Name:      payload.Name,
			Enabled:   payload.Enabled,
		})
		if err != nil {
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}

		// Disabling is a kill switch, and a kill switch that waits for the
		// other end to cooperate is not one. The database change alone would
		// leave a connected daemon running until it next authenticated, so the
		// socket is closed from this side now.
		if payload.Enabled != nil && !*payload.Enabled {
			h.dropMachineSocket(rs.Machine.ID)
		}
		return c.JSON(rs)
	}
}

func (h *handler) deleteMachine() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		ctx, cancel := newContext(c)
		defer cancel()

		id := c.Params("id")
		if err := h.crud.DeleteMachine(ctx, entity.DeleteMachineRequest{
			UserID:    c.Locals("user_id").(string),
			MachineID: id,
		}); err != nil {
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}

		// Same reasoning as disabling: the row going is what makes the token
		// unusable, but a daemon already connected would keep running until it
		// next tried to authenticate.
		h.dropMachineSocket(id)

		c.Status(http.StatusNoContent)
		return nil
	}
}

// dropMachineSocket closes a machine's socket if this instance holds it.
//
// Only this instance's — another one holding the socket will not be reached
// here. That is a known limit of revoking across instances and is why a
// disabled machine is also refused at the next authentication: the socket
// closing is the fast path, not the guarantee.
func (h *handler) dropMachineSocket(machineID string) {
	if h.machineRegistry == nil {
		return
	}
	if id := monoflake.IDFromBase62(machineID).Int64(); id != 0 {
		h.machineRegistry.Drop(id)
	}
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
