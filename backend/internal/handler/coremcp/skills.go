// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package coremcp

import (
	"context"
	"embed"

	"github.com/agentrq/agentrq/backend/internal/service/mcp"
	"github.com/agentrq/agentrq/backend/internal/service/mcpskill"

	mcpevent "github.com/agentrq/agentrq/backend/internal/controller/mcp"
)

// builtinSkillFiles are the skills the server serves, built into the binary.
// skills/agentrq is the supervisor skill the plugins ship, kept identical to
// theirs by plugin_docs_test.go.
//
//go:embed skills
var builtinSkillFiles embed.FS

// builtinSkills are the built-in skills, loaded once.
var builtinSkills = mcpskill.MustLoad(builtinSkillFiles, "skills")

// skillsCapabilities are what the mcp package infers, plus the Skills
// extension. Setting any capability drops the package's default logging one, so it is kept
// here to leave every other capability as it was.
func skillsCapabilities() *mcp.ServerCapabilities {
	caps := &mcp.ServerCapabilities{Logging: &mcp.LoggingCapabilities{}}
	mcpskill.AddCapability(caps)
	return caps
}

// registerSkills serves the built-in skills over the Skills extension.
func (s *WorkspaceServer) registerSkills() {
	s.skills.Serve(s.server, func(ctx context.Context, method string) {
		s.emitTelemetry(ctx, mcpevent.ActionMCPMethodCall, method, 0)
	})
}
