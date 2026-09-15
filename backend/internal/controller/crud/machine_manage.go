// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package crud

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/mustafaturan/monoflake"

	machinerules "github.com/agentrq/agentrq/backend/internal/controller/machine"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
)

// MachineManageController is the control panel's view of enrolled machines.
type MachineManageController interface {
	ListMachines(ctx context.Context, req entity.ListMachinesRequest) (*entity.ListMachinesResponse, error)
	GetMachine(ctx context.Context, req entity.GetMachineRequest) (*entity.GetMachineResponse, error)
	UpdateMachine(ctx context.Context, req entity.UpdateMachineRequest) (*entity.UpdateMachineResponse, error)
	DeleteMachine(ctx context.Context, req entity.DeleteMachineRequest) error
	// RecordMachineMetrics stores what a daemon reported about its machine.
	// Not part of the control panel's surface — the daemon socket calls it —
	// but it belongs here rather than in a handler reaching for the
	// repository.
	RecordMachineMetrics(ctx context.Context, req entity.RecordMachineMetricsRequest) error
}

// toMachineView renders a machine for the API, deriving online from the last
// heartbeat rather than reading a stored flag.
func toMachineView(m model.Machine, now time.Time, sessions int) entity.MachineView {
	var lastSeen time.Time
	if m.LastSeenAt != nil {
		lastSeen = *m.LastSeenAt
	}
	return entity.MachineView{
		ID:               monoflake.ID(m.ID).String(),
		Name:             m.Name,
		Hostname:         m.Hostname,
		OS:               m.OS,
		Arch:             m.Arch,
		Version:          m.Version,
		Enabled:          m.Enabled,
		Online:           machinerules.IsOnline(lastSeen, now, machinerules.OnlineThreshold),
		LastSeenAt:       m.LastSeenAt,
		CreatedAt:        m.CreatedAt,
		AvailableVersion: m.AvailableVersion,
		Sessions:         sessions,
		Metrics:          toMetricsView(m),
	}
}

// toMetricsView renders the last snapshot, or nothing.
//
// Nothing when the machine has never reported: a zeroed struct would say this
// box has no memory and an idle CPU, which is a confident lie where "we have
// not heard" is the truth.
func toMetricsView(m model.Machine) *entity.MachineMetricsView {
	if m.MetricsAt == nil {
		return nil
	}
	v := &entity.MachineMetricsView{
		MemTotal:     m.MemTotal,
		MemAvailable: m.MemAvailable,
		CPUPercent:   m.CPUPercent,
		UptimeSec:    m.UptimeSec,
		ReportedAt:   *m.MetricsAt,
	}
	// Both of these are stored as JSON because they are lists whose length is
	// a property of the machine. A column each would mean deciding how many
	// filesystems a machine is allowed to have.
	if m.LoadAvg != "" {
		var avg []float64
		if err := json.Unmarshal([]byte(m.LoadAvg), &avg); err == nil {
			v.LoadAvg = avg
		}
	}
	if len(m.Disks) > 0 {
		var disks []entity.MachineDiskView
		if err := json.Unmarshal(m.Disks, &disks); err == nil {
			v.Disks = disks
		}
	}
	return v
}

// RecordMachineMetrics stores the latest snapshot and nothing historical.
func (c *controller) RecordMachineMetrics(ctx context.Context, req entity.RecordMachineMetricsRequest) error {
	if req.MachineID == 0 {
		return fmt.Errorf("invalid machine id")
	}
	at := req.ReportedAt
	if at.IsZero() {
		at = time.Now()
	}
	m := model.Machine{
		ID:           req.MachineID,
		MemTotal:     req.MemTotal,
		MemAvailable: req.MemAvailable,
		CPUPercent:   req.CPUPercent,
		UptimeSec:    req.UptimeSec,
		MetricsAt:    &at,
	}
	// Absent rather than empty when the platform has none, so the view can
	// tell "no load average here" from "a load average of zero".
	if len(req.LoadAvg) > 0 {
		if b, err := json.Marshal(req.LoadAvg); err == nil {
			m.LoadAvg = string(b)
		}
	}
	if len(req.Disks) > 0 {
		if b, err := json.Marshal(req.Disks); err == nil {
			m.Disks = b
		}
	}
	return c.repository.RecordMachineMetrics(ctx, m)
}

func (c *controller) ListMachines(ctx context.Context, req entity.ListMachinesRequest) (*entity.ListMachinesResponse, error) {
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	if uid == 0 {
		return nil, fmt.Errorf("invalid userID")
	}
	rows, err := c.repository.ListMachines(ctx, uid)
	if err != nil {
		return nil, err
	}

	// One query for every machine's count, not one per machine: a page with
	// twenty machines should not be twenty-one queries. A failure here loses
	// the counts and keeps the list — a machine list with no numbers on it is
	// still the page somebody asked for.
	counts, err := c.repository.CountLiveSessionsByUser(ctx, uid)
	if err != nil {
		counts = nil
	}

	now := time.Now()
	out := make([]entity.MachineView, 0, len(rows))
	for _, m := range rows {
		out = append(out, toMachineView(m, now, counts[m.ID]))
	}
	return &entity.ListMachinesResponse{Machines: out}, nil
}

func (c *controller) GetMachine(ctx context.Context, req entity.GetMachineRequest) (*entity.GetMachineResponse, error) {
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	id := monoflake.IDFromBase62(req.MachineID).Int64()
	if uid == 0 || id == 0 {
		return nil, fmt.Errorf("invalid id")
	}
	m, err := c.repository.GetMachine(ctx, id, uid)
	if err != nil {
		return nil, err
	}
	counts, err := c.repository.CountLiveSessionsByUser(ctx, uid)
	if err != nil {
		counts = nil
	}
	return &entity.GetMachineResponse{Machine: toMachineView(m, time.Now(), counts[m.ID])}, nil
}

// UpdateMachine renames a machine or flips its kill switch.
//
// Only the fields that were sent are touched. Without that, renaming a machine
// would silently re-enable one somebody had deliberately turned off — the kind
// of bug that is discovered by a machine coming back to life.
func (c *controller) UpdateMachine(ctx context.Context, req entity.UpdateMachineRequest) (*entity.UpdateMachineResponse, error) {
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	id := monoflake.IDFromBase62(req.MachineID).Int64()
	if uid == 0 || id == 0 {
		return nil, fmt.Errorf("invalid id")
	}

	m, err := c.repository.GetMachine(ctx, id, uid)
	if err != nil {
		return nil, err
	}
	if req.Name != nil {
		m.Name = trim(*req.Name, 128)
	}
	if req.Enabled != nil {
		m.Enabled = *req.Enabled
	}
	m.UpdatedAt = time.Now()

	updated, err := c.repository.UpdateMachine(ctx, m)
	if err != nil {
		return nil, err
	}
	// No count here: an update is a rename or a kill switch, and the caller
	// has the list page's numbers already.
	return &entity.UpdateMachineResponse{Machine: toMachineView(updated, time.Now(), 0)}, nil
}

// DeleteMachine removes a machine, which is how its token is revoked.
//
// The row going is what makes the token unusable: authentication is a lookup
// by token hash, and there is nothing left to find. Closing any socket it
// still holds is the caller's job — that lives in the process holding the
// registry, not in the database.
func (c *controller) DeleteMachine(ctx context.Context, req entity.DeleteMachineRequest) error {
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	id := monoflake.IDFromBase62(req.MachineID).Int64()
	if uid == 0 || id == 0 {
		return fmt.Errorf("invalid id")
	}
	return c.repository.DeleteMachine(ctx, id, uid)
}
