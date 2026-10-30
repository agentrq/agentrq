// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// A verdict for a workspace whose server is not loaded loads it; one whose
// server cannot be built is reported, not dropped.
func TestManager_SendPermissionVerdict(t *testing.T) {
	t.Run("a server that cannot be built", func(t *testing.T) {
		m := NewManager(func(int64, string) *WorkspaceServer { return nil })
		err := m.SendPermissionVerdict(context.Background(), 1, "user", 7, "req-1", "allow")
		if err == nil || !strings.Contains(err.Error(), "workspace server 1 not found") {
			t.Fatalf("err = %v, want workspace server 1 not found", err)
		}
	})

	t.Run("a server loaded on demand", func(t *testing.T) {
		replies := 0
		ps := permissionServer(t, &replies)
		m := NewManager(func(int64, string) *WorkspaceServer { return ps })

		err := m.SendPermissionVerdict(context.Background(), 1, "user", 7, "req-unknown", "allow")
		if err == nil || !strings.Contains(err.Error(), "unknown request ID") {
			t.Fatalf("err = %v, want the server's own unknown request ID", err)
		}
		if m.Get(1, "user") != ps {
			t.Error("the server loaded for the verdict was not kept")
		}
	})
}

// The agent snapshots are nil for a workspace with no server, and the
// server's own otherwise.
func TestManager_AgentSnapshots(t *testing.T) {
	m := NewManager(func(workspaceID int64, userID string) *WorkspaceServer {
		return &WorkspaceServer{workspaceID: workspaceID, userID: userID}
	})
	if m.AgentModels(1) != nil || m.AgentClient(1) != nil {
		t.Fatal("a workspace with no server reported an agent")
	}

	ps := m.Get(1, "user")
	ps.mcpServer = mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	if got := m.AgentModels(1); got != ps.AgentModels() {
		t.Errorf("AgentModels = %+v, want the server's %+v", got, ps.AgentModels())
	}
	if got := m.AgentClient(1); got != ps.AgentClient() {
		t.Errorf("AgentClient = %+v, want the server's %+v", got, ps.AgentClient())
	}
}

func TestAction_String(t *testing.T) {
	for action, want := range map[Action]string{
		ActionMCPToolCall:     "tool_call",
		ActionMCPMethodCall:   "method_call",
		ActionMCPNotification: "notification",
		ActionMCPConnect:      "connect",
		ActionMCPClearContext: "clear_context",
		ActionMCPPlanUpdate:   "plan_update",
		Action(250):           "unknown",
	} {
		if got := action.String(); got != want {
			t.Errorf("Action(%d).String() = %q, want %q", action, got, want)
		}
	}
	if got := (MCPEvent{Action: ActionMCPConnect}).String(); got != "mcp_event:connect" {
		t.Errorf("MCPEvent.String() = %q, want mcp_event:connect", got)
	}
}

func TestClientIdentityFromRequest_NoRequestIsNobody(t *testing.T) {
	if got := clientIdentityFromRequest(nil); got != (clientIdentity{}) {
		t.Errorf("a nil request identified %+v", got)
	}
}
