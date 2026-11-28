// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package crud

import (
	"errors"
	"strings"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/mustafaturan/monoflake"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
)

// The half of "does this workspace already have an agent?" that the database
// answers. A session that has been started but has not connected yet is
// invisible to the live-connection check, and that window is exactly long
// enough for somebody to press the button twice.
func TestActiveSessionForWorkspace(t *testing.T) {
	env := newTestController(t)
	uid := monoflake.IDFromBase62(testUserBase62).Int64()
	env.repo.EXPECT().ActiveSessionForWorkspace(gomock.Any(), int64(7), uid).
		Return(model.Session{ID: 9, WorkspaceID: 7, Status: "starting"}, nil)

	got, err := env.controller.ActiveSessionForWorkspace(t.Context(), entity.ActiveSessionRequest{
		UserID: testUserBase62, WorkspaceID: monoflake.ID(7).String(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Status != "starting" {
		t.Fatalf("active = %+v", got)
	}
}

// No agent is the ordinary answer, and not an error: every first launch for a
// workspace passes through here.
func TestNoActiveSessionIsNotAnError(t *testing.T) {
	env := newTestController(t)
	uid := monoflake.IDFromBase62(testUserBase62).Int64()
	env.repo.EXPECT().ActiveSessionForWorkspace(gomock.Any(), int64(7), uid).
		Return(model.Session{}, base.ErrNotFound)

	got, err := env.controller.ActiveSessionForWorkspace(t.Context(), entity.ActiveSessionRequest{
		UserID: testUserBase62, WorkspaceID: monoflake.ID(7).String(),
	})
	if err != nil {
		t.Fatalf("a workspace with no agent reported an error: %v", err)
	}
	if got != nil {
		t.Errorf("active = %+v, want nothing", got)
	}
}

// A database that is down must not read as "no agent here", which would let a
// second one start.
func TestARealFailureIsNotMistakenForNoAgent(t *testing.T) {
	env := newTestController(t)
	uid := monoflake.IDFromBase62(testUserBase62).Int64()
	env.repo.EXPECT().ActiveSessionForWorkspace(gomock.Any(), int64(7), uid).
		Return(model.Session{}, errors.New("database is down"))

	if _, err := env.controller.ActiveSessionForWorkspace(t.Context(), entity.ActiveSessionRequest{
		UserID: testUserBase62, WorkspaceID: monoflake.ID(7).String(),
	}); err == nil {
		t.Error("a failed query was reported as no agent")
	}
}

func TestActiveSessionRefusesAnUnusableID(t *testing.T) {
	env := newTestController(t)
	if _, err := env.controller.ActiveSessionForWorkspace(t.Context(), entity.ActiveSessionRequest{}); err == nil {
		t.Error("an empty request was accepted")
	}
}

// A finished session is written and then removed. Written first so nothing is
// lost between the two: the update is what the event stream carries, and it is
// that event — not the row — which tells somebody their agent failed and why.
func TestAFinishedSessionIsRemoved(t *testing.T) {
	for _, status := range []string{"exited", "killed", "failed"} {
		t.Run(status, func(t *testing.T) {
			env := newTestController(t)
			env.repo.EXPECT().
				UpdateSessionState(gomock.Any(), int64(9), int64(7), status, gomock.Any(), gomock.Any(), gomock.Any()).
				Return(nil)
			env.repo.EXPECT().DeleteFinishedSession(gomock.Any(), int64(9)).Return(nil)

			if err := env.controller.UpdateSessionState(t.Context(), entity.UpdateSessionStateRequest{
				SessionID: monoflake.ID(9).String(), Status: status,
				MachineID: 7,
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// A session that is still going is never removed, whatever else happens.
func TestALiveSessionIsKept(t *testing.T) {
	for _, status := range []string{"starting", "running"} {
		t.Run(status, func(t *testing.T) {
			env := newTestController(t)
			env.repo.EXPECT().
				UpdateSessionState(gomock.Any(), int64(9), int64(7), status, gomock.Any(), gomock.Any(), gomock.Any()).
				Return(nil)
			// No DeleteFinishedSession expectation: gomock fails the test if
			// one is called, which is the assertion.

			if err := env.controller.UpdateSessionState(t.Context(), entity.UpdateSessionStateRequest{
				SessionID: monoflake.ID(9).String(), Status: status,
				MachineID: 7,
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// A row that outlives its session is untidy; failing the state report over it
// would lose the event that actually matters to somebody.
func TestAFailedCleanupDoesNotFailTheReport(t *testing.T) {
	env := newTestController(t)
	env.repo.EXPECT().
		UpdateSessionState(gomock.Any(), int64(9), int64(7), "exited", gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil)
	env.repo.EXPECT().DeleteFinishedSession(gomock.Any(), int64(9)).
		Return(errors.New("database is busy"))

	if err := env.controller.UpdateSessionState(t.Context(), entity.UpdateSessionStateRequest{
		SessionID: monoflake.ID(9).String(), Status: "exited",
		MachineID: 7,
	}); err != nil {
		t.Errorf("a failed cleanup failed the state report: %v", err)
	}
}

// The write comes first, so the event the stream carries is the finished state
// rather than whatever was there before.
func TestTheStateIsWrittenBeforeTheRowGoes(t *testing.T) {
	env := newTestController(t)
	written := false
	env.repo.EXPECT().
		UpdateSessionState(gomock.Any(), int64(9), int64(7), "exited", gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ any, _, _ int64, _ string, _ *int, _ any, _ bool) error {
			written = true
			return nil
		})
	env.repo.EXPECT().DeleteFinishedSession(gomock.Any(), int64(9)).
		DoAndReturn(func(_ any, _ int64) error {
			if !written {
				t.Error("the row was removed before its final state was recorded")
			}
			return nil
		})

	if err := env.controller.UpdateSessionState(t.Context(), entity.UpdateSessionStateRequest{
		SessionID: monoflake.ID(9).String(), Status: "exited",
		MachineID: 7,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestForkFolderMachines(t *testing.T) {
	env := newTestController(t)
	uid := monoflake.IDFromBase62(testUserBase62).Int64()
	env.repo.EXPECT().ForkFolderMachines(gomock.Any(), int64(7), uid).Return([]int64{3, 4}, nil)

	got, err := env.controller.ForkFolderMachines(t.Context(), entity.ActiveSessionRequest{
		UserID: testUserBase62, WorkspaceID: monoflake.ID(7).String(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != monoflake.ID(3).String() || got[1] != monoflake.ID(4).String() {
		t.Errorf("machines = %v", got)
	}
}

// No machine on record is an answer only for a fork that never made a folder.
// One that has a folder ran before machines were recorded, and answering none
// would let the merge report the folder deleted when nothing deleted it.
func TestForkFolderMachinesNoneRecorded(t *testing.T) {
	uid := monoflake.IDFromBase62(testUserBase62).Int64()
	req := entity.ActiveSessionRequest{UserID: testUserBase62, WorkspaceID: monoflake.ID(7).String()}

	env := newTestController(t)
	env.repo.EXPECT().ForkFolderMachines(gomock.Any(), int64(7), uid).Return(nil, nil)
	env.repo.EXPECT().GetWorkspace(gomock.Any(), int64(7), uid).Return(model.Workspace{ID: 7}, nil)
	if got, err := env.controller.ForkFolderMachines(t.Context(), req); err != nil || len(got) != 0 {
		t.Errorf("a fork with no folder: %v, %v", got, err)
	}

	env = newTestController(t)
	env.repo.EXPECT().ForkFolderMachines(gomock.Any(), int64(7), uid).Return(nil, nil)
	env.repo.EXPECT().GetWorkspace(gomock.Any(), int64(7), uid).Return(model.Workspace{ID: 7, WorkingDirectory: "/home/u/.agentrq/forks/7"}, nil)
	_, err := env.controller.ForkFolderMachines(t.Context(), req)
	var fe *entity.ForkError
	if !errors.As(err, &fe) || !errors.Is(err, entity.ErrForkAgentRunning) ||
		!strings.Contains(err.Error(), "no record of which machine") || !strings.Contains(err.Error(), "/home/u/.agentrq/forks/7") {
		t.Errorf("a fork with a folder nobody recorded: %v", err)
	}

	env = newTestController(t)
	env.repo.EXPECT().ForkFolderMachines(gomock.Any(), int64(7), uid).Return(nil, nil)
	env.repo.EXPECT().GetWorkspace(gomock.Any(), int64(7), uid).Return(model.Workspace{}, errors.New("database is down"))
	if _, err := env.controller.ForkFolderMachines(t.Context(), req); err == nil {
		t.Error("a failed workspace read was swallowed")
	}
}

func TestForkFolderMachinesFailures(t *testing.T) {
	env := newTestController(t)
	if _, err := env.controller.ForkFolderMachines(t.Context(), entity.ActiveSessionRequest{}); err == nil {
		t.Error("an empty request was accepted")
	}
	uid := monoflake.IDFromBase62(testUserBase62).Int64()
	env.repo.EXPECT().ForkFolderMachines(gomock.Any(), int64(7), uid).Return(nil, errors.New("database is down"))
	if _, err := env.controller.ForkFolderMachines(t.Context(), entity.ActiveSessionRequest{
		UserID: testUserBase62, WorkspaceID: monoflake.ID(7).String(),
	}); err == nil {
		t.Error("a failed query was swallowed")
	}
}
