// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package crud

import (
	"context"
	"fmt"
	"time"

	"github.com/mustafaturan/monoflake"

	machinerules "github.com/agentrq/agentrq/backend/internal/controller/machine"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
)

// SessionController records agent sessions.
type SessionController interface {
	CreateSession(ctx context.Context, req entity.CreateSessionRequest) (*entity.CreateSessionResponse, error)
	UpdateSessionState(ctx context.Context, req entity.UpdateSessionStateRequest) error
	GetSession(ctx context.Context, req entity.GetSessionRequest) (*entity.GetSessionResponse, error)
	ListSessions(ctx context.Context, req entity.ListSessionsRequest) (*entity.ListSessionsResponse, error)
	ReconcileSessions(ctx context.Context, req entity.ReconcileSessionsRequest) error
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
func (c *controller) UpdateSessionState(ctx context.Context, req entity.UpdateSessionStateRequest) error {
	id := monoflake.IDFromBase62(req.SessionID).Int64()
	if id == 0 {
		return fmt.Errorf("invalid session id")
	}
	return c.repository.UpdateSessionState(ctx, id, req.Status, req.ExitCode, req.EndedAt, req.Restored)
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
	return &entity.GetSessionResponse{Session: toSessionView(s)}, nil
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
	return &entity.ListSessionsResponse{Sessions: out}, nil
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
