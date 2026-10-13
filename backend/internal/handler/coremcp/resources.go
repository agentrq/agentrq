// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package coremcp

import (
	"context"

	mcpevent "github.com/agentrq/agentrq/backend/internal/controller/mcp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	newWorkspaceGuideURI  = "agentrq://guides/new-workspace"
	agentrqdSetupGuideURI = "agentrq://guides/agentrqd-setup"

	// The install script and docs live at these URLs regardless of which
	// AgentRQ server answers this request — see
	// frontend/src/composables/useDaemonInstall.js, the source of truth for
	// installation. Keep the two in step if either one moves; the actual
	// install/run steps are deliberately not duplicated here (see
	// docs/agents/machines-and-daemon.md, "Installation is answered in three
	// places, and they must agree" — a fourth prose copy in Go is the mistake
	// that note exists to avoid).
	daemonDocsURL = "https://agentrq.com/docs/daemon"
)

// registerResources exposes reference material a supervisor agent can pull
// into context on its own, alongside the tools that act on it.
func (s *WorkspaceServer) registerResources() {
	s.server.AddResource(&mcp.Resource{
		URI:         newWorkspaceGuideURI,
		Name:        "new-workspace-guide",
		Title:       "Creating a new workspace",
		Description: "How to set up a new AgentRQ workspace for an agent to run in, from the supervisor",
		MIMEType:    "text/markdown",
	}, s.readNewWorkspaceGuide)

	s.server.AddResource(&mcp.Resource{
		URI:         agentrqdSetupGuideURI,
		Name:        "agentrqd-setup-guide",
		Title:       "Setting up agentrqd on a machine",
		Description: "How to install agentrqd and enrol a machine so a workspace can run agents there",
		MIMEType:    "text/markdown",
	}, s.readAgentrqdSetupGuide)
}

func (s *WorkspaceServer) readNewWorkspaceGuide(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	s.emitTelemetry(ctx, mcpevent.ActionMCPMethodCall, "resource:new-workspace-guide", 0)
	text := "# Creating a new workspace\n\n" +
		"A workspace is where a single agent runs, with its own tasks and memory.\n\n" +
		"1. Call **workspace** action `list` first — check one for the same purpose doesn't already exist.\n" +
		"2. Call **workspace** action `create` with a short, descriptive `name` (and an optional `description`).\n" +
		"3. Call **workspace** action `get` on the new workspace's id for its `mcpUrl`. A human still has to open that " +
		"workspace's Settings page in the web app to connect an agent to it — creating the workspace here " +
		"does not by itself start one or write its `.mcp.json`.\n" +
		"4. Once an agent is connected, hand it its first task with **task** action `create`.\n"

	return &mcp.ReadResourceResult{
		Contents: []*mcp.ResourceContents{
			{URI: req.Params.URI, MIMEType: "text/markdown", Text: text},
		},
	}, nil
}

func (s *WorkspaceServer) readAgentrqdSetupGuide(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	s.emitTelemetry(ctx, mcpevent.ActionMCPMethodCall, "resource:agentrqd-setup-guide", 0)
	server := s.baseURL
	if server == "" {
		server = "<server>"
	}
	enrolCommand := "agentrqd enroll --server " + server + " --code <code>"

	text := "# Setting up agentrqd on a machine\n\n" +
		"agentrqd runs agents on somebody's own machine. There is no remote enrolment — a human has to be " +
		"at the target machine to run the install and enrol commands themselves; this guide is for telling " +
		"them what to run, not for running it on their behalf.\n\n" +
		"1. Install it: the one-line installer for the machine's platform is at " + daemonDocsURL + ".\n" +
		"2. Get a one-time enrolment code by calling the **machines** tool's `createEnrolmentCode` action. It is shown once and " +
		"expires shortly, so mint it right before the human needs it.\n" +
		"3. Enrol the machine, filling in the code from step 2:\n   `" + enrolCommand + "`\n" +
		"4. Run it: `agentrqd serve` (see " + daemonDocsURL + " for keeping it running after logout).\n"

	return &mcp.ReadResourceResult{
		Contents: []*mcp.ResourceContents{
			{URI: req.Params.URI, MIMEType: "text/markdown", Text: text},
		},
	}, nil
}
