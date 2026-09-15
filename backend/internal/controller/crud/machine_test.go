// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package crud

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/mustafaturan/monoflake"
	"gorm.io/gorm"

	machinerules "github.com/agentrq/agentrq/backend/internal/controller/machine"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
)

const testUserBase62 = "0imWEckAo7t"

func TestCreateEnrolmentCodeReturnsThePlainCodeOnlyHere(t *testing.T) {
	env := newTestController(t)
	env.idgen.EXPECT().NextID().Return(int64(101))

	var stored model.EnrolmentCode
	env.repo.EXPECT().CreateEnrolmentCode(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ any, c model.EnrolmentCode) (model.EnrolmentCode, error) {
			stored = c
			return c, nil
		})

	rs, err := env.controller.CreateEnrolmentCode(t.Context(),
		entity.CreateEnrolmentCodeRequest{UserID: testUserBase62})
	if err != nil {
		t.Fatalf("CreateEnrolmentCode: %v", err)
	}

	if rs.Code == "" {
		t.Fatal("no code returned")
	}
	// The row must hold a hash, never the code. This response is the single
	// moment the plain code exists anywhere.
	if stored.CodeHash == rs.Code {
		t.Fatal("the enrolment code was stored in plain text")
	}
	if !machinerules.SecretMatches(stored.CodeHash, rs.Code) {
		t.Error("the stored hash does not match the code that was handed out")
	}
	if stored.UsedAt != nil {
		t.Error("a fresh code is already marked used")
	}
	if !stored.ExpiresAt.After(time.Now()) {
		t.Error("a fresh code is already expired")
	}
}

// The guard catches an absent user id, and only that.
//
// Worth being precise about, because it is weaker than it looks:
// monoflake.IDFromBase62 does not validate — "not-an-id" parses happily into
// 10877870460556359 rather than zero. So this rejects an empty or zero id and
// nothing else. That is acceptable here because the value comes from the
// session rather than from the request body (see the handler), but the check
// should not be mistaken for validation. The same pattern is used throughout
// this package; changing it is a separate job.
func TestCreateEnrolmentCodeRejectsAnAbsentUser(t *testing.T) {
	for _, bad := range []string{"", "0", "   ", "!!!"} {
		env := newTestController(t) // no repository calls may happen
		if _, err := env.controller.CreateEnrolmentCode(t.Context(),
			entity.CreateEnrolmentCodeRequest{UserID: bad}); err == nil {
			t.Errorf("CreateEnrolmentCode(%q) succeeded, want a failure", bad)
		}
	}
}

// codeFor builds the stored row for a known code.
func codeFor(t *testing.T, code string, userID int64, expires time.Time, used *time.Time) model.EnrolmentCode {
	t.Helper()
	return model.EnrolmentCode{
		ID: 55, UserID: userID,
		CodeHash:  machinerules.HashSecret(machinerules.NormalizeCode(code)),
		ExpiresAt: expires, UsedAt: used,
	}
}

func TestEnrolMachineTradesACodeForATokenItNeverStores(t *testing.T) {
	env := newTestController(t)
	uid := monoflake.IDFromBase62(testUserBase62).Int64()
	stored := codeFor(t, "ABCD-2345", uid, time.Now().Add(time.Minute), nil)

	env.repo.EXPECT().GetEnrolmentCode(gomock.Any(), stored.CodeHash).Return(stored, nil)
	env.idgen.EXPECT().NextID().Return(int64(777))

	var created model.Machine
	env.repo.EXPECT().CreateMachine(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ any, m model.Machine) (model.Machine, error) {
			created = m
			return m, nil
		})
	env.repo.EXPECT().ConsumeEnrolmentCode(gomock.Any(), stored.ID, int64(777), gomock.Any()).Return(true, nil)

	// Typed the way a person types it: lowercase, no hyphen.
	rs, err := env.controller.EnrolMachine(t.Context(), entity.EnrolMachineRequest{
		Code: "abcd2345", Hostname: "bb01", OS: "linux", Arch: "arm64", Version: "0.7.0",
	})
	if err != nil {
		t.Fatalf("EnrolMachine: %v", err)
	}

	if rs.MachineToken == "" || rs.MachineID == "" {
		t.Fatalf("incomplete response: %+v", rs)
	}
	// The row holds a hash. A database that leaks must not hand over the
	// ability to act as every enrolled machine.
	if created.TokenHash == rs.MachineToken {
		t.Fatal("the machine token was stored in plain text")
	}
	if !machinerules.SecretMatches(created.TokenHash, rs.MachineToken) {
		t.Error("the stored hash does not match the token that was handed out")
	}
	// The code decides whose machine this is — not anything in the request.
	if created.UserID != uid {
		t.Errorf("machine owner = %d, want %d (from the code)", created.UserID, uid)
	}
	if !created.Enabled {
		t.Error("a newly enrolled machine should be enabled")
	}
	// With no name given, the hostname is the sensible fallback.
	if created.Name != "bb01" {
		t.Errorf("name = %q, want the hostname", created.Name)
	}
}

// "Unknown", "expired" and "already used" are different facts. Telling them
// apart lets someone holding a stolen code learn whether it was ever real.
func TestEnrolMachineGivesOneAnswerForEveryRejection(t *testing.T) {
	uid := monoflake.IDFromBase62(testUserBase62).Int64()
	used := time.Now().Add(-time.Minute)

	tests := []struct {
		name  string
		setup func(env *testEnv)
	}{
		{"unknown code", func(env *testEnv) {
			env.repo.EXPECT().GetEnrolmentCode(gomock.Any(), gomock.Any()).
				Return(model.EnrolmentCode{}, gorm.ErrRecordNotFound)
		}},
		{"expired code", func(env *testEnv) {
			env.repo.EXPECT().GetEnrolmentCode(gomock.Any(), gomock.Any()).
				Return(codeFor(t, "ABCD-2345", uid, time.Now().Add(-time.Second), nil), nil)
		}},
		{"already used", func(env *testEnv) {
			env.repo.EXPECT().GetEnrolmentCode(gomock.Any(), gomock.Any()).
				Return(codeFor(t, "ABCD-2345", uid, time.Now().Add(time.Minute), &used), nil)
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env := newTestController(t)
			tc.setup(env)
			_, err := env.controller.EnrolMachine(t.Context(),
				entity.EnrolMachineRequest{Code: "ABCD-2345"})
			if !errors.Is(err, ErrEnrolmentRejected) {
				t.Fatalf("error = %v, want ErrEnrolmentRejected", err)
			}
			// Identical text, not merely the same sentinel: a message that
			// varied would leak the distinction the sentinel hides.
			if err.Error() != ErrEnrolmentRejected.Error() {
				t.Errorf("rejection text = %q, want the shared one", err.Error())
			}
		})
	}
}

func TestEnrolMachineRejectsAnEmptyCodeWithoutTouchingTheDatabase(t *testing.T) {
	env := newTestController(t) // no repository expectations: none may be called
	if _, err := env.controller.EnrolMachine(t.Context(),
		entity.EnrolMachineRequest{Code: "   "}); !errors.Is(err, ErrEnrolmentRejected) {
		t.Errorf("error = %v, want ErrEnrolmentRejected", err)
	}
}

// Two requests arriving together both pass the "is it used?" check. Only one
// can win the conditional update, and the loser must not keep a machine.
func TestEnrolMachineUndoesItselfWhenItLosesTheRace(t *testing.T) {
	env := newTestController(t)
	uid := monoflake.IDFromBase62(testUserBase62).Int64()
	stored := codeFor(t, "ABCD-2345", uid, time.Now().Add(time.Minute), nil)

	env.repo.EXPECT().GetEnrolmentCode(gomock.Any(), gomock.Any()).Return(stored, nil)
	env.idgen.EXPECT().NextID().Return(int64(777))
	env.repo.EXPECT().CreateMachine(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ any, m model.Machine) (model.Machine, error) { return m, nil })
	// Somebody else consumed the code first.
	env.repo.EXPECT().ConsumeEnrolmentCode(gomock.Any(), stored.ID, int64(777), gomock.Any()).Return(false, nil)
	// So the machine this call made has to go.
	env.repo.EXPECT().DeleteMachine(gomock.Any(), int64(777), uid).Return(nil)

	if _, err := env.controller.EnrolMachine(t.Context(),
		entity.EnrolMachineRequest{Code: "ABCD-2345"}); !errors.Is(err, ErrEnrolmentRejected) {
		t.Errorf("error = %v, want ErrEnrolmentRejected", err)
	}
}

// A daemon is not necessarily one of ours, and a hostname of a megabyte should
// be cut here rather than rejected by the database as a server error.
func TestEnrolMachineBoundsWhatTheDaemonReports(t *testing.T) {
	env := newTestController(t)
	uid := monoflake.IDFromBase62(testUserBase62).Int64()
	stored := codeFor(t, "ABCD-2345", uid, time.Now().Add(time.Minute), nil)

	env.repo.EXPECT().GetEnrolmentCode(gomock.Any(), gomock.Any()).Return(stored, nil)
	env.idgen.EXPECT().NextID().Return(int64(777))
	var created model.Machine
	env.repo.EXPECT().CreateMachine(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ any, m model.Machine) (model.Machine, error) {
			created = m
			return m, nil
		})
	env.repo.EXPECT().ConsumeEnrolmentCode(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil)

	if _, err := env.controller.EnrolMachine(t.Context(), entity.EnrolMachineRequest{
		Code:     "ABCD-2345",
		Name:     strings.Repeat("n", 5000),
		Hostname: strings.Repeat("h", 5000),
		OS:       strings.Repeat("o", 5000),
		Arch:     strings.Repeat("a", 5000),
		Version:  strings.Repeat("v", 5000),
	}); err != nil {
		t.Fatalf("EnrolMachine: %v", err)
	}

	for field, got := range map[string]struct {
		value string
		max   int
	}{
		"name": {created.Name, 128}, "hostname": {created.Hostname, 255},
		"os": {created.OS, 32}, "arch": {created.Arch, 32}, "version": {created.Version, 64},
	} {
		if len(got.value) > got.max {
			t.Errorf("%s is %d characters, want at most %d", field, len(got.value), got.max)
		}
	}
}
