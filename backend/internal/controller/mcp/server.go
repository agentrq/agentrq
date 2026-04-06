package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/robfig/cron/v3"
	zlog "github.com/rs/zerolog/log"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	mapper "github.com/agentrq/agentrq/backend/internal/mapper/api"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/auth"
	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
	"github.com/agentrq/agentrq/backend/internal/service/idgen"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
	"github.com/agentrq/agentrq/backend/internal/service/storage"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/mustafaturan/monoflake"
	"gorm.io/datatypes"
)

// CreateTaskFunc is a callback the MCP server calls when an LLM creates a task.
// The controller layer provides this so the MCP package doesn't import the controller.
type CreateTaskFunc func(ctx context.Context, task model.Task) (model.Task, error)
type UpdateTaskStatusFunc func(ctx context.Context, taskID int64, status string) (model.Task, error)
type GetTaskFunc func(ctx context.Context, taskID int64) (model.Task, error)
type ListTasksFunc func(ctx context.Context) ([]model.Task, error)
type ReplyFunc func(ctx context.Context, chatID string, text string, attachments []entity.Attachment, metadata any) (int64, error)
type UpdateMessageMetadataFunc func(ctx context.Context, taskID int64, messageID int64, metadata any) error
type UpdateWorkspaceAutoAllowedToolsFunc func(ctx context.Context, tools []string) error

type PermissionRequestParams struct {
	RequestID    string `json:"request_id"`
	ToolName     string `json:"tool_name"`
	Description  string `json:"description"`
	InputPreview string `json:"input_preview"`
}

// WorkspaceServer is a per-workspace MCP server that exposes the Claude Channels protocol.
type WorkspaceServer struct {
	workspaceID           int64
	userID                string
	mcpServer             *mcp.Server
	streamServer          *mcp.StreamableHTTPHandler
	createTask            CreateTaskFunc
	updateStatus          UpdateTaskStatusFunc
	getTask               GetTaskFunc
	listTasks             ListTasksFunc
	reply                 ReplyFunc
	updateMessageMetadata UpdateMessageMetadataFunc
	updateAutoAllowed     UpdateWorkspaceAutoAllowedToolsFunc
	bus                   *eventbus.Bus
	idgen                 idgen.Service
	storage               storage.Service
	pubsub                pubsub.Service
	tokenSvc              auth.TokenService
	autoAllowedToolsMu    sync.RWMutex
	autoAllowedTools      []string
	permissionRequestsMu  sync.RWMutex
	permissionRequests    map[string]string // requestID -> sessionID
	requestToolsMu        sync.RWMutex
	requestTools          map[string]string // requestID -> toolName
	requestParamsMu       sync.RWMutex
	requestParams         map[string]*PermissionRequestParams
	sessionTasksMu        sync.RWMutex
	sessionTasks          map[string]int64 // sessionID -> taskID

	permissionResponsesMu sync.RWMutex
	permissionResponses   map[string]int64 // requestID -> messageID
	metadataMu            sync.RWMutex
	icon                  string
	name                  string
	description           string
	archivedAt            *time.Time
	lastUpdateCheckAt     time.Time
	agentConnections      atomic.Int32
}

// CreateTaskParams is the input to the create_task tool.
type CreateTaskParams struct {
	Title        string `json:"title" jsonschema:"Short title of the task"`
	Body         string `json:"body" jsonschema:"Detailed description of the task or action needed"`
	Assignee     string `json:"assignee,omitempty" jsonschema:"Who should complete the task: 'human' or 'agent'. Default is 'agent'."`
	Attachments  []any  `json:"attachments,omitempty" jsonschema:"Optional attachments"`
	CronSchedule string `json:"cron_schedule,omitempty" jsonschema:"Optional cron schedule (5-field format: minute hour dom month dow). When set, creates a recurring cron task. Minimum granularity is hourly (e.g. '30 * * * *'). Sub-hourly schedules are not allowed."`
}

// UpdateTaskStatusParams is the input to the update_task_status tool.
type UpdateTaskStatusParams struct {
	TaskID string `json:"task_id" jsonschema:"The ID of the task to update"`
	Status string `json:"status" jsonschema:"New status: 'ongoing', 'completed', 'blocked', 'rejected', or 'notstarted'"`
}

// ReplyParams is the input to the reply tool.
type ReplyParams struct {
	ChatID      string              `json:"chat_id" jsonschema:"The conversation to reply in (from the chat_id tag field)"`
	Text        string              `json:"text" jsonschema:"The message text to send"`
	Attachments []entity.Attachment `json:"attachments,omitempty" jsonschema:"Optional attachments to include in the reply"`
}

// DownloadAttachmentParams is the input to the download_attachment tool.
type DownloadAttachmentParams struct {
	AttachmentID string `json:"attachment_id" jsonschema:"The ID of the attachment to download"`
}

// GetTaskMessagesParams is the input to the getTaskMessages tool.
type GetTaskMessagesParams struct {
	TaskID string `json:"task_id" jsonschema:"The ID of the task to get messages for"`
	Cursor int    `json:"cursor,omitempty" jsonschema:"The offset cursor. Default is 0."`
	Limit  int    `json:"limit,omitempty" jsonschema:"The maximum items to return. Default is 5."`
}

func NewWorkspaceServer(
	workspaceID int64,
	userID string,
	baseURL string,
	createTask CreateTaskFunc,
	updateStatus UpdateTaskStatusFunc,
	getTask GetTaskFunc,
	listTasks ListTasksFunc,
	reply ReplyFunc,
	updateMessageMetadata UpdateMessageMetadataFunc,
	updateAutoAllowed UpdateWorkspaceAutoAllowedToolsFunc,
	bus *eventbus.Bus,
	ids idgen.Service,
	store storage.Service,
	icon string,
	name string,
	description string,
	archivedAt *time.Time,
	autoAllowedTools []string,
	tokenSvc auth.TokenService,
	pubsub pubsub.Service,
) *WorkspaceServer {
	zlog.Info().Int64("workspace_id", workspaceID).Msg("new workspace server created")
	ps := &WorkspaceServer{
		workspaceID:           workspaceID,
		userID:                userID,
		createTask:            createTask,
		updateStatus:          updateStatus,
		getTask:               getTask,
		listTasks:             listTasks,
		reply:                 reply,
		updateMessageMetadata: updateMessageMetadata,
		updateAutoAllowed:     updateAutoAllowed,
		bus:                   bus,
		idgen:                 ids,
		storage:               store,
		tokenSvc:              tokenSvc,
		autoAllowedTools:      autoAllowedTools,
		permissionRequests:    make(map[string]string),
		requestTools:          make(map[string]string),
		requestParams:         make(map[string]*PermissionRequestParams),
		sessionTasks:          make(map[string]int64),
		permissionResponses:   make(map[string]int64),
		icon:                  icon,
		name:                  name,
		description:           description,
		archivedAt:            archivedAt,
		pubsub:                pubsub,
	}

	workspaceIDStr := monoflake.ID(workspaceID).String()
	var icons []mcp.Icon
	if icon != "" {
		icons = append(icons, mcp.Icon{Source: icon})
	}

	mcpSrv := mcp.NewServer(
		&mcp.Implementation{
			Name:    fmt.Sprintf("agentrq-workspace-%s", workspaceIDStr),
			Version: "1.0.0",
			Icons:   icons,
		},
		&mcp.ServerOptions{
			Capabilities: &mcp.ServerCapabilities{
				Experimental: map[string]any{
					"claude/channel":            map[string]any{},
					"claude/channel/permission": map[string]any{},
				},
			},
			Instructions: fmt.Sprintf(
				"You are connected to AgentRQ workspace %s.\n\n"+
					"## HOW THIS WORKS\n"+
					"- Messages from the human arrive as <channel source=\"agentrq\" chat_id=\"...\">.\n"+
					"- You reply using the `reply` tool, passing the chat_id from the tag.\n"+
					"- Use `createTask` to assign tasks to the human.\n"+
					"- The human is REMOTE and can ONLY see what you send via `reply`. Your stdout/text output is NOT visible to them.\n\n"+
					"## RULES (follow strictly)\n\n"+
					"1. **START**: When you receive a task, IMMEDIATELY call `updateTaskStatus` to set it to 'ongoing'. Then call `getWorkspace` to see the mission context.\n\n"+
					"2. **SHARE EVERYTHING**: The human cannot see your screen. You MUST proactively share:\n"+
					"   - What you're about to do and why\n"+
					"   - File paths you're reading or editing\n"+
					"   - Commands you're running and their output (especially errors)\n"+
					"   - Key decisions and trade-offs you're making\n"+
					"   - Code snippets or diffs when relevant\n"+
					"   - Any unexpected findings or issues\n\n"+
					"3. **PROGRESS UPDATES**: Send a `reply` every few steps or at every significant milestone. Do NOT go silent for long stretches. Examples of good updates:\n"+
					"   - \"Reading src/api/handler.go to understand the current structure...\"\n"+
					"   - \"Found the bug: the nil check on line 42 is missing. Fixing now.\"\n"+
					"   - \"Tests pass (12/12). Moving on to the frontend changes.\"\n"+
					"   - \"I ran `npm run build` and got this error: [error]. Investigating.\"\n\n"+
					"4. **ASK VIA REPLY**: If you need permission, clarification, or more info, use `reply` to ask. Do NOT ask in your text output — the human won't see it.\n\n"+
					"5. **COMPLETE**: When done, send a summary of all changes via `reply`, then set the task status to 'completed'. Use 'blocked' if you are stuck and need human help.\n",
				workspaceIDStr,
			),
		},
	)

	// Register the create_task tool
	mcp.AddTool(mcpSrv, &mcp.Tool{
		Name:        "createTask",
		Description: "Create a task for the human user. Returns the task ID.",
	}, ps.handleCreateTask)

	mcp.AddTool(mcpSrv, &mcp.Tool{
		Name:        "updateTaskStatus",
		Description: "Update the status of a task. Useful for moving tasks to ongoing or completed.",
	}, ps.handleUpdateTaskStatus)

	mcp.AddTool(mcpSrv, &mcp.Tool{
		Name:        "reply",
		Description: "Send a message to the current ongoing task. You can optionally include attachments.",
	}, ps.handleReply)

	mcp.AddTool(mcpSrv, &mcp.Tool{
		Name:        "downloadAttachment",
		Description: "Download the content of an attachment by its ID",
	}, ps.handleDownloadAttachment)

	mcp.AddTool(mcpSrv, &mcp.Tool{
		Name:        "getWorkspace",
		Description: "Returns the workspace title and mission description.",
	}, ps.handleGetWorkspace)

	mcp.AddTool(mcpSrv, &mcp.Tool{
		Name:        "getTaskMessages",
		Description: "Read the chat history and messages of a task. Returns messages ordered from oldest to newest with cursor-based pagination.",
	}, ps.handleGetTaskMessages)

	// Add middleware to handle incoming notifications (like permission_request)
	mcpSrv.AddReceivingMiddleware(ps.notificationMiddleware)

	cp := http.NewCrossOriginProtection()
	// Allow all origins for the MCP server in development (same syntax as ServeMux)
	cp.AddInsecureBypassPattern("/")

	streamHandler := mcp.NewStreamableHTTPHandler(func(request *http.Request) *mcp.Server {
		return mcpSrv
	}, &mcp.StreamableHTTPOptions{
		CrossOriginProtection: cp,
	})

	ps.mcpServer = mcpSrv
	ps.streamServer = streamHandler

	return ps
}

func (ps *WorkspaceServer) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := w.(http.Flusher); !ok {
			zlog.Warn().Msg("HTTP ResponseWriter does not support Flusher, SSE will be buffered")
		}

		sessID := r.Header.Get("Mcp-Session-Id")

		logID := sessID
		if len(logID) > 12 {
			logID = logID[:12] + "..."
		}

		// Track agent connection status
		isSSE := r.Header.Get("Accept") == "text/event-stream"
		if isSSE {
			count := ps.agentConnections.Add(1)
			if count == 1 {
				ps.bus.Publish(ps.workspaceID, ps.userID, eventbus.Event{
					Type:    "agent.connected",
					Payload: map[string]bool{"connected": true},
				})
			}
			defer func() {
				if ps.agentConnections.Add(-1) == 0 {
					ps.bus.Publish(ps.workspaceID, ps.userID, eventbus.Event{
						Type:    "agent.connected",
						Payload: map[string]bool{"connected": false},
					})
				}
			}()
		}

		zlog.Debug().Str("method", r.Method).Str("path", r.URL.Path).Str("session_id", logID).Bool("sse", isSSE).Msg("MCP request")
		ps.streamServer.ServeHTTP(w, r)
	})
}

func (ps *WorkspaceServer) IsAgentConnected() bool {
	return ps.agentConnections.Load() > 0
}

// SendChannelNotification delivers a human-originated message to any connected LLM session.
func (ps *WorkspaceServer) SendChannelNotification(ctx context.Context, taskID int64, content string) {
	zlog.Debug().Int64("workspace_id", ps.workspaceID).Int64("task_id", taskID).Msg("send MCP channel notification")

	params := map[string]any{
		"content": content,
		"meta": map[string]string{
			"chat_id":    monoflake.ID(taskID).String(),
			"message_id": monoflake.ID(taskID).String(),
			"user":       "human",
			"ts":         time.Now().Format(time.RFC3339),
		},
	}
	zlog.Debug().Interface("params", params).Msg("sending MCP notification params")

	// as the official SDK does not yet expose a public API for generic notifications.
	sessionCount := 0
	for sess := range ps.mcpServer.Sessions() {
		sessionCount++
		sessID := sess.ID()
		logID := sessID
		if len(logID) > 12 {
			logID = logID[:12] + "..."
		}

		authStatus := "UNAUTHENTICATED"
		if c, err := ps.tokenSvc.ValidateToken(sessID); err == nil {
			authStatus = "AUTHENTICATED: " + c.Subject
		}

		zlog.Debug().Str("session_id", logID).Str("auth", authStatus).Msg("found active session")
		v := reflect.ValueOf(sess).Elem()
		connField := v.FieldByName("conn")
		if connField.IsValid() {
			// Bypass export check using reflect.NewAt + unsafe.Pointer
			connField = reflect.NewAt(connField.Type(), unsafe.Pointer(connField.UnsafeAddr())).Elem()
			if !connField.IsNil() {
				method := connField.MethodByName("Notify")
				if method.IsValid() {
					results := method.Call([]reflect.Value{
						reflect.ValueOf(ctx),
						reflect.ValueOf("notifications/claude/channel"),
						reflect.ValueOf(params),
					})
					if len(results) > 0 && !results[0].IsNil() {
						err := results[0].Interface().(error)
						zlog.Error().Err(err).Msg("MCP notify error for session")
					}
				}
			}
		}
	}
	zlog.Debug().Int("sessions", sessionCount).Msg("MCP notification sent")
}

// StartPing pings all connected MCP client sessions every 30 seconds to keep connections alive.
func (ps *WorkspaceServer) StartPing() {
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			for sess := range ps.mcpServer.Sessions() {
				if err := sess.Ping(ctx, nil); err != nil {
					zlog.Warn().Err(err).Int64("workspace_id", ps.workspaceID).Str("session_id", sess.ID()).Msg("MCP ping error")
				}
			}
			cancel()
		}
	}()
}

// StartPoller checks for pending tasks periodically and pushes them if no ongoing tasks exist.
func (ps *WorkspaceServer) StartPoller(repo base.Repository) {
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			ps.metadataMu.RLock()
			isArchived := ps.archivedAt != nil
			ps.metadataMu.RUnlock()
			if isArchived {
				continue
			}
			req := entity.ListTasksRequest{WorkspaceID: ps.workspaceID, UserID: ps.userID}
			uid := monoflake.IDFromBase62(ps.userID).Int64()
			tasks, err := repo.ListTasks(context.Background(), req, uid)
			if err != nil {
				continue
			}

			hasOngoing := false
			var ongoingTask model.Task
			var pendingTasks []model.Task
			for _, t := range tasks {
				if t.Status == "ongoing" {
					hasOngoing = true
					ongoingTask = t
					break
				}
				if t.Status == "notstarted" && t.Assignee == "agent" {
					pendingTasks = append(pendingTasks, t)
				}
			}

			if hasOngoing {
				if time.Since(ps.lastUpdateCheckAt) > 5*time.Minute {
					msg := fmt.Sprintf("Status Check: You are currently working on task %s. Please provide a brief status update for the mission: %s", monoflake.ID(ongoingTask.ID).String(), ongoingTask.Title)
					ps.SendChannelNotification(context.Background(), ongoingTask.ID, msg)
					ps.lastUpdateCheckAt = time.Now()
				}
			} else if len(pendingTasks) > 0 {
				sort.Slice(pendingTasks, func(i, j int) bool {
					orderI := pendingTasks[i].SortOrder
					if orderI == 0 {
						orderI = float64(pendingTasks[i].CreatedAt.UnixMilli()) / 1000.0
					}
					orderJ := pendingTasks[j].SortOrder
					if orderJ == 0 {
						orderJ = float64(pendingTasks[j].CreatedAt.UnixMilli()) / 1000.0
					}
					if orderI != orderJ {
						return orderI < orderJ
					}
					return pendingTasks[i].ID < pendingTasks[j].ID
				})
				nextTask := pendingTasks[0]
				msg := fmt.Sprintf("Next assigned task:\nTitle: %s\nDetails: %s", nextTask.Title, nextTask.Body)
				if atts := formatModelAttachments(nextTask.Attachments); atts != "" {
					msg += "\n" + atts
				}
				ps.SendChannelNotification(context.Background(), nextTask.ID, msg)
			}
		}
	}()
}

func (ps *WorkspaceServer) UpdateMetadata(name, description, icon string) {
	ps.metadataMu.Lock()
	defer ps.metadataMu.Unlock()
	ps.name = name
	ps.description = description
	ps.icon = icon

	// MCP SDK might not allow easy dynamic implementation metadata update after Server creation
}

func (ps *WorkspaceServer) UpdateArchivedAt(at *time.Time) {
	ps.metadataMu.Lock()
	defer ps.metadataMu.Unlock()
	ps.archivedAt = at
}

func (ps *WorkspaceServer) UpdateAutoAllowedTools(tools []string) {
	ps.autoAllowedToolsMu.Lock()
	defer ps.autoAllowedToolsMu.Unlock()
	ps.autoAllowedTools = tools
}

// ── Tool handlers ─────────────────────────────────────────────────────────────

func (ps *WorkspaceServer) handleCreateTask(ctx context.Context, req *mcp.CallToolRequest, params CreateTaskParams) (*mcp.CallToolResult, any, error) {
	ps.emitTelemetry(ctx, ActionMCPToolCall, "createTask")
	ps.metadataMu.RLock()
	isArchived := ps.archivedAt != nil
	ps.metadataMu.RUnlock()

	if isArchived {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "workspace is archived and read-only"}},
		}, nil, nil
	}
	if params.Title == "" {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "title is required"}},
		}, nil, nil
	}
	if params.Body == "" {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "body is required"}},
		}, nil, nil
	}

	if params.CronSchedule != "" {
		if err := validateCronGranularity(params.CronSchedule); err != nil {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
			}, nil, nil
		}
	}

	var attachmentsJSON string
	if len(params.Attachments) > 0 {
		if b, err := json.Marshal(params.Attachments); err == nil {
			attachmentsJSON = string(b)
		}
	}

	now := time.Now()
	assignee := params.Assignee
	if assignee == "" {
		assignee = "agent"
	}

	status := "notstarted"
	if params.CronSchedule != "" {
		status = "cron"
	}

	t := model.Task{
		ID:           ps.idgen.NextID(),
		CreatedAt:    now,
		UpdatedAt:    now,
		WorkspaceID:  ps.workspaceID,
		UserID:       monoflake.IDFromBase62(ps.userID).Int64(),
		CreatedBy:    "agent",
		Assignee:     assignee,
		Status:       status,
		Title:        params.Title,
		Body:         params.Body,
		CronSchedule: params.CronSchedule,
	}

	if attachmentsJSON != "" {
		t.Attachments = datatypes.JSON(attachmentsJSON)
	}

	created, err := ps.createTask(ctx, t)
	if err != nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("failed to create task: %v", err)}},
		}, nil, nil
	}

	// Update session-to-task mapping for context-aware routing (like permissions)
	if req != nil && req.GetSession() != nil {
		sessID := req.GetSession().ID()
		ps.sessionTasksMu.Lock()
		ps.sessionTasks[sessID] = created.ID
		ps.sessionTasksMu.Unlock()
	}

	// Push SSE event to human subscribers
	ps.bus.Publish(ps.workspaceID, ps.userID, eventbus.Event{
		Type:    "task.created",
		Payload: mapper.FromModelTaskToView(created),
	})

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{
			Text: fmt.Sprintf("task created with id=%s", monoflake.ID(created.ID).String()),
		}},
	}, nil, nil
}

func (ps *WorkspaceServer) handleUpdateTaskStatus(ctx context.Context, req *mcp.CallToolRequest, params UpdateTaskStatusParams) (*mcp.CallToolResult, any, error) {
	ps.emitTelemetry(ctx, ActionMCPToolCall, "updateTaskStatus")
	ps.metadataMu.RLock()
	isArchived := ps.archivedAt != nil
	ps.metadataMu.RUnlock()

	if isArchived {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "workspace is archived and read-only"}},
		}, nil, nil
	}
	if params.TaskID == "" {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "task_id is required"}},
		}, nil, nil
	}
	if params.Status == "" {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "status is required"}},
		}, nil, nil
	}

	var taskID int64
	// Try parsing as Base62 first (standard for this app)
	id := monoflake.IDFromBase62(params.TaskID)
	if id != 0 {
		taskID = id.Int64()
	} else {
		// Fallback to numeric if Base62 fails and it's purely digits
		if tid, err := strconv.ParseInt(params.TaskID, 10, 64); err == nil {
			taskID = tid
		} else {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: "invalid task_id format"}},
			}, nil, nil
		}
	}

	updated, err := ps.updateStatus(ctx, taskID, params.Status)
	if err != nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("failed to update task status: %v", err)}},
		}, nil, nil
	}

	// Update session-to-task mapping for context-aware routing (like permissions)
	if req != nil && req.GetSession() != nil {
		sessID := req.GetSession().ID()
		ps.sessionTasksMu.Lock()
		ps.sessionTasks[sessID] = taskID
		ps.sessionTasksMu.Unlock()
	}

	// Push SSE event to human subscribers
	ps.bus.Publish(ps.workspaceID, ps.userID, eventbus.Event{
		Type:    "task.updated",
		Payload: mapper.FromModelTaskToView(updated),
	})

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{
			Text: fmt.Sprintf("task %s updated to status=%s", monoflake.ID(taskID).String(), params.Status),
		}},
	}, nil, nil
}

func (ps *WorkspaceServer) handleReply(ctx context.Context, req *mcp.CallToolRequest, params ReplyParams) (*mcp.CallToolResult, any, error) {
	ps.emitTelemetry(ctx, ActionMCPToolCall, "reply")
	ps.metadataMu.RLock()
	isArchived := ps.archivedAt != nil
	ps.metadataMu.RUnlock()

	if isArchived {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "workspace is archived and read-only"}},
		}, nil, nil
	}
	if params.ChatID == "" || params.Text == "" {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "chat_id and text are required"}},
		}, nil, nil
	}

	if _, err := ps.reply(ctx, params.ChatID, params.Text, params.Attachments, nil); err != nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("failed to deliver reply: %v", err)}},
		}, nil, nil
	}

	// Update session-to-task mapping for context-aware routing (like permissions)
	if tid := monoflake.IDFromBase62(params.ChatID).Int64(); tid != 0 && req != nil && req.GetSession() != nil {
		sessID := req.GetSession().ID()
		ps.sessionTasksMu.Lock()
		ps.sessionTasks[sessID] = tid
		ps.sessionTasksMu.Unlock()
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: "reply sent"}},
	}, nil, nil
}

func (ps *WorkspaceServer) handleDownloadAttachment(ctx context.Context, req *mcp.CallToolRequest, params DownloadAttachmentParams) (*mcp.CallToolResult, any, error) {
	ps.emitTelemetry(ctx, ActionMCPToolCall, "downloadAttachment")
	if params.AttachmentID == "" {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "attachment_id is required"}},
		}, nil, nil
	}

	tasks, err := ps.listTasks(ctx)
	if err != nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("failed to list tasks: %v", err)}},
		}, nil, nil
	}

	for _, t := range tasks {
		// Check task attachments
		if len(t.Attachments) > 0 {
			var atts []entity.Attachment
			if err := json.Unmarshal(t.Attachments, &atts); err == nil {
				for _, a := range atts {
					if a.ID == params.AttachmentID {
						data, _ := ps.storage.Load(a.ID)
						return &mcp.CallToolResult{
							Content: []mcp.Content{&mcp.TextContent{Text: data}}, // Return base64 data
						}, nil, nil
					}
				}
			}
		}
		// Check message attachments
		for _, m := range t.Messages {
			if len(m.Attachments) > 0 {
				var atts []entity.Attachment
				if err := json.Unmarshal(m.Attachments, &atts); err == nil {
					for _, a := range atts {
						if a.ID == params.AttachmentID {
							data, _ := ps.storage.Load(a.ID)
							return &mcp.CallToolResult{
								Content: []mcp.Content{&mcp.TextContent{Text: data}},
							}, nil, nil
						}
					}
				}
			}
		}
	}

	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: "attachment not found"}},
	}, nil, nil
}

func (ps *WorkspaceServer) handleGetWorkspace(ctx context.Context, req *mcp.CallToolRequest, params any) (*mcp.CallToolResult, any, error) {
	ps.emitTelemetry(ctx, ActionMCPToolCall, "getWorkspace")
	ps.metadataMu.RLock()
	name := ps.name
	desc := ps.description
	ps.metadataMu.RUnlock()

	tasks, err := ps.listTasks(ctx)
	if err != nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("failed to fetch stats: %v", err)}},
		}, nil, nil
	}

	stats := map[string]int{
		"notstarted": 0,
		"ongoing":    0,
		"completed":  0,
		"rejected":   0,
		"blocked":    0,
	}

	for _, t := range tasks {
		if _, ok := stats[t.Status]; ok {
			stats[t.Status]++
		}
	}

	content := fmt.Sprintf("Workspace: %s\nDescription: %s\n\nTask Statistics:\n- Not Started: %d\n- Ongoing: %d\n- Completed: %d\n- Rejected: %d\n- Blocked: %d",
		name, desc, stats["notstarted"], stats["ongoing"], stats["completed"], stats["rejected"], stats["blocked"])

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: content}},
	}, nil, nil
}

func (ps *WorkspaceServer) handleGetTaskMessages(ctx context.Context, req *mcp.CallToolRequest, params GetTaskMessagesParams) (*mcp.CallToolResult, any, error) {
	ps.emitTelemetry(ctx, ActionMCPToolCall, "getTaskMessages")
	if params.TaskID == "" {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "task_id is required"}},
		}, nil, nil
	}

	if params.Limit <= 0 {
		params.Limit = 5
	}
	if params.Cursor < 0 {
		params.Cursor = 0
	}

	var taskID int64
	id := monoflake.IDFromBase62(params.TaskID)
	if id != 0 {
		taskID = id.Int64()
	} else {
		if tid, err := strconv.ParseInt(params.TaskID, 10, 64); err == nil {
			taskID = tid
		} else {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: "invalid task_id format"}},
			}, nil, nil
		}
	}

	task, err := ps.getTask(ctx, taskID)
	if err != nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("failed to get task: %v", err)}},
		}, nil, nil
	}

	messages := task.Messages
	sort.Slice(messages, func(i, j int) bool {
		return messages[i].ID < messages[j].ID
	})

	total := len(messages)
	start := params.Cursor
	if start > total {
		start = total
	}
	end := start + params.Limit
	if end > total {
		end = total
	}

	paginated := messages[start:end]

	output := make([]map[string]any, 0)
	for _, m := range paginated {
		// Parse attachments — include metadata only, not base64 data
		type attMeta struct {
			ID       string `json:"id"`
			Filename string `json:"filename"`
			MimeType string `json:"mimeType"`
		}
		var attachments []attMeta
		if len(m.Attachments) > 0 {
			var atts []entity.Attachment
			if err := json.Unmarshal(m.Attachments, &atts); err == nil {
				for _, a := range atts {
					if a.ID != "" {
						attachments = append(attachments, attMeta{
							ID:       a.ID,
							Filename: a.Filename,
							MimeType: a.MimeType,
						})
					}
				}
			}
		}
		output = append(output, map[string]any{
			"id":          monoflake.ID(m.ID).String(),
			"sender":      m.Sender,
			"text":        m.Text,
			"created_at":  m.CreatedAt,
			"attachments": attachments,
			"metadata":    string(m.Metadata),
		})
	}

	b, _ := json.Marshal(map[string]any{
		"messages": output,
		"total":    total,
		"cursor":   end,
	})

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(b)}},
	}, nil, nil
}
func (ps *WorkspaceServer) notificationMiddleware(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		zlog.Debug().Str("method", method).Msg("MCP incoming")
		if method == "notifications/claude/channel/permission_request" {
			ps.emitTelemetry(ctx, ActionMCPNotification, "permission_request")
			params := req.GetParams()
			var p PermissionRequestParams
			b, _ := json.Marshal(params)
			_ = json.Unmarshal(b, &p)

			sessID := ""
			if req != nil && req.GetSession() != nil {
				sessID = req.GetSession().ID()
				ps.permissionRequestsMu.Lock()
				ps.permissionRequests[p.RequestID] = sessID
				ps.permissionRequestsMu.Unlock()
			}

			ps.requestToolsMu.Lock()
			ps.requestTools[p.RequestID] = p.ToolName
			ps.requestToolsMu.Unlock()

			ps.requestParamsMu.Lock()
			ps.requestParams[p.RequestID] = &p
			ps.requestParamsMu.Unlock()

			// Check if tool is auto-allowed
			ps.autoAllowedToolsMu.RLock()
			isAutoAllowed := ps.checkAutoAllow(p.ToolName, p.InputPreview)
			ps.autoAllowedToolsMu.RUnlock()

			if isAutoAllowed {
				zlog.Info().Str("request_id", p.RequestID).Str("tool", p.ToolName).Msg("auto-allowing permission request")
				go func() {
					time.Sleep(100 * time.Millisecond) // Give session time to stabilize if needed
					_ = ps.SendPermissionVerdict(context.Background(), p.RequestID, "allow")
				}()
				ps.emitTelemetry(context.Background(), ActionMCPNotification, "permission_auto_allow")
				return nil, nil
			}

			ps.sessionTasksMu.RLock()
			taskID, ok := ps.sessionTasks[sessID]
			ps.sessionTasksMu.RUnlock()

			if ok {
				zlog.Info().Str("request_id", p.RequestID).Int64("task_id", taskID).Msg("relaying permission request")
				// Type "permission_request" helps UI render buttons
				metadata := map[string]any{
					"type":          "permission_request",
					"request_id":    p.RequestID,
					"tool_name":     p.ToolName,
					"description":   p.Description,
					"input_preview": p.InputPreview,
				}
				_, _ = ps.reply(ctx, monoflake.ID(taskID).String(), fmt.Sprintf("Permission requested for %s: %s", p.ToolName, p.Description), nil, metadata)
			} else {
				zlog.Warn().Str("request_id", p.RequestID).Str("session_id", sessID).Msg("could not relay permission request: no active task")
			}
			return nil, nil // Notifications must return nil, nil
		}
		return next(ctx, method, req)
	}
}

func (ps *WorkspaceServer) cleanupRequest(requestID string) {
	ps.permissionRequestsMu.Lock()
	delete(ps.permissionRequests, requestID)
	ps.permissionRequestsMu.Unlock()

	ps.requestToolsMu.Lock()
	delete(ps.requestTools, requestID)
	ps.requestToolsMu.Unlock()

	ps.requestParamsMu.Lock()
	delete(ps.requestParams, requestID)
	ps.requestParamsMu.Unlock()

	ps.permissionResponsesMu.Lock()
	delete(ps.permissionResponses, requestID)
	ps.permissionResponsesMu.Unlock()
}

func (ps *WorkspaceServer) SendPermissionVerdict(ctx context.Context, requestID string, behavior string) error {
	defer ps.cleanupRequest(requestID)

	ps.permissionRequestsMu.RLock()
	sessID, ok := ps.permissionRequests[requestID]
	ps.permissionRequestsMu.RUnlock()

	if !ok {
		return fmt.Errorf("unknown request ID (expired): %s", requestID)
	}

	ps.sessionTasksMu.RLock()
	taskID, okTask := ps.sessionTasks[sessID]
	ps.sessionTasksMu.RUnlock()

	effectiveBehavior := behavior
	if behavior == "allow_always" {
		effectiveBehavior = "allow"

		ps.requestToolsMu.RLock()
		toolName := ps.requestTools[requestID]
		ps.requestToolsMu.RUnlock()

		ps.requestParamsMu.RLock()
		reqParams := ps.requestParams[requestID]
		ps.requestParamsMu.RUnlock()

		if toolName != "" {
			rule := ps.buildAutoAllowRule(toolName, reqParams)

			ps.autoAllowedToolsMu.Lock()
			exists := false
			for _, t := range ps.autoAllowedTools {
				if t == rule {
					exists = true
					break
				}
			}
			if !exists {
				ps.autoAllowedTools = append(ps.autoAllowedTools, rule)
				if ps.updateAutoAllowed != nil {
					_ = ps.updateAutoAllowed(ctx, ps.autoAllowedTools)
				}
			}
			ps.autoAllowedToolsMu.Unlock()
			zlog.Info().Str("rule", rule).Msg("auto-allow rule saved")
		}
	}

	switch effectiveBehavior {
	case "allow":
		ps.emitTelemetry(ctx, ActionMCPNotification, "permission_manual_allow")
	case "deny":
		ps.emitTelemetry(ctx, ActionMCPNotification, "permission_manual_deny")
	}

	// Notify Claude Code session
	params := map[string]any{
		"request_id": requestID,
		"behavior":   effectiveBehavior, // "allow" | "deny"
	}

	for sess := range ps.mcpServer.Sessions() {
		if sess.ID() == sessID {
			// session was found
			v := reflect.ValueOf(sess).Elem()
			connField := v.FieldByName("conn")
			if connField.IsValid() {
				connField = reflect.NewAt(connField.Type(), unsafe.Pointer(connField.UnsafeAddr())).Elem()
				if !connField.IsNil() {
					method := connField.MethodByName("Notify")
					if method.IsValid() {
						// Update the original permission request message metadata with the verdict
						if okTask {
							ps.permissionResponsesMu.RLock()
							msgID, hasMsg := ps.permissionResponses[requestID]
							ps.permissionResponsesMu.RUnlock()

							if hasMsg {
								_ = ps.updateMessageMetadata(ctx, taskID, msgID, map[string]any{"status": behavior})
							}
						}

						method.Call([]reflect.Value{
							reflect.ValueOf(ctx),
							reflect.ValueOf("notifications/claude/channel/permission"),
							reflect.ValueOf(params),
						})
						return nil
					}
				}
			}
		}
	}

	return fmt.Errorf("session %s not found", sessID)
}

func (ps *WorkspaceServer) HandleCustomNotification(ctx context.Context, sessionID string, data []byte) {
	var msg struct {
		Method string                  `json:"method"`
		Params PermissionRequestParams `json:"params"`
	}
	if err := json.Unmarshal(data, &msg); err != nil {
		return
	}

	if msg.Method == "notifications/claude/channel/permission_request" {
		p := msg.Params
		ps.permissionRequestsMu.Lock()
		ps.permissionRequests[p.RequestID] = sessionID
		ps.permissionRequestsMu.Unlock()

		ps.requestToolsMu.Lock()
		ps.requestTools[p.RequestID] = p.ToolName
		ps.requestToolsMu.Unlock()

		ps.requestParamsMu.Lock()
		ps.requestParams[p.RequestID] = &p
		ps.requestParamsMu.Unlock()

		// Check if tool is auto-allowed (same logic as notificationMiddleware)
		ps.autoAllowedToolsMu.RLock()
		isAutoAllowed := ps.checkAutoAllow(p.ToolName, p.InputPreview)
		ps.autoAllowedToolsMu.RUnlock()

		if isAutoAllowed {
			zlog.Info().Str("request_id", p.RequestID).Str("tool", p.ToolName).Msg("auto-allowing permission request (via custom notification)")
			go func() {
				time.Sleep(100 * time.Millisecond)
				_ = ps.SendPermissionVerdict(context.Background(), p.RequestID, "allow")
			}()
			ps.emitTelemetry(context.Background(), ActionMCPNotification, "permission_auto_allow")
			return
		}

		ps.sessionTasksMu.RLock()
		taskID, ok := ps.sessionTasks[sessionID]
		ps.sessionTasksMu.RUnlock()

		if ok {
			zlog.Info().Str("request_id", p.RequestID).Int64("task_id", taskID).Str("session_id", sessionID).Msg("relaying permission request (custom notification)")
			metadata := map[string]any{
				"type":          "permission_request",
				"request_id":    p.RequestID,
				"tool_name":     p.ToolName,
				"description":   p.Description,
				"input_preview": p.InputPreview,
				"status":        "pending",
			}
			msgID, _ := ps.reply(ctx, monoflake.ID(taskID).String(), fmt.Sprintf("Permission requested for %s: %s", p.ToolName, p.Description), nil, metadata)
			if msgID != 0 {
				ps.permissionResponsesMu.Lock()
				ps.permissionResponses[p.RequestID] = msgID
				ps.permissionResponsesMu.Unlock()
			}
		} else {
			zlog.Warn().Str("request_id", p.RequestID).Str("session_id", sessionID).Msg("could not relay permission request: no active task (custom notification)")
		}
	}
}

func (ps *WorkspaceServer) emitTelemetry(ctx context.Context, action Action, toolOrMethod string) {
	uid := monoflake.IDFromBase62(ps.userID).Int64()
	ps.pubsub.Publish(ctx, pubsub.PublishRequest{
		PubSubID: entity.PubSubTopicMCP,
		Event: MCPEvent{
			Action:      action,
			WorkspaceID: ps.workspaceID,
			UserID:      uid,
			ToolName:    toolOrMethod,
			Method:      toolOrMethod,
			Actor:       2, // Agent
		},
	})
}

// formatModelAttachments builds an attachment summary from raw JSON (model.Task.Attachments)
// for inclusion in LLM notifications so the agent can call downloadAttachment.
func formatModelAttachments(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	var atts []entity.Attachment
	if err := json.Unmarshal(raw, &atts); err != nil {
		return ""
	}
	parts := make([]string, 0, len(atts))
	for _, a := range atts {
		if a.ID != "" {
			parts = append(parts, fmt.Sprintf("  - id=%s name=%s type=%s", a.ID, a.Filename, a.MimeType))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "Attachments:\n" + strings.Join(parts, "\n")
}

// validateCronGranularity validates that the cron schedule is syntactically valid
// and has a minimum repeat interval of one hour (no sub-hourly schedules).
// The minute field must be a single fixed value (0-59); wildcards, steps, ranges,
// and comma-lists in the minute field are all rejected because they would cause
// the job to fire more than once per hour.
func validateCronGranularity(schedule string) error {
	fields := strings.Fields(schedule)
	if len(fields) != 5 {
		return fmt.Errorf("cron schedule must have exactly 5 fields (minute hour dom month dow)")
	}

	minuteField := fields[0]

	// Reject wildcards, steps (*/n), ranges (a-b), and comma-lists in the minute field.
	if minuteField == "*" ||
		strings.Contains(minuteField, "/") ||
		strings.Contains(minuteField, "-") ||
		strings.Contains(minuteField, ",") {
		return fmt.Errorf("cron schedule granularity too fine: minute field must be a single fixed value (0-59), not %q — only hourly or coarser schedules are allowed", minuteField)
	}

	minute, err := strconv.Atoi(minuteField)
	if err != nil || minute < 0 || minute > 59 {
		return fmt.Errorf("cron schedule minute field must be a valid integer between 0 and 59, got %q", minuteField)
	}

	// Validate overall syntax using the standard 5-field parser.
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	if _, err := parser.Parse(schedule); err != nil {
		return fmt.Errorf("invalid cron schedule: %w", err)
	}

	return nil
}
