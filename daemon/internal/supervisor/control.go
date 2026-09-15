// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package supervisor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/agentrq/agentrq/daemon/wire"
)

// Reporter sends a session's state back to the backend.
type Reporter interface {
	ReportSessionState(wire.SessionState) error
}

// Handle acts on a control message from the backend.
//
// Every outcome is reported, including refusals. A start that is rejected and
// says nothing leaves a row sitting in "starting" forever, blocking the
// workspace's next launch — so a refusal travels back as a failed state with
// the reason in it, not as silence.
func (s *Supervisor) Handle(ctx context.Context, profile string, c wire.Control, r Reporter) error {
	switch c.Op {
	case wire.OpStartSession:
		var req wire.StartSession
		if err := json.Unmarshal(c.Body, &req); err != nil {
			return fmt.Errorf("supervisor: parse startSession: %w", err)
		}
		return s.handleStart(ctx, profile, req, r)

	case wire.OpKillSession:
		var req wire.KillSession
		if err := json.Unmarshal(c.Body, &req); err != nil {
			return fmt.Errorf("supervisor: parse killSession: %w", err)
		}
		if err := s.Kill(req.SessionID); err != nil && !errors.Is(err, ErrNoSuchSession) {
			return err
		}
		// Reported even when the session was already gone: the backend asked
		// for it to be dead, and it is.
		return r.ReportSessionState(wire.SessionState{
			SessionID: req.SessionID,
			State:     string(StateKilled),
		})

	default:
		// An op from a newer backend is ignored rather than fatal. Dropping
		// the connection over an unrecognised message would make every new
		// control op a breaking change for daemons in the field.
		return nil
	}
}

func (s *Supervisor) handleStart(ctx context.Context, profile string, req wire.StartSession, r Reporter) error {
	fail := func(err error) error {
		// The reason reaches the person, not just the log. "Working directory
		// does not exist: /srv/app" is actionable; "failed to start" is not.
		if reportErr := r.ReportSessionState(wire.SessionState{
			SessionID: req.SessionID,
			State:     string(StateFailed),
			Error:     err.Error(),
		}); reportErr != nil {
			return errors.Join(err, reportErr)
		}
		// Returning nil: the start failed, but handling it did not. A returned
		// error here would close the socket, which would turn one bad launch
		// into every session on the machine losing its connection.
		return nil
	}

	sess, err := s.Start(ctx, profile, Request{
		ID:   req.SessionID,
		Kind: Kind(req.Kind),
		Params: Params{
			Workspace:  req.Workspace,
			ServerName: req.ServerName,
			Model:      req.Model,
			Agent:      req.Agent,
		},
		Dir:    req.Dir,
		MCPURL: req.MCPURL,
		Cols:   req.Cols,
		Rows:   req.Rows,
	})
	if err != nil {
		return fail(err)
	}

	if err := r.ReportSessionState(wire.SessionState{
		SessionID: req.SessionID,
		State:     string(StateRunning),
	}); err != nil {
		return err
	}

	// Watch for the end and report that too, so a session that dies on its own
	// does not sit in the UI as running forever.
	go func() {
		s.awaitEnd(sess)
		state, code, _ := sess.State()
		_ = r.ReportSessionState(wire.SessionState{
			SessionID: req.SessionID,
			State:     string(state),
			ExitCode:  &code,
		})
	}()
	return nil
}

// awaitEnd blocks until a session reaches a terminal state.
func (s *Supervisor) awaitEnd(sess *Session) {
	tty := sess.PTY()
	if tty == nil {
		return
	}
	// Wait is idempotent and already remembers its answer, so calling it here
	// as well as in reap is safe and is what lets this goroutine block until
	// the process is genuinely finished.
	_, _ = tty.Wait()
}
