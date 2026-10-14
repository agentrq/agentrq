// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package coremcp

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/agentrq/agentrq/backend/internal/controller/agentlaunch"
	mcpevent "github.com/agentrq/agentrq/backend/internal/controller/mcp"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// AgentLauncher is agentlaunch.Launcher, the launch the web form runs too, so
// launchAgent keeps every gate it does.
type AgentLauncher interface {
	Launch(ctx context.Context, rq agentlaunch.Request) (*entity.CreateSessionResponse, error)
	Stop(ctx context.Context, rq agentlaunch.StopRequest) (*entity.SessionView, error)
}

type LaunchAgentParams struct {
	WorkspaceID string `json:"workspaceId" jsonschema:"The workspace to start an agent in (base62)"`
	MachineID   string `json:"machineId" jsonschema:"The machine to run it on (base62), from machines action list"`
	Kind        string `json:"kind" jsonschema:"enum: claude-code, acp-gateway"`
	Model       string `json:"model,omitempty" jsonschema:"The model: an alias such as opus or sonnet for claude-code, or a model the gateway's agent offers for acp-gateway. Leave it out for the default"`
	Effort      string `json:"effort,omitempty" jsonschema:"claude-code only: low, medium, high, xhigh or max. Leave it out for the default"`
	Autocompact string `json:"autocompact,omitempty" jsonschema:"claude-code only: the auto-compact window, 100k to 900k in whole hundreds such as 600k. Leave it out for the default, 400k"`
	Agent       string `json:"agent,omitempty" jsonschema:"acp-gateway only: which agent the gateway runs"`
	ACPVersion  string `json:"acpVersion,omitempty" jsonschema:"acp-gateway only: \"1\" (or \"2\") starts the agent in native ACP mode, where AgentRQ speaks ACP itself. Leave it out for the classic gateway"`
}

type StopAgentParams struct {
	WorkspaceID string `json:"workspaceId" jsonschema:"The workspace whose agent to stop (base62)"`
}

func (s *WorkspaceServer) handleListMachines(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error) {
	s.emitTelemetry(ctx, mcpevent.ActionMCPToolCall, "listMachines", 0)
	res, err := s.crud.ListMachines(ctx, entity.ListMachinesRequest{UserID: getUserID(ctx)})
	if err != nil {
		return errorResponse(err), nil, nil
	}
	return jsonResponse(res), nil, nil
}

func (s *WorkspaceServer) handleLaunchAgent(ctx context.Context, req *mcp.CallToolRequest, args LaunchAgentParams) (*mcp.CallToolResult, any, error) {
	s.emitTelemetry(ctx, mcpevent.ActionMCPToolCall, "launchAgent", parseID(args.WorkspaceID))
	// The schema's enum is only a description to the SDK, so it is checked
	// here: an unknown kind would otherwise reach the machine as a session.
	if !slices.Contains(agentlaunch.Kinds, args.Kind) {
		return errorResponse(fmt.Errorf("kind must be one of %s", strings.Join(agentlaunch.Kinds, ", "))), nil, nil
	}
	res, err := s.launcher.Launch(ctx, agentlaunch.Request{
		UserID:      getUserID(ctx),
		WorkspaceID: args.WorkspaceID,
		MachineID:   args.MachineID,
		Kind:        args.Kind,
		Model:       args.Model,
		Effort:      args.Effort,
		Autocompact: args.Autocompact,
		Agent:       args.Agent,
		ACPVersion:  args.ACPVersion,
	})
	if err != nil {
		return errorResponse(err), nil, nil
	}
	return jsonResponse(res), nil, nil
}

func (s *WorkspaceServer) handleStopAgent(ctx context.Context, req *mcp.CallToolRequest, args StopAgentParams) (*mcp.CallToolResult, any, error) {
	s.emitTelemetry(ctx, mcpevent.ActionMCPToolCall, "stopAgent", parseID(args.WorkspaceID))
	session, err := s.launcher.Stop(ctx, agentlaunch.StopRequest{
		UserID:      getUserID(ctx),
		WorkspaceID: args.WorkspaceID,
	})
	if err != nil {
		return errorResponse(err), nil, nil
	}
	return jsonResponse(map[string]any{"stopped": session != nil, "session": session}), nil, nil
}

// handleCreateEnrolmentCode mints the code; it never enrols anything itself.
// There is no remote enrolment — a human has to run the resulting command on
// the target machine themselves.
func (s *WorkspaceServer) handleCreateEnrolmentCode(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error) {
	s.emitTelemetry(ctx, mcpevent.ActionMCPToolCall, "createEnrolmentCode", 0)
	userID := getUserID(ctx)
	res, err := s.crud.CreateEnrolmentCode(ctx, entity.CreateEnrolmentCodeRequest{UserID: userID})
	if err != nil {
		return errorResponse(err), nil, nil
	}

	return jsonResponse(map[string]any{
		"code":         res.Code,
		"expiresAt":    res.ExpiresAt,
		"enrolCommand": fmt.Sprintf("agentrqd enroll --server %s --code %s", s.baseURL, res.Code),
	}), nil, nil
}
