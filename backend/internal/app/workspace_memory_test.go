// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/golang/mock/gomock"
	"github.com/mustafaturan/monoflake"
	"gorm.io/gorm"

	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	mock_repository "github.com/agentrq/agentrq/backend/internal/service/mocks/repository"
)

type sqliteConn struct{ db *gorm.DB }

func (c sqliteConn) Conn(context.Context) *gorm.DB { return c.db }
func (c sqliteConn) Close(context.Context)         {}

type countingIDs struct{ n atomic.Int64 }

func (c *countingIDs) NextID() int64        { return 5000 + c.n.Add(1) }
func (c *countingIDs) NextIDString() string { return monoflake.ID(c.NextID()).String() }

const (
	memUser   = int64(482467435371298817)
	memParent = int64(10)
	memFork   = int64(20)
)

func TestMCPWorkspaceResolvesAForkToItsParent(t *testing.T) {
	ctrl := gomock.NewController(t)
	repo := mock_repository.NewMockRepository(ctrl)
	owner := monoflake.ID(memUser).String()

	repo.EXPECT().SystemGetWorkspace(gomock.Any(), memFork).Return(model.Workspace{ID: memFork, UserID: memUser, ForkOfID: memParent}, nil)
	w, gotOwner, content := mcpWorkspace(context.Background(), repo, memFork, "someone")
	if w.ID != memFork || gotOwner != owner || content != memParent {
		t.Errorf("a fork: %d, %s, %d; want %d, %s, %d", w.ID, gotOwner, content, memFork, owner, memParent)
	}

	repo.EXPECT().SystemGetWorkspace(gomock.Any(), memParent).Return(model.Workspace{ID: memParent, UserID: memUser}, nil)
	if _, _, content := mcpWorkspace(context.Background(), repo, memParent, "someone"); content != memParent {
		t.Errorf("a workspace: %d, want its own", content)
	}

	repo.EXPECT().SystemGetWorkspace(gomock.Any(), memFork).Return(model.Workspace{}, errRepo)
	if _, gotOwner, content := mcpWorkspace(context.Background(), repo, memFork, "someone"); gotOwner != "someone" || content != memFork {
		t.Errorf("unreadable: %s, %d; want the caller and itself", gotOwner, content)
	}
}

// A fork's agent reads its parent's memory.md, and what it saves is the
// parent's: a fork writes its learnings into the parent's memory.
func TestAForksMemoryIsItsParents(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Workspace{}, &model.Memory{}); err != nil {
		t.Fatal(err)
	}
	for _, w := range []model.Workspace{
		{ID: memParent, UserID: memUser, Name: "api"},
		{ID: memFork, UserID: memUser, Name: "api fork", ForkOfID: memParent},
	} {
		if err := db.Create(&w).Error; err != nil {
			t.Fatal(err)
		}
	}
	repo := base.New(sqliteConn{db: db})
	ids := &countingIDs{}
	ctx := context.Background()
	memoryOf := func(ws int64) workspaceMemory {
		_, owner, content := mcpWorkspace(ctx, repo, ws, "")
		return workspaceMemory{repo: repo, ids: ids, workspaceID: content, userID: owner}
	}
	parent, fork := memoryOf(memParent), memoryOf(memFork)

	if err := parent.save(ctx, "MEMORY.md", "# Index"); err != nil {
		t.Fatal(err)
	}
	if got, found, err := fork.load(ctx, "MEMORY.md"); err != nil || !found || got != "# Index" {
		t.Fatalf("the fork's loadMemory: %q, %v, %v; want the parent's", got, found, err)
	}

	if err := fork.save(ctx, "deploys.md", "how we ship"); err != nil {
		t.Fatal(err)
	}
	if got, found, _ := parent.load(ctx, "deploys.md"); !found || got != "how we ship" {
		t.Fatalf("the parent reads what the fork saved: %q, %v", got, found)
	}

	if deleted, err := fork.delete(ctx, "deploys.md"); err != nil || !deleted {
		t.Fatalf("the fork's deleteMemory: %v, %v", deleted, err)
	}
	if _, found, _ := parent.load(ctx, "deploys.md"); found {
		t.Fatal("deleted from the fork, the parent still has it")
	}
	if deleted, err := fork.delete(ctx, "deploys.md"); err != nil || deleted {
		t.Fatalf("deleting it again: %v, %v; want nothing to delete", deleted, err)
	}
	if _, found, err := fork.load(ctx, "never.md"); err != nil || found {
		t.Fatalf("a memory never written: %v, %v", found, err)
	}
}

func TestWorkspaceMemoryReportsStorageFailures(t *testing.T) {
	ctrl := gomock.NewController(t)
	repo := mock_repository.NewMockRepository(ctrl)
	m := workspaceMemory{repo: repo, ids: &countingIDs{}, workspaceID: memParent, userID: monoflake.ID(memUser).String()}
	ctx := context.Background()

	repo.EXPECT().GetMemory(gomock.Any(), memUser, memParent, "MEMORY.md").Return(model.Memory{}, errRepo)
	if _, _, err := m.load(ctx, "MEMORY.md"); err != errRepo {
		t.Errorf("load: %v", err)
	}
	repo.EXPECT().DeleteMemory(gomock.Any(), memUser, memParent, "MEMORY.md").Return(errRepo)
	if _, err := m.delete(ctx, "MEMORY.md"); err != errRepo {
		t.Errorf("delete: %v", err)
	}
}

// In a workspace shared with teams, memory.md is the index of its team
// workspace: every shared workspace of its name, in every team it is shared
// with, once, by owner, the agent's own marked. A member's memory reads by
// its workspace's id, only while a team lets the owner read it, and never
// writes; the agent's own memory still reads and writes, plainly or by its
// own id. A workspace in no team keeps its own memory.md.
func TestATeamWorkspacesMemoryIsAnIndexOfItsMembers(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.Workspace{}, &model.Memory{}, &model.Team{},
		&model.TeamMember{}, &model.WorkspaceShare{}); err != nil {
		t.Fatal(err)
	}
	const (
		alice, carol, dave, erin     = int64(1001), int64(1003), int64(1004), int64(1005)
		aliceAPI, carolWeb, carolOld = int64(31), int64(32), int64(33)
		daveAPI, erinAPI             = int64(34), int64(35)
		team1, team2                 = int64(71), int64(72)
	)
	archived := time.Now()
	rows := []any{
		&model.User{ID: memUser, Name: "Bob", Email: "bob@x"}, &model.User{ID: alice, Name: "alice", Email: "alice@x"},
		&model.User{ID: carol, Name: "Carol", Email: "carol@x"}, &model.User{ID: dave, Name: "Dave", Email: "dave@x"},
		&model.User{ID: erin, Name: "", Email: "erin@x"},
		&model.Workspace{ID: memParent, UserID: memUser, Name: "api"},
		&model.Workspace{ID: memFork, UserID: memUser, Name: "api fork", ForkOfID: memParent},
		&model.Workspace{ID: aliceAPI, UserID: alice, Name: "API"},
		&model.Workspace{ID: carolWeb, UserID: carol, Name: "web"},
		&model.Workspace{ID: carolOld, UserID: carol, Name: "api", ArchivedAt: &archived},
		&model.Workspace{ID: daveAPI, UserID: dave, Name: "api"},
		&model.Workspace{ID: erinAPI, UserID: erin, Name: " Api "},
		&model.Team{ID: team1, Name: "one", Version: 1}, &model.Team{ID: team2, Name: "two", Version: 1},
	}
	for i, m := range [][2]int64{{team1, memUser}, {team1, alice}, {team1, carol}, {team2, memUser}, {team2, erin}} {
		rows = append(rows, &model.TeamMember{ID: int64(100 + i), TeamID: m[0], UserID: m[1]})
	}
	for i, s := range [][3]int64{{team1, memParent, memUser}, {team1, aliceAPI, alice}, {team1, carolWeb, carol},
		{team1, carolOld, carol}, {team2, memParent, memUser}, {team2, erinAPI, erin}} {
		rows = append(rows, &model.WorkspaceShare{ID: int64(200 + i), TeamID: s[0], WorkspaceID: s[1], UserID: s[2]})
	}
	for i, m := range [][3]any{{memUser, memParent, "mine"}, {alice, aliceAPI, "alice's"}, {carol, carolWeb, "web's"}, {dave, daveAPI, "dave's"}} {
		rows = append(rows, &model.Memory{ID: int64(300 + i), UserID: m[0].(int64), WorkspaceID: m[1].(int64), Name: "memory.md", Content: m[2].(string)})
	}
	for _, r := range rows {
		if err := db.Create(r).Error; err != nil {
			t.Fatal(err)
		}
	}
	repo := base.New(sqliteConn{db: db})
	ctx := context.Background()
	memoryOf := func(ws int64) workspaceMemory {
		_, owner, content := mcpWorkspace(ctx, repo, ws, "")
		return workspaceMemory{repo: repo, ids: &countingIDs{}, workspaceID: content, userID: owner}
	}
	id := func(ws int64) string { return monoflake.ID(ws).String() }
	bob, fork := memoryOf(memParent), memoryOf(memFork)

	want := teamIndexHeader + "\n" +
		"A member: " + id(erinAPI) + "/memory.md\n" +
		"alice: " + id(aliceAPI) + "/memory.md\n" +
		"Bob: " + id(memParent) + "/memory.md (your actual memory that you can update)\n"
	for _, m := range []workspaceMemory{bob, fork} {
		if got, found, err := m.load(ctx, "memory.md"); err != nil || !found || got != want {
			t.Fatalf("the index: %v %v\n%s\nwant\n%s", found, err, got, want)
		}
	}
	if got, found, err := bob.load(ctx, id(aliceAPI)+"/memory.md"); err != nil || !found || got != "alice's" {
		t.Errorf("a member's memory: %q %v %v", got, found, err)
	}
	if _, found, err := bob.load(ctx, id(erinAPI)+"/memory.md"); err != nil || found {
		t.Errorf("a member's memory never written: %v %v", found, err)
	}
	if got, found, err := bob.load(ctx, id(memParent)+"/memory.md"); err != nil || !found || got != "mine" {
		t.Errorf("the agent's own memory by its id: %q %v %v", got, found, err)
	}
	for _, other := range []int64{carolWeb, carolOld, daveAPI, 999} {
		if _, _, err := bob.load(ctx, id(other)+"/memory.md"); err != errNotTeammate {
			t.Errorf("workspace %d is no member's of the team workspace: %v", other, err)
		}
	}
	if err := bob.save(ctx, id(aliceAPI)+"/memory.md", "x"); err != errTeammateMemoryReadOnly {
		t.Errorf("save to a member's: %v", err)
	}
	if _, err := bob.delete(ctx, id(aliceAPI)+"/memory.md"); err != errTeammateMemoryReadOnly {
		t.Errorf("delete a member's: %v", err)
	}
	if err := fork.save(ctx, id(memParent)+"/notes.md", "n"); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := bob.load(ctx, "notes.md"); got != "n" {
		t.Errorf("saved by its own id, read plainly: %q", got)
	}
	if deleted, err := bob.delete(ctx, id(memParent)+"/notes.md"); err != nil || !deleted {
		t.Errorf("delete by its own id: %v %v", deleted, err)
	}

	// Out of team two, Bob no longer reads Erin's memory, though her share
	// is still listed there.
	if err := db.Where("team_id = ? AND user_id = ?", team2, memUser).Delete(&model.TeamMember{}).Error; err != nil {
		t.Fatal(err)
	}
	if _, _, err := bob.load(ctx, id(erinAPI)+"/memory.md"); err != errNotTeammate {
		t.Errorf("a team that no longer lets the owner read it: %v", err)
	}

	// Shared nowhere, memory.md is the workspace's own.
	if got, found, err := memoryOf(daveAPI).load(ctx, "memory.md"); err != nil || !found || got != "dave's" {
		t.Errorf("a workspace in no team: %q %v %v", got, found, err)
	}
}

func TestATeamWorkspacesMemoryReportsStorageFailures(t *testing.T) {
	ctrl := gomock.NewController(t)
	repo := mock_repository.NewMockRepository(ctrl)
	m := workspaceMemory{repo: repo, ids: &countingIDs{}, workspaceID: memParent, userID: monoflake.ID(memUser).String()}
	ctx := context.Background()
	other := monoflake.ID(31).String() + "/memory.md"

	repo.EXPECT().ListTeamPeerWorkspaces(gomock.Any(), memParent).Return(nil, errRepo)
	if _, _, err := m.load(ctx, "memory.md"); err != errRepo {
		t.Errorf("the index: %v", err)
	}
	repo.EXPECT().ListTeamPeerWorkspaces(gomock.Any(), memParent).Return(nil, errRepo)
	if _, _, err := m.load(ctx, other); err != errRepo {
		t.Errorf("a member's, listing: %v", err)
	}
	repo.EXPECT().ListTeamPeerWorkspaces(gomock.Any(), memParent).Return([]base.TeamWorkspacePeer{
		{TeamID: 7, WorkspaceID: memParent, Name: "api"}, {TeamID: 7, WorkspaceID: 31, Name: "api"},
	}, nil)
	repo.EXPECT().CanReadWorkspaceInTeam(gomock.Any(), memUser, int64(7), int64(31)).Return(false, errRepo)
	if _, _, err := m.load(ctx, other); err != errRepo {
		t.Errorf("a member's, the check: %v", err)
	}
}

// One owner with two shared workspaces of the name is two lines, by
// workspace.
func TestATeamMemoryIndexOrdersOneOwnersWorkspaces(t *testing.T) {
	m := workspaceMemory{workspaceID: memParent}
	got := m.teamIndex([]base.TeamWorkspacePeer{{WorkspaceID: 41, OwnerName: "Ann"}, {WorkspaceID: 40, OwnerName: "ann"}})
	want := teamIndexHeader + "\nann: " + monoflake.ID(40).String() + "/memory.md\nAnn: " + monoflake.ID(41).String() + "/memory.md\n"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}
