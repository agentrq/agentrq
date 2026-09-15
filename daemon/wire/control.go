// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package wire

import (
	"encoding/json"
	"fmt"
)

// Op names a control message. Control is the only part of the protocol that is
// JSON: it is low-rate, it benefits from being readable in a log, and unlike
// terminal traffic it is not arbitrary bytes.
type Op string

const (
	OpHello           Op = "hello"           // daemon → backend, first message
	OpHeartbeat       Op = "heartbeat"       // daemon → backend, liveness + metrics
	OpStartSession    Op = "startSession"    // backend → daemon
	OpKillSession     Op = "killSession"     // backend → daemon
	OpSessionState    Op = "sessionState"    // daemon → backend
	OpAttach          Op = "attach"          // backend → daemon
	OpDetach          Op = "detach"          // backend → daemon
	OpUpdateAvailable Op = "updateAvailable" // daemon → backend
	OpUpdateNow       Op = "updateNow"       // backend → daemon, the user approved
	OpError           Op = "error"           // either way, always correlated
)

// Control is the envelope every control message shares.
//
// ID correlates a reply with its request. It is set by whichever side is
// asking; a message that is not a reply leaves it empty. Errors carry the ID of
// whatever provoked them, because an uncorrelated error is a log line rather
// than something a caller can act on.
type Control struct {
	ID string `json:"id,omitempty"`
	Op Op     `json:"op"`
	// Body is the op-specific payload, left as raw JSON so this package does
	// not have to know every message shape — and so a daemon can receive an op
	// from a newer backend without failing to parse the envelope.
	Body json.RawMessage `json:"body,omitempty"`
}

// Resize is the payload of a [TypeResize] frame.
type Resize struct {
	Cols uint16 `json:"cols"`
	Rows uint16 `json:"rows"`
}

// Exit is the payload of a [TypeExit] frame.
type Exit struct {
	Code int `json:"code"`
}

// ControlFrame builds a control frame, which by definition has session 0.
func ControlFrame(c Control) (Frame, error) {
	if c.Op == "" {
		return Frame{}, fmt.Errorf("wire: control message needs an op")
	}
	b, err := json.Marshal(c)
	if err != nil {
		return Frame{}, fmt.Errorf("wire: marshal control: %w", err)
	}
	return Frame{Type: TypeControl, Payload: b}, nil
}

// ParseControl reads the control message out of a frame.
func ParseControl(f Frame) (Control, error) {
	if f.Type != TypeControl {
		return Control{}, fmt.Errorf("wire: not a control frame: %s", f.Type)
	}
	var c Control
	if err := json.Unmarshal(f.Payload, &c); err != nil {
		return Control{}, fmt.Errorf("wire: parse control: %w", err)
	}
	if c.Op == "" {
		return Control{}, fmt.Errorf("wire: control message has no op")
	}
	return c, nil
}

// SessionFrame builds a frame that belongs to a session, which by definition
// has a non-zero session id.
func SessionFrame(t Type, sessionID uint64, payload []byte) (Frame, error) {
	f := Frame{Type: t, SessionID: sessionID, Payload: payload}
	if err := f.validate(); err != nil {
		return Frame{}, err
	}
	return f, nil
}
