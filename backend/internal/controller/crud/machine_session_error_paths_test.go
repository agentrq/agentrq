// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package crud

import (
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/mustafaturan/monoflake"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
)

func TestFirstNonEmpty_AllEmpty(t *testing.T) {
	if got := firstNonEmpty("", ""); got != "" {
		t.Errorf("firstNonEmpty of blanks = %q, want empty", got)
	}
}

func TestEnrolMachine_PassesOnWriteFailures(t *testing.T) {
	uid := monoflake.IDFromBase62(testUserBase62).Int64()
	t.Run("the machine", func(t *testing.T) {
		env := newTestController(t)
		stored := codeFor(t, "ABCD-2345", uid, time.Now().Add(time.Minute), nil)
		env.repo.EXPECT().GetEnrolmentCode(gomock.Any(), gomock.Any()).Return(stored, nil)
		env.idgen.EXPECT().NextID().Return(int64(777))
		env.repo.EXPECT().CreateMachine(gomock.Any(), gomock.Any()).Return(model.Machine{}, errDBDown)
		_, err := env.controller.EnrolMachine(t.Context(), entity.EnrolMachineRequest{Code: "ABCD-2345"})
		wantErrIs(t, "EnrolMachine", err, errDBDown)
	})
	t.Run("the code", func(t *testing.T) {
		env := newTestController(t)
		stored := codeFor(t, "ABCD-2345", uid, time.Now().Add(time.Minute), nil)
		env.repo.EXPECT().GetEnrolmentCode(gomock.Any(), gomock.Any()).Return(stored, nil)
		env.idgen.EXPECT().NextID().Return(int64(777))
		env.repo.EXPECT().CreateMachine(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ any, m model.Machine) (model.Machine, error) { return m, nil })
		env.repo.EXPECT().ConsumeEnrolmentCode(gomock.Any(), stored.ID, int64(777), gomock.Any()).Return(false, errDBDown)
		_, err := env.controller.EnrolMachine(t.Context(), entity.EnrolMachineRequest{Code: "ABCD-2345"})
		wantErrIs(t, "EnrolMachine", err, errDBDown)
	})
}

func TestListMachines_PassesOnAFailure(t *testing.T) {
	env := newTestController(t)
	uid := monoflake.IDFromBase62(testUserBase62).Int64()
	env.repo.EXPECT().ListMachines(gomock.Any(), uid).Return(nil, errDBDown)
	_, err := env.controller.ListMachines(t.Context(), entity.ListMachinesRequest{UserID: testUserBase62})
	wantErrIs(t, "ListMachines", err, errDBDown)
}

// One machine carries its live session count, and keeps its page when the
// count cannot be had.
func TestGetMachine(t *testing.T) {
	uid := monoflake.IDFromBase62(testUserBase62).Int64()
	req := entity.GetMachineRequest{UserID: testUserBase62, MachineID: monoflake.ID(5).String()}
	machine := model.Machine{ID: 5, UserID: uid, Name: "studio", Enabled: true}

	env := newTestController(t)
	env.repo.EXPECT().GetMachine(gomock.Any(), int64(5), uid).Return(machine, nil)
	env.repo.EXPECT().CountLiveSessionsByUser(gomock.Any(), uid).Return(map[int64]int{5: 2}, nil)
	rs, err := env.controller.GetMachine(t.Context(), req)
	if err != nil || rs.Machine.Name != "studio" || rs.Machine.Sessions != 2 {
		t.Errorf("GetMachine = %+v, %v; want studio with 2 sessions", rs, err)
	}

	env = newTestController(t)
	env.repo.EXPECT().GetMachine(gomock.Any(), int64(5), uid).Return(machine, nil)
	env.repo.EXPECT().CountLiveSessionsByUser(gomock.Any(), uid).Return(nil, errDBDown)
	rs, err = env.controller.GetMachine(t.Context(), req)
	if err != nil || rs.Machine.Sessions != 0 {
		t.Errorf("GetMachine without counts = %+v, %v; want the machine with none", rs, err)
	}

	env = newTestController(t)
	env.repo.EXPECT().GetMachine(gomock.Any(), int64(5), uid).Return(model.Machine{}, base.ErrNotFound)
	_, err = env.controller.GetMachine(t.Context(), req)
	wantErrIs(t, "GetMachine", err, base.ErrNotFound)
}

func TestUpdateMachine_Failures(t *testing.T) {
	uid := monoflake.IDFromBase62(testUserBase62).Int64()
	name := "n"
	req := entity.UpdateMachineRequest{UserID: testUserBase62, MachineID: monoflake.ID(5).String(), Name: &name}

	env := newTestController(t)
	_, err := env.controller.UpdateMachine(t.Context(), entity.UpdateMachineRequest{UserID: testUserBase62})
	wantErrText(t, "UpdateMachine", err, "invalid id")

	env.repo.EXPECT().GetMachine(gomock.Any(), int64(5), uid).Return(model.Machine{}, base.ErrNotFound)
	_, err = env.controller.UpdateMachine(t.Context(), req)
	wantErrIs(t, "UpdateMachine", err, base.ErrNotFound)

	env.repo.EXPECT().GetMachine(gomock.Any(), int64(5), uid).Return(model.Machine{ID: 5, UserID: uid}, nil)
	env.repo.EXPECT().UpdateMachine(gomock.Any(), gomock.Any()).Return(model.Machine{}, errDBDown)
	_, err = env.controller.UpdateMachine(t.Context(), req)
	wantErrIs(t, "UpdateMachine", err, errDBDown)
}

func TestSessions_RefuseUnusableIDs(t *testing.T) {
	env := newTestController(t) // no repository calls may happen
	ctx := t.Context()
	if _, err := env.controller.CreateSession(ctx, entity.CreateSessionRequest{UserID: testUserBase62}); err == nil {
		t.Error("CreateSession accepted an absent machine")
	}
	if err := env.controller.UpdateSessionState(ctx, entity.UpdateSessionStateRequest{}); err == nil {
		t.Error("UpdateSessionState accepted an absent session")
	}
	if _, err := env.controller.GetSession(ctx, entity.GetSessionRequest{UserID: testUserBase62}); err == nil {
		t.Error("GetSession accepted an absent session")
	}
	if _, err := env.controller.ListSessions(ctx, entity.ListSessionsRequest{UserID: testUserBase62}); err == nil {
		t.Error("ListSessions accepted an absent machine")
	}
}

func TestSessions_PassOnFailures(t *testing.T) {
	uid := monoflake.IDFromBase62(testUserBase62).Int64()
	ctx := t.Context()
	env := newTestController(t)

	env.idgen.EXPECT().NextID().Return(int64(9))
	env.repo.EXPECT().CreateSession(gomock.Any(), gomock.Any()).Return(model.Session{}, errDBDown)
	_, err := env.controller.CreateSession(ctx, entity.CreateSessionRequest{UserID: testUserBase62, MachineID: monoflake.ID(3).String()})
	wantErrIs(t, "CreateSession", err, errDBDown)

	env.repo.EXPECT().GetSession(gomock.Any(), int64(9), uid).Return(model.Session{}, base.ErrNotFound)
	_, err = env.controller.GetSession(ctx, entity.GetSessionRequest{UserID: testUserBase62, SessionID: monoflake.ID(9).String()})
	wantErrIs(t, "GetSession", err, base.ErrNotFound)

	env.repo.EXPECT().ListSessionsByMachine(gomock.Any(), int64(3), uid).Return(nil, errDBDown)
	_, err = env.controller.ListSessions(ctx, entity.ListSessionsRequest{UserID: testUserBase62, MachineID: monoflake.ID(3).String()})
	wantErrIs(t, "ListSessions", err, errDBDown)
}
