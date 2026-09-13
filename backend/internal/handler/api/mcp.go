// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package api

import (
	"context"
	"time"

	mcpctrl "github.com/agentrq/agentrq/backend/internal/controller/mcp"
)

// The handler's view of the MCP controller, narrowed to what it actually uses.
//
// Declared here, in the package that consumes them, rather than beside the
// implementations: these are the handler's requirements, and a workspace server
// should not have to know that an HTTP layer exists in order to describe
// itself. It also means adding a method to the controller does not widen this
// surface by accident — the interface only grows when a handler reaches for
// something new.
//
// The reason they exist at all is testability. Both are satisfied in production
// by exactly one type, and neither is a plugin point: what they buy is a
// handler test that can drive a *successful* call. Before this, `Get` handed
// back a concrete `*mcpctrl.WorkspaceServer` whose fields are unexported, and
// the only way to obtain one that would accept a model or a concurrency change
// was to stand up a live MCP session — which needs unexported test helpers from
// the controller's own package. So every handler test could reach the refusals
// and none could reach the success, and the two routes that ask an agent to do
// something sat at 71.4% and 88.5% with their happy paths untested.

type (
	// mcpManager is the part of mcpctrl.Manager this package uses.
	mcpManager interface {
		// Get returns the workspace's server, or nil if there is none.
		//
		// Callers test the result against nil and several act on it without
		// testing at all, so an implementation must return a genuinely nil
		// interface rather than a nil pointer inside one. See liveMCPManager.Get.
		Get(workspaceID int64, userID string) workspaceServer
		Remove(workspaceID int64)
		IsAgentConnected(workspaceID int64) bool
		SupportsStop(workspaceID int64) bool
		AgentModels(workspaceID int64) *mcpctrl.AgentModelsSnapshot
		AgentCommands(workspaceID int64) *mcpctrl.AgentCommandsSnapshot
		AgentClient(workspaceID int64) *mcpctrl.AgentClientInfo
		AgentConcurrency(workspaceID int64) *mcpctrl.AgentConcurrencySnapshot
	}

	// workspaceServer is the part of one workspace's MCP server this package
	// uses: everything the HTTP layer asks a connected agent to do, plus the
	// workspace metadata a running server has to be told about.
	workspaceServer interface {
		SendSetModelNotification(ctx context.Context, modelID string) error
		SendSetConcurrencyNotification(ctx context.Context, limit int) error
		SendChannelNotification(ctx context.Context, taskID int64, content string)
		SendPermissionVerdictFrom(ctx context.Context, taskID int64, requestID, behavior, decidedBy string) error
		SendCancelNotification(ctx context.Context, taskID int64) mcpctrl.StopOutcome
		RespondToElicitation(requestID, action string, content map[string]any) error
		UpdateArchivedAt(at *time.Time)
		UpdateMetadata(name, description, icon string)
		UpdateAutoAllowedTools(tools []string)
	}
)

// liveMCPManager is the real manager, seen through the interface above.
//
// The wrapper exists for one narrow reason: Go has no covariant return types.
// *mcpctrl.Manager already has every method mcpManager names, but its Get
// returns *mcpctrl.WorkspaceServer where the interface says workspaceServer —
// and that alone means it does not satisfy it, however compatible the two are.
// Everything other than Get is promoted from the embedded manager unchanged.
type liveMCPManager struct {
	*mcpctrl.Manager
}

// Get returns the workspace's server, and turns "no server" into a nil
// interface.
//
// This conversion is the whole reason the method is written out rather than
// promoted, and removing it would be silent. `return l.Manager.Get(...)` would
// wrap a nil *WorkspaceServer in a non-nil interface value, because an
// interface holding a typed nil is not nil. Seven call sites across this
// package guard their work with `srv != nil` or `srv == nil`; every one of them
// would quietly invert, and the first method called on the nil pointer behind
// the interface panics on its first field access.
//
// In production the manager builds a server on demand and this is never nil.
// The path exists because a caller may hold a manager that cannot build one,
// which is precisely the state the handler's 404 answers describe.
func (l liveMCPManager) Get(workspaceID int64, userID string) workspaceServer {
	srv := l.Manager.Get(workspaceID, userID)
	if srv == nil {
		return nil
	}
	return srv
}
