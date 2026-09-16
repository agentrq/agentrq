// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package crud

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"gorm.io/datatypes"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
)

// A zeroed struct would say this box has no memory and an idle CPU, which is a
// confident lie where "we have not heard from it" is the truth.
func TestAMachineThatHasNeverReportedShowsNoMetrics(t *testing.T) {
	if got := toMetricsView(model.Machine{MemTotal: 1 << 30}); got != nil {
		t.Errorf("a machine with no report showed %+v", got)
	}
}

func TestTheLastSnapshotIsRendered(t *testing.T) {
	at := time.Now()
	m := model.Machine{
		MemTotal: 16 << 30, MemAvailable: 4 << 30,
		CPUPercent: 37.4, UptimeSec: 918273,
		LoadAvg:   "[1.2,0.9,0.7]",
		Disks:     datatypes.JSON(`[{"mount":"/","total":100,"free":40}]`),
		MetricsAt: &at,
	}
	v := toMetricsView(m)
	if v == nil {
		t.Fatal("a reported machine showed nothing")
	}
	if v.MemAvailable != 4<<30 || v.CPUPercent != 37.4 || v.UptimeSec != 918273 {
		t.Errorf("metrics = %+v", v)
	}
	if len(v.LoadAvg) != 3 || v.LoadAvg[0] != 1.2 {
		t.Errorf("loadAvg = %v", v.LoadAvg)
	}
	if len(v.Disks) != 1 || v.Disks[0].Mount != "/" || v.Disks[0].Free != 40 {
		t.Errorf("disks = %+v", v.Disks)
	}
	if !v.ReportedAt.Equal(at) {
		t.Error("a snapshot with no time on it cannot be shown as stale")
	}
}

// Absent, not zeroed: three zeroes render as a perfectly idle machine, which
// is the most misleading thing this view could say about a Windows box.
func TestAWindowsMachineShowsNoLoadAverage(t *testing.T) {
	at := time.Now()
	v := toMetricsView(model.Machine{MetricsAt: &at, CPUPercent: 12})
	if v.LoadAvg != nil {
		t.Errorf("loadAvg = %v, want nothing at all", v.LoadAvg)
	}
}

// Stored JSON that cannot be read is dropped rather than taking the whole
// snapshot with it: the memory and CPU figures beside it are still true.
func TestUnreadableStoredListsDoNotLoseTheRest(t *testing.T) {
	at := time.Now()
	v := toMetricsView(model.Machine{
		MetricsAt: &at, CPUPercent: 12,
		LoadAvg: "not json", Disks: datatypes.JSON("not json"),
	})
	if v == nil || v.CPUPercent != 12 {
		t.Fatalf("view = %+v", v)
	}
	if v.LoadAvg != nil || v.Disks != nil {
		t.Errorf("unreadable lists were rendered: %+v", v)
	}
}

func TestRecordMachineMetrics(t *testing.T) {
	env := newTestController(t)
	at := time.Now()

	env.repo.EXPECT().
		RecordMachineMetrics(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ any, m model.Machine) error {
			if m.ID != 11 {
				t.Errorf("recorded against machine %d", m.ID)
			}
			if m.MemAvailable != 4<<30 || m.CPUPercent != 37.4 {
				t.Errorf("stored %+v", m)
			}
			var avg []float64
			if err := json.Unmarshal([]byte(m.LoadAvg), &avg); err != nil || len(avg) != 3 {
				t.Errorf("loadAvg stored as %q", m.LoadAvg)
			}
			var disks []entity.MachineDiskView
			if err := json.Unmarshal(m.Disks, &disks); err != nil || len(disks) != 1 {
				t.Errorf("disks stored as %q", m.Disks)
			}
			if m.MetricsAt == nil || !m.MetricsAt.Equal(at) {
				t.Errorf("metricsAt = %v", m.MetricsAt)
			}
			return nil
		})

	err := env.controller.RecordMachineMetrics(t.Context(), entity.RecordMachineMetricsRequest{
		MachineID: 11, MemTotal: 16 << 30, MemAvailable: 4 << 30,
		CPUPercent: 37.4, LoadAvg: []float64{1.2, 0.9, 0.7}, UptimeSec: 918273,
		Disks:      []entity.MachineDiskView{{Mount: "/", Total: 100, Free: 40}},
		ReportedAt: at,
	})
	if err != nil {
		t.Fatal(err)
	}
}

// A platform with no load average stores none, so the view can tell "not here"
// from "zero".
func TestRecordingNoLoadAverageStoresNothing(t *testing.T) {
	env := newTestController(t)
	env.repo.EXPECT().
		RecordMachineMetrics(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ any, m model.Machine) error {
			if m.LoadAvg != "" {
				t.Errorf("loadAvg stored as %q for a platform that has none", m.LoadAvg)
			}
			if len(m.Disks) != 0 {
				t.Errorf("disks stored as %q when none were reported", m.Disks)
			}
			return nil
		})

	if err := env.controller.RecordMachineMetrics(t.Context(), entity.RecordMachineMetricsRequest{
		MachineID: 11, CPUPercent: 12,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRecordMachineMetricsRefusesAnEmptyMachine(t *testing.T) {
	env := newTestController(t)
	if err := env.controller.RecordMachineMetrics(t.Context(), entity.RecordMachineMetricsRequest{}); err == nil {
		t.Error("metrics were recorded against no machine at all")
	}
}

// A report with no time on it is still a report; it is stamped on arrival
// rather than dropped.
func TestAReportWithNoTimeIsStampedOnArrival(t *testing.T) {
	env := newTestController(t)
	env.repo.EXPECT().
		RecordMachineMetrics(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ any, m model.Machine) error {
			if m.MetricsAt == nil || m.MetricsAt.IsZero() {
				t.Error("a report arrived with no time and was stored with none")
			}
			return nil
		})

	if err := env.controller.RecordMachineMetrics(t.Context(), entity.RecordMachineMetricsRequest{
		MachineID: 11,
	}); err != nil {
		t.Fatal(err)
	}
}

// The daemon is the authority on what is alive on its own machine.
func TestReconcileSessions(t *testing.T) {
	env := newTestController(t)
	env.repo.EXPECT().
		ReconcileSessions(gomock.Any(), int64(11), []int64{9, 3}, gomock.Any()).
		Return(nil)

	if err := env.controller.ReconcileSessions(t.Context(), entity.ReconcileSessionsRequest{
		MachineID: 11, Running: []int64{9, 3},
	}); err != nil {
		t.Fatal(err)
	}
}

func TestReconcileSessionsRefusesAnEmptyMachine(t *testing.T) {
	env := newTestController(t)
	if err := env.controller.ReconcileSessions(t.Context(), entity.ReconcileSessionsRequest{}); err == nil {
		t.Error("sessions were reconciled against no machine at all")
	}
}
