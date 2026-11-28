// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package crud

import (
	"errors"
	"reflect"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/mustafaturan/monoflake"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
)

// A woken machine gets back only what the server marked away and would still
// launch, and the rest of its away sessions are stopped. An id with no row on
// this machine is left alone — agentrqd names every profile's sessions on each
// profile's socket — and a check that cannot run decides nothing.
func TestReconcileSessionsDecidesWhatAWokenMachineKeeps(t *testing.T) {
	env := newMachineTelemetryEnv(t)
	const machineID, owner = int64(11), int64(77)
	broken := errors.New("database unavailable")
	away := func(id, workspace int64) model.Session {
		return model.Session{ID: id, MachineID: machineID, UserID: owner, WorkspaceID: workspace, Status: "away", Kind: "claude-code"}
	}
	named := []int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}

	env.repo.EXPECT().ReconcileSessions(gomock.Any(), machineID, named, gomock.Any()).Return(nil)
	env.repo.EXPECT().SessionsOnMachine(gomock.Any(), machineID, named).Return([]model.Session{
		{ID: 1, MachineID: machineID, UserID: owner, WorkspaceID: 101, Status: "running"},
		away(2, 102),
		// 3 has no row on this machine.
		away(4, 0),
		away(5, 105),
		away(6, 106),
		away(7, 107),
		away(8, 108),
		away(9, 109),
		away(10, 110),
	}, nil)
	for ws, err := range map[int64]error{102: nil, 105: base.ErrNotFound, 106: nil, 107: broken, 108: nil, 109: nil, 110: nil} {
		env.repo.EXPECT().GetWorkspace(gomock.Any(), ws, owner).Return(model.Workspace{ID: ws}, err)
	}
	for ws, err := range map[int64]error{102: base.ErrNotFound, 106: nil, 108: broken, 109: base.ErrNotFound, 110: base.ErrNotFound} {
		env.repo.EXPECT().ActiveSessionForWorkspace(gomock.Any(), ws, owner).Return(model.Session{ID: 99}, err)
	}
	env.repo.EXPECT().ResumeSession(gomock.Any(), int64(2), machineID).Return(true, nil)
	env.repo.EXPECT().ResumeSession(gomock.Any(), int64(9), machineID).Return(false, nil)
	env.repo.EXPECT().ResumeSession(gomock.Any(), int64(10), machineID).Return(false, broken)

	res, err := env.controller.ReconcileSessions(t.Context(), entity.ReconcileSessionsRequest{
		MachineID: machineID, Running: named,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Resumed) != 1 || res.Resumed[0].ID != monoflake.ID(2).String() || res.Resumed[0].Status != "running" {
		t.Errorf("resumed = %+v, want only session 2, running", res.Resumed)
	}
	// 4: no workspace. 5: workspace gone. 6: another agent took it.
	if want := []int64{4, 5, 6}; !reflect.DeepEqual(res.Stop, want) {
		t.Errorf("stop = %v, want %v", res.Stop, want)
	}

	want := []entity.Action{
		entity.ActionMachineSessionResume,
		entity.ActionMachineSessionRefuse, entity.ActionMachineSessionRefuse, entity.ActionMachineSessionRefuse,
	}
	if got := env.actions(); !reflect.DeepEqual(got, want) {
		t.Fatalf("actions = %v, want %v", got, want)
	}
	if ev := (*env.events)[0]; ev.ResourceID != 2 || ev.WorkspaceID != 102 || ev.UserID != owner || ev.Actor != entity.ActorAgent {
		t.Errorf("resume = %+v", ev)
	}
	if ev := (*env.events)[3]; ev.ResourceID != 6 || ev.WorkspaceID != 106 || ev.UserID != owner {
		t.Errorf("refusal = %+v", ev)
	}
}

// Naming nothing still ends what the machine was running, and asks nothing
// else.
func TestReconcileSessionsNamingNothing(t *testing.T) {
	env := newMachineTelemetryEnv(t)
	env.repo.EXPECT().ReconcileSessions(gomock.Any(), int64(11), gomock.Len(0), gomock.Any()).Return(nil)
	env.repo.EXPECT().SessionsOnMachine(gomock.Any(), int64(11), gomock.Len(0)).Return(nil, nil)

	res, err := env.controller.ReconcileSessions(t.Context(), entity.ReconcileSessionsRequest{MachineID: 11})
	if err != nil || len(res.Resumed) != 0 || len(res.Stop) != 0 {
		t.Errorf("ReconcileSessions = %+v, %v; want nothing to do", res, err)
	}
}

func TestReconcileSessionsFailures(t *testing.T) {
	broken := errors.New("database unavailable")

	env := newMachineTelemetryEnv(t)
	if _, err := env.controller.ReconcileSessions(t.Context(), entity.ReconcileSessionsRequest{}); err == nil {
		t.Error("sessions were reconciled against no machine at all")
	}

	env.repo.EXPECT().ReconcileSessions(gomock.Any(), int64(11), gomock.Any(), gomock.Any()).Return(broken)
	if _, err := env.controller.ReconcileSessions(t.Context(), entity.ReconcileSessionsRequest{MachineID: 11, Running: []int64{1}}); !errors.Is(err, broken) {
		t.Errorf("a failed reconcile gave %v, want %v", err, broken)
	}

	env.repo.EXPECT().ReconcileSessions(gomock.Any(), int64(11), gomock.Any(), gomock.Any()).Return(nil)
	env.repo.EXPECT().SessionsOnMachine(gomock.Any(), int64(11), gomock.Any()).Return(nil, broken)
	if _, err := env.controller.ReconcileSessions(t.Context(), entity.ReconcileSessionsRequest{MachineID: 11, Running: []int64{1}}); !errors.Is(err, broken) {
		t.Errorf("a failed read gave %v, want %v", err, broken)
	}
	if len(*env.events) != 0 {
		t.Errorf("published %+v after a failure, want nothing", *env.events)
	}
}

// A state report names the machine reporting it, or it is refused.
func TestUpdateSessionStateNeedsTheMachine(t *testing.T) {
	env := newMachineTelemetryEnv(t)
	if err := env.controller.UpdateSessionState(t.Context(), entity.UpdateSessionStateRequest{
		SessionID: monoflake.ID(500).String(), Status: "running",
	}); err == nil {
		t.Error("a state report with no machine was recorded")
	}
}
