// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package acpv1

import (
	"fmt"
	"slices"
	"sync"
	"testing"
)

// starts records the turns a queue starts, as "<task><content>", and how
// often it reported its numbers or asked for work.
type starts struct {
	mu    sync.Mutex
	got   []string
	moves int
	rooms int
	queue *turnQueue
}

func newStarts(limit int) *starts {
	s := &starts{}
	s.queue = newTurnQueue(limit, func(d delivery) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.got = append(s.got, fmt.Sprintf("%d%s", d.taskID, d.content))
	}, func() { s.mu.Lock(); s.moves++; s.mu.Unlock() }, func() { s.mu.Lock(); s.rooms++; s.mu.Unlock() })
	return s
}

func (s *starts) started() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := slices.Clone(s.got)
	slices.Sort(out)
	return out
}

func (s *starts) roomAsks() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rooms
}

// waitStarted waits until exactly the turns want have started, in any order.
func (s *starts) waitStarted(t *testing.T, want ...string) {
	t.Helper()
	slices.Sort(want)
	waitFor(t, func() bool { return slices.Equal(s.started(), want) })
}

func (s *starts) numbers(t *testing.T, limit, active, queued int) {
	t.Helper()
	if l, a, q := s.queue.numbers(); l != limit || a != active || q != queued {
		t.Fatalf("numbers were (limit %d, active %d, queued %d), want (%d, %d, %d)", l, a, q, limit, active, queued)
	}
}

func TestTurnQueue(t *testing.T) {
	t.Run("a delivery starts at once when a slot is free", func(t *testing.T) {
		s := newStarts(1)
		s.queue.deliver(1, "a")
		s.waitStarted(t, "1a")
		s.numbers(t, 1, 1, 0)
	})

	t.Run("a second task waits for the slot", func(t *testing.T) {
		s := newStarts(1)
		s.queue.deliver(1, "a")
		s.queue.deliver(2, "b")
		s.waitStarted(t, "1a")
		s.numbers(t, 1, 1, 1)
		s.queue.finish(1)
		s.waitStarted(t, "1a", "2b")
	})

	t.Run("a second message for a running task waits for the turn and does not hold a slot", func(t *testing.T) {
		s := newStarts(2)
		s.queue.deliver(1, "a")
		s.queue.deliver(1, "b")
		s.queue.deliver(2, "c")
		s.waitStarted(t, "1a", "2c")
		s.numbers(t, 2, 2, 0)
		s.queue.finish(1)
		s.waitStarted(t, "1a", "2c", "1b")
	})

	t.Run("a byte-identical repeat for a queued task is dropped", func(t *testing.T) {
		s := newStarts(1)
		s.queue.deliver(1, "a")
		s.queue.deliver(1, "a")
		s.waitStarted(t, "1a")
		s.queue.finish(1)
		s.numbers(t, 1, 0, 0)
		if s.queue.owns(1) {
			t.Fatal("the task still had a chain after its only turn finished; the repeat was queued")
		}
	})

	t.Run("the same text after the task finished runs again", func(t *testing.T) {
		s := newStarts(1)
		s.queue.deliver(1, "a")
		s.waitStarted(t, "1a")
		s.queue.finish(1)
		s.queue.deliver(1, "a")
		s.waitStarted(t, "1a", "1a")
	})

	t.Run("raising the limit starts every waiting turn that fits", func(t *testing.T) {
		s := newStarts(1)
		s.queue.deliver(1, "a")
		s.queue.deliver(2, "b")
		s.queue.deliver(3, "c")
		s.waitStarted(t, "1a")
		s.queue.setLimit(3)
		s.waitStarted(t, "1a", "2b", "3c")
		s.numbers(t, 3, 3, 0)
	})

	t.Run("lowering the limit interrupts nothing", func(t *testing.T) {
		s := newStarts(3)
		for i := int64(1); i <= 4; i++ {
			s.queue.deliver(i, "x")
		}
		s.waitStarted(t, "1x", "2x", "3x")
		s.queue.setLimit(1)
		s.numbers(t, 1, 3, 1)
		s.queue.finish(1)
		s.numbers(t, 1, 2, 1)
		s.queue.finish(2)
		s.numbers(t, 1, 1, 1)
		s.queue.finish(3)
		s.waitStarted(t, "1x", "2x", "3x", "4x")
	})

	t.Run("the limit stays between 1 and 64", func(t *testing.T) {
		s := newStarts(0)
		s.numbers(t, 1, 0, 0)
		s.queue.setLimit(500)
		s.numbers(t, 64, 0, 0)
		s.queue.setLimit(-3)
		s.numbers(t, 1, 0, 0)
	})

	t.Run("a cancel skips what was queued before it and not what came after", func(t *testing.T) {
		s := newStarts(1)
		s.queue.deliver(1, "a") // seq 1
		if !s.queue.cancel(1) {
			t.Fatal("cancel reported no turn for a task with one running")
		}
		s.queue.deliver(1, "b") // seq 3
		if !s.queue.cancelledSince(1, 1) {
			t.Error("the delivery queued before the cancel was not skipped")
		}
		if s.queue.cancelledSince(1, 3) {
			t.Error("the delivery queued after the cancel was skipped")
		}
	})

	t.Run("a cancel for a task with nothing queued records nothing", func(t *testing.T) {
		s := newStarts(1)
		if s.queue.cancel(7) {
			t.Fatal("cancel reported a turn for a task the queue never saw")
		}
		if s.queue.owns(7) || s.queue.cancelledSince(7, 0) {
			t.Fatal("a cancel for an unknown task left something behind")
		}
	})

	t.Run("a freed slot with nothing waiting asks for work", func(t *testing.T) {
		s := newStarts(1)
		s.queue.deliver(1, "a")
		s.waitStarted(t, "1a")
		s.queue.finish(1)
		if got := s.roomAsks(); got != 1 {
			t.Fatalf("the queue asked for work %d times, want once", got)
		}
	})

	t.Run("a freed slot with a turn waiting asks for nothing", func(t *testing.T) {
		s := newStarts(1)
		s.queue.deliver(1, "a")
		s.queue.deliver(2, "b")
		s.queue.finish(1)
		if got := s.roomAsks(); got != 0 {
			t.Fatalf("the queue asked for work %d times, want none while a turn waits", got)
		}
	})

	t.Run("nothing is kept about finished tasks", func(t *testing.T) {
		s := newStarts(64)
		for i := int64(1); i <= 300; i++ {
			s.queue.deliver(i, "x")
			s.queue.cancel(i)
			s.queue.finish(i)
		}
		s.queue.mu.Lock()
		n := len(s.queue.chains)
		s.queue.mu.Unlock()
		if n != 0 {
			t.Fatalf("%d chains were kept for 300 finished tasks, want none", n)
		}
	})
}
