// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package crud

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/mustafaturan/monoflake"
	"gorm.io/datatypes"
)

// supervisorWorkspaceName is the account-wide workspace, which is never forked:
// it is found by its name, and a second one would answer to it too.
const supervisorWorkspaceName = "supervisor"

// maxWorkspaceNameRunes is the width of the workspaces.name column.
const maxWorkspaceNameRunes = 128

// ForkWorkspace makes a fork of a workspace: a workspace of its own, with its
// own queue and agent, that inherits the parent's settings and is merged back
// into it when its tasks are done.
func (c *controller) ForkWorkspace(ctx context.Context, req entity.ForkWorkspaceRequest) (*entity.ForkWorkspaceResponse, error) {
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	if c.limiter != nil && !c.limiter.AllowWorkspace(uid) {
		return nil, fmt.Errorf("rate limit exceeded")
	}
	parent, err := c.repository.GetWorkspace(ctx, req.WorkspaceID, uid)
	if err != nil {
		return nil, err
	}
	switch {
	case parent.ForkOfID != 0:
		return nil, entity.NewForkError(entity.ErrForkOfFork, "a fork cannot be forked")
	case parent.Name == supervisorWorkspaceName:
		return nil, entity.NewForkError(entity.ErrForkOfFork, "the supervisor workspace cannot be forked")
	case parent.ArchivedAt != nil:
		return nil, entity.NewForkError(entity.ErrForkOfFork, "an archived workspace cannot be forked")
	}

	name := req.Name
	if name == "" {
		name = forkName(parent.Name)
	}

	// Everything is the parent's but what makes the fork a place of its own:
	// its identity, its name, and its folder, which the daemon makes on the
	// first launch.
	now := time.Now()
	m := parent
	m.ID = c.idgen.NextID()
	m.CreatedAt = now
	m.UpdatedAt = now
	m.Name = name
	m.WorkingDirectory = ""
	m.ForkOfID = parent.ID

	created, err := c.repository.CreateWorkspace(ctx, m)
	if err != nil {
		return nil, fmt.Errorf("create workspace: %w", err)
	}
	for _, action := range []entity.Action{entity.ActionWorkspaceCreate, entity.ActionWorkspaceForkCreate} {
		c.emitEvent(ctx, entity.CRUDEvent{
			Action:       action,
			WorkspaceID:  created.ID,
			UserID:       created.UserID,
			ResourceType: entity.ResourceWorkspace,
			ResourceID:   created.ID,
			Actor:        entity.ActorHuman,
		})
	}
	w := fromModelWorkspaceToEntity(created)
	w.ForkOf = &entity.WorkspaceRef{ID: parent.ID, Name: parent.Name}
	return &entity.ForkWorkspaceResponse{Workspace: w}, nil
}

// notKebab is what kebab-case turns into a single hyphen, as the web app's
// workspace form does.
var notKebab = regexp.MustCompile(`[^a-z0-9]+`)

// forkName is the name a fork gets when none is given: "<parent>-fork" in
// kebab-case, like every workspace name, cut so it still fits.
func forkName(parent string) string {
	const suffix = "-fork"
	base := strings.Trim(notKebab.ReplaceAllString(strings.ToLower(parent), "-"), "-")
	if keep := maxWorkspaceNameRunes - len(suffix); len(base) > keep {
		base = strings.TrimRight(base[:keep], "-") // ASCII only, so bytes are runes
	}
	if base == "" {
		return "fork"
	}
	return base + suffix
}

// MergeFork moves every task of a fork back into its parent and removes the
// fork. It is refused while any of those tasks is unfinished.
func (c *controller) MergeFork(ctx context.Context, req entity.MergeForkRequest) (*entity.MergeForkResponse, error) {
	fork, err := c.forkToMerge(ctx, req)
	if err != nil {
		return nil, err
	}
	moved, err := c.repository.MergeForkIntoParent(ctx, fork.ID, fork.ForkOfID)
	if err != nil {
		return nil, err
	}
	c.emitEvent(ctx, entity.CRUDEvent{
		Action:       entity.ActionWorkspaceForkMerge,
		WorkspaceID:  fork.ForkOfID,
		UserID:       fork.UserID,
		ResourceType: entity.ResourceWorkspace,
		ResourceID:   fork.ID,
		Actor:        entity.ActorHuman,
	})

	// The merge is done; a task that cannot be read back only misses its
	// live update, and shows on the next load.
	tasks := make([]entity.Task, 0, len(moved))
	for _, id := range moved {
		t, err := c.repository.GetTask(ctx, fork.ForkOfID, id, fork.UserID)
		if err != nil {
			continue
		}
		tasks = append(tasks, c.fromModelTaskToEntity(t))
	}
	return &entity.MergeForkResponse{
		ParentID:   monoflake.ID(fork.ForkOfID).String(),
		MovedTasks: len(moved),
		Tasks:      tasks,
	}, nil
}

// CheckForkMerge says whether a merge would be refused, before the fork's agent
// is stopped for it: a merge that is going to fail must not kill the agent.
// MergeFork asks again inside its transaction, which is the check that holds.
func (c *controller) CheckForkMerge(ctx context.Context, req entity.MergeForkRequest) error {
	fork, err := c.forkToMerge(ctx, req)
	if err != nil {
		return err
	}
	unfinished, err := c.repository.CountUnfinishedTasks(ctx, []int64{fork.ID})
	if err != nil {
		return err
	}
	if n := unfinished[fork.ID]; n > 0 {
		return entity.NewForkError(entity.ErrForkUnfinished, base.UnfinishedForkMessage(n))
	}
	return nil
}

// forkToMerge reads the workspace a merge names, refusing one that is no fork.
func (c *controller) forkToMerge(ctx context.Context, req entity.MergeForkRequest) (model.Workspace, error) {
	fork, err := c.repository.GetWorkspace(ctx, req.WorkspaceID, monoflake.IDFromBase62(req.UserID).Int64())
	if err != nil {
		return model.Workspace{}, err
	}
	if fork.ForkOfID == 0 {
		return model.Workspace{}, entity.NewForkError(entity.ErrNotAFork, "only a fork can be merged")
	}
	return fork, nil
}

// refuseForkOrParent says why a workspace cannot be deleted or archived: a
// fork only leaves by merging, and a parent not before its forks have.
func (c *controller) refuseForkOrParent(ctx context.Context, m model.Workspace) error {
	if m.ForkOfID != 0 {
		return entity.NewForkError(entity.ErrForkNoDelete, "merge this fork instead")
	}
	n, err := c.repository.CountForks(ctx, m.ID, m.UserID)
	if err != nil {
		return err
	}
	switch {
	case n == 1:
		return entity.NewForkError(entity.ErrHasForks, "this workspace has 1 fork; merge it first")
	case n > 1:
		return entity.NewForkError(entity.ErrHasForks, fmt.Sprintf("this workspace has %d forks; merge them first", n))
	}
	return nil
}

// refuseInheritedChange refuses an update to a fork that would change what it
// inherits. Its own name, icon, description and folder still change.
func (c *controller) refuseInheritedChange(ctx context.Context, before, after model.Workspace) error {
	if before.ForkOfID == 0 || sameForkSettings(before, after) {
		return nil
	}
	parent := "its parent"
	if p, err := c.repository.GetWorkspace(ctx, before.ForkOfID, before.UserID); err == nil {
		parent = p.Name
	}
	return entity.NewForkError(entity.ErrForkInherited, fmt.Sprintf("this setting is inherited from %s; change it there", parent))
}

// sameForkSettings compares what a fork inherits by meaning: a settings form
// sends back the same settings it was given, but not always the same bytes —
// a key left out reads as false, just as one set to false does.
func sameForkSettings(a, b model.Workspace) bool {
	bs := b.ForkSettings()
	for k, va := range a.ForkSettings() {
		if !reflect.DeepEqual(normalizeSetting(va), normalizeSetting(bs[k])) {
			return false
		}
	}
	return true
}

func normalizeSetting(v any) any {
	j, ok := v.(datatypes.JSON)
	if !ok {
		return v
	}
	var out any
	if len(j) == 0 || json.Unmarshal(j, &out) != nil {
		return nil
	}
	return dropZero(out)
}

// dropZero turns every empty or zero JSON value into nil, and drops the keys
// that hold one.
func dropZero(v any) any {
	switch x := v.(type) {
	case []any:
		if len(x) == 0 {
			return nil
		}
	case map[string]any:
		for k, e := range x {
			if dropZero(e) == nil {
				delete(x, k)
			}
		}
		if len(x) == 0 {
			return nil
		}
	case bool:
		if !x {
			return nil
		}
	case string:
		if x == "" {
			return nil
		}
	case float64:
		if x == 0 {
			return nil
		}
	}
	return v
}

// withForkInfo fills in, across a list, what the interface shows about forks:
// a fork's parent and unfinished tasks, and how many forks a parent has.
func (c *controller) withForkInfo(ctx context.Context, ws []entity.Workspace) error {
	byID := make(map[int64]int, len(ws))
	for i := range ws {
		byID[ws[i].ID] = i
	}
	var forkIDs []int64
	for i := range ws {
		if ws[i].ForkOfID == 0 {
			continue
		}
		forkIDs = append(forkIDs, ws[i].ID)
		if p, ok := byID[ws[i].ForkOfID]; ok {
			ws[i].ForkOf = &entity.WorkspaceRef{ID: ws[p].ID, Name: ws[p].Name}
			ws[p].ForkCount++
		}
	}
	if len(forkIDs) == 0 {
		return nil
	}
	unfinished, err := c.repository.CountUnfinishedTasks(ctx, forkIDs)
	if err != nil {
		return err
	}
	for _, id := range forkIDs {
		ws[byID[id]].UnfinishedTasks = int(unfinished[id])
	}
	return nil
}

// RecordForkDirectory stores the folder a daemon made for a fork's session as
// the fork's working directory, so it can be shown and relaunched from. A
// session of any other workspace is left alone: its folder is the person's.
func (c *controller) RecordForkDirectory(ctx context.Context, req entity.RecordForkDirectoryRequest) error {
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	id := monoflake.IDFromBase62(req.SessionID).Int64()
	if uid == 0 || id == 0 {
		return fmt.Errorf("invalid id")
	}
	dir, err := normalizeWorkingDirectory(req.Dir)
	if err != nil || dir == "" {
		return err
	}
	session, err := c.repository.GetSession(ctx, id, uid)
	if err != nil {
		return err
	}
	ws, err := c.repository.GetWorkspace(ctx, session.WorkspaceID, uid)
	if err != nil {
		return err
	}
	if ws.ForkOfID == 0 {
		return nil
	}
	if err := c.repository.RecordForkFolder(ctx, ws.ID, session.MachineID, uid); err != nil {
		return err
	}
	if ws.WorkingDirectory == dir {
		return nil
	}
	return c.repository.SetWorkingDirectory(ctx, ws.ID, uid, dir)
}
