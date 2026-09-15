// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package link

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/agentrq/agentrq/daemon/internal/restore"
	"github.com/agentrq/agentrq/daemon/internal/supervisor"
	"github.com/agentrq/agentrq/daemon/internal/update"
	"github.com/agentrq/agentrq/daemon/wire"
)

// CheckEvery is how often the release feed is read.
//
// Hourly, because the only thing this can do with the answer is offer it: the
// daemon never updates on its own initiative, so checking more often would
// just be more requests to reach the same person with the same button.
const CheckEvery = time.Hour

// Updater replaces the daemon's binary, when and only when somebody asks.
//
// The order below is the whole design, and it is not rearrangeable:
//
//	verify → download → test → *write the note* → kill → swap → restart
//
// The note goes to disk before anything is killed because the process holding
// it in memory is the process about to be replaced. Everything before the note
// is reversible; everything after it is not.
type Updater struct {
	// BinaryPath is what will be replaced.
	BinaryPath string
	// ManifestURL is the release feed.
	ManifestURL string
	// Version is what is running now.
	Version string
	// StateDir is where the restore note is written.
	StateDir string

	Client     update.Fetcher
	Supervisor *supervisor.Supervisor
	Log        *slog.Logger

	// GOOS and GOARCH pick the artefact. Fields rather than runtime constants
	// so a test can ask for a platform it is not on.
	GOOS, GOARCH string
	// Mode is how the daemon comes back. Resolved at construction from the
	// environment the supervisor sets.
	Mode update.Mode
	// Restart is the handover, injected so everything up to it can be tested
	// without a test binary that replaces itself.
	Restart func(update.Mode, string, []string) error

	available string
}

// Available is the version this daemon has found and reported, if any.
func (u *Updater) Available() string { return u.available }

// Check reads the release feed and reports anything newer.
//
// Reporting is all it does. The daemon never updates on its own initiative:
// somebody sees the version in the control panel and decides, because the
// approval means "kill the sessions and update" and only a person can mean
// that.
func (u *Updater) Check(ctx context.Context, conn *Conn) {
	m, err := update.FetchManifest(ctx, u.Client, u.ManifestURL)
	if err != nil {
		u.Log.Debug("could not read the release feed", "error", err)
		return
	}
	if !update.Newer(u.Version, m.Version) {
		return
	}
	// Not verified here, deliberately. This is an offer, and verification is
	// what happens before anything is *installed* — doing it here as well
	// would be a second place that could be got wrong, and a signature that
	// failed now would simply mean no offer, which is what an unreadable feed
	// already does.
	u.available = m.Version
	u.Log.Info("a newer agentrqd is available", "version", m.Version, "running", u.Version)

	if err := conn.Control(wire.Control{Op: wire.OpUpdateAvailable, Body: mustJSON(wire.UpdateAvailable{
		Version: m.Version,
	})}); err != nil {
		u.Log.Debug("could not report the available version", "error", err)
	}
}

// Watch checks periodically for the life of a connection.
func (u *Updater) Watch(ctx context.Context, conn *Conn) {
	t := time.NewTicker(CheckEvery)
	defer t.Stop()
	u.Check(ctx, conn)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			u.Check(ctx, conn)
		}
	}
}

// ErrNotWhatWasApproved means the release moved between the offer and the yes.
var ErrNotWhatWasApproved = errors.New("update: the available release is not the one that was approved")

// Apply does what an approval asked for.
//
// It returns only on failure. On success the process has been replaced or has
// exited for its supervisor to restart.
func (u *Updater) Apply(ctx context.Context, approved string) error {
	plan, err := update.Prepare(ctx, u.Client, u.ManifestURL, u.Version, u.GOOS, u.GOARCH, u.BinaryPath)
	if err != nil {
		return err
	}

	// Somebody agreed to lose their sessions for a particular version. If a
	// newer one appeared between the offer and the yes, they did not agree to
	// that — so it is refused and offered again rather than installed.
	if approved != "" && approved != plan.Version {
		plan.Abandon()
		return fmt.Errorf("%w: approved %s, feed now offers %s", ErrNotWhatWasApproved, approved, plan.Version)
	}

	// The note first. Written to disk before a single session is killed,
	// because the thing that remembers it is the thing being replaced.
	live := u.Supervisor.Live()
	note := restore.File{
		Reason:      "update",
		FromVersion: u.Version,
		Sessions:    make([]restore.Session, 0, len(live)),
	}
	for _, sess := range live {
		note.Sessions = append(note.Sessions, restore.Session{
			ID:         sess.ID,
			Profile:    u.Supervisor.Profile(sess.ID),
			Kind:       string(sess.Kind),
			Dir:        sess.Dir,
			Workspace:  sess.Params.Workspace,
			ServerName: sess.Params.ServerName,
			Model:      sess.Params.Model,
			Agent:      sess.Params.Agent,
			Cols:       sess.Cols,
			Rows:       sess.Rows,
		})
	}
	if err := restore.Write(u.StateDir, note); err != nil {
		// Abandoned rather than pressed on with. An update that kills sessions
		// it has not written down is an update that loses them.
		plan.Abandon()
		return err
	}

	u.Log.Warn("updating agentrqd; every session on this machine is being stopped",
		"from", u.Version, "to", plan.Version, "sessions", len(note.Sessions))

	for _, sess := range live {
		if err := u.Supervisor.Kill(sess.ID); err != nil && !errors.Is(err, supervisor.ErrNoSuchSession) {
			u.Log.Warn("could not stop a session before updating", "session", sess.ID, "error", err)
		}
	}

	if err := update.Swap(u.BinaryPath, plan.Staged); err != nil {
		// Nothing was replaced, but the sessions are gone. The note stays: the
		// next start — this process carrying on, in fact — brings them back.
		return err
	}

	if err := u.Restart(u.Mode, u.BinaryPath, restartArgs()); err != nil {
		// The binary just installed cannot be executed. This is the case the
		// retained copy exists for.
		u.Log.Error("the new binary will not run; rolling back", "error", err)
		if rollbackErr := update.Rollback(u.BinaryPath); rollbackErr != nil {
			return errors.Join(err, rollbackErr)
		}
		return err
	}
	return nil
}
