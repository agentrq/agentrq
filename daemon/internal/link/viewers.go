// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package link

import "sync"

// viewerCount tracks how many browsers are watching each session.
//
// Kept so the machine's own status report can say it. Somebody watching a
// terminal on this machine is the single fact most worth surfacing to the
// person sitting at it, and the backend knowing it is no help to them.
type viewerCount struct {
	mu sync.Mutex
	n  map[uint64]int
}

func newViewerCount() *viewerCount { return &viewerCount{n: map[uint64]int{}} }

func (v *viewerCount) join(session uint64) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.n[session]++
}

func (v *viewerCount) leave(session uint64) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.n[session] <= 1 {
		delete(v.n, session)
		return
	}
	v.n[session]--
}

func (v *viewerCount) get(session uint64) int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.n[session]
}

// Viewers is how many browsers are attached to a session's terminal.
func (l *Link) Viewers(session uint64) int { return l.viewers.get(session) }
