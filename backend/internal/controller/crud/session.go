// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package crud

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/mustafaturan/monoflake"

	machinerules "github.com/agentrq/agentrq/backend/internal/controller/machine"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
)

// SessionController records agent sessions.
type SessionController interface {
	CreateSession(ctx context.Context, req entity.CreateSessionRequest) (*entity.CreateSessionResponse, error)
	UpdateSessionState(ctx context.Context, req entity.UpdateSessionStateRequest) error
	GetSession(ctx context.Context, req entity.GetSessionRequest) (*entity.GetSessionResponse, error)
	ListSessions(ctx context.Context, req entity.ListSessionsRequest) (*entity.ListSessionsResponse, error)
	ReconcileSessions(ctx context.Context, req entity.ReconcileSessionsRequest) error
	ActiveSessionForWorkspace(ctx context.Context, req entity.ActiveSessionRequest) (*entity.SessionView, error)
	ForkFolderMachines(ctx context.Context, req entity.ActiveSessionRequest) ([]string, error)
	RecordTerminalView(ctx context.Context, req entity.RecordTerminalViewRequest)
	RecordSessionKill(ctx context.Context, req entity.RecordSessionKillRequest)
}

func toSessionView(s model.Session) entity.SessionView {
	v := entity.SessionView{
		ID:        monoflake.ID(s.ID).String(),
		MachineID: monoflake.ID(s.MachineID).String(),
		Kind:      s.Kind,
		Status:    s.Status,
		ExitCode:  s.ExitCode,
		Restored:  s.Restored,
		Cols:      s.Cols,
		Rows:      s.Rows,
		StartedAt: s.StartedAt,
		EndedAt:   s.EndedAt,
		CreatedAt: s.CreatedAt,
	}
	if s.WorkspaceID != 0 {
		v.WorkspaceID = monoflake.ID(s.WorkspaceID).String()
	}
	return v
}

// CreateSession records a session that is about to be started.
//
// It begins as "starting" rather than "running": the daemon has not been asked
// yet, let alone succeeded. A row that claimed to be running before anything
// had run would make the workspace look occupied by a session that may never
// exist.
func (c *controller) CreateSession(ctx context.Context, req entity.CreateSessionRequest) (*entity.CreateSessionResponse, error) {
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	machineID := monoflake.IDFromBase62(req.MachineID).Int64()
	if uid == 0 || machineID == 0 {
		return nil, fmt.Errorf("invalid id")
	}

	now := time.Now()
	m := model.Session{
		ID:          c.idgen.NextID(),
		CreatedAt:   now,
		UpdatedAt:   now,
		MachineID:   machineID,
		UserID:      uid,
		WorkspaceID: monoflake.IDFromBase62(req.WorkspaceID).Int64(),
		Kind:        req.Kind,
		Status:      machinerules.SessionStarting,
		Cols:        int(req.Cols),
		Rows:        int(req.Rows),
	}
	created, err := c.repository.CreateSession(ctx, m)
	if err != nil {
		return nil, err
	}
	// The workspace is free here — this is the one place that knows it
	// without a lookup, because it just wrote it.
	c.emitEvent(ctx, entity.CRUDEvent{
		Action:       entity.ActionMachineSessionCreate,
		WorkspaceID:  created.WorkspaceID,
		UserID:       created.UserID,
		ResourceType: entity.ResourceSession,
		ResourceID:   created.ID,
		Actor:        entity.ActorHuman,
	})
	return &entity.CreateSessionResponse{Session: toSessionView(created)}, nil
}

// UpdateSessionState records what the daemon reported.
//
// A session that has finished is written and then removed. Written first so
// nothing is lost between the two — the update is what the event stream
// carries to whoever is watching, and it is that event, not the row, which
// tells somebody their agent failed and why. What the row is for is "what is
// running on this machine", and a finished session is not an answer to that.
func (c *controller) UpdateSessionState(ctx context.Context, req entity.UpdateSessionStateRequest) error {
	id := monoflake.IDFromBase62(req.SessionID).Int64()
	if id == 0 {
		return fmt.Errorf("invalid session id")
	}
	// Read before the update, and before the delete below, because a session
	// that has finished is removed — after that there is nothing left to ask
	// which workspace it was working in. Only when there is a user to scope
	// the read to, and only for the two states worth counting: the report
	// itself must not fail over telemetry, so this never returns an error.
	var counted *model.Session
	if req.UserID != "" && (req.Status == machinerules.SessionRunning || machinerules.SessionTerminal(req.Status)) {
		if uid := monoflake.IDFromBase62(req.UserID).Int64(); uid != 0 {
			if s, err := c.repository.GetSession(ctx, id, uid); err == nil {
				counted = &s
			}
		}
	}
	if err := c.repository.UpdateSessionState(ctx, id, req.Status, req.ExitCode, req.EndedAt, req.Restored); err != nil {
		return err
	}
	if counted != nil {
		action := entity.ActionMachineSessionOpen
		if machinerules.SessionTerminal(req.Status) {
			action = entity.ActionMachineSessionClose
		}
		c.emitEvent(ctx, entity.CRUDEvent{
			Action:       action,
			WorkspaceID:  counted.WorkspaceID,
			UserID:       counted.UserID,
			ResourceType: entity.ResourceSession,
			ResourceID:   id,
			// The daemon reports these, not a person: an agent exits on its
			// own, and a machine restarting closes every session on it.
			Actor: entity.ActorAgent,
		})
	}
	if !machinerules.SessionTerminal(req.Status) {
		return nil
	}
	// Best effort: a row that outlives its session is untidy, and failing the
	// state report over it would lose the event that matters.
	if err := c.repository.DeleteFinishedSession(ctx, id); err != nil {
		return nil
	}
	return nil
}

// GetSession reads one session, scoped to its owner.
//
// The scoping is in the query rather than a check after it: a session
// belonging to somebody else is not found, which is also the right thing to
// tell the caller.
func (c *controller) GetSession(ctx context.Context, req entity.GetSessionRequest) (*entity.GetSessionResponse, error) {
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	id := monoflake.IDFromBase62(req.SessionID).Int64()
	if uid == 0 || id == 0 {
		return nil, fmt.Errorf("invalid id")
	}
	s, err := c.repository.GetSession(ctx, id, uid)
	if err != nil {
		return nil, err
	}
	v := toSessionView(s)
	c.nameWorkspaces(ctx, uid, []*entity.SessionView{&v})
	// Best effort, as the workspace is: a terminal that cannot say where it
	// runs is still a terminal.
	if m, err := c.repository.GetMachine(ctx, s.MachineID, uid); err == nil {
		v.MachineName = firstNonEmpty(m.Name, m.Hostname)
	}
	return &entity.GetSessionResponse{Session: v}, nil
}

// nameWorkspaces fills in which workspace each session is working in.
//
// Best effort, and deliberately so: the sessions are the answer to the
// question that was asked, and a workspace that has been renamed out from
// under a row, or a lookup that fails, must not turn "what is running here"
// into an error page. A session with no name shows its kind, as it did before.
func (c *controller) nameWorkspaces(ctx context.Context, userID int64, views []*entity.SessionView) {
	ids := make([]int64, 0, len(views))
	seen := map[int64]struct{}{}
	for _, v := range views {
		id := monoflake.IDFromBase62(v.WorkspaceID).Int64()
		if id == 0 {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return
	}
	names, err := c.repository.WorkspaceNamesByID(ctx, ids, userID)
	if err != nil {
		return
	}
	for _, v := range views {
		if name, ok := names[monoflake.IDFromBase62(v.WorkspaceID).Int64()]; ok {
			v.WorkspaceName = name
		}
	}
}

func (c *controller) ListSessions(ctx context.Context, req entity.ListSessionsRequest) (*entity.ListSessionsResponse, error) {
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	machineID := monoflake.IDFromBase62(req.MachineID).Int64()
	if uid == 0 || machineID == 0 {
		return nil, fmt.Errorf("invalid id")
	}
	rows, err := c.repository.ListSessionsByMachine(ctx, machineID, uid)
	if err != nil {
		return nil, err
	}
	out := make([]entity.SessionView, 0, len(rows))
	for _, s := range rows {
		out = append(out, toSessionView(s))
	}
	refs := make([]*entity.SessionView, len(out))
	for i := range out {
		refs[i] = &out[i]
	}
	c.nameWorkspaces(ctx, uid, refs)
	return &entity.ListSessionsResponse{Sessions: out}, nil
}

// ActiveSessionForWorkspace returns the session already running for a
// workspace, or nil.
//
// This is the half of "does this workspace already have an agent?" that the
// database answers. The other half is the live MCP connection, and both are
// needed: a session that has been started but has not connected yet is
// invisible to the connection check, and that window is exactly long enough
// for somebody to press the button twice.
func (c *controller) ActiveSessionForWorkspace(ctx context.Context, req entity.ActiveSessionRequest) (*entity.SessionView, error) {
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	wid := monoflake.IDFromBase62(req.WorkspaceID).Int64()
	if uid == 0 || wid == 0 {
		return nil, fmt.Errorf("invalid id")
	}
	s, err := c.repository.ActiveSessionForWorkspace(ctx, wid, uid)
	if err != nil {
		if errors.Is(err, base.ErrNotFound) {
			// No agent is the ordinary answer, and not an error: every first
			// launch for a workspace passes through here.
			return nil, nil
		}
		return nil, err
	}
	v := toSessionView(s)
	return &v, nil
}

// ForkFolderMachines names the machines holding a fork's folder, as base62
// ids. A fork with a folder but no machine on record ran before machines were
// recorded; it is refused rather than answered with none, which would let a
// merge leave the folder behind and report it gone.
func (c *controller) ForkFolderMachines(ctx context.Context, req entity.ActiveSessionRequest) ([]string, error) {
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	wid := monoflake.IDFromBase62(req.WorkspaceID).Int64()
	if uid == 0 || wid == 0 {
		return nil, fmt.Errorf("invalid id")
	}
	ids, err := c.repository.ForkFolderMachines(ctx, wid, uid)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		ws, err := c.repository.GetWorkspace(ctx, wid, uid)
		if err != nil {
			return nil, err
		}
		if ws.WorkingDirectory != "" {
			return nil, entity.NewForkError(entity.ErrForkAgentRunning,
				"there is no record of which machine holds the fork's folder — merge without deleting it, and delete "+
					ws.WorkingDirectory+" by hand")
		}
	}
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = monoflake.ID(id).String()
	}
	return out, nil
}

// ReconcileSessions ends the sessions a machine is no longer running.
//
// The daemon is the authority on what is alive on its own machine: it says
// what it is supervising, and anything else still marked live has ended
// without anybody being told. Most often that is a daemon that restarted,
// which comes back supervising nothing — and those rows would otherwise sit as
// "running" forever and block the workspace's next launch.
func (c *controller) ReconcileSessions(ctx context.Context, req entity.ReconcileSessionsRequest) error {
	if req.MachineID == 0 {
		return fmt.Errorf("invalid machine id")
	}
	return c.repository.ReconcileSessions(ctx, req.MachineID, req.Running, time.Now())
}

// RecordTerminalView counts somebody opening or closing a session's terminal.
//
// Called from the socket handler rather than emitted there: that package
// copies bytes between two WebSockets and deliberately owns no database and no
// bus, which is what keeps it testable without either. So it reports the fact
// and this decides what to do with it.
//
// Nothing here can fail in a way worth telling the caller about — the caller
// is a socket that has already been authorised and is about to carry a
// terminal, and refusing to serve it because a counter did not increment
// would be the wrong trade. Hence no error.
func (c *controller) RecordTerminalView(ctx context.Context, req entity.RecordTerminalViewRequest) {
	if req.UserID == 0 {
		return
	}
	action := entity.ActionMachineTerminalOpen
	if !req.Open {
		action = entity.ActionMachineTerminalClose
	}
	c.emitEvent(ctx, entity.CRUDEvent{
		Action:       action,
		WorkspaceID:  req.WorkspaceID,
		UserID:       req.UserID,
		ResourceType: entity.ResourceSession,
		ResourceID:   req.SessionID,
		Actor:        entity.ActorHuman,
	})
}

// RecordSessionKill counts a person stopping an agent.
//
// Emitted from the handler that asks the daemon rather than from the state
// report that follows, because by then the two are indistinguishable: a
// killed session reports the same terminal state as one that finished, and
// what is worth counting here is that somebody decided to end it.
//
// No error, for the same reason as RecordTerminalView: the kill has already
// been sent, and failing the request afterwards over a counter would undo
// nothing and report a failure that did not happen.
func (c *controller) RecordSessionKill(ctx context.Context, req entity.RecordSessionKillRequest) {
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	if uid == 0 {
		return
	}
	c.emitEvent(ctx, entity.CRUDEvent{
		Action:       entity.ActionMachineSessionKill,
		WorkspaceID:  monoflake.IDFromBase62(req.WorkspaceID).Int64(),
		UserID:       uid,
		ResourceType: entity.ResourceSession,
		ResourceID:   monoflake.IDFromBase62(req.SessionID).Int64(),
		Actor:        entity.ActorHuman,
	})
}
