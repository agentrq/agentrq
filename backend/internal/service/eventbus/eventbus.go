// Package eventbus provides a simple per-workspace SSE event broadcaster.
// Human clients subscribe to workspace events; the MCP layer publishes them.
package eventbus

import (
	"encoding/json"
	"sync"
)

// Event is a named payload pushed to SSE subscribers.
type Event struct {
	Type    string `json:"type"` // "task.created" | "reply.received" | "respond.ack"
	Payload any    `json:"payload"`
}

type Bus struct {
	mu            sync.RWMutex
	workspaceSubs map[int64][]chan []byte // workspaceID → channels
	userSubs      map[string][]chan []byte // userID → channels
}

func New() *Bus {
	return &Bus{
		workspaceSubs: make(map[int64][]chan []byte),
		userSubs:      make(map[string][]chan []byte),
	}
}

// Subscribe returns a channel that receives SSE-formatted data lines.
// The caller must call Unsubscribe with the same channel when done.
func (b *Bus) Subscribe(workspaceID int64, userID string) chan []byte {
	ch := make(chan []byte, 32)
	b.mu.Lock()
	if workspaceID == 0 {
		b.userSubs[userID] = append(b.userSubs[userID], ch)
	} else {
		b.workspaceSubs[workspaceID] = append(b.workspaceSubs[workspaceID], ch)
	}
	b.mu.Unlock()
	return ch
}

// Unsubscribe removes the channel and closes it.
func (b *Bus) Unsubscribe(workspaceID int64, userID string, ch chan []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	
	if workspaceID == 0 {
		subs := b.userSubs[userID]
		for i, s := range subs {
			if s == ch {
				b.userSubs[userID] = append(subs[:i], subs[i+1:]...)
				close(ch)
				return
			}
		}
	} else {
		subs := b.workspaceSubs[workspaceID]
		for i, s := range subs {
			if s == ch {
				b.workspaceSubs[workspaceID] = append(subs[:i], subs[i+1:]...)
				close(ch)
				return
			}
		}
	}
}

// Publish sends an event to all subscribers of the given workspace and the specific user's global stream.
func (b *Bus) Publish(workspaceID int64, userID string, evt Event) {
	data, err := json.Marshal(evt)
	if err != nil {
		return
	}
	line := append([]byte("data: "), data...)
	line = append(line, '\n', '\n')

	b.mu.RLock()
	defer b.mu.RUnlock()
	
	// Send to specific workspace subscribers
	if workspaceID != 0 {
		for _, ch := range b.workspaceSubs[workspaceID] {
			select {
			case ch <- line:
			default:
				// drop if slow consumer
			}
		}
	}

	// Also send to global subscribers for this user
	if userID != "" {
		for _, ch := range b.userSubs[userID] {
			select {
			case ch <- line:
			default:
				// drop if slow consumer
			}
		}
	}
}
