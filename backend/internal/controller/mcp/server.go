package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	zlog "github.com/rs/zerolog/log"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	mapper "github.com/agentrq/agentrq/backend/internal/mapper/api"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/auth"
	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
	"github.com/agentrq/agentrq/backend/internal/service/idgen"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
	"github.com/agentrq/agentrq/backend/internal/service/schedule"
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

// ListTasksFilter specifies optional filters for listing tasks.
type ListTasksFilter struct {
	Status []string
	Limit  int
}

type ListTasksFunc func(ctx context.Context, filter ListTasksFilter) ([]model.Task, error)
type GetNextTaskFunc func(ctx context.Context) (model.Task, error)
type ReplyFunc func(ctx context.Context, chatID string, text string, attachments []entity.Attachment, metadata any) (int64, error)
type UpdateMessageMetadataFunc func(ctx context.Context, taskID int64, messageID int64, metadata any) error
type UpdateWorkspaceAutoAllowedToolsFunc func(ctx context.Context, tools []string) error

// WorkflowRunContext identifies the workflow run a publishing task belongs to.
// The zero value means "not part of a workflow run", which keeps the publish on
// the original global-trigger path.
type WorkflowRunContext struct {
	// WorkflowID is the run the publishing task belongs to, read from the task
	// itself. The task is the only thing that knows this: an agent is told which
	// task it is publishing for, never which workflow that task sits in.
	WorkflowID int64
	// Depth is how many hops already preceded the publishing task, carried so
	// the consumer's runaway guard survives the task boundary.
	Depth int
}

// PublishEventFunc fires a named event. run scopes the consumer's fan-out to a
// single workflow's steps instead of the global triggers.
type PublishEventFunc func(ctx context.Context, eventName string, payload string, faq []entity.EventFAQ, run WorkflowRunContext) error

// RecordToolCallFunc persists a tool-call permission decision (auto-allowed or
// pending a manual verdict) so it can be shown as a "tool calls" list, separate
// from the message thread.
type RecordToolCallFunc func(ctx context.Context, tc model.ToolCall) (model.ToolCall, error)

// UpdateToolCallStatusFunc updates a previously-recorded tool call once a manual
// permission request is resolved ("allowed" or "denied").
type UpdateToolCallStatusFunc func(ctx context.Context, id int64, status string) error

type PermissionRequestParams struct {
	RequestID    string `json:"request_id"`
	TaskID       string `json:"task_id,omitempty"`
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
	getNextTask           GetNextTaskFunc
	reply                 ReplyFunc
	updateMessageMetadata UpdateMessageMetadataFunc
	updateAutoAllowed     UpdateWorkspaceAutoAllowedToolsFunc
	publishEvent          PublishEventFunc
	recordToolCall        RecordToolCallFunc
	updateToolCallStatus  UpdateToolCallStatusFunc
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

	requestTaskIDsMu      sync.RWMutex
	requestTaskIDs        map[string]int64 // requestID -> taskID (resolved at request time)
	permissionResponsesMu sync.RWMutex
	permissionResponses   map[string]int64 // requestID -> messageID
	toolCallIDsMu         sync.RWMutex
	toolCallIDs           map[string]int64 // requestID -> ToolCall row ID (manual requests only, until resolved)
	metadataMu            sync.RWMutex
	icon                  string
	name                  string
	description           string
	archivedAt            *time.Time
	lastUpdateCheckAt     time.Time
	agentConnections      atomic.Int32

	// done is closed by Close to stop the StartPing/StartPoller ticker goroutines, so a
	// removed workspace server does not leak them for the lifetime of the process.
	done      chan struct{}
	closeOnce sync.Once
}

// Close stops the server's background goroutines (StartPing / StartPoller). It is
// idempotent and safe to call more than once (e.g. from Manager.Remove).
func (ps *WorkspaceServer) Close() {
	ps.closeOnce.Do(func() {
		if ps.done != nil {
			close(ps.done)
		}
	})
}

// CreateTaskParams is the input to the create_task tool.
type CreateTaskParams struct {
	Title        string `json:"title" jsonschema:"Short title of the task"`
	Body         string `json:"body" jsonschema:"Detailed description of the task or action needed"`
	Assignee     string `json:"assignee,omitempty" jsonschema:"Who should complete the task: 'human' or 'agent'. Default is 'agent'."`
	Attachments  []any  `json:"attachments,omitempty" jsonschema:"Optional attachments"`
	CronSchedule string `json:"cronSchedule,omitempty" jsonschema:"Optional cron schedule (5-field format: minute hour dom month dow). For RECURRING tasks (dom and month use wildcards) the minimum granularity is hourly — the minute field must be a single integer 0-59, not a wildcard or step (e.g. '30 * * * *'). For ONE-TIME tasks (fixed dom and month, e.g. '30 14 25 4 *') any fixed minute value 0-59 is accepted, enabling minute-level precision."`
	EventID      string `json:"eventId,omitempty" jsonschema:"Optional event ID (base62) — when this task completes the named event is published automatically."`
}

// PublishEventParams is the input to the publishEvent tool.
type PublishEventParams struct {
	Name    string            `json:"name" jsonschema:"The event name to publish (must match an existing event in this workspace's owner account)"`
	Payload string            `json:"payload,omitempty" jsonschema:"Unstructured text payload describing what happened"`
	TaskID  string            `json:"taskId,omitempty" jsonschema:"The ID of the task you are completing (base62). Copy it from that task's publishEvent instruction. It identifies which workflow run this publish continues, so omitting it can leave the run stranded."`
	FAQ     []PublishEventFAQ `json:"faq,omitempty" jsonschema:"Optional question-answer pairs providing additional context"`
}

// PublishEventFAQ is a single question-answer pair in a publishEvent call.
type PublishEventFAQ struct {
	Q string `json:"q"`
	A string `json:"a"`
}

// UpdateTaskStatusParams is the input to the update_task_status tool.
type UpdateTaskStatusParams struct {
	TaskID string `json:"taskId" jsonschema:"The ID of the task to update"`
	Status string `json:"status" jsonschema:"New status: 'ongoing', 'completed', 'blocked', 'rejected', or 'notstarted'"`
}

// ReplyParams is the input to the reply tool.
type ReplyParams struct {
	ChatID      string              `json:"chatId" jsonschema:"The conversation to reply in (from the chat_id tag field)"`
	Text        string              `json:"text" jsonschema:"The message text to send"`
	Attachments []entity.Attachment `json:"attachments,omitempty" jsonschema:"Optional attachments to include in the reply"`
}

// DownloadAttachmentParams is the input to the download_attachment tool.
type DownloadAttachmentParams struct {
	AttachmentID string `json:"attachmentId" jsonschema:"The ID of the attachment to download"`
	TaskID       string `json:"taskId" jsonschema:"The ID of the task containing the attachment"`
}

// GetTaskParams is the input to the getTask tool.
type GetTaskParams struct {
	TaskID              string `json:"taskId,omitempty" jsonschema:"The ID of the task to fetch. If omitted, the next available 'not started' task assigned to the agent is returned (dequeues the work queue)."`
	IncludeConversation bool   `json:"includeConversation,omitempty" jsonschema:"When true, include the task's conversation messages in the response."`
	Cursor              int    `json:"cursor,omitempty" jsonschema:"Message pagination offset cursor; only used when includeConversation is true. Default is 0."`
	Limit               int    `json:"limit,omitempty" jsonschema:"Maximum number of messages to return; only used when includeConversation is true. Default is 5."`
}

func NewWorkspaceServer(
	workspaceID int64,
	userID string,
	baseURL string,
	createTask CreateTaskFunc,
	updateStatus UpdateTaskStatusFunc,
	getTask GetTaskFunc,
	listTasks ListTasksFunc,
	getNextTask GetNextTaskFunc,
	reply ReplyFunc,
	updateMessageMetadata UpdateMessageMetadataFunc,
	updateAutoAllowed UpdateWorkspaceAutoAllowedToolsFunc,
	publishEvent PublishEventFunc,
	recordToolCall RecordToolCallFunc,
	updateToolCallStatus UpdateToolCallStatusFunc,
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
		done:                  make(chan struct{}),
		createTask:            createTask,
		updateStatus:          updateStatus,
		getTask:               getTask,
		listTasks:             listTasks,
		getNextTask:           getNextTask,
		reply:                 reply,
		updateMessageMetadata: updateMessageMetadata,
		updateAutoAllowed:     updateAutoAllowed,
		publishEvent:          publishEvent,
		recordToolCall:        recordToolCall,
		updateToolCallStatus:  updateToolCallStatus,
		bus:                   bus,
		idgen:                 ids,
		storage:               store,
		tokenSvc:              tokenSvc,
		autoAllowedTools:      autoAllowedTools,
		permissionRequests:    make(map[string]string),
		requestTools:          make(map[string]string),
		requestParams:         make(map[string]*PermissionRequestParams),
		sessionTasks:          make(map[string]int64),
		requestTaskIDs:        make(map[string]int64),
		permissionResponses:   make(map[string]int64),
		toolCallIDs:           make(map[string]int64),
		icon:                  icon,
		name:                  name,
		description:           description,
		archivedAt:            archivedAt,
		pubsub:                pubsub,
		lastUpdateCheckAt:     time.Now(), // defer first status check by a full hour
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
		Name:        "getTask",
		Description: "Fetch a task. With no taskId, returns the next available \"not started\" task assigned to the agent (dequeues the work queue). With a taskId, returns that specific task. Set includeConversation=true to also include the task's chat history (oldest to newest, with cursor-based pagination).",
	}, ps.handleGetTask)

	mcp.AddTool(mcpSrv, &mcp.Tool{
		Name:        "publishEvent",
		Description: "Publish a named event so that subscriber workspaces are notified and their trigger tasks are created automatically. When the task you are completing includes a publishEvent instruction, copy the name and taskId from it exactly as written — taskId is what identifies the workflow run being continued. Write the payload yourself, plus an optional faq.",
	}, ps.handlePublishEvent)

	// Add middleware to handle incoming notifications (like permission_request)
	mcpSrv.AddReceivingMiddleware(ps.notificationMiddleware, ps.discoverCacheMiddleware)

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
					Payload: map[string]any{"connected": true, "workspaceId": ps.workspaceID},
				})
				ps.emitTelemetry(r.Context(), ActionMCPConnect, "connect", clientIdentityFromHTTPRequest(r))
			}
			defer func() {
				if ps.agentConnections.Add(-1) == 0 {
					ps.bus.Publish(ps.workspaceID, ps.userID, eventbus.Event{
						Type:    "agent.connected",
						Payload: map[string]any{"connected": false, "workspaceId": ps.workspaceID},
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
	//zlog.Debug().Interface("params", params).Msg("sending MCP notification params")

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

// StartPing pings all connected MCP client sessions every minute to keep connections alive.
// If a session's underlying stream is already closed or times out, it is explicitly closed
// so the SDK removes it from its session registry, preventing repeated errors.
func (ps *WorkspaceServer) StartPing() {
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ps.done:
				return
			case <-ticker.C:
			}
			for sess := range ps.mcpServer.Sessions() {
				sess := sess // shadow for closure capture
				go func() {
					ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
					defer cancel()
					if err := sess.Ping(ctx, nil); err != nil {
						errStr := err.Error()
						// Treat timeout, stream closed, or already closed as reasons to evict
						if errors.Is(err, context.DeadlineExceeded) ||
							strings.Contains(errStr, "stream not connected") ||
							strings.Contains(errStr, "already closed") {
							zlog.Debug().Err(err).Int64("workspace_id", ps.workspaceID).Str("session_id", sess.ID()).Msg("MCP session unhealthy; evicting from registry")
							if closeErr := sess.Close(); closeErr != nil {
								zlog.Debug().Err(closeErr).Str("session_id", sess.ID()).Msg("MCP session close error (expected)")
							}
						} else {
							zlog.Warn().Err(err).Int64("workspace_id", ps.workspaceID).Str("session_id", sess.ID()).Msg("MCP ping error")
						}
					}
				}()
			}
		}
	}()
}

// StartPoller checks for pending tasks periodically and pushes them if no ongoing tasks exist.
func (ps *WorkspaceServer) StartPoller(repo base.Repository) {
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ps.done:
				return
			case <-ticker.C:
			}
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
				if time.Since(ps.lastUpdateCheckAt) > time.Hour {
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
				// The ID is part of the push because a task body can instruct the
				// agent to quote it back when publishing an event, and this path
				// is how workflow-step tasks are delivered.
				msg := fmt.Sprintf("Next assigned task:\nID: %s\nTitle: %s\nDetails: %s", monoflake.ID(nextTask.ID).String(), nextTask.Title, nextTask.Body)
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
	ps.emitTelemetry(ctx, ActionMCPToolCall, "createTask", clientIdentityFromRequest(req))
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
		if err := schedule.ValidateCronGranularity(params.CronSchedule); err != nil {
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

	eventID := monoflake.IDFromBase62(params.EventID).Int64()

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
		EventID:      eventID,
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
	ps.emitTelemetry(ctx, ActionMCPToolCall, "updateTaskStatus", clientIdentityFromRequest(req))
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
			Content: []mcp.Content{&mcp.TextContent{Text: "taskId is required"}},
		}, nil, nil
	}
	if params.Status == "" {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "status is required"}},
		}, nil, nil
	}

	id := monoflake.IDFromBase62(params.TaskID)
	if id == 0 {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "invalid taskId format"}},
		}, nil, nil
	}
	taskID := id.Int64()

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
	ps.emitTelemetry(ctx, ActionMCPToolCall, "reply", clientIdentityFromRequest(req))
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
	ps.emitTelemetry(ctx, ActionMCPToolCall, "downloadAttachment", clientIdentityFromRequest(req))
	if params.AttachmentID == "" {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "attachmentId is required"}},
		}, nil, nil
	}
	if params.TaskID == "" {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "taskId is required"}},
		}, nil, nil
	}

	id := monoflake.IDFromBase62(params.TaskID)
	if id == 0 {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "invalid taskId format"}},
		}, nil, nil
	}
	taskID := id.Int64()

	task, err := ps.getTask(ctx, taskID)
	if err != nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("failed to get task: %v", err)}},
		}, nil, nil
	}

	// Check task attachments
	if len(task.Attachments) > 0 {
		var atts []entity.Attachment
		if err := json.Unmarshal(task.Attachments, &atts); err == nil {
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
	for _, m := range task.Messages {
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

	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: "attachment not found in task"}},
	}, nil, nil
}

func (ps *WorkspaceServer) handlePublishEvent(ctx context.Context, req *mcp.CallToolRequest, params PublishEventParams) (*mcp.CallToolResult, any, error) {
	ps.emitTelemetry(ctx, ActionMCPToolCall, "publishEvent", clientIdentityFromRequest(req))
	if params.Name == "" {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "name is required"}},
		}, nil, nil
	}
	faq := make([]entity.EventFAQ, len(params.FAQ))
	for i, f := range params.FAQ {
		faq[i] = entity.EventFAQ{Q: f.Q, A: f.A}
	}
	run := ps.currentWorkflowRun(ctx, req, params.TaskID)
	if err := ps.publishEvent(ctx, params.Name, params.Payload, faq, run); err != nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("failed to publish event: %v", err)}},
		}, nil, nil
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{
			Text: fmt.Sprintf("event %q published", params.Name),
		}},
	}, nil, nil
}

// currentWorkflowRun reports the workflow run the publishing task belongs to,
// so a chained event stays inside its own workflow instead of falling back to
// the global triggers.
//
// paramTaskID is the task the agent says it is completing, copied out of that
// task's own publishEvent instruction. It is preferred over session lookup
// because it is the only source that names the run rather than reconstructing
// it: the session→task map is mutated as a side effect by createTask, reply and
// updateTaskStatus, so an agent that creates a follow-up task before publishing
// would otherwise be resolved against that new task and lose its workflow.
//
// Falling back to session resolution keeps agents that omit the ID working, but
// that path stays a guess — its last resort is "whichever task is ongoing" —
// so it can return a different run rather than none.
func (ps *WorkspaceServer) currentWorkflowRun(ctx context.Context, req *mcp.CallToolRequest, paramTaskID string) WorkflowRunContext {
	sessID := ""
	if req != nil && req.GetSession() != nil {
		sessID = req.GetSession().ID()
	}
	if sessID == "" && paramTaskID == "" {
		return WorkflowRunContext{}
	}
	taskID, ok := ps.resolveTaskID(ctx, sessID, paramTaskID)
	if !ok {
		return WorkflowRunContext{}
	}
	task, err := ps.getTask(ctx, taskID)
	if err != nil {
		return WorkflowRunContext{}
	}
	return WorkflowRunContext{WorkflowID: task.WorkflowID, Depth: task.WorkflowDepth}
}

func (ps *WorkspaceServer) handleGetWorkspace(ctx context.Context, req *mcp.CallToolRequest, params any) (*mcp.CallToolResult, any, error) {
	ps.emitTelemetry(ctx, ActionMCPToolCall, "getWorkspace", clientIdentityFromRequest(req))
	ps.metadataMu.RLock()
	name := ps.name
	desc := ps.description
	ps.metadataMu.RUnlock()

	tasks, err := ps.listTasks(ctx, ListTasksFilter{})
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

// handleGetTask merges the former getNextTask and getTaskMessages tools. With no
// taskId it dequeues the next "not started" task (like getNextTask); with a taskId
// it fetches that task. When includeConversation is true, the task's chat history
// is appended as a JSON block (paginated via cursor/limit).
func (ps *WorkspaceServer) handleGetTask(ctx context.Context, req *mcp.CallToolRequest, params GetTaskParams) (*mcp.CallToolResult, any, error) {
	ps.emitTelemetry(ctx, ActionMCPToolCall, "getTask", clientIdentityFromRequest(req))

	var task model.Task
	header := "Task details:"

	if params.TaskID == "" {
		// No taskId: behave like the former getNextTask — dequeue the next task.
		t, err := ps.getNextTask(ctx)
		if err != nil {
			if errors.Is(err, base.ErrNotFound) {
				return &mcp.CallToolResult{
					Content: []mcp.Content{&mcp.TextContent{Text: "no pending tasks exist"}},
				}, nil, nil
			}
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("failed to get next task: %v", err)}},
			}, nil, nil
		}
		task = t
		header = "Next assigned task:"

		// Associate session with the task ID for context-aware routing (like permissions)
		if req != nil && req.GetSession() != nil {
			sessID := req.GetSession().ID()
			ps.sessionTasksMu.Lock()
			ps.sessionTasks[sessID] = task.ID
			ps.sessionTasksMu.Unlock()
			zlog.Debug().Str("session_id", sessID).Int64("task_id", task.ID).Msg("Associated session with task in getTask")
		}
	} else {
		id := monoflake.IDFromBase62(params.TaskID)
		if id == 0 {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: "invalid taskId format"}},
			}, nil, nil
		}
		t, err := ps.getTask(ctx, id.Int64())
		if err != nil {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("failed to get task: %v", err)}},
			}, nil, nil
		}
		task = t
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "%s\nID: %s\nTitle: %s", header, monoflake.ID(task.ID).String(), task.Title)
	if task.Status != "" {
		fmt.Fprintf(&sb, "\nStatus: %s", task.Status)
	}
	fmt.Fprintf(&sb, "\nDetails: %s", task.Body)
	if atts := formatModelAttachments(task.Attachments); atts != "" {
		sb.WriteString("\n")
		sb.WriteString(atts)
	}

	if params.IncludeConversation {
		sb.WriteString("\n\nConversation:\n")
		sb.WriteString(buildConversationJSON(task, params.Cursor, params.Limit))
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: sb.String()}},
	}, nil, nil
}

// buildConversationJSON returns the task's chat history as a JSON string of the
// form {"messages":[...],"total":N,"cursor":M}. permission_request messages are
// filtered out (the LLM doesn't need allow/deny history) and attachments include
// metadata only (no base64 data).
func buildConversationJSON(task model.Task, cursor, limit int) string {
	if limit <= 0 {
		limit = 5
	}
	if cursor < 0 {
		cursor = 0
	}

	allMessages := task.Messages
	sort.Slice(allMessages, func(i, j int) bool {
		return allMessages[i].ID < allMessages[j].ID
	})

	messages := make([]model.Message, 0, len(allMessages))
	for _, m := range allMessages {
		if len(m.Metadata) > 0 {
			var meta map[string]any
			if err := json.Unmarshal(m.Metadata, &meta); err == nil {
				if meta["type"] == "permission_request" {
					continue
				}
			}
		}
		messages = append(messages, m)
	}

	total := len(messages)
	start := cursor
	if start > total {
		start = total
	}
	end := start + limit
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
	return string(b)
}

// methodServerDiscover is the SDK's discovery method. The SDK keeps its own
// constant unexported, so the literal is repeated here.
const methodServerDiscover = "server/discover"

// discoverCacheTTL is how long a client may cache a server/discover result.
//
// Everything the result carries — the advertised protocol revisions, the
// capabilities, the tool set and the instructions — is fixed when the server is
// constructed and never mutated afterwards (every AddTool call happens in
// NewWorkspaceServer). The SDK defaults the hint to 0, which its own docs
// define as "immediately stale", so without this a client re-fetches static
// configuration on every connection.
//
// An hour is comfortable precisely because the tool set cannot change while
// the process runs: a client would have to reconnect to a restarted server to
// see a different answer, and reconnecting re-probes anyway.
const discoverCacheTTL = time.Hour

// discoverCacheMiddleware supplies the cache hint the SDK leaves at zero.
// server/discover is a CacheableResult, and answering it is the cheap probe
// clients use before negotiating, so telling them how long the answer is good
// for is the difference between a usable hint and a no-op.
func (ps *WorkspaceServer) discoverCacheMiddleware(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		res, err := next(ctx, method, req)
		if err != nil || method != methodServerDiscover {
			return res, err
		}
		// CacheScope is left as the SDK set it ("public"): the result is
		// identical for every caller, carrying no per-user state.
		if discoverRes, ok := res.(*mcp.DiscoverResult); ok {
			discoverRes.TTLMs = int(discoverCacheTTL.Milliseconds())
		}
		return res, err
	}
}

func (ps *WorkspaceServer) notificationMiddleware(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		zlog.Debug().Str("method", method).Msg("MCP incoming")
		if method == "notifications/claude/channel/permission_request" {
			ps.emitTelemetry(ctx, ActionMCPNotification, "permission_request", clientIdentityFromRequest(req))
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

			// Resolve taskID: first from the payload, then from the session, then from the DB.
			taskID, ok := ps.resolveTaskID(ctx, sessID, p.TaskID)

			if isAutoAllowed {
				zlog.Info().Str("request_id", p.RequestID).Str("tool", p.ToolName).Msg("auto-allowing permission request")
				go func() {
					time.Sleep(100 * time.Millisecond) // Give session time to stabilize if needed
					_ = ps.SendPermissionVerdict(context.Background(), 0, p.RequestID, "allow")
				}()
				ps.emitTelemetry(context.Background(), ActionMCPNotification, "permission_auto_allow", clientIdentityFromRequest(req))
				if ok {
					ps.persistToolCall(ctx, taskID, p, "auto_allowed")
				}
				return nil, nil
			}

			if ok {
				task, err := ps.getTask(ctx, taskID)
				if err == nil && task.AllowAllCommands {
					zlog.Info().Str("request_id", p.RequestID).Int64("task_id", taskID).Msg("auto-allowing permission request (task level)")
					go func() {
						time.Sleep(100 * time.Millisecond) // Give session time to stabilize if needed
						_ = ps.SendPermissionVerdict(context.Background(), taskID, p.RequestID, "allow")
					}()
					ps.emitTelemetry(context.Background(), ActionMCPNotification, "permission_auto_allow", clientIdentityFromRequest(req))
					ps.persistToolCall(ctx, taskID, p, "auto_allowed")
					return nil, nil
				}

				zlog.Info().Str("request_id", p.RequestID).Int64("task_id", taskID).Msg("relaying permission request")
				// Type "permission_request" helps UI render buttons
				metadata := map[string]any{
					"type":          "permission_request",
					"request_id":    p.RequestID,
					"tool_name":     p.ToolName,
					"description":   p.Description,
					"input_preview": p.InputPreview,
					"status":        "pending",
				}
				// Store resolved taskID with the request for later use in SendPermissionVerdict
				ps.requestTaskIDsMu.Lock()
				ps.requestTaskIDs[p.RequestID] = taskID
				ps.requestTaskIDsMu.Unlock()

				if tcID := ps.persistToolCall(ctx, taskID, p, "pending"); tcID != 0 {
					ps.toolCallIDsMu.Lock()
					ps.toolCallIDs[p.RequestID] = tcID
					ps.toolCallIDsMu.Unlock()
				}

				msgID, _ := ps.reply(ctx, monoflake.ID(taskID).String(), fmt.Sprintf("Permission requested for %s: %s", p.ToolName, p.Description), nil, metadata)
				if msgID != 0 {
					ps.permissionResponsesMu.Lock()
					ps.permissionResponses[p.RequestID] = msgID
					ps.permissionResponsesMu.Unlock()
				}
			} else {
				zlog.Warn().Str("request_id", p.RequestID).Str("session_id", sessID).Msg("could not relay permission request: no active task")
			}
			return nil, nil // Notifications must return nil, nil
		}
		return next(ctx, method, req)
	}
}

// resolveTaskID finds which task a permission request/tool call belongs to:
// first the payload's own taskID, then the session->task mapping, then (falling
// back) the workspace's current ongoing/blocked task. Populates the session
// mapping on the DB fallback so subsequent requests on the same session resolve
// without another query.
func (ps *WorkspaceServer) resolveTaskID(ctx context.Context, sessID, payloadTaskID string) (int64, bool) {
	if payloadTaskID != "" {
		if id := monoflake.IDFromBase62(payloadTaskID); id != 0 {
			if _, err := ps.getTask(ctx, id.Int64()); err == nil {
				return id.Int64(), true
			}
		}
	}

	if sessID != "" {
		ps.sessionTasksMu.RLock()
		taskID, ok := ps.sessionTasks[sessID]
		ps.sessionTasksMu.RUnlock()
		if ok {
			return taskID, true
		}
	}

	if tasks, err := ps.listTasks(ctx, ListTasksFilter{Status: []string{"ongoing", "blocked"}, Limit: 1}); err == nil {
		for _, t := range tasks {
			// Only cache under a real session key: a caller with no session
			// would otherwise write an entry that every later sessionless
			// caller reads back as its own task.
			if sessID != "" {
				ps.sessionTasksMu.Lock()
				ps.sessionTasks[sessID] = t.ID
				ps.sessionTasksMu.Unlock()
			}
			zlog.Debug().Str("session_id", sessID).Int64("task_id", t.ID).Msg("Session not found; resolved task from DB")
			return t.ID, true
		}
	}

	return 0, false
}

// toolCallInputPreviewMaxLen caps how much of a tool call's input preview gets
// persisted — tool inputs (e.g. a large file write) can be arbitrarily long,
// and the "tool calls" list only needs enough to identify what happened.
const toolCallInputPreviewMaxLen = 2000

func truncateForStorage(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// persistToolCall records a tool-call permission decision so it shows up in the
// task's "tool calls" list, separate from the message thread. Best-effort: this
// is a secondary audit trail, not the allow/deny decision itself, so failures
// are logged and swallowed. Returns the new row's ID (0 if not recorded).
func (ps *WorkspaceServer) persistToolCall(ctx context.Context, taskID int64, p PermissionRequestParams, status string) int64 {
	if ps.recordToolCall == nil {
		return 0
	}
	tc, err := ps.recordToolCall(ctx, model.ToolCall{
		ID:           ps.idgen.NextID(),
		CreatedAt:    time.Now(),
		TaskID:       taskID,
		WorkspaceID:  ps.workspaceID,
		ToolName:     p.ToolName,
		Description:  p.Description,
		InputPreview: truncateForStorage(p.InputPreview, toolCallInputPreviewMaxLen),
		Status:       status,
	})
	if err != nil {
		zlog.Error().Err(err).Str("request_id", p.RequestID).Str("tool", p.ToolName).Msg("failed to record tool call")
		return 0
	}
	return tc.ID
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

	ps.requestTaskIDsMu.Lock()
	delete(ps.requestTaskIDs, requestID)
	ps.requestTaskIDsMu.Unlock()

	ps.permissionResponsesMu.Lock()
	delete(ps.permissionResponses, requestID)
	ps.permissionResponsesMu.Unlock()

	ps.toolCallIDsMu.Lock()
	delete(ps.toolCallIDs, requestID)
	ps.toolCallIDsMu.Unlock()
}

func (ps *WorkspaceServer) SendPermissionVerdict(ctx context.Context, taskID int64, requestID string, behavior string) error {
	defer ps.cleanupRequest(requestID)

	ps.permissionRequestsMu.RLock()
	sessID, ok := ps.permissionRequests[requestID]
	ps.permissionRequestsMu.RUnlock()

	if !ok {
		return fmt.Errorf("unknown request ID (expired): %s", requestID)
	}

	var okTask bool
	if taskID != 0 {
		// Validate that the supplied taskID belongs to this workspace
		if _, err := ps.getTask(ctx, taskID); err != nil {
			zlog.Warn().Int64("task_id", taskID).Err(err).Msg("supplied taskID not found in workspace, falling back")
			taskID = 0
		} else {
			okTask = true
		}
	}

	// Use the taskID stored when the permission request first came in
	if !okTask {
		ps.requestTaskIDsMu.RLock()
		if storedTaskID, found := ps.requestTaskIDs[requestID]; found && storedTaskID != 0 {
			taskID = storedTaskID
			okTask = true
		}
		ps.requestTaskIDsMu.RUnlock()
	}

	// Fallback to session-based lookup
	if !okTask {
		ps.sessionTasksMu.RLock()
		taskID, okTask = ps.sessionTasks[sessID]
		ps.sessionTasksMu.RUnlock()
	}

	// Fallback: if still not found, query the DB
	// for the first ongoing or blocked task in the workspace.
	if !okTask {
		if tasks, err := ps.listTasks(ctx, ListTasksFilter{Status: []string{"ongoing", "blocked"}, Limit: 1}); err == nil {
			for _, t := range tasks {
				taskID = t.ID
				okTask = true
				zlog.Debug().Int64("task_id", taskID).Str("session_id", sessID).Msg("SendPermissionVerdict: resolved task from DB fallback")
				break
			}
		}
	}

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
			zlog.Info().Msg("auto-allow rule saved")
		}
	}

	// No inbound MCP request is available here (this is a human-triggered verdict, not
	// itself a request handler), so client identity is unknown for these two events.
	switch effectiveBehavior {
	case "allow":
		ps.emitTelemetry(ctx, ActionMCPNotification, "permission_manual_allow", clientIdentity{})
	case "deny":
		ps.emitTelemetry(ctx, ActionMCPNotification, "permission_manual_deny", clientIdentity{})
	}

	if ps.updateToolCallStatus != nil {
		ps.toolCallIDsMu.RLock()
		tcID, hasTC := ps.toolCallIDs[requestID]
		ps.toolCallIDsMu.RUnlock()
		if hasTC {
			tcStatus := "denied"
			if effectiveBehavior == "allow" {
				tcStatus = "allowed"
			}
			if err := ps.updateToolCallStatus(ctx, tcID, tcStatus); err != nil {
				zlog.Error().Err(err).Int64("tool_call_id", tcID).Msg("failed to update tool call status")
			}
		}
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
		zlog.Error().Err(err).Str("session_id", sessionID).Msg("Failed to unmarshal custom MCP notification")
		return
	}

	zlog.Debug().Str("method", msg.Method).Str("session_id", sessionID).Msg("HandleCustomNotification received method")

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

		// Resolve taskID: first from the payload, then from the session, then from the DB.
		taskID, ok := ps.resolveTaskID(ctx, sessionID, p.TaskID)

		if isAutoAllowed {
			zlog.Info().Str("request_id", p.RequestID).Str("tool", p.ToolName).Msg("auto-allowing permission request (via custom notification)")
			go func() {
				time.Sleep(100 * time.Millisecond)
				_ = ps.SendPermissionVerdict(context.Background(), 0, p.RequestID, "allow")
			}()
			// No mcp.Request here (custom out-of-band notification), so client identity is unknown.
			ps.emitTelemetry(context.Background(), ActionMCPNotification, "permission_auto_allow", clientIdentity{})
			if ok {
				ps.persistToolCall(ctx, taskID, p, "auto_allowed")
			}
			return
		}

		zlog.Debug().Str("session_id", sessionID).Int64("task_id", taskID).Bool("found", ok).Msg("Session to task mapping lookup")

		if ok {
			task, err := ps.getTask(ctx, taskID)
			if err == nil && task.AllowAllCommands {
				zlog.Info().Str("request_id", p.RequestID).Int64("task_id", taskID).Str("session_id", sessionID).Msg("auto-allowing permission request (task level, custom notification)")
				go func() {
					time.Sleep(100 * time.Millisecond)
					_ = ps.SendPermissionVerdict(context.Background(), taskID, p.RequestID, "allow")
				}()
				ps.emitTelemetry(context.Background(), ActionMCPNotification, "permission_auto_allow", clientIdentity{})
				ps.persistToolCall(ctx, taskID, p, "auto_allowed")
				return
			}

			zlog.Info().Str("request_id", p.RequestID).Int64("task_id", taskID).Str("session_id", sessionID).Msg("relaying permission request (custom notification)")
			metadata := map[string]any{
				"type":          "permission_request",
				"request_id":    p.RequestID,
				"tool_name":     p.ToolName,
				"description":   p.Description,
				"input_preview": p.InputPreview,
				"status":        "pending",
			}
			// Store resolved taskID with the request for later use in SendPermissionVerdict
			ps.requestTaskIDsMu.Lock()
			ps.requestTaskIDs[p.RequestID] = taskID
			ps.requestTaskIDsMu.Unlock()

			if tcID := ps.persistToolCall(ctx, taskID, p, "pending"); tcID != 0 {
				ps.toolCallIDsMu.Lock()
				ps.toolCallIDs[p.RequestID] = tcID
				ps.toolCallIDsMu.Unlock()
			}

			msgID, _ := ps.reply(ctx, monoflake.ID(taskID).String(), fmt.Sprintf("Permission requested for %s: %s", p.ToolName, p.Description), nil, metadata)
			if msgID != 0 {
				ps.permissionResponsesMu.Lock()
				ps.permissionResponses[p.RequestID] = msgID
				ps.permissionResponsesMu.Unlock()
			}
		} else {
			ps.sessionTasksMu.RLock()
			var currentSessions []string
			for k := range ps.sessionTasks {
				currentSessions = append(currentSessions, k)
			}
			ps.sessionTasksMu.RUnlock()
			zlog.Warn().Str("request_id", p.RequestID).Str("session_id", sessionID).Strs("active_sessions", currentSessions).Msg("could not relay permission request: no active task (custom notification)")
		}
	}
}

func (ps *WorkspaceServer) emitTelemetry(ctx context.Context, action Action, toolOrMethod string, ci clientIdentity) {
	uid := monoflake.IDFromBase62(ps.userID).Int64()
	ps.pubsub.Publish(ctx, pubsub.PublishRequest{
		PubSubID: entity.PubSubTopicMCP,
		Event: MCPEvent{
			Action:        action,
			WorkspaceID:   ps.workspaceID,
			UserID:        uid,
			ToolName:      toolOrMethod,
			Method:        toolOrMethod,
			Actor:         2, // Agent
			ClientID:      ci.hash(),
			ClientName:    ci.name,
			ClientVersion: ci.version,
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
