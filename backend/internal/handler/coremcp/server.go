// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package coremcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	mcpevent "github.com/agentrq/agentrq/backend/internal/controller/mcp"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	apiMapper "github.com/agentrq/agentrq/backend/internal/mapper/api"
	"github.com/agentrq/agentrq/backend/internal/service/config"
	"github.com/agentrq/agentrq/backend/internal/service/mcphint"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
	"github.com/agentrq/agentrq/backend/internal/service/skill"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/mustafaturan/monoflake"
)

type WorkspaceServer struct {
	server       *mcp.Server
	streamServer *mcp.StreamableHTTPHandler
	crud         crud.Controller
	forks        ForkMerger
	launcher     AgentLauncher
	baseURL      string
	pubsub       pubsub.Service
}

// ForkMerger is forkmerge.Merger: the merge REST runs too, so mergeFork stops
// the fork's agent before its tasks move.
type ForkMerger interface {
	Merge(ctx context.Context, rq entity.MergeForkRequest) (*entity.MergeForkResponse, error)
}

// NewServer creates a single MCP server instance with tools that span all user-accessible endpoints.
func NewServer(crudCtrl crud.Controller, baseURL string, pubsubSvc pubsub.Service) *WorkspaceServer {
	srv := mcp.NewServer(&mcp.Implementation{
		Name:    "agentrq",
		Version: config.Release(),
		Icons: []mcp.Icon{
			{
				Source:   baseURL + "/agentrq.png",
				MIMEType: "image/png",
			},
		},
	}, &mcp.ServerOptions{})

	// Stateless, unlike the per-workspace server, which pushes to its agent over
	// the SSE stream and so must keep sessions. Nothing here pushes, so there is
	// no session worth holding — and only a stateless transport may serve
	// revision 2026-07-28. Older revisions are served exactly as before.
	streamHandler := mcp.NewStreamableHTTPHandler(func(request *http.Request) *mcp.Server {
		return srv
	}, &mcp.StreamableHTTPOptions{Stateless: true})

	ws := &WorkspaceServer{
		server:       srv,
		streamServer: streamHandler,
		crud:         crudCtrl,
		baseURL:      baseURL,
		pubsub:       pubsubSvc,
	}

	ws.registerTools()
	return ws
}

// emitTelemetry reports a tool call or a resource/prompt read on
// PubSubTopicMCP, the same topic and event shape the per-workspace MCP server
// uses (see controller/mcp/event.go) — the telemetry controller's recordMCP
// switch doesn't care which server an MCPEvent came from.
//
// workspaceID is 0 when the call has no single workspace to attribute to
// (listWorkspaces, an account-wide machine tool, a resource or prompt read),
// mirroring the machine-action convention in controller/telemetry.
func (s *WorkspaceServer) emitTelemetry(ctx context.Context, action mcpevent.Action, toolOrMethod string, workspaceID int64) {
	if s.pubsub == nil {
		return
	}
	s.pubsub.Publish(ctx, pubsub.PublishRequest{
		PubSubID: entity.PubSubTopicMCP,
		Event: mcpevent.MCPEvent{
			Action:      action,
			WorkspaceID: workspaceID,
			UserID:      monoflake.IDFromBase62(getUserID(ctx)).Int64(),
			ToolName:    toolOrMethod,
			Method:      toolOrMethod,
			Actor:       2, // Agent — coremcp is reached only from a workspace's own agent session.
		},
	})
}

func (s *WorkspaceServer) Handler() *mcp.StreamableHTTPHandler {
	return s.streamServer
}

// MCPServer returns the underlying MCP server for introspection — currently
// only controller/telemetry's SubActionID parity test, which lists what's
// actually registered rather than trusting a hand-typed copy of it. Not for
// handling requests directly; use Handler for that.
func (s *WorkspaceServer) MCPServer() *mcp.Server {
	return s.server
}

func textResponse(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: text,
			},
		},
	}
}

var errTitleRequired = errors.New("title is required")

func errorResponse(err error) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: err.Error(),
			},
		},
	}
}

func jsonResponse(data interface{}) *mcp.CallToolResult {
	b, err := json.Marshal(data)
	if err != nil {
		return errorResponse(err)
	}
	return textResponse(string(b))
}

// Helper to extract user_id from context
func getUserID(ctx context.Context) string {
	if val := ctx.Value("user_id"); val != nil {
		if s, ok := val.(string); ok {
			return s
		}
	}
	return ""
}

// parseID parses a base62 string to int64, handling the case where it's already a numeric string
func parseID(str interface{}) int64 {
	if str == nil {
		return 0
	}
	s, ok := str.(string)
	if !ok || s == "" {
		return 0
	}
	return monoflake.IDFromBase62(s).Int64()
}

func (s *WorkspaceServer) mcpURL(workspaceID int64) string {
	if s.baseURL == "" {
		return ""
	}
	return s.baseURL + "/mcp/" + monoflake.ID(workspaceID).String()
}

// ── Params Structs ────────────────────────────────────────────────────────────

type ListWorkspacesParams struct {
	IncludeArchived bool `json:"includeArchived,omitempty" jsonschema:"Include archived workspaces"`
}

type CreateWorkspaceParams struct {
	Name                 string         `json:"name" jsonschema:"The workspace's name, in kebab-case, at most 128 characters."`
	Description          *string        `json:"description,omitempty" jsonschema:"Its mission: what its agent is for."`
	NotificationSettings map[string]any `json:"notificationSettings,omitempty" jsonschema:"Its notification settings, as the web app stores them."`
	SelfLearningLoopNote *string        `json:"selfLearningLoopNote,omitempty" jsonschema:"A note its agent is asked to keep, about what it learned."`
}

type ForkWorkspaceParams struct {
	WorkspaceID string `json:"workspaceId" jsonschema:"The workspace to fork (base62)"`
	Name        string `json:"name,omitempty" jsonschema:"Name for the fork in kebab-case, like other workspace names, at most 128 characters. Defaults to '<parent>-fork'"`
}

type MergeForkParams struct {
	WorkspaceID  string `json:"workspaceId" jsonschema:"The fork to merge back (base62)"`
	DeleteFolder bool   `json:"deleteFolder,omitempty" jsonschema:"Also delete the fork's folder on its machine. Its git branch is kept. Refused if that machine is offline. Default false"`
}

type GetWorkspaceParams struct {
	ID string `json:"id" jsonschema:"Workspace ID (base62 or integer)"`
}

type UpdateWorkspaceParams struct {
	ID                   string         `json:"id" jsonschema:"Workspace ID (base62)."`
	Name                 *string        `json:"name,omitempty" jsonschema:"A new name, in kebab-case."`
	Description          *string        `json:"description,omitempty" jsonschema:"A new mission."`
	NotificationSettings map[string]any `json:"notificationSettings,omitempty" jsonschema:"New notification settings, as the web app stores them."`
	SelfLearningLoopNote *string        `json:"selfLearningLoopNote,omitempty" jsonschema:"A new self-learning note."`
}

type GetWorkspaceStatsParams struct {
	ID    string `json:"id" jsonschema:"Workspace ID (base62)."`
	Range string `json:"range" jsonschema:"Time range strictly in format '7d' or '30d'"`
	From  int64  `json:"from,omitempty" jsonschema:"Unix timestamp"`
	To    int64  `json:"to,omitempty" jsonschema:"Unix timestamp"`
}

type ListTasksParams struct {
	WorkspaceID          string `json:"workspaceId" jsonschema:"The workspace's ID (base62)."`
	Filter               string `json:"filter,omitempty" jsonschema:"Only tasks whose title or body contains it, ignoring case."`
	Status               string `json:"status,omitempty" jsonschema:"Only tasks with this status."`
	CreatedBy            string `json:"createdBy,omitempty" jsonschema:"Only tasks created by 'human' or 'agent'."`
	CreatedByWorkspaceID string `json:"createdByWorkspaceId,omitempty" jsonschema:"Only tasks delegated by this workspace (base62) through this tool's create."`
	Limit                int    `json:"limit,omitempty" jsonschema:"Maximum number of tasks to return, default 5, capped at 50"`
	Offset               int    `json:"offset,omitempty" jsonschema:"Number of tasks to skip"`
}

type ListAllTasksParams struct {
	Filter               string `json:"filter,omitempty" jsonschema:"Only tasks whose title or body contains it, ignoring case."`
	Status               string `json:"status,omitempty" jsonschema:"Only tasks with this status."`
	CreatedBy            string `json:"createdBy,omitempty" jsonschema:"Only tasks created by 'human' or 'agent'."`
	CreatedByWorkspaceID string `json:"createdByWorkspaceId,omitempty" jsonschema:"Only tasks delegated by this workspace (base62) through this tool's create."`
	Limit                int    `json:"limit,omitempty" jsonschema:"Maximum number of tasks to return, default 5, capped at 50"`
	Offset               int    `json:"offset,omitempty" jsonschema:"Number of tasks to skip"`
}

type CreateTaskParams struct {
	WorkspaceID  string `json:"workspaceId" jsonschema:"The workspace's ID (base62)."`
	Title        string `json:"title" jsonschema:"The task's title."`
	Body         string `json:"body,omitempty" jsonschema:"The task's body: what is to be done."`
	Assignee     string `json:"assignee,omitempty" jsonschema:"enum: human, agent"`
	CronSchedule string `json:"cronSchedule,omitempty" jsonschema:"A 5-field cron schedule (minute hour dom month dow) to run the task on. A recurring task is at most hourly: a single minute 0-59, e.g. '30 * * * *'. A one-time task fixes dom and month, e.g. '30 14 25 4 *'."`
	ParentID     string `json:"parentId,omitempty" jsonschema:"The task this one belongs under (base62)."`
	ClearContext bool   `json:"clearContext,omitempty" jsonschema:"Ask for a clean slate: /clear is sent to the agent's terminal before this task is handed over, so it starts without the previous task's context. Ignored when the target workspace has no running Claude Code session. Defaults to the target workspace's own setting."`
}

type GetTaskParams struct {
	WorkspaceID      string `json:"workspaceId" jsonschema:"The workspace's ID (base62)."`
	TaskID           string `json:"taskId" jsonschema:"The task's ID (base62)."`
	IncludeToolCalls bool   `json:"includeToolCalls,omitempty" jsonschema:"true adds the agent's tool calls, most of a busy task's size; without it toolCallCount still says how many there are. Default false."`
	MessagesSince    string `json:"messagesSince,omitempty" jsonschema:"Only the messages created after this time (RFC3339), like the createdAt of the last message you read."`
}

type TaskMessagesParams struct {
	WorkspaceID string `json:"workspaceId" jsonschema:"The workspace's ID (base62)."`
	TaskID      string `json:"taskId" jsonschema:"The task's ID (base62)."`
	Since       string `json:"since,omitempty" jsonschema:"Only the messages created after this time (RFC3339), like the createdAt of the last message you read."`
	Limit       int    `json:"limit,omitempty" jsonschema:"At most this many messages, the newest ones; default 20, capped at 100."`
}

type RespondToTaskParams struct {
	WorkspaceID string `json:"workspaceId" jsonschema:"The workspace's ID (base62)."`
	TaskID      string `json:"taskId" jsonschema:"The task's ID (base62)."`
	// Must match crud.ValidTaskResponseActions. `reject`, not `deny`.
	Action string `json:"action" jsonschema:"enum: allow, allow_all, reject, text"`
	Text   string `json:"text,omitempty" jsonschema:"With action text, the reply to post."`
}

type ReplyToTaskParams struct {
	WorkspaceID string `json:"workspaceId" jsonschema:"The workspace's ID (base62)."`
	TaskID      string `json:"taskId" jsonschema:"The task's ID (base62)."`
	Text        string `json:"text" jsonschema:"The message's text."`
}

type UpdateTaskStatusParams struct {
	WorkspaceID string `json:"workspaceId" jsonschema:"The workspace's ID (base62)."`
	TaskID      string `json:"taskId" jsonschema:"The task's ID (base62)."`
	// Must match crud.ValidTaskStatuses — a struct tag cannot be built from that
	// slice, so a test compares them instead.
	Status string `json:"status" jsonschema:"enum: notstarted, ongoing, blocked, completed, rejected, cron"`
}

type UpdateTaskOrderParams struct {
	WorkspaceID string  `json:"workspaceId" jsonschema:"The workspace's ID (base62)."`
	TaskID      string  `json:"taskId" jsonschema:"The task's ID (base62)."`
	SortOrder   float64 `json:"sortOrder" jsonschema:"The task's new sort order: tasks are listed in ascending order."`
}

type UpdateTaskAssigneeParams struct {
	WorkspaceID string `json:"workspaceId" jsonschema:"The workspace's ID (base62)."`
	TaskID      string `json:"taskId" jsonschema:"The task's ID (base62)."`
	Assignee    string `json:"assignee" jsonschema:"enum: agent, human"`
}

type UpdateTaskAllowAllParams struct {
	WorkspaceID string `json:"workspaceId" jsonschema:"The workspace's ID (base62)."`
	TaskID      string `json:"taskId" jsonschema:"The task's ID (base62)."`
	AllowAll    bool   `json:"allowAll" jsonschema:"Whether the task's agent may run every command without asking."`
}

type UpdateScheduledTaskParams struct {
	WorkspaceID  string `json:"workspaceId" jsonschema:"The workspace's ID (base62)."`
	TaskID       string `json:"taskId" jsonschema:"The task's ID (base62)."`
	Title        string `json:"title,omitempty" jsonschema:"The task's new title."`
	Body         string `json:"body,omitempty" jsonschema:"The task's new body."`
	CronSchedule string `json:"cronSchedule,omitempty" jsonschema:"The task's new 5-field cron schedule."`
	IsOneTime    bool   `json:"isOneTime,omitempty" jsonschema:"Ignored: the schedule says whether it is one-time."`
}

// DeleteTaskParams removes a task outright, attachments and messages with it.
//
// The supervisor could already create a scheduled task and revise it, but not
// retire one — which is only a gap until something has to *reconcile*. An
// extension that declares a nightly task, and is then uninstalled, has to be
// able to leave nothing behind; without this it could only abandon the schedule
// and hope somebody noticed.
type DeleteTaskParams struct {
	WorkspaceID string `json:"workspaceId" jsonschema:"The workspace's ID (base62)."`
	TaskID      string `json:"taskId" jsonschema:"The task's ID (base62)."`
}

type GetAttachmentParams struct {
	WorkspaceID  string `json:"workspaceId" jsonschema:"The workspace's ID (base62)."`
	TaskID       string `json:"taskId" jsonschema:"The task's ID (base62)."`
	AttachmentID string `json:"attachmentId" jsonschema:"The attachment's ID."`
	Format       string `json:"format,omitempty" jsonschema:"'url' (default) returns the attachment's public link when it has one, and its base64 content as data when it does not. 'base64' always returns the content. 'textContent' returns it as plain text, for a text file such as markdown or HTML."`
}

type ListMemoriesParams struct {
	WorkspaceID string `json:"workspaceId" jsonschema:"The workspace's ID (base62)."`
}

type GetMemoryParams struct {
	WorkspaceID string `json:"workspaceId" jsonschema:"The workspace's ID (base62)."`
	Name        string `json:"name" jsonschema:"The memory's name, as memory action list reports it."`
}

type SearchSkillsParams struct {
	WorkspaceID string `json:"workspaceId" jsonschema:"The workspace whose view of the skills to search (base62)."`
	Q           string `json:"q,omitempty" jsonschema:"Text to find in a skill's name or description, ignoring case; at least 3 characters. Leave it out to list every skill."`
	Limit       int    `json:"limit,omitempty" jsonschema:"How many skills to return, at most 100. Leave it out to return every match."`
	Offset      int    `json:"offset,omitempty" jsonschema:"How many matches to skip, for the next page."`
}

type GetSkillParams struct {
	WorkspaceID string `json:"workspaceId" jsonschema:"The workspace whose skill to read (base62)."`
	URI         string `json:"uri" jsonschema:"The file to read, as skill://<name>/<path>. skill://<name> alone reads its SKILL.md."`
}

// ── Tool Definitions ──────────────────────────────────────────────────────────

func (s *WorkspaceServer) registerTools() {
	// Every tool takes an action and its payload — see tools.go and task.go.
	// The annotation is the most a tool's actions can do, so a tool with a
	// delete is destructive even for its reads.
	mcp.AddTool(s.server, &mcp.Tool{Name: "workspace", Description: workspaceToolDescription, Annotations: mcphint.Overwrite("Manage workspaces")}, s.handleWorkspaceTool)
	mcp.AddTool(s.server, &mcp.Tool{Name: "task", Description: taskToolDescription, Annotations: mcphint.Overwrite("Manage tasks")}, s.handleTask)
	mcp.AddTool(s.server, &mcp.Tool{Name: "memory", Description: memoryToolDescription, Annotations: mcphint.Read("Read a workspace's memory")}, s.handleMemoryTool)
	mcp.AddTool(s.server, &mcp.Tool{Name: "skills", Description: skillsToolDescription, Annotations: mcphint.Read("Read a workspace's skills")}, s.handleSkillsTool)
	mcp.AddTool(s.server, &mcp.Tool{Name: "events", Description: eventsToolDescription, Annotations: mcphint.Overwrite("Manage events and triggers")}, s.handleEventsTool)
	mcp.AddTool(s.server, &mcp.Tool{Name: "workflows", Description: workflowsToolDescription, Annotations: mcphint.Overwrite("Manage workflows")}, s.handleWorkflowsTool)
	mcp.AddTool(s.server, &mcp.Tool{Name: "machines", Description: machinesToolDescription, Annotations: mcphint.Overwrite("Manage machines and agents")}, s.handleMachinesTool)
	s.registerPanoTools()
	mcp.AddTool(s.server, &mcp.Tool{Name: "help", Description: helpToolDescription, Annotations: mcphint.Read("How to call these tools")}, s.handleHelpTool)
	s.server.AddReceivingMiddleware(mcpevent.ActionToolsMiddleware(actionToolNames, legacyTools))

	// Resources and prompts — see resources.go and prompts.go.
	s.registerResources()
	s.registerPrompts()
}

// ── Handlers ──────────────────────────────────────────────────────────────────

func (s *WorkspaceServer) handleListWorkspaces(ctx context.Context, req *mcp.CallToolRequest, args ListWorkspacesParams) (*mcp.CallToolResult, any, error) {
	s.emitTelemetry(ctx, mcpevent.ActionMCPToolCall, "listWorkspaces", 0)
	userID := getUserID(ctx)
	if userID == "" {
		return errorResponse(context.Canceled), nil, nil
	}

	res, err := s.crud.ListWorkspaces(ctx, entity.ListWorkspacesRequest{
		UserID:          userID,
		IncludeArchived: args.IncludeArchived,
	})
	if err != nil {
		return errorResponse(err), nil, nil
	}

	b := apiMapper.FromListWorkspacesResponseEntityToMCPResponse(res, s.mcpURL)
	return textResponse(string(b)), nil, nil
}

func (s *WorkspaceServer) handleCreateWorkspace(ctx context.Context, req *mcp.CallToolRequest, args CreateWorkspaceParams) (*mcp.CallToolResult, any, error) {
	s.emitTelemetry(ctx, mcpevent.ActionMCPToolCall, "createWorkspace", 0)
	userID := getUserID(ctx)
	workspace := entity.Workspace{
		Name: args.Name,
	}
	if args.Description != nil {
		workspace.Description = *args.Description
	}
	if args.SelfLearningLoopNote != nil {
		workspace.SelfLearningLoopNote = *args.SelfLearningLoopNote
	}

	if args.NotificationSettings != nil {
		b, _ := json.Marshal(args.NotificationSettings)
		var ns entity.NotificationSettings
		if err := json.Unmarshal(b, &ns); err == nil {
			workspace.NotificationSettings = &ns
		}
	}

	res, err := s.crud.CreateWorkspace(ctx, entity.CreateWorkspaceRequest{
		UserID:    userID,
		Workspace: workspace,
	})
	if err != nil {
		return errorResponse(err), nil, nil
	}

	b := apiMapper.FromCreateWorkspaceResponseEntityToMCPResponse(res, s.mcpURL(res.Workspace.ID))
	return textResponse(string(b)), nil, nil
}

func (s *WorkspaceServer) handleForkWorkspace(ctx context.Context, req *mcp.CallToolRequest, args ForkWorkspaceParams) (*mcp.CallToolResult, any, error) {
	s.emitTelemetry(ctx, mcpevent.ActionMCPToolCall, "forkWorkspace", parseID(args.WorkspaceID))
	rq := apiMapper.FromMCPToForkWorkspaceRequestEntity(parseID(args.WorkspaceID), args.Name)
	if rq == nil {
		return errorResponse(errors.New("the fork's name is longer than 128 characters")), nil, nil
	}
	rq.UserID = getUserID(ctx)
	res, err := s.crud.ForkWorkspace(ctx, *rq)
	if err != nil {
		return errorResponse(err), nil, nil
	}

	b := apiMapper.FromForkWorkspaceResponseEntityToMCPResponse(res, s.mcpURL(res.Workspace.ID))
	return textResponse(string(b)), nil, nil
}

func (s *WorkspaceServer) handleMergeFork(ctx context.Context, req *mcp.CallToolRequest, args MergeForkParams) (*mcp.CallToolResult, any, error) {
	s.emitTelemetry(ctx, mcpevent.ActionMCPToolCall, "mergeFork", parseID(args.WorkspaceID))
	res, err := s.forks.Merge(ctx, entity.MergeForkRequest{
		UserID:       getUserID(ctx),
		WorkspaceID:  parseID(args.WorkspaceID),
		DeleteFolder: args.DeleteFolder,
	})
	if err != nil {
		return errorResponse(err), nil, nil
	}

	return textResponse(string(apiMapper.FromMergeForkResponseEntityToHTTPResponse(res))), nil, nil
}

func (s *WorkspaceServer) handleGetWorkspace(ctx context.Context, req *mcp.CallToolRequest, args GetWorkspaceParams) (*mcp.CallToolResult, any, error) {
	s.emitTelemetry(ctx, mcpevent.ActionMCPToolCall, "getWorkspace", parseID(args.ID))
	userID := getUserID(ctx)
	res, err := s.crud.GetWorkspace(ctx, entity.GetWorkspaceRequest{
		UserID: userID,
		ID:     parseID(args.ID),
	})
	if err != nil {
		return errorResponse(err), nil, nil
	}

	b := apiMapper.FromGetWorkspaceResponseEntityToMCPResponse(res, s.mcpURL(res.Workspace.ID))
	return textResponse(string(b)), nil, nil
}

func (s *WorkspaceServer) handleUpdateWorkspace(ctx context.Context, req *mcp.CallToolRequest, args UpdateWorkspaceParams) (*mcp.CallToolResult, any, error) {
	s.emitTelemetry(ctx, mcpevent.ActionMCPToolCall, "updateWorkspace", parseID(args.ID))
	userID := getUserID(ctx)
	existing, err := s.crud.GetWorkspace(ctx, entity.GetWorkspaceRequest{
		UserID: userID,
		ID:     parseID(args.ID),
	})
	if err != nil {
		return errorResponse(err), nil, nil
	}

	workspace := existing.Workspace
	if args.Name != nil {
		workspace.Name = *args.Name
	}
	if args.Description != nil {
		workspace.Description = *args.Description
	}
	if args.SelfLearningLoopNote != nil {
		workspace.SelfLearningLoopNote = *args.SelfLearningLoopNote
	}
	if args.NotificationSettings != nil {
		b, _ := json.Marshal(args.NotificationSettings)
		var ns entity.NotificationSettings
		if err := json.Unmarshal(b, &ns); err == nil {
			workspace.NotificationSettings = &ns
		}
	}

	res, err := s.crud.UpdateWorkspace(ctx, entity.UpdateWorkspaceRequest{
		UserID:    userID,
		Workspace: workspace,
	})
	if err != nil {
		return errorResponse(err), nil, nil
	}

	b := apiMapper.FromUpdateWorkspaceResponseEntityToMCPResponse(&res.Workspace, s.mcpURL(res.Workspace.ID))
	return textResponse(string(b)), nil, nil
}

func (s *WorkspaceServer) handleGetWorkspaceStats(ctx context.Context, req *mcp.CallToolRequest, args GetWorkspaceStatsParams) (*mcp.CallToolResult, any, error) {
	s.emitTelemetry(ctx, mcpevent.ActionMCPToolCall, "getWorkspaceStats", parseID(args.ID))
	userID := getUserID(ctx)
	rng := args.Range
	if rng == "" {
		rng = "7d"
	}

	res, err := s.crud.GetDetailedWorkspaceStats(ctx, entity.GetWorkspaceStatsRequest{
		UserID: userID,
		ID:     parseID(args.ID),
		Range:  rng,
		From:   args.From,
		To:     args.To,
	})
	if err != nil {
		return errorResponse(err), nil, nil
	}

	return jsonResponse(res), nil, nil
}

func (s *WorkspaceServer) handleListTasks(ctx context.Context, req *mcp.CallToolRequest, args ListTasksParams) (*mcp.CallToolResult, any, error) {
	s.emitTelemetry(ctx, mcpevent.ActionMCPToolCall, "listTasks", parseID(args.WorkspaceID))
	userID := getUserID(ctx)

	var statuses []string
	if args.Status != "" {
		statuses = []string{args.Status}
	}

	limit := args.Limit
	if limit <= 0 {
		limit = 5
	}
	if limit > 50 {
		limit = 50
	}

	res, err := s.crud.ListTasks(ctx, entity.ListTasksRequest{
		UserID:               userID,
		WorkspaceID:          parseID(args.WorkspaceID),
		Search:               args.Filter,
		CreatedByWorkspaceID: parseID(args.CreatedByWorkspaceID),
		Status:               statuses,
		CreatedBy:            args.CreatedBy,
		Limit:                limit,
		Offset:               args.Offset,
	})
	if err != nil {
		return errorResponse(err), nil, nil
	}

	b := apiMapper.FromListTasksResponseEntityToHTTPResponse(res)
	return textResponse(string(b)), nil, nil
}

func (s *WorkspaceServer) handleListAllTasks(ctx context.Context, req *mcp.CallToolRequest, args ListAllTasksParams) (*mcp.CallToolResult, any, error) {
	s.emitTelemetry(ctx, mcpevent.ActionMCPToolCall, "listAllTasks", 0)
	userID := getUserID(ctx)

	var statuses []string
	if args.Status != "" {
		statuses = []string{args.Status}
	}

	limit := args.Limit
	if limit <= 0 {
		limit = 5
	}
	if limit > 50 {
		limit = 50
	}

	res, err := s.crud.ListTasks(ctx, entity.ListTasksRequest{
		UserID:               userID,
		Search:               args.Filter,
		CreatedByWorkspaceID: parseID(args.CreatedByWorkspaceID),
		Status:               statuses,
		CreatedBy:            args.CreatedBy,
		Limit:                limit,
		Offset:               args.Offset,
		// WorkspaceID = 0 implies all
	})
	if err != nil {
		return errorResponse(err), nil, nil
	}

	b := apiMapper.FromListTasksResponseEntityToHTTPResponse(res)
	return textResponse(string(b)), nil, nil
}

func (s *WorkspaceServer) handleCreateTask(ctx context.Context, req *mcp.CallToolRequest, args CreateTaskParams) (*mcp.CallToolResult, any, error) {
	s.emitTelemetry(ctx, mcpevent.ActionMCPToolCall, "createTask", parseID(args.WorkspaceID))
	// Checked here because the controller now lets a title be left out, for
	// the interface's sake; an agent must still name what it asks for.
	if strings.TrimSpace(args.Title) == "" {
		return errorResponse(errTitleRequired), nil, nil
	}
	userID := getUserID(ctx)
	assignee := args.Assignee
	if assignee == "" {
		assignee = "human" // default
	}

	res, err := s.crud.CreateTask(ctx, entity.CreateTaskRequest{
		UserID: userID,
		Task: entity.Task{
			WorkspaceID:  parseID(args.WorkspaceID),
			CreatedBy:    "agent",
			Title:        args.Title,
			Body:         args.Body,
			Assignee:     assignee,
			CronSchedule: args.CronSchedule,
			ParentID:     parseID(args.ParentID),
			ClearContext: args.ClearContext,
		},
		BySupervisor: true,
	})
	if err != nil {
		return errorResponse(err), nil, nil
	}

	b := apiMapper.FromCreateTaskResponseEntityToHTTPResponse(res)
	return textResponse(string(b)), nil, nil
}

func (s *WorkspaceServer) handleGetTask(ctx context.Context, req *mcp.CallToolRequest, args GetTaskParams) (*mcp.CallToolResult, any, error) {
	s.emitTelemetry(ctx, mcpevent.ActionMCPToolCall, "getTask", parseID(args.WorkspaceID))
	since, err := parseSince("messagesSince", args.MessagesSince)
	if err != nil {
		return errorResponse(err), nil, nil
	}
	userID := getUserID(ctx)
	res, err := s.crud.GetTask(ctx, entity.GetTaskRequest{
		UserID:      userID,
		WorkspaceID: parseID(args.WorkspaceID),
		TaskID:      parseID(args.TaskID),
	})
	if err != nil {
		return errorResponse(err), nil, nil
	}
	if !args.IncludeToolCalls {
		res.Task.ToolCalls = nil
	}
	res.Task.Messages = messagesAfter(res.Task.Messages, since)

	b := apiMapper.FromGetTaskResponseEntityToHTTPResponse(res)
	return textResponse(string(b)), nil, nil
}

// handleTaskMessages answers what a task's thread says, oldest first, without
// the tool calls and agent telemetry that make up most of a get.
func (s *WorkspaceServer) handleTaskMessages(ctx context.Context, req *mcp.CallToolRequest, args TaskMessagesParams) (*mcp.CallToolResult, any, error) {
	s.emitTelemetry(ctx, mcpevent.ActionMCPToolCall, "getTaskMessages", parseID(args.WorkspaceID))
	since, err := parseSince("since", args.Since)
	if err != nil {
		return errorResponse(err), nil, nil
	}
	res, err := s.crud.GetTask(ctx, entity.GetTaskRequest{
		UserID:      getUserID(ctx),
		WorkspaceID: parseID(args.WorkspaceID),
		TaskID:      parseID(args.TaskID),
	})
	if err != nil {
		return errorResponse(err), nil, nil
	}

	msgs := textMessages(messagesAfter(res.Task.Messages, since))
	total := len(msgs)
	limit := args.Limit
	if limit <= 0 {
		limit = defaultTaskMessages
	}
	limit = min(limit, maxTaskMessages)
	if len(msgs) > limit {
		msgs = msgs[len(msgs)-limit:]
	}
	return textResponse(string(apiMapper.FromTaskEntityToTaskMessagesMCPResponse(res.Task, msgs, total))), nil, nil
}

func (s *WorkspaceServer) handleRespondToTask(ctx context.Context, req *mcp.CallToolRequest, args RespondToTaskParams) (*mcp.CallToolResult, any, error) {
	s.emitTelemetry(ctx, mcpevent.ActionMCPToolCall, "respondToTask", parseID(args.WorkspaceID))
	userID := getUserID(ctx)
	res, err := s.crud.RespondToTask(ctx, entity.RespondToTaskRequest{
		UserID:      userID,
		WorkspaceID: parseID(args.WorkspaceID),
		TaskID:      parseID(args.TaskID),
		Action:      args.Action,
		Text:        args.Text,
	})
	if err != nil {
		return errorResponse(err), nil, nil
	}

	b := apiMapper.FromRespondToTaskResponseEntityToHTTPResponse(res)
	return textResponse(string(b)), nil, nil
}

func (s *WorkspaceServer) handleReplyToTask(ctx context.Context, req *mcp.CallToolRequest, args ReplyToTaskParams) (*mcp.CallToolResult, any, error) {
	s.emitTelemetry(ctx, mcpevent.ActionMCPToolCall, "replyToTask", parseID(args.WorkspaceID))
	userID := getUserID(ctx)
	res, err := s.crud.ReplyToTask(ctx, entity.ReplyToTaskRequest{
		UserID:      userID,
		WorkspaceID: parseID(args.WorkspaceID),
		TaskID:      parseID(args.TaskID),
		Text:        args.Text,
	})
	if err != nil {
		return errorResponse(err), nil, nil
	}

	b := apiMapper.FromReplyToTaskResponseEntityToHTTPResponse(res)
	return textResponse(string(b)), nil, nil
}

func (s *WorkspaceServer) handleUpdateTaskStatus(ctx context.Context, req *mcp.CallToolRequest, args UpdateTaskStatusParams) (*mcp.CallToolResult, any, error) {
	s.emitTelemetry(ctx, mcpevent.ActionMCPToolCall, "updateTaskStatus", parseID(args.WorkspaceID))
	userID := getUserID(ctx)
	res, err := s.crud.UpdateTaskStatus(ctx, entity.UpdateTaskStatusRequest{
		UserID:      userID,
		WorkspaceID: parseID(args.WorkspaceID),
		TaskID:      parseID(args.TaskID),
		Status:      args.Status,
	})
	if err != nil {
		return errorResponse(err), nil, nil
	}

	b := apiMapper.FromUpdateTaskStatusResponseEntityToHTTPResponse(res)
	return textResponse(string(b)), nil, nil
}

func (s *WorkspaceServer) handleUpdateTaskOrder(ctx context.Context, req *mcp.CallToolRequest, args UpdateTaskOrderParams) (*mcp.CallToolResult, any, error) {
	s.emitTelemetry(ctx, mcpevent.ActionMCPToolCall, "updateTaskOrder", parseID(args.WorkspaceID))
	userID := getUserID(ctx)
	res, err := s.crud.UpdateTaskOrder(ctx, entity.UpdateTaskOrderRequest{
		UserID:      userID,
		WorkspaceID: parseID(args.WorkspaceID),
		TaskID:      parseID(args.TaskID),
		SortOrder:   args.SortOrder,
	})
	if err != nil {
		return errorResponse(err), nil, nil
	}

	b := apiMapper.FromUpdateTaskOrderResponseEntityToHTTPResponse(res)
	return textResponse(string(b)), nil, nil
}

func (s *WorkspaceServer) handleUpdateTaskAssignee(ctx context.Context, req *mcp.CallToolRequest, args UpdateTaskAssigneeParams) (*mcp.CallToolResult, any, error) {
	s.emitTelemetry(ctx, mcpevent.ActionMCPToolCall, "updateTaskAssignee", parseID(args.WorkspaceID))
	userID := getUserID(ctx)
	res, err := s.crud.UpdateTaskAssignee(ctx, entity.UpdateTaskAssigneeRequest{
		UserID:      userID,
		WorkspaceID: parseID(args.WorkspaceID),
		TaskID:      parseID(args.TaskID),
		Assignee:    args.Assignee,
	})
	if err != nil {
		return errorResponse(err), nil, nil
	}

	b := apiMapper.FromUpdateTaskAssigneeResponseEntityToHTTPResponse(res)
	return textResponse(string(b)), nil, nil
}

func (s *WorkspaceServer) handleUpdateTaskAllowAll(ctx context.Context, req *mcp.CallToolRequest, args UpdateTaskAllowAllParams) (*mcp.CallToolResult, any, error) {
	s.emitTelemetry(ctx, mcpevent.ActionMCPToolCall, "updateTaskAllowAll", parseID(args.WorkspaceID))
	userID := getUserID(ctx)
	res, err := s.crud.UpdateTaskAllowAllCommands(ctx, entity.UpdateTaskAllowAllCommandsRequest{
		UserID:           userID,
		WorkspaceID:      parseID(args.WorkspaceID),
		TaskID:           parseID(args.TaskID),
		AllowAllCommands: args.AllowAll,
	})
	if err != nil {
		return errorResponse(err), nil, nil
	}

	b := apiMapper.FromUpdateTaskAllowAllCommandsResponseEntityToHTTPResponse(res)
	return textResponse(string(b)), nil, nil
}

func (s *WorkspaceServer) handleUpdateScheduledTask(ctx context.Context, req *mcp.CallToolRequest, args UpdateScheduledTaskParams) (*mcp.CallToolResult, any, error) {
	s.emitTelemetry(ctx, mcpevent.ActionMCPToolCall, "updateScheduledTask", parseID(args.WorkspaceID))
	userID := getUserID(ctx)
	res, err := s.crud.UpdateScheduledTask(ctx, entity.UpdateScheduledTaskRequest{
		UserID:       userID,
		WorkspaceID:  parseID(args.WorkspaceID),
		TaskID:       parseID(args.TaskID),
		Title:        args.Title,
		Body:         args.Body,
		CronSchedule: args.CronSchedule,
	})
	if err != nil {
		return errorResponse(err), nil, nil
	}

	b := apiMapper.FromUpdateScheduledTaskResponseEntityToHTTPResponse(res)
	return textResponse(string(b)), nil, nil
}

func (s *WorkspaceServer) handleDeleteTask(ctx context.Context, req *mcp.CallToolRequest, args DeleteTaskParams) (*mcp.CallToolResult, any, error) {
	s.emitTelemetry(ctx, mcpevent.ActionMCPToolCall, "deleteTask", parseID(args.WorkspaceID))
	if _, err := s.crud.DeleteTask(ctx, entity.DeleteTaskRequest{
		UserID:      getUserID(ctx),
		WorkspaceID: parseID(args.WorkspaceID),
		TaskID:      parseID(args.TaskID),
	}); err != nil {
		return errorResponse(err), nil, nil
	}
	return textResponse("task deleted"), nil, nil
}

func (s *WorkspaceServer) handleGetAttachment(ctx context.Context, req *mcp.CallToolRequest, args GetAttachmentParams) (*mcp.CallToolResult, any, error) {
	s.emitTelemetry(ctx, mcpevent.ActionMCPToolCall, "getAttachment", parseID(args.WorkspaceID))
	format, err := mcpevent.AttachmentFormat(args.Format)
	if err != nil {
		return errorResponse(err), nil, nil
	}
	userID := getUserID(ctx)
	res, err := s.crud.GetAttachment(ctx, entity.GetAttachmentRequest{
		UserID:       userID,
		WorkspaceID:  parseID(args.WorkspaceID),
		TaskID:       parseID(args.TaskID),
		AttachmentID: args.AttachmentID,
		LinkOnly:     format == mcpevent.AttachmentFormatURL,
	})
	if err != nil {
		return errorResponse(err), nil, nil
	}

	out := mcpevent.AttachmentView{Filename: res.Filename, MimeType: res.MimeType}
	if format == mcpevent.AttachmentFormatURL && res.URL != "" {
		out.URL = res.URL
	} else if err := out.SetContent(format, res.Data); err != nil {
		return errorResponse(err), nil, nil
	}
	return jsonResponse(out), nil, nil
}

func (s *WorkspaceServer) handleListMemories(ctx context.Context, req *mcp.CallToolRequest, args ListMemoriesParams) (*mcp.CallToolResult, any, error) {
	s.emitTelemetry(ctx, mcpevent.ActionMCPToolCall, "listMemories", parseID(args.WorkspaceID))
	userID := getUserID(ctx)
	res, err := s.crud.ListMemories(ctx, entity.ListMemoriesRequest{
		UserID:      userID,
		WorkspaceID: parseID(args.WorkspaceID),
	})
	if err != nil {
		return errorResponse(err), nil, nil
	}

	b := apiMapper.FromListMemoriesResponseEntityToHTTPResponse(res)
	return textResponse(string(b)), nil, nil
}

func (s *WorkspaceServer) handleSearchSkills(ctx context.Context, req *mcp.CallToolRequest, args SearchSkillsParams) (*mcp.CallToolResult, any, error) {
	s.emitTelemetry(ctx, mcpevent.ActionMCPToolCall, "searchSkills", parseID(args.WorkspaceID))
	ctx = entity.WithOrigin(ctx, entity.OriginMCP)
	res, err := s.crud.SearchSkills(ctx, entity.SearchSkillsRequest{
		UserID:      getUserID(ctx),
		WorkspaceID: parseID(args.WorkspaceID),
		Query:       args.Q,
		Limit:       args.Limit,
		Offset:      args.Offset,
	})
	if err != nil {
		return errorResponse(err), nil, nil
	}
	return textResponse(string(apiMapper.FromSearchSkillsResponseEntityToHTTPResponse(res))), nil, nil
}

// handleGetSkill returns one file with its skill's metadata; for a SKILL.md,
// the metadata lists the skill's other files too, as loadSkill's footer does
// on the workspace server.
func (s *WorkspaceServer) handleGetSkill(ctx context.Context, req *mcp.CallToolRequest, args GetSkillParams) (*mcp.CallToolResult, any, error) {
	workspaceID := parseID(args.WorkspaceID)
	s.emitTelemetry(ctx, mcpevent.ActionMCPToolCall, "getSkill", workspaceID)
	ctx = entity.WithOrigin(ctx, entity.OriginMCP)
	name, p, err := skill.ParseURI(args.URI)
	if err != nil {
		return errorResponse(err), nil, nil
	}
	if p == "" {
		p = skill.FileName
	}
	userID := getUserID(ctx)
	res, err := s.crud.GetSkillFile(ctx, entity.GetSkillFileRequest{UserID: userID, WorkspaceID: workspaceID, Name: name, Path: p})
	if err != nil {
		return errorResponse(err), nil, nil
	}
	if p == skill.FileName {
		meta, err := s.crud.GetSkill(ctx, entity.GetSkillRequest{UserID: userID, WorkspaceID: workspaceID, Name: name})
		if err != nil {
			return errorResponse(err), nil, nil
		}
		res.Skill.Files = meta.Skill.Files
	}
	return textResponse(string(apiMapper.FromGetSkillFileResponseEntityToHTTPResponse(res))), nil, nil
}

func (s *WorkspaceServer) handleGetMemory(ctx context.Context, req *mcp.CallToolRequest, args GetMemoryParams) (*mcp.CallToolResult, any, error) {
	s.emitTelemetry(ctx, mcpevent.ActionMCPToolCall, "getMemory", parseID(args.WorkspaceID))
	userID := getUserID(ctx)
	res, err := s.crud.GetMemory(ctx, entity.GetMemoryRequest{
		UserID:      userID,
		WorkspaceID: parseID(args.WorkspaceID),
		Name:        args.Name,
	})
	if err != nil {
		return errorResponse(err), nil, nil
	}

	b := apiMapper.FromGetMemoryResponseEntityToHTTPResponse(res)
	return textResponse(string(b)), nil, nil
}
