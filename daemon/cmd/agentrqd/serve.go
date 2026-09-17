// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/agentrq/agentrq/daemon/internal/config"
	"github.com/agentrq/agentrq/daemon/internal/link"
	"github.com/agentrq/agentrq/daemon/internal/metrics"
	"github.com/agentrq/agentrq/daemon/internal/pty"
	"github.com/agentrq/agentrq/daemon/internal/supervisor"
	"github.com/agentrq/agentrq/daemon/internal/update"
)

// Capacity limits, per profile and for the machine as a whole.
//
// Two numbers rather than one because they answer different questions: the
// per-profile cap stops one account filling a shared machine, and the machine
// cap is what stops the machine falling over. Either alone leaves the other
// case open.
const (
	sessionsPerProfile = 4
	sessionsPerMachine = 8
)

// dialTimeout bounds one connection attempt.
const dialTimeout = 30 * time.Second

// shutdownGrace is how long the daemon waits for its agents to go.
//
// Long enough for a process that handles the hang-up to write out whatever it
// was doing, short enough that stopping the service does not feel broken.
const shutdownGrace = 5 * time.Second

// reportGrace is how long the connections are held open afterwards, so the
// backend hears that the sessions ended rather than only that the machine went.
const reportGrace = 300 * time.Millisecond

// cmdServe connects every enrolled profile and stays connected.
//
// One supervisor across all of them, deliberately: the machine-wide cap is a
// property of the machine, and a supervisor per profile could not enforce it.
func cmdServe(ctx context.Context, args []string) error {
	fs := newFlags("serve")
	only := fs.String("profile", "", "connect only this profile (default: all of them)")
	verbose := fs.Bool("verbose", false, "log every frame decision")
	manifestURL := fs.String("release-feed", DefaultManifestURL, "where to look for newer releases")
	if err := fs.Parse(args); err != nil {
		return err
	}

	st, err := stores()
	if err != nil {
		return err
	}
	file, err := st.load()
	if err != nil {
		return err
	}

	profiles := file.Profiles
	if *only != "" {
		p, err := file.Find(*only)
		if err != nil {
			return err
		}
		profiles = []config.Profile{p}
	}
	if len(profiles) == 0 {
		return fmt.Errorf("no profiles are enrolled — run `agentrqd enroll` first")
	}

	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	sup := supervisor.New(pty.Start, sessionsPerProfile, sessionsPerMachine)

	// One collector for the machine, shared by every profile. The numbers are
	// the same hardware whichever account is asking, and measuring once per
	// profile would make the measurement itself part of the load.
	//
	// It leaks nothing new: each account already knows this machine exists,
	// having enrolled it.
	collector := metrics.New(func() []string { return sup.Dirs() })
	go collector.Run(ctx)

	// What an update stopped, if anything. Read once, at start, and removed as
	// it is read: a note left behind would start somebody's agents again on
	// every subsequent start, forever.
	pending := link.Restored(ctx, st.dir, sup, log)

	// The connections get a context of their own so the sessions can be
	// stopped, and their exits reported, while the sockets are still up. Using
	// the signal's context for both would close the connections first and the
	// backend would never hear what happened to the agents.
	linkCtx, stopLinks := context.WithCancel(context.Background())
	defer stopLinks()

	var wg sync.WaitGroup
	links := make([]*link.Link, 0, len(profiles))
	started := 0
	for _, p := range profiles {
		token, err := st.tokens.Get(p.ID)
		if err != nil {
			// One unreadable token must not stop the other profiles. A machine
			// enrolled with two accounts, one of whose credential has been
			// removed, should still serve the other.
			log.Error("cannot read the token for a profile; skipping it",
				"profile", p.ID, "error", err, "tokens", st.tokens.Describe())
			continue
		}
		url, err := link.SocketURL(p.ServerURL)
		if err != nil {
			log.Error("unusable server url; skipping this profile", "profile", p.ID, "error", err)
			continue
		}
		if w := config.InsecureWarning(p); w != "" {
			// Every start, not just enrolment. A warning printed once, months
			// ago, is not a safeguard.
			fmt.Fprintf(os.Stderr, "%s\n", w)
		}

		l := link.New(p.ID, url, link.Identity{
			MachineID: p.MachineID,
			UserID:    p.UserID,
			Token:     token,
			Version:   version,
		}, &dialer{}, sup, log.With("profile", p.ID))
		l.Metrics = collector.Snapshot
		l.Pending = pending
		l.Updater = newUpdater(p.ID, st.dir, log.With("profile", p.ID), sup, *manifestURL)

		links = append(links, l)
		started++
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = l.Run(linkCtx)
		}()
	}

	if started == 0 {
		return fmt.Errorf("no profile could be connected")
	}
	log.Info("agentrqd running", "profiles", started, "version", version)

	// The machine's own record of what is running on it, for whoever is
	// sitting at it. `agentrqd status` reads this.
	go reportStatus(ctx, st.dir, sup, links, log)

	<-ctx.Done()

	// The agents go with the daemon, on purpose.
	//
	// They would mostly go anyway: closing a pseudo-terminal hangs up on the
	// process using it. But "mostly" is not a design, and an agent that
	// survives is one nothing can reach afterwards — it is not listed, it
	// cannot be stopped, and the next daemon does not adopt it, so the panel
	// shows an idle machine while a process keeps working against the
	// workspace with the credential still in its folder.
	//
	// Done before the connections close, so the backend hears about each of
	// them and the sessions do not linger in the panel as running.
	if stopped := sup.StopAll(context.Background(), shutdownGrace); len(stopped) > 0 {
		log.Warn("stopped the agents running on this machine", "count", len(stopped))
		// A moment for the state reports to reach the backend. They are on
		// their way already; this is only the difference between the panel
		// being right immediately and being right when the daemon next
		// connects.
		time.Sleep(reportGrace)
	}

	stopLinks()
	wg.Wait()
	log.Info("agentrqd stopped")
	return nil
}

// DefaultManifestURL is the release feed.
const DefaultManifestURL = "https://agentrq.com/releases/agentrqd.json"

// newUpdater builds the self-update machinery, or nothing.
//
// Nothing when this build has no release key, when the binary's own path
// cannot be resolved, or when the feed is not https — each of which is a
// reason this daemon must not replace itself, and each of which is said out
// loud rather than discovered as a silent no-op later.
func newUpdater(profile, stateDir string, log *slog.Logger, sup *supervisor.Supervisor, manifestURL string) *link.Updater {
	if update.ReleaseKey == "" {
		log.Info("this build has no release key, so it will not update itself; update it by hand")
		return nil
	}
	self, err := os.Executable()
	if err != nil {
		log.Warn("cannot locate this binary, so it will not update itself", "error", err)
		return nil
	}
	// Symlinks resolved, because replacing a symlink with a binary is not what
	// anybody meant by updating.
	if resolved, err := filepath.EvalSymlinks(self); err == nil {
		self = resolved
	}

	return &link.Updater{
		BinaryPath:  self,
		ManifestURL: manifestURL,
		Version:     version,
		StateDir:    stateDir,
		Client:      &httpClient{timeout: dialTimeout},
		Supervisor:  sup,
		Log:         log,
		GOOS:        runtime.GOOS,
		GOARCH:      runtime.GOARCH,
		Mode:        update.DetectMode(os.Getenv, runtime.GOOS),
		Restart:     update.Restart,
	}
}

// dialer is the real WebSocket dialer.
//
// Certificate verification is never disabled here, and there is no flag that
// would. --insecure permits plain HTTP to a host that asked for it, which the
// URL already records; it does not mean "and stop checking certificates".
type dialer struct{}

func (dialer) Dial(url string, h http.Header) (*websocket.Conn, *http.Response, error) {
	d := &websocket.Dialer{
		HandshakeTimeout: dialTimeout,
		ReadBufferSize:   4096,
		WriteBufferSize:  4096,
	}
	return d.Dial(url, h)
}

// cmdRollback puts the previous binary back.
//
// The manual half of "never auto-update on a failed start". The daemon refuses
// to install a binary that does not run here, but nothing inside a process can
// undo an update that installed cleanly and then misbehaved — so this exists
// for the person who has to fix that, and it needs no working daemon to run.
func cmdRollback() error {
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot locate this binary: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(self); err == nil {
		self = resolved
	}
	if err := update.Rollback(self); err != nil {
		return err
	}
	fmt.Printf("Rolled back to the previous agentrqd at %s\n", self)
	fmt.Printf("The version that was replaced is kept at %s.failed\n", self)
	fmt.Println("Restart the daemon to run it.")
	return nil
}
