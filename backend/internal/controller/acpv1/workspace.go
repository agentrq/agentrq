// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package acpv1

import (
	"context"
	"errors"

	mcpctrl "github.com/agentrq/agentrq/backend/internal/controller/mcp"
)

// TokenAuth resolves a bearer token to the workspace and user it was minted for. app implements it from the token service and the CRUD controller.
type TokenAuth interface {
	WorkspaceForToken(ctx context.Context, token string) (workspaceID int64, userID string, err error)
}

var (
	ErrUnauthorized = errors.New("not a workspace access token")
	ErrForbidden    = errors.New("no access to this workspace")
	ErrArchived     = errors.New("workspace is archived")
)

// Workspace is the part of the workspace's MCP server this package uses.
// *mcp.WorkspaceServer satisfies it. It is declared here, not shared, so acpv1
// depends on no other ACP package.
type Workspace interface {
	AttachAgentLink(link mcpctrl.AgentLink)
	DetachAgentLink(ctx context.Context, id string)
	HandleAgentIdentity(ctx context.Context, sessionID string, p mcpctrl.AgentIdentityParams)
	HandleAgentModels(ctx context.Context, sessionID string, p mcpctrl.AgentModelsParams)
	HandleAgentEffort(ctx context.Context, sessionID string, p mcpctrl.AgentEffortParams)
	HandleAgentCommands(ctx context.Context, sessionID string, p mcpctrl.AgentCommandsParams)
	HandleAgentConcurrency(ctx context.Context, sessionID string, p mcpctrl.AgentConcurrencyParams)
	HandleAgentTelemetry(ctx context.Context, sessionID string, p mcpctrl.AgentTelemetryParams)
	HandlePermissionRequest(ctx context.Context, linkID string, p mcpctrl.PermissionRequestParams)
	ClosePermissionRequest(ctx context.Context, requestID string)
	PostAgentReply(ctx context.Context, linkID string, taskID int64, text string) (int64, error)
	AskHuman(ctx context.Context, taskID int64, q mcpctrl.Question) (mcpctrl.Answer, error)
	CreateHumanTask(ctx context.Context, title, body string) (int64, error)
	MarkTaskOngoing(ctx context.Context, linkID string, taskID int64) error
	OfferNextTask(ctx context.Context) int64
	ToolNames() map[string]struct{}
}

var _ Workspace = (*mcpctrl.WorkspaceServer)(nil)
