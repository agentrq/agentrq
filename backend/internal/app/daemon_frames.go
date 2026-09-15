// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package app

import (
	"context"
	"encoding/json"
	"time"

	"github.com/mustafaturan/monoflake"
	zlog "github.com/rs/zerolog/log"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	"github.com/agentrq/agentrq/backend/internal/controller/machine"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/daemon/wire"
)

// daemonRecorder is the slice of the CRUD controller this needs. Narrow so the
// routing can be tested without a database.
type daemonRecorder interface {
	UpdateSessionState(ctx context.Context, req entity.UpdateSessionStateRequest) error
	RecordMachineMetrics(ctx context.Context, req entity.RecordMachineMetricsRequest) error
	ReconcileSessions(ctx context.Context, req entity.ReconcileSessionsRequest) error
}

// daemonFrames routes a frame arriving from a daemon.
//
// It returns an error only for something that makes the connection itself
// unusable, which is nothing here. A malformed control message or an op from a
// newer daemon is ignored: dropping the socket over one would turn every
// addition to the protocol into a breaking change for daemons already in the
// field, and would take the machine's other sessions down with it.
func daemonFrames(relay *machine.Relay, rec daemonRecorder) func(context.Context, *machine.Session, wire.Frame) error {
	return func(ctx context.Context, s *machine.Session, f wire.Frame) error {
		if f.Type != wire.TypeControl {
			// Terminal traffic. Copied to whoever is watching and not read:
			// a backend that parsed the stream would be a backend that could
			// get it wrong.
			relay.FromDaemon(f)
			return nil
		}

		c, err := wire.ParseControl(f)
		if err != nil {
			zlog.Warn().Err(err).Int64("machine_id", s.Identity.MachineID).Msg("[machine] unreadable control frame")
			return nil
		}
		switch c.Op {
		case wire.OpSessionState:
			// Below.
		case wire.OpHello:
			var hello wire.Hello
			if err := json.Unmarshal(c.Body, &hello); err != nil {
				zlog.Warn().Err(err).Int64("machine_id", s.Identity.MachineID).Msg("[machine] unreadable hello")
				return nil
			}
			reconcile(ctx, rec, s.Identity.MachineID, hello.Sessions)
			return nil
		case wire.OpHeartbeat:
			var hb wire.Heartbeat
			if err := json.Unmarshal(c.Body, &hb); err != nil {
				zlog.Warn().Err(err).Int64("machine_id", s.Identity.MachineID).Msg("[machine] unreadable heartbeat")
				return nil
			}
			recordMetrics(ctx, rec, s.Identity.MachineID, hb)
			reconcile(ctx, rec, s.Identity.MachineID, hb.Sessions)
			return nil
		default:
			// An op from a newer daemon. Ignored, not fatal.
			return nil
		}

		var st wire.SessionState
		if err := json.Unmarshal(c.Body, &st); err != nil {
			zlog.Warn().Err(err).Int64("machine_id", s.Identity.MachineID).Msg("[machine] unreadable session state")
			return nil
		}

		req := entity.UpdateSessionStateRequest{
			SessionID: monoflake.ID(int64(st.SessionID)).String(),
			Status:    st.State,
			ExitCode:  st.ExitCode,
			Error:     st.Error,
		}
		// Only a terminal state ends a session. Writing an end time for
		// "running" would make every session look finished the moment it
		// started.
		if machine.SessionTerminal(st.State) {
			now := time.Now()
			req.EndedAt = &now
		}
		if err := rec.UpdateSessionState(ctx, req); err != nil {
			zlog.Warn().Err(err).Int64("machine_id", s.Identity.MachineID).Msg("[machine] could not record session state")
		}
		return nil
	}
}

// recordMetrics stores what a machine said about itself.
//
// The machine id comes from the authenticated socket and never from the
// payload: a daemon may only ever describe itself, and a heartbeat claiming
// another machine's id is the one thing this must not honour.
func recordMetrics(ctx context.Context, rec daemonRecorder, machineID int64, hb wire.Heartbeat) {
	disks := make([]entity.MachineDiskView, 0, len(hb.Disks))
	for _, d := range hb.Disks {
		disks = append(disks, entity.MachineDiskView{Mount: d.Mount, Total: d.Total, Free: d.Free})
	}
	err := rec.RecordMachineMetrics(ctx, entity.RecordMachineMetricsRequest{
		MachineID:    machineID,
		MemTotal:     hb.MemTotal,
		MemAvailable: hb.MemAvailable,
		CPUPercent:   hb.CPUPercent,
		LoadAvg:      hb.LoadAvg,
		UptimeSec:    hb.UptimeSec,
		Disks:        disks,
		ReportedAt:   time.Now(),
	})
	if err != nil {
		zlog.Warn().Err(err).Int64("machine_id", machineID).Msg("[machine] could not record metrics")
	}
}

// reconcile ends the sessions this machine is no longer running.
//
// A daemon that restarted comes back supervising nothing, and the rows it left
// behind would otherwise sit as "running" forever and block the workspace's
// next launch. Scoped to the machine on the socket, so a daemon can only
// correct its own.
func reconcile(ctx context.Context, rec daemonRecorder, machineID int64, running []uint64) {
	ids := make([]int64, 0, len(running))
	for _, id := range running {
		ids = append(ids, int64(id))
	}
	if err := rec.ReconcileSessions(ctx, entity.ReconcileSessionsRequest{
		MachineID: machineID,
		Running:   ids,
	}); err != nil {
		zlog.Warn().Err(err).Int64("machine_id", machineID).Msg("[machine] could not reconcile sessions")
	}
}

// compile-time check that the CRUD controller satisfies the narrow interface.
var _ daemonRecorder = crud.Controller(nil)
