// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

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

const (
	_routePathAgentLaunch  = "/workspaces/:id/agent"
	_routePathAgentSession = "/workspaces/:id/session"
	_routePathAcpAgents    = "/machines/:id/acp-agents"
	_routePathAcpModels    = "/workspaces/:id/acp-models"
)

// acpGatewayAskTimeout bounds how long a lookup waits on the daemon.
//
// Long enough for `npx` to fetch a package it has never run before —
// --list-models has been seen taking most of a minute cold — and bounded
// regardless, because this backs an autocomplete a person can always bypass
// by typing: a request that hung forever behind a slow daemon would be a
// worse outcome than answering "nothing to suggest" on time.
const acpGatewayAskTimeout = 50 * time.Second

// mcpServerName is the entry written into .mcp.json and what `server:<name>`
// refers to on the command line.
//
// Matches the repository's own .mcp.json, so a machine that already has one
// configured by hand keeps working rather than acquiring a second entry
// pointing at the same place.
const mcpServerName = "agentrq-workspace"

// supervisorWorkspaceName is the workspace that also gets the account-wide
// server, matching the one created for every new account in
// `controller/crud/user.go` and the same exact-name rule the setup tab applies.
//
// A person can rename a workspace to this, and that is allowed: what it buys
// them is a second entry pointing at a server their own account already owns
// and which authenticates them separately.
const supervisorWorkspaceName = "supervisor"

func (h *handler) registerAgentLaunchRoutes() {
	h.router.Post(_routePathAgentLaunch, h.launchAgent())
	h.router.Get(_routePathAgentSession, h.workspaceSession())
	h.router.Get(_routePathAcpAgents, h.listAcpAgents())
	h.router.Get(_routePathAcpModels, h.listAcpModels())
}

// listAcpAgents answers the acp-gateway's agent catalogue, for the launch
// form's autocomplete.
//
// Fails open, deliberately and completely: an unrecognised or unowned
// machine, one not connected, a daemon too old to know the op, a command
// that errored on that machine, or a reply that never arrives all answer the
// same way — an empty list, with 200 OK — because free text is always the
// form's fallback and there is nothing an error status would tell it that
// emptiness does not already say.
func (h *handler) listAcpAgents() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		ctx, cancel := newContext(c)
		defer cancel()

		out := wire.AcpAgentsList{}
		if _, err := h.crud.GetMachine(ctx, entity.GetMachineRequest{
			UserID: c.Locals("user_id").(string), MachineID: c.Params("id"),
		}); err != nil {
			return c.JSON(out)
		}
		machineID := monoflake.IDFromBase62(c.Params("id")).Int64()

		reply, err := h.askDaemon(ctx, machineID, wire.Control{Op: wire.OpListAcpAgents})
		if err != nil {
			return c.JSON(out)
		}
		_ = json.Unmarshal(reply.Body, &out)
		return c.JSON(out)
	}
}

// listAcpModels answers what one agent supports, for the same autocomplete
// once somebody has picked an agent.
//
// --list-models opens a real agent session, and the gateway refuses without
// a .mcp.json it can find in its working directory — so this needs a
// workspace, unlike [handler.listAcpAgents], for the folder to run it in. A
// workspace with no working directory set, or one nothing has ever launched
// from (so no .mcp.json exists there yet), fails open the same as any other
// reason this could come back empty.
func (h *handler) listAcpModels() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		ctx, cancel := newContext(c)
		defer cancel()

		agent := c.Query("agent")
		out := wire.AcpModelsList{Agent: agent}
		if agent == "" || c.Query("machineId") == "" {
			return c.JSON(out)
		}

		userID := c.Locals("user_id").(string)
		ws, err := h.crud.GetWorkspace(ctx, entity.GetWorkspaceRequest{
			ID: monoflake.IDFromBase62(c.Params("id")).Int64(), UserID: userID,
		})
		if err != nil || ws.Workspace.WorkingDirectory == "" {
			return c.JSON(out)
		}
		if _, err := h.crud.GetMachine(ctx, entity.GetMachineRequest{
			UserID: userID, MachineID: c.Query("machineId"),
		}); err != nil {
			return c.JSON(out)
		}
		machineID := monoflake.IDFromBase62(c.Query("machineId")).Int64()

		body, err := json.Marshal(wire.ListAcpModels{Agent: agent, Dir: ws.Workspace.WorkingDirectory})
		if err != nil {
			return c.JSON(out)
		}
		reply, err := h.askDaemon(ctx, machineID, wire.Control{Op: wire.OpListAcpModels, Body: body})
		if err != nil {
			return c.JSON(out)
		}
		_ = json.Unmarshal(reply.Body, &out)
		return c.JSON(out)
	}
}

// askDaemon sends a correlated request to a machine and waits for its reply,
// failing open the same way for every reason it might not get one: no
// registry configured on this server, no socket held for that machine, a
// write that failed, or a reply that never arrived in time.
func (h *handler) askDaemon(ctx context.Context, machineID int64, c wire.Control) (wire.Control, error) {
	if h.machineRegistry == nil {
		return wire.Control{}, machinectrl.ErrNotConnected
	}
	reply, err := h.machineRegistry.Ask(ctx, machineID, c, acpGatewayAskTimeout)
	if err != nil {
		zlog.Debug().Err(err).Int64("machine_id", machineID).Str("op", string(c.Op)).
			Msg("[machine] an acp-gateway lookup did not answer")
	}
	return reply, err
}

// workspaceSession returns the session running for a workspace, or null.
//
// The workspace page knows an agent is *connected* — that arrives over the
// event stream and turns the header dot green — but a connection is not
// something you can open. Reaching the terminal needs the session's id, and
// sessions are otherwise listed per machine, so the page would have to ask
// every machine in turn and pick the row naming this workspace.
//
// The question is the one the launch gate already asks, from the same method:
// a row in `starting` or `running`, scoped to the signed-in user. Which is why
// a workspace belonging to somebody else answers null here rather than being
// refused — the query never sees it, so there is nothing to leak and nothing
// to distinguish "not yours" from "nothing running".
//
// Nothing running is the ordinary answer and not an error: most workspaces
// have no agent most of the time, and a 404 for the common case would have
// every caller treating an error as a fact.
func (h *handler) workspaceSession() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		ctx, cancel := newContext(c)
		defer cancel()

		session, err := h.crud.ActiveSessionForWorkspace(ctx, entity.ActiveSessionRequest{
			UserID:      c.Locals("user_id").(string),
			WorkspaceID: c.Params("id"),
		})
		if err != nil {
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}
		return c.JSON(entity.WorkspaceSessionResponse{Session: session})
	}
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
			Effort    string `json:"effort,omitempty"`
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
		//
		// A fork runs in a folder the daemon makes from its parent's, so it is
		// the parent's that has to be set.
		dir := ws.Workspace.WorkingDirectory
		var fork *wire.ForkSpec
		if ws.Workspace.ForkOfID != 0 {
			parent, err := h.crud.GetWorkspace(ctx, entity.GetWorkspaceRequest{ID: ws.Workspace.ForkOfID, UserID: userID})
			if err != nil {
				e, status := mapper.FromErrorToHTTPResponse(err)
				c.Status(status)
				return c.Send(e)
			}
			if parent.Workspace.WorkingDirectory == "" {
				c.Status(http.StatusPreconditionRequired)
				return c.Send(mapper.FromMessageToHTTPResponse(
					"set "+parent.Workspace.Name+"'s working directory before launching an agent in its fork",
					http.StatusPreconditionRequired))
			}
			dir = parent.Workspace.WorkingDirectory
			fork = &wire.ForkSpec{ID: monoflake.ID(ws.Workspace.ID).String(), From: dir}
		} else if dir == "" {
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
		// An agentrqd that predates forks ignores the fork field: the agent
		// would run in the parent's folder and connect as the parent. The
		// version floor catches a build that says the capability early.
		if fork != nil && (!h.machineRegistry.HasCapability(machineID, wire.CapabilityFork) ||
			!wire.VersionAtLeast(machine.Machine.Version, wire.MinForkVersion)) {
			c.Status(http.StatusConflict)
			return c.Send(mapper.FromMessageToHTTPResponse(
				"update agentrqd on this machine to run a fork (it needs "+wire.MinForkVersion+" or newer)",
				http.StatusConflict))
		}
		// An older agentrqd drops a Claude Code model and effort and starts
		// the defaults, which would look like the choice was honoured.
		if payload.Kind == "claude-code" && (payload.Model != "" || payload.Effort != "") &&
			!h.machineRegistry.HasCapability(machineID, wire.CapabilityClaudeOptions) {
			c.Status(http.StatusConflict)
			return c.Send(mapper.FromMessageToHTTPResponse(
				"update agentrqd on this machine to choose Claude Code's model or effort, or leave both blank",
				http.StatusConflict))
		}

		start := wire.StartSession{
			Kind:       payload.Kind,
			Dir:        dir,
			Fork:       fork,
			ServerName: mcpServerName,
			Workspace:  ws.Workspace.Name,
			Model:      payload.Model,
			Effort:     payload.Effort,
			Agent:      payload.Agent,
			Cols:       payload.Cols,
			Rows:       payload.Rows,
		}

		// The supervisor workspace works across every other one, so its agent
		// gets the account-wide server as well. Decided here and nowhere else:
		// the daemon writes the entry when it is given a URL and works none of
		// this out from the workspace's name.
		if ws.Workspace.Name == supervisorWorkspaceName {
			start.CoreMCPURL = h.coreMCPURL()
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

		// Counted on the frame actually being sent, not on the request coming
		// in: a launch refused above never reached a machine, and a launch that
		// timed out here is a delivery failure, not a spin-up.
		if launchAction, ok := agentLaunchAction(payload.Kind); ok {
			if err := h.crud.RecordTelemetry(ctx, entity.RecordTelemetryRequest{
				Action:      launchAction,
				WorkspaceID: workspaceID,
				UserID:      userID,
			}); err != nil {
				zlog.Warn().Err(err).Int64("workspace_id", workspaceID).
					Str("kind", payload.Kind).
					Msg("agent launch happened but was not counted")
			}
		}

		c.Status(http.StatusAccepted)
		return c.JSON(session)
	}
}

// agentLaunchAction resolves a launch request's kind to the telemetry action
// that counts it. The daemon is the actual authority on valid kinds
// (supervisor.Resolve); a kind neither server recognises is left uncounted
// rather than guessed at.
func agentLaunchAction(kind string) (entity.Action, bool) {
	switch kind {
	case "claude-code":
		return entity.ActionAgentLaunchClaudeCode, true
	case "acp-gateway":
		return entity.ActionAgentLaunchACPGateway, true
	}
	return 0, false
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
