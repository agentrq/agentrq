package mcp

import (
	"runtime"
	"testing"
	"time"
)

// Manager.Remove must Close the server so its background goroutines stop, rather than
// just dropping the map entry and leaking them.
func TestManagerRemoveClosesServer(t *testing.T) {
	ws := &WorkspaceServer{done: make(chan struct{})}
	m := NewManager(func(workspaceID int64, userID string) *WorkspaceServer { return ws })

	m.Get(1, "user")
	m.Remove(1)

	select {
	case <-ws.done:
		// closed as expected
	default:
		t.Fatal("Remove did not Close the server (done channel not closed)")
	}

	ws.Close() // idempotent: must not panic on a second close
}

// Close must actually stop the StartPing ticker goroutine (previously it ran forever).
func TestStartPingStopsOnClose(t *testing.T) {
	ws := &WorkspaceServer{done: make(chan struct{})}

	before := runtime.NumGoroutine()
	ws.StartPing() // launches one ticker goroutine (60s tick; it will not fire in this test)
	ws.Close()     // should cause the goroutine to return via <-ps.done

	deadline := time.Now().Add(2 * time.Second)
	for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if runtime.NumGoroutine() > before {
		t.Fatalf("StartPing goroutine did not exit after Close (before=%d, now=%d)", before, runtime.NumGoroutine())
	}
}
