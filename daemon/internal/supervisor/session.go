// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package supervisor

import (
	"context"
	"errors"
	"fmt"
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
	Cols   uint16
	Rows   uint16
}

// Session is one running agent.
type Session struct {
	ID   uint64
	Kind Kind

	mu       sync.RWMutex
	state    State
	exitCode int
	err      error

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
	sess := &Session{ID: req.ID, Kind: req.Kind, state: StateStarting}
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
		if _, err := WriteMCPConfig(req.Dir, req.Params.ServerName, req.MCPURL); err != nil {
			release()
			return nil, err
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
	defer sess.mu.Unlock()
	if sess.state == StateKilled {
		// Already accounted for by Kill; the exit is the consequence, not news.
		return
	}
	sess.exitCode = code
	sess.err = err
	if err != nil {
		sess.state = StateFailed
	} else {
		sess.state = StateExited
	}
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
