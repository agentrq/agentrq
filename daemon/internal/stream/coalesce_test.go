// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package stream

import (
	"bytes"
	"testing"
	"time"
)

var t0 = time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)

// The property the whole design rests on: concatenation changes nothing. The
// terminal at the far end sees exactly the stream it would have seen, only in
// fewer, larger writes — which is why this is safe to do to a stateful byte
// stream when dropping is not.
func TestCoalescingIsLossless(t *testing.T) {
	c := NewCoalescer(Window, MaxBatch)

	writes := [][]byte{
		{0x1b, '[', '2', 'K'},
		[]byte("progress: 10%\r"),
		{0x1b, '[', '0', 'm'},
		{0xff, 0xfe, 0x00}, // invalid UTF-8 and a NUL
		[]byte("done\n"),
	}
	var want []byte
	for _, w := range writes {
		c.Add(w, t0)
		want = append(want, w...)
	}

	got := c.Take()
	if !bytes.Equal(got, want) {
		t.Errorf("batch changed the stream:\n got %#v\nwant %#v", got, want)
	}
	if c.Pending() != 0 {
		t.Errorf("Take left %d bytes behind", c.Pending())
	}
}

// A spinner redrawing a hundred times a second becomes about thirty frames,
// and the bytes are identical.
func TestARedrawingLineCollapsesIntoOneBatch(t *testing.T) {
	c := NewCoalescer(Window, MaxBatch)

	now := t0
	sent := 0
	var total []byte
	for i := 0; i < 100; i++ {
		frame := []byte("\r[====    ] working")
		total = append(total, frame...)
		if c.Add(frame, now) {
			c.Take()
			sent++
		}
		now = now.Add(time.Millisecond) // 1000 redraws a second
	}
	if c.Pending() > 0 {
		sent++
	}
	// 100 ms of redraws at a 25 ms window: a handful of frames, not a hundred.
	if sent > 6 {
		t.Errorf("sent %d frames for 100 redraws; coalescing is not working", sent)
	}
}

func TestDueRespectsTheWindow(t *testing.T) {
	c := NewCoalescer(30*time.Millisecond, MaxBatch)

	if c.Due(t0) {
		t.Error("an empty coalescer is due")
	}
	if c.Add([]byte("x"), t0) {
		t.Error("a fresh byte is due immediately; the window is not being honoured")
	}
	if c.Due(t0.Add(29 * time.Millisecond)) {
		t.Error("due before the window closed")
	}
	if !c.Due(t0.Add(30 * time.Millisecond)) {
		t.Error("not due once the window closed")
	}
}

// Without a size limit, a steady writer grows one batch until the window
// expires, and a burst could build a frame larger than the protocol allows.
func TestALargeBurstIsSentWithoutWaiting(t *testing.T) {
	c := NewCoalescer(time.Hour, 1024)
	if c.Add(bytes.Repeat([]byte("x"), 1023), t0) {
		t.Error("due below the limit")
	}
	if !c.Add([]byte("xx"), t0) {
		t.Error("a batch over the limit was not sent")
	}
}

// Reusing the backing array would hand the caller bytes that the next write
// overwrites — the same aliasing bug the wire decoder avoids, from the other
// direction.
func TestTakeDoesNotAliasTheNextBatch(t *testing.T) {
	c := NewCoalescer(Window, MaxBatch)
	c.Add([]byte("first"), t0)
	first := c.Take()

	c.Add([]byte("SECOND-and-longer"), t0)
	_ = c.Take()

	if string(first) != "first" {
		t.Errorf("the first batch changed when the second was written: %q", first)
	}
}

func TestDeadlineTellsAPumpHowLongToSleep(t *testing.T) {
	c := NewCoalescer(25*time.Millisecond, MaxBatch)
	if !c.Deadline().IsZero() {
		t.Error("an empty coalescer has a deadline")
	}
	c.Add([]byte("x"), t0)
	if got, want := c.Deadline(), t0.Add(25*time.Millisecond); !got.Equal(want) {
		t.Errorf("Deadline() = %v, want %v", got, want)
	}
}

// The one place bytes are dropped, and it is safe only because it is paired
// with a full redraw.
func TestDiscardCountsWhatItThrewAway(t *testing.T) {
	c := NewCoalescer(Window, MaxBatch)
	c.Add(bytes.Repeat([]byte("x"), 500), t0)

	if n := c.Discard(); n != 500 {
		t.Errorf("Discard() = %d, want 500", n)
	}
	if c.Pending() != 0 {
		t.Error("Discard left bytes pending")
	}
	c.Add(bytes.Repeat([]byte("y"), 200), t0)
	c.Discard()

	// A session that keeps resyncing is a process producing more output than
	// anyone can read — usually a bug on that machine, and worth surfacing.
	if c.Elided() != 700 {
		t.Errorf("Elided() = %d, want 700", c.Elided())
	}
}

func TestEmptyWritesAreIgnored(t *testing.T) {
	c := NewCoalescer(Window, MaxBatch)
	if c.Add(nil, t0) || c.Add([]byte{}, t0) {
		t.Error("an empty write made a batch due")
	}
	if c.Take() != nil {
		t.Error("an empty write produced a batch")
	}
}

func TestDefaultsAreApplied(t *testing.T) {
	c := NewCoalescer(0, 0)
	c.Add([]byte("x"), t0)
	if got, want := c.Deadline(), t0.Add(Window); !got.Equal(want) {
		t.Errorf("default window not applied: %v", got)
	}
}

// A batch that has already been Taken and then could not be sent is gone, and
// the coalescer never saw it go. Without being told, the count under-reports
// exactly the loss it exists to surface — which is what a pump test caught.
func TestElideCountsLossTheCoalescerNeverSaw(t *testing.T) {
	c := NewCoalescer(Window, MaxBatch)
	c.Add([]byte("taken and then undeliverable"), t0)
	batch := c.Take()

	if c.Elided() != 0 {
		t.Fatalf("Take counted a loss: %d", c.Elided())
	}
	c.Elide(len(batch))
	if c.Elided() != len(batch) {
		t.Errorf("Elided() = %d, want %d", c.Elided(), len(batch))
	}

	// Nonsense counts are ignored rather than corrupting the total.
	c.Elide(0)
	c.Elide(-5)
	if c.Elided() != len(batch) {
		t.Errorf("Elided() = %d after no-op calls", c.Elided())
	}
}
