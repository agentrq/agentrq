// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/agentrq/agentrq/daemon/internal/config"
	"github.com/agentrq/agentrq/daemon/internal/link"
	"github.com/agentrq/agentrq/daemon/internal/localstatus"
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
//
// Defaults, not fixed limits: --max-per-profile and --max-per-machine override
// them, because the right numbers are a property of the machine — a laptop and
// a 64-core box are not the same question — and rebuilding the daemon is not a
// reasonable way to answer it.
const (
	sessionsPerProfile = 8
	sessionsPerMachine = 16
)

// sessionCaps validates what was asked for on the command line.
//
// Zero is allowed and means no cap, which is what the supervisor already does
// with it. It is a real answer for a machine somebody owns outright, and
// refusing it here would leave no way to say so. A negative is not an answer
// to anything, and silently reading as "unlimited" is the wrong way to find
// that out.
func sessionCaps(perProfile, perMachine int) (int, int, error) {
	if perProfile < 0 {
		return 0, 0, fmt.Errorf("--max-per-profile cannot be negative: %d", perProfile)
	}
	if perMachine < 0 {
		return 0, 0, fmt.Errorf("--max-per-machine cannot be negative: %d", perMachine)
	}
	return perProfile, perMachine, nil
}

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
	foreground := fs.Bool("foreground", false, "stay attached to this terminal instead of running in the background")
	manifestURL := fs.String("release-feed", DefaultManifestURL, "where to look for newer releases")
	maxPerProfile := fs.Int("max-per-profile", sessionsPerProfile, "most agents one account may run here (0 for no limit)")
	maxPerMachine := fs.Int("max-per-machine", sessionsPerMachine, "most agents this machine will run at once (0 for no limit)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	perProfile, perMachine, err := sessionCaps(*maxPerProfile, *maxPerMachine)
	if err != nil {
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

	if shouldDetach(*foreground, os.Getenv, runtime.GOOS) {
		return startBackground(st.dir, args, time.Now())
	}

	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	sup := supervisor.New(pty.Start, perProfile, perMachine)
	sup.Log = log

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
		l.Restarter, l.Updater = remoteControl(st.dir, log.With("profile", p.ID), sup, *manifestURL)

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

	// Said before any of the work below, because the work below can take
	// seconds. Until this line existed the daemon went out in silence, so a
	// stop it was asked for and a process destroyed outright left identical
	// logs — and "who stopped this" is the first question asked of a daemon
	// that is no longer running. SIGKILL still leaves nothing, and that
	// absence is now an answer rather than an ambiguity.
	log.Info("shutting down", "reason", stopReason(ctx))

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

// shouldDetach reports whether `agentrqd serve` should spawn a background
// child and return the terminal to the caller.
//
// Service managers (systemd, launchd) expect the process they started to stay
// in the foreground so they can track its lifetime, and `--foreground` asks
// for the same thing at a terminal (including the detached child itself).
func shouldDetach(foreground bool, env func(string) string, goos string) bool {
	if foreground {
		return false
	}
	return update.DetectMode(env, goos) != update.ModeSupervisor
}

// backgroundArgs builds the command line for the detached daemon.
//
// `--foreground` goes in `os.Args` of the child so that a later restart or
// update from the control panel (`restartArgs`) replaces the background
// process in place rather than detaching a second time or re-running `enroll`.
func backgroundArgs(args []string) []string {
	out := make([]string, 0, 2+len(args))
	out = append(out, "serve", "--foreground")
	return append(out, args...)
}

// startDetached spawns the background daemon process, swapped out by a test.
var startDetached = runDetached

func runDetached(bin string, args []string, logFile *os.File) (int, error) {
	cmd := exec.Command(bin, args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = detachAttr()
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	pid := cmd.Process.Pid
	_ = cmd.Process.Release()
	return pid, nil
}

// startBackground launches the daemon detached from this terminal, writing its
// logs to `agentrqd.log` inside the config folder.
func startBackground(dir string, args []string, now time.Time) error {
	if f, err := localstatus.Read(dir, now); err == nil {
		fmt.Printf("agentrqd is already running (pid %d).\n", f.PID)
		return nil
	}

	self, err := executable()
	if err != nil {
		return fmt.Errorf("cannot locate this binary: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(self); err == nil {
		self = resolved
	}

	logPath := filepath.Join(dir, "agentrqd.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("open %s: %w", logPath, err)
	}
	defer logFile.Close()

	pid, err := startDetached(self, backgroundArgs(args), logFile)
	if err != nil {
		return fmt.Errorf("cannot start in the background: %w", err)
	}
	fmt.Printf("agentrqd started in the background (pid %d, log: %s)\n", pid, logPath)
	return nil
}

// signalStop sends the stop signal to a running daemon, swapped out by a test.
var signalStop = stopProcess

// stopWait bounds how long `agentrqd stop` waits for the daemon to clear its
// status file after being signalled.
var stopWait = 6 * time.Second

// stopPoll is how often `agentrqd stop` checks whether the status file is gone.
var stopPoll = 50 * time.Millisecond

func cmdStop() error {
	dir, err := configDir()
	if err != nil {
		return err
	}
	return stopDaemon(dir, time.Now())
}

// stopDaemon stops the daemon recorded in `status.json` and waits for it to
// finish shutting down its agents.
func stopDaemon(dir string, now time.Time) error {
	f, err := localstatus.Read(dir, now)
	if errors.Is(err, localstatus.ErrNoDaemon) || errors.Is(err, localstatus.ErrStale) {
		localstatus.Clear(dir)
		fmt.Println("agentrqd is not running.")
		return nil
	}
	if err != nil {
		return err
	}

	if err := signalStop(f.PID); err != nil {
		return fmt.Errorf("cannot stop agentrqd (pid %d): %w", f.PID, err)
	}

	deadline := time.Now().Add(stopWait)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(localstatus.Path(dir)); errors.Is(err, os.ErrNotExist) {
			break
		}
		time.Sleep(stopPoll)
	}
	localstatus.Clear(dir)
	fmt.Printf("Stopped agentrqd (pid %d).\n", f.PID)
	return nil
}

// DefaultManifestURL is the release feed: the signed manifest attached to the
// newest release. agentrq.com/releases/agentrqd.json, the feed before this,
// answers with the website's page, so no daemon reading it ever saw an update.
const DefaultManifestURL = "https://github.com/agentrq/agentrq/releases/latest/download/agentrqd.json"

// remoteControl builds what restarts and updates this daemon from the panel.
func remoteControl(stateDir string, log *slog.Logger, sup *supervisor.Supervisor, manifestURL string) (*link.Restarter, *link.Updater) {
	r := newRestarter(stateDir, log, sup)
	return r, newUpdater(r, log, manifestURL)
}

// executable is os.Executable, swapped out by a test.
var executable = os.Executable

// newRestarter builds what restarts this daemon from the panel, or nothing
// when the binary's own path cannot be resolved — said out loud rather than
// discovered as a silent no-op later.
func newRestarter(stateDir string, log *slog.Logger, sup *supervisor.Supervisor) *link.Restarter {
	self, err := executable()
	if err != nil {
		log.Warn("cannot locate this binary, so it cannot be restarted or updated from the panel", "error", err)
		return nil
	}
	// Symlinks resolved, because replacing a symlink with a binary is not what
	// anybody meant by updating.
	if resolved, err := filepath.EvalSymlinks(self); err == nil {
		self = resolved
	}
	return &link.Restarter{
		BinaryPath: self,
		Version:    version,
		StateDir:   stateDir,
		Supervisor: sup,
		Log:        log,
		Mode:       update.DetectMode(os.Getenv, runtime.GOOS),
		Restart:    update.Restart,
	}
}

// newUpdater builds the self-update machinery, or nothing when there is no
// restarter or this build has no release key: a verification step with
// nothing to verify against is worse than none.
func newUpdater(r *link.Restarter, log *slog.Logger, manifestURL string) *link.Updater {
	if r == nil {
		return nil
	}
	if update.ReleaseKey == "" {
		log.Info("this build has no release key, so it will not update itself; update it by hand")
		return nil
	}
	return &link.Updater{
		ManifestURL: manifestURL,
		Client:      &httpClient{timeout: dialTimeout},
		Log:         log,
		GOOS:        runtime.GOOS,
		GOARCH:      runtime.GOARCH,
		Restarter:   r,
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
