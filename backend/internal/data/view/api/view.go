package api

import "time"

type (
	// Workspace views

	Workspace struct {
		ID          string `json:"id"`
		CreatedAt   time.Time `json:"created_at"`
		UpdatedAt   time.Time `json:"updated_at"`
		Name        string    `json:"name"`
		Description string    `json:"description"`
		ArchivedAt           *time.Time            `json:"archived_at,omitempty"`
		Icon                 string                `json:"icon,omitempty"`
		NotificationSettings *NotificationSettings `json:"notification_settings,omitempty"`
		AgentConnected       bool                  `json:"agent_connected"`
		MCPURL               string                `json:"mcp_url"`
		MCPToken             string                `json:"mcp_token,omitempty"`
		AutoAllowedTools     []string              `json:"auto_allowed_tools,omitempty"`
	}

	NotificationSettings struct {
		TaskCreated         bool     `json:"task_created"`
		TaskStatusUpdated   bool     `json:"task_status_updated"`
		TaskReceivedMessage bool     `json:"task_received_message"`
		WorkspaceArchived   bool     `json:"workspace_archived"`
		WorkspaceUnarchived bool     `json:"workspace_unarchived"`
		Channels            []string `json:"channels"` // e.g. ["email"]
	}

	CreateWorkspaceRequest struct {
		Workspace Workspace `json:"workspace"`
	}

	CreateWorkspaceResponse struct {
		Workspace Workspace `json:"workspace"`
	}

	UpdateWorkspaceRequest struct {
		Workspace Workspace `json:"workspace"`
	}

	GetWorkspaceResponse struct {
		Workspace Workspace `json:"workspace"`
	}

	ListWorkspacesResponse struct {
		Workspaces []Workspace `json:"workspaces"`
	}

	// Task views

	Attachment struct {
		ID       string `json:"id"`
		Filename string `json:"filename"`
		MimeType string `json:"mimeType"`
		Data     string `json:"data"` // base64
	}

	Message struct {
		ID          string       `json:"id"`
		CreatedAt   time.Time    `json:"created_at"`
		TaskID      string       `json:"task_id"`
		UserID      string       `json:"user_id"`
		Sender      string       `json:"sender"`
		Text        string       `json:"text"`
		Attachments []Attachment `json:"attachments,omitempty"`
		Metadata    any          `json:"metadata,omitempty"`
	}

	// CreatedBy: "human" | "agent"
	// Status:    "notstarted" | "ongoing" | "completed" | "rejected" | "cron" | "blocked"
	Task struct {
		ID        string    `json:"id"`
		CreatedAt time.Time `json:"created_at"`
		UpdatedAt time.Time `json:"updated_at"`

		WorkspaceID   string       `json:"workspace_id"`
		CreatedBy     string       `json:"created_by"`
		Assignee      string       `json:"assignee"`
		Status        string       `json:"status"`
		Title       string       `json:"title"`
		Body        string       `json:"body"`
		Response    string       `json:"response,omitempty"`
		ReplyText   string       `json:"reply_text,omitempty"`
		Attachments []Attachment `json:"attachments,omitempty"`
		Metadata    any          `json:"metadata,omitempty"`
		Messages    []Message    `json:"messages,omitempty"`
		CronSchedule string      `json:"cron_schedule,omitempty"`
		ParentID     string      `json:"parent_id,omitempty"`
		SortOrder    float64     `json:"sort_order"`
	}

	CreateTaskRequest struct {
		Task Task `json:"task"`
	}

	CreateTaskResponse struct {
		Task Task `json:"task"`
	}

	GetTaskResponse struct {
		Task Task `json:"task"`
	}

	ListTasksResponse struct {
		Tasks []Task `json:"tasks"`
	}

	RespondToTaskRequest struct {
		Response TaskResponse `json:"response"`
	}

	TaskResponse struct {
		Action      string       `json:"action"` // "allow" | "reject" | "allow_all" | "text"
		Text        string       `json:"text,omitempty"`
		Attachments []Attachment `json:"attachments,omitempty"`
		Metadata    any          `json:"metadata,omitempty"`
	}

	RespondToTaskResponse struct {
		Task Task `json:"task"`
	}

	UpdateTaskStatusRequest struct {
		Status TaskStatusUpdate `json:"status"`
	}

	TaskStatusUpdate struct {
		Value string `json:"value"` // "notstarted" | "ongoing" | "completed"
	}

	UpdateTaskStatusResponse struct {
		Task Task `json:"task"`
	}

	UpdateTaskOrderRequest struct {
		Order TaskOrderUpdate `json:"order"`
	}

	TaskOrderUpdate struct {
		Value float64 `json:"value"`
	}

	UpdateTaskOrderResponse struct {
		Task Task `json:"task"`
	}

	ReplyToTaskRequest struct {
		Reply TaskReply `json:"reply"`
	}

	TaskReply struct {
		Text        string       `json:"text"`
		Attachments []Attachment `json:"attachments,omitempty"`
		Metadata    any          `json:"metadata,omitempty"`
	}

	ReplyToTaskResponse struct {
		Task Task `json:"task"`
	}

	SendPermissionVerdictRequest struct {
		RequestID string `json:"request_id"`
		Behavior  string `json:"behavior"` // "allow" | "deny"
	}

	UpdateScheduledTaskRequest struct {
		Task Task `json:"task"`
	}

	UpdateScheduledTaskResponse struct {
		Task Task `json:"task"`
	}
)
