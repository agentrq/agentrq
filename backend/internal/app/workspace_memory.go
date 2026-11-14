// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/agentrq/agentrq/backend/internal/controller/mcp"
	"github.com/agentrq/agentrq/backend/internal/controller/teamview"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/idgen"
	"github.com/mustafaturan/monoflake"
)

// mcpWorkspace reads the workspace an MCP server is being built for, and
// returns it with the account it belongs to and the workspace whose memory,
// skills, site shares and attachment files it uses — its parent, for a fork.
// A fork's parent never changes, so this is resolved once per server. A
// workspace that cannot be read is taken to be userID's, and its own.
func mcpWorkspace(ctx context.Context, repo base.Repository, workspaceID int64, userID string) (model.Workspace, string, int64) {
	w, err := repo.SystemGetWorkspace(ctx, workspaceID)
	if err != nil {
		return w, userID, workspaceID
	}
	return w, monoflake.ID(w.UserID).String(), w.ContentID()
}

// workspaceMemory is a workspace's memory for its MCP server. Scoped to the
// workspace and its owner here, so the tools themselves only ever name a
// memory — an agent cannot reach another workspace's notes by asking for them.
//
// The one exception is a workspace shared with a team. There, memory.md is
// an index built at each load, one line per member of its team workspace
// (the shared workspaces of its name, in every team it is shared with), and
// "<workspaceId>/<name>" reads a member's memory, read-only, after
// CanReadWorkspaceInTeam allows it. The agent's own memory is still saved
// under a plain name, or under its own workspace's id.
type workspaceMemory struct {
	repo        base.Repository
	ids         idgen.Service
	workspaceID int64
	userID      string
}

// errTeammateMemoryReadOnly refuses a write to another member's memory.
var errTeammateMemoryReadOnly = errors.New("a teammate's memory is read-only: save to your own memory instead")

// errNotTeammate is a workspace id no team of this workspace lets it read.
var errNotTeammate = errors.New("no team this workspace is shared with has that workspace: load memory.md for the ones it has")

// teamIndexHeader starts a team workspace's memory index.
const teamIndexHeader = "This is a shared team memory for this workspace, each user's agent built its own memory:"

func (m workspaceMemory) uid() int64 { return monoflake.IDFromBase62(m.userID).Int64() }

// split is a name's workspace and the name within it: this workspace for a
// plain name (prefixed false), or for its own id.
func (m workspaceMemory) split(name string) (workspaceID int64, rest string, prefixed bool) {
	id, rest, ok := strings.Cut(name, "/")
	if !ok {
		return m.workspaceID, name, false
	}
	return monoflake.IDFromBase62(id).Int64(), rest, true
}

// teamPeers is the members of this workspace's team workspace: each
// workspace of its name shared with a team it is shared with, once, with
// that team. None when it is shared with no team.
func (m workspaceMemory) teamPeers(ctx context.Context) ([]base.TeamWorkspacePeer, error) {
	rows, err := m.repo.ListTeamPeerWorkspaces(ctx, m.workspaceID)
	if err != nil {
		return nil, err
	}
	key := ""
	for _, r := range rows {
		if r.WorkspaceID == m.workspaceID {
			key = teamview.WorkspaceKey(r.Name)
		}
	}
	seen := map[int64]bool{}
	out := []base.TeamWorkspacePeer{}
	for _, r := range rows {
		if teamview.WorkspaceKey(r.Name) == key && !seen[r.WorkspaceID] {
			seen[r.WorkspaceID] = true
			out = append(out, r)
		}
	}
	return out, nil
}

// teamIndex is the memory index of a team workspace, a line per member's
// workspace, by owner, the agent's own marked.
func (m workspaceMemory) teamIndex(peers []base.TeamWorkspacePeer) string {
	sort.SliceStable(peers, func(i, j int) bool {
		a, b := strings.ToLower(peers[i].OwnerName), strings.ToLower(peers[j].OwnerName)
		if a != b {
			return a < b
		}
		return peers[i].WorkspaceID < peers[j].WorkspaceID
	})
	var b strings.Builder
	b.WriteString(teamIndexHeader)
	for _, p := range peers {
		owner := p.OwnerName
		if owner == "" {
			owner = "A member"
		}
		fmt.Fprintf(&b, "\n%s: %s/%s", owner, monoflake.ID(p.WorkspaceID).String(), mcp.DefaultMemoryName)
		if p.WorkspaceID == m.workspaceID {
			b.WriteString(" (your actual memory that you can update)")
		}
	}
	return b.String() + "\n"
}

// peer is the member's workspace the agent may read, by id: one of this
// workspace's team peers, which CanReadWorkspaceInTeam allows its owner to
// read in that team. errNotTeammate otherwise.
func (m workspaceMemory) peer(ctx context.Context, workspaceID int64) (base.TeamWorkspacePeer, error) {
	peers, err := m.teamPeers(ctx)
	if err != nil {
		return base.TeamWorkspacePeer{}, err
	}
	for _, p := range peers {
		if p.WorkspaceID != workspaceID {
			continue
		}
		ok, err := m.repo.CanReadWorkspaceInTeam(ctx, m.uid(), p.TeamID, p.WorkspaceID)
		if err != nil {
			return base.TeamWorkspacePeer{}, err
		}
		if ok {
			return p, nil
		}
	}
	return base.TeamWorkspacePeer{}, errNotTeammate
}

// read is a memory of a workspace, as its owner reads it.
func (m workspaceMemory) read(ctx context.Context, userID, workspaceID int64, name string) (string, bool, error) {
	mem, err := m.repo.GetMemory(ctx, userID, workspaceID, name)
	if errors.Is(err, base.ErrNotFound) {
		// A memory nobody has written yet: the ordinary state of a
		// fresh workspace, and not a failure to report.
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return mem.Content, true, nil
}

func (m workspaceMemory) load(ctx context.Context, name string) (string, bool, error) {
	workspaceID, rest, prefixed := m.split(name)
	if workspaceID != m.workspaceID {
		p, err := m.peer(ctx, workspaceID)
		if err != nil {
			return "", false, err
		}
		return m.read(ctx, p.UserID, p.WorkspaceID, rest)
	}
	if !prefixed && rest == mcp.DefaultMemoryName {
		peers, err := m.teamPeers(ctx)
		if err != nil {
			return "", false, err
		}
		if len(peers) > 0 {
			return m.teamIndex(peers), true, nil
		}
	}
	return m.read(ctx, m.uid(), m.workspaceID, rest)
}

func (m workspaceMemory) save(ctx context.Context, name string, content string) error {
	workspaceID, rest, _ := m.split(name)
	if workspaceID != m.workspaceID {
		return errTeammateMemoryReadOnly
	}
	now := time.Now()
	_, err := m.repo.UpsertMemory(ctx, model.Memory{
		ID:          m.ids.NextID(),
		CreatedAt:   now,
		UpdatedAt:   now,
		UserID:      m.uid(),
		WorkspaceID: m.workspaceID,
		Name:        rest,
		Content:     content,
	})
	return err
}

func (m workspaceMemory) delete(ctx context.Context, name string) (bool, error) {
	workspaceID, rest, _ := m.split(name)
	if workspaceID != m.workspaceID {
		return false, errTeammateMemoryReadOnly
	}
	err := m.repo.DeleteMemory(ctx, m.uid(), m.workspaceID, rest)
	if errors.Is(err, base.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
