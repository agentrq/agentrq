package coremcp

import (
	"context"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	apiMapper "github.com/agentrq/agentrq/backend/internal/mapper/api"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Workflows, over MCP.
//
// Events and triggers arrived here first (`events.go`), and a workflow is the
// thing they add up to: a named graph whose start event fans out to steps, each
// of which creates a task somewhere and may emit the next event. The pieces were
// reachable one at a time; the graph they form was REST-only, which meant the
// UI could show somebody a workflow that nothing else could build or take apart.
//
// The extension host is the caller that made that gap matter. An extension runs
// on the desktop, so it is not running at three in the morning — it does not
// need to be, as long as it can *configure* something that is. Standing work
// outlives the process that set it up only if it can be created, revised and
// removed through one API, and reconciliation in particular needs all three:
// leaving a workflow behind on uninstall is worse than never creating it.
//
// Each handler is a thin wrapper over the same controller method the REST API
// calls, so an agent gets no path the UI does not have and no validation the UI
// does not enforce: ownership of the workflow, of the events it names and of the
// workspaces its steps point at, and the cycle check that stops a graph emitting
// its way back into itself, are all decided in the controller.
//
// ## Two ways to write the same graph
//
// `createWorkflowStep` and `deleteWorkflowStep` edit one node at a time, which
// is what an agent adding a branch to an existing workflow wants.
// `replaceWorkflowFromText` writes the whole graph from the indented document
// the UI's text mode uses, which is what anything *declarative* wants — an
// extension reconciling what it declared against what exists compares two
// documents rather than diffing a graph node by node.
//
// Both go through the same controller and therefore the same rules; neither is a
// shortcut past the other.

// ── Params ────────────────────────────────────────────────────────────────────

type CreateWorkflowParams struct {
	Name         string `json:"name" jsonschema:"Workflow name"`
	Description  string `json:"description,omitempty"`
	StartEventID string `json:"startEventId,omitempty" jsonschema:"The event that starts this workflow. Its steps hang off this one"`
}

type GetWorkflowParams struct {
	WorkflowID string `json:"workflowId" jsonschema:"Workflow ID (base62)"`
}

// UpdateWorkflowParams mirrors the PATCH: every field is a pointer so that
// omitting one leaves it alone. A workflow's layout and its description are
// edited by different screens, and a full-replacement update would have each
// blanking the other's work.
type UpdateWorkflowParams struct {
	WorkflowID   string  `json:"workflowId"`
	Name         *string `json:"name,omitempty" jsonschema:"Only sent if it should change"`
	Description  *string `json:"description,omitempty" jsonschema:"Only sent if it should change"`
	StartEventID *string `json:"startEventId,omitempty" jsonschema:"Only sent if it should change"`
	Layout       *string `json:"layout,omitempty" jsonschema:"Canvas positions, as the UI stores them. Leave this out unless you are moving nodes"`
}

type DeleteWorkflowParams struct {
	WorkflowID string `json:"workflowId"`
}

type CreateWorkflowStepParams struct {
	WorkflowID       string `json:"workflowId"`
	EventID          string `json:"eventId" jsonschema:"The event this step reacts to"`
	WorkspaceID      string `json:"workspaceId" jsonschema:"The workspace the step's task is created in"`
	Title            string `json:"title" jsonschema:"Task title, used exactly as written: placeholders are not substituted here"`
	Body             string `json:"body,omitempty" jsonschema:"Task body. {{EVENT_PAYLOAD}} and {{EVENT_FAQ}} are replaced with what the publisher sent"`
	Assignee         string `json:"assignee,omitempty" jsonschema:"enum: agent, human. Defaults to agent"`
	AllowAllCommands bool   `json:"allowAllCommands,omitempty" jsonschema:"Let the spawned task run commands without asking for permission"`
	EmitEventID      string `json:"emitEventId,omitempty" jsonschema:"Event to publish when this step's task completes. This is how one step hands off to the next"`
}

type ListWorkflowStepsParams struct {
	WorkflowID string `json:"workflowId"`
}

type DeleteWorkflowStepParams struct {
	WorkflowID string `json:"workflowId"`
	StepID     string `json:"stepId" jsonschema:"Workflow step ID (base62)"`
}

type ListWorkflowTasksParams struct {
	WorkflowID string `json:"workflowId"`
}

type GetWorkflowTextParams struct {
	WorkflowID string `json:"workflowId"`
}

type ReplaceWorkflowFromTextParams struct {
	WorkflowID string `json:"workflowId"`
	Text       string `json:"text" jsonschema:"The whole graph as an indented document. Two spaces per level, every list line '- agent:<workspace>' or '- event:<name>', alternating. Events and workspaces are named, so they must already exist"`
}

// ── Tool definitions ──────────────────────────────────────────────────────────

func (s *WorkspaceServer) registerWorkflowTools() {
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "listWorkflows",
		Description: "List the workflows defined for this account. A workflow is a named graph: a start event, and the steps that react to it and to each other",
	}, s.handleListWorkflows)
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "createWorkflow",
		Description: "Create an empty workflow around a start event. Add steps to it afterwards, one at a time or as a whole document",
	}, s.handleCreateWorkflow)
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "getWorkflow",
		Description: "Get a workflow by ID",
	}, s.handleGetWorkflow)
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "updateWorkflow",
		Description: "Revise a workflow. Only the fields sent are changed, so leaving one out keeps it",
	}, s.handleUpdateWorkflow)
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "deleteWorkflow",
		Description: "Delete a workflow and its steps. The events it named are left alone",
	}, s.handleDeleteWorkflow)
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "createWorkflowStep",
		Description: "Add a step: when this event fires, create a task in this workspace, and optionally emit a second event when that task completes",
	}, s.handleCreateWorkflowStep)
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "listWorkflowSteps",
		Description: "List a workflow's steps — everything that happens once it starts",
	}, s.handleListWorkflowSteps)
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "deleteWorkflowStep",
		Description: "Remove one step from a workflow, leaving the rest of the graph in place",
	}, s.handleDeleteWorkflowStep)
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "listWorkflowTasks",
		Description: "List the tasks a workflow has spawned, to see whether a system that was wired up is running",
	}, s.handleListWorkflowTasks)
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "getWorkflowText",
		Description: "Read a workflow's whole graph as the indented document the UI's text mode edits. Steps naming a deleted event or workspace are left out, so the document always parses",
	}, s.handleGetWorkflowText)
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "replaceWorkflowFromText",
		Description: "Replace a workflow's entire graph with a document. Every name is resolved before anything is written, so an unknown one on the last line leaves the workflow untouched",
	}, s.handleReplaceWorkflowFromText)
}

// ── Handlers ──────────────────────────────────────────────────────────────────

func (s *WorkspaceServer) handleListWorkflows(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error) {
	res, err := s.crud.ListWorkflows(ctx, entity.ListWorkflowsRequest{UserID: getUserID(ctx)})
	if err != nil {
		return errorResponse(err), nil, nil
	}
	return textResponse(string(apiMapper.FromListWorkflowsResponseEntityToHTTPResponse(res))), nil, nil
}

func (s *WorkspaceServer) handleCreateWorkflow(ctx context.Context, req *mcp.CallToolRequest, args CreateWorkflowParams) (*mcp.CallToolResult, any, error) {
	res, err := s.crud.CreateWorkflow(ctx, entity.CreateWorkflowRequest{
		UserID:       getUserID(ctx),
		Name:         args.Name,
		Description:  args.Description,
		StartEventID: parseID(args.StartEventID),
	})
	if err != nil {
		return errorResponse(err), nil, nil
	}
	return textResponse(string(apiMapper.FromCreateWorkflowResponseEntityToHTTPResponse(res))), nil, nil
}

func (s *WorkspaceServer) handleGetWorkflow(ctx context.Context, req *mcp.CallToolRequest, args GetWorkflowParams) (*mcp.CallToolResult, any, error) {
	res, err := s.crud.GetWorkflow(ctx, entity.GetWorkflowRequest{
		UserID: getUserID(ctx),
		ID:     parseID(args.WorkflowID),
	})
	if err != nil {
		return errorResponse(err), nil, nil
	}
	return textResponse(string(apiMapper.FromGetWorkflowResponseEntityToHTTPResponse(res))), nil, nil
}

func (s *WorkspaceServer) handleUpdateWorkflow(ctx context.Context, req *mcp.CallToolRequest, args UpdateWorkflowParams) (*mcp.CallToolResult, any, error) {
	update := entity.UpdateWorkflowRequest{
		UserID:      getUserID(ctx),
		ID:          parseID(args.WorkflowID),
		Name:        args.Name,
		Description: args.Description,
		Layout:      args.Layout,
	}
	// Parsed rather than passed through, and only when sent: the controller
	// takes an id, and "not supplied" has to stay distinguishable from "set to
	// nothing" all the way down or a name-only edit would detach the workflow
	// from the event that starts it.
	if args.StartEventID != nil {
		startEventID := parseID(*args.StartEventID)
		update.StartEventID = &startEventID
	}

	res, err := s.crud.UpdateWorkflow(ctx, update)
	if err != nil {
		return errorResponse(err), nil, nil
	}
	return textResponse(string(apiMapper.FromUpdateWorkflowResponseEntityToHTTPResponse(res))), nil, nil
}

func (s *WorkspaceServer) handleDeleteWorkflow(ctx context.Context, req *mcp.CallToolRequest, args DeleteWorkflowParams) (*mcp.CallToolResult, any, error) {
	if err := s.crud.DeleteWorkflow(ctx, entity.DeleteWorkflowRequest{
		UserID: getUserID(ctx),
		ID:     parseID(args.WorkflowID),
	}); err != nil {
		return errorResponse(err), nil, nil
	}
	return textResponse("workflow deleted"), nil, nil
}

func (s *WorkspaceServer) handleCreateWorkflowStep(ctx context.Context, req *mcp.CallToolRequest, args CreateWorkflowStepParams) (*mcp.CallToolResult, any, error) {
	res, err := s.crud.CreateWorkflowStep(ctx, entity.CreateWorkflowStepRequest{
		UserID:           getUserID(ctx),
		WorkflowID:       parseID(args.WorkflowID),
		EventID:          parseID(args.EventID),
		WorkspaceID:      parseID(args.WorkspaceID),
		EmitEventID:      parseID(args.EmitEventID),
		Title:            args.Title,
		Body:             args.Body,
		Assignee:         triggerAssignee(args.Assignee),
		AllowAllCommands: args.AllowAllCommands,
	})
	if err != nil {
		return errorResponse(err), nil, nil
	}
	return textResponse(string(apiMapper.FromCreateWorkflowStepResponseEntityToHTTPResponse(res))), nil, nil
}

func (s *WorkspaceServer) handleListWorkflowSteps(ctx context.Context, req *mcp.CallToolRequest, args ListWorkflowStepsParams) (*mcp.CallToolResult, any, error) {
	res, err := s.crud.ListWorkflowSteps(ctx, entity.ListWorkflowStepsRequest{
		UserID:     getUserID(ctx),
		WorkflowID: parseID(args.WorkflowID),
	})
	if err != nil {
		return errorResponse(err), nil, nil
	}
	return textResponse(string(apiMapper.FromListWorkflowStepsResponseEntityToHTTPResponse(res))), nil, nil
}

func (s *WorkspaceServer) handleDeleteWorkflowStep(ctx context.Context, req *mcp.CallToolRequest, args DeleteWorkflowStepParams) (*mcp.CallToolResult, any, error) {
	if err := s.crud.DeleteWorkflowStep(ctx, entity.DeleteWorkflowStepRequest{
		UserID:     getUserID(ctx),
		WorkflowID: parseID(args.WorkflowID),
		ID:         parseID(args.StepID),
	}); err != nil {
		return errorResponse(err), nil, nil
	}
	return textResponse("workflow step deleted"), nil, nil
}

func (s *WorkspaceServer) handleListWorkflowTasks(ctx context.Context, req *mcp.CallToolRequest, args ListWorkflowTasksParams) (*mcp.CallToolResult, any, error) {
	res, err := s.crud.ListTasksFromWorkflow(ctx, entity.ListTasksFromWorkflowRequest{
		UserID:     getUserID(ctx),
		WorkflowID: parseID(args.WorkflowID),
	})
	if err != nil {
		return errorResponse(err), nil, nil
	}
	return textResponse(string(apiMapper.FromListTasksFromWorkflowResponseEntityToHTTPResponse(res))), nil, nil
}

func (s *WorkspaceServer) handleGetWorkflowText(ctx context.Context, req *mcp.CallToolRequest, args GetWorkflowTextParams) (*mcp.CallToolResult, any, error) {
	res, err := s.crud.GetWorkflowText(ctx, entity.GetWorkflowTextRequest{
		UserID: getUserID(ctx),
		ID:     parseID(args.WorkflowID),
	})
	if err != nil {
		return errorResponse(err), nil, nil
	}
	// The document itself, not the JSON envelope the browser gets: a caller
	// asking for text mode is going to edit these lines and hand them back, and
	// wrapping them in a field would mean unwrapping before every edit.
	return textResponse(res.Text), nil, nil
}

func (s *WorkspaceServer) handleReplaceWorkflowFromText(ctx context.Context, req *mcp.CallToolRequest, args ReplaceWorkflowFromTextParams) (*mcp.CallToolResult, any, error) {
	res, err := s.crud.ReplaceWorkflowFromText(ctx, entity.ReplaceWorkflowFromTextRequest{
		UserID: getUserID(ctx),
		ID:     parseID(args.WorkflowID),
		Text:   args.Text,
	})
	if err != nil {
		return errorResponse(err), nil, nil
	}
	return textResponse(string(apiMapper.FromReplaceWorkflowFromTextResponseEntityToHTTPResponse(res))), nil, nil
}
