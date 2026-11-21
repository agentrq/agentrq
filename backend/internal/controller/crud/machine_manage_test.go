// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package crud

import (
	"errors"
	"strings"
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

// The cut-off is the online threshold back from now, and the counts come back
// keyed by each user's base62 id.
func TestCountOnlineMachinesPerUser(t *testing.T) {
	env := newTestController(t)
	before := time.Now().Add(-machinerules.OnlineThreshold)
	env.repo.EXPECT().CountOnlineMachinesPerUser(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ any, since time.Time) (map[int64]int, error) {
			if since.Before(before) || since.After(time.Now().Add(-machinerules.OnlineThreshold)) {
				t.Errorf("cut-off %v is not the online threshold back from now", since)
			}
			return map[int64]int{100: 2, 200: 1}, nil
		})

	got, err := env.controller.CountOnlineMachinesPerUser(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{monoflake.ID(100).String(): 2, monoflake.ID(200).String(): 1}
	if len(got) != len(want) || got[monoflake.ID(100).String()] != 2 || got[monoflake.ID(200).String()] != 1 {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestCountOnlineMachinesPerUser_PassesOnAFailure(t *testing.T) {
	env := newTestController(t)
	env.repo.EXPECT().CountOnlineMachinesPerUser(gomock.Any(), gomock.Any()).Return(nil, errors.New("database unavailable"))
	if got, err := env.controller.CountOnlineMachinesPerUser(t.Context()); err == nil || got != nil {
		t.Errorf("got %v, %v; want nil and the error", got, err)
	}
}

// setCeiling sets the server's machines.maxSessions for one test.
func setCeiling(t *testing.T, n int) {
	was := MachineMaxSessions
	t.Cleanup(func() { MachineMaxSessions = was })
	MachineMaxSessions = n
}

// A machine's own cap is stored as sent, and null takes it back to the
// server's; a request that does not mention it leaves it alone.
func TestUpdateMachineSetsAndClearsItsSessionCap(t *testing.T) {
	for _, tc := range []struct {
		name     string
		ceiling  int
		was      *int
		set      bool
		limit    *int
		want     *int
		wantView int
	}{
		{name: "set", ceiling: 8, set: true, limit: intPtr(3), want: intPtr(3), wantView: 3},
		{name: "set to the ceiling", ceiling: 8, set: true, limit: intPtr(8), want: intPtr(8), wantView: 8},
		{name: "no cap under no ceiling", was: intPtr(3), set: true, limit: intPtr(0), want: intPtr(0), wantView: 0},
		{name: "above 8 under no ceiling", set: true, limit: intPtr(50), want: intPtr(50), wantView: 50},
		{name: "cleared", ceiling: 8, was: intPtr(3), set: true, want: nil, wantView: 8},
		{name: "not mentioned", ceiling: 8, was: intPtr(3), want: intPtr(3), wantView: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setCeiling(t, tc.ceiling)
			env := newMachineTelemetryEnv(t)
			env.repo.EXPECT().GetMachine(gomock.Any(), int64(9), testUserID).
				Return(model.Machine{ID: 9, UserID: testUserID, Enabled: true, MaxSessions: tc.was}, nil)
			var saved model.Machine
			env.repo.EXPECT().UpdateMachine(gomock.Any(), gomock.Any()).
				DoAndReturn(func(_ any, m model.Machine) (model.Machine, error) { saved = m; return m, nil })

			rs, err := env.controller.UpdateMachine(t.Context(), entity.UpdateMachineRequest{
				UserID: testUserIDStr, MachineID: monoflake.ID(9).String(),
				SetMaxSessions: tc.set, MaxSessions: tc.limit,
			})
			if err != nil {
				t.Fatal(err)
			}
			if !sameLimit(saved.MaxSessions, tc.want) {
				t.Errorf("stored cap = %v, want %v", saved.MaxSessions, tc.want)
			}
			if rs.Machine.SessionLimit != tc.wantView || !sameLimit(rs.Machine.MaxSessions, tc.want) {
				t.Errorf("view = %v/%d, want %v/%d", rs.Machine.MaxSessions, rs.Machine.SessionLimit, tc.want, tc.wantView)
			}
			if rs.Machine.SessionCeiling != tc.ceiling {
				t.Errorf("ceiling = %d, want %d", rs.Machine.SessionCeiling, tc.ceiling)
			}
		})
	}
}

// Refused before the machine is read, so nothing is stored. Under a ceiling a
// machine may only go lower, and "no cap" would go past it.
func TestUpdateMachineRefusesASessionCapOutOfRange(t *testing.T) {
	for _, tc := range []struct {
		ceiling int
		n       int
		want    string
	}{
		{0, -1, "between 0 and 1000"},
		{0, MaxMachineSessionsOverride + 1, "between 0 and 1000"},
		{8, 9, "between 1 and 8, the server's limit"},
		{8, 0, "between 1 and 8, the server's limit"},
		{8, -1, "between 1 and 8, the server's limit"},
	} {
		setCeiling(t, tc.ceiling)
		env := newTestController(t) // no repository calls may happen
		_, err := env.controller.UpdateMachine(t.Context(), entity.UpdateMachineRequest{
			UserID: testUserBase62, MachineID: monoflake.ID(5).String(),
			SetMaxSessions: true, MaxSessions: intPtr(tc.n),
		})
		var launchErr *entity.LaunchError
		if !errors.As(err, &launchErr) || !errors.Is(launchErr.Kind, entity.ErrLaunchInvalid) ||
			!strings.Contains(err.Error(), tc.want) {
			t.Errorf("ceiling %d, maxSessions %d: err = %v, want an invalid request saying %q", tc.ceiling, tc.n, err, tc.want)
		}
	}
}

// The server's cap applies until the machine sets one, and is the ceiling of
// one it sets: lowering it lowers a cap stored above it, or a stored "no cap".
func TestMachineViewSessionLimit(t *testing.T) {
	for _, tc := range []struct {
		name    string
		ceiling int
		own     *int
		want    int
	}{
		{"no cap of its own", 5, nil, 5},
		{"its own, lower", 5, intPtr(2), 2},
		{"its own, the same", 5, intPtr(5), 5},
		{"its own, above a lowered ceiling", 5, intPtr(9), 5},
		{"its own no cap, under a ceiling", 5, intPtr(0), 5},
		{"no ceiling, its own", 0, intPtr(50), 50},
		{"no ceiling, its own no cap", 0, intPtr(0), 0},
		{"no ceiling, nothing of its own", 0, nil, 0},
	} {
		setCeiling(t, tc.ceiling)
		if got := toMachineView(model.Machine{MaxSessions: tc.own}, time.Now(), 0).SessionLimit; got != tc.want {
			t.Errorf("%s: %d, want %d", tc.name, got, tc.want)
		}
	}
}
