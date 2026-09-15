// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

// Package pty runs a process inside a pseudo-terminal the daemon owns.
//
// This is the platform layer, and it is the part of agentrqd with the least
// portable ground under it. Unix has had openpty for decades; Windows grew
// ConPTY only in Windows 10 1809, it has no SIGWINCH, and its signal semantics
// are different enough that "kill" is a different operation rather than the
// same one spelled differently. github.com/aymanbagabas/go-pty presents one
// interface over both, which is why it is here rather than creack/pty — the
// latter is Unix-only.
//
// Everything above this package talks to [Session] and never to a platform, so
// the supervisor is testable without a terminal. What cannot be faked is
// whether a real ConPTY behaves: that is what the integration tests beside this
// file are for, and why they run on all three operating systems in CI.
package pty

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	xpty "github.com/aymanbagabas/go-pty"
)

// Session is a process running in a pseudo-terminal.
//
// Read yields whatever the process drew, including the escape sequences it used
// to draw it; Write is the keyboard. Neither interprets anything — a terminal
// stream is bytes, and a layer that "helpfully" re-encoded them would break Esc
// and every colour reset in the stream.
type Session interface {
	io.ReadWriteCloser

	// Resize tells the process its window changed.
	Resize(cols, rows uint16) error

	// Wait blocks until the process exits and reports its exit code. A process
	// killed by a signal reports a non-zero code rather than an error: dying is
	// a normal outcome for a session, not a failure of this package.
	Wait() (int, error)
}

// Spec describes the process to start.
type Spec struct {
	// Argv is the command and its arguments. Argv[0] is the executable.
	Argv []string
	// Dir is the working directory. Required: an agent started in whatever
	// directory the daemon happened to be in is a bug that looks like it works.
	Dir string
	// Env is the process environment. Nil inherits the daemon's.
	Env []string
	// Cols and Rows are the initial window size. Zero means 80x24, because a
	// process that asks and is told nothing tends to assume something worse.
	Cols, Rows uint16
}

// Defaults applied when a Spec leaves the size unset.
const (
	DefaultCols = 80
	DefaultRows = 24
)

// Errors callers are expected to distinguish. A missing working directory in
// particular is a user-fixable condition, not a fault, and it must reach them
// naming the path rather than as "failed to start".
var (
	ErrNoArgv     = errors.New("pty: no command given")
	ErrNoDir      = errors.New("pty: no working directory given")
	ErrDirMissing = errors.New("pty: working directory does not exist")
	ErrDirNotDir  = errors.New("pty: working directory is not a directory")
)

// Start opens a pseudo-terminal and starts the process in it.
//
// The working directory is checked here, before anything is spawned, so the
// failure names the path. The server cannot do this check — the agent runs on a
// different machine — so if this package does not do it, nobody does, and the
// user is told only that the session failed to start.
func Start(ctx context.Context, spec Spec) (Session, error) {
	if len(spec.Argv) == 0 {
		return nil, ErrNoArgv
	}
	if spec.Dir == "" {
		return nil, ErrNoDir
	}
	if err := checkDir(spec.Dir); err != nil {
		return nil, err
	}

	cols, rows := spec.Cols, spec.Rows
	if cols == 0 {
		cols = DefaultCols
	}
	if rows == 0 {
		rows = DefaultRows
	}

	p, err := xpty.New()
	if err != nil {
		return nil, fmt.Errorf("pty: open: %w", err)
	}

	// Set the size before starting, so the process never observes the default
	// and redraws. A TUI that has already painted at 80x24 does not always
	// repaint cleanly when told otherwise a moment later.
	if err := p.Resize(int(cols), int(rows)); err != nil {
		_ = p.Close()
		return nil, fmt.Errorf("pty: initial resize: %w", err)
	}

	cmd := p.CommandContext(ctx, spec.Argv[0], spec.Argv[1:]...)
	cmd.Dir = spec.Dir
	cmd.Env = spec.Env
	if err := cmd.Start(); err != nil {
		_ = p.Close()
		return nil, fmt.Errorf("pty: start %q: %w", spec.Argv[0], err)
	}

	return &session{pty: p, cmd: cmd}, nil
}

// checkDir reports why a working directory cannot be used, in terms the person
// who set it can act on.
func checkDir(dir string) error {
	info, err := os.Stat(dir)
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%w: %s", ErrDirMissing, dir)
	}
	if err != nil {
		return fmt.Errorf("pty: working directory %s: %w", dir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%w: %s", ErrDirNotDir, dir)
	}
	return nil
}

type session struct {
	pty xpty.Pty
	cmd *xpty.Cmd

	// Wait is called by whoever is supervising and again by Close on the way
	// out; os/exec returns an error if Wait runs twice, so the result is
	// remembered instead.
	once     sync.Once
	exitCode int
	waitErr  error

	closeOnce sync.Once
	closeErr  error
}

func (s *session) Read(p []byte) (int, error)  { return s.pty.Read(p) }
func (s *session) Write(p []byte) (int, error) { return s.pty.Write(p) }

func (s *session) Resize(cols, rows uint16) error {
	if cols == 0 || rows == 0 {
		return fmt.Errorf("pty: resize to %dx%d is not a window", cols, rows)
	}
	return s.pty.Resize(int(cols), int(rows))
}

func (s *session) Wait() (int, error) {
	s.once.Do(func() {
		err := s.cmd.Wait()
		if s.cmd.ProcessState != nil {
			s.exitCode = s.cmd.ProcessState.ExitCode()
		}
		// A non-zero exit arrives as an *ExitError. That is the process's
		// answer, not this package failing, so it is reported as a code.
		var exit *os.SyscallError
		if err != nil && !errors.As(err, &exit) && s.cmd.ProcessState == nil {
			s.waitErr = err
		}
	})
	return s.exitCode, s.waitErr
}

// Close kills the process and releases the pseudo-terminal.
//
// Closing the PTY first is deliberate: on Unix it delivers EOF to the child,
// which is how a well-behaved process is asked to leave. The kill is what
// covers the rest.
func (s *session) Close() error {
	s.closeOnce.Do(func() {
		var errs []error
		if err := s.pty.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close pty: %w", err))
		}
		if s.cmd.Process != nil {
			// Already gone is the expected case once the PTY is closed, and is
			// not worth reporting as a failure to close.
			if err := s.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
				errs = append(errs, fmt.Errorf("kill: %w", err))
			}
		}
		s.closeErr = errors.Join(errs...)
	})
	return s.closeErr
}
