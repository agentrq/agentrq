// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package teamview

import (
	"context"
	"errors"
	"reflect"
	"testing"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/mustafaturan/monoflake"
)

// unreadable is the [team, workspace] pairs CanReadWorkspaceInTeam refuses.
var unreadable = map[*fake]map[[2]int64]bool{}

// memoryReads is the owner reads the fake was asked for, as "user/workspace/name".
var memoryReads = map[*fake][]string{}

func (f *fake) CanReadWorkspaceInTeam(_ context.Context, userID, teamID, workspaceID int64) (bool, error) {
	if err := f.err("CanReadWorkspaceInTeam"); err != nil {
		return false, err
	}
	return userID == 2 && !unreadable[f][[2]int64{teamID, workspaceID}], nil
}

func (f *fake) ListMemories(_ context.Context, req entity.ListMemoriesRequest) (*entity.ListMemoriesResponse, error) {
	memoryReads[f] = append(memoryReads[f], req.UserID+"/"+monoflake.ID(req.WorkspaceID).String())
	if err := f.err("ListMemories"); err != nil {
		return nil, err
	}
	return &entity.ListMemoriesResponse{Memories: []entity.Memory{{ID: 7, WorkspaceID: req.WorkspaceID, Name: "memory.md", SizeBytes: 5}}}, nil
}

func (f *fake) GetMemory(_ context.Context, req entity.GetMemoryRequest) (*entity.GetMemoryResponse, error) {
	memoryReads[f] = append(memoryReads[f], req.UserID+"/"+monoflake.ID(req.WorkspaceID).String()+"/"+req.Name)
	if err := f.err("GetMemory"); err != nil {
		return nil, err
	}
	return &entity.GetMemoryResponse{Memory: entity.Memory{ID: 7, WorkspaceID: req.WorkspaceID, Name: req.Name, Content: "notes"}}, nil
}

func owner(userID, workspaceID int64, name ...string) string {
	s := monoflake.ID(userID).String() + "/" + monoflake.ID(workspaceID).String()
	for _, n := range name {
		s += "/" + n
	}
	return s
}

// A teammate's memories are read as their owner reads them, from the
// teammate's own workspace, and a read of one is counted.
func TestReadTeammateMemory(t *testing.T) {
	f, c := newFake()
	ctx := context.Background()
	list, err := c.ListTeammateMemoryNames(ctx, 2, 200, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Memories) != 1 || list.Memories[0].Name != "memory.md" {
		t.Fatalf("list = %+v, want alice's memory.md", list.Memories)
	}
	got, err := c.GetTeammateMemory(ctx, 2, 200, 100, "memory.md")
	if err != nil {
		t.Fatal(err)
	}
	if got.Memory.Content != "notes" {
		t.Fatalf("memory = %+v, want alice's notes", got.Memory)
	}
	want := []string{owner(1, 100), owner(1, 100, "memory.md")}
	if !reflect.DeepEqual(memoryReads[f], want) {
		t.Fatalf("owner reads %v, want %v", memoryReads[f], want)
	}
	if len(f.published) != 1 {
		t.Fatalf("published %v, want one view", f.published)
	}
	ev := f.published[0]
	if ev.Action != entity.ActionTeammateMemoryView || ev.UserID != 2 || ev.WorkspaceID != 200 || ev.ResourceID != 7 {
		t.Fatalf("event = %+v, want a teammate memory view by 2 in 200 of memory 7", ev)
	}
}

// Leaving a teammate out of the agent's index does not hide their memory
// from the person.
func TestReadExcludedTeammateMemory(t *testing.T) {
	_, c := newFake()
	ctx := context.Background()
	if _, err := c.SetTeammateMemory(ctx, 2, 200, 3, false); err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetTeammateMemory(ctx, 2, 200, 300, "memory.md"); err != nil {
		t.Fatalf("read carol's memory after leaving her out: %v", err)
	}
}

// A teammate's workspace the team does not let the caller read is not found.
func TestReadTeammateMemoryChecksTheTeam(t *testing.T) {
	f, c := newFake()
	unreadable[f] = map[[2]int64]bool{{10, 100}: true}
	if _, err := c.GetTeammateMemory(context.Background(), 2, 200, 100, "memory.md"); !errors.Is(err, base.ErrNotFound) {
		t.Fatalf("alice's 100, unreadable in team 10: %v, want not found", err)
	}
}

// Only a teammate's workspace in this team workspace is read: not the
// caller's own, not another team workspace's, not someone else's from
// a workspace that is not the caller's.
func TestReadTeammateMemoryOnlyOfTeammates(t *testing.T) {
	f, c := newFake()
	ctx := context.Background()
	for _, tc := range []struct {
		name                string
		user, workspace, of int64
	}{
		{"own workspace", 2, 200, 200},
		{"another team workspace", 2, 200, 400},
		{"not in the team", 2, 200, 999},
		{"someone else's workspace", 3, 200, 100},
	} {
		if _, err := c.ListTeammateMemoryNames(ctx, tc.user, tc.workspace, tc.of); !errors.Is(err, base.ErrNotFound) {
			t.Errorf("list, %s: %v, want not found", tc.name, err)
		}
		if _, err := c.GetTeammateMemory(ctx, tc.user, tc.workspace, tc.of, "memory.md"); !errors.Is(err, base.ErrNotFound) {
			t.Errorf("get, %s: %v, want not found", tc.name, err)
		}
	}
	if len(memoryReads[f]) != 0 || len(f.published) != 0 {
		t.Fatalf("read %v and published %v for no teammate", memoryReads[f], f.published)
	}
}

// Every failure is passed on, as it is, and nothing is counted.
func TestReadTeammateMemoryFailures(t *testing.T) {
	ctx := context.Background()
	for _, fail := range []string{"GetWorkspace", "ListTeamPeerWorkspaces", "CanReadWorkspaceInTeam", "ListMemories", "GetMemory"} {
		f, c := newFake()
		f.fail = fail
		_, errList := c.ListTeammateMemoryNames(ctx, 2, 200, 100)
		_, errGet := c.GetTeammateMemory(ctx, 2, 200, 100, "memory.md")
		if fail != "GetMemory" && !errors.Is(errList, errBoom) {
			t.Errorf("list, %s: %v, want %v", fail, errList, errBoom)
		}
		if fail != "ListMemories" && !errors.Is(errGet, errBoom) {
			t.Errorf("get, %s: %v, want %v", fail, errGet, errBoom)
		}
		if errGet != nil && len(f.published) != 0 {
			t.Errorf("%s: published %v after a failure", fail, f.published)
		}
	}
}
