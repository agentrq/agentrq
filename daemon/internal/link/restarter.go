// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package link

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/agentrq/agentrq/daemon/internal/restore"
	"github.com/agentrq/agentrq/daemon/internal/supervisor"
	"github.com/agentrq/agentrq/daemon/internal/update"
)

// HandOverGrace is how long the agents get to go before the daemon restarts.
const HandOverGrace = 5 * time.Second

// ErrNotRestarted means the agents were stopped and the daemon did not
// restart, so this process has to start them again itself.
var ErrNotRestarted = errors.New("the daemon stopped its agents and could not restart")

// Restarter stops this daemon and starts it again, agents included.
//
//	write the note → stop the agents → install (an update only) → restart
//
// The note goes to disk before anything is stopped because the process
// holding it in memory is the process about to be replaced.
type Restarter struct {
	// BinaryPath is what is started again.
	BinaryPath string
	// Version is what is running now.
	Version string
	// StateDir is where the restore note is written.
	StateDir string

	Supervisor *supervisor.Supervisor
	Log        *slog.Logger

	// Mode is how the daemon comes back, resolved at construction from the
	// environment the supervisor sets.
	Mode update.Mode
	// Restart is the handover, injected so everything up to it can be tested
	// without a test binary that replaces itself.
	Restart func(update.Mode, string, []string) error
	// Grace bounds the wait for the agents to go; zero is HandOverGrace.
	Grace time.Duration
}

// Now restarts the daemon as it is. It returns only on failure.
func (r *Restarter) Now(ctx context.Context) error {
	return r.handOver(ctx, "restart", "", nil, nil)
}

// handOver does the part an update and a restart share. install runs once the
// agents have gone and undo if the restart then fails; both are nil for a
// restart.
func (r *Restarter) handOver(ctx context.Context, reason, to string, install, undo func() error) error {
	live := r.Supervisor.Live()
	note := restore.File{
		Reason:      reason,
		FromVersion: r.Version,
		Sessions:    make([]restore.Session, 0, len(live)),
	}
	for _, sess := range live {
		note.Sessions = append(note.Sessions, restore.Session{
			ID:         sess.ID,
			Profile:    r.Supervisor.Profile(sess.ID),
			Kind:       string(sess.Kind),
			Dir:        sess.Dir,
			Workspace:  sess.Params.Workspace,
			ServerName: sess.Params.ServerName,
			Model:      sess.Params.Model,
			Effort:     sess.Params.Effort,
			Agent:      sess.Params.Agent,
			Cols:       sess.Cols,
			Rows:       sess.Rows,
		})
	}
	if err := restore.Write(r.StateDir, note); err != nil {
		// Refused rather than pressed on with: stopping sessions that have
		// not been written down loses them.
		return err
	}

	r.Log.Warn("restarting agentrqd; its sessions are stopped and started again",
		"reason", reason, "from", r.Version, "to", to, "sessions", len(note.Sessions))

	grace := r.Grace
	if grace <= 0 {
		grace = HandOverGrace
	}
	r.Supervisor.HandOver(ctx, grace)

	if install != nil {
		if err := install(); err != nil {
			return errors.Join(ErrNotRestarted, err)
		}
	}
	if err := r.Restart(r.Mode, r.BinaryPath, restartArgs()); err != nil {
		r.Log.Error("the daemon could not restart", "error", err)
		if undo != nil {
			if undoErr := undo(); undoErr != nil {
				return errors.Join(ErrNotRestarted, err, undoErr)
			}
		}
		return errors.Join(ErrNotRestarted, err)
	}
	return nil
}
