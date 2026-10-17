// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package acpv1

import (
	"container/list"
	"sync"

	"github.com/agentrq/agentrq/backend/internal/controller/agentlink"
)

// delivery is one message for a task, stamped with where it fell in the
// stream of events so a cancel only stops what was queued before it.
type delivery struct {
	taskID      int64
	content     string
	attachments []agentlink.Attachment
	seq         uint64
}

// chain is a task's turns: one running or waiting for a slot, the rest parked
// behind it. A parked delivery holds no slot, so a chatty task cannot take
// every slot while doing nothing (the gateway's runTurnForTask rule). The
// repeat guard and the cancel mark live here too, and go with the chain: both
// only matter while the task has a turn queued or running, so nothing about a
// finished task is kept.
type chain struct {
	parked    []delivery
	last      string // the content of the task's last delivery
	cancelled uint64 // seq of the task's last cancel, 0 for none
}

// turnQueue decides which turn runs when. Waiting turns are a FIFO
// (container/list: O(1) push and pop); chains are a map keyed by task id, so
// it holds exactly the tasks with a turn queued or running.
type turnQueue struct {
	mu      sync.Mutex
	limit   int
	active  int
	waiting *list.List // of delivery
	chains  map[int64]*chain
	seq     uint64
	start   func(delivery) // runs a turn on its own goroutine
	moved   func()         // the numbers moved: report them
	room    func()         // a slot is free with nothing waiting: ask for work
}

func newTurnQueue(limit int, start func(delivery), moved, room func()) *turnQueue {
	return &turnQueue{limit: clampLimit(limit), waiting: list.New(), chains: map[int64]*chain{},
		start: start, moved: moved, room: room}
}

func clampLimit(limit int) int { return min(max(limit, 1), maxConcurrency) }

// deliver queues a message. A byte-identical repeat of a task's last message
// while that task is still queued or running is dropped: the poller re-pushes
// pending tasks every minute by design.
func (q *turnQueue) deliver(taskID int64, content string, attachments ...agentlink.Attachment) {
	q.mu.Lock()
	ch := q.chains[taskID]
	if ch != nil && ch.last == content {
		q.mu.Unlock()
		return
	}
	q.seq++
	d := delivery{taskID: taskID, content: content, attachments: attachments, seq: q.seq}
	if ch != nil {
		ch.last = content
		ch.parked = append(ch.parked, d)
		q.mu.Unlock()
		return
	}
	q.chains[taskID] = &chain{last: content}
	q.waiting.PushBack(d)
	toStart := q.fillLocked()
	q.mu.Unlock()
	q.launch(toStart)
	q.moved()
}

// finish releases a finished turn's slot, moves its task's next parked
// message to the back of the FIFO, and starts whatever now fits.
func (q *turnQueue) finish(taskID int64) {
	q.mu.Lock()
	q.active--
	if ch := q.chains[taskID]; ch != nil {
		if len(ch.parked) > 0 {
			q.waiting.PushBack(ch.parked[0])
			ch.parked = ch.parked[1:]
		} else {
			delete(q.chains, taskID)
		}
	}
	toStart := q.fillLocked()
	idle := q.active < q.limit && q.waiting.Len() == 0
	q.mu.Unlock()
	q.launch(toStart)
	q.moved()
	if idle {
		q.room()
	}
}

// setLimit changes how many turns run at once. Raising starts every waiting
// turn that now fits; lowering interrupts nothing.
func (q *turnQueue) setLimit(limit int) {
	q.mu.Lock()
	q.limit = clampLimit(limit)
	toStart := q.fillLocked()
	idle := q.active < q.limit && q.waiting.Len() == 0
	q.mu.Unlock()
	q.launch(toStart)
	q.moved()
	if idle {
		q.room()
	}
}

// cancel records a cancel for a task and reports whether the task had a turn
// running or waiting. Deliveries queued before now are skipped when they come
// up; one queued after it runs. A task with nothing queued has nothing to
// skip, so nothing is recorded for it.
func (q *turnQueue) cancel(taskID int64) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	ch := q.chains[taskID]
	if ch == nil {
		return false
	}
	q.seq++
	ch.cancelled = q.seq
	return true
}

// cancelledSince reports whether the task was cancelled after delivery seq.
func (q *turnQueue) cancelledSince(taskID int64, seq uint64) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	ch := q.chains[taskID]
	return ch != nil && ch.cancelled > seq
}

// numbers reports the limit, how many run, and how many wait for a slot.
func (q *turnQueue) numbers() (limit, active, queued int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.limit, q.active, q.waiting.Len()
}

func (q *turnQueue) owns(taskID int64) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.chains[taskID] != nil
}

func (q *turnQueue) fillLocked() []delivery {
	var out []delivery
	for q.active < q.limit && q.waiting.Len() > 0 {
		d := q.waiting.Remove(q.waiting.Front()).(delivery)
		q.active++
		out = append(out, d)
	}
	return out
}

func (q *turnQueue) launch(ds []delivery) {
	for _, d := range ds {
		go q.start(d)
	}
}
