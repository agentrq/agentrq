// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package coremcp

import (
	"context"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/agentrq/agentrq/backend/internal/controller/agentlaunch"
	mcpevent "github.com/agentrq/agentrq/backend/internal/controller/mcp"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/service/mcphint"
)

func (s *WorkspaceServer) registerPanoTools() {
	mcp.AddTool(s.server, &mcp.Tool{Name: "pano", Description: mcpevent.PanoToolDescription, Annotations: mcphint.Overwrite("Read and write Pano docs")}, s.handlePano)
}

// handlePano runs the workspace server's pano tool for the supervisor, which
// writes as the account's supervisor workspace.
func (s *WorkspaceServer) handlePano(ctx context.Context, req *mcp.CallToolRequest, args mcpevent.PanoParams) (*mcp.CallToolResult, any, error) {
	s.emitTelemetry(ctx, mcpevent.ActionMCPToolCall, "pano", 0)
	userID := getUserID(ctx)
	if userID == "" {
		return errorResponse(context.Canceled), nil, nil
	}
	res, err := s.crud.ListWorkspaces(ctx, entity.ListWorkspacesRequest{UserID: userID, IncludeArchived: true})
	if err != nil {
		return errorResponse(err), nil, nil
	}
	for _, w := range res.Workspaces {
		if w.Name == agentlaunch.SupervisorWorkspaceName && w.ForkOfID == 0 {
			return mcpevent.RunPano(entity.WithAgentWorkspace(ctx, w.ID), s.crud, userID, w.ID, args), nil, nil
		}
	}
	return errorResponse(errors.New("this account has no supervisor workspace to write the Pano as")), nil, nil
}
