// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/agentrq/agentrq/daemon/internal/link"
	"github.com/agentrq/agentrq/daemon/internal/localstatus"
	"github.com/agentrq/agentrq/daemon/internal/supervisor"
)

// reportEvery is how often the local status file is rewritten.
//
// Often enough that `agentrqd status` is current, and far enough inside
// [localstatus.Stale] that an idle daemon is never mistaken for a killed one.
const reportEvery = 20 * time.Second

// reportStatus keeps the machine's own record of what is running on it.
//
// Written for the person at the keyboard, not for the server. Somebody whose
// machine is running agents for an account they no longer control must be able
// to see that without asking the account.
func reportStatus(ctx context.Context, dir string, sup *supervisor.Supervisor, links []*link.Link, log *slog.Logger) {
	started := time.Now()

	write := func() {
		live := sup.Live()
		sessions := make([]localstatus.Session, 0, len(live))
		for _, s := range live {
			sessions = append(sessions, localstatus.Session{
				ID:        s.ID,
				Profile:   sup.Profile(s.ID),
				Kind:      string(s.Kind),
				Dir:       s.Dir,
				Workspace: s.Params.Workspace,
				Viewers:   viewersOf(links, s.ID),
			})
		}
		if err := localstatus.Write(dir, localstatus.File{
			PID:       os.Getpid(),
			Version:   version,
			StartedAt: started,
			Sessions:  sessions,
		}); err != nil {
			log.Debug("could not write the local status report", "error", err)
		}
	}

	t := time.NewTicker(reportEvery)
	defer t.Stop()
	write()
	for {
		select {
		case <-ctx.Done():
			// Removed on a clean stop. A report left behind would tell the
			// next person that something is running when nothing is — and a
			// daemon that was *killed* cannot do this, which is exactly why
			// the reader treats an old report as suspect rather than as fact.
			localstatus.Clear(dir)
			return
		case <-t.C:
			write()
		}
	}
}

func viewersOf(links []*link.Link, session uint64) int {
	for _, l := range links {
		if n := l.Viewers(session); n > 0 {
			return n
		}
	}
	return 0
}
