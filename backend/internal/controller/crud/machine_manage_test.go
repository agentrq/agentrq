// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package crud

import (
	"errors"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/mustafaturan/monoflake"

	machinerules "github.com/agentrq/agentrq/backend/internal/controller/machine"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
)

// A stored flag says "online" forever when a daemon is killed, which is
// exactly the moment somebody is looking at the screen to find out.
func TestListMachinesDerivesOnlineFromTheLastHeartbeat(t *testing.T) {
	env := newTestController(t)
	uid := monoflake.IDFromBase62(testUserBase62).Int64()

	now := time.Now()
	fresh := now.Add(-time.Second)
	stale := now.Add(-2 * machinerules.OnlineThreshold)

	env.repo.EXPECT().ListMachines(gomock.Any(), uid).Return([]model.Machine{
		{ID: 1, UserID: uid, Name: "live", Enabled: true, LastSeenAt: &fresh},
		{ID: 2, UserID: uid, Name: "gone", Enabled: true, LastSeenAt: &stale},
		{ID: 3, UserID: uid, Name: "never", Enabled: true},
	}, nil)
	env.repo.EXPECT().CountLiveSessionsByUser(gomock.Any(), uid).
		Return(map[int64]int{1: 2}, nil)

	rs, err := env.controller.ListMachines(t.Context(), entity.ListMachinesRequest{UserID: testUserBase62})
	if err != nil {
		t.Fatalf("ListMachines: %v", err)
	}
	if len(rs.Machines) != 3 {
		t.Fatalf("machines = %d, want 3", len(rs.Machines))
	}
	want := map[string]bool{"live": true, "gone": false, "never": false}
	for _, m := range rs.Machines {
		if m.Online != want[m.Name] {
			t.Errorf("%s online = %v, want %v", m.Name, m.Online, want[m.Name])
		}
		if m.ID == "" {
			t.Errorf("%s has no base62 id", m.Name)
		}
	}

	// Zero is a real answer for a machine with no agents on it, and the count
	// comes from one query rather than one per machine.
	counts := map[string]int{}
	for _, m := range rs.Machines {
		counts[m.Name] = m.Sessions
	}
	if counts["live"] != 2 || counts["gone"] != 0 || counts["never"] != 0 {
		t.Errorf("session counts = %v", counts)
	}
}

// A machine list with no numbers on it is still the page somebody asked for.
func TestTheListSurvivesLosingItsCounts(t *testing.T) {
	env := newTestController(t)
	uid := monoflake.IDFromBase62(testUserBase62).Int64()

	env.repo.EXPECT().ListMachines(gomock.Any(), uid).
		Return([]model.Machine{{ID: 1, UserID: uid, Name: "live", Enabled: true}}, nil)
	env.repo.EXPECT().CountLiveSessionsByUser(gomock.Any(), uid).
		Return(nil, errors.New("the counting query failed"))

	rs, err := env.controller.ListMachines(t.Context(), entity.ListMachinesRequest{UserID: testUserBase62})
	if err != nil {
		t.Fatalf("a failed count lost the whole list: %v", err)
	}
	if len(rs.Machines) != 1 || rs.Machines[0].Sessions != 0 {
		t.Errorf("machines = %+v", rs.Machines)
	}
}

// Without pointers, a rename would silently re-enable a machine somebody had
// deliberately turned off — a bug discovered by a machine coming back to life.
func TestUpdateMachineTouchesOnlyWhatWasSent(t *testing.T) {
	uid := monoflake.IDFromBase62(testUserBase62).Int64()
	existing := model.Machine{ID: 5, UserID: uid, Name: "old name", Enabled: false}

	t.Run("rename leaves the kill switch alone", func(t *testing.T) {
		env := newTestController(t)
		env.repo.EXPECT().GetMachine(gomock.Any(), int64(5), uid).Return(existing, nil)

		var saved model.Machine
		env.repo.EXPECT().UpdateMachine(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ any, m model.Machine) (model.Machine, error) { saved = m; return m, nil })

		name := "new name"
		if _, err := env.controller.UpdateMachine(t.Context(), entity.UpdateMachineRequest{
			UserID: testUserBase62, MachineID: monoflake.ID(5).String(), Name: &name,
		}); err != nil {
			t.Fatalf("UpdateMachine: %v", err)
		}
		if saved.Name != "new name" {
			t.Errorf("name = %q", saved.Name)
		}
		if saved.Enabled {
			t.Error("a rename re-enabled a machine that was deliberately disabled")
		}
	})

	t.Run("enabling leaves the name alone", func(t *testing.T) {
		env := newTestController(t)
		env.repo.EXPECT().GetMachine(gomock.Any(), int64(5), uid).Return(existing, nil)

		var saved model.Machine
		env.repo.EXPECT().UpdateMachine(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ any, m model.Machine) (model.Machine, error) { saved = m; return m, nil })

		on := true
		if _, err := env.controller.UpdateMachine(t.Context(), entity.UpdateMachineRequest{
			UserID: testUserBase62, MachineID: monoflake.ID(5).String(), Enabled: &on,
		}); err != nil {
			t.Fatalf("UpdateMachine: %v", err)
		}
		if saved.Name != "old name" {
			t.Errorf("enabling changed the name to %q", saved.Name)
		}
		if !saved.Enabled {
			t.Error("the machine was not enabled")
		}
	})
}

func TestMachineReadsRejectUnusableIDs(t *testing.T) {
	env := newTestController(t) // no repository calls may happen
	if _, err := env.controller.GetMachine(t.Context(), entity.GetMachineRequest{UserID: "", MachineID: "x"}); err == nil {
		t.Error("GetMachine accepted an absent user")
	}
	if _, err := env.controller.ListMachines(t.Context(), entity.ListMachinesRequest{UserID: ""}); err == nil {
		t.Error("ListMachines accepted an absent user")
	}
	if err := env.controller.DeleteMachine(t.Context(), entity.DeleteMachineRequest{UserID: "", MachineID: "x"}); err == nil {
		t.Error("DeleteMachine accepted an absent user")
	}
}
