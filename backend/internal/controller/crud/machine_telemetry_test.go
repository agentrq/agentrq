// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package crud

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/mustafaturan/monoflake"

	machinerules "github.com/agentrq/agentrq/backend/internal/controller/machine"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	mock_idgen "github.com/agentrq/agentrq/backend/internal/service/mocks/idgen"
	mock_pubsub "github.com/agentrq/agentrq/backend/internal/service/mocks/pubsub"
	mock_repo "github.com/agentrq/agentrq/backend/internal/service/mocks/repository"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
)

// A controller whose published events are collected rather than discarded.
//
// The shared harness stubs Publish with AnyTimes and throws the event away,
// which is the right default for tests about behaviour — but here the event
// *is* the behaviour being tested.
type machineTelemetryEnv struct {
	controller Controller
	repo       *mock_repo.MockRepository
	idgen      *mock_idgen.MockService
	events     *[]entity.CRUDEvent
}

func newMachineTelemetryEnv(t *testing.T) *machineTelemetryEnv {
	t.Helper()
	ctrl := gomock.NewController(t)
	repo := mock_repo.NewMockRepository(ctrl)
	idgen := mock_idgen.NewMockService(ctrl)
	psSvc := mock_pubsub.NewMockService(ctrl)

	events := &[]entity.CRUDEvent{}
	psSvc.EXPECT().Publish(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, req pubsub.PublishRequest) (*pubsub.PublishResponse, error) {
			if req.PubSubID != entity.PubSubTopicCRUD {
				t.Errorf("published to topic %d, want the CRUD topic", req.PubSubID)
			}
			if e, ok := req.Event.(entity.CRUDEvent); ok {
				*events = append(*events, e)
			}
			return &pubsub.PublishResponse{}, nil
		}).AnyTimes()

	return &machineTelemetryEnv{
		controller: New(Params{IDGen: idgen, Repository: repo, PubSub: psSvc}),
		repo:       repo,
		idgen:      idgen,
		events:     events,
	}
}

// only returns the single event expected, failing if there is not exactly one.
//
// Exactness matters more than it looks: an action emitted twice double-counts
// forever, and there is no way to tell from the stored rows afterwards.
func (e *machineTelemetryEnv) only(t *testing.T) entity.CRUDEvent {
	t.Helper()
	if len(*e.events) != 1 {
		t.Fatalf("published %d events, want exactly 1: %+v", len(*e.events), *e.events)
	}
	return (*e.events)[0]
}

func (e *machineTelemetryEnv) actions() []entity.Action {
	out := make([]entity.Action, 0, len(*e.events))
	for _, ev := range *e.events {
		out = append(out, ev.Action)
	}
	return out
}

// ── Machines ────────────────────────────────────────────────────────────────

// The first half of adding a machine. Counted on its own so that a code with
// no enrolment behind it — somebody who could not finish installing the daemon
// — is visible at all.
func TestCreateEnrolmentCodeCountsTheStartOfAdding(t *testing.T) {
	env := newMachineTelemetryEnv(t)
	env.idgen.EXPECT().NextID().Return(int64(31))
	env.repo.EXPECT().CreateEnrolmentCode(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, m model.EnrolmentCode) (model.EnrolmentCode, error) { return m, nil })

	_, err := env.controller.CreateEnrolmentCode(t.Context(), entity.CreateEnrolmentCodeRequest{UserID: testUserIDStr})
	if err != nil {
		t.Fatal(err)
	}

	ev := env.only(t)
	if ev.Action != entity.ActionMachineEnrolCodeCreate {
		t.Errorf("action = %v, want machine_enrol_code_create", ev.Action)
	}
	if ev.UserID != testUserID || ev.WorkspaceID != 0 {
		t.Errorf("scope = ws %d / user %d", ev.WorkspaceID, ev.UserID)
	}
}

// A code that was never stored cannot be redeemed, so it is not the start of
// anything.
func TestCreateEnrolmentCodeDoesNotCountAFailedWrite(t *testing.T) {
	env := newMachineTelemetryEnv(t)
	env.idgen.EXPECT().NextID().Return(int64(31))
	env.repo.EXPECT().CreateEnrolmentCode(gomock.Any(), gomock.Any()).
		Return(model.EnrolmentCode{}, errors.New("database unavailable"))

	if _, err := env.controller.CreateEnrolmentCode(t.Context(), entity.CreateEnrolmentCodeRequest{UserID: testUserIDStr}); err == nil {
		t.Fatal("expected the failure to be reported")
	}
	if len(*env.events) != 0 {
		t.Errorf("published %+v, want nothing", *env.events)
	}
}

func TestEnrolMachineCountsTheEnrolment(t *testing.T) {
	env := newMachineTelemetryEnv(t)
	code := model.EnrolmentCode{ID: 5, UserID: testUserID, ExpiresAt: time.Now().Add(time.Hour)}
	env.repo.EXPECT().GetEnrolmentCode(gomock.Any(), gomock.Any()).Return(code, nil)
	env.idgen.EXPECT().NextID().Return(int64(77))
	env.repo.EXPECT().CreateMachine(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, m model.Machine) (model.Machine, error) { return m, nil })
	env.repo.EXPECT().ConsumeEnrolmentCode(gomock.Any(), int64(5), int64(77), gomock.Any()).Return(true, nil)

	if _, err := env.controller.EnrolMachine(t.Context(), entity.EnrolMachineRequest{Code: "abc-def"}); err != nil {
		t.Fatal(err)
	}

	ev := env.only(t)
	if ev.Action != entity.ActionMachineAdd {
		t.Errorf("action = %v, want machine_add", ev.Action)
	}
	if ev.UserID != testUserID {
		t.Errorf("userID = %d, want %d", ev.UserID, testUserID)
	}
	// A machine belongs to an account and runs agents for many workspaces, so
	// there is no workspace to attribute an enrolment to.
	if ev.WorkspaceID != 0 {
		t.Errorf("workspaceID = %d, want 0 — a machine has no workspace", ev.WorkspaceID)
	}
	if ev.ResourceType != entity.ResourceMachine || ev.ResourceID != 77 {
		t.Errorf("resource = %v/%d", ev.ResourceType, ev.ResourceID)
	}
}

// The loser of an enrolment race has its machine deleted again, so counting it
// would count an enrolment that does not exist.
func TestEnrolMachineDoesNotCountALostRace(t *testing.T) {
	env := newMachineTelemetryEnv(t)
	code := model.EnrolmentCode{ID: 5, UserID: testUserID, ExpiresAt: time.Now().Add(time.Hour)}
	env.repo.EXPECT().GetEnrolmentCode(gomock.Any(), gomock.Any()).Return(code, nil)
	env.idgen.EXPECT().NextID().Return(int64(77))
	env.repo.EXPECT().CreateMachine(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, m model.Machine) (model.Machine, error) { return m, nil })
	env.repo.EXPECT().ConsumeEnrolmentCode(gomock.Any(), int64(5), int64(77), gomock.Any()).Return(false, nil)
	env.repo.EXPECT().DeleteMachine(gomock.Any(), int64(77), testUserID).Return(nil)

	if _, err := env.controller.EnrolMachine(t.Context(), entity.EnrolMachineRequest{Code: "abc-def"}); err == nil {
		t.Fatal("expected the enrolment to be rejected")
	}

	if len(*env.events) != 0 {
		t.Errorf("published %+v, want nothing", *env.events)
	}
}

func TestDeleteMachineCountsTheRemoval(t *testing.T) {
	env := newMachineTelemetryEnv(t)
	env.repo.EXPECT().DeleteMachine(gomock.Any(), int64(9), testUserID).Return(nil)

	err := env.controller.DeleteMachine(t.Context(), entity.DeleteMachineRequest{
		UserID: testUserIDStr, MachineID: monoflake.ID(9).String(),
	})
	if err != nil {
		t.Fatal(err)
	}

	ev := env.only(t)
	if ev.Action != entity.ActionMachineRemove || ev.ResourceID != 9 {
		t.Errorf("event = %+v", ev)
	}
}

// A delete that failed removed nothing.
func TestDeleteMachineDoesNotCountAFailure(t *testing.T) {
	env := newMachineTelemetryEnv(t)
	env.repo.EXPECT().DeleteMachine(gomock.Any(), int64(9), testUserID).Return(errors.New("database unavailable"))

	err := env.controller.DeleteMachine(t.Context(), entity.DeleteMachineRequest{
		UserID: testUserIDStr, MachineID: monoflake.ID(9).String(),
	})
	if err == nil {
		t.Fatal("expected the failure to be reported")
	}
	if len(*env.events) != 0 {
		t.Errorf("published %+v, want nothing", *env.events)
	}
}

// The kill switch, in both directions — and only when it actually moved.
func TestUpdateMachineCountsTheKillSwitchTransition(t *testing.T) {
	for _, tc := range []struct {
		name    string
		was     bool
		set     *bool
		newName *string
		// The machine's own session cap before, and what is sent.
		limitWas *int
		setLimit bool
		limit    *int
		want     []entity.Action
	}{
		{name: "enabled to disabled", was: true, set: boolPtr(false), want: []entity.Action{entity.ActionMachineDisable}},
		{name: "disabled to enabled", was: false, set: boolPtr(true), want: []entity.Action{entity.ActionMachineEnable}},
		// Asking for a state it is already in has turned nothing off. Counting
		// it would make the number mean "how often was this asked for".
		{name: "already disabled", was: false, set: boolPtr(false), want: nil},
		{name: "already enabled", was: true, set: boolPtr(true), want: nil},
		// A rename says nothing about whether machines are being used.
		{name: "rename only", was: true, newName: strPtr("kitchen pi"), want: nil},
		{name: "a cap set", was: true, setLimit: true, limit: intPtr(4), want: []entity.Action{entity.ActionMachineSessionLimitSet}},
		{name: "a cap changed", was: true, limitWas: intPtr(4), setLimit: true, limit: intPtr(2), want: []entity.Action{entity.ActionMachineSessionLimitSet}},
		{name: "a cap cleared", was: true, limitWas: intPtr(4), setLimit: true, want: []entity.Action{entity.ActionMachineSessionLimitSet}},
		{name: "the same cap again", was: true, limitWas: intPtr(4), setLimit: true, limit: intPtr(4), want: nil},
		{name: "no cap cleared again", was: true, setLimit: true, want: nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newMachineTelemetryEnv(t)
			env.repo.EXPECT().GetMachine(gomock.Any(), int64(9), testUserID).
				Return(model.Machine{ID: 9, UserID: testUserID, Enabled: tc.was, MaxSessions: tc.limitWas}, nil)
			env.repo.EXPECT().UpdateMachine(gomock.Any(), gomock.Any()).
				DoAndReturn(func(_ context.Context, m model.Machine) (model.Machine, error) { return m, nil })

			_, err := env.controller.UpdateMachine(t.Context(), entity.UpdateMachineRequest{
				UserID: testUserIDStr, MachineID: monoflake.ID(9).String(),
				Enabled: tc.set, Name: tc.newName,
				SetMaxSessions: tc.setLimit, MaxSessions: tc.limit,
			})
			if err != nil {
				t.Fatal(err)
			}

			got := env.actions()
			if len(got) != len(tc.want) {
				t.Fatalf("actions = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("actions = %v, want %v", got, tc.want)
				}
			}
		})
	}
}

// ── Sessions ────────────────────────────────────────────────────────────────

func TestCreateSessionCountsItAgainstItsWorkspace(t *testing.T) {
	env := newMachineTelemetryEnv(t)
	env.idgen.EXPECT().NextID().Return(int64(500))
	env.repo.EXPECT().CreateSession(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, s model.Session) (model.Session, error) { return s, nil })

	_, err := env.controller.CreateSession(t.Context(), entity.CreateSessionRequest{
		UserID:      testUserIDStr,
		MachineID:   monoflake.ID(9).String(),
		WorkspaceID: monoflake.ID(testWorkspaceID).String(),
		Kind:        "claude-code",
	})
	if err != nil {
		t.Fatal(err)
	}

	ev := env.only(t)
	if ev.Action != entity.ActionMachineSessionCreate {
		t.Errorf("action = %v, want machine_session_create", ev.Action)
	}
	// Unlike a machine, a session knows which workspace it is working in, and
	// that is the dimension worth having.
	if ev.WorkspaceID != testWorkspaceID {
		t.Errorf("workspaceID = %d, want %d", ev.WorkspaceID, testWorkspaceID)
	}
	if ev.ResourceType != entity.ResourceSession || ev.ResourceID != 500 {
		t.Errorf("resource = %v/%d", ev.ResourceType, ev.ResourceID)
	}
}

func TestUpdateSessionStateCountsOpenAndClose(t *testing.T) {
	for _, tc := range []struct {
		name     string
		status   string
		want     entity.Action
		terminal bool
	}{
		{name: "running is an open", status: machinerules.SessionRunning, want: entity.ActionMachineSessionOpen},
		{name: "exited is a close", status: machinerules.SessionExited, want: entity.ActionMachineSessionClose, terminal: true},
		{name: "killed is a close", status: machinerules.SessionKilled, want: entity.ActionMachineSessionClose, terminal: true},
		{name: "failed is a close", status: machinerules.SessionFailed, want: entity.ActionMachineSessionClose, terminal: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newMachineTelemetryEnv(t)
			env.repo.EXPECT().GetSession(gomock.Any(), int64(500), testUserID).
				Return(model.Session{ID: 500, UserID: testUserID, WorkspaceID: testWorkspaceID}, nil)
			env.repo.EXPECT().UpdateSessionState(gomock.Any(), int64(500), int64(7), tc.status, gomock.Any(), gomock.Any(), false).Return(nil)
			if tc.terminal {
				env.repo.EXPECT().DeleteFinishedSession(gomock.Any(), int64(500)).Return(nil)
			}

			err := env.controller.UpdateSessionState(t.Context(), entity.UpdateSessionStateRequest{
				SessionID: monoflake.ID(500).String(),
				MachineID: 7,
				UserID:    testUserIDStr,
				Status:    tc.status,
			})
			if err != nil {
				t.Fatal(err)
			}

			ev := env.only(t)
			if ev.Action != tc.want {
				t.Errorf("action = %v, want %v", ev.Action, tc.want)
			}
			if ev.WorkspaceID != testWorkspaceID || ev.UserID != testUserID {
				t.Errorf("scope = ws %d / user %d", ev.WorkspaceID, ev.UserID)
			}
			// The daemon reports these: an agent exits on its own, and a
			// machine restarting closes every session on it. Recording that as
			// a person's doing would put it in the wrong half of every actor
			// breakdown.
			if ev.Actor != entity.ActorAgent {
				t.Errorf("actor = %v, want agent", ev.Actor)
			}
		})
	}
}

// "starting" is the row being written, which CreateSession already counted.
func TestUpdateSessionStateDoesNotCountStarting(t *testing.T) {
	env := newMachineTelemetryEnv(t)
	env.repo.EXPECT().UpdateSessionState(gomock.Any(), int64(500), int64(7), machinerules.SessionStarting, gomock.Any(), gomock.Any(), false).Return(nil)

	err := env.controller.UpdateSessionState(t.Context(), entity.UpdateSessionStateRequest{
		SessionID: monoflake.ID(500).String(),
		MachineID: 7,
		UserID:    testUserIDStr,
		Status:    machinerules.SessionStarting,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(*env.events) != 0 {
		t.Errorf("published %+v, want nothing", *env.events)
	}
}

// A caller that does not say who owns the session still records the state.
// Telemetry is the thing that gives way, never the report.
func TestUpdateSessionStateWithoutAUserStillRecordsTheState(t *testing.T) {
	// "" is a caller that did not set it; "0" is one whose id decodes to
	// nothing. Note that arbitrary rubbish does *not* land here — base62
	// decodes it to some number, and that path fails at the lookup instead
	// (see TestUpdateSessionStateSurvivesAnUnreadableSession).
	for _, user := range []string{"", "0"} {
		env := newMachineTelemetryEnv(t)
		env.repo.EXPECT().UpdateSessionState(gomock.Any(), int64(500), int64(7), machinerules.SessionRunning, gomock.Any(), gomock.Any(), false).Return(nil)

		err := env.controller.UpdateSessionState(t.Context(), entity.UpdateSessionStateRequest{
			SessionID: monoflake.ID(500).String(),
			MachineID: 7,
			UserID:    user,
			Status:    machinerules.SessionRunning,
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(*env.events) != 0 {
			t.Errorf("user %q: published %+v, want nothing", user, *env.events)
		}
	}
}

// A session that cannot be read is not counted, and the state report still
// succeeds — the row is what the daemon came to write.
func TestUpdateSessionStateSurvivesAnUnreadableSession(t *testing.T) {
	env := newMachineTelemetryEnv(t)
	env.repo.EXPECT().GetSession(gomock.Any(), int64(500), testUserID).Return(model.Session{}, errors.New("session not found"))
	env.repo.EXPECT().UpdateSessionState(gomock.Any(), int64(500), int64(7), machinerules.SessionRunning, gomock.Any(), gomock.Any(), false).Return(nil)

	err := env.controller.UpdateSessionState(t.Context(), entity.UpdateSessionStateRequest{
		SessionID: monoflake.ID(500).String(),
		MachineID: 7,
		UserID:    testUserIDStr,
		Status:    machinerules.SessionRunning,
	})
	if err != nil {
		t.Fatalf("a failed lookup must not fail the state report: %v", err)
	}
	if len(*env.events) != 0 {
		t.Errorf("published %+v, want nothing", *env.events)
	}
}

// A failed write is not a state change, so there is nothing to count.
func TestUpdateSessionStateDoesNotCountAFailedWrite(t *testing.T) {
	env := newMachineTelemetryEnv(t)
	env.repo.EXPECT().GetSession(gomock.Any(), int64(500), testUserID).
		Return(model.Session{ID: 500, UserID: testUserID, WorkspaceID: testWorkspaceID}, nil)
	env.repo.EXPECT().UpdateSessionState(gomock.Any(), int64(500), int64(7), machinerules.SessionRunning, gomock.Any(), gomock.Any(), false).
		Return(errors.New("database unavailable"))

	err := env.controller.UpdateSessionState(t.Context(), entity.UpdateSessionStateRequest{
		SessionID: monoflake.ID(500).String(),
		MachineID: 7,
		UserID:    testUserIDStr,
		Status:    machinerules.SessionRunning,
	})
	if err == nil {
		t.Fatal("expected the write failure to be reported")
	}
	if len(*env.events) != 0 {
		t.Errorf("published %+v, want nothing", *env.events)
	}
}

// ── Stopping and watching ───────────────────────────────────────────────────

func TestRecordSessionKillCountsThePerson(t *testing.T) {
	env := newMachineTelemetryEnv(t)

	env.controller.RecordSessionKill(t.Context(), entity.RecordSessionKillRequest{
		UserID:      testUserIDStr,
		WorkspaceID: monoflake.ID(testWorkspaceID).String(),
		SessionID:   monoflake.ID(500).String(),
	})

	ev := env.only(t)
	if ev.Action != entity.ActionMachineSessionKill {
		t.Errorf("action = %v, want machine_session_kill", ev.Action)
	}
	// The distinction this action exists for: a close is whatever the daemon
	// reported, a kill is somebody deciding.
	if ev.Actor != entity.ActorHuman {
		t.Errorf("actor = %v, want human", ev.Actor)
	}
	if ev.WorkspaceID != testWorkspaceID || ev.ResourceID != 500 {
		t.Errorf("event = %+v", ev)
	}
}

func TestRecordSessionKillIgnoresAnUnreadableUser(t *testing.T) {
	env := newMachineTelemetryEnv(t)

	env.controller.RecordSessionKill(t.Context(), entity.RecordSessionKillRequest{
		UserID:    "",
		SessionID: monoflake.ID(500).String(),
	})

	if len(*env.events) != 0 {
		t.Errorf("published %+v, want nothing", *env.events)
	}
}

func TestRecordMachineCommandCountsThePerson(t *testing.T) {
	for _, action := range []entity.Action{entity.ActionMachineRestart, entity.ActionMachineUpdate} {
		env := newMachineTelemetryEnv(t)

		env.controller.RecordMachineCommand(t.Context(), entity.RecordMachineCommandRequest{
			UserID:    testUserIDStr,
			MachineID: 42,
			Action:    action,
		})

		ev := env.only(t)
		if ev.Action != action || ev.Actor != entity.ActorHuman {
			t.Errorf("event = %+v", ev)
		}
		// A machine has no workspace; see docs/agents/telemetry.md.
		if ev.WorkspaceID != 0 || ev.ResourceType != entity.ResourceMachine || ev.ResourceID != 42 {
			t.Errorf("event = %+v", ev)
		}
	}
}

func TestRecordMachineCommandIgnoresAnUnreadableUser(t *testing.T) {
	env := newMachineTelemetryEnv(t)

	env.controller.RecordMachineCommand(t.Context(), entity.RecordMachineCommandRequest{
		MachineID: 42, Action: entity.ActionMachineRestart,
	})

	if len(*env.events) != 0 {
		t.Errorf("published %+v, want nothing", *env.events)
	}
}

func TestRecordTerminalViewCountsBothEnds(t *testing.T) {
	for _, tc := range []struct {
		name string
		open bool
		want entity.Action
	}{
		{name: "attach", open: true, want: entity.ActionMachineTerminalOpen},
		{name: "detach", open: false, want: entity.ActionMachineTerminalClose},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newMachineTelemetryEnv(t)

			env.controller.RecordTerminalView(t.Context(), entity.RecordTerminalViewRequest{
				UserID:      testUserID,
				WorkspaceID: testWorkspaceID,
				SessionID:   500,
				Open:        tc.open,
			})

			ev := env.only(t)
			if ev.Action != tc.want {
				t.Errorf("action = %v, want %v", ev.Action, tc.want)
			}
			// Somebody sat down in front of it, which is the whole point of
			// counting an attach.
			if ev.Actor != entity.ActorHuman {
				t.Errorf("actor = %v, want human", ev.Actor)
			}
			if ev.WorkspaceID != testWorkspaceID || ev.ResourceID != 500 {
				t.Errorf("event = %+v", ev)
			}
		})
	}
}

func TestRecordTerminalViewIgnoresAnUnknownUser(t *testing.T) {
	env := newMachineTelemetryEnv(t)

	env.controller.RecordTerminalView(t.Context(), entity.RecordTerminalViewRequest{SessionID: 500, Open: true})

	if len(*env.events) != 0 {
		t.Errorf("published %+v, want nothing", *env.events)
	}
}

// ── Names and values ────────────────────────────────────────────────────────

// Every new action has to stringify, or it lands in telemetry as "unknown" and
// the counts cannot be told apart when they are read back.
func TestMachineActionsStringify(t *testing.T) {
	for action, name := range map[entity.Action]string{
		entity.ActionMachineAdd:             "machine_add",
		entity.ActionMachineRemove:          "machine_remove",
		entity.ActionMachineDisable:         "machine_disable",
		entity.ActionMachineEnable:          "machine_enable",
		entity.ActionMachineSessionCreate:   "machine_session_create",
		entity.ActionMachineSessionOpen:     "machine_session_open",
		entity.ActionMachineSessionClose:    "machine_session_close",
		entity.ActionMachineSessionKill:     "machine_session_kill",
		entity.ActionMachineSessionResume:   "machine_session_resume",
		entity.ActionMachineSessionRefuse:   "machine_session_refuse",
		entity.ActionMachineTerminalOpen:    "machine_terminal_open",
		entity.ActionMachineTerminalClose:   "machine_terminal_close",
		entity.ActionMachineEnrolCodeCreate: "machine_enrol_code_create",
		entity.ActionMachineRestart:         "machine_restart",
		entity.ActionMachineUpdate:          "machine_update",
	} {
		if got := action.String(); got != name {
			t.Errorf("%d: got %q, want %q", action, got, name)
		}
	}
}

// Distinct values, because the action is stored as a number: two actions
// sharing one would be indistinguishable rows forever after.
func TestMachineActionsAreDistinct(t *testing.T) {
	seen := map[entity.Action]bool{}
	for _, a := range []entity.Action{
		entity.ActionMachineAdd,
		entity.ActionMachineRemove,
		entity.ActionMachineDisable,
		entity.ActionMachineEnable,
		entity.ActionMachineSessionCreate,
		entity.ActionMachineSessionOpen,
		entity.ActionMachineSessionClose,
		entity.ActionMachineSessionKill,
		entity.ActionMachineTerminalOpen,
		entity.ActionMachineTerminalClose,
		entity.ActionMachineEnrolCodeCreate,
		entity.ActionMachineRestart,
		entity.ActionMachineUpdate,
	} {
		if seen[a] {
			t.Errorf("%v (%s) is used twice", a, a.String())
		}
		seen[a] = true
	}
}

func TestMachineResourceTypesStringify(t *testing.T) {
	if got := entity.ResourceMachine.String(); got != "machine" {
		t.Errorf("machine resource = %q", got)
	}
	if got := entity.ResourceSession.String(); got != "session" {
		t.Errorf("session resource = %q", got)
	}
}

func boolPtr(b bool) *bool    { return &b }
func intPtr(n int) *int       { return &n }
func strPtr(s string) *string { return &s }
