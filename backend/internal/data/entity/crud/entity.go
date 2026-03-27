package crud

import "time"

type (
	// Workspace entity
	Workspace struct {
		ID          int64
		CreatedAt   time.Time
		UpdatedAt   time.Time
		UserID      string
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
		UserID      string
		Sender      string
		Text        string
		Attachments []Attachment
		Metadata    any
	}

	// Task entity
	// CreatedBy: "human" | "agent"
	// Status:    "pending" | "done" | "rejected"
	Task struct {
		ID        int64
		CreatedAt time.Time
		UpdatedAt time.Time

		UserID      string
		WorkspaceID   int64
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
		TaskID    int64
		UserID    string
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
		WorkspaceID   int64
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
		TaskID    int64
		Status    string
		UserID    string
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
		TaskID    int64
		Text        string
		Attachments []Attachment
		UserID      string
	}

	ReplyToTaskResponse struct {
		Task Task
	}

	DeleteTaskRequest struct {
		WorkspaceID int64
		TaskID    int64
		UserID    string
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

	GetWorkspaceStatsResponse struct {
		Stats       []DailyStat `json:"stats"`
		Total       int64       `json:"total"`
		ActiveTasks int64       `json:"active_tasks"`
		TotalTasks  int64       `json:"total_tasks"`
	}

	User struct {
		ID         int64
		CreatedAt  time.Time
		UpdatedAt  time.Time
		Email      string
		ExternalID string
		Name       string
		Picture    string
	}

	FindOrCreateUserRequest struct {
		Email      string
		ExternalID string
		Name       string
		Picture    string
	}

	FindOrCreateUserResponse struct {
		User User
	}
)
