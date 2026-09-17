// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package crud

import (
	"errors"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/mustafaturan/monoflake"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
)

// A machine runs agents for several workspaces at once and most of them are
// the same kind, so a list of sessions that carries only the kind cannot be
// read: which of these is the migration, which one can I stop.
func TestListSessionsNamesTheWorkspaces(t *testing.T) {
	env := newTestController(t)
	uid := monoflake.IDFromBase62(testUserBase62).Int64()

	env.repo.EXPECT().ListSessionsByMachine(gomock.Any(), int64(3), uid).
		Return([]model.Session{
			{ID: 9, MachineID: 3, WorkspaceID: 7, Kind: "claude-code", Status: "running"},
			{ID: 10, MachineID: 3, WorkspaceID: 8, Kind: "claude-code", Status: "running"},
		}, nil)
	env.repo.EXPECT().WorkspaceNamesByID(gomock.Any(), gomock.Any(), uid).
		DoAndReturn(func(_ any, ids []int64, _ int64) (map[int64]string, error) {
			if len(ids) != 2 {
				t.Errorf("asked for %d workspaces, want one query for both", len(ids))
			}
			return map[int64]string{7: "Q3 migration", 8: "Terminal probe"}, nil
		})

	rs, err := env.controller.ListSessions(t.Context(), entity.ListSessionsRequest{
		UserID: testUserBase62, MachineID: monoflake.ID(3).String(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if rs.Sessions[0].WorkspaceName != "Q3 migration" || rs.Sessions[1].WorkspaceName != "Terminal probe" {
		t.Errorf("names = %q, %q", rs.Sessions[0].WorkspaceName, rs.Sessions[1].WorkspaceName)
	}
}

// One query for the set, not one per row: a page with ten sessions must not be
// eleven queries.
func TestTheWorkspacesAreLookedUpOnce(t *testing.T) {
	env := newTestController(t)
	uid := monoflake.IDFromBase62(testUserBase62).Int64()

	env.repo.EXPECT().ListSessionsByMachine(gomock.Any(), int64(3), uid).
		Return([]model.Session{
			{ID: 9, MachineID: 3, WorkspaceID: 7, Kind: "claude-code"},
			{ID: 10, MachineID: 3, WorkspaceID: 7, Kind: "acp-gateway"},
		}, nil)
	env.repo.EXPECT().WorkspaceNamesByID(gomock.Any(), []int64{7}, uid).
		Return(map[int64]string{7: "Q3 migration"}, nil)

	rs, err := env.controller.ListSessions(t.Context(), entity.ListSessionsRequest{
		UserID: testUserBase62, MachineID: monoflake.ID(3).String(),
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range rs.Sessions {
		if s.WorkspaceName != "Q3 migration" {
			t.Errorf("session %s is unnamed", s.ID)
		}
	}
}

// The sessions are the answer to the question that was asked. A workspace
// lookup that fails must not turn "what is running here" into an error page.
func TestAFailedWorkspaceLookupStillListsTheSessions(t *testing.T) {
	env := newTestController(t)
	uid := monoflake.IDFromBase62(testUserBase62).Int64()

	env.repo.EXPECT().ListSessionsByMachine(gomock.Any(), int64(3), uid).
		Return([]model.Session{{ID: 9, MachineID: 3, WorkspaceID: 7, Kind: "claude-code"}}, nil)
	env.repo.EXPECT().WorkspaceNamesByID(gomock.Any(), gomock.Any(), uid).
		Return(nil, errors.New("database is down"))

	rs, err := env.controller.ListSessions(t.Context(), entity.ListSessionsRequest{
		UserID: testUserBase62, MachineID: monoflake.ID(3).String(),
	})
	if err != nil {
		t.Fatalf("a failed name lookup lost the sessions: %v", err)
	}
	if len(rs.Sessions) != 1 || rs.Sessions[0].WorkspaceName != "" {
		t.Errorf("sessions = %+v", rs.Sessions)
	}
}

// A session with no workspace — one recorded before the field, or one whose
// workspace has gone — asks for nothing and is left unnamed.
func TestASessionWithNoWorkspaceAsksForNothing(t *testing.T) {
	env := newTestController(t)
	uid := monoflake.IDFromBase62(testUserBase62).Int64()

	env.repo.EXPECT().ListSessionsByMachine(gomock.Any(), int64(3), uid).
		Return([]model.Session{{ID: 9, MachineID: 3, Kind: "claude-code"}}, nil)

	rs, err := env.controller.ListSessions(t.Context(), entity.ListSessionsRequest{
		UserID: testUserBase62, MachineID: monoflake.ID(3).String(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if rs.Sessions[0].WorkspaceName != "" {
		t.Errorf("name = %q, want none", rs.Sessions[0].WorkspaceName)
	}
}

// The terminal page reads one session, and needs the same answer.
func TestGetSessionNamesTheWorkspace(t *testing.T) {
	env := newTestController(t)
	uid := monoflake.IDFromBase62(testUserBase62).Int64()

	env.repo.EXPECT().GetSession(gomock.Any(), int64(9), uid).
		Return(model.Session{ID: 9, MachineID: 3, WorkspaceID: 7, Kind: "claude-code"}, nil)
	env.repo.EXPECT().WorkspaceNamesByID(gomock.Any(), []int64{7}, uid).
		Return(map[int64]string{7: "Q3 migration"}, nil)

	rs, err := env.controller.GetSession(t.Context(), entity.GetSessionRequest{
		UserID: testUserBase62, SessionID: monoflake.ID(9).String(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if rs.Session.WorkspaceName != "Q3 migration" {
		t.Errorf("name = %q", rs.Session.WorkspaceName)
	}
}
