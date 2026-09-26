// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package crud

import (
	"context"
	"time"
)

type (
	// Actor enum for mapping human and agent actions
	Actor int

	// ResourceType enum for mapping resources
	ResourceType int

	// Action enum for mapping standard CRUD and system actions
	Action int

	// Origin enum for mapping event origins
	Origin int

	// Workspace entity
	Workspace struct {
		ID                    int64
		CreatedAt             time.Time
		UpdatedAt             time.Time
		UserID                int64
		Name                  string
		Description           string
		ArchivedAt            *time.Time
		Icon                  string
		NotificationSettings  *NotificationSettings
		AgentConnected        bool
		AgentSupportsStop     bool
		AgentModels           *AgentModels
		AgentCommands         *AgentCommands
		AgentClient           *AgentClient
		AgentConcurrency      *AgentConcurrency
		AutoAllowedTools      []string
		AllowAllCommands      bool
		ClearContextDefault   bool
		SelfLearningLoopNote  string
		InputSendDelaySeconds int
		WorkingDirectory      string
		Slack                 *SlackConfig
	}

	// SlackConfig holds the Slack channel linked to a workspace.
	// AgentModels is what the workspace's connected agent says it can switch
	// between. Live state read off the MCP session rather than a stored column,
	// so it is absent whenever no agent is connected or none has reported any.
	AgentModels struct {
		// ConfigID names the session config option the agent advertised these
		// under, which is what a selection has to be written back to.
		ConfigID     string
		CurrentModel string
		Models       []AgentModel
		// CanSet reports whether the connected agent will act on one of these
		// being chosen. False for every gateway too old to say so, which is
		// what keeps a picker from being offered where it would do nothing.
		CanSet bool
	}

	AgentModel struct {
		ID          string
		Name        string
		Description string
		Current     bool
		Group       string
	}

	// AgentCommands is the set of slash commands the workspace's connected
	// agent offers. Live state read off the MCP session rather than a stored
	// column, so it is absent whenever no agent is connected or none has
	// reported any.
	AgentCommands struct {
		Commands []AgentCommand
	}

	AgentCommand struct {
		Name        string
		Description string
		Hint        string
	}

	// AgentClient is what the client attached to the workspace said it was.
	// Live state read off the MCP session, so it is absent whenever nothing is
	// connected.
	AgentClient struct {
		Name    string
		Title   string
		Version string
	}

	// AgentConcurrency is how many tasks the workspace's connected gateway
	// will run at once, and what its queue is doing right now. Live state read
	// off the MCP session rather than a stored column — the limit itself lives
	// in the gateway's memory and does not survive its restart — so it is
	// absent whenever nothing is connected or nothing has reported one.
	AgentConcurrency struct {
		// MaxConcurrency is the limit in force.
		MaxConcurrency int
		// Active may exceed MaxConcurrency for a while after a lower: running
		// tasks are never interrupted, the queue simply stops handing out new
		// ones until the count falls back under the limit.
		Active int
		Queued int
		// Min and Max bound what may be asked for, as the gateway reported it.
		// Max is 0 when it named no ceiling.
		Min int
		Max int
		// CanSet reports whether the connected gateway will act on being told a
		// new limit. False for every gateway too old to say so, which is what
		// keeps a control from being offered where it would do nothing.
		CanSet bool
	}

	SlackConfig struct {
		Enabled     bool   `json:"enabled"`
		Installed   bool   `json:"installed"`
		ChannelID   string `json:"channelId,omitempty"`
		ChannelName string `json:"channelName,omitempty"`
		AutoCreated bool   `json:"autoCreated,omitempty"`
		ClientID    string `json:"clientId,omitempty"`
		AuthURL     string `json:"authUrl,omitempty"`
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
		UserID    string
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

	// SetWorkspaceSlackChannelRequest assigns a Slack channel to a workspace.
	SetWorkspaceSlackChannelRequest struct {
		WorkspaceID int64
		ChannelID   string
		ChannelName string
		AutoCreated bool
		UserID      string
	}

	// RemoveWorkspaceSlackChannelRequest removes the Slack channel assignment from a workspace.
	RemoveWorkspaceSlackChannelRequest struct {
		WorkspaceID int64
		UserID      string
	}

	Attachment struct {
		ID       string `json:"id"`
		Filename string `json:"filename"`
		MimeType string `json:"mimeType"`
		Data     string `json:"data"` // base64
		// URL is where anyone can read the file, set when it is kept in S3.
		URL string `json:"url,omitempty"`
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

	// ToolCall entity
	// Status: "auto_allowed" | "pending" | "allowed" | "denied"
	ToolCall struct {
		ID           int64
		CreatedAt    time.Time
		TaskID       int64
		WorkspaceID  int64
		ToolName     string
		Description  string
		InputPreview string
		Status       string
	}

	// Task entity
	// CreatedBy: "human" | "agent"
	// Status:    "notstarted" | "ongoing" | "completed" | "rejected" | "cron" | "blocked"
	Task struct {
		ID                    int64
		CreatedAt             time.Time
		UpdatedAt             time.Time
		UserID                int64
		WorkspaceID           int64
		CreatedBy             string
		Assignee              string
		Status                string
		Title                 string
		Body                  string
		Response              string
		ReplyText             string
		Attachments           []Attachment
		Messages              []Message
		ToolCalls             []ToolCall
		CronSchedule          string
		ParentID              int64
		SortOrder             float64
		AllowAllCommands      bool
		ClearContext          bool
		EventID               int64
		WorkflowID            int64
		WorkflowDepth         int
		CompletionTriggerType int16
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
		WorkspaceID     int64
		CreatedBy       string   // optional filter
		Assignee        string   // optional filter, e.g. "agent" | "human"
		Status          []string // optional filter
		Filter          string   // e.g. "pending_approval"
		Limit           int
		Offset          int
		UserID          string
		PreloadMessages bool
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

	// ForkTaskRequest copies a task's conversation, from its first message up
	// to and including MessageID, into a new task. Status is the new task's:
	// the caller picks it, because only the caller knows whether the agent has
	// room to take it on now.
	ForkTaskRequest struct {
		WorkspaceID int64
		TaskID      int64
		MessageID   int64
		Status      string // "ongoing" | "notstarted"
		UserID      string
	}

	ForkTaskResponse struct {
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

	UpdateTaskAssigneeRequest struct {
		WorkspaceID int64
		TaskID      int64
		Assignee    string
		UserID      string
	}

	UpdateTaskAssigneeResponse struct {
		Task Task
	}

	MoveTaskRequest struct {
		WorkspaceID            int64
		TaskID                 int64
		DestinationWorkspaceID int64
		UserID                 string
	}

	MoveTaskResponse struct {
		Task Task
	}

	UpdateTaskAllowAllCommandsRequest struct {
		WorkspaceID      int64
		TaskID           int64
		AllowAllCommands bool
		UserID           string
	}

	UpdateTaskAllowAllCommandsResponse struct {
		Task Task
	}

	ReplyToTaskRequest struct {
		WorkspaceID int64
		TaskID      int64
		Text        string
		Attachments []Attachment
		UserID      string
		SlackUser   string
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
		TaskID       int64
		AttachmentID string
		UserID       string
	}

	GetAttachmentResponse struct {
		Data     []byte
		Filename string
		MimeType string
	}

	UpdateScheduledTaskRequest struct {
		WorkspaceID      int64
		TaskID           int64
		Title            string
		Body             string
		Assignee         string
		CronSchedule     string
		AllowAllCommands bool
		ClearContext     bool
		UserID           string
	}

	UpdateScheduledTaskResponse struct {
		Task Task
	}

	DailyStat struct {
		Date  string `json:"date"`
		Count int64  `json:"count"`
	}

	GlobalTaskStatsResponse struct {
		PendingTasks   int64 `json:"pendingTasks"`
		ScheduledTasks int64 `json:"scheduledTasks"`
	}

	// TelemetryCount is one grouped row out of a telemetry rollup source
	// query — never serialized over the API, so it carries no json tags.
	// See internal/service/telemetryaggregator.
	TelemetryCount struct {
		UserID      int64
		WorkspaceID int64
		Action      uint8
		SubActionID uint8
		Actor       uint8
		Count       int64
	}

	GetWorkspaceTaskCountsRequest struct {
		WorkspaceID int64
		UserID      string
	}

	GetWorkspaceStatsRequest struct {
		ID     int64  `json:"id"`
		UserID string `json:"userId"`
		Range  string `json:"range"` // 1d, 7d, week, 30d, month, custom
		From   int64  `json:"from"`  // unix timestamp for custom range
		To     int64  `json:"to"`    // unix timestamp for custom range
	}

	GetDetailedWorkspaceStatsResponse struct {
		Summary    WorkspaceStatsSummary    `json:"summary"`
		Timeseries WorkspaceStatsTimeseries `json:"timeseries"`
		Heatmap    WorkspaceStatsHeatmap    `json:"heatmap"`
	}

	WorkspaceStatsSummary struct {
		TasksCompleted  int64 `json:"tasksCompleted"`
		TasksScheduled  int64 `json:"tasksScheduled"`
		Messages        int64 `json:"messages"`
		ManualApprovals int64 `json:"manualApprovals"`
		AutoApprovals   int64 `json:"autoApprovals"`
		Denies          int64 `json:"denies"`
	}

	WorkspaceStatsTimeseries struct {
		TasksCompleted []DailyStat `json:"tasksCompleted"`
		Messages       []DailyStat `json:"messages"`
	}

	HeatmapStat struct {
		Bucket string `json:"bucket"`
		Count  int64  `json:"count"`
	}

	WorkspaceStatsHeatmap struct {
		Granularity    string        `json:"granularity"` // "hour" or "day"
		RangeStart     int64         `json:"rangeStart"`  // unix timestamp
		RangeEnd       int64         `json:"rangeEnd"`    // unix timestamp
		TasksCompleted []HeatmapStat `json:"tasksCompleted"`
		Messages       []HeatmapStat `json:"messages"`
	}

	// GetUserStatsRequest asks for the same statistics as
	// GetWorkspaceStatsRequest, but summed over every workspace the user owns.
	// There is no ID to pass: the scope *is* the caller, taken from the
	// session, which is what makes the endpoint safe without an ownership
	// check of its own.
	GetUserStatsRequest struct {
		UserID string `json:"userId"`
		Range  string `json:"range"` // 1d, 7d, week, 30d, month, custom
		From   int64  `json:"from"`  // unix timestamp for custom range
		To     int64  `json:"to"`    // unix timestamp for custom range
	}

	// GetDetailedUserStatsResponse embeds the per-workspace response shape
	// verbatim so the account dashboard can render the very same components,
	// and adds the breakdown that only makes sense once several workspaces are
	// in view.
	GetDetailedUserStatsResponse struct {
		Summary    WorkspaceStatsSummary     `json:"summary"`
		Timeseries WorkspaceStatsTimeseries  `json:"timeseries"`
		Heatmap    WorkspaceStatsHeatmap     `json:"heatmap"`
		Workspaces []WorkspaceStatsBreakdown `json:"workspaces"`
	}

	// GetDetailedUserStatsRows is what the repository returns: the same
	// aggregates, but with the breakdown still keyed by raw int64 ID. The
	// repository deals in int64 throughout and does not know about base62, so
	// the controller is what turns this into GetDetailedUserStatsResponse.
	GetDetailedUserStatsRows struct {
		Summary    WorkspaceStatsSummary
		Timeseries WorkspaceStatsTimeseries
		Heatmap    WorkspaceStatsHeatmap
		Workspaces []WorkspaceStatsBreakdownRow
	}

	// WorkspaceStatsBreakdownRow is the repository's shape for a breakdown
	// entry, keyed by the raw ID.
	WorkspaceStatsBreakdownRow struct {
		WorkspaceID    int64
		Name           string
		TasksCompleted int64
		Messages       int64
	}

	// WorkspaceStatsBreakdown is one workspace's contribution to the account
	// totals over the same window.
	//
	// Name is resolved from the workspaces table rather than stored on the
	// telemetry row, so a renamed workspace reads correctly in history. A
	// workspace the user has since deleted keeps its rows and surfaces with an
	// empty name; the frontend labels that case rather than dropping the count,
	// which would make the breakdown fail to add up to the total.
	WorkspaceStatsBreakdown struct {
		WorkspaceID    string `json:"workspaceId"`
		Name           string `json:"name"`
		TasksCompleted int64  `json:"tasksCompleted"`
		Messages       int64  `json:"messages"`
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
		WorkspaceID  int64        `json:"workspaceId"`
		UserID       int64        `json:"userId"`
		ResourceType ResourceType `json:"resourceType"`
		ResourceID   int64        `json:"resourceId"`
		Actor        Actor        `json:"actor"`  // 1: Human, 2: Agent, 3: Extension
		Origin       Origin       `json:"origin"` // 1: API, 2: MCP, 3: Scheduler, 4: Slack
	}

	// Push subscription entities

	SavePushSubscriptionRequest struct {
		UserID      int64
		WorkspaceID int64
		Endpoint    string
		P256dh      string
		Auth        string
		UserAgent   string
		Types       []string // empty = all types
	}

	// RecordTelemetryRequest reports one client-side occurrence of Action.
	// Action is already resolved from the allowlist by the time it gets here,
	// so the controller never sees a name the client invented.
	RecordTelemetryRequest struct {
		Action      Action
		WorkspaceID int64
		UserID      string
	}

	DeletePushSubscriptionRequest struct {
		UserID   int64
		Endpoint string
	}

	CheckPushSubscriptionRequest struct {
		UserID      int64
		WorkspaceID int64
		Endpoint    string
	}

	DeletePushSubscriptionByWorkspaceRequest struct {
		UserID      int64
		WorkspaceID int64
		Endpoint    string
	}

	// Event entities

	EventFAQ struct {
		Q string
		A string
	}

	// Memory is one of a workspace's remembered notes. Size travels with the
	// list so a caller can show what is there without fetching every memory in
	// full; Content is only populated when one was asked for by name.
	Memory struct {
		ID          int64
		CreatedAt   time.Time
		UpdatedAt   time.Time
		WorkspaceID int64
		Name        string
		Content     string
		SizeBytes   int
	}

	ListMemoriesRequest struct {
		WorkspaceID int64
		UserID      string
	}

	ListMemoriesResponse struct {
		Memories []Memory
	}

	GetMemoryRequest struct {
		WorkspaceID int64
		UserID      string
		Name        string
	}

	GetMemoryResponse struct {
		Memory Memory
	}

	// Skill is one of the skills a workspace can use: its own, or one shared
	// into it, in which case SharedFromWorkspaceID names the owner and the
	// skill is read-only here. Files is only filled when one skill was asked
	// for, and then without content.
	Skill struct {
		ID                    int64
		CreatedAt             time.Time
		UpdatedAt             time.Time
		WorkspaceID           int64
		Name                  string
		Description           string
		SourceType            string
		SourceRepo            string
		SourceRef             string
		SourceCommit          string
		SourcePath            string
		LocallyModified       bool
		FileCount             int
		TotalBytes            int
		SharedFromWorkspaceID int64
		Files                 []SkillFile
	}

	SkillFile struct {
		Path      string
		SizeBytes int
		UpdatedAt time.Time
		Content   string
	}

	SkillShare struct {
		TargetWorkspaceID int64
		CreatedAt         time.Time
	}

	SkillImportSkip struct {
		Name   string
		Path   string
		Reason string
	}

	// SearchSkillsRequest finds skills by name or description. An empty Query
	// matches every skill, and a Limit of 0 returns every match.
	SearchSkillsRequest struct {
		WorkspaceID int64
		UserID      string
		Query       string
		Limit       int
		Offset      int
	}

	SearchSkillsResponse struct {
		Skills []Skill
		Total  int
	}

	GetSkillRequest struct {
		WorkspaceID int64
		UserID      string
		Name        string
	}

	GetSkillResponse struct {
		Skill Skill
	}

	GetSkillFileRequest struct {
		WorkspaceID int64
		UserID      string
		Name        string
		Path        string
	}

	GetSkillFileResponse struct {
		Skill Skill
		File  SkillFile
	}

	SaveSkillFileRequest struct {
		WorkspaceID int64
		UserID      string
		Name        string
		Path        string
		Content     string
	}

	SaveSkillFileResponse struct {
		Skill Skill
		File  SkillFile
	}

	DeleteSkillRequest struct {
		WorkspaceID int64
		UserID      string
		Name        string
	}

	DeleteSkillFileRequest struct {
		WorkspaceID int64
		UserID      string
		Name        string
		Path        string
	}

	DeleteSkillFileResponse struct {
		Skill Skill
	}

	// ImportSkillsRequest imports every skill the URL names, or only those
	// whose directories Skills lists.
	ImportSkillsRequest struct {
		WorkspaceID int64
		UserID      string
		URL         string
		Overwrite   bool
		Skills      []string
	}

	// SkillImportCandidate is a skill a repository too large to import whole
	// offers, for the import to be asked again naming the ones wanted.
	SkillImportCandidate struct {
		Name       string
		Path       string
		SkillBytes int64
		Reason     string
	}

	ImportSkillsResponse struct {
		Imported     []Skill
		Skipped      []SkillImportSkip
		Candidates   []SkillImportCandidate
		SourceRepo   string
		SourceRef    string
		SourceCommit string
	}

	ListSkillSharesRequest struct {
		WorkspaceID int64
		UserID      string
		Name        string
	}

	ListSkillSharesResponse struct {
		Shares []SkillShare
	}

	ShareSkillRequest struct {
		WorkspaceID       int64
		UserID            string
		Name              string
		TargetWorkspaceID int64
	}

	Event struct {
		ID                int64
		CreatedAt         time.Time
		UpdatedAt         time.Time
		UserID            int64
		Name              string
		PayloadGuidelines string
	}

	CreateEventRequest struct {
		Name              string
		PayloadGuidelines string
		UserID            string
	}

	CreateEventResponse struct {
		Event Event
	}

	GetEventRequest struct {
		ID     int64
		UserID string
	}

	GetEventResponse struct {
		Event Event
	}

	ListEventsRequest struct {
		UserID string
	}

	ListEventsResponse struct {
		Events []Event
	}

	UpdateEventRequest struct {
		ID                int64
		UserID            string
		PayloadGuidelines string
	}

	UpdateEventResponse struct {
		Event Event
	}

	DeleteEventRequest struct {
		ID     int64
		UserID string
	}

	// EventTrigger entities

	EventTrigger struct {
		ID               int64
		CreatedAt        time.Time
		UpdatedAt        time.Time
		EventID          int64
		WorkspaceID      int64
		UserID           int64
		Title            string
		Body             string
		Assignee         string
		CronSchedule     string
		AllowAllCommands bool
		EmitEventID      int64
	}

	CreateEventTriggerRequest struct {
		EventID          int64
		WorkspaceID      int64
		Title            string
		Body             string
		Assignee         string
		CronSchedule     string
		AllowAllCommands bool
		EmitEventID      int64
		UserID           string
	}

	CreateEventTriggerResponse struct {
		EventTrigger EventTrigger
	}

	GetEventTriggerRequest struct {
		ID     int64
		UserID string
	}

	GetEventTriggerResponse struct {
		EventTrigger EventTrigger
	}

	ListEventTriggersRequest struct {
		EventID int64
		UserID  string
	}

	ListEventTriggersResponse struct {
		EventTriggers []EventTrigger
	}

	UpdateEventTriggerRequest struct {
		ID               int64
		UserID           string
		WorkspaceID      int64
		Title            string
		Body             string
		Assignee         string
		CronSchedule     string
		AllowAllCommands bool
		EmitEventID      int64
	}

	UpdateEventTriggerResponse struct {
		EventTrigger EventTrigger
	}

	DeleteEventTriggerRequest struct {
		ID     int64
		UserID string
	}

	ListTasksFromEventRequest struct {
		EventID int64
		UserID  string
	}

	ListTasksFromEventResponse struct {
		Tasks []Task
	}

	// Workflow entities (experimental)

	Workflow struct {
		ID           int64
		CreatedAt    time.Time
		UpdatedAt    time.Time
		UserID       int64
		Name         string
		Description  string
		StartEventID int64
		Layout       string
	}

	CreateWorkflowRequest struct {
		Name         string
		Description  string
		StartEventID int64
		UserID       string
	}

	CreateWorkflowResponse struct {
		Workflow Workflow
	}

	GetWorkflowRequest struct {
		ID     int64
		UserID string
	}

	GetWorkflowResponse struct {
		Workflow Workflow
	}

	ListWorkflowsRequest struct {
		UserID string
	}

	ListWorkflowsResponse struct {
		Workflows []Workflow
	}

	// UpdateWorkflowRequest carries only the mutable fields. Each is a pointer
	// so a PATCH can distinguish "not supplied" from "set to empty" — a layout
	// save must not blank the description, and vice versa.
	UpdateWorkflowRequest struct {
		ID           int64
		UserID       string
		Name         *string
		Description  *string
		StartEventID *int64
		Layout       *string
	}

	UpdateWorkflowResponse struct {
		Workflow Workflow
	}

	DeleteWorkflowRequest struct {
		ID     int64
		UserID string
	}

	// WorkflowStep entities

	WorkflowStep struct {
		ID               int64
		CreatedAt        time.Time
		UpdatedAt        time.Time
		WorkflowID       int64
		UserID           int64
		EventID          int64
		WorkspaceID      int64
		EmitEventID      int64
		Title            string
		Body             string
		Assignee         string
		AllowAllCommands bool
	}

	CreateWorkflowStepRequest struct {
		WorkflowID       int64
		EventID          int64
		WorkspaceID      int64
		EmitEventID      int64
		Title            string
		Body             string
		Assignee         string
		AllowAllCommands bool
		UserID           string
	}

	CreateWorkflowStepResponse struct {
		WorkflowStep WorkflowStep
	}

	ListWorkflowStepsRequest struct {
		WorkflowID int64
		UserID     string
	}

	ListWorkflowStepsResponse struct {
		WorkflowSteps []WorkflowStep
	}

	DeleteWorkflowStepRequest struct {
		ID         int64
		WorkflowID int64
		UserID     string
	}

	ListTasksFromWorkflowRequest struct {
		WorkflowID int64
		UserID     string
	}

	ListTasksFromWorkflowResponse struct {
		Tasks []Task
	}

	// Workflow text mode

	GetWorkflowTextRequest struct {
		ID     int64
		UserID string
	}

	GetWorkflowTextResponse struct {
		Text string
	}

	ReplaceWorkflowFromTextRequest struct {
		ID     int64
		UserID string
		Text   string
	}

	ReplaceWorkflowFromTextResponse struct {
		Workflow  Workflow
		StepCount int
	}
)

const (
	PubSubTopicCRUD   int64 = 1
	PubSubTopicMCP    int64 = 2
	PubSubTopicEvents int64 = 3
)

// What a task fires when it completes. Stored on Task.CompletionTriggerType to
// record the author's choice, which a non-zero WorkflowID alone cannot express:
// choosing a workflow also sets EventID to that workflow's start event.
const (
	CompletionTriggerNone     int16 = 0
	CompletionTriggerEvent    int16 = 1
	CompletionTriggerWorkflow int16 = 2
)

// EventPublishedPayload is the message sent on PubSubTopicEvents when an event fires.
type EventPublishedPayload struct {
	EventID int64
	Name    string
	Payload string
	FAQ     []EventFAQ
	// WorkflowID is set when the publishing task was part of a workflow run.
	// It scopes the consumer's fan-out to that workflow's steps instead of the
	// global event triggers; zero keeps the original global behavior.
	WorkflowID int64
	// Depth counts how many hops this run has already taken, so a cyclic graph
	// exhausts a budget instead of spawning tasks forever.
	Depth int
}

const (
	ActorHuman Actor = 1
	ActorAgent Actor = 2
	// ActorExtension is work done by a desktop extension acting on the user's
	// behalf. It is neither of the other two: counting it as human overstates
	// what a person did, and counting it as agent puts it in the same bucket as
	// the thing the workspace exists to measure.
	//
	// Appended, never inserted — the value is stored on every telemetry row, so
	// renumbering would silently reinterpret history.
	ActorExtension Actor = 3
)

const (
	ResourceUser ResourceType = iota + 1
	ResourceWorkspace
	ResourceTask
	ResourceMessage
	// Appended, like the actions below: a machine belongs to an account and a
	// session to a machine, so neither could be folded into the four above.
	ResourceMachine
	ResourceSession
	ResourceSkill
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
	case ResourceMachine:
		return "machine"
	case ResourceSession:
		return "session"
	case ResourceSkill:
		return "skill"
	}
	return "unknown"
}

const (
	OriginInvalid Origin = iota
	OriginAPI
	OriginMCP
	OriginScheduler
	OriginSlack
)

func (o Origin) String() string {
	switch o {
	case OriginInvalid:
		return "invalid"
	case OriginAPI:
		return "api"
	case OriginMCP:
		return "mcp"
	case OriginScheduler:
		return "scheduler"
	case OriginSlack:
		return "slack"
	}
	return "unknown"
}

func (o Origin) Int64() int64 {
	return int64(o)
}

type contextKey string

const OriginContextKey contextKey = "event_origin"

func WithOrigin(ctx context.Context, o Origin) context.Context {
	return context.WithValue(ctx, OriginContextKey, o)
}

func GetOrigin(ctx context.Context) Origin {
	if ctx == nil {
		return OriginInvalid
	}
	if o, ok := ctx.Value(OriginContextKey).(Origin); ok {
		return o
	}
	return OriginInvalid
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
	ActionTaskComplete               Action = 13
	ActionTaskApproveManual          Action = 14
	ActionTaskFromScheduled          Action = 15
	ActionTaskRejectManual           Action = 16
	ActionMCPToolCall                Action = 20
	ActionMCPPermissionManual        Action = 21
	ActionMCPPermissionAuto          Action = 22
	ActionTaskAllowAllCommandsToggle Action = 23
	// A model chosen from the interface. Recorded through the same controller
	// as the browser reports below, but emitted by the backend right after it
	// asks the agent to switch — so it stays out of ClientReportableAction,
	// which exists to say what a browser may claim, and this is not claimed.
	ActionAgentModelSelect Action = 24
	ActionEventPublished   Action = 30
	// Local-AI feature usage, reported by the browser rather than emitted by
	// the backend: these models run in the user's own tab, so nothing
	// server-side ever observes them. See ClientReportableAction, which is the
	// allowlist of what a browser is permitted to claim.
	ActionLocalAITitleGenerate Action = 40
	ActionLocalAIRecordingEnd  Action = 41
	// Interface usage, reported by the browser for the same reason: a keypress,
	// a search and a copy all begin and end in the tab, so the report is the
	// only evidence there is. Same allowlist, same ownership check.
	ActionUIShortcutUse    Action = 50
	ActionUISearch         Action = 51
	ActionUISearchOpen     Action = 52
	ActionUICopyLink       Action = 53
	ActionUICopyMarkdown   Action = 54
	ActionUITrajectoryView Action = 55
	ActionUICopyCode       Action = 56
	// Machines and the agents run on them, all emitted by the backend right
	// after it does the work — an enrolment, a delete, a kill switch, a
	// session row, a terminal socket. None of them is browser-reported and
	// none belongs in ClientReportableAction: the server is what observes
	// every one, so there is nothing for a client to claim.
	//
	// The three machine actions carry no workspace, and that is not an
	// omission: a machine belongs to an account and runs agents for many
	// workspaces at once, so there is no one workspace to attribute an
	// enrolment to. The session and terminal actions do carry one, because a
	// session knows which workspace it is working in.
	ActionMachineAdd     Action = 60
	ActionMachineRemove  Action = 61
	ActionMachineDisable Action = 62
	// Three moments rather than one, because the gaps between them are where
	// launching an agent goes wrong: a create with no open is a launch that
	// never started, and the interval between open and close is how long an
	// agent actually ran.
	ActionMachineSessionCreate Action = 63
	ActionMachineSessionOpen   Action = 64
	ActionMachineSessionClose  Action = 65
	// Somebody watching an agent work. The attach is already audited for the
	// same reason it is counted here — that a person opened a terminal, and
	// when they left, is the fact worth keeping. What they typed is not.
	ActionMachineTerminalOpen  Action = 66
	ActionMachineTerminalClose Action = 67
	// The other half of the kill switch. Numbered after the terminal actions
	// rather than beside its opposite because these are only ever appended;
	// the pairing is in the names, not the values.
	ActionMachineEnable Action = 68
	// A person stopping an agent, as distinct from one that ended.
	//
	// Close counts every session that finishes, however it finished — the
	// daemon reports the same terminal state whether the agent completed,
	// crashed, or was cut short. Only the request distinguishes somebody
	// deciding this was not working, which is the more interesting of the two.
	ActionMachineSessionKill Action = 69
	// Asking for an enrolment code: the first half of adding a machine.
	//
	// Counted separately from the enrolment itself because the gap between
	// them is the install funnel — somebody who asks for a code and never
	// redeems it got stuck installing the daemon, and that is invisible if
	// only the finished enrolments are counted.
	ActionMachineEnrolCodeCreate Action = 70
	// The gateway's task-concurrency limit changed from the interface.
	// Counted on the asking, same as ActionAgentModelSelect and for the same
	// reason: the gateway clamps or ignores the value and only its own
	// notification says which, so the count answers "how often people reach
	// for this", not "how often it took effect".
	ActionAgentConcurrencySelect Action = 71
	// Which kind of agent process was launched for a workspace. Two actions
	// rather than one action with a kind attribute, so acp-gateway and
	// claude-code are two directly comparable counts instead of a value
	// buried inside one bucket.
	ActionAgentLaunchClaudeCode Action = 72
	ActionAgentLaunchACPGateway Action = 73
	// Skills used from the interface. An agent's skill tools are already
	// counted as MCP tool calls, so these skip an MCP-origin context rather
	// than count the same read twice.
	ActionSkillImport Action = 74
	ActionSkillView   Action = 75
	ActionSkillSearch Action = 76
	// A conversation forked into a new task from the interface. The new task
	// is also counted as a task_create.
	ActionTaskFork Action = 77
	// A site shared into a workspace from the Chrome extension, or withdrawn.
	// Backend-observed, on the browser socket.
	ActionSiteShare   Action = 78
	ActionSiteUnshare Action = 79
)

// ClientReportableAction resolves an action name a browser is allowed to
// report into its Action.
//
// The allowlist is the security boundary for the telemetry ingest route.
// Every other action is emitted by the backend after it has done the work it
// describes, so accepting an arbitrary name from a client would let a caller
// mint task or message events it never performed and skew the same counters
// the real ones feed.
func ClientReportableAction(name string) (Action, bool) {
	switch name {
	case "local_ai_title_generate":
		return ActionLocalAITitleGenerate, true
	case "local_ai_recording_end":
		return ActionLocalAIRecordingEnd, true
	case "ui_shortcut_use":
		return ActionUIShortcutUse, true
	case "ui_search":
		return ActionUISearch, true
	case "ui_search_open":
		return ActionUISearchOpen, true
	case "ui_copy_link":
		return ActionUICopyLink, true
	case "ui_copy_markdown":
		return ActionUICopyMarkdown, true
	case "ui_trajectory_view":
		return ActionUITrajectoryView, true
	case "ui_copy_code":
		return ActionUICopyCode, true
	}
	return 0, false
}

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
	case ActionTaskAllowAllCommandsToggle:
		return "task_allow_all_commands_toggle"
	case ActionAgentModelSelect:
		return "agent_model_select"
	case ActionEventPublished:
		return "event_published"
	case ActionLocalAITitleGenerate:
		return "local_ai_title_generate"
	case ActionLocalAIRecordingEnd:
		return "local_ai_recording_end"
	case ActionUIShortcutUse:
		return "ui_shortcut_use"
	case ActionUISearch:
		return "ui_search"
	case ActionUISearchOpen:
		return "ui_search_open"
	case ActionUICopyLink:
		return "ui_copy_link"
	case ActionUICopyMarkdown:
		return "ui_copy_markdown"
	case ActionUITrajectoryView:
		return "ui_trajectory_view"
	case ActionUICopyCode:
		return "ui_copy_code"
	case ActionMachineAdd:
		return "machine_add"
	case ActionMachineRemove:
		return "machine_remove"
	case ActionMachineDisable:
		return "machine_disable"
	case ActionMachineSessionCreate:
		return "machine_session_create"
	case ActionMachineSessionOpen:
		return "machine_session_open"
	case ActionMachineSessionClose:
		return "machine_session_close"
	case ActionMachineTerminalOpen:
		return "machine_terminal_open"
	case ActionMachineTerminalClose:
		return "machine_terminal_close"
	case ActionMachineEnable:
		return "machine_enable"
	case ActionMachineSessionKill:
		return "machine_session_kill"
	case ActionMachineEnrolCodeCreate:
		return "machine_enrol_code_create"
	case ActionAgentConcurrencySelect:
		return "agent_concurrency_select"
	case ActionAgentLaunchClaudeCode:
		return "agent_launch_claude_code"
	case ActionAgentLaunchACPGateway:
		return "agent_launch_acp_gateway"
	case ActionSkillImport:
		return "skill_import"
	case ActionSkillView:
		return "skill_view"
	case ActionSkillSearch:
		return "skill_search"
	case ActionTaskFork:
		return "task_fork"
	case ActionSiteShare:
		return "site_share"
	case ActionSiteUnshare:
		return "site_unshare"
	}
	return "unknown"
}

// ── Machines ────────────────────────────────────────────────────────────────

type (
	// CreateEnrolmentCodeRequest asks for a short code to type on a machine.
	CreateEnrolmentCodeRequest struct {
		UserID string
	}

	// CreateEnrolmentCodeResponse carries the code back exactly once. It is
	// stored hashed, so this is the only moment it can be shown.
	CreateEnrolmentCodeResponse struct {
		Code      string    `json:"code"`
		ExpiresAt time.Time `json:"expiresAt"`
	}

	// EnrolMachineRequest is what a daemon sends to trade a code for identity.
	//
	// There is no UserID: the caller is not authenticated, and the code is what
	// decides whose machine this becomes. Taking an account from the request
	// would let anyone enrol a machine into anyone's account.
	EnrolMachineRequest struct {
		Code     string
		Name     string
		Hostname string
		OS       string
		Arch     string
		Version  string
	}

	// EnrolMachineResponse is returned once. The token is never recoverable.
	EnrolMachineResponse struct {
		MachineID string `json:"machineId"`
		// UserID is the account this machine now belongs to. Not a secret —
		// the daemon just traded a code issued by that account — and it is
		// what lets the daemon name itself in the headers on every later
		// connection, so a mismatch between token and claim is something the
		// backend can refuse rather than something it has to infer.
		UserID       string `json:"userId"`
		MachineToken string `json:"machineToken"`
	}
)

type (
	// MachineView is one machine as the control panel sees it.
	//
	// Online is **derived** here rather than stored, from LastSeenAt against a
	// threshold. A stored flag says "online" forever when a daemon is killed,
	// which is exactly the moment somebody is looking at the screen to find out.
	MachineView struct {
		ID         string     `json:"id"`
		Name       string     `json:"name"`
		Hostname   string     `json:"hostname"`
		OS         string     `json:"os"`
		Arch       string     `json:"arch"`
		Version    string     `json:"version"`
		Enabled    bool       `json:"enabled"`
		Online     bool       `json:"online"`
		LastSeenAt *time.Time `json:"lastSeenAt,omitempty"`
		CreatedAt  time.Time  `json:"createdAt"`

		AvailableVersion string `json:"availableVersion,omitempty"`

		// Sessions is how many are still running on this machine. Zero is a
		// real answer here, unlike Metrics: a machine with no agents on it is
		// exactly what "0" should say.
		Sessions int `json:"sessions"`

		// Metrics is the last snapshot the daemon reported, or nil if it has
		// not reported one. Nil rather than a zeroed struct: a machine that
		// has never said how much memory it has is a different thing from one
		// that has none.
		Metrics *MachineMetricsView `json:"metrics,omitempty"`
	}

	// MachineMetricsView is what a machine last said about itself.
	MachineMetricsView struct {
		// MemAvailable is what a new process could get, not what is unused —
		// on Linux "free" excludes the page cache and reads alarmingly low on
		// a perfectly healthy machine.
		MemTotal     int64 `json:"memTotal"`
		MemAvailable int64 `json:"memAvailable"`

		CPUPercent float64 `json:"cpuPercent"`

		// LoadAvg is absent on Windows, and must render as absent rather than
		// as three zeroes — which would read as a perfectly idle machine.
		LoadAvg []float64 `json:"loadAvg,omitempty"`

		UptimeSec int64 `json:"uptimeSec"`

		// Disks is per mount, never one number: a machine can be 2% full and
		// still fail to check out a repository, because the full filesystem is
		// the one that matters.
		Disks []MachineDiskView `json:"disks,omitempty"`

		// ReportedAt is when the machine measured this, so a stale snapshot
		// can be shown as stale rather than as current.
		ReportedAt time.Time `json:"reportedAt"`
	}

	// MachineDiskView is one filesystem's space.
	MachineDiskView struct {
		Mount string `json:"mount"`
		Total int64  `json:"total"`
		Free  int64  `json:"free"`
	}

	// RecordMachineMetricsRequest is a daemon's report about its own machine.
	//
	// The machine id comes from the authenticated socket, never from the
	// payload: a daemon may only ever describe itself.
	RecordMachineMetricsRequest struct {
		MachineID    int64
		MemTotal     int64
		MemAvailable int64
		CPUPercent   float64
		LoadAvg      []float64
		UptimeSec    int64
		Disks        []MachineDiskView
		ReportedAt   time.Time
	}

	// ReconcileSessionsRequest ends the sessions a machine is not running.
	//
	// Running is what the daemon says it is supervising. Everything else this
	// machine has marked live has ended without anybody being told.
	// RecordAvailableVersionRequest stores a release a daemon has found.
	//
	// The machine id comes from the authenticated socket, never the payload:
	// a daemon may only ever describe itself.
	RecordAvailableVersionRequest struct {
		MachineID int64
		// Version is empty when the daemon is current again, which clears the
		// offer rather than leaving a machine advertising an update it has
		// already installed.
		Version string
	}

	// ApproveMachineUpdateRequest is a person saying yes to losing their
	// sessions on one machine, for one release.
	ApproveMachineUpdateRequest struct {
		UserID    string
		MachineID string
		Version   string
	}
	ApproveMachineUpdateResponse struct {
		MachineID int64
		Version   string
	}

	// RecordMachineVersionRequest is what a daemon says it is running, from
	// its hello — the only message that knows.
	RecordMachineVersionRequest struct {
		MachineID int64
		Version   string
	}

	ReconcileSessionsRequest struct {
		MachineID int64
		Running   []int64
	}

	// ActiveSessionRequest asks whether a workspace already has an agent
	// running, from the database rather than from a live connection.
	ActiveSessionRequest struct {
		UserID      string
		WorkspaceID string
	}

	// RecordSessionKillRequest counts a person stopping an agent.
	//
	// Base62 as it arrives from the route and the session view, because the
	// caller is a REST handler that has just read the session.
	RecordSessionKillRequest struct {
		UserID      string
		WorkspaceID string
		SessionID   string
	}

	// RecordTerminalViewRequest counts a person watching a session's terminal.
	//
	// Already-resolved ids rather than base62 strings, because the caller is
	// the socket handler and it has them as numbers — it authorised the attach
	// by looking the session up.
	RecordTerminalViewRequest struct {
		UserID      int64
		WorkspaceID int64
		SessionID   int64
		// Open distinguishes the attach from the detach. Two actions rather
		// than one with a duration, because the socket can be closed by a
		// crash, and half an interval is worse than two counts.
		Open bool
	}

	// RecordSiteShareRequest counts a site shared into (Shared) or withdrawn
	// from a workspace.
	RecordSiteShareRequest struct {
		UserID      int64
		WorkspaceID int64
		Shared      bool
	}

	GetSessionRequest struct {
		UserID    string
		SessionID string
	}
	GetSessionResponse struct {
		Session SessionView `json:"session"`
	}

	// WorkspaceSessionResponse answers "is an agent running here, and which
	// one" for a single workspace.
	//
	// A pointer so that "nothing is running" is `null` rather than a
	// SessionView full of zero values. An empty struct would arrive at the
	// interface as a session with no id, which reads as a session right up to
	// the point something tries to open it.
	WorkspaceSessionResponse struct {
		Session *SessionView `json:"session"`
	}

	ListMachinesRequest struct {
		UserID string
	}
	ListMachinesResponse struct {
		Machines []MachineView `json:"machines"`
	}

	GetMachineRequest struct {
		UserID    string
		MachineID string
	}
	GetMachineResponse struct {
		Machine MachineView `json:"machine"`
	}

	// UpdateMachineRequest renames or enables/disables a machine.
	//
	// Both fields are pointers so "not mentioned" and "set to empty/false" are
	// different requests. Without that, a rename would silently re-enable a
	// machine somebody had deliberately turned off.
	UpdateMachineRequest struct {
		UserID    string
		MachineID string
		Name      *string
		Enabled   *bool
	}
	UpdateMachineResponse struct {
		Machine MachineView `json:"machine"`
	}

	DeleteMachineRequest struct {
		UserID    string
		MachineID string
	}
)

type (
	// SessionView is one agent session as the control panel sees it.
	SessionView struct {
		ID          string `json:"id"`
		MachineID   string `json:"machineId"`
		WorkspaceID string `json:"workspaceId,omitempty"`
		// WorkspaceName is which workspace this agent is working in.
		//
		// Carried beside the id because the id is not an answer to anybody:
		// a machine runs agents for several workspaces at once, and a list of
		// sessions that says only "claude-code" three times cannot be read.
		WorkspaceName string     `json:"workspaceName,omitempty"`
		Kind          string     `json:"kind"`
		Status        string     `json:"status"`
		ExitCode      *int       `json:"exitCode,omitempty"`
		Restored      bool       `json:"restored,omitempty"`
		Cols          int        `json:"cols,omitempty"`
		Rows          int        `json:"rows,omitempty"`
		StartedAt     *time.Time `json:"startedAt,omitempty"`
		EndedAt       *time.Time `json:"endedAt,omitempty"`
		CreatedAt     time.Time  `json:"createdAt"`
	}

	CreateSessionRequest struct {
		UserID      string
		MachineID   string
		WorkspaceID string
		Kind        string
		Cols        uint16
		Rows        uint16
	}
	CreateSessionResponse struct {
		Session SessionView `json:"session"`
	}

	UpdateSessionStateRequest struct {
		SessionID string
		// UserID is who owns the session, and is only used to count it.
		//
		// The state report authenticates a machine rather than a workspace and
		// names no workspace, so counting an agent starting or finishing needs
		// the row — and the row can only be read scoped to its owner. Optional
		// on purpose: a caller that does not set it still records the state, it
		// just is not counted, which is the right way round for a report that
		// must never fail over telemetry.
		UserID   string
		Status   string
		ExitCode *int
		EndedAt  *time.Time
		// Error explains a failure in terms the person can act on. Not stored
		// on the row today — it reaches the UI through the event stream — but
		// carried here so the controller has it when that lands.
		Error string
		// Restored marks a session re-spawned after an update, so the UI can
		// say why the scrollback is empty.
		Restored bool
	}

	// TerminalTicketResponse is the credential a page presents when it opens
	// a session's terminal socket.
	//
	// `expiresIn` is seconds, and is here so the caller does not have to know
	// the server's policy to know whether what it is holding is still worth
	// sending. Nothing caches a ticket today — every connection attempt asks
	// for a new one — and that is what keeps the lifetime short enough to
	// travel in a URL.
	TerminalTicketResponse struct {
		Ticket    string `json:"ticket"`
		ExpiresIn int    `json:"expiresIn"`
	}

	ListSessionsRequest struct {
		UserID    string
		MachineID string
	}
	ListSessionsResponse struct {
		Sessions []SessionView `json:"sessions"`
	}
)
