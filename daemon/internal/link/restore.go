// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package link

import (
	"context"
	"log/slog"
	"time"

	"github.com/agentrq/agentrq/daemon/internal/restore"
	"github.com/agentrq/agentrq/daemon/internal/supervisor"
	"github.com/agentrq/agentrq/daemon/wire"
)

// Restored starts again what an update stopped.
//
// **What comes back is intent, not state.** These are new processes with new
// pseudo-terminals: same kind, same folder, same arguments. The scrollback is
// gone, whatever the agent was part-way through is gone, and anything
// half-typed is gone. Every session that comes back this way is marked
// restored, in the report and therefore in the panel, so nobody is left
// wondering why their terminal is empty.
//
// Best effort and bounded: a session that does not come back is reported
// failed, once, rather than retried until somebody notices.
func Restored(ctx context.Context, dir string, sup *supervisor.Supervisor, log *slog.Logger) []restore.Session {
	note, err := restore.Take(dir, time.Now())
	if err != nil {
		// Including a stale note, which is deliberately not acted on: an
		// update takes seconds, and starting somebody's agents from an
		// hour-old note is a surprise rather than a restoration.
		log.Warn("not restoring sessions", "error", err)
		return nil
	}
	if len(note.Sessions) == 0 {
		return nil
	}

	log.Warn("restoring sessions after an update; their scrollback and in-flight work are gone",
		"count", len(note.Sessions), "from", note.FromVersion)
	return note.Sessions
}

// Restore starts one recorded session again and reports what happened.
//
// The MCP URL is not in the note — the credential is inside it — so a kind
// that needs one cannot be restored from disk alone and is reported failed
// with that reason. Saying so is the point: a session silently missing is
// worse than one that says why it is not there.
func (l *Link) Restore(ctx context.Context, conn *Conn, s restore.Session) {
	if s.Profile != l.Profile {
		return
	}

	report := func(state supervisor.State, reason string) {
		_ = conn.Control(wire.Control{Op: wire.OpSessionState, Body: mustJSON(wire.SessionState{
			SessionID: s.ID,
			State:     string(state),
			Error:     reason,
			Restored:  true,
		})})
	}

	kind := supervisor.Kind(s.Kind)
	cmd, err := supervisor.Resolve(kind, supervisor.Params{
		Workspace:  s.Workspace,
		ServerName: s.ServerName,
		Model:      s.Model,
		Agent:      s.Agent,
	})
	if err != nil {
		l.Log.Warn("cannot restore a session", "session", s.ID, "error", err)
		report(supervisor.StateFailed, err.Error())
		return
	}
	// The credential is not in the note, so a kind that needs one reads the
	// config written into its folder when it was first launched. Nothing new
	// is written to disk here: this reuses what was already there.
	sess, err := l.Supervisor.Start(ctx, l.Profile, supervisor.Request{
		ID:   s.ID,
		Kind: kind,
		Params: supervisor.Params{
			Workspace:  s.Workspace,
			ServerName: s.ServerName,
			Model:      s.Model,
			Agent:      s.Agent,
		},
		Dir:            s.Dir,
		ReuseMCPConfig: cmd.NeedsMCPConfig,
		Cols:           s.Cols,
		Rows:           s.Rows,
	})
	if err != nil {
		l.Log.Warn("a session did not come back", "session", s.ID, "error", err)
		report(supervisor.StateFailed, err.Error())
		return
	}

	report(supervisor.StateRunning, "")

	if tty := sess.PTY(); tty != nil {
		cols, rows := s.Cols, s.Rows
		if cols == 0 || rows == 0 {
			cols, rows = 80, 24
		}
		l.streams.add(s.ID, cols, rows, tty, conn)
	}
}
