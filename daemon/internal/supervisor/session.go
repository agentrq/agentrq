// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package supervisor

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/agentrq/agentrq/daemon/internal/pty"
	"github.com/agentrq/agentrq/daemon/wire"
)

// State is where a session is in its life.
type State string

const (
	StateStarting State = "starting"
	StateRunning  State = "running"
	StateExited   State = "exited"
	StateKilled   State = "killed"
	StateFailed   State = "failed"
)

// Terminal reports whether a state is final. Anything else may still change.
func (s State) Terminal() bool {
	return s == StateExited || s == StateKilled || s == StateFailed
}

// Request is what the backend asks for.
//
// A kind and validated parameters. Never an argv, never an environment, never
// a working directory the daemon has not checked.
type Request struct {
	ID     uint64
	Kind   Kind
	Params Params
	// Dir is the workspace's folder on this machine.
	Dir string
	// MCPURL points the agent at its workspace. The credential is inside it.
	MCPURL string
	// CoreMCPURL adds the account-wide server, for the workspace the backend
	// judged to be the supervisor. Empty for every other workspace, and that
	// emptiness is the whole decision: this daemon does not work out which
	// workspace is which.
	CoreMCPURL string
	// ReuseMCPConfig says this session is being restored after the daemon
	// replaced itself, and its config is already in the folder.
	//
	// The note that survives a restart carries no MCP URL on purpose — the
	// credential is inside it — so a restored agent reads the file that was
	// written when it was first launched. If that file is not there, the
	// session fails with a reason rather than starting an agent that cannot
	// reach its workspace and looks merely broken.
	ReuseMCPConfig bool
	// Fork runs a workspace fork in a folder of its own, made from Fork.From;
	// Dir is ignored then. Nil for every other workspace.
	Fork *wire.ForkSpec
	// Progress shows the launch's notices as they happen, and what making a
	// fork's folder prints, before the agent has a terminal. Nil keeps the
	// notices for [Session.Notices] only.
	Progress Progress
	// Resume picks a restored claude-code session's conversation back up,
	// when there is one saved.
	Resume bool

	Cols uint16
	Rows uint16
}

// Session is one running agent.
type Session struct {
	ID   uint64
	Kind Kind
	// Dir is the workspace folder this session runs in. Kept so the machine
	// can report the free space on the filesystem that actually matters.
	Dir string
	// Params are the validated arguments this session was started with, kept
	// so it can be started again after the daemon replaces itself.
	//
	// The MCP URL is deliberately *not* kept: the credential is inside it, and
	// holding it for the life of a session so it could be written to a file
	// later would turn a short-lived, scoped token into a long-lived one. The
	// backend issues a fresh one when it asks for the session again.
	Params Params
	Cols   uint16
	Rows   uint16

	mu       sync.RWMutex
	state    State
	exitCode int
	err      error
	// notices are things the person launching this needs told: how a fork's
	// folder was made, a folder whose .mcp.json already named one of our
	// servers and was kept as it was, and the command the agent runs as.
	//
	// Kept on the session rather than only logged because the daemon's log is
	// on the machine and the person is not: these are put into the terminal
	// the moment it is being streamed, which is the one surface the panel
	// already shows. A notice never carries a URL, since one of them holds a
	// token.
	notices []string
	// progress is where a notice is also shown as it is added; nil for none.
	// Set when the session is made and never changed.
	progress Progress
	// cancelPrepare stops a fork's folder being made, for a kill that
	// arrives meanwhile.
	cancelPrepare context.CancelFunc
	// endedAt is when this session reached a terminal state, and is zero
	// until it does. Kept so a finished session can be dropped once nobody
	// is going to ask about it.
	endedAt time.Time
	// handedOver marks a session stopped by [Supervisor.HandOver], to be
	// started again by the next daemon. Its end is not news to the backend.
	handedOver bool

	// ended closes once the session has reached a terminal state *and* that
	// state has been recorded.
	//
	// Waiting on the pseudo-terminal is not enough, and that difference is the
	// whole reason this exists: two goroutines wake on the same exit, and a
	// reader that only waited for the process would race the writer that
	// records what happened — and report a dead session as running.
	ended chan struct{}

	tty pty.Session
}

// Starter opens a pseudo-terminal. Injected so the supervisor is testable
// without one; the real implementation is pty.Start.
type Starter func(ctx context.Context, spec pty.Spec) (pty.Session, error)

// Errors from supervising sessions.
var (
	ErrAtCapacity    = errors.New("supervisor: too many sessions running")
	ErrNoSuchSession = errors.New("supervisor: no such session")
	ErrAlreadyExists = errors.New("supervisor: session already exists")
	// ErrNoMCPConfig means a restored session's folder no longer has the
	// config it was going to read. Relaunching it from the panel writes a
	// fresh one; this cannot, because it has no credential to write.
	ErrNoMCPConfig = errors.New("supervisor: the config a restored session needs is not there")
	// ErrStoppedWhileStarting means a kill arrived while a fork's folder was
	// still being made, so the agent was never started.
	ErrStoppedWhileStarting = errors.New("supervisor: the session was stopped before it started")
)

// KillGrace is how long a process gets to leave politely.
//
// Long enough for an agent to finish writing a file it had open; short enough
// that a person killing a runaway session sees something happen.
const KillGrace = 5 * time.Second

// FinishedRetention is how long a finished session can still be asked about.
//
// It stays in the map after it exits so that "what happened to it" has an
// answer — but nothing in the daemon calls Forget, so without a window the
// map is where every session a machine has ever run accumulates for the life
// of the process. Long enough to cover the exit report and a viewer still
// looking at the terminal it happened in.
const FinishedRetention = 5 * time.Minute

// Supervisor owns every session on this machine.
type Supervisor struct {
	start Starter

	// Log is where this reports what it decided but was not asked about —
	// notably a folder whose .mcp.json already named a server, which is left
	// as it was. A field rather than a constructor argument because every
	// caller but one is a test that does not care, and nil is the daemon's
	// default logger.
	Log *slog.Logger

	// PerProfile caps one account. WholeMachine caps the box.
	//
	// The second is the one that protects anything. CPU and memory are
	// physical and shared, and two accounts that cannot see each other will
	// otherwise exhaust a machine neither believes it is overloading.
	perProfile   int
	wholeMachine int

	// Home is where the forks folder lives; empty means the user's home.
	Home string
	// RemoveDir deletes a fork's folder; nil is [RemoveForkDir]. A field so
	// a test can hold a removal open.
	RemoveDir func(home, forkID string) error
	// PrepareDir makes a fork's folder; nil is [PrepareForkDir]. A field for
	// the same reason.
	PrepareDir func(ctx context.Context, home, from, forkID string, out Progress) (dir string, err error)
	// ClaudeDir is where claude-code keeps its conversations; empty means
	// $CLAUDE_CONFIG_DIR, or ~/.claude.
	ClaudeDir string

	// finishedRetention is how long a finished session stays answerable.
	// A field rather than a constant so a test need not wait out the window.
	finishedRetention time.Duration

	mu       sync.Mutex
	sessions map[uint64]*Session
	profiles map[uint64]string // session id → profile
	// returning are the sessions a handover stopped and a restart will start
	// again. [Supervisor.Running] still names them, or a heartbeat between
	// the stop and the restart would have the backend delete their rows.
	returning map[uint64]bool
}

// New makes a supervisor.
func New(start Starter, perProfile, wholeMachine int) *Supervisor {
	return &Supervisor{
		start:             start,
		perProfile:        perProfile,
		wholeMachine:      wholeMachine,
		finishedRetention: FinishedRetention,
		sessions:          map[uint64]*Session{},
		profiles:          map[uint64]string{},
		returning:         map[uint64]bool{},
	}
}

// log is the supervisor's logger, or the daemon's default one.
func (s *Supervisor) log() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

// home is where the forks folder lives.
func (s *Supervisor) home() (string, error) {
	if s.Home != "" {
		return s.Home, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("supervisor: no home folder for the fork: %w", err)
	}
	return home, nil
}

// workspaceDir decides whether a folder is one this daemon will write into,
// and returns the path it will use.
//
// Checked here rather than left to the pty layer because by the time the
// process starts, three files have already been written into that folder. The
// backend names it, so this is the boundary where a name becomes a path:
//
//   - Absolute only. A relative path resolves against the daemon's own
//     working directory, so "projects/app" would put a workspace's config
//     next to the daemon rather than in the folder somebody chose, and never
//     say so. It is refused rather than resolved, because guessing which of
//     the two was meant is exactly the kind of guess that goes unnoticed.
//   - Cleaned, so one folder has one spelling however the path was typed.
//   - It has to already exist and be a directory. The daemon runs an agent in
//     a folder that is there; it does not conjure one, and a typo in the
//     workspace's setting should say so rather than quietly become a new
//     empty directory with a token in it.
//
// The pty layer checks the folder too, and still should: it is the one that
// has to be right about the directory the process actually starts in.
func workspaceDir(dir string) (string, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return "", fmt.Errorf("%w: dir", ErrMissingParam)
	}
	if !filepath.IsAbs(dir) {
		return "", fmt.Errorf("%w: dir=%q is not an absolute path", ErrBadParameter, dir)
	}
	clean := filepath.Clean(dir)

	info, err := os.Stat(clean)
	if errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("%w: %s", pty.ErrDirMissing, clean)
	}
	if err != nil {
		return "", fmt.Errorf("supervisor: working directory %s: %w", clean, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%w: %s", pty.ErrDirNotDir, clean)
	}
	return clean, nil
}

// Start launches an agent.
//
// The order matters and is deliberate: resolve the command, check the caps,
// write the MCP config, then spawn. Everything that can be refused is refused
// before a process exists, so a rejected request leaves nothing behind.
func (s *Supervisor) Start(ctx context.Context, profile string, req Request) (*Session, error) {
	cmd, err := Resolve(req.Kind, req.Params)
	if err != nil {
		return nil, err
	}
	// A fork's folder is made after the reservation below, not before: it can
	// take a minute, and a kill that arrives meanwhile has to find the session.
	var home string
	if req.Fork != nil {
		if home, err = s.home(); err != nil {
			return nil, err
		}
	} else {
		dir, err := workspaceDir(req.Dir)
		if err != nil {
			return nil, err
		}
		req.Dir = dir
	}

	s.mu.Lock()
	s.pruneFinishedLocked(time.Now())
	if _, taken := s.sessions[req.ID]; taken {
		s.mu.Unlock()
		return nil, fmt.Errorf("%w: %d", ErrAlreadyExists, req.ID)
	}
	if err := s.checkCapacityLocked(profile); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	// Reserved before the slow work below, so two concurrent starts cannot
	// both pass the capacity check.
	//
	// The folder outlives the socket that asked for it, as the agent does.
	prepCtx, cancelPrepare := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelPrepare()
	sess := &Session{
		ID: req.ID, Kind: req.Kind, Dir: req.Dir,
		Params: req.Params, Cols: req.Cols, Rows: req.Rows,
		state: StateStarting, ended: make(chan struct{}),
		progress: req.Progress, cancelPrepare: cancelPrepare,
	}
	s.sessions[req.ID] = sess
	s.profiles[req.ID] = profile
	delete(s.returning, req.ID)
	s.mu.Unlock()

	// From here, any failure has to release the reservation.
	release := func() {
		s.mu.Lock()
		delete(s.sessions, req.ID)
		delete(s.profiles, req.ID)
		s.mu.Unlock()
	}

	if req.Fork != nil {
		prepare := s.PrepareDir
		if prepare == nil {
			prepare = PrepareForkDir
		}
		dir, err := prepare(prepCtx, home, req.Fork.From, req.Fork.ID, launchProgress{sess})
		if err == nil {
			dir, err = workspaceDir(dir)
		}
		// Asked first: the kill cancels the checkout, and the git it kills
		// fails, which would otherwise be reported as the reason.
		if state, _, _ := sess.State(); state == StateKilled {
			release()
			return nil, ErrStoppedWhileStarting
		}
		if err != nil {
			release()
			return nil, err
		}
		s.mu.Lock()
		sess.Dir = dir
		s.mu.Unlock()
		req.Dir = dir
	}

	servers := []MCPEntry{{Name: req.Params.ServerName, URL: req.MCPURL}}
	if req.CoreMCPURL != "" {
		servers = append(servers, MCPEntry{Name: wire.CoreMCPServerName, URL: req.CoreMCPURL})
	}

	if cmd.NeedsMCPConfig {
		switch {
		case req.ReuseMCPConfig:
			if !HasMCPConfig(req.Dir, req.Params.ServerName) {
				release()
				return nil, fmt.Errorf("%w: %s has no %s entry for %q", ErrNoMCPConfig,
					req.Dir, MCPConfigName, req.Params.ServerName)
			}
		default:
			// Excluded before it is written, never after. A folder that is a
			// git checkout would otherwise hold a file with a live token in it
			// for as long as it takes to get to the next line — and a commit
			// made in that window cannot be unmade once it is pushed.
			//
			// This file only. The permissions file written below carries no
			// credential, and whether it is checked in is the repository's
			// decision.
			if _, err := EnsureGitIgnored(req.Dir, MCPConfigName); err != nil {
				release()
				return nil, err
			}
			path, kept, err := WriteMCPConfig(req.Dir, servers...)
			if err != nil {
				release()
				return nil, err
			}
			if req.Fork != nil && slices.Contains(kept, req.Params.ServerName) &&
				configPointsElsewhere(path, req.Params.ServerName, req.MCPURL) {
				release()
				return nil, fmt.Errorf("%w: %s already has a %q entry, which would connect this fork as its parent workspace; remove it from %s",
					ErrForkConfigCollision, path, req.Params.ServerName, MCPConfigName)
			}
			if len(kept) > 0 {
				// Said out loud rather than assumed: the agent is about to
				// talk to whatever those entries point at, which is not
				// necessarily what this launch was for.
				//
				// Both places on purpose. The log is for whoever is on the
				// machine; the notice reaches the person who pressed the
				// button, who can see only the terminal.
				s.log().Info("kept the MCP servers this folder already configured",
					"session", req.ID, "file", path, "servers", strings.Join(kept, ", "))
				sess.addNotice(fmt.Sprintf(
					"%s already configured %s — kept as it is, and this launch's own settings were not written over it.",
					MCPConfigName, strings.Join(kept, " and ")))
			}
		}
	}

	// The permissions file is written for a fresh launch only. A restored
	// session's folder already has the one written when it was first launched,
	// and it holds no credential to go stale — unlike the MCP config, there is
	// nothing here a restart could invalidate.
	if cmd.NeedsClaudeSettings && !req.ReuseMCPConfig {
		names := make([]string, 0, len(servers))
		for _, srv := range servers {
			names = append(names, srv.Name)
		}
		if _, err := WriteClaudeSettings(req.Dir, names...); err != nil {
			release()
			return nil, err
		}
	}

	// The agent outlives the request that asked for it, so the context that
	// asked must not own it.
	//
	// `ctx` here is the daemon's *connection* to the backend, cancelled every
	// time that socket drops — a backend deploy, a network blink, a machine
	// disabled and re-enabled. The pty layer binds the process to the context
	// it is given, so passing this one straight through meant every agent on
	// the machine was killed whenever the daemon reconnected, which is the
	// opposite of what reconnecting is for. Measured, not theorised: an agent
	// started from the panel died the moment its daemon's socket was closed,
	// while the daemon itself carried on retrying.
	//
	// WithoutCancel rather than Background so anything carried in the context
	// — a logger, a trace — survives; only the cancellation is dropped. What
	// ends a session is Kill, the process itself, or StopAll at shutdown.
	argv := slices.Concat(cmd.Argv, s.conversationArgs(req))
	// The command itself, last, so the terminal says what is about to run in
	// it. No argument carries a credential: the token is in the MCP config.
	sess.addNotice(CommandLine(req.Dir, argv))
	tty, err := s.start(context.WithoutCancel(ctx), pty.Spec{
		Argv: argv,
		Dir:  req.Dir,
		Cols: req.Cols,
		Rows: req.Rows,
	})
	if err != nil {
		release()
		return nil, err
	}

	sess.mu.Lock()
	sess.tty = tty
	sess.state = StateRunning
	sess.mu.Unlock()

	go s.reap(sess)
	return sess, nil
}

// checkCapacityLocked refuses a start that would exceed either cap.
//
// Only live sessions count, and that is the whole point of the loop. A
// finished session stays in the map so its exit can still be reported, and
// counting those meant every agent a machine had ever run held a slot until
// the daemon restarted: four that had exited, and the next launch was refused
// as "4 running". Running, Live and Dirs already skip them for the same reason.
func (s *Supervisor) checkCapacityLocked(profile string) error {
	machine, forProfile := 0, 0
	for id, sess := range s.sessions {
		if state, _, _ := sess.State(); state.Terminal() {
			continue
		}
		machine++
		if s.profiles[id] == profile {
			forProfile++
		}
	}
	// The limit is named as well as the count. The reason reaches a person, in
	// a toast that is the only place they will see it, and "8 running" without
	// "the limit is 8" reads as a fact rather than as something to act on.
	if s.wholeMachine > 0 && machine >= s.wholeMachine {
		return fmt.Errorf("%w: %d already running on this machine, and the limit is %d",
			ErrAtCapacity, machine, s.wholeMachine)
	}
	if s.perProfile > 0 && forProfile >= s.perProfile {
		return fmt.Errorf("%w: %d already running for profile %q, and the limit is %d",
			ErrAtCapacity, forProfile, profile, s.perProfile)
	}
	return nil
}

// pruneFinishedLocked drops finished sessions nobody is going to ask about.
func (s *Supervisor) pruneFinishedLocked(now time.Time) {
	cutoff := now.Add(-s.finishedRetention)
	for id, sess := range s.sessions {
		if sess.finishedBefore(cutoff) {
			delete(s.sessions, id)
			delete(s.profiles, id)
		}
	}
}

// finishedBefore reports whether this session ended before t. A session that
// has not ended is never before anything.
func (sess *Session) finishedBefore(t time.Time) bool {
	sess.mu.RLock()
	defer sess.mu.RUnlock()
	return !sess.endedAt.IsZero() && sess.endedAt.Before(t)
}

// reap waits for a session to end and records how.
//
// The session stays in the map after it exits, for FinishedRetention. A caller
// asking about a session that has just died should be told it died and with
// what code, not that it never existed — "no such session" for something
// somebody was watching a moment ago is a confusing answer.
func (s *Supervisor) reap(sess *Session) {
	code, err := sess.tty.Wait()

	sess.mu.Lock()
	if sess.state != StateKilled {
		// A kill is already accounted for by Kill; the exit it caused is the
		// consequence, not news.
		sess.exitCode = code
		sess.err = err
		if err != nil {
			sess.state = StateFailed
		} else {
			sess.state = StateExited
		}
	}
	sess.endedAt = time.Now()
	sess.mu.Unlock()

	// Announced only now, with the state written. Anyone waiting on the end of
	// this session sees the answer rather than racing for it.
	close(sess.ended)
}

// Kill ends a session.
//
// Closing the pseudo-terminal first is how a well-behaved process is asked to
// leave: on Unix it delivers EOF. The kill that follows covers everything
// else. The whole process group goes, because agents spawn children and
// leaking them is how a machine fills up.
func (s *Supervisor) Kill(id uint64) error {
	s.mu.Lock()
	sess, ok := s.sessions[id]
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("%w: %d", ErrNoSuchSession, id)
	}

	sess.mu.Lock()
	if sess.state.Terminal() {
		sess.mu.Unlock()
		return nil // already gone; killing it again is not an error
	}
	sess.state = StateKilled
	tty := sess.tty
	cancelPrepare := sess.cancelPrepare
	sess.mu.Unlock()

	if cancelPrepare != nil {
		cancelPrepare()
	}

	if tty == nil {
		return nil
	}
	return tty.Close()
}

// Get returns a session, running or finished.
func (s *Supervisor) Get(id uint64) (*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[id]
	if !ok {
		return nil, fmt.Errorf("%w: %d", ErrNoSuchSession, id)
	}
	return sess, nil
}

// Forget drops a finished session from the map.
//
// Only a finished one: forgetting a running session would leave a process
// nothing is watching and nothing can kill.
func (s *Supervisor) Forget(id uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[id]
	if !ok {
		return fmt.Errorf("%w: %d", ErrNoSuchSession, id)
	}
	sess.mu.RLock()
	terminal := sess.state.Terminal()
	sess.mu.RUnlock()
	if !terminal {
		return fmt.Errorf("supervisor: session %d is still running", id)
	}
	delete(s.sessions, id)
	delete(s.profiles, id)
	return nil
}

// Count is how many sessions exist, finished ones included.
func (s *Supervisor) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sessions)
}

// Running lists the sessions that are still alive.
//
// Finished ones are excluded on purpose: this answers "what is this daemon
// actually supervising", which is what a reconnecting backend needs in order
// to correct rows it believes are running.
func (s *Supervisor) Running() []uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]uint64, 0, len(s.sessions)+len(s.returning))
	for id, sess := range s.sessions {
		if state, _, _ := sess.State(); !state.Terminal() {
			ids = append(ids, id)
		}
	}
	for id := range s.returning {
		if sess, ok := s.sessions[id]; ok {
			if state, _, _ := sess.State(); !state.Terminal() {
				continue // a process that outlived the wait, listed above
			}
		}
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

// Ended closes once the session has finished and its outcome is recorded.
func (sess *Session) Ended() <-chan struct{} { return sess.ended }

// StopAll ends every running session and waits for them to go.
//
// Called when the daemon is shutting down. Leaving them is not an option, and
// the reason is not that they would keep running — closing a pseudo-terminal
// hangs up on its foreground process group, so most of them die anyway. The
// reason is that "most" and "anyway" are not a design: a process that ignores
// SIGHUP, or a grandchild in its own process group, survives. And a surviving
// agent cannot be reached by anything afterwards — nothing lists it, nothing
// can stop it, and the next daemon does not adopt it. It starts believing
// nothing is running, says so, and the row disappears while the process keeps
// working against the workspace with the credential still in its folder.
//
// An agent nobody can see and nobody can stop is the outcome this design
// exists to prevent, so shutdown stops them on purpose rather than hoping.
//
// Bounded, because a shutdown that waits for ever is a machine somebody has to
// go and find. Whatever has not gone by then is left to the hang-up that
// follows when this process exits.
func (s *Supervisor) StopAll(ctx context.Context, wait time.Duration) []uint64 {
	live := s.Live()
	stopped := make([]uint64, 0, len(live))
	for _, sess := range live {
		if err := s.Kill(sess.ID); err == nil {
			stopped = append(stopped, sess.ID)
		}
	}

	deadline := time.After(wait)
	for _, sess := range live {
		select {
		case <-sess.Ended():
		case <-deadline:
			return stopped
		case <-ctx.Done():
			return stopped
		}
	}
	return stopped
}

// HandOver stops every running session for a restart that will start them
// again, and returns them.
//
// Unlike [Supervisor.StopAll] their ends are not reported, and [Running] keeps
// naming them until they are started again or [Supervisor.Abandon] gives up:
// the backend deletes the row of a session that ends, and a restored agent
// with no row is one nothing can list or stop. Finished, they are dropped from
// the map so the same ids can be started again.
func (s *Supervisor) HandOver(ctx context.Context, wait time.Duration) []*Session {
	live := s.Live()
	s.mu.Lock()
	for _, sess := range live {
		sess.mu.Lock()
		sess.handedOver = true
		sess.mu.Unlock()
		s.returning[sess.ID] = true
	}
	s.mu.Unlock()

	for _, sess := range live {
		_ = s.Kill(sess.ID)
	}
	deadline := time.After(wait)
	for _, sess := range live {
		select {
		case <-sess.Ended():
		case <-deadline:
		case <-ctx.Done():
		}
	}

	s.mu.Lock()
	for _, sess := range live {
		// Ended, not a terminal state: a kill is terminal the moment it is
		// asked for, and dropping a process that is still there would leave
		// it unlisted.
		select {
		case <-sess.Ended():
			delete(s.sessions, sess.ID)
			delete(s.profiles, sess.ID)
		default:
		}
	}
	s.mu.Unlock()
	return live
}

// Expect names sessions a previous daemon handed over, from its note, until
// they are started again: the first hello must list them or the backend
// deletes the rows they come back to.
func (s *Supervisor) Expect(ids ...uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range ids {
		s.returning[id] = true
	}
}

// Abandon stops naming sessions that are not coming back — the ones given,
// or with none given all of them.
func (s *Supervisor) Abandon(ids ...uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(ids) == 0 {
		clear(s.returning)
		return
	}
	for _, id := range ids {
		delete(s.returning, id)
	}
}

// HandedOver reports whether this session was stopped to be started again.
func (sess *Session) HandedOver() bool {
	sess.mu.RLock()
	defer sess.mu.RUnlock()
	return sess.handedOver
}

// Live lists the sessions still running, with what they were started with.
//
// Used to write the note that survives a restart. It carries intent — kind,
// folder, arguments — and nothing about the state of the terminal, because
// none of that survives a new process anyway.
func (s *Supervisor) Live() []*Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		if state, _, _ := sess.State(); !state.Terminal() {
			out = append(out, sess)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Profile is the profile a session belongs to.
func (s *Supervisor) Profile(id uint64) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.profiles[id]
}

// Dirs are the working directories of the sessions still running.
//
// Used to decide which filesystems are worth reporting: the mount a workspace
// sits on is the one that fills up, and it is not always the root one.
func (s *Supervisor) Dirs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	seen := map[string]bool{}
	out := make([]string, 0, len(s.sessions))
	for _, sess := range s.sessions {
		if state, _, _ := sess.State(); state.Terminal() {
			continue
		}
		if sess.Dir == "" || seen[sess.Dir] {
			continue
		}
		seen[sess.Dir] = true
		out = append(out, sess.Dir)
	}
	sort.Strings(out)
	return out
}

// State reports a session's current state and exit code.
func (sess *Session) State() (State, int, error) {
	sess.mu.RLock()
	defer sess.mu.RUnlock()
	return sess.state, sess.exitCode, sess.err
}

// PTY is the terminal, for the streaming layer to read and write.
func (sess *Session) PTY() pty.Session {
	sess.mu.RLock()
	defer sess.mu.RUnlock()
	return sess.tty
}

// addNotice records something the person needs told about this launch.
func (sess *Session) addNotice(text string) {
	sess.mu.Lock()
	sess.notices = append(sess.notices, text)
	sess.mu.Unlock()
	if sess.progress != nil {
		sess.progress.Notice(text)
	}
}

// launchProgress is what a fork's folder is made with: its steps are the
// session's notices, and what git prints goes wherever the launch is watched.
type launchProgress struct{ sess *Session }

func (p launchProgress) Notice(text string) { p.sess.addNotice(text) }

func (p launchProgress) Write(b []byte) (int, error) {
	if p.sess.progress == nil {
		return len(b), nil
	}
	return p.sess.progress.Write(b)
}

// Notices are what the launch did and decided, for the streaming layer to put at the top of the terminal.
//
// The terminal rather than the log, because the log is on the machine and the
// person is in a browser. Written into the stream, so the agent's own process
// never sees them: they are for the human reading the scrollback, and a line
// injected into an agent's input would be a line it might act on.
func (sess *Session) Notices() []string {
	sess.mu.RLock()
	defer sess.mu.RUnlock()
	return append([]string(nil), sess.notices...)
}
