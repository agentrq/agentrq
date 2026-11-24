// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package mcp

import (
	"context"
	"embed"

	"github.com/agentrq/agentrq/backend/internal/service/mcp"
	"github.com/agentrq/agentrq/backend/internal/service/mcpskill"
)

// builtinSkillFiles are the skills every workspace server serves over the
// MCP Skills extension, so an agent needs no plugin to get them.
// skills/agentrq is the agentrq-workspace plugins' skill, kept identical to
// theirs by coremcp's plugin_docs_test.go.
//
//go:embed skills
var builtinSkillFiles embed.FS

// builtinSkills are the built-in skills, loaded once.
var builtinSkills = mcpskill.MustLoad(builtinSkillFiles, "skills")

// serveBuiltinSkills serves the built-in skills on srv, counting each call
// against this workspace.
func (ps *WorkspaceServer) serveBuiltinSkills(srv *mcp.Server) {
	builtinSkills.Serve(srv, func(ctx context.Context, method string) {
		ps.emitTelemetry(ctx, ActionMCPMethodCall, method, clientIdentity{})
	})
}
