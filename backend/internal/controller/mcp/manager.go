// Package mcp provides a dynamic per-workspace MCP server manager.
package mcp

import (
	"sync"
)

// Manager holds a registry of per-workspace MCP servers.
type Manager struct {
	mu      sync.RWMutex
	servers map[int64]*WorkspaceServer
	newFn   func(workspaceID int64, userID string) *WorkspaceServer
}

func NewManager(newFn func(workspaceID int64, userID string) *WorkspaceServer) *Manager {
	return &Manager{
		servers: make(map[int64]*WorkspaceServer),
		newFn:   newFn,
	}
}


// Get returns an existing server or creates one lazily.
func (m *Manager) Get(workspaceID int64, userID string) *WorkspaceServer {
	m.mu.RLock()
	srv, ok := m.servers[workspaceID]
	m.mu.RUnlock()
	if ok {
		return srv
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	// double-check after acquiring write lock
	if srv, ok = m.servers[workspaceID]; ok {
		return srv
	}
	srv = m.newFn(workspaceID, userID)
	m.servers[workspaceID] = srv
	return srv
}

// Remove tears down a workspace's MCP server.
func (m *Manager) Remove(workspaceID int64) {
	m.mu.Lock()
	delete(m.servers, workspaceID)
	m.mu.Unlock()
}
// IsAgentConnected returns true if any agent is currently connected to the workspace server.
func (m *Manager) IsAgentConnected(workspaceID int64) bool {
	m.mu.RLock()
	srv, ok := m.servers[workspaceID]
	m.mu.RUnlock()
	if !ok {
		return false
	}
	return srv.IsAgentConnected()
}
