// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package link

import (
	"errors"
	"io"
	"testing"

	"github.com/agentrq/agentrq/daemon/wire"
)

// failingSender is a connection that will not take anything.
type failingSender struct{ err error }

func (f failingSender) Send(wire.Frame) error { return f.err }

// A repaint that cannot be sent must not stop the other terminals being moved
// onto the new connection. One session's socket failing is not a reason to
// leave every other session on this machine writing into a socket that is gone.
func TestRebindReportsARepaintThatCouldNotBeSent(t *testing.T) {
	s := newStreams()
	r, w := io.Pipe()
	t.Cleanup(func() { _ = w.Close() })
	s.add(7, 80, 24, r, failingSender{})

	boom := errors.New("socket is gone")
	errs := s.rebind(failingSender{err: boom}, func(uint64) bool { return true })
	if len(errs) != 1 || !errors.Is(errs[0], boom) {
		t.Fatalf("rebind reported %v, want the send failure", errs)
	}
}

// Nobody watching means nothing to repaint — the pump is moved and stays quiet
// until somebody attaches.
func TestRebindDoesNotRepaintAnUnwatchedTerminal(t *testing.T) {
	s := newStreams()
	r, w := io.Pipe()
	t.Cleanup(func() { _ = w.Close() })
	s.add(7, 80, 24, r, failingSender{})

	if errs := s.rebind(failingSender{err: errors.New("would fail if sent")}, nil); len(errs) != 0 {
		t.Errorf("rebind sent something for a terminal nobody is watching: %v", errs)
	}
	if errs := s.rebind(failingSender{err: errors.New("would fail if sent")},
		func(uint64) bool { return false }); len(errs) != 0 {
		t.Errorf("rebind sent something for a terminal nobody is watching: %v", errs)
	}
}
