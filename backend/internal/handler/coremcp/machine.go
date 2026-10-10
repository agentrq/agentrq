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
	"github.com/agentrq/agentrq/backend/internal/service/mcphint"
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
	MachineID   string `json:"machineId" jsonschema:"The machine to run it on (base62), from listMachines"`
	Kind        string `json:"kind" jsonschema:"enum: claude-code, acp-gateway"`
	Model       string `json:"model,omitempty" jsonschema:"The model: an alias such as opus or sonnet for claude-code, or a model the gateway's agent offers for acp-gateway. Leave it out for the default"`
	Effort      string `json:"effort,omitempty" jsonschema:"claude-code only: low, medium, high, xhigh or max. Leave it out for the default"`
	Agent       string `json:"agent,omitempty" jsonschema:"acp-gateway only: which agent the gateway runs"`
}

type StopAgentParams struct {
	WorkspaceID string `json:"workspaceId" jsonschema:"The workspace whose agent to stop (base62)"`
}

func (s *WorkspaceServer) registerMachineTools() {
	mcp.AddTool(s.server, &mcp.Tool{Name: "createEnrolmentCode", Description: "Mint a one-time code for enrolling a new machine with agentrqd. It is shown once and expires shortly, so hand it to the human right away — see the agentrq://guides/agentrqd-setup resource for the rest of the setup", Annotations: mcphint.Write("Create an enrolment code")}, s.handleCreateEnrolmentCode)
	mcp.AddTool(s.server, &mcp.Tool{Name: "listMachines", Description: "List the account's machines running agentrqd: name, OS, agentrqd version, whether each is enabled and online, and how many agents it is running. launchAgent takes a machine's id", Annotations: mcphint.Read("List machines")}, s.handleListMachines)
	mcp.AddTool(s.server, &mcp.Tool{Name: "launchAgent", Description: "Start an agent for a workspace on one of the account's machines, in the workspace's working directory (a fork's folder is made from its parent's). Refused while the workspace already has an agent, when it has no working directory, or when the machine is disabled, offline or runs an agentrqd too old for what was asked. Returns the session; the agent connects to the workspace a few seconds later and then picks up its tasks", Annotations: mcphint.Write("Launch an agent")}, s.handleLaunchAgent)
	mcp.AddTool(s.server, &mcp.Tool{Name: "stopAgent", Description: "Stop the agent a workspace is running on a machine, ending whatever it is in the middle of. stopped is false when none was running on a machine", Annotations: mcphint.Overwrite("Stop an agent")}, s.handleStopAgent)
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
		Agent:       args.Agent,
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
