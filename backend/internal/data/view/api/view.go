package api

import "time"

type (
	// Workspace views

	Workspace struct {
		ID          string `json:"id"`
		CreatedAt   time.Time `json:"createdAt"`
		UpdatedAt   time.Time `json:"updatedAt"`
		Name        string    `json:"name"`
		Description string    `json:"description"`
		ArchivedAt           *time.Time            `json:"archivedAt,omitempty"`
		Icon                 string                `json:"icon,omitempty"`
		NotificationSettings *NotificationSettings `json:"notificationSettings,omitempty"`
		AgentConnected       bool                  `json:"agentConnected"`
		MCPURL               string                `json:"mcpUrl"`
		MCPToken             string                `json:"mcpToken,omitempty"`
		AutoAllowedTools     []string              `json:"autoAllowedTools,omitempty"`
		AllowAllCommands     bool                  `json:"allowAllCommands"`
		SelfLearningLoopNote string                `json:"selfLearningLoopNote,omitempty"`
	}

	NotificationSettings struct {
		TaskCreated         bool     `json:"taskCreated"`
		TaskStatusUpdated   bool     `json:"taskStatusUpdated"`
		TaskReceivedMessage bool     `json:"taskReceivedMessage"`
		WorkspaceArchived   bool     `json:"workspaceArchived"`
		WorkspaceUnarchived bool     `json:"workspaceUnarchived"`
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
		CreatedAt   time.Time    `json:"createdAt"`
		TaskID      string       `json:"taskId"`
		UserID      string       `json:"userId"`
		Sender      string       `json:"sender"`
		Text        string       `json:"text"`
		Attachments []Attachment `json:"attachments,omitempty"`
		Metadata    any          `json:"metadata,omitempty"`
	}

	// CreatedBy: "human" | "agent"
	// Status:    "notstarted" | "ongoing" | "completed" | "rejected" | "cron" | "blocked"
	Task struct {
		ID        string    `json:"id"`
		CreatedAt time.Time `json:"createdAt"`
		UpdatedAt time.Time `json:"updatedAt"`

		WorkspaceID   string       `json:"workspaceId"`
		CreatedBy     string       `json:"createdBy"`
		Assignee      string       `json:"assignee"`
		Status        string       `json:"status"`
		Title       string       `json:"title"`
		Body        string       `json:"body"`
		Response    string       `json:"response,omitempty"`
		ReplyText   string       `json:"replyText,omitempty"`
		Attachments []Attachment `json:"attachments,omitempty"`
		Metadata    any          `json:"metadata,omitempty"`
		Messages    []Message    `json:"messages,omitempty"`
		CronSchedule string      `json:"cronSchedule,omitempty"`
		ParentID     string      `json:"parentId,omitempty"`
		SortOrder    float64     `json:"sortOrder"`
		AllowAllCommands bool    `json:"allowAllCommands"`
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

	UpdateTaskAllowAllCommandsRequest struct {
		AllowAll TaskAllowAllUpdate `json:"allowAll"`
	}

	TaskAllowAllUpdate struct {
		Value bool `json:"value"`
	}

	UpdateTaskAllowAllCommandsResponse struct {
		Task Task `json:"task"`
	}

	UpdateTaskOrderResponse struct {
		Task Task `json:"task"`
	}

	UpdateTaskAssigneeRequest struct {
		Assignee TaskAssigneeUpdate `json:"assignee"`
	}

	TaskAssigneeUpdate struct {
		Value string `json:"value"` // "human" | "agent"
	}

	UpdateTaskAssigneeResponse struct {
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
		RequestID string `json:"requestId"`
		Behavior  string `json:"behavior"` // "allow" | "deny"
	}

	UpdateScheduledTaskRequest struct {
		Task Task `json:"task"`
	}

	UpdateScheduledTaskResponse struct {
		Task Task `json:"task"`
	}
)
