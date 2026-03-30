package crud

import "time"

type (
	// Actor enum for mapping human and agent actions
	Actor int

	// ResourceType enum for mapping resources
	ResourceType int

	// Action enum for mapping standard CRUD and system actions
	Action int

	// Workspace entity
	Workspace struct {
		ID          int64
		CreatedAt   time.Time
		UpdatedAt   time.Time
		UserID      int64
		Name        string
		Description string
		ArchivedAt           *time.Time
		Icon                 string
		NotificationSettings *NotificationSettings
		AgentConnected       bool
		TokenEncrypted       string
		TokenNonce           string
		AutoAllowedTools     []string
	}

	NotificationSettings struct {
		TaskCreated         bool
		TaskStatusUpdated   bool
		TaskReceivedMessage bool
		WorkspaceArchived   bool
		WorkspaceUnarchived bool
		Channels            []string
	}

	CreateWorkspaceRequest struct {
		Workspace Workspace
		UserID  string
	}

	CreateWorkspaceResponse struct {
		Workspace Workspace
	}

	GetWorkspaceRequest struct {
		ID     int64
		UserID string
	}

	GetWorkspaceResponse struct {
		Workspace Workspace
	}

	ListWorkspacesRequest struct {
		UserID          string
		IncludeArchived bool
	}

	ListWorkspacesResponse struct {
		Workspaces []Workspace
	}

	DeleteWorkspaceRequest struct {
		ID     int64
		UserID string
	}

	ArchiveWorkspaceRequest struct {
		ID     int64
		UserID string
	}

	UnarchiveWorkspaceRequest struct {
		ID     int64
		UserID string
	}

	UpdateWorkspaceRequest struct {
		Workspace Workspace
		UserID    string
	}

	UpdateWorkspaceResponse struct {
		Workspace Workspace
	}

	UpdateWorkspaceAutoAllowedToolsRequest struct {
		WorkspaceID int64
		Tools       []string
		UserID      string
	}

	Attachment struct {
		ID       string `json:"id"`
		Filename string `json:"filename"`
		MimeType string `json:"mimeType"`
		Data     string `json:"data"` // base64
	}

	Message struct {
		ID          int64
		CreatedAt   time.Time
		TaskID      int64
		UserID      int64
		Sender      string
		Text        string
		Attachments []Attachment
		Metadata    any
	}

	// Task entity
	// CreatedBy: "human" | "agent"
	// Status:    "pending" | "completed" | "rejected"
	Task struct {
		ID          int64
		CreatedAt   time.Time
		UpdatedAt   time.Time
		UserID      int64
		WorkspaceID int64
		CreatedBy   string
		Assignee    string
		Status      string
		Title       string
		Body        string
		Response    string
		ReplyText   string
		Attachments []Attachment
		Messages    []Message
		CronSchedule string
		ParentID     int64
		SortOrder    float64
	}

	CreateTaskRequest struct {
		Task   Task
		UserID string
	}

	CreateTaskResponse struct {
		Task Task
	}

	GetTaskRequest struct {
		WorkspaceID int64
		TaskID      int64
		UserID      string
	}

	GetTaskResponse struct {
		Task Task
	}

	ListTasksRequest struct {
		WorkspaceID int64
		CreatedBy   string   // optional filter
		Status      []string // optional filter
		Filter      string   // e.g. "pending_approval"
		Limit       int
		Offset      int
		UserID      string
	}

	ListTasksResponse struct {
		Tasks []Task
	}

	RespondToTaskRequest struct {
		WorkspaceID int64
		TaskID      int64
		Action      string // "allow" | "reject" | "allow_all" | "text"
		Text        string // optional for "text" action
		Attachments []Attachment
		UserID      string
	}

	RespondToTaskResponse struct {
		Task Task
	}

	UpdateTaskStatusRequest struct {
		WorkspaceID int64
		TaskID      int64
		Status      string
		UserID      string
	}

	UpdateTaskStatusResponse struct {
		Task Task
	}

	UpdateTaskOrderRequest struct {
		WorkspaceID int64
		TaskID      int64
		SortOrder   float64
		UserID      string
	}

	UpdateTaskOrderResponse struct {
		Task Task
	}

	ReplyToTaskRequest struct {
		WorkspaceID int64
		TaskID      int64
		Text        string
		Attachments []Attachment
		UserID      string
	}

	ReplyToTaskResponse struct {
		Task Task
	}

	DeleteTaskRequest struct {
		WorkspaceID int64
		TaskID      int64
		UserID      string
	}

	DeleteTaskResponse struct{}

	UpdateMessageMetadataRequest struct {
		WorkspaceID int64
		TaskID      int64
		MessageID   int64
		Metadata    any
		UserID      string
	}

	GetAttachmentRequest struct {
		WorkspaceID  int64
		AttachmentID string
		UserID       string
	}

	GetAttachmentResponse struct {
		Data     []byte
		Filename string
		MimeType string
	}

	UpdateScheduledTaskRequest struct {
		WorkspaceID  int64
		TaskID       int64
		Title        string
		Body         string
		Assignee     string
		CronSchedule string
		UserID       string
	}

	UpdateScheduledTaskResponse struct {
		Task Task
	}

	DailyStat struct {
		Date  string `json:"date"`
		Count int64  `json:"count"`
	}

	GetWorkspaceStatsRequest struct {
		ID     int64  `json:"id"`
		UserID string `json:"user_id"`
		Range  string `json:"range"` // 1d, 7d, week, 30d, month, custom
		From   int64  `json:"from"`  // unix timestamp for custom range
		To     int64  `json:"to"`    // unix timestamp for custom range
	}

	GetDetailedWorkspaceStatsResponse struct {
		Summary    WorkspaceStatsSummary    `json:"summary"`
		Timeseries WorkspaceStatsTimeseries `json:"timeseries"`
	}

	WorkspaceStatsSummary struct {
		TasksCompleted  int64 `json:"tasks_completed"`
		TasksScheduled  int64 `json:"tasks_scheduled"`
		Messages        int64 `json:"messages"`
		ManualApprovals int64 `json:"manual_approvals"`
		AutoApprovals   int64 `json:"auto_approvals"`
		Denies          int64 `json:"denies"`
	}

	WorkspaceStatsTimeseries struct {
		TasksCompleted []DailyStat `json:"tasks_completed"`
		Messages       []DailyStat `json:"messages"`
	}

	User struct {
		ID        int64
		CreatedAt time.Time
		UpdatedAt time.Time
		Email     string
		Name      string
		Picture   string
	}

	FindOrCreateUserRequest struct {
		Email   string
		Name    string
		Picture string
	}

	FindOrCreateUserResponse struct {
		User User
	}

	// CRUDEvent is the central structure for all CRUD events published via PubSub
	CRUDEvent struct {
		Action       Action       `json:"action"`
		WorkspaceID  int64        `json:"workspace_id"`
		UserID       int64        `json:"user_id"`
		ResourceType ResourceType `json:"resource_type"`
		ResourceID   int64        `json:"resource_id"`
		Actor        Actor        `json:"actor"` // 1: Human, 2: Agent
	}
)

const (
	PubSubTopicCRUD int64 = 1
	PubSubTopicMCP  int64 = 2
)

const (
	ActorHuman Actor = 1
	ActorAgent Actor = 2
)

const (
	ResourceUser ResourceType = iota + 1
	ResourceWorkspace
	ResourceTask
	ResourceMessage
)

func (r ResourceType) String() string {
	switch r {
	case ResourceUser:
		return "user"
	case ResourceWorkspace:
		return "workspace"
	case ResourceTask:
		return "task"
	case ResourceMessage:
		return "message"
	}
	return "unknown"
}

const (
	ActionUserCreate Action = iota + 1
	ActionUserUpdate
	ActionUserDelete
	ActionWorkspaceCreate
	ActionWorkspaceUpdate
	ActionWorkspaceDelete
	ActionTaskCreate
	ActionTaskUpdate
	ActionTaskDelete
	ActionMessageCreate
	ActionMessageUpdate
	ActionMessageDelete
	ActionTaskComplete        Action = 13
	ActionTaskApproveManual    Action = 14
	ActionTaskFromScheduled   Action = 15
	ActionTaskRejectManual    Action = 16
	ActionMCPToolCall         Action = 20
	ActionMCPPermissionManual Action = 21
	ActionMCPPermissionAuto   Action = 22
)

func (a Action) String() string {
	switch a {
	case ActionUserCreate:
		return "user_create"
	case ActionUserUpdate:
		return "user_update"
	case ActionUserDelete:
		return "user_delete"
	case ActionWorkspaceCreate:
		return "workspace_create"
	case ActionWorkspaceUpdate:
		return "workspace_update"
	case ActionWorkspaceDelete:
		return "workspace_delete"
	case ActionTaskCreate:
		return "task_create"
	case ActionTaskUpdate:
		return "task_update"
	case ActionTaskDelete:
		return "task_delete"
	case ActionMessageCreate:
		return "message_create"
	case ActionMessageUpdate:
		return "message_update"
	case ActionMessageDelete:
		return "message_delete"
	case ActionTaskComplete:
		return "task_complete"
	case ActionTaskApproveManual:
		return "task_approve_manual"
	case ActionTaskFromScheduled:
		return "task_from_scheduled"
	case ActionMCPToolCall:
		return "mcp_tool_call"
	case ActionMCPPermissionManual:
		return "mcp_permission_manual"
	case ActionMCPPermissionAuto:
		return "mcp_permission_auto"
	}
	return "unknown"
}
