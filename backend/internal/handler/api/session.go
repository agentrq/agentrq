// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gofiber/fiber/v3"
	"github.com/mustafaturan/monoflake"

	machinectrl "github.com/agentrq/agentrq/backend/internal/controller/machine"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	mapper "github.com/agentrq/agentrq/backend/internal/mapper/api"
	"github.com/agentrq/agentrq/backend/internal/service/auth"
)

const (
	_routePathMachineSessions = "/machines/:id/sessions"
	_routePathSession         = "/sessions/:id"
	// Deliberately a child of the socket's own path rather than a sibling.
	// The socket lives on the stdlib mux, which matches exact paths only, so
	// this longer path falls through to Fiber — see the routing note in
	// backend/AGENTS.md, where getting this wrong hangs the request instead of
	// failing it.
	_routePathSessionTerminalTicket = "/sessions/:id/terminal/ticket"
)

func (h *handler) registerSessionRoutes() {
	h.router.Get(_routePathMachineSessions, h.listSessions())
	h.router.Get(_routePathSession, h.getSession())
	h.router.Delete(_routePathSession, h.killSession())
	h.router.Post(_routePathSessionTerminalTicket, h.terminalTicket())
}

// terminalTicket mints the credential a page presents to the terminal socket.
//
// The socket cannot read the `at` cookie in every build that has one. Every
// other API call the frontend makes is a same-origin relative URL, which is
// what lets the desktop app forward it through its app:// handler with the
// cookie attached in the main process. A WebSocket cannot be forwarded that
// way — Electron's handler does not intercept upgrades — so the socket URL is
// absolute, the upgrade is cross-site, and the browser withholds a
// SameSite=Lax cookie from it. The desktop terminal therefore never connected
// at all.
//
// This route is an ordinary cookie-authenticated POST, so it goes through that
// forwarding like everything else, and hands back a credential the page can
// present explicitly. The authorisation is the read below and nothing else:
// whoever may see this session may watch its terminal, which is the same rule
// the socket applies.
func (h *handler) terminalTicket() fiber.Handler {
	return func(c fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		ctx, cancel := newContext(c)
		defer cancel()

		userID := c.Locals("user_id").(string)

		// Scoped to the caller, and read before anything is minted: this is
		// what decides whether they may watch this session at all. A ticket
		// minted first and checked later would be a credential that existed,
		// however briefly, without a reason.
		rs, err := h.crud.GetSession(ctx, entity.GetSessionRequest{
			UserID:    userID,
			SessionID: c.Params("id"),
		})
		if err != nil {
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}

		// The numeric form, because that is what the socket has: its router
		// hands it a uint64 and it never sees the base62 spelling. Derived on
		// both sides from the same decode rather than round-tripped through
		// base62, so the two cannot disagree about what this session is
		// called.
		sessionID := strconv.FormatInt(monoflake.IDFromBase62(rs.Session.ID).Int64(), 10)

		ticket, err := h.tokenSvc.CreateTerminalTicket(userID, sessionID)
		if err != nil {
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}

		return c.JSON(entity.TerminalTicketResponse{
			Ticket:    ticket,
			ExpiresIn: int(auth.TerminalTicketTTL.Seconds()),
		})
	}
}

// getSession returns one session.
//
// The terminal page reads this so it can say what it is showing — which agent,
// in which workspace, and whether it is still running. Without it the page can
// only show a rectangle and hope, and a session that has ended is
// indistinguishable from one that is quiet.
func (h *handler) getSession() fiber.Handler {
	return func(c fiber.Ctx) error {
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
	return func(c fiber.Ctx) error {
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
	return func(c fiber.Ctx) error {
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
		if err := machinectrl.KillSession(h.machineRegistry, machineID, rs.Session.ID); err != nil {
			status, msg := http.StatusBadGateway, err.Error()
			switch {
			case errors.Is(err, machinectrl.ErrNoConnections):
				status = http.StatusServiceUnavailable
			case errors.Is(err, machinectrl.ErrNotConnected):
				status, msg = http.StatusConflict, "that machine is not connected"
			}
			c.Status(status)
			return c.Send(mapper.FromMessageToHTTPResponse(msg, status))
		}

		// Counted only once the request is on its way to the machine. A kill
		// that was refused above never happened, and counting the attempt
		// would make the number mean something else.
		h.crud.RecordSessionKill(ctx, entity.RecordSessionKillRequest{
			UserID:      userID,
			WorkspaceID: rs.Session.WorkspaceID,
			SessionID:   rs.Session.ID,
		})

		c.Status(http.StatusAccepted)
		return nil
	}
}
