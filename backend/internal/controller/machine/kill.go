// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package machine

import (
	"encoding/json"
	"errors"

	"github.com/mustafaturan/monoflake"
	zlog "github.com/rs/zerolog/log"

	"github.com/agentrq/agentrq/daemon/wire"
)

var (
	// ErrNoConnections is KillSession's answer on a server holding no daemon sockets.
	ErrNoConnections = errors.New("machine connections are not available on this server")
	// ErrUnreachable is KillSession's answer when the kill could not be sent.
	ErrUnreachable = errors.New("could not reach that machine")
)

// KillSession asks the daemon holding a session to kill it. It returns once
// the request is on its way, not once the agent is dead: the daemon reports
// that on the session row. r may be nil.
func KillSession(r *Registry, machineID int64, sessionID string) error {
	if r == nil {
		return ErrNoConnections
	}
	if _, err := r.Get(machineID); err != nil {
		return err
	}
	// Neither can fail: a plain struct, and a control message with its op.
	body, _ := json.Marshal(wire.KillSession{
		SessionID: uint64(monoflake.IDFromBase62(sessionID).Int64()),
	})
	frame, _ := wire.ControlFrame(wire.Control{Op: wire.OpKillSession, Body: body})
	if err := r.Send(machineID, frame); err != nil {
		zlog.Error().Err(err).Str("session", sessionID).Msg("[session] could not reach the machine")
		return ErrUnreachable
	}
	return nil
}
