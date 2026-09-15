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

// closeAll ends every pump, which is what a lost connection does.
//
// The sessions themselves keep running. A daemon that killed its agents every
// time the network blinked would be worse than no daemon at all.
func (s *streams) closeAll() {
	s.mu.Lock()
	ids := make([]uint64, 0, len(s.pumps))
	for id := range s.pumps {
		ids = append(ids, id)
	}
	s.mu.Unlock()
	for _, id := range ids {
		s.remove(id)
	}
}
