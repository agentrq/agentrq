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

// sessionRecorder is the slice of the CRUD controller this needs. Narrow so
// the routing can be tested without a database.
type sessionRecorder interface {
	UpdateSessionState(ctx context.Context, req entity.UpdateSessionStateRequest) error
}

// daemonFrames routes a frame arriving from a daemon.
//
// It returns an error only for something that makes the connection itself
// unusable, which is nothing here. A malformed control message or an op from a
// newer daemon is ignored: dropping the socket over one would turn every
// addition to the protocol into a breaking change for daemons already in the
// field, and would take the machine's other sessions down with it.
func daemonFrames(relay *machine.Relay, rec sessionRecorder) func(context.Context, *machine.Session, wire.Frame) error {
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
		if c.Op != wire.OpSessionState {
			// Heartbeats and everything else are handled elsewhere or not yet.
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

// compile-time check that the CRUD controller satisfies the narrow interface.
var _ sessionRecorder = crud.Controller(nil)
