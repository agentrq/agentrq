// Package mcp provides a dynamic per-workspace MCP server manager.
package mcp

import (
	"context"
	"fmt"
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

// Remove tears down a workspace's MCP server, stopping its background goroutines.
func (m *Manager) Remove(workspaceID int64) {
	m.mu.Lock()
	srv := m.servers[workspaceID]
	delete(m.servers, workspaceID)
	m.mu.Unlock()
	if srv != nil {
		srv.Close() // stop StartPing/StartPoller so they don't leak
	}
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

// SendPermissionVerdict dispatches a permission request verdict to the appropriate WorkspaceServer.
func (m *Manager) SendPermissionVerdict(ctx context.Context, workspaceID int64, userID string, taskID int64, requestID, behavior string) error {
	m.mu.RLock()
	srv, ok := m.servers[workspaceID]
	m.mu.RUnlock()
	if !ok {
		// If the server isn't running or loaded, try loading it using Get
		srv = m.Get(workspaceID, userID)
	}
	if srv == nil {
		return fmt.Errorf("mcp manager: workspace server %d not found or couldn't be loaded", workspaceID)
	}
	return srv.SendPermissionVerdict(ctx, taskID, requestID, behavior)
}

// SendChannelNotification forwards a channel notification to the appropriate WorkspaceServer.
func (m *Manager) SendChannelNotification(ctx context.Context, workspaceID int64, userID string, taskID int64, content string) {
	m.mu.RLock()
	srv, ok := m.servers[workspaceID]
	m.mu.RUnlock()
	if !ok {
		srv = m.Get(workspaceID, userID)
	}
	if srv != nil {
		srv.SendChannelNotification(ctx, taskID, content)
	}
}
