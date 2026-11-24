// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package mcp

import (
	"context"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/service/auth"
	"github.com/agentrq/agentrq/backend/internal/service/mcp"
)

// A workspace server's session id reads back as the handshake it recorded,
// on that workspace for that user, and nowhere else.
func TestSessionIDs_RecordTheHandshakeForThisWorkspaceOnly(t *testing.T) {
	tokens := auth.NewTokenService(auth.TokenConfig{APISecret: "api", MCPSecret: "mcp"})
	ids := sessionIDs{tokens: tokens, userID: "user-1", workspaceID: "ws-1"}
	ctx := context.Background()

	id, err := ids.Issue(ctx, mcp.SessionInfo{ProtocolVersion: "2025-11-25", ClientName: "claude-code", ClientVersion: "2.1"})
	if err != nil {
		t.Fatal(err)
	}
	info, err := ids.Resolve(ctx, id)
	if err != nil {
		t.Fatalf("its own session id was refused: %v", err)
	}
	if info.ProtocolVersion != "2025-11-25" || info.ClientName != "claude-code" || info.ClientVersion != "2.1" {
		t.Errorf("read back %+v, want the handshake", info)
	}

	for name, other := range map[string]sessionIDs{
		"another workspace": {tokens: tokens, userID: "user-1", workspaceID: "ws-2"},
		"another user":      {tokens: tokens, userID: "user-2", workspaceID: "ws-1"},
	} {
		if _, err := other.Resolve(ctx, id); err == nil {
			t.Errorf("%s accepted the session id", name)
		}
	}
}
