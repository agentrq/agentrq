// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package coremcp

import (
	"context"
	"errors"
	"fmt"

	mcpevent "github.com/agentrq/agentrq/backend/internal/controller/mcp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerPrompts adds ready-made prompt templates for the workflows the
// supervisor exists for: standing up new workspaces and machines, and
// reporting on the ones that already exist, without the human having to spell
// out the same steps by hand every time.
func (s *WorkspaceServer) registerPrompts() {
	s.server.AddPrompt(&mcp.Prompt{
		Name:        "new-workspace",
		Title:       "Set up a new workspace",
		Description: "Create a new workspace for a stated purpose and report what's left for a human to connect an agent to it",
		Arguments: []*mcp.PromptArgument{
			{Name: "name", Description: "Name for the new workspace", Required: true},
			{Name: "purpose", Description: "What the workspace is for", Required: false},
		},
	}, s.promptNewWorkspace)

	s.server.AddPrompt(&mcp.Prompt{
		Name:        "setup-agentrqd",
		Title:       "Set up agentrqd on a machine",
		Description: "Mint a fresh enrolment code and hand back the exact install and enrol commands for a human to run on a new machine",
		Arguments: []*mcp.PromptArgument{
			{Name: "platform", Description: "linux, macos, or windows, if known", Required: false},
		},
	}, s.promptSetupAgentrqd)

	s.server.AddPrompt(&mcp.Prompt{
		Name:        "workspace-status",
		Title:       "Cross-workspace status report",
		Description: "Summarize what's happening across every workspace right now, one brain overseeing many",
	}, s.promptWorkspaceStatus)
}

func promptMessage(text string) *mcp.PromptMessage {
	return &mcp.PromptMessage{
		Role:    "user",
		Content: &mcp.TextContent{Text: text},
	}
}

func (s *WorkspaceServer) promptNewWorkspace(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	s.emitTelemetry(ctx, mcpevent.ActionMCPMethodCall, "prompt:new-workspace", 0)
	name := req.Params.Arguments["name"]
	if name == "" {
		return nil, errors.New("new-workspace: the \"name\" argument is required")
	}
	purpose := req.Params.Arguments["purpose"]

	descriptionClause := ""
	if purpose != "" {
		descriptionClause = fmt.Sprintf(" and description %q", purpose)
	}

	text := fmt.Sprintf(
		"Set up a new workspace named %q.\n\n"+
			"1. Call workspace action list and check none of the existing workspaces already cover this.\n"+
			"2. Read the %s resource for the full steps.\n"+
			"3. Call workspace action create with name %q%s.\n"+
			"4. Tell me the new workspace's id and what I still need to do myself to connect an agent to it.",
		name, newWorkspaceGuideURI, name, descriptionClause,
	)

	return &mcp.GetPromptResult{
		Description: "Set up workspace " + name,
		Messages:    []*mcp.PromptMessage{promptMessage(text)},
	}, nil
}

func (s *WorkspaceServer) promptSetupAgentrqd(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	s.emitTelemetry(ctx, mcpevent.ActionMCPMethodCall, "prompt:setup-agentrqd", 0)
	platform := req.Params.Arguments["platform"]
	target := "a machine"
	if platform != "" {
		target = fmt.Sprintf("a %s machine", platform)
	}

	text := fmt.Sprintf(
		"Help me set up agentrqd on %s.\n\n"+
			"1. Read the %s resource for the install steps.\n"+
			"2. Call machines action createEnrolmentCode to mint a fresh one-time code.\n"+
			"3. Give me the install command from the guide and the enrol command with that code filled in, "+
			"in the order I should run them.\n"+
			"4. Remind me I have to run these myself, on that machine — there is no remote enrolment.",
		target, agentrqdSetupGuideURI,
	)

	return &mcp.GetPromptResult{
		Description: "Set up agentrqd on " + target,
		Messages:    []*mcp.PromptMessage{promptMessage(text)},
	}, nil
}

func (s *WorkspaceServer) promptWorkspaceStatus(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	s.emitTelemetry(ctx, mcpevent.ActionMCPMethodCall, "prompt:workspace-status", 0)
	text := "Give me a status report across every workspace.\n\n" +
		"1. Call workspace action list.\n" +
		"2. For each workspace, call workspace action stats (range 7d) and task action list for anything ongoing or blocked.\n" +
		"3. Summarize per workspace: what's running, what's blocked on a human, and anything that looks stuck.\n" +
		"4. Call out whatever needs my attention first."

	return &mcp.GetPromptResult{
		Description: "Cross-workspace status report",
		Messages:    []*mcp.PromptMessage{promptMessage(text)},
	}, nil
}
