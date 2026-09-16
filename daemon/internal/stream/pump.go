// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package stream

import (
	"errors"
	"io"
	"sync"
	"time"

	"github.com/agentrq/agentrq/daemon/wire"
)

// Sender delivers a frame to the backend.
type Sender interface {
	Send(wire.Frame) error
}

// ErrBackpressure is what a Sender returns when it cannot keep up.
//
// Distinguished from a broken connection because the two want opposite
// responses: a broken connection ends the session, while backpressure is
// answered by resyncing and carrying on.
var ErrBackpressure = errors.New("stream: send would block")

// Pump reads a session's terminal and ships it to the backend.
//
// It does three things at once, and the order matters:
//
//  1. Everything read is written to the screen, always, whether or not anyone
//     is attached. The screen has to be correct the moment somebody attaches,
//     and it cannot be rebuilt later from bytes nobody kept.
//  2. Output is coalesced into frames.
//  3. If the backend cannot keep up, the pending batch is discarded and a
//     redraw is sent instead — the viewer loses intermediate frames and keeps a
//     correct screen, which is the only safe way to shed load from a stateful
//     stream.
type Pump struct {
	SessionID uint64
	Screen    *Screen
	Sender    Sender

	// Now is the clock, injected so the batching can be tested at exact
	// instants rather than by sleeping.
	Now func() time.Time

	mu        sync.Mutex
	coalescer *Coalescer
	attached  bool
	resyncs   int
}

// NewPump makes a pump for one session.
func NewPump(sessionID uint64, screen *Screen, sender Sender) *Pump {
	return &Pump{
		SessionID: sessionID,
		Screen:    screen,
		Sender:    sender,
		Now:       time.Now,
		coalescer: NewCoalescer(Window, MaxBatch),
	}
}

// Attach marks a viewer as present and sends them the current screen.
//
// A redraw rather than a replay of history: output that redraws in place makes
// byte history useless, and this is what makes a reattached progress bar look
// like a progress bar.
func (p *Pump) Attach() error {
	p.mu.Lock()
	p.attached = true
	// Anything pending is superseded by the redraw about to be sent, and
	// sending both would paint the screen and then paint a fragment of its
	// history on top.
	p.coalescer.Discard()
	p.mu.Unlock()

	return p.sendFrame(wire.TypeReplay, p.Screen.Redraw())
}

// Detach marks the viewer as gone.
//
// The screen keeps being fed. Stopping would mean the next attach shows
// whatever was on screen when the last viewer left.
func (p *Pump) Detach() {
	p.mu.Lock()
	p.attached = false
	p.coalescer.Discard()
	p.mu.Unlock()
}

// Run reads until the terminal ends.
func (p *Pump) Run(r io.Reader) error {
	buf := make([]byte, 32*1024)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			if writeErr := p.Feed(buf[:n]); writeErr != nil {
				return writeErr
			}
		}
		if err != nil {
			// A pty read ends with an error rather than EOF when the child
			// exits (EIO on Linux), so this is the ordinary way out. Send
			// whatever is still pending before going.
			_ = p.Flush()
			if errors.Is(err, io.EOF) {
				return nil
			}
			return nil
		}
	}
}

// Feed handles one read from the terminal.
func (p *Pump) Feed(b []byte) error {
	// The screen first and unconditionally: it is the thing that has to be
	// right later, and it is cheap.
	if _, err := p.Screen.Write(b); err != nil {
		return err
	}

	p.mu.Lock()
	due := p.coalescer.Add(b, p.Now())
	attached := p.attached
	p.mu.Unlock()

	if !attached || !due {
		return nil
	}
	return p.Flush()
}

// Flush sends whatever is pending.
func (p *Pump) Flush() error {
	p.mu.Lock()
	if !p.attached {
		// Nothing is watching; the screen already has it.
		p.coalescer.Discard()
		p.mu.Unlock()
		return nil
	}
	batch := p.coalescer.Take()
	p.mu.Unlock()

	if len(batch) == 0 {
		return nil
	}

	err := p.sendFrame(wire.TypeOutput, batch)
	if errors.Is(err, ErrBackpressure) {
		// The one safe way to shed load: throw away the frames nobody could
		// receive and send the screen as it is now. Dropping bytes without
		// this would leave the viewer's terminal permanently wrong.
		//
		// The batch is passed in because Take already removed it: the
		// coalescer never saw those bytes go, and without telling it the count
		// under-reports exactly the loss it exists to surface.
		return p.resync(len(batch))
	}
	return err
}

// resync discards the backlog and repaints.
func (p *Pump) resync(alreadyLost int) error {
	p.mu.Lock()
	p.coalescer.Elide(alreadyLost)
	p.coalescer.Discard()
	p.resyncs++
	p.mu.Unlock()

	err := p.sendFrame(wire.TypeReplay, p.Screen.Redraw())
	if errors.Is(err, ErrBackpressure) {
		// Even the redraw could not go. Nothing more to try — the screen
		// stays correct here, and the next successful attach will repaint.
		return nil
	}
	return err
}

// Resyncs is how many times this session has had to repaint under load.
//
// Surfaced rather than hidden: a session resyncing repeatedly is a process
// producing more output than anyone can read, which is usually a bug on that
// machine and something the owner should be told about.
func (p *Pump) Resyncs() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.resyncs
}

// Elided is how many bytes have been superseded by resyncs and detaches.
func (p *Pump) Elided() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.coalescer.Elided()
}

// Deadline is when the pending batch must be sent, for a caller that wants to
// wake exactly then rather than poll.
func (p *Pump) Deadline() time.Time {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.coalescer.Deadline()
}

func (p *Pump) sendFrame(t wire.Type, payload []byte) error {
	if len(payload) == 0 {
		return nil
	}
	f, err := wire.SessionFrame(t, p.SessionID, payload)
	if err != nil {
		return err
	}
	return p.Sender.Send(f)
}
