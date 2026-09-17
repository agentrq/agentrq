// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

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
	if err := c.repository.UpdateSessionState(ctx, id, req.Status, req.ExitCode, req.EndedAt, req.Restored); err != nil {
		return err
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
