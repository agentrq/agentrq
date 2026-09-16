// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package crud

import (
	"context"
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
}

// toMachineView renders a machine for the API, deriving online from the last
// heartbeat rather than reading a stored flag.
func toMachineView(m model.Machine, now time.Time) entity.MachineView {
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
	}
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

	now := time.Now()
	out := make([]entity.MachineView, 0, len(rows))
	for _, m := range rows {
		out = append(out, toMachineView(m, now))
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
	return &entity.GetMachineResponse{Machine: toMachineView(m, time.Now())}, nil
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
	return &entity.UpdateMachineResponse{Machine: toMachineView(updated, time.Now())}, nil
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
