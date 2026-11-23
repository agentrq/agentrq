// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package machine

import (
	"encoding/json"

	"github.com/mustafaturan/monoflake"
	zlog "github.com/rs/zerolog/log"

	"github.com/agentrq/agentrq/daemon/wire"
)

// CanRemoveForkDir reports whether a machine is connected here and its daemon
// can delete a fork's folder. r may be nil.
func CanRemoveForkDir(r *Registry, machineID int64) bool {
	if r == nil {
		return false
	}
	if _, err := r.Get(machineID); err != nil {
		return false
	}
	return r.HasCapability(machineID, wire.CapabilityForkCleanup)
}

// RemoveForkDir asks the daemon on a machine to delete a fork's folder. It
// returns once the request is on its way; the daemon names the folder itself
// from the fork's id. r may be nil.
func RemoveForkDir(r *Registry, machineID, forkID int64) error {
	if r == nil {
		return ErrNoConnections
	}
	if _, err := r.Get(machineID); err != nil {
		return err
	}
	// Neither can fail: a plain struct, and a control message with its op.
	body, _ := json.Marshal(wire.RemoveForkDir{ForkID: monoflake.ID(forkID).String()})
	frame, _ := wire.ControlFrame(wire.Control{Op: wire.OpRemoveForkDir, Body: body})
	if err := r.Send(machineID, frame); err != nil {
		zlog.Error().Err(err).Int64("machine", machineID).Msg("[fork] could not reach the machine")
		return ErrUnreachable
	}
	return nil
}
