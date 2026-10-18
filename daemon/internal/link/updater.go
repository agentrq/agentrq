// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: Apache-2.0

package link

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

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
//	verify → download → test → *write the note* → stop → swap → restart
//
// Everything before the note is reversible; the rest is the [Restarter]'s.
type Updater struct {
	// ManifestURL is the release feed.
	ManifestURL string

	Client update.Fetcher
	Log    *slog.Logger

	// GOOS and GOARCH pick the artefact. Fields rather than runtime constants
	// so a test can ask for a platform it is not on.
	GOOS, GOARCH string

	// Restarter holds the binary being replaced, the running version, and
	// the handover that brings the agents back.
	Restarter *Restarter

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
	if !update.Newer(u.Restarter.Version, m.Version) {
		return
	}
	// Not verified here, deliberately. This is an offer, and verification is
	// what happens before anything is *installed* — doing it here as well
	// would be a second place that could be got wrong, and a signature that
	// failed now would simply mean no offer, which is what an unreadable feed
	// already does.
	u.available = m.Version
	u.Log.Info("a newer agentrqd is available", "version", m.Version, "running", u.Restarter.Version)

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
	r := u.Restarter
	plan, err := update.Prepare(ctx, u.Client, u.ManifestURL, r.Version, u.GOOS, u.GOARCH, r.BinaryPath)
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

	err = r.handOver(ctx, "update", plan.Version,
		func() error { return update.Swap(r.BinaryPath, plan.Staged) },
		// The binary just installed cannot be executed, which is the case the
		// retained copy exists for.
		func() error { return update.Rollback(r.BinaryPath) })
	if err != nil {
		// A no-op once the swap has moved the staged file into place.
		plan.Abandon()
	}
	return err
}
