// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package mcp

import (
	"context"

	"github.com/agentrq/agentrq/backend/internal/service/auth"
	"github.com/agentrq/agentrq/backend/internal/service/mcp"
)

// sessionIDs mints this workspace server's MCP session ids, bound to the
// workspace and the user it serves: an id minted here is refused anywhere
// else, and the request carrying it has been authenticated by then.
type sessionIDs struct {
	tokens      auth.TokenService
	userID      string
	workspaceID string
}

func (s sessionIDs) Issue(_ context.Context, info mcp.SessionInfo) (string, error) {
	return s.tokens.CreateMCPSessionToken(s.userID, s.workspaceID, auth.MCPSession{
		ProtocolVersion: info.ProtocolVersion,
		ClientName:      info.ClientName,
		ClientVersion:   info.ClientVersion,
	})
}

func (s sessionIDs) Resolve(_ context.Context, id string) (mcp.SessionInfo, error) {
	session, err := s.tokens.ValidateMCPSessionToken(id, s.userID, s.workspaceID)
	if err != nil {
		return mcp.SessionInfo{}, err
	}
	return mcp.SessionInfo{
		ProtocolVersion: session.ProtocolVersion,
		ClientName:      session.ClientName,
		ClientVersion:   session.ClientVersion,
		ExpiresAt:       session.ExpiresAt,
	}, nil
}
