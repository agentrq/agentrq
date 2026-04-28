package mcp

import (
	"testing"
)

func TestManager(t *testing.T) {
	newCount := 0
	newFn := func(workspaceID int64, userID string) *WorkspaceServer {
		newCount++
		return &WorkspaceServer{workspaceID: workspaceID, userID: userID}
	}

	m := NewManager(newFn)

	// Test Get - new server
	srv1 := m.Get(1, "user1")
	if srv1 == nil {
		t.Fatal("expected server 1 to be not nil")
	}
	if srv1.workspaceID != 1 || srv1.userID != "user1" {
		t.Errorf("unexpected server 1 metadata: %d, %s", srv1.workspaceID, srv1.userID)
	}
	if newCount != 1 {
		t.Errorf("expected newFn to be called once, got %d", newCount)
	}

	// Test Get - existing server
	srv1Repeat := m.Get(1, "user1")
	if srv1Repeat != srv1 {
		t.Error("expected to get same server instance")
	}
	if newCount != 1 {
		t.Errorf("expected newFn not to be called again, got %d", newCount)
	}

	// Test Get - different workspace
	srv2 := m.Get(2, "user1")
	if srv2 == nil {
		t.Fatal("expected server 2 to be not nil")
	}
	if srv2.workspaceID != 2 {
		t.Errorf("expected workspace ID 2, got %d", srv2.workspaceID)
	}
	if newCount != 2 {
		t.Errorf("expected newFn to be called twice, got %d", newCount)
	}

	// Test IsAgentConnected - not found
	if m.IsAgentConnected(3) {
		t.Error("expected false for nonexistent server")
	}

	// Test IsAgentConnected - found
	srv1.agentConnections.Store(1)
	if !m.IsAgentConnected(1) {
		t.Error("expected true when agent is connected")
	}

	// Test Remove
	m.Remove(1)
	if m.IsAgentConnected(1) {
		t.Error("expected false after removal")
	}

	// Test Get after Remove - should create new
	srv1New := m.Get(1, "user1")
	if srv1New == srv1 {
		t.Error("expected a new server instance after removal")
	}
	if newCount != 3 {
		t.Errorf("expected newFn to be called three times, got %d", newCount)
	}
}
