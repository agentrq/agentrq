// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package link

import (
	"io"
	"sync"
	"time"

	"github.com/agentrq/agentrq/daemon/internal/stream"
)

// FlushInterval is how often a pending batch is checked for.
//
// The coalescer only sends when a batch comes due during a read, so a burst
// that stops mid-window would otherwise sit unsent until the program produced
// more output. That is exactly the case of a prompt: the last thing written
// before somebody is asked to type is the thing they most need to see.
const FlushInterval = 25 * time.Millisecond

// streams owns one pump per running session.
//
// Separate from the supervisor because the supervisor knows about processes
// and this knows about the connection: a reconnect replaces every pump without
// touching a single running agent.
type streams struct {
	mu    sync.Mutex
	pumps map[uint64]*stream.Pump
	stop  map[uint64]chan struct{}
}

func newStreams() *streams {
	return &streams{pumps: map[uint64]*stream.Pump{}, stop: map[uint64]chan struct{}{}}
}

// add starts pumping a session's terminal to the backend.
func (s *streams) add(id uint64, cols, rows uint16, r io.Reader, sender stream.Sender) *stream.Pump {
	screen := stream.NewScreen(int(cols), int(rows))
	p := stream.NewPump(id, screen, sender)

	done := make(chan struct{})
	s.mu.Lock()
	s.pumps[id] = p
	s.stop[id] = done
	s.mu.Unlock()

	go func() {
		_ = p.Run(r)
		s.remove(id)
	}()
	go s.flush(p, done)
	return p
}

// flush sends a batch that came due while the terminal was quiet.
func (s *streams) flush(p *stream.Pump, done <-chan struct{}) {
	t := time.NewTicker(FlushInterval)
	defer t.Stop()
	for {
		select {
		case <-done:
			return
		case <-t.C:
			_ = p.Flush()
		}
	}
}

func (s *streams) get(id uint64) (*stream.Pump, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.pumps[id]
	return p, ok
}

func (s *streams) remove(id uint64) {
	s.mu.Lock()
	done, ok := s.stop[id]
	delete(s.pumps, id)
	delete(s.stop, id)
	s.mu.Unlock()
	if ok {
		close(done)
	}
}

// detachAll forgets the viewers, which is what a lost connection means.
//
// The pumps stay, and so do the sessions. A pump belongs to a session rather
// than to a socket for two reasons: it holds the screen, which has to survive
// a reconnect or the next viewer sees a blank terminal it will never get back;
// and it is blocked reading a pseudo-terminal that nothing has closed, so a
// second pump on the same session would not replace the first, it would race
// it for every byte the agent produced.
func (s *streams) detachAll() {
	for _, p := range s.all() {
		p.Detach()
	}
}

// rebind points every pump at the new connection.
//
// This is the other half of surviving a reconnect. The sessions kept running
// across the gap — that is what reconnecting is for — but their output was
// going to a socket that has gone, so without this a surviving agent's
// terminal goes silent for good: nothing reaches the backend and an attach
// finds a stream that cannot answer.
//
// `watched` says which sessions somebody is still looking at, and those are
// repainted: the browser's own socket is unaffected by the daemon's
// reconnecting, so the person watching never asked to attach again and the
// backend has no reason to ask on their behalf.
func (s *streams) rebind(sender stream.Sender, watched func(uint64) bool) []error {
	var errs []error
	for id, p := range s.snapshot() {
		p.Rebind(sender)
		if watched == nil || !watched(id) {
			continue
		}
		if err := p.Attach(); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

// all is the pumps, copied so a caller can work without the lock.
func (s *streams) all() []*stream.Pump {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*stream.Pump, 0, len(s.pumps))
	for _, p := range s.pumps {
		out = append(out, p)
	}
	return out
}

// snapshot is the pumps with their session ids, copied for the same reason.
func (s *streams) snapshot() map[uint64]*stream.Pump {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[uint64]*stream.Pump, len(s.pumps))
	for id, p := range s.pumps {
		out[id] = p
	}
	return out
}
