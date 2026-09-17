// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package api

import (
	"encoding/json"
	"net/http"

	"github.com/gofiber/fiber/v2"
	"github.com/mustafaturan/monoflake"
	zlog "github.com/rs/zerolog/log"

	machinectrl "github.com/agentrq/agentrq/backend/internal/controller/machine"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	mapper "github.com/agentrq/agentrq/backend/internal/mapper/api"
	"github.com/agentrq/agentrq/daemon/wire"
)

const (
	_routePathMachineSessions = "/machines/:id/sessions"
	_routePathSession         = "/sessions/:id"
)

func (h *handler) registerSessionRoutes() {
	h.router.Get(_routePathMachineSessions, h.listSessions())
	h.router.Get(_routePathSession, h.getSession())
	h.router.Delete(_routePathSession, h.killSession())
}

// getSession returns one session.
//
// The terminal page reads this so it can say what it is showing — which agent,
// in which workspace, and whether it is still running. Without it the page can
// only show a rectangle and hope, and a session that has ended is
// indistinguishable from one that is quiet.
func (h *handler) getSession() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		ctx, cancel := newContext(c)
		defer cancel()

		rs, err := h.crud.GetSession(ctx, entity.GetSessionRequest{
			UserID:    c.Locals("user_id").(string),
			SessionID: c.Params("id"),
		})
		if err != nil {
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}
		return c.JSON(rs)
	}
}

// listSessions returns a machine's sessions, newest first.
func (h *handler) listSessions() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		ctx, cancel := newContext(c)
		defer cancel()

		rs, err := h.crud.ListSessions(ctx, entity.ListSessionsRequest{
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

// killSession asks the daemon to end a session.
//
// The row is not marked killed here. The daemon reports what actually
// happened, and a row that says "killed" for a process still running would be
// worse than one that takes a moment to catch up — the kill switch people
// trust is the one that never lies about having worked.
func (h *handler) killSession() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		ctx, cancel := newContext(c)
		defer cancel()

		userID := c.Locals("user_id").(string)

		// Read first, scoped to the caller: this is what decides whether they
		// may touch this session at all, and it also names the machine to send
		// the request to.
		rs, err := h.crud.GetSession(ctx, entity.GetSessionRequest{
			UserID:    userID,
			SessionID: c.Params("id"),
		})
		if err != nil {
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}

		if machinectrl.SessionTerminal(rs.Session.Status) {
			// Already over. Answered as success rather than as an error: the
			// caller asked for it to be dead, and it is.
			c.Status(http.StatusNoContent)
			return nil
		}

		machineID := monoflake.IDFromBase62(rs.Session.MachineID).Int64()
		if h.machineRegistry == nil {
			c.Status(http.StatusServiceUnavailable)
			return c.Send(mapper.FromMessageToHTTPResponse(
				"machine connections are not available on this server", http.StatusServiceUnavailable))
		}
		if _, err := h.machineRegistry.Get(machineID); err != nil {
			c.Status(http.StatusConflict)
			return c.Send(mapper.FromMessageToHTTPResponse(
				"that machine is not connected", http.StatusConflict))
		}

		body, err := json.Marshal(wire.KillSession{
			SessionID: uint64(monoflake.IDFromBase62(rs.Session.ID).Int64()),
		})
		if err != nil {
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}
		frame, err := wire.ControlFrame(wire.Control{Op: wire.OpKillSession, Body: body})
		if err != nil {
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}
		if err := h.machineRegistry.Send(machineID, frame); err != nil {
			zlog.Error().Err(err).Str("session", rs.Session.ID).Msg("[session] could not reach the machine")
			c.Status(http.StatusBadGateway)
			return c.Send(mapper.FromMessageToHTTPResponse(
				"could not reach that machine", http.StatusBadGateway))
		}

		c.Status(http.StatusAccepted)
		return nil
	}
}
