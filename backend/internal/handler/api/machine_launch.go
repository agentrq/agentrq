// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/mustafaturan/monoflake"
	zlog "github.com/rs/zerolog/log"

	machinectrl "github.com/agentrq/agentrq/backend/internal/controller/machine"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	mapper "github.com/agentrq/agentrq/backend/internal/mapper/api"
	"github.com/agentrq/agentrq/daemon/wire"
)

const _routePathAgentLaunch = "/workspaces/:id/agent"

// mcpServerName is the entry written into .mcp.json and what `server:<name>`
// refers to on the command line.
//
// Matches the repository's own .mcp.json, so a machine that already has one
// configured by hand keeps working rather than acquiring a second entry
// pointing at the same place.
const mcpServerName = "agentrq-workspace"

func (h *handler) registerAgentLaunchRoutes() {
	h.router.Post(_routePathAgentLaunch, h.launchAgent())
}

// launchAgent starts an agent for a workspace on a chosen machine.
//
// The gates here are the substance, and their order is deliberate: everything
// that can refuse does so before a session row exists or a frame is sent, so a
// refused launch leaves nothing behind to clean up.
func (h *handler) launchAgent() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)

		var payload struct {
			MachineID string `json:"machineId"`
			Kind      string `json:"kind"`
			Model     string `json:"model,omitempty"`
			Agent     string `json:"agent,omitempty"`
			Cols      uint16 `json:"cols,omitempty"`
			Rows      uint16 `json:"rows,omitempty"`
		}
		if err := c.BodyParser(&payload); err != nil {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}

		userID := c.Locals("user_id").(string)
		workspaceID := monoflake.IDFromBase62(c.Params("id")).Int64()
		machineID := monoflake.IDFromBase62(payload.MachineID).Int64()
		if workspaceID == 0 || machineID == 0 {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}

		ctx, cancel := newContext(c)
		defer cancel()

		// Access first: everything below leaks something about the workspace.
		ws, err := h.crud.GetWorkspace(ctx, entity.GetWorkspaceRequest{ID: workspaceID, UserID: userID})
		if err != nil {
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}

		// The gate, and it is two questions rather than one.
		//
		// Checked here rather than only in the UI because two people can press
		// the button at the same moment, and only the server-side check makes
		// that race come out with one agent.
		//
		// The live connection answers "is an agent talking to this workspace
		// right now". It does not answer "has one been started and not
		// finished connecting yet", and that window is seconds long — easily
		// long enough to press the button twice, which is how two agents end
		// up sharing one .mcp.json and racing each other for the same tasks.
		// The session row answers that half, and survives a backend restart
		// into the bargain.
		if h.mcpManager.IsAgentConnected(workspaceID) {
			c.Status(http.StatusConflict)
			return c.Send(mapper.FromMessageToHTTPResponse(
				"this workspace already has an agent connected", http.StatusConflict))
		}
		active, err := h.crud.ActiveSessionForWorkspace(ctx, entity.ActiveSessionRequest{
			UserID:      userID,
			WorkspaceID: c.Params("id"),
		})
		if err != nil {
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}
		if active != nil {
			c.Status(http.StatusConflict)
			return c.Send(mapper.FromMessageToHTTPResponse(
				"this workspace already has an agent "+active.Status+" on a machine", http.StatusConflict))
		}

		// The folder. Empty means the person has to choose one — never guessed,
		// and never defaulted to wherever the daemon happens to live.
		if ws.Workspace.WorkingDirectory == "" {
			c.Status(http.StatusPreconditionRequired)
			return c.Send(mapper.FromMessageToHTTPResponse(
				"set this workspace's working directory before launching an agent",
				http.StatusPreconditionRequired))
		}

		machine, err := h.crud.GetMachine(ctx, entity.GetMachineRequest{
			UserID: userID, MachineID: payload.MachineID,
		})
		if err != nil {
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}
		if !machine.Machine.Enabled {
			c.Status(http.StatusConflict)
			return c.Send(mapper.FromMessageToHTTPResponse(
				"that machine is disabled", http.StatusConflict))
		}
		// Refused here rather than sent and silently dropped: a launch that
		// goes nowhere and reports success is worse than one that fails.
		if h.machineRegistry == nil {
			c.Status(http.StatusServiceUnavailable)
			return c.Send(mapper.FromMessageToHTTPResponse(
				"machine connections are not available on this server",
				http.StatusServiceUnavailable))
		}
		if _, err := h.machineRegistry.Get(machineID); err != nil {
			c.Status(http.StatusConflict)
			return c.Send(mapper.FromMessageToHTTPResponse(
				"that machine is not connected", http.StatusConflict))
		}

		start := wire.StartSession{
			Kind:       payload.Kind,
			Dir:        ws.Workspace.WorkingDirectory,
			ServerName: mcpServerName,
			Workspace:  ws.Workspace.Name,
			Model:      payload.Model,
			Agent:      payload.Agent,
			Cols:       payload.Cols,
			Rows:       payload.Rows,
		}

		// The credential is minted only once everything else has passed: a
		// token created and then discarded by a later refusal is a token that
		// existed for no reason.
		//
		// Both kinds read it. That is easy to get wrong — the gateway takes
		// its model and agent on the command line, so it looks self-contained
		// — and getting it wrong is not subtle from the outside: the gateway
		// starts, says it cannot find its config, and dies.
		token, err := h.tokenSvc.CreateMCPToken(userID, c.Params("id"), "access")
		if err != nil {
			zlog.Error().Err(err).Msg("[launch] mint workspace token")
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}
		// The token is a query parameter in the URL, which is why this value
		// travels in a frame and never in an argv or a log line.
		start.MCPURL = h.mcpURL(workspaceID) + "?token=" + token

		session, err := h.crud.CreateSession(ctx, entity.CreateSessionRequest{
			UserID:      userID,
			MachineID:   payload.MachineID,
			WorkspaceID: c.Params("id"),
			Kind:        payload.Kind,
			Cols:        payload.Cols,
			Rows:        payload.Rows,
		})
		if err != nil {
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}
		start.SessionID = uint64(monoflake.IDFromBase62(session.Session.ID).Int64())

		if err := h.sendStart(machineID, start); err != nil {
			// The row exists and the daemon never heard about it. Marking it
			// failed is what stops it sitting in "starting" forever and
			// blocking the workspace's next launch.
			h.markSessionFailed(session.Session.ID, err.Error())
			zlog.Error().Err(err).
				Interface("start", start.Redacted()).
				Msg("[launch] could not reach the machine")
			c.Status(http.StatusBadGateway)
			return c.Send(mapper.FromMessageToHTTPResponse(
				"could not reach that machine", http.StatusBadGateway))
		}

		c.Status(http.StatusAccepted)
		return c.JSON(session)
	}
}

// sendStart delivers the start request to the daemon.
func (h *handler) sendStart(machineID int64, start wire.StartSession) error {
	body, err := json.Marshal(start)
	if err != nil {
		return err
	}
	frame, err := wire.ControlFrame(wire.Control{Op: wire.OpStartSession, Body: body})
	if err != nil {
		return err
	}
	return h.machineRegistry.Send(machineID, frame)
}

// markSessionFailed records that a session never got started.
func (h *handler) markSessionFailed(sessionID, reason string) {
	id := monoflake.IDFromBase62(sessionID).Int64()
	if id == 0 {
		return
	}
	now := time.Now()
	if err := h.crud.UpdateSessionState(context.Background(), entity.UpdateSessionStateRequest{
		SessionID: sessionID,
		Status:    machinectrl.SessionFailed,
		EndedAt:   &now,
		Error:     reason,
	}); err != nil {
		zlog.Error().Err(err).Str("session", sessionID).Msg("[launch] could not record the failure")
	}
}
