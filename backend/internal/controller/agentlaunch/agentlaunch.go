// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Package agentlaunch starts and stops a workspace's agent on a machine. The
// web launch form and the supervisor's launchAgent and stopAgent all run this
// one flow, so neither can skip a gate the other keeps.
package agentlaunch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	machinectrl "github.com/agentrq/agentrq/backend/internal/controller/machine"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/daemon/wire"
	"github.com/mustafaturan/monoflake"
	zlog "github.com/rs/zerolog/log"
)

// mcpServerName is the entry written into .mcp.json and what `server:<name>`
// refers to on the command line.
//
// Matches the repository's own .mcp.json, so a machine that already has one
// configured by hand keeps working rather than acquiring a second entry
// pointing at the same place.
const mcpServerName = "agentrq-workspace"

// SupervisorWorkspaceName is the workspace that also gets the account-wide
// server, matching the one created for every new account in
// `controller/crud/user.go` and the same exact-name rule the setup tab applies.
//
// A person can rename a workspace to this, and that is allowed: what it buys
// them is a second entry pointing at a server their own account already owns
// and which authenticates them separately.
const SupervisorWorkspaceName = "supervisor"

type (
	// Crud is the part of crud.Controller a launch or a stop uses.
	Crud interface {
		GetWorkspace(ctx context.Context, req entity.GetWorkspaceRequest) (*entity.GetWorkspaceResponse, error)
		ActiveSessionForWorkspace(ctx context.Context, req entity.ActiveSessionRequest) (*entity.SessionView, error)
		GetMachine(ctx context.Context, req entity.GetMachineRequest) (*entity.GetMachineResponse, error)
		CreateSession(ctx context.Context, req entity.CreateSessionRequest) (*entity.CreateSessionResponse, error)
		UpdateSessionState(ctx context.Context, req entity.UpdateSessionStateRequest) error
		RecordTelemetry(ctx context.Context, rq entity.RecordTelemetryRequest) error
		RecordSessionKill(ctx context.Context, req entity.RecordSessionKillRequest)
	}

	// Agents answers whether an agent is talking to a workspace right now.
	Agents interface {
		IsAgentConnected(workspaceID int64) bool
	}

	// Tokens mints the workspace credential the agent connects with.
	Tokens interface {
		CreateMCPToken(userID, workspaceID, tokenType string) (string, error)
	}

	// Launcher starts and stops agents. Machines may be nil on a server that
	// holds no machine connections; every launch is then refused.
	Launcher struct {
		Crud     Crud
		Agents   Agents
		Machines *machinectrl.Registry
		Tokens   Tokens
		URLs     URLs

		// launching holds the workspaces with a launch between its checks and
		// its session row, on this instance.
		mu        sync.Mutex
		launching map[int64]struct{}
	}

	// Request is one launch: which agent, in which workspace, on which
	// machine. IDs are base62.
	Request struct {
		UserID      string
		WorkspaceID string
		MachineID   string
		Kind        string
		Model       string
		Effort      string
		Agent       string
		Cols        uint16
		Rows        uint16
	}

	// StopRequest names the workspace whose agent to stop. IDs are base62.
	StopRequest struct {
		UserID      string
		WorkspaceID string
	}

	// URLs builds the MCP server addresses an agent is given, for the
	// deployment this server runs as.
	URLs struct {
		// Base is the server's own address, used as is on a local deployment.
		Base string
		// Domain, when it is a real one, moves each server to its own host
		// under mcp.<Domain>.
		Domain string
		// Secure picks https over http for those hosts.
		Secure bool
	}
)

func busy(message string) error { return entity.NewLaunchError(entity.ErrLaunchBusy, message) }

// Launch starts an agent for a workspace on a chosen machine.
//
// The gates here are the substance, and their order is deliberate: everything
// that can refuse does so before a session row exists or a frame is sent, so a
// refused launch leaves nothing behind to clean up.
func (l *Launcher) Launch(ctx context.Context, rq Request) (*entity.CreateSessionResponse, error) {
	workspaceID := monoflake.IDFromBase62(rq.WorkspaceID).Int64()
	machineID := monoflake.IDFromBase62(rq.MachineID).Int64()
	if workspaceID == 0 || machineID == 0 {
		return nil, entity.NewLaunchError(entity.ErrLaunchInvalid, "name a workspace and a machine to launch an agent")
	}

	// Access first: everything below leaks something about the workspace.
	ws, err := l.Crud.GetWorkspace(ctx, entity.GetWorkspaceRequest{ID: workspaceID, UserID: rq.UserID})
	if err != nil {
		return nil, err
	}

	// The checks below and the session row are separate steps, so two
	// launches at once would both pass them. A supervisor can send parallel
	// calls, which makes that easy to hit. Held per workspace on this
	// instance; a launch on another instance still relies on the row.
	if !l.claim(workspaceID) {
		return nil, busy("this workspace already has an agent starting")
	}
	defer l.release(workspaceID)

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
	if l.Agents.IsAgentConnected(workspaceID) {
		return nil, busy("this workspace already has an agent connected")
	}
	active, err := l.Crud.ActiveSessionForWorkspace(ctx, entity.ActiveSessionRequest{
		UserID:      rq.UserID,
		WorkspaceID: rq.WorkspaceID,
	})
	if err != nil {
		return nil, err
	}
	if active != nil {
		return nil, busy("this workspace already has an agent " + active.Status + " on a machine")
	}

	// The folder. Empty means the person has to choose one — never guessed,
	// and never defaulted to wherever the daemon happens to live.
	//
	// A fork runs in a folder the daemon makes from its parent's, so it is
	// the parent's that has to be set.
	dir := ws.Workspace.WorkingDirectory
	var fork *wire.ForkSpec
	if ws.Workspace.ForkOfID != 0 {
		parent, err := l.Crud.GetWorkspace(ctx, entity.GetWorkspaceRequest{ID: ws.Workspace.ForkOfID, UserID: rq.UserID})
		if err != nil {
			return nil, err
		}
		if parent.Workspace.WorkingDirectory == "" {
			return nil, entity.NewLaunchError(entity.ErrLaunchNoFolder,
				"set "+parent.Workspace.Name+"'s working directory before launching an agent in its fork")
		}
		dir = parent.Workspace.WorkingDirectory
		fork = &wire.ForkSpec{ID: monoflake.ID(ws.Workspace.ID).String(), From: dir}
	} else if dir == "" {
		return nil, entity.NewLaunchError(entity.ErrLaunchNoFolder,
			"set this workspace's working directory before launching an agent")
	}

	machine, err := l.Crud.GetMachine(ctx, entity.GetMachineRequest{UserID: rq.UserID, MachineID: rq.MachineID})
	if err != nil {
		return nil, err
	}
	if !machine.Machine.Enabled {
		return nil, busy("that machine is disabled")
	}
	// Refused here rather than sent and silently dropped: a launch that
	// goes nowhere and reports success is worse than one that fails.
	if l.Machines == nil {
		return nil, entity.NewLaunchError(entity.ErrLaunchUnavailable,
			"machine connections are not available on this server")
	}
	if _, err := l.Machines.Get(machineID); err != nil {
		return nil, busy("that machine is not connected")
	}
	// An agentrqd that predates forks ignores the fork field: the agent
	// would run in the parent's folder and connect as the parent. The
	// version floor catches a build that says the capability early.
	if fork != nil && (!l.Machines.HasCapability(machineID, wire.CapabilityFork) ||
		!wire.VersionAtLeast(machine.Machine.Version, wire.MinForkVersion)) {
		return nil, busy("update agentrqd on this machine to run a fork (it needs " + wire.MinForkVersion + " or newer)")
	}
	// An older agentrqd drops a Claude Code model and effort and starts
	// the defaults, which would look like the choice was honoured.
	if rq.Kind == "claude-code" && (rq.Model != "" || rq.Effort != "") &&
		!l.Machines.HasCapability(machineID, wire.CapabilityClaudeOptions) {
		return nil, busy("update agentrqd on this machine to choose Claude Code's model or effort, or leave both blank")
	}

	start := wire.StartSession{
		Kind:       rq.Kind,
		Dir:        dir,
		Fork:       fork,
		ServerName: mcpServerName,
		Workspace:  ws.Workspace.Name,
		Model:      rq.Model,
		Effort:     rq.Effort,
		Agent:      rq.Agent,
		Cols:       rq.Cols,
		Rows:       rq.Rows,
	}

	// The supervisor workspace works across every other one, so its agent
	// gets the account-wide server as well. Decided here and nowhere else:
	// the daemon writes the entry when it is given a URL and works none of
	// this out from the workspace's name.
	if ws.Workspace.Name == SupervisorWorkspaceName {
		start.CoreMCPURL = l.URLs.Core()
	}

	// The credential is minted only once everything else has passed: a
	// token created and then discarded by a later refusal is a token that
	// existed for no reason.
	//
	// Both kinds read it. That is easy to get wrong — the gateway takes
	// its model and agent on the command line, so it looks self-contained
	// — and getting it wrong is not subtle from the outside: the gateway
	// starts, says it cannot find its config, and dies.
	token, err := l.Tokens.CreateMCPToken(rq.UserID, rq.WorkspaceID, "access")
	if err != nil {
		zlog.Error().Err(err).Msg("[launch] mint workspace token")
		return nil, err
	}
	// The token is a query parameter in the URL, which is why this value
	// travels in a frame and never in an argv or a log line.
	start.MCPURL = l.URLs.Workspace(workspaceID) + "?token=" + token

	session, err := l.Crud.CreateSession(ctx, entity.CreateSessionRequest{
		UserID:      rq.UserID,
		MachineID:   rq.MachineID,
		WorkspaceID: rq.WorkspaceID,
		Kind:        rq.Kind,
		Cols:        rq.Cols,
		Rows:        rq.Rows,
	})
	if err != nil {
		return nil, err
	}
	start.SessionID = uint64(monoflake.IDFromBase62(session.Session.ID).Int64())

	if err := l.sendStart(machineID, start); err != nil {
		// The row exists and the daemon never heard about it. Marking it
		// failed is what stops it sitting in "starting" forever and
		// blocking the workspace's next launch.
		l.markSessionFailed(session.Session.ID, err.Error())
		zlog.Error().Err(err).
			Interface("start", start.Redacted()).
			Msg("[launch] could not reach the machine")
		return nil, entity.NewLaunchError(entity.ErrLaunchUnreachable, "could not reach that machine")
	}

	// Counted on the frame actually being sent, not on the request coming
	// in: a launch refused above never reached a machine, and a launch that
	// timed out here is a delivery failure, not a spin-up.
	if launchAction, ok := launchAction(rq.Kind); ok {
		if err := l.Crud.RecordTelemetry(ctx, entity.RecordTelemetryRequest{
			Action:      launchAction,
			WorkspaceID: workspaceID,
			UserID:      rq.UserID,
		}); err != nil {
			zlog.Warn().Err(err).Int64("workspace_id", workspaceID).
				Str("kind", rq.Kind).
				Msg("agent launch happened but was not counted")
		}
	}

	return session, nil
}

// claim reports whether this call may launch into the workspace, and marks it
// taken until release.
func (l *Launcher) claim(workspaceID int64) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, taken := l.launching[workspaceID]; taken {
		return false
	}
	if l.launching == nil {
		l.launching = map[int64]struct{}{}
	}
	l.launching[workspaceID] = struct{}{}
	return true
}

func (l *Launcher) release(workspaceID int64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.launching, workspaceID)
}

// Stop asks the machine running a workspace's agent to stop it, and answers
// the session it stopped, or nil when none was running on a machine. It
// returns once the request is on its way: the daemon reports the end on the
// session row.
func (l *Launcher) Stop(ctx context.Context, rq StopRequest) (*entity.SessionView, error) {
	if monoflake.IDFromBase62(rq.WorkspaceID).Int64() == 0 {
		return nil, entity.NewLaunchError(entity.ErrLaunchInvalid, "name a workspace to stop its agent")
	}
	// Scoped to the caller, so another account's workspace reads as nothing
	// running rather than as a refusal that says it exists.
	session, err := l.Crud.ActiveSessionForWorkspace(ctx, entity.ActiveSessionRequest{
		UserID:      rq.UserID,
		WorkspaceID: rq.WorkspaceID,
	})
	if err != nil || session == nil {
		return nil, err
	}

	machineID := monoflake.IDFromBase62(session.MachineID).Int64()
	if err := machinectrl.KillSession(l.Machines, machineID, session.ID); err != nil {
		switch {
		case errors.Is(err, machinectrl.ErrNoConnections):
			return nil, entity.NewLaunchError(entity.ErrLaunchUnavailable,
				"machine connections are not available on this server")
		case errors.Is(err, machinectrl.ErrNotConnected):
			return nil, busy("that machine is not connected")
		}
		return nil, entity.NewLaunchError(entity.ErrLaunchUnreachable, "could not reach that machine")
	}

	// Counted only once the request is on its way, as the web's stop is.
	l.Crud.RecordSessionKill(ctx, entity.RecordSessionKillRequest{
		UserID:      rq.UserID,
		WorkspaceID: session.WorkspaceID,
		SessionID:   session.ID,
	})
	return session, nil
}

// Kinds are the agents this server knows how to launch and count. The daemon
// is the authority on what it can run, so the web form is not held to these;
// the supervisor's launchAgent offers exactly these.
var Kinds = []string{"claude-code", "acp-gateway"}

// launchAction resolves a launch request's kind to the telemetry action
// that counts it. The daemon is the actual authority on valid kinds
// (supervisor.Resolve); a kind neither server recognises is left uncounted
// rather than guessed at.
func launchAction(kind string) (entity.Action, bool) {
	switch kind {
	case "claude-code":
		return entity.ActionAgentLaunchClaudeCode, true
	case "acp-gateway":
		return entity.ActionAgentLaunchACPGateway, true
	}
	return 0, false
}

// sendStart delivers the start request to the daemon.
func (l *Launcher) sendStart(machineID int64, start wire.StartSession) error {
	// Neither can fail: a plain struct, and a control message with its op.
	body, _ := json.Marshal(start)
	frame, _ := wire.ControlFrame(wire.Control{Op: wire.OpStartSession, Body: body})
	return l.Machines.Send(machineID, frame)
}

// markSessionFailed records that a session never got started.
func (l *Launcher) markSessionFailed(sessionID, reason string) {
	now := time.Now()
	if err := l.Crud.UpdateSessionState(context.Background(), entity.UpdateSessionStateRequest{
		SessionID: sessionID,
		Status:    machinectrl.SessionFailed,
		EndedAt:   &now,
		Error:     reason,
	}); err != nil {
		zlog.Error().Err(err).Str("session", sessionID).Msg("[launch] could not record the failure")
	}
}

// masked reports whether each MCP server gets its own host under the domain.
func (u URLs) masked() bool {
	return u.Domain != "" && !strings.HasPrefix(u.Domain, "localhost") && !strings.HasPrefix(u.Domain, "127.0.0.1")
}

func (u URLs) proto() string {
	if u.Secure {
		return "https"
	}
	return "http"
}

// Workspace is a workspace's own MCP server.
func (u URLs) Workspace(workspaceID int64) string {
	if u.masked() {
		// Subdomain based URLs use base36 for better compatibility (case-insensitive subdomains)
		id36 := strings.ToLower(strconv.FormatInt(workspaceID, 36))
		return fmt.Sprintf("%s://%s.mcp.%s", u.proto(), id36, u.Domain)
	}
	return fmt.Sprintf("%s/mcp/%s", u.Base, monoflake.ID(workspaceID).String())
}

// Core is the account-wide server, templated from the same host as the
// per-workspace one and by the same rule, so the two never disagree about
// which deployment they mean.
func (u URLs) Core() string {
	if u.masked() {
		return fmt.Sprintf("%s://mcp.%s/mcp", u.proto(), u.Domain)
	}
	return u.Base + "/mcp"
}
