// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package coremcp

import (
	"cmp"
	"context"
	"slices"
	"time"

	"github.com/agentrq/agentrq/backend/internal/controller/agentlink"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/mustafaturan/monoflake"
)

// AgentPresence is the MCP manager: whether an agent is attached to a
// workspace on this server, and the model it says it runs.
type AgentPresence interface {
	IsAgentConnected(workspaceID int64) bool
	AgentModels(workspaceID int64) *agentlink.ModelsSnapshot
}

// The states an agent reads as.
const (
	agentRunning = "running"
	agentIdle    = "idle"
	agentPaused  = "paused"
	agentOffline = "offline"
)

// recentOutput is how long after its terminal last printed an agent still
// reads as running. Claude Code redraws its spinner every second while it
// works, and agentrqd reports every 15 seconds.
const recentOutput = time.Minute

// agentStateHelp is the help text every action that shows an agent shares.
const agentStateHelp = "agent.state is running (its terminal printed in the last minute; an agent agentrqd does not run reads running while it has an ongoing task), idle, paused (it hit its usage limit; pausedUntil is when the limit lifts, when it said) or offline (no agent connected or running). taskId is its latest ongoing task, lastActivityAt when its terminal last printed."

// agentActivity is what the database knows of every agent of the account.
func (s *WorkspaceServer) agentActivity(ctx context.Context, userID string) (map[int64]entity.AgentActivity, error) {
	res, err := s.crud.ListAgentActivity(ctx, entity.ListAgentActivityRequest{UserID: userID})
	if err != nil {
		return nil, err
	}
	return res.Workspaces, nil
}

// agentState is a workspace's agent, with what this server knows of its
// connection added to what the database knows.
func (s *WorkspaceServer) agentState(workspaceID int64, a entity.AgentActivity, now time.Time) (entity.AgentState, bool) {
	connected, model := false, ""
	if s.agents != nil {
		connected = s.agents.IsAgentConnected(workspaceID)
		if m := s.agents.AgentModels(workspaceID); m != nil {
			model = m.CurrentModel
		}
	}
	return agentStateOf(workspaceID, a, connected, model, now), connected
}

// fillAgent sets what the workspace's agent is doing.
func (s *WorkspaceServer) fillAgent(w *entity.Workspace, activity map[int64]entity.AgentActivity, now time.Time) {
	st, connected := s.agentState(w.ID, activity[w.ID], now)
	w.AgentConnected = connected
	w.Agent = &st
}

// fillMachineAgents lists on each machine the agents it runs.
func (s *WorkspaceServer) fillMachineAgents(machines []entity.MachineView, activity map[int64]entity.AgentActivity, now time.Time) {
	byMachine := make(map[string]int, len(machines))
	for i, m := range machines {
		byMachine[m.ID] = i
	}
	for id, a := range activity {
		if a.MachineID == 0 {
			continue
		}
		st, _ := s.agentState(id, a, now)
		if i, ok := byMachine[st.MachineID]; ok {
			machines[i].Agents = append(machines[i].Agents, st)
		}
	}
	for i := range machines {
		slices.SortFunc(machines[i].Agents, func(x, y entity.AgentState) int {
			return cmp.Or(cmp.Compare(x.WorkspaceName, y.WorkspaceName), cmp.Compare(x.WorkspaceID, y.WorkspaceID))
		})
	}
}

// agentStateOf decides what an agent is doing. A paused agent is still
// connected and calls nothing, so the terminal is what tells it from one at
// work: a usage-limit notice whose reset is still ahead, or one with no reset
// and nothing printed since. An agent with no terminal to read — not run by
// agentrqd, or by one too old to report it — falls back on its task.
func agentStateOf(workspaceID int64, a entity.AgentActivity, connected bool, model string, now time.Time) entity.AgentState {
	st := entity.AgentState{
		WorkspaceID:    monoflake.ID(workspaceID).String(),
		WorkspaceName:  a.WorkspaceName,
		Model:          cmp.Or(model, a.Model),
		LastActivityAt: a.LastOutputAt,
	}
	if a.MachineID != 0 {
		st.MachineID = monoflake.ID(a.MachineID).String()
	}
	if a.TaskID != 0 {
		st.TaskID = monoflake.ID(a.TaskID).String()
	}
	switch {
	case a.MachineID == 0 && !connected:
		st.State = agentOffline
	case a.LastOutputAt == nil:
		st.State = agentIdle
		if a.TaskID != 0 {
			st.State = agentRunning
		}
	case a.UsageLimited && a.UsageResetsAt != nil && a.UsageResetsAt.After(now),
		a.UsageLimited && a.UsageResetsAt == nil && now.Sub(*a.LastOutputAt) >= recentOutput:
		st.State = agentPaused
		st.PausedUntil = a.UsageResetsAt
	case now.Sub(*a.LastOutputAt) < recentOutput:
		st.State = agentRunning
	default:
		st.State = agentIdle
	}
	return st
}
