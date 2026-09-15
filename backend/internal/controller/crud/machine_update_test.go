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

func TestRecordAvailableVersion(t *testing.T) {
	env := newTestController(t)
	env.repo.EXPECT().RecordAvailableVersion(gomock.Any(), int64(11), "0.7.1").Return(nil)

	if err := env.controller.RecordAvailableVersion(t.Context(), entity.RecordAvailableVersionRequest{
		MachineID: 11, Version: "0.7.1",
	}); err != nil {
		t.Fatal(err)
	}
}

// An empty version clears the offer, rather than leaving a machine advertising
// an update it has already installed.
func TestRecordingAnEmptyVersionClearsTheOffer(t *testing.T) {
	env := newTestController(t)
	env.repo.EXPECT().RecordAvailableVersion(gomock.Any(), int64(11), "").Return(nil)

	if err := env.controller.RecordAvailableVersion(t.Context(), entity.RecordAvailableVersionRequest{
		MachineID: 11,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRecordAvailableVersionRefusesAnEmptyMachine(t *testing.T) {
	env := newTestController(t)
	if err := env.controller.RecordAvailableVersion(t.Context(), entity.RecordAvailableVersionRequest{}); err == nil {
		t.Error("a version was recorded against no machine at all")
	}
}

func TestApproveMachineUpdate(t *testing.T) {
	env := newTestController(t)
	uid := monoflake.IDFromBase62(testUserBase62).Int64()
	env.repo.EXPECT().GetMachine(gomock.Any(), int64(5), uid).
		Return(model.Machine{ID: 5, UserID: uid, AvailableVersion: "0.7.1"}, nil)

	rs, err := env.controller.ApproveMachineUpdate(t.Context(), entity.ApproveMachineUpdateRequest{
		UserID: testUserBase62, MachineID: monoflake.ID(5).String(), Version: "0.7.1",
	})
	if err != nil {
		t.Fatalf("ApproveMachineUpdate: %v", err)
	}
	if rs.MachineID != 5 || rs.Version != "0.7.1" {
		t.Errorf("approved %+v", rs)
	}
}

// Somebody agreeing to lose their sessions agreed to lose them for a
// particular release.
func TestApprovingSomethingTheMachineNeverOfferedIsRefused(t *testing.T) {
	uid := monoflake.IDFromBase62(testUserBase62).Int64()

	t.Run("a different version", func(t *testing.T) {
		env := newTestController(t)
		env.repo.EXPECT().GetMachine(gomock.Any(), int64(5), uid).
			Return(model.Machine{ID: 5, UserID: uid, AvailableVersion: "0.7.1"}, nil)

		_, err := env.controller.ApproveMachineUpdate(t.Context(), entity.ApproveMachineUpdateRequest{
			UserID: testUserBase62, MachineID: monoflake.ID(5).String(), Version: "9.9.9",
		})
		if !errors.Is(err, ErrNoUpdateOffered) {
			t.Errorf("error = %v, want ErrNoUpdateOffered", err)
		}
	})

	t.Run("nothing offered at all", func(t *testing.T) {
		env := newTestController(t)
		env.repo.EXPECT().GetMachine(gomock.Any(), int64(5), uid).
			Return(model.Machine{ID: 5, UserID: uid}, nil)

		_, err := env.controller.ApproveMachineUpdate(t.Context(), entity.ApproveMachineUpdateRequest{
			UserID: testUserBase62, MachineID: monoflake.ID(5).String(), Version: "0.7.1",
		})
		if !errors.Is(err, ErrNoUpdateOffered) {
			t.Errorf("error = %v, want ErrNoUpdateOffered", err)
		}
	})
}

func TestApproveMachineUpdateRefusesAnUnusableID(t *testing.T) {
	env := newTestController(t)
	if _, err := env.controller.ApproveMachineUpdate(t.Context(), entity.ApproveMachineUpdateRequest{}); err == nil {
		t.Error("an approval with no machine was accepted")
	}
}

// A machine belonging to somebody else is not found, which is both the right
// answer and the one that says least.
func TestApprovingAnotherAccountsMachineIsNotFound(t *testing.T) {
	env := newTestController(t)
	uid := monoflake.IDFromBase62(testUserBase62).Int64()
	env.repo.EXPECT().GetMachine(gomock.Any(), int64(5), uid).
		Return(model.Machine{}, errors.New("not found"))

	if _, err := env.controller.ApproveMachineUpdate(t.Context(), entity.ApproveMachineUpdateRequest{
		UserID: testUserBase62, MachineID: monoflake.ID(5).String(), Version: "0.7.1",
	}); err == nil {
		t.Error("an approval for another account's machine succeeded")
	}
}

func TestRecordMachineVersion(t *testing.T) {
	env := newTestController(t)
	env.repo.EXPECT().RecordMachineVersion(gomock.Any(), int64(11), "0.7.1").Return(nil)

	if err := env.controller.RecordMachineVersion(t.Context(), entity.RecordMachineVersionRequest{
		MachineID: 11, Version: "0.7.1",
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRecordMachineVersionRefusesAnEmptyMachine(t *testing.T) {
	env := newTestController(t)
	if err := env.controller.RecordMachineVersion(t.Context(), entity.RecordMachineVersionRequest{}); err == nil {
		t.Error("a version was recorded against no machine at all")
	}
}

// Set, never cleared: a restored session goes on to report running and then
// exiting, and those later reports say nothing about how it started.
func TestRestoredIsStickyAcrossLaterReports(t *testing.T) {
	env := newTestController(t)
	id := monoflake.ID(9).String()

	env.repo.EXPECT().UpdateSessionState(gomock.Any(), int64(9), "running", gomock.Any(), gomock.Any(), true).Return(nil)
	env.repo.EXPECT().UpdateSessionState(gomock.Any(), int64(9), "exited", gomock.Any(), gomock.Any(), false).Return(nil)

	if err := env.controller.UpdateSessionState(t.Context(), entity.UpdateSessionStateRequest{
		SessionID: id, Status: "running", Restored: true,
	}); err != nil {
		t.Fatal(err)
	}
	// The repository is what refuses to clear it; the controller passes on
	// what it was told, and the later report says nothing about restoration.
	if err := env.controller.UpdateSessionState(t.Context(), entity.UpdateSessionStateRequest{
		SessionID: id, Status: "exited",
	}); err != nil {
		t.Fatal(err)
	}
}
