// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package teamview

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
)

// memoryState is what the teammate-memory methods of fake read and write:
// workspace 200 is user 2's, shared as "API" with alice (1, workspace 100,
// and her "api " in a second team, 101), carol (3, 300) and dan, whose
// "Web" (400) is another team workspace.
type memoryState struct {
	peers    []base.TeamWorkspacePeer
	excluded map[[3]int64]bool // workspace, user, teammate
	updates  map[int64]time.Time
}

var memoryAt = time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)

var memories = map[*fake]*memoryState{}

func (f *fake) mem() *memoryState {
	if memories[f] == nil {
		memories[f] = &memoryState{
			peers: []base.TeamWorkspacePeer{
				{TeamID: 10, TeamWorkspaceID: 5, WorkspaceID: 100, UserID: 1, Name: "API", OwnerName: "Alice", OwnerPicture: "a.png"},
				{TeamID: 10, TeamWorkspaceID: 5, WorkspaceID: 200, UserID: 2, Name: "api", OwnerName: "Bob"},
				{TeamID: 10, TeamWorkspaceID: 5, WorkspaceID: 300, UserID: 3, Name: " Api", OwnerName: "Carol"},
				{TeamID: 10, TeamWorkspaceID: 6, WorkspaceID: 400, UserID: 4, Name: "Web", OwnerName: "Dan"},
				{TeamID: 11, TeamWorkspaceID: 7, WorkspaceID: 200, UserID: 2, Name: "api", OwnerName: "Bob"},
				{TeamID: 11, TeamWorkspaceID: 7, WorkspaceID: 100, UserID: 1, Name: "API", OwnerName: "Alice", OwnerPicture: "a.png"},
				{TeamID: 11, TeamWorkspaceID: 7, WorkspaceID: 101, UserID: 1, Name: "api ", OwnerName: "Alice", OwnerPicture: "a.png"},
			},
			excluded: map[[3]int64]bool{},
			updates:  map[int64]time.Time{100: memoryAt},
		}
	}
	return memories[f]
}

func (f *fake) GetWorkspace(_ context.Context, id, userID int64) (model.Workspace, error) {
	if err := f.err("GetWorkspace"); err != nil {
		return model.Workspace{}, err
	}
	if id != 200 || userID != 2 {
		return model.Workspace{}, base.ErrNotFound
	}
	return model.Workspace{ID: id, UserID: userID}, nil
}

func (f *fake) ListTeamPeerWorkspaces(context.Context, int64) ([]base.TeamWorkspacePeer, error) {
	return append([]base.TeamWorkspacePeer(nil), f.mem().peers...), f.err("ListTeamPeerWorkspaces")
}

func (f *fake) ListMemoryExclusions(_ context.Context, userID, workspaceID int64) ([]int64, error) {
	ids := []int64{}
	for k := range f.mem().excluded {
		if k[0] == workspaceID && k[1] == userID {
			ids = append(ids, k[2])
		}
	}
	return ids, f.err("ListMemoryExclusions")
}

func (f *fake) ExcludeTeammateMemory(_ context.Context, e model.MemoryExclusion) error {
	if err := f.err("ExcludeTeammateMemory"); err != nil {
		return err
	}
	f.mem().excluded[[3]int64{e.WorkspaceID, e.UserID, e.ExcludedUserID}] = true
	return nil
}

func (f *fake) IncludeTeammateMemory(_ context.Context, userID, workspaceID, teammateID int64) error {
	if err := f.err("IncludeTeammateMemory"); err != nil {
		return err
	}
	delete(f.mem().excluded, [3]int64{workspaceID, userID, teammateID})
	return nil
}

func (f *fake) MemoryUpdates(_ context.Context, ids []int64) (map[int64]time.Time, error) {
	out := map[int64]time.Time{}
	for _, id := range ids {
		if at, ok := f.mem().updates[id]; ok {
			out[id] = at
		}
	}
	return out, f.err("MemoryUpdates")
}

func userIDs(ms []entity.TeammateMemory) (ids []int64, included []bool) {
	for _, m := range ms {
		ids = append(ids, m.UserID)
		included = append(included, m.Included)
	}
	return ids, included
}

// The teammates are the team workspace's other members, one per workspace,
// all included by default, and the caller is never one of them.
func TestListTeammateMemories(t *testing.T) {
	_, c := newFake()
	ms, err := c.ListTeammateMemories(context.Background(), 2, 200)
	if err != nil {
		t.Fatal(err)
	}
	ids, included := userIDs(ms)
	if !reflect.DeepEqual(ids, []int64{1, 3, 1}) || !reflect.DeepEqual(included, []bool{true, true, true}) {
		t.Fatalf("teammates %v included %v, want alice, carol, alice's second, all included", ids, included)
	}
	want := entity.TeammateMemory{UserID: 1, Name: "Alice", Picture: "a.png", WorkspaceID: 100, UpdatedAt: &memoryAt, Included: true}
	if !reflect.DeepEqual(ms[0], want) {
		t.Fatalf("alice = %+v, want %+v", ms[0], want)
	}
	if ms[1].UpdatedAt != nil {
		t.Fatalf("carol has written no memory, got updatedAt %v", ms[1].UpdatedAt)
	}
}

// Someone else's workspace, or a missing one, is not found.
func TestTeammateMemoriesOfAnotherWorkspace(t *testing.T) {
	_, c := newFake()
	ctx := context.Background()
	if _, err := c.ListTeammateMemories(ctx, 3, 200); !errors.Is(err, base.ErrNotFound) {
		t.Fatalf("list someone else's workspace: %v, want not found", err)
	}
	if _, err := c.SetTeammateMemory(ctx, 3, 200, 1, false); !errors.Is(err, base.ErrNotFound) {
		t.Fatalf("set on someone else's workspace: %v, want not found", err)
	}
}

// A toggle answers only for a teammate in the team workspace: dan, in
// another one, the caller and a stranger are not found, and nothing is
// written.
func TestSetTeammateMemoryOnlyForTeammates(t *testing.T) {
	f, c := newFake()
	for _, who := range []int64{4, 2, 99} {
		if _, err := c.SetTeammateMemory(context.Background(), 2, 200, who, false); !errors.Is(err, base.ErrNotFound) {
			t.Fatalf("set user %d: %v, want not found", who, err)
		}
	}
	if len(f.mem().excluded) != 0 || len(f.published) != 0 {
		t.Fatalf("a refused toggle wrote %v and published %v", f.mem().excluded, f.published)
	}
}

// Leaving a teammate out and back in writes the caller's own row, answers
// the row as now set, and publishes the action on the caller's workspace.
func TestSetTeammateMemory(t *testing.T) {
	f, c := newFake()
	ctx := context.Background()
	m, err := c.SetTeammateMemory(ctx, 2, 200, 1, false)
	if err != nil || m.Included || m.UserID != 1 || m.WorkspaceID != 100 {
		t.Fatalf("exclude alice = %+v, %v", m, err)
	}
	if !f.mem().excluded[[3]int64{200, 2, 1}] {
		t.Fatalf("exclusions %v, want bob's of alice in 200", f.mem().excluded)
	}
	ms, _ := c.ListTeammateMemories(ctx, 2, 200)
	if _, included := userIDs(ms); !reflect.DeepEqual(included, []bool{false, true, false}) {
		t.Fatalf("included after excluding alice = %v", included)
	}
	if m, err = c.SetTeammateMemory(ctx, 2, 200, 1, true); err != nil || !m.Included {
		t.Fatalf("include alice = %+v, %v", m, err)
	}
	if len(f.mem().excluded) != 0 {
		t.Fatalf("exclusions after including alice again: %v", f.mem().excluded)
	}
	got := []entity.Action{}
	for _, e := range f.published {
		if e.WorkspaceID != 200 || e.UserID != 2 || e.ResourceID != 1 {
			t.Fatalf("published %+v, want bob's workspace 200 about alice", e)
		}
		got = append(got, e.Action)
	}
	if want := []entity.Action{entity.ActionTeammateMemoryExclude, entity.ActionTeammateMemoryInclude}; !reflect.DeepEqual(got, want) {
		t.Fatalf("published %v, want %v", got, want)
	}
}

// MemoryPeers drops the teammates the owner left out, never the owner's own
// workspaces, and does not read exclusions for a workspace alone.
func TestMemoryPeers(t *testing.T) {
	f, _ := newFake()
	ctx := context.Background()
	f.mem().excluded[[3]int64{200, 2, 1}] = true
	f.mem().excluded[[3]int64{200, 2, 2}] = true // the API never writes one; ignored
	f.mem().excluded[[3]int64{200, 9, 3}] = true // someone else's choice
	peers, err := MemoryPeers(ctx, f, 2, 200)
	if err != nil {
		t.Fatal(err)
	}
	got := []int64{}
	for _, p := range peers {
		got = append(got, p.WorkspaceID)
	}
	if !reflect.DeepEqual(got, []int64{200, 300}) {
		t.Fatalf("peers %v, want bob's own and carol's", got)
	}

	f.mem().peers = f.mem().peers[:1]
	f.fail = "ListMemoryExclusions"
	if peers, err = MemoryPeers(ctx, f, 1, 100); err != nil || len(peers) != 1 {
		t.Fatalf("a workspace alone = %v, %v; want itself, no exclusions read", peers, err)
	}
}

// Every failure is passed on, as it is.
func TestTeammateMemoryFailures(t *testing.T) {
	ctx := context.Background()
	list := func(c Controller) error { _, err := c.ListTeammateMemories(ctx, 2, 200); return err }
	exclude := func(c Controller) error { _, err := c.SetTeammateMemory(ctx, 2, 200, 1, false); return err }
	include := func(c Controller) error { _, err := c.SetTeammateMemory(ctx, 2, 200, 1, true); return err }
	for _, tc := range []struct {
		fail string
		call func(c Controller) error
	}{
		{"GetWorkspace", list},
		{"ListTeamPeerWorkspaces", list},
		{"ListMemoryExclusions", list},
		{"MemoryUpdates", list},
		{"GetWorkspace", exclude},
		{"ExcludeTeammateMemory", exclude},
		{"IncludeTeammateMemory", include},
	} {
		f, c := newFake()
		f.fail = tc.fail
		if err := tc.call(c); !errors.Is(err, errBoom) {
			t.Errorf("%s: %v, want %v", tc.fail, err, errBoom)
		}
		if len(f.published) != 0 {
			t.Errorf("%s: published %v after a failure", tc.fail, f.published)
		}
	}
	f, _ := newFake()
	f.fail = "ListMemoryExclusions"
	if _, err := MemoryPeers(ctx, f, 2, 200); !errors.Is(err, errBoom) {
		t.Errorf("MemoryPeers: %v, want %v", err, errBoom)
	}
	f.fail = "ListTeamPeerWorkspaces"
	if _, err := TeamPeers(ctx, f, 200); !errors.Is(err, errBoom) {
		t.Errorf("TeamPeers: %v, want %v", err, errBoom)
	}
}
