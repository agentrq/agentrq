// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

// Package stream carries a session's terminal output to the backend.
//
// The shape of this package is decided by one fact: a progress bar is not a
// lot of output. It is one line rewritten a hundred times a second, and
// claude-code does exactly that, so it is the common case rather than an edge
// case. Two obvious designs are both wrong.
//
// A byte ring buffer is the wrong data structure for scrollback: 256 KiB of it
// becomes 256 KiB of the same line redrawn, and the history that mattered has
// been pushed out. See screen.go, which keeps a terminal instead.
//
// And "drop the oldest bytes" under load is actively destructive. Terminal
// output is a stateful stream: drop a chunk and you may drop the ESC[0m that
// resets colour, after which everything renders wrong permanently; drop part
// of an escape sequence and the emulator parses garbage. So nothing here ever
// drops a byte — it coalesces, and when that is not enough it resyncs from
// screen state.
package stream

import "time"

// Window is how long output is gathered before it is sent.
//
// 16–33 ms is a frame at 60–30 fps. A spinner redrawing 100 times a second
// collapses to about 30 frames with no visual difference, because the
// intermediate redraws simply land inside one paint.
const Window = 25 * time.Millisecond

// MaxBatch forces a send before the window closes.
//
// Without it, a process writing steadily would grow one batch until the window
// expired, and a burst could build a frame larger than the protocol allows.
// Well under wire.MaxPayload so a batch always fits.
const MaxBatch = 256 * 1024

// Coalescer gathers PTY output into batches.
//
// **Concatenation is lossless.** The bytes are unchanged and still contiguous,
// so the terminal at the far end sees exactly the stream it would have seen —
// only fewer, larger writes. That is the whole reason this is safe to do to a
// stateful byte stream when dropping is not.
//
// The timing lives outside this type. What is here is the decision — "is there
// anything to send, and is it time" — which is testable without a clock; the
// pump that calls it is a few lines in the session loop.
type Coalescer struct {
	window time.Duration
	max    int

	buf     []byte
	firstAt time.Time
	elided  int // bytes lost to a resync, reported rather than hidden
}

// NewCoalescer makes one. A zero window or max takes the defaults.
func NewCoalescer(window time.Duration, max int) *Coalescer {
	if window <= 0 {
		window = Window
	}
	if max <= 0 {
		max = MaxBatch
	}
	return &Coalescer{window: window, max: max}
}

// Add appends output and reports whether it should be sent now.
//
// `now` is passed in rather than read so the rule can be tested at exact
// instants instead of by sleeping, which is how timing tests become flaky.
func (c *Coalescer) Add(p []byte, now time.Time) bool {
	if len(p) == 0 {
		return false
	}
	if len(c.buf) == 0 {
		c.firstAt = now
	}
	c.buf = append(c.buf, p...)
	return c.Due(now)
}

// Due reports whether the pending batch should be sent.
func (c *Coalescer) Due(now time.Time) bool {
	if len(c.buf) == 0 {
		return false
	}
	if len(c.buf) >= c.max {
		return true
	}
	return !now.Before(c.firstAt.Add(c.window))
}

// Take returns the pending batch and clears it.
//
// The slice is handed over rather than copied, and the buffer is replaced
// rather than truncated. Reusing the backing array would hand the caller bytes
// that the next write then overwrites — the same aliasing bug the wire decoder
// avoids, arriving from the other direction.
func (c *Coalescer) Take() []byte {
	if len(c.buf) == 0 {
		return nil
	}
	out := c.buf
	c.buf = nil
	return out
}

// Pending is how many bytes are waiting.
func (c *Coalescer) Pending() int { return len(c.buf) }

// Deadline is when the pending batch must be sent, or zero if nothing is
// pending. A pump uses this to sleep exactly long enough rather than polling.
func (c *Coalescer) Deadline() time.Time {
	if len(c.buf) == 0 {
		return time.Time{}
	}
	return c.firstAt.Add(c.window)
}

// Discard throws away the pending batch, counting what was lost.
//
// Used only by a resync, where the screen is about to be redrawn in full and
// the pending bytes are superseded. This is the one place bytes are dropped,
// and it is safe precisely because it is paired with a redraw — the viewer
// loses intermediate frames and gains a correct screen.
func (c *Coalescer) Discard() int {
	n := len(c.buf)
	c.buf = nil
	c.elided += n
	return n
}

// Elided is how many bytes have been superseded by resyncs.
//
// Reported rather than hidden: a session that keeps resyncing is a process
// producing more output than anyone can read, which is usually a bug on that
// machine and worth saying out loud.
func (c *Coalescer) Elided() int { return c.elided }
