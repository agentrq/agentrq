// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package teamview

import (
	"context"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
)

// Peers is what TeamPeers and MemoryPeers read.
type Peers interface {
	ListTeamPeerWorkspaces(ctx context.Context, workspaceID int64) ([]base.TeamWorkspacePeer, error)
	ListMemoryExclusions(ctx context.Context, userID, workspaceID int64) ([]int64, error)
}

// TeamPeers is the members' workspaces in a workspace's team workspace: each
// workspace of its name shared with a team it is shared with, once, with
// that team, itself included. None when it is shared with no team.
func TeamPeers(ctx context.Context, repo Peers, workspaceID int64) ([]base.TeamWorkspacePeer, error) {
	rows, err := repo.ListTeamPeerWorkspaces(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	key := ""
	for _, r := range rows {
		if r.WorkspaceID == workspaceID {
			key = WorkspaceKey(r.Name)
		}
	}
	seen := map[int64]bool{}
	out := []base.TeamWorkspacePeer{}
	for _, r := range rows {
		if WorkspaceKey(r.Name) == key && !seen[r.WorkspaceID] {
			seen[r.WorkspaceID] = true
			out = append(out, r)
		}
	}
	return out, nil
}

// MemoryPeers is TeamPeers less the teammates whose memory userID, the
// workspace's owner, left out of its memory index. The owner's own
// workspaces always stay.
func MemoryPeers(ctx context.Context, repo Peers, userID, workspaceID int64) ([]base.TeamWorkspacePeer, error) {
	peers, err := TeamPeers(ctx, repo, workspaceID)
	if err != nil || len(peers) < 2 {
		return peers, err
	}
	excluded, err := excludedSet(ctx, repo, userID, workspaceID)
	if err != nil {
		return nil, err
	}
	out := peers[:0]
	for _, p := range peers {
		if p.UserID == userID || !excluded[p.UserID] {
			out = append(out, p)
		}
	}
	return out, nil
}

func excludedSet(ctx context.Context, repo Peers, userID, workspaceID int64) (map[int64]bool, error) {
	ids, err := repo.ListMemoryExclusions(ctx, userID, workspaceID)
	if err != nil {
		return nil, err
	}
	set := make(map[int64]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	return set, nil
}

// teammateMemories is the teammates in the team workspace of the user's own
// workspace, one per workspace of theirs, the user's own left out; base.
// ErrNotFound for a workspace that is not the user's.
func (c *controller) teammateMemories(ctx context.Context, userID, workspaceID int64) ([]entity.TeammateMemory, error) {
	if _, err := c.repo.GetWorkspace(ctx, workspaceID, userID); err != nil {
		return nil, err
	}
	peers, err := TeamPeers(ctx, c.repo, workspaceID)
	if err != nil {
		return nil, err
	}
	excluded, err := excludedSet(ctx, c.repo, userID, workspaceID)
	if err != nil {
		return nil, err
	}
	ids := []int64{}
	for _, p := range peers {
		if p.UserID != userID {
			ids = append(ids, p.WorkspaceID)
		}
	}
	updates, err := c.repo.MemoryUpdates(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := []entity.TeammateMemory{}
	for _, p := range peers {
		if p.UserID == userID {
			continue
		}
		m := entity.TeammateMemory{
			UserID: p.UserID, Name: p.OwnerName, Picture: p.OwnerPicture,
			WorkspaceID: p.WorkspaceID, Included: !excluded[p.UserID],
		}
		if at, ok := updates[p.WorkspaceID]; ok {
			m.UpdatedAt = &at
		}
		out = append(out, m)
	}
	return out, nil
}

func (c *controller) ListTeammateMemories(ctx context.Context, userID, workspaceID int64) ([]entity.TeammateMemory, error) {
	return c.teammateMemories(ctx, userID, workspaceID)
}

func (c *controller) SetTeammateMemory(ctx context.Context, userID, workspaceID, teammateID int64, included bool) (entity.TeammateMemory, error) {
	all, err := c.teammateMemories(ctx, userID, workspaceID)
	if err != nil {
		return entity.TeammateMemory{}, err
	}
	var found *entity.TeammateMemory
	for i := range all {
		if all[i].UserID == teammateID {
			found = &all[i]
			break
		}
	}
	if found == nil {
		return entity.TeammateMemory{}, base.ErrNotFound
	}
	action := entity.ActionTeammateMemoryInclude
	if included {
		err = c.repo.IncludeTeammateMemory(ctx, userID, workspaceID, teammateID)
	} else {
		action = entity.ActionTeammateMemoryExclude
		err = c.repo.ExcludeTeammateMemory(ctx, model.MemoryExclusion{
			WorkspaceID: workspaceID, UserID: userID, ExcludedUserID: teammateID,
		})
	}
	if err != nil {
		return entity.TeammateMemory{}, err
	}
	c.emit(ctx, action, userID, workspaceID, teammateID)
	found.Included = included
	return *found, nil
}
