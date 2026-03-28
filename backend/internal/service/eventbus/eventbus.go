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
	mu          sync.RWMutex
	subscribers map[int64][]chan []byte // workspaceID → channels
}

func New() *Bus {
	return &Bus{subscribers: make(map[int64][]chan []byte)}
}

// Subscribe returns a channel that receives SSE-formatted data lines.
// The caller must call Unsubscribe with the same channel when done.
func (b *Bus) Subscribe(workspaceID int64) chan []byte {
	ch := make(chan []byte, 32)
	b.mu.Lock()
	b.subscribers[workspaceID] = append(b.subscribers[workspaceID], ch)
	b.mu.Unlock()
	return ch
}

// Unsubscribe removes the channel and closes it.
func (b *Bus) Unsubscribe(workspaceID int64, ch chan []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	subs := b.subscribers[workspaceID]
	for i, s := range subs {
		if s == ch {
			b.subscribers[workspaceID] = append(subs[:i], subs[i+1:]...)
			close(ch)
			return
		}
	}
}

// Publish sends an event to all subscribers of the given workspace and global ones.
func (b *Bus) Publish(workspaceID int64, evt Event) {
	data, err := json.Marshal(evt)
	if err != nil {
		return
	}
	line := append([]byte("data: "), data...)
	line = append(line, '\n', '\n')

	b.mu.RLock()
	defer b.mu.RUnlock()
	
	// Send to specific workspace subscribers
	for _, ch := range b.subscribers[workspaceID] {
		select {
		case ch <- line:
		default:
			// drop if slow consumer
		}
	}

	// Also send to global subscribers (workspaceID 0) if it's not already global
	if workspaceID != 0 {
		for _, ch := range b.subscribers[0] {
			select {
			case ch <- line:
			default:
				// drop if slow consumer
			}
		}
	}
}
