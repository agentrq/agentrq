// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

// Package forkmerge merges a workspace fork back into its parent. REST and
// CoreMCP both run this one flow, so neither can merge past a running agent.
package forkmerge

import (
	"context"
	"errors"
	"time"

	machinectrl "github.com/agentrq/agentrq/backend/internal/controller/machine"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	mapper "github.com/agentrq/agentrq/backend/internal/mapper/api"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
	"github.com/mustafaturan/monoflake"
	zlog "github.com/rs/zerolog/log"
)

// How long a merge waits, by default, for the daemon to report the fork's
// agent dead, and how often it looks.
const (
	defaultStopWait = 10 * time.Second
	defaultStopPoll = 250 * time.Millisecond
)

type (
	// Crud is the part of crud.Controller a merge uses.
	Crud interface {
		CheckForkMerge(ctx context.Context, req entity.MergeForkRequest) error
		ActiveSessionForWorkspace(ctx context.Context, req entity.ActiveSessionRequest) (*entity.SessionView, error)
		ForkFolderMachines(ctx context.Context, req entity.ActiveSessionRequest) ([]string, error)
		GetMachine(ctx context.Context, req entity.GetMachineRequest) (*entity.GetMachineResponse, error)
		GetSession(ctx context.Context, req entity.GetSessionRequest) (*entity.GetSessionResponse, error)
		MergeFork(ctx context.Context, req entity.MergeForkRequest) (*entity.MergeForkResponse, error)
	}

	// Servers holds the workspaces' MCP servers; the fork's goes with it.
	Servers interface {
		Remove(workspaceID int64)
	}

	// Merger merges forks. StopWait and StopPoll default when zero.
	Merger struct {
		Crud     Crud
		Machines *machinectrl.Registry
		Servers  Servers
		Bus      *eventbus.Bus
		StopWait time.Duration
		StopPoll time.Duration
	}
)

// Merge refuses a merge that would fail before touching the agent, then stops
// the fork's agent so it is gone before the workspace it works for, then moves
// the tasks and removes the fork.
func (m *Merger) Merge(ctx context.Context, rq entity.MergeForkRequest) (*entity.MergeForkResponse, error) {
	if err := m.Crud.CheckForkMerge(ctx, rq); err != nil {
		return nil, err
	}
	// Found while the fork still exists: once it is merged nothing says which
	// machines held its folder.
	folders, err := m.folderMachines(ctx, rq)
	if err != nil {
		return nil, err
	}
	if err := m.stopAgent(ctx, rq); err != nil {
		return nil, err
	}
	rs, err := m.Crud.MergeFork(ctx, rq)
	if err != nil {
		return nil, err
	}
	m.Servers.Remove(rq.WorkspaceID)
	for _, id := range folders {
		// The merge is done, so a machine that drops off now only keeps its
		// folder, which the machine page can still show.
		if err := machinectrl.RemoveForkDir(m.Machines, id, rq.WorkspaceID); err != nil {
			zlog.Warn().Err(err).Int64("machine", id).Msg("[fork] the folder was not removed")
		}
	}

	// Each task leaves the fork and appears in the parent, as a move does.
	parentID := monoflake.IDFromBase62(rs.ParentID).Int64()
	for _, t := range rs.Tasks {
		m.Bus.Publish(rq.WorkspaceID, rq.UserID, eventbus.Event{
			Type:    "task.deleted",
			Payload: map[string]string{"id": monoflake.ID(t.ID).String()},
		})
		m.Bus.Publish(parentID, rq.UserID, eventbus.Event{
			Type:    "task.created",
			Payload: mapper.FromEntityTaskToView(t),
		})
	}
	return rs, nil
}

// folderMachines lists the machines whose copy of the fork's folder a merge
// was asked to delete, and refuses the merge when one of them cannot be told:
// after it the fork is gone, and so is the way to find the folder.
func (m *Merger) folderMachines(ctx context.Context, rq entity.MergeForkRequest) ([]int64, error) {
	if !rq.DeleteFolder {
		return nil, nil
	}
	ids, err := m.Crud.ForkFolderMachines(ctx, entity.ActiveSessionRequest{
		UserID:      rq.UserID,
		WorkspaceID: monoflake.ID(rq.WorkspaceID).String(),
	})
	if err != nil {
		return nil, err
	}
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		machineID := monoflake.IDFromBase62(id).Int64()
		if !machinectrl.CanRemoveForkDir(m.Machines, machineID) {
			name := "a machine that ran it"
			if mr, err := m.Crud.GetMachine(ctx, entity.GetMachineRequest{UserID: rq.UserID, MachineID: id}); err == nil && mr.Machine.Name != "" {
				name = mr.Machine.Name
			}
			return nil, entity.NewForkError(entity.ErrForkAgentRunning, "the fork's folder is on "+name+
				", which is offline or runs an agentrqd that cannot delete it — merge without deleting the folder, or bring the machine back")
		}
		out = append(out, machineID)
	}
	return out, nil
}

// stopAgent kills the fork's running agent, if it has one, and waits for the
// daemon to say it is dead. Merging with it alive would leave it working, with
// its token on disk, for a workspace that no longer exists.
func (m *Merger) stopAgent(ctx context.Context, rq entity.MergeForkRequest) error {
	s, err := m.Crud.ActiveSessionForWorkspace(ctx, entity.ActiveSessionRequest{
		UserID:      rq.UserID,
		WorkspaceID: monoflake.ID(rq.WorkspaceID).String(),
	})
	if err != nil || s == nil {
		return err
	}
	machine := "its machine"
	if mr, err := m.Crud.GetMachine(ctx, entity.GetMachineRequest{UserID: rq.UserID, MachineID: s.MachineID}); err == nil && mr.Machine.Name != "" {
		machine = mr.Machine.Name
	}
	if err := machinectrl.KillSession(m.Machines, monoflake.IDFromBase62(s.MachineID).Int64(), s.ID); err != nil {
		return entity.NewForkError(entity.ErrForkAgentRunning, "the fork's agent is on "+machine+
			", which is offline — stop it from the machine page or bring the machine back")
	}

	wait, poll := m.StopWait, m.StopPoll
	if wait == 0 {
		wait = defaultStopWait
	}
	if poll == 0 {
		poll = defaultStopPoll
	}
	deadline := time.Now().Add(wait)
	for {
		rs, err := m.Crud.GetSession(ctx, entity.GetSessionRequest{UserID: rq.UserID, SessionID: s.ID})
		if errors.Is(err, base.ErrNotFound) {
			// The row of a session that ended is deleted, usually before
			// the first look: gone means dead.
			return nil
		}
		if err != nil {
			return err
		}
		if machinectrl.SessionTerminal(rs.Session.Status) {
			return nil
		}
		if time.Now().After(deadline) {
			return entity.NewForkError(entity.ErrForkAgentRunning, "the fork's agent on "+machine+
				" has not stopped yet — try the merge again in a moment")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(poll):
		}
	}
}
