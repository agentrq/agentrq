// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package base

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/agentrq/agentrq/backend/internal/data/model"
)

func machineDB(t *testing.T) (*gorm.DB, Repository) {
	t.Helper()
	db := forkFolderDB(t)
	if err := db.AutoMigrate(&model.EnrolmentCode{}); err != nil {
		t.Fatal(err)
	}
	return db, New(&mockDB{db: db})
}

// An enrolment code is found by its hash, and consumed exactly once.
func TestEnrolmentCodeIsSingleUse(t *testing.T) {
	_, repo := machineDB(t)
	ctx := context.Background()
	code, err := repo.CreateEnrolmentCode(ctx, model.EnrolmentCode{ID: 5, UserID: memUserID, CodeHash: "h", ExpiresAt: time.Now().Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if code.ID != 5 {
		t.Errorf("created code id = %d, want 5", code.ID)
	}

	got, err := repo.GetEnrolmentCode(ctx, "h")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != 5 || got.UserID != memUserID {
		t.Errorf("code = %+v, want id 5 of user %d", got, memUserID)
	}
	if _, err := repo.GetEnrolmentCode(ctx, "other"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Errorf("an unknown hash gave %v, want gorm.ErrRecordNotFound", err)
	}

	at := time.Now()
	ok, err := repo.ConsumeEnrolmentCode(ctx, 5, 77, at)
	if err != nil || !ok {
		t.Fatalf("first consume = %v, %v; want true", ok, err)
	}
	ok, err = repo.ConsumeEnrolmentCode(ctx, 5, 78, at)
	if err != nil || ok {
		t.Errorf("second consume = %v, %v; want false, the code is used", ok, err)
	}
	got, _ = repo.GetEnrolmentCode(ctx, "h")
	if got.UsedAt == nil || got.MachineID != 77 {
		t.Errorf("used code = %+v, want used by machine 77", got)
	}
}

// A machine is read by its token hash or by id within its own account, and
// is written back whole.
func TestMachineCreateReadUpdate(t *testing.T) {
	_, repo := machineDB(t)
	ctx := context.Background()
	if _, err := repo.CreateMachine(ctx, model.Machine{ID: 1, UserID: memUserID, TokenHash: "t1", Name: "old"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateMachine(ctx, model.Machine{ID: 2, UserID: memUserID, TokenHash: "t2", CreatedAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateMachine(ctx, model.Machine{ID: 3, UserID: memOtherUserID, TokenHash: "t3"}); err != nil {
		t.Fatal(err)
	}

	m, err := repo.GetMachineByTokenHash(ctx, "t1")
	if err != nil || m.ID != 1 {
		t.Errorf("by token = %+v, %v; want machine 1", m, err)
	}
	if _, err := repo.GetMachineByTokenHash(ctx, "nope"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Errorf("unknown token gave %v, want gorm.ErrRecordNotFound", err)
	}
	if _, err := repo.GetMachine(ctx, 3, memUserID); !errors.Is(err, ErrNotFound) {
		t.Errorf("another account's machine gave %v, want ErrNotFound", err)
	}

	list, err := repo.ListMachines(ctx, memUserID)
	if err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for _, m := range list {
		ids = append(ids, m.ID)
	}
	if want := []int64{2, 1}; !reflect.DeepEqual(ids, want) {
		t.Errorf("listed %v, want %v (newest first, own only)", ids, want)
	}

	m, _ = repo.GetMachine(ctx, 1, memUserID)
	m.Name = "new"
	if _, err := repo.UpdateMachine(ctx, m); err != nil {
		t.Fatal(err)
	}
	if m, _ = repo.GetMachine(ctx, 1, memUserID); m.Name != "new" {
		t.Errorf("name after update = %q, want new", m.Name)
	}
}

// A heartbeat pairs a machine with an instance; only that instance can
// release it.
func TestTouchAndReleaseMachine(t *testing.T) {
	_, repo := machineDB(t)
	ctx := context.Background()
	if _, err := repo.CreateMachine(ctx, model.Machine{ID: 1, UserID: memUserID, TokenHash: "t"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.TouchMachine(ctx, 1, time.Now(), "inst-a"); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReleaseMachine(ctx, 1, "inst-b"); err != nil {
		t.Fatal(err)
	}
	m, _ := repo.GetMachine(ctx, 1, memUserID)
	if m.InstanceID != "inst-a" || m.LastSeenAt == nil || m.ConnectedAt == nil {
		t.Errorf("after another instance's release = %+v, want still held by inst-a", m)
	}
	if err := repo.ReleaseMachine(ctx, 1, "inst-a"); err != nil {
		t.Fatal(err)
	}
	m, _ = repo.GetMachine(ctx, 1, memUserID)
	if m.InstanceID != "" || m.ConnectedAt != nil {
		t.Errorf("after its own release = %+v, want unpaired", m)
	}
}

// The version a daemon reports is stored, and an offer of that same version
// is cleared while an offer of a newer one stays. An empty version writes
// nothing.
func TestRecordMachineVersion(t *testing.T) {
	_, repo := machineDB(t)
	ctx := context.Background()
	for _, m := range []model.Machine{
		{ID: 1, UserID: memUserID, TokenHash: "a", Version: "0.1", AvailableVersion: "0.2"},
		{ID: 2, UserID: memUserID, TokenHash: "b", Version: "0.1", AvailableVersion: "0.3"},
	} {
		if _, err := repo.CreateMachine(ctx, m); err != nil {
			t.Fatal(err)
		}
	}
	for id, v := range map[int64]string{1: "0.2", 2: "0.2"} {
		if err := repo.RecordMachineVersion(ctx, id, v); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.RecordMachineVersion(ctx, 1, ""); err != nil {
		t.Fatal(err)
	}
	m1, _ := repo.GetMachine(ctx, 1, memUserID)
	m2, _ := repo.GetMachine(ctx, 2, memUserID)
	if m1.Version != "0.2" || m1.AvailableVersion != "" {
		t.Errorf("caught-up machine = %q offered %q, want 0.2 offered nothing", m1.Version, m1.AvailableVersion)
	}
	if m2.Version != "0.2" || m2.AvailableVersion != "0.3" {
		t.Errorf("still-behind machine = %q offered %q, want 0.2 offered 0.3", m2.Version, m2.AvailableVersion)
	}

	if err := repo.RecordAvailableVersion(ctx, 1, "0.4"); err != nil {
		t.Fatal(err)
	}
	if m1, _ = repo.GetMachine(ctx, 1, memUserID); m1.AvailableVersion != "0.4" {
		t.Errorf("available version = %q, want 0.4", m1.AvailableVersion)
	}
}

func TestRecordMachineMetrics(t *testing.T) {
	_, repo := machineDB(t)
	ctx := context.Background()
	if _, err := repo.CreateMachine(ctx, model.Machine{ID: 1, UserID: memUserID, TokenHash: "a", Name: "kept"}); err != nil {
		t.Fatal(err)
	}
	at := time.Now()
	err := repo.RecordMachineMetrics(ctx, model.Machine{
		ID: 1, MemTotal: 100, MemAvailable: 40, CPUPercent: 12.5, LoadAvg: "[1,2,3]",
		UptimeSec: 60, Disks: datatypes.JSON(`[{"mount":"/"}]`), MetricsAt: &at,
	})
	if err != nil {
		t.Fatal(err)
	}
	m, _ := repo.GetMachine(ctx, 1, memUserID)
	if m.MemTotal != 100 || m.MemAvailable != 40 || m.CPUPercent != 12.5 || m.LoadAvg != "[1,2,3]" ||
		m.UptimeSec != 60 || string(m.Disks) != `[{"mount":"/"}]` || m.MetricsAt == nil || m.Name != "kept" {
		t.Errorf("machine after metrics = %+v", m)
	}
}

// A session is read within its own account, listed per machine newest first,
// and is the workspace's active one only while it is starting or running.
func TestSessionReads(t *testing.T) {
	_, repo := machineDB(t)
	ctx := context.Background()
	now := time.Now()
	for _, s := range []model.Session{
		{ID: 10, MachineID: 1, UserID: memUserID, WorkspaceID: 7, Status: "exited", CreatedAt: now},
		{ID: 11, MachineID: 1, UserID: memUserID, WorkspaceID: 7, Status: "running", CreatedAt: now.Add(time.Minute)},
		{ID: 12, MachineID: 2, UserID: memUserID, WorkspaceID: 8, Status: "starting", CreatedAt: now},
		{ID: 13, MachineID: 1, UserID: memOtherUserID, WorkspaceID: 9, Status: "running", CreatedAt: now},
	} {
		if _, err := repo.CreateSession(ctx, s); err != nil {
			t.Fatal(err)
		}
	}

	if s, err := repo.GetSession(ctx, 11, memUserID); err != nil || s.Status != "running" {
		t.Errorf("session 11 = %+v, %v", s, err)
	}
	if _, err := repo.GetSession(ctx, 13, memUserID); !errors.Is(err, ErrNotFound) {
		t.Errorf("another account's session gave %v, want ErrNotFound", err)
	}

	list, err := repo.ListSessionsByMachine(ctx, 1, memUserID)
	if err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for _, s := range list {
		ids = append(ids, s.ID)
	}
	if want := []int64{11, 10}; !reflect.DeepEqual(ids, want) {
		t.Errorf("machine 1's sessions = %v, want %v", ids, want)
	}

	if s, err := repo.ActiveSessionForWorkspace(ctx, 7, memUserID); err != nil || s.ID != 11 {
		t.Errorf("workspace 7's active session = %d, %v; want 11", s.ID, err)
	}
	if _, err := repo.ActiveSessionForWorkspace(ctx, 9, memUserID); !errors.Is(err, ErrNotFound) {
		t.Errorf("another account's workspace gave %v, want ErrNotFound", err)
	}

	counts, err := repo.CountLiveSessionsByUser(ctx, memUserID)
	if err != nil {
		t.Fatal(err)
	}
	if want := map[int64]int{1: 1, 2: 1}; !reflect.DeepEqual(counts, want) {
		t.Errorf("live counts = %v, want %v", counts, want)
	}
}

// A state report writes the exit and end only when given, and sets restored
// without ever clearing it.
func TestUpdateSessionState(t *testing.T) {
	_, repo := machineDB(t)
	ctx := context.Background()
	if _, err := repo.CreateSession(ctx, model.Session{ID: 10, UserID: memUserID, Status: "starting"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateSessionState(ctx, 10, "running", nil, nil, true); err != nil {
		t.Fatal(err)
	}
	s, _ := repo.GetSession(ctx, 10, memUserID)
	if s.Status != "running" || !s.Restored || s.ExitCode != nil || s.EndedAt != nil {
		t.Errorf("after running = %+v, want running, restored, no exit", s)
	}

	code, ended := 3, time.Now()
	if err := repo.UpdateSessionState(ctx, 10, "exited", &code, &ended, false); err != nil {
		t.Fatal(err)
	}
	s, _ = repo.GetSession(ctx, 10, memUserID)
	if s.Status != "exited" || !s.Restored || s.ExitCode == nil || *s.ExitCode != 3 || s.EndedAt == nil {
		t.Errorf("after exit = %+v, want exited with 3, still restored", s)
	}
}

// Only an ended session can be deleted one at a time.
func TestDeleteFinishedSession(t *testing.T) {
	db, repo := machineDB(t)
	ctx := context.Background()
	for _, s := range []model.Session{
		{ID: 10, UserID: memUserID, Status: "running"},
		{ID: 11, UserID: memUserID, Status: "killed"},
	} {
		if _, err := repo.CreateSession(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []int64{10, 11} {
		if err := repo.DeleteFinishedSession(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	if got, want := sessionIDs(t, db), []int64{10}; !reflect.DeepEqual(got, want) {
		t.Errorf("sessions left = %v, want %v", got, want)
	}
}

// A hello ends this machine's live sessions the daemon did not name, and no
// other machine's; naming none ends all of them.
func TestReconcileSessions(t *testing.T) {
	db, repo := machineDB(t)
	ctx := context.Background()
	for _, s := range []model.Session{
		{ID: 10, MachineID: 1, UserID: memUserID, Status: "running"},
		{ID: 11, MachineID: 1, UserID: memUserID, Status: "starting"},
		{ID: 12, MachineID: 1, UserID: memUserID, Status: "exited"},
		{ID: 13, MachineID: 2, UserID: memUserID, Status: "running"},
		{ID: 14, MachineID: 3, UserID: memUserID, Status: "running"},
	} {
		if _, err := repo.CreateSession(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.ReconcileSessions(ctx, 1, []int64{10}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReconcileSessions(ctx, 3, nil, time.Now()); err != nil {
		t.Fatal(err)
	}
	if got, want := sessionIDs(t, db), []int64{10, 12, 13}; !reflect.DeepEqual(got, want) {
		t.Errorf("sessions left = %v, want %v", got, want)
	}
}

// With no tables at all every read and write fails, and the failure is what
// comes back rather than a zero value that looks like an answer.
func TestMachineRepositoryPassesOnFailures(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	repo := New(&mockDB{db: db})
	ctx := context.Background()
	now := time.Now()

	checks := map[string]error{}
	_, checks["CreateEnrolmentCode"] = repo.CreateEnrolmentCode(ctx, model.EnrolmentCode{ID: 1})
	_, checks["GetEnrolmentCode"] = repo.GetEnrolmentCode(ctx, "h")
	_, checks["ConsumeEnrolmentCode"] = repo.ConsumeEnrolmentCode(ctx, 1, 2, now)
	_, checks["CreateMachine"] = repo.CreateMachine(ctx, model.Machine{ID: 1})
	_, checks["GetMachineByTokenHash"] = repo.GetMachineByTokenHash(ctx, "t")
	_, checks["GetMachine"] = repo.GetMachine(ctx, 1, 2)
	_, checks["UpdateMachine"] = repo.UpdateMachine(ctx, model.Machine{ID: 1})
	_, checks["CreateSession"] = repo.CreateSession(ctx, model.Session{ID: 1})
	_, checks["GetSession"] = repo.GetSession(ctx, 1, 2)
	_, checks["ActiveSessionForWorkspace"] = repo.ActiveSessionForWorkspace(ctx, 1, 2)
	_, checks["CountLiveSessionsByUser"] = repo.CountLiveSessionsByUser(ctx, 1)
	for name, err := range checks {
		if err == nil {
			t.Errorf("%s: no table, and no error", name)
		}
		if errors.Is(err, ErrNotFound) || errors.Is(err, gorm.ErrRecordNotFound) {
			t.Errorf("%s: a broken database read as not found: %v", name, err)
		}
	}
}
