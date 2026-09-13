// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package api

import (
	"context"
	"testing"
	"time"

	mcpctrl "github.com/agentrq/agentrq/backend/internal/controller/mcp"
)

// The doubles the handler tests inject through the mcpManager seam.
//
// Both are deliberately dumb: they record what they were asked and answer with
// whatever the test set. Nothing here simulates a gateway — the point of the
// seam is that a handler test does not have to.

// fakeMCPManager hands back one server for every workspace.
//
// `server` left as the zero value is a genuinely nil interface, which is what
// "no server for this workspace" has to look like for the handler's nil checks
// to mean anything. Note the field is the interface rather than a concrete
// pointer: a test that stored a nil *fakeWorkspaceServer here would hand back a
// non-nil interface wrapping a nil pointer, which is the exact trap
// liveMCPManager.Get exists to avoid.
type fakeMCPManager struct {
	server workspaceServer

	models      *mcpctrl.AgentModelsSnapshot
	commands    *mcpctrl.AgentCommandsSnapshot
	client      *mcpctrl.AgentClientInfo
	concurrency *mcpctrl.AgentConcurrencySnapshot
	connected   bool
	supportStop bool

	removed []int64
}

func (f *fakeMCPManager) Get(workspaceID int64, userID string) workspaceServer { return f.server }
func (f *fakeMCPManager) Remove(workspaceID int64)                             { f.removed = append(f.removed, workspaceID) }
func (f *fakeMCPManager) IsAgentConnected(workspaceID int64) bool              { return f.connected }
func (f *fakeMCPManager) SupportsStop(workspaceID int64) bool                  { return f.supportStop }

func (f *fakeMCPManager) AgentModels(workspaceID int64) *mcpctrl.AgentModelsSnapshot {
	return f.models
}

func (f *fakeMCPManager) AgentCommands(workspaceID int64) *mcpctrl.AgentCommandsSnapshot {
	return f.commands
}

func (f *fakeMCPManager) AgentClient(workspaceID int64) *mcpctrl.AgentClientInfo { return f.client }

func (f *fakeMCPManager) AgentConcurrency(workspaceID int64) *mcpctrl.AgentConcurrencySnapshot {
	return f.concurrency
}

// fakeWorkspaceServer records what the handler asked a connected agent to do.
//
// The zero value answers every request the way a server with nothing connected
// does: no error from the notifications, and a StopOutcome that did nothing —
// which `Acted()` reads as false.
type fakeWorkspaceServer struct {
	modelID    string
	modelCalls int
	modelErr   error

	limit            int
	concurrencyCalls int
	concurrencyErr   error

	stopOutcome mcpctrl.StopOutcome
}

func (f *fakeWorkspaceServer) SendSetModelNotification(ctx context.Context, modelID string) error {
	f.modelCalls++
	f.modelID = modelID
	return f.modelErr
}

func (f *fakeWorkspaceServer) SendSetConcurrencyNotification(ctx context.Context, limit int) error {
	f.concurrencyCalls++
	f.limit = limit
	return f.concurrencyErr
}

func (f *fakeWorkspaceServer) SendCancelNotification(ctx context.Context, taskID int64) mcpctrl.StopOutcome {
	return f.stopOutcome
}

// The rest of the interface, which these tests do not exercise. Present because
// the handler needs them, not because anything here asserts on them.

func (f *fakeWorkspaceServer) SendChannelNotification(ctx context.Context, taskID int64, content string) {
}

func (f *fakeWorkspaceServer) SendPermissionVerdictFrom(ctx context.Context, taskID int64, requestID, behavior, decidedBy string) error {
	return nil
}

func (f *fakeWorkspaceServer) RespondToElicitation(requestID, action string, content map[string]any) error {
	return nil
}

func (f *fakeWorkspaceServer) UpdateArchivedAt(at *time.Time)                {}
func (f *fakeWorkspaceServer) UpdateMetadata(name, description, icon string) {}
func (f *fakeWorkspaceServer) UpdateAutoAllowedTools(tools []string)         {}

// The adapter's one job, and the reason it is written out rather than promoted.
//
// A manager that builds no server returns a nil *WorkspaceServer. Returned
// straight through, that becomes an interface holding a typed nil — not nil
// itself — and the seven `srv == nil` / `srv != nil` guards in this package all
// silently invert. What follows is a method call on a nil pointer that panics
// on its first field access, so the failure would surface far from its cause.
func TestLiveMCPManagerGet(t *testing.T) {
	t.Run("no server is a nil interface, not a typed nil", func(t *testing.T) {
		m := liveMCPManager{mcpctrl.NewManager(
			func(workspaceID int64, userID string) *mcpctrl.WorkspaceServer { return nil },
		)}

		got := m.Get(1, "user")
		if got != nil {
			t.Fatalf("Get() = %#v, want an interface that compares equal to nil", got)
		}
	})

	t.Run("a real server is handed back", func(t *testing.T) {
		srv := &mcpctrl.WorkspaceServer{}
		m := liveMCPManager{mcpctrl.NewManager(
			func(workspaceID int64, userID string) *mcpctrl.WorkspaceServer { return srv },
		)}

		got := m.Get(1, "user")
		if got == nil {
			t.Fatal("Get() = nil, want the server the manager built")
		}
		if got != workspaceServer(srv) {
			t.Errorf("Get() returned some other server: %#v", got)
		}
	})
}
