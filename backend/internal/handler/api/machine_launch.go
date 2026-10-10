// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/mustafaturan/monoflake"
	zlog "github.com/rs/zerolog/log"

	"github.com/agentrq/agentrq/backend/internal/controller/agentlaunch"
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
	_routePathAcpRegistry  = "/acp-registry/agents"
)

// acpGatewayAskTimeout bounds how long a lookup waits on the daemon.
//
// Long enough for `npx` to fetch a package it has never run before —
// --list-models has been seen taking most of a minute cold — and bounded
// regardless, because this backs an autocomplete a person can always bypass
// by typing: a request that hung forever behind a slow daemon would be a
// worse outcome than answering "nothing to suggest" on time.
const acpGatewayAskTimeout = 50 * time.Second

// errNoWorkingDirectory is a models lookup for a workspace with no folder to
// run the gateway in.
var errNoWorkingDirectory = errors.New("workspace has no working directory")

func (h *handler) registerAgentLaunchRoutes() {
	h.router.Post(_routePathAgentLaunch, h.launchAgent())
	h.router.Get(_routePathAgentSession, h.workspaceSession())
	h.router.Get(_routePathAcpAgents, h.listAcpAgents())
	h.router.Get(_routePathAcpModels, h.listAcpModels())
	h.router.Get(_routePathAcpRegistry, h.listAcpRegistryAgents())
}

// listAcpRegistryAgents answers the official ACP registry's agents, the ids
// acp-gateway accepts, so the launch forms can list them instead of leaving
// the agent to be guessed.
//
// Fails open like [handler.listAcpAgents]: a registry that cannot be reached
// answers an empty list, and the forms fall back to the machine's own.
func (h *handler) listAcpRegistryAgents() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		ctx, cancel := newContext(c)
		defer cancel()

		body, err := h.acpRegistry.Load(ctx)
		if err != nil {
			return c.Send(_emptyAcpRegistry)
		}
		return c.Send(body)
	}
}

var _emptyAcpRegistry = mapper.FromAcpRegistryAgentsEntityToHTTPResponse(nil)

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

		key := machinectrl.LookupKey{MachineID: machineID, Op: wire.OpListAcpAgents}
		body, err := h.acpLookups.Load(ctx, key, func(ctx context.Context) ([]byte, bool, error) {
			list, err := askDaemonFor[wire.AcpAgentsList](h, ctx, machineID, wire.Control{Op: key.Op})
			if err != nil {
				return nil, false, err
			}
			body, err := json.Marshal(list)
			return body, len(list.Agents) > 0, err
		})
		if err != nil {
			return c.JSON(out)
		}
		return c.Send(body)
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

		// Cloned: fiber reuses the request's buffer, and agent outlives the
		// request as a cache key and in a shared fetch.
		agent := strings.Clone(c.Query("agent"))
		out := wire.AcpModelsList{Agent: agent}
		if agent == "" || c.Query("machineId") == "" {
			return c.JSON(out)
		}

		userID := c.Locals("user_id").(string)
		workspaceID := monoflake.IDFromBase62(c.Params("id")).Int64()
		if _, err := h.crud.GetMachine(ctx, entity.GetMachineRequest{
			UserID: userID, MachineID: c.Query("machineId"),
		}); err != nil {
			return c.JSON(out)
		}
		machineID := monoflake.IDFromBase62(c.Query("machineId")).Int64()

		// The workspace is read only on a miss: it names the folder to ask
		// from, and a cached answer needs none.
		key := machinectrl.LookupKey{MachineID: machineID, Op: wire.OpListAcpModels, Agent: agent}
		reply, err := h.acpLookups.Load(ctx, key, func(ctx context.Context) ([]byte, bool, error) {
			ws, err := h.crud.GetWorkspace(ctx, entity.GetWorkspaceRequest{
				ID: workspaceID, UserID: userID,
			})
			if err != nil {
				return nil, false, err
			}
			if ws.Workspace.WorkingDirectory == "" {
				return nil, false, errNoWorkingDirectory
			}
			body, _ := json.Marshal(wire.ListAcpModels{Agent: agent, Dir: ws.Workspace.WorkingDirectory})
			list, err := askDaemonFor[wire.AcpModelsList](h, ctx, machineID, wire.Control{Op: key.Op, Body: body})
			if err != nil {
				return nil, false, err
			}
			list.Agent = agent
			reply, err := json.Marshal(list)
			return reply, len(list.Models) > 0, err
		})
		if err != nil {
			return c.JSON(out)
		}
		return c.Send(reply)
	}
}

// askDaemonFor asks a machine one lookup and decodes its answer. An error
// reply decodes to nothing.
//
// Answers are kept in [machinectrl.LookupCache] per machine and agent, not per
// workspace: what a machine's agents and models are does not depend on the
// folder asked from. Only an answer with something in it is kept, since an
// empty one is how every failure on the machine arrives.
func askDaemonFor[T any](h *handler, ctx context.Context, machineID int64, c wire.Control) (T, error) {
	var out T
	reply, err := h.askDaemon(ctx, machineID, c)
	if err != nil {
		return out, err
	}
	if reply.Op != wire.OpError {
		_ = json.Unmarshal(reply.Body, &out)
	}
	return out, nil
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

// launchAgent starts an agent for a workspace on a chosen machine, through
// the launcher the supervisor's launchAgent uses too.
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
		if err := c.BodyParser(&payload); err != nil ||
			monoflake.IDFromBase62(c.Params("id")).Int64() == 0 ||
			monoflake.IDFromBase62(payload.MachineID).Int64() == 0 {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}

		ctx, cancel := newContext(c)
		defer cancel()

		session, err := h.launcher.Launch(ctx, agentlaunch.Request{
			UserID:      c.Locals("user_id").(string),
			WorkspaceID: c.Params("id"),
			MachineID:   payload.MachineID,
			Kind:        payload.Kind,
			Model:       payload.Model,
			Effort:      payload.Effort,
			Agent:       payload.Agent,
			Cols:        payload.Cols,
			Rows:        payload.Rows,
		})
		if err != nil {
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}

		c.Status(http.StatusAccepted)
		return c.JSON(session)
	}
}
