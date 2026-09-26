// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package coremcp

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	mcpevent "github.com/agentrq/agentrq/backend/internal/controller/mcp"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	apiMapper "github.com/agentrq/agentrq/backend/internal/mapper/api"
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
	baseURL      string
	pubsub       pubsub.Service
}

// NewServer creates a single MCP server instance with tools that span all user-accessible endpoints.
func NewServer(crudCtrl crud.Controller, baseURL string, pubsubSvc pubsub.Service) *WorkspaceServer {
	srv := mcp.NewServer(&mcp.Implementation{
		Name:    "agentrq",
		Version: "1.0.0",
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
	IncludeArchived bool `json:"includeArchived" jsonschema:"Include archived workspaces"`
}

type CreateWorkspaceParams struct {
	Name                 string         `json:"name"`
	Description          *string        `json:"description,omitempty"`
	NotificationSettings map[string]any `json:"notificationSettings,omitempty"`
	SelfLearningLoopNote *string        `json:"selfLearningLoopNote,omitempty"`
}

type GetWorkspaceParams struct {
	ID string `json:"id" jsonschema:"Workspace ID (base62 or integer)"`
}

type UpdateWorkspaceParams struct {
	ID                   string         `json:"id"`
	Name                 *string        `json:"name,omitempty"`
	Description          *string        `json:"description,omitempty"`
	NotificationSettings map[string]any `json:"notificationSettings,omitempty"`
	SelfLearningLoopNote *string        `json:"selfLearningLoopNote,omitempty"`
}

type GetWorkspaceStatsParams struct {
	ID    string `json:"id"`
	Range string `json:"range" jsonschema:"Time range strictly in format '7d' or '30d'"`
	From  int64  `json:"from,omitempty" jsonschema:"Unix timestamp"`
	To    int64  `json:"to,omitempty" jsonschema:"Unix timestamp"`
}

type ListTasksParams struct {
	WorkspaceID string `json:"workspaceId"`
	Filter      string `json:"filter,omitempty"`
	Status      string `json:"status,omitempty"`
	CreatedBy   string `json:"createdBy,omitempty"`
	Limit       int    `json:"limit,omitempty" jsonschema:"Maximum number of tasks to return, default 5, capped at 50"`
	Offset      int    `json:"offset,omitempty" jsonschema:"Number of tasks to skip"`
}

type ListAllTasksParams struct {
	Filter    string `json:"filter,omitempty"`
	Status    string `json:"status,omitempty"`
	CreatedBy string `json:"createdBy,omitempty"`
	Limit     int    `json:"limit,omitempty" jsonschema:"Maximum number of tasks to return, default 5, capped at 50"`
	Offset    int    `json:"offset,omitempty" jsonschema:"Number of tasks to skip"`
}

type CreateTaskParams struct {
	WorkspaceID  string `json:"workspaceId"`
	Title        string `json:"title"`
	Body         string `json:"body,omitempty"`
	Assignee     string `json:"assignee,omitempty" jsonschema:"enum: human, agent"`
	CronSchedule string `json:"cronSchedule,omitempty" jsonschema:"Optional 5-field cron schedule. Fields are read as UTC unless the schedule names a zone, as 'CRON_TZ=Europe/Berlin 30 8 * * *'"`
	ParentID     string `json:"parentId,omitempty"`
	ClearContext bool   `json:"clearContext,omitempty" jsonschema:"Ask for a clean slate: /clear is sent to the agent's terminal before this task is handed over, so it starts without the previous task's context. Ignored when the target workspace has no running Claude Code session. Defaults to the target workspace's own setting."`
}

type GetTaskParams struct {
	WorkspaceID string `json:"workspaceId"`
	TaskID      string `json:"taskId"`
}

type RespondToTaskParams struct {
	WorkspaceID string `json:"workspaceId"`
	TaskID      string `json:"taskId"`
	// Must match crud.ValidTaskResponseActions. `reject`, not `deny`.
	Action string `json:"action" jsonschema:"enum: allow, allow_all, reject, text"`
	Text   string `json:"text,omitempty"`
}

type ReplyToTaskParams struct {
	WorkspaceID string `json:"workspaceId"`
	TaskID      string `json:"taskId"`
	Text        string `json:"text"`
}

type UpdateTaskStatusParams struct {
	WorkspaceID string `json:"workspaceId"`
	TaskID      string `json:"taskId"`
	// Must match crud.ValidTaskStatuses — a struct tag cannot be built from that
	// slice, so a test compares them instead.
	Status string `json:"status" jsonschema:"enum: notstarted, ongoing, blocked, completed, rejected, cron"`
}

type UpdateTaskOrderParams struct {
	WorkspaceID string  `json:"workspaceId"`
	TaskID      string  `json:"taskId"`
	SortOrder   float64 `json:"sortOrder"`
}

type UpdateTaskAssigneeParams struct {
	WorkspaceID string `json:"workspaceId"`
	TaskID      string `json:"taskId"`
	Assignee    string `json:"assignee" jsonschema:"enum: agent, human"`
}

type UpdateTaskAllowAllParams struct {
	WorkspaceID string `json:"workspaceId"`
	TaskID      string `json:"taskId"`
	AllowAll    bool   `json:"allowAll"`
}

type UpdateScheduledTaskParams struct {
	WorkspaceID  string `json:"workspaceId"`
	TaskID       string `json:"taskId"`
	Title        string `json:"title,omitempty"`
	Body         string `json:"body,omitempty"`
	CronSchedule string `json:"cronSchedule,omitempty" jsonschema:"New 5-field cron schedule. Fields are read as UTC unless the schedule names a zone, as 'CRON_TZ=Europe/Berlin 30 8 * * *'"`
	IsOneTime    bool   `json:"isOneTime,omitempty"`
}

// DeleteTaskParams removes a task outright, attachments and messages with it.
//
// The supervisor could already create a scheduled task and revise it, but not
// retire one — which is only a gap until something has to *reconcile*. An
// extension that declares a nightly task, and is then uninstalled, has to be
// able to leave nothing behind; without this it could only abandon the schedule
// and hope somebody noticed.
type DeleteTaskParams struct {
	WorkspaceID string `json:"workspaceId"`
	TaskID      string `json:"taskId"`
}

type GetAttachmentParams struct {
	WorkspaceID  string `json:"workspaceId"`
	AttachmentID string `json:"attachmentId"`
}

type ListMemoriesParams struct {
	WorkspaceID string `json:"workspaceId"`
}

type GetMemoryParams struct {
	WorkspaceID string `json:"workspaceId"`
	Name        string `json:"name" jsonschema:"The memory's name, as listMemories reports it."`
}

type SearchSkillsParams struct {
	WorkspaceID string `json:"workspaceId"`
	Q           string `json:"q,omitempty" jsonschema:"Text to find in a skill's name or description, ignoring case; at least 3 characters. Leave it out to list every skill."`
	Limit       int    `json:"limit,omitempty" jsonschema:"How many skills to return, at most 100. Leave it out to return every match."`
	Offset      int    `json:"offset,omitempty" jsonschema:"How many matches to skip, for the next page."`
}

type GetSkillParams struct {
	WorkspaceID string `json:"workspaceId"`
	URI         string `json:"uri" jsonschema:"The file to read, as skill://<name>/<path>. skill://<name> alone reads its SKILL.md."`
}

// ── Tool Definitions ──────────────────────────────────────────────────────────

func (s *WorkspaceServer) registerTools() {
	mcp.AddTool(s.server, &mcp.Tool{Name: "listWorkspaces", Description: "List all workspaces for the authenticated user", Annotations: mcphint.Read("List workspaces")}, s.handleListWorkspaces)
	mcp.AddTool(s.server, &mcp.Tool{Name: "createWorkspace", Description: "Create a new workspace", Annotations: mcphint.Write("Create a workspace")}, s.handleCreateWorkspace)
	mcp.AddTool(s.server, &mcp.Tool{Name: "getWorkspace", Description: "Get a workspace by ID", Annotations: mcphint.Read("Get a workspace")}, s.handleGetWorkspace)
	mcp.AddTool(s.server, &mcp.Tool{Name: "updateWorkspace", Description: "Update a workspace", Annotations: mcphint.Update("Update a workspace")}, s.handleUpdateWorkspace)
	mcp.AddTool(s.server, &mcp.Tool{Name: "getWorkspaceStats", Description: "Get statistics for a workspace", Annotations: mcphint.Read("Workspace statistics")}, s.handleGetWorkspaceStats)
	mcp.AddTool(s.server, &mcp.Tool{Name: "listTasks", Description: "List tasks in a specific workspace", Annotations: mcphint.Read("List a workspace's tasks")}, s.handleListTasks)
	mcp.AddTool(s.server, &mcp.Tool{Name: "listAllTasks", Description: "List all tasks across all workspaces", Annotations: mcphint.Read("List every task")}, s.handleListAllTasks)
	mcp.AddTool(s.server, &mcp.Tool{Name: "createTask", Description: "Create a new task in a workspace", Annotations: mcphint.Write("Create a task")}, s.handleCreateTask)
	mcp.AddTool(s.server, &mcp.Tool{Name: "getTask", Description: "Get a specific task by ID", Annotations: mcphint.Read("Get a task")}, s.handleGetTask)
	mcp.AddTool(s.server, &mcp.Tool{Name: "respondToTask", Description: "Answer a permission request a task is waiting on: allow, allow_all, reject, or text to reply without deciding", Annotations: mcphint.Write("Answer a permission request")}, s.handleRespondToTask)
	mcp.AddTool(s.server, &mcp.Tool{Name: "replyToTask", Description: "Post a message to a task thread", Annotations: mcphint.Write("Reply in a task thread")}, s.handleReplyToTask)
	mcp.AddTool(s.server, &mcp.Tool{Name: "updateTaskStatus", Description: "Update a task's status", Annotations: mcphint.Update("Update a task's status")}, s.handleUpdateTaskStatus)
	mcp.AddTool(s.server, &mcp.Tool{Name: "updateTaskOrder", Description: "Update a task's sort order", Annotations: mcphint.Update("Reorder a task")}, s.handleUpdateTaskOrder)
	mcp.AddTool(s.server, &mcp.Tool{Name: "updateTaskAssignee", Description: "Update a task's assignee", Annotations: mcphint.Update("Reassign a task")}, s.handleUpdateTaskAssignee)
	mcp.AddTool(s.server, &mcp.Tool{Name: "updateTaskAllowAll", Description: "Toggle allow_all_commands for a task", Annotations: mcphint.Update("Set allow-all-commands")}, s.handleUpdateTaskAllowAll)
	mcp.AddTool(s.server, &mcp.Tool{Name: "updateScheduledTask", Description: "Update a scheduled/cron task", Annotations: mcphint.Update("Update a scheduled task")}, s.handleUpdateScheduledTask)
	mcp.AddTool(s.server, &mcp.Tool{Name: "deleteTask", Description: "Delete a task, with its messages and attachments. This cannot be undone; to stop a scheduled task without losing its history, set its status to rejected instead", Annotations: mcphint.Overwrite("Delete a task")}, s.handleDeleteTask)
	mcp.AddTool(s.server, &mcp.Tool{Name: "getAttachment", Description: "Get attachment data as base64 and metadata", Annotations: mcphint.Read("Get an attachment")}, s.handleGetAttachment)
	mcp.AddTool(s.server, &mcp.Tool{Name: "listMemories", Description: "List a workspace's memories: name, size and when each was last changed. Content is not included — get one by name for that.", Annotations: mcphint.Read("List a workspace's memories")}, s.handleListMemories)
	mcp.AddTool(s.server, &mcp.Tool{Name: "getMemory", Description: "Get one of a workspace's memories in full, by name. MEMORY.md is the index the others hang off.", Annotations: mcphint.Read("Get a memory")}, s.handleGetMemory)
	mcp.AddTool(s.server, &mcp.Tool{Name: "searchSkills", Description: "Find the skills a workspace can use, its own and those shared into it: name, description, source and size, plus the total number of matches. With q (at least 3 characters) only skills whose name or description contains it are returned; limit and offset page through the matches. A shared-in skill carries sharedFromWorkspaceId and is read-only there. Content is not included — get a file with getSkill.", Annotations: mcphint.Read("Search a workspace's skills")}, s.handleSearchSkills)
	mcp.AddTool(s.server, &mcp.Tool{Name: "getSkill", Description: "Get one file of a workspace's skill in full, by its skill://<name>/<path> URI; skill://<name> alone gets its SKILL.md, together with the list of the skill's other files.", Annotations: mcphint.Read("Get a skill file")}, s.handleGetSkill)

	// Events and their triggers — see events.go.
	s.registerEventTools()
	s.registerWorkflowTools()
	s.registerMachineTools()

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
		UserID:      userID,
		WorkspaceID: parseID(args.WorkspaceID),
		Filter:      args.Filter,
		Status:      statuses,
		CreatedBy:   args.CreatedBy,
		Limit:       limit,
		Offset:      args.Offset,
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
		UserID:    userID,
		Filter:    args.Filter,
		Status:    statuses,
		CreatedBy: args.CreatedBy,
		Limit:     limit,
		Offset:    args.Offset,
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
	})
	if err != nil {
		return errorResponse(err), nil, nil
	}

	b := apiMapper.FromCreateTaskResponseEntityToHTTPResponse(res)
	return textResponse(string(b)), nil, nil
}

func (s *WorkspaceServer) handleGetTask(ctx context.Context, req *mcp.CallToolRequest, args GetTaskParams) (*mcp.CallToolResult, any, error) {
	s.emitTelemetry(ctx, mcpevent.ActionMCPToolCall, "getTask", parseID(args.WorkspaceID))
	userID := getUserID(ctx)
	res, err := s.crud.GetTask(ctx, entity.GetTaskRequest{
		UserID:      userID,
		WorkspaceID: parseID(args.WorkspaceID),
		TaskID:      parseID(args.TaskID),
	})
	if err != nil {
		return errorResponse(err), nil, nil
	}

	b := apiMapper.FromGetTaskResponseEntityToHTTPResponse(res)
	return textResponse(string(b)), nil, nil
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
	userID := getUserID(ctx)
	res, err := s.crud.GetAttachment(ctx, entity.GetAttachmentRequest{
		UserID:       userID,
		WorkspaceID:  parseID(args.WorkspaceID),
		AttachmentID: args.AttachmentID,
	})
	if err != nil {
		return errorResponse(err), nil, nil
	}

	return jsonResponse(res), nil, nil
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
