// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package supervisor

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/agentrq/agentrq/daemon/wire"
)

// ErrSessionNotRunning is returned for input aimed at a session that has
// finished. Distinguished from "no such session" because they mean different
// things to a person: one terminal has closed, the other never existed.
var ErrSessionNotRunning = errors.New("supervisor: session is not running")

// HandleFrame acts on a session frame from the backend.
//
// Only input and resize arrive this way. Output, replay and exit travel the
// other direction, and a backend sending one of those would be a backend that
// has misunderstood the protocol — so they are refused rather than ignored.
func (s *Supervisor) HandleFrame(f wire.Frame) error {
	switch f.Type {
	case wire.TypeInput:
		return s.writeInput(f.SessionID, f.Payload)
	case wire.TypeResize:
		var r wire.Resize
		if err := json.Unmarshal(f.Payload, &r); err != nil {
			return fmt.Errorf("supervisor: parse resize: %w", err)
		}
		return s.Resize(f.SessionID, r.Cols, r.Rows)
	default:
		return fmt.Errorf("supervisor: %s is not a frame the daemon accepts", f.Type)
	}
}

// writeInput puts bytes into a session's terminal.
//
// Exactly the bytes that arrived, with no interpretation whatsoever. Esc is
// 0x1b and gets no special handling — that is the point. Any layer that
// enumerated "special keys" would already be wrong for the next one, and a
// transparent byte pipe is correct for all of them, including the ones nobody
// has thought about.
func (s *Supervisor) writeInput(id uint64, b []byte) error {
	if len(b) == 0 {
		return nil
	}
	tty, err := s.runningTTY(id)
	if err != nil {
		return err
	}
	_, err = tty.Write(b)
	return err
}

// Resize tells a session's terminal its window changed.
//
// On Unix this becomes TIOCSWINSZ and the process is signalled. Windows has no
// SIGWINCH — ConPTY notifies the program through its own channel — which is
// why this goes through the platform layer rather than trying to send a signal
// itself.
func (s *Supervisor) Resize(id uint64, cols, rows uint16) error {
	if cols == 0 || rows == 0 {
		return fmt.Errorf("supervisor: %dx%d is not a window", cols, rows)
	}
	tty, err := s.runningTTY(id)
	if err != nil {
		return err
	}
	return tty.Resize(cols, rows)
}

// runningTTY returns a session's terminal, if it is still running.
func (s *Supervisor) runningTTY(id uint64) (ttyWriter, error) {
	sess, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	state, _, _ := sess.State()
	if state.Terminal() {
		// Typing into a terminal whose process has gone is not an error worth
		// closing a connection over — the person's browser simply has not
		// caught up yet — but it must not be reported as delivered.
		return nil, fmt.Errorf("%w: %d is %s", ErrSessionNotRunning, id, state)
	}
	tty := sess.PTY()
	if tty == nil {
		return nil, fmt.Errorf("%w: %d has no terminal", ErrSessionNotRunning, id)
	}
	return tty, nil
}

// ttyWriter is the part of a terminal input needs.
type ttyWriter interface {
	Write([]byte) (int, error)
	Resize(cols, rows uint16) error
}
