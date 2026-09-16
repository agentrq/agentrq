// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package supervisor

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/agentrq/agentrq/daemon/internal/pty"
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
	// ReuseMCPConfig says this session is being restored after the daemon
	// replaced itself, and its config is already in the folder.
	//
	// The note that survives a restart carries no MCP URL on purpose — the
	// credential is inside it — so a restored agent reads the file that was
	// written when it was first launched. If that file is not there, the
	// session fails with a reason rather than starting an agent that cannot
	// reach its workspace and looks merely broken.
	ReuseMCPConfig bool
	Cols           uint16
	Rows           uint16
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
)

// KillGrace is how long a process gets to leave politely.
//
// Long enough for an agent to finish writing a file it had open; short enough
// that a person killing a runaway session sees something happen.
const KillGrace = 5 * time.Second

// Supervisor owns every session on this machine.
type Supervisor struct {
	start Starter

	// PerProfile caps one account. WholeMachine caps the box.
	//
	// The second is the one that protects anything. CPU and memory are
	// physical and shared, and two accounts that cannot see each other will
	// otherwise exhaust a machine neither believes it is overloading.
	perProfile   int
	wholeMachine int

	mu       sync.Mutex
	sessions map[uint64]*Session
	profiles map[uint64]string // session id → profile
}

// New makes a supervisor.
func New(start Starter, perProfile, wholeMachine int) *Supervisor {
	return &Supervisor{
		start:        start,
		perProfile:   perProfile,
		wholeMachine: wholeMachine,
		sessions:     map[uint64]*Session{},
		profiles:     map[uint64]string{},
	}
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

	s.mu.Lock()
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
	sess := &Session{
		ID: req.ID, Kind: req.Kind, Dir: req.Dir,
		Params: req.Params, Cols: req.Cols, Rows: req.Rows,
		state: StateStarting, ended: make(chan struct{}),
	}
	s.sessions[req.ID] = sess
	s.profiles[req.ID] = profile
	s.mu.Unlock()

	// From here, any failure has to release the reservation.
	release := func() {
		s.mu.Lock()
		delete(s.sessions, req.ID)
		delete(s.profiles, req.ID)
		s.mu.Unlock()
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
			if _, err := WriteMCPConfig(req.Dir, req.Params.ServerName, req.MCPURL); err != nil {
				release()
				return nil, err
			}
		}
	}

	tty, err := s.start(ctx, pty.Spec{
		Argv: cmd.Argv,
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
func (s *Supervisor) checkCapacityLocked(profile string) error {
	if s.wholeMachine > 0 && len(s.sessions) >= s.wholeMachine {
		return fmt.Errorf("%w: %d running on this machine", ErrAtCapacity, len(s.sessions))
	}
	if s.perProfile > 0 {
		n := 0
		for _, p := range s.profiles {
			if p == profile {
				n++
			}
		}
		if n >= s.perProfile {
			return fmt.Errorf("%w: %d running for profile %q", ErrAtCapacity, n, profile)
		}
	}
	return nil
}

// reap waits for a session to end and records how.
//
// The session stays in the map after it exits. A caller asking about a session
// that has just died should be told it died and with what code, not that it
// never existed — "no such session" for something somebody was watching a
// moment ago is a confusing answer.
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
	sess.mu.Unlock()

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
	ids := make([]uint64, 0, len(s.sessions))
	for id, sess := range s.sessions {
		if state, _, _ := sess.State(); !state.Terminal() {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

// Ended closes once the session has finished and its outcome is recorded.
func (sess *Session) Ended() <-chan struct{} { return sess.ended }

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
