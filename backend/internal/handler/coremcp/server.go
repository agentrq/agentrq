package coremcp

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	apiMapper "github.com/agentrq/agentrq/backend/internal/mapper/api"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/mustafaturan/monoflake"
)

type WorkspaceServer struct {
	server       *mcp.Server
	streamServer *mcp.StreamableHTTPHandler
	crud         crud.Controller
	baseURL      string
}

// NewServer creates a single MCP server instance with tools that span all user-accessible endpoints.
func NewServer(crudCtrl crud.Controller, baseURL string) *WorkspaceServer {
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

	streamHandler := mcp.NewStreamableHTTPHandler(func(request *http.Request) *mcp.Server {
		return srv
	}, &mcp.StreamableHTTPOptions{})

	ws := &WorkspaceServer{
		server:       srv,
		streamServer: streamHandler,
		crud:         crudCtrl,
		baseURL:      baseURL,
	}

	ws.registerTools()
	return ws
}

func (s *WorkspaceServer) Handler() *mcp.StreamableHTTPHandler {
	return s.streamServer
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
	Icon                 *string        `json:"icon,omitempty"`
	NotificationSettings map[string]any `json:"notificationSettings,omitempty"`
}

type GetWorkspaceParams struct {
	ID string `json:"id" jsonschema:"Workspace ID (base62 or integer)"`
}

type UpdateWorkspaceParams struct {
	ID                   string         `json:"id"`
	Name                 *string        `json:"name,omitempty"`
	Description          *string        `json:"description,omitempty"`
	Icon                 *string        `json:"icon,omitempty"`
	NotificationSettings map[string]any `json:"notificationSettings,omitempty"`
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
	CronSchedule string `json:"cronSchedule,omitempty"`
	ParentID     string `json:"parentId,omitempty"`
}

type GetTaskParams struct {
	WorkspaceID string `json:"workspaceId"`
	TaskID      string `json:"taskId"`
}

type RespondToTaskParams struct {
	WorkspaceID string `json:"workspaceId"`
	TaskID      string `json:"taskId"`
	Action      string `json:"action" jsonschema:"enum: allow, deny"`
	Text        string `json:"text,omitempty"`
}

type ReplyToTaskParams struct {
	WorkspaceID string `json:"workspaceId"`
	TaskID      string `json:"taskId"`
	Text        string `json:"text"`
}

type UpdateTaskStatusParams struct {
	WorkspaceID string `json:"workspaceId"`
	TaskID      string `json:"taskId"`
	Status      string `json:"status" jsonschema:"enum: notstarted, ongoing, waiting, completed, done, cron, failed"`
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
	CronSchedule string `json:"cronSchedule,omitempty"`
	IsOneTime    bool   `json:"isOneTime,omitempty"`
}

type GetAttachmentParams struct {
	WorkspaceID  string `json:"workspaceId"`
	AttachmentID string `json:"attachmentId"`
}

// ── Tool Definitions ──────────────────────────────────────────────────────────

func (s *WorkspaceServer) registerTools() {
	mcp.AddTool(s.server, &mcp.Tool{Name: "listWorkspaces", Description: "List all workspaces for the authenticated user"}, s.handleListWorkspaces)
	mcp.AddTool(s.server, &mcp.Tool{Name: "createWorkspace", Description: "Create a new workspace"}, s.handleCreateWorkspace)
	mcp.AddTool(s.server, &mcp.Tool{Name: "getWorkspace", Description: "Get a workspace by ID"}, s.handleGetWorkspace)
	mcp.AddTool(s.server, &mcp.Tool{Name: "updateWorkspace", Description: "Update a workspace"}, s.handleUpdateWorkspace)
	mcp.AddTool(s.server, &mcp.Tool{Name: "getWorkspaceStats", Description: "Get statistics for a workspace"}, s.handleGetWorkspaceStats)
	mcp.AddTool(s.server, &mcp.Tool{Name: "listTasks", Description: "List tasks in a specific workspace"}, s.handleListTasks)
	mcp.AddTool(s.server, &mcp.Tool{Name: "listAllTasks", Description: "List all tasks across all workspaces"}, s.handleListAllTasks)
	mcp.AddTool(s.server, &mcp.Tool{Name: "createTask", Description: "Create a new task in a workspace"}, s.handleCreateTask)
	mcp.AddTool(s.server, &mcp.Tool{Name: "getTask", Description: "Get a specific task by ID"}, s.handleGetTask)
	mcp.AddTool(s.server, &mcp.Tool{Name: "respondToTask", Description: "Submit an allow/deny response to a task"}, s.handleRespondToTask)
	mcp.AddTool(s.server, &mcp.Tool{Name: "replyToTask", Description: "Post a message to a task thread"}, s.handleReplyToTask)
	mcp.AddTool(s.server, &mcp.Tool{Name: "updateTaskStatus", Description: "Update a task's status"}, s.handleUpdateTaskStatus)
	mcp.AddTool(s.server, &mcp.Tool{Name: "updateTaskOrder", Description: "Update a task's sort order"}, s.handleUpdateTaskOrder)
	mcp.AddTool(s.server, &mcp.Tool{Name: "updateTaskAssignee", Description: "Update a task's assignee"}, s.handleUpdateTaskAssignee)
	mcp.AddTool(s.server, &mcp.Tool{Name: "updateTaskAllowAll", Description: "Toggle allow_all_commands for a task"}, s.handleUpdateTaskAllowAll)
	mcp.AddTool(s.server, &mcp.Tool{Name: "updateScheduledTask", Description: "Update a scheduled/cron task"}, s.handleUpdateScheduledTask)
	mcp.AddTool(s.server, &mcp.Tool{Name: "getAttachment", Description: "Get metadata for an attachment"}, s.handleGetAttachment)
}

// ── Handlers ──────────────────────────────────────────────────────────────────

func (s *WorkspaceServer) handleListWorkspaces(ctx context.Context, req *mcp.CallToolRequest, args ListWorkspacesParams) (*mcp.CallToolResult, any, error) {
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

	b := apiMapper.FromListWorkspacesResponseEntityToHTTPResponse(res, s.mcpURL)
	return textResponse(string(b)), nil, nil
}

func (s *WorkspaceServer) handleCreateWorkspace(ctx context.Context, req *mcp.CallToolRequest, args CreateWorkspaceParams) (*mcp.CallToolResult, any, error) {
	userID := getUserID(ctx)
	workspace := entity.Workspace{
		Name: args.Name,
	}
	if args.Description != nil {
		workspace.Description = *args.Description
	}
	if args.Icon != nil {
		workspace.Icon = *args.Icon
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

	b := apiMapper.FromCreateWorkspaceResponseEntityToHTTPResponse(res, s.mcpURL(res.Workspace.ID))
	return textResponse(string(b)), nil, nil
}

func (s *WorkspaceServer) handleGetWorkspace(ctx context.Context, req *mcp.CallToolRequest, args GetWorkspaceParams) (*mcp.CallToolResult, any, error) {
	userID := getUserID(ctx)
	res, err := s.crud.GetWorkspace(ctx, entity.GetWorkspaceRequest{
		UserID: userID,
		ID:     parseID(args.ID),
	})
	if err != nil {
		return errorResponse(err), nil, nil
	}

	b := apiMapper.FromGetWorkspaceResponseEntityToHTTPResponse(res, s.mcpURL(res.Workspace.ID))
	return textResponse(string(b)), nil, nil
}

func (s *WorkspaceServer) handleUpdateWorkspace(ctx context.Context, req *mcp.CallToolRequest, args UpdateWorkspaceParams) (*mcp.CallToolResult, any, error) {
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
	if args.Icon != nil {
		workspace.Icon = *args.Icon
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

	b := apiMapper.FromUpdateWorkspaceResponseEntityToHTTPResponse(&res.Workspace, s.mcpURL(res.Workspace.ID))
	return textResponse(string(b)), nil, nil
}

func (s *WorkspaceServer) handleGetWorkspaceStats(ctx context.Context, req *mcp.CallToolRequest, args GetWorkspaceStatsParams) (*mcp.CallToolResult, any, error) {
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
	userID := getUserID(ctx)

	var statuses []string
	if args.Status != "" {
		statuses = []string{args.Status}
	}

	limit := args.Limit
	if limit <= 0 {
		limit = 5
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
	userID := getUserID(ctx)

	var statuses []string
	if args.Status != "" {
		statuses = []string{args.Status}
	}

	limit := args.Limit
	if limit <= 0 {
		limit = 5
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
	userID := getUserID(ctx)
	assignee := args.Assignee
	if assignee == "" {
		assignee = "human" // default
	}

	res, err := s.crud.CreateTask(ctx, entity.CreateTaskRequest{
		UserID: userID,
		Task: entity.Task{
			WorkspaceID:  parseID(args.WorkspaceID),
			Title:        args.Title,
			Body:         args.Body,
			Assignee:     assignee,
			CronSchedule: args.CronSchedule,
			ParentID:     parseID(args.ParentID),
		},
	})
	if err != nil {
		return errorResponse(err), nil, nil
	}

	b := apiMapper.FromCreateTaskResponseEntityToHTTPResponse(res)
	return textResponse(string(b)), nil, nil
}

func (s *WorkspaceServer) handleGetTask(ctx context.Context, req *mcp.CallToolRequest, args GetTaskParams) (*mcp.CallToolResult, any, error) {
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

func (s *WorkspaceServer) handleGetAttachment(ctx context.Context, req *mcp.CallToolRequest, args GetAttachmentParams) (*mcp.CallToolResult, any, error) {
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
