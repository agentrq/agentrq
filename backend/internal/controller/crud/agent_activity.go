// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package crud

import (
	"context"
	"fmt"

	"github.com/mustafaturan/monoflake"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
)

// RecordSessionActivity stores what a heartbeat said of each session's
// terminal.
func (c *controller) RecordSessionActivity(ctx context.Context, req entity.RecordSessionActivityRequest) error {
	return c.repository.RecordSessionActivity(ctx, req.MachineID, req.Sessions)
}

// ListAgentActivity is what the database knows of each workspace's agent: its
// live session, newest first if there are several, and its latest ongoing
// task. Whether it is connected is the MCP server's to say, not this.
func (c *controller) ListAgentActivity(ctx context.Context, req entity.ListAgentActivityRequest) (*entity.ListAgentActivityResponse, error) {
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	if uid == 0 {
		return nil, fmt.Errorf("invalid userID")
	}
	sessions, err := c.repository.ListLiveSessionsByUser(ctx, uid)
	if err != nil {
		return nil, err
	}
	tasks, err := c.repository.LatestOngoingAgentTasks(ctx, uid)
	if err != nil {
		return nil, err
	}

	out := make(map[int64]entity.AgentActivity, len(sessions)+len(tasks))
	ids := make([]int64, 0, len(sessions))
	for _, s := range sessions {
		if _, seen := out[s.WorkspaceID]; seen {
			continue
		}
		out[s.WorkspaceID] = entity.AgentActivity{
			WorkspaceID:   s.WorkspaceID,
			MachineID:     s.MachineID,
			Model:         s.Model,
			LastOutputAt:  s.LastOutputAt,
			UsageLimited:  s.UsageLimited,
			UsageResetsAt: s.UsageResetsAt,
		}
		ids = append(ids, s.WorkspaceID)
	}
	for wid, tid := range tasks {
		a := out[wid]
		a.WorkspaceID, a.TaskID = wid, tid
		out[wid] = a
	}
	// Best effort, as nameWorkspaces is: a machine's agents without names
	// are still the answer.
	if len(ids) > 0 {
		if names, err := c.repository.WorkspaceNamesByID(ctx, ids, uid); err == nil {
			for _, id := range ids {
				a := out[id]
				a.WorkspaceName = names[id]
				out[id] = a
			}
		}
	}
	return &entity.ListAgentActivityResponse{Workspaces: out}, nil
}
