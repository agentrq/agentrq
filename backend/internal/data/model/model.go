package model

import (
	"time"

	"gorm.io/datatypes"
)

type (
	// Workspace hosts an agentrq workspace
	Workspace struct {
		ID                   int64 `gorm:"primaryKey;autoIncrement:false"`
		CreatedAt            time.Time
		UpdatedAt            time.Time
		UserID               int64  `gorm:"index:idx_workspaces_user_id"`
		Name                 string `gorm:"type:varchar(128)"`
		Description          string `gorm:"type:text"`
		ArchivedAt           *time.Time
		Icon                 string         `gorm:"type:text"`
		NotificationSettings datatypes.JSON `gorm:"type:text"`
		AutoAllowedTools     datatypes.JSON `gorm:"type:text"`
		AllowAllCommands     bool           `gorm:"default:false"`
		SelfLearningLoopNote string         `gorm:"type:text"`
	}

	// Task hosts a task created by a human or an agent within a workspace
	Task struct {
		ID        int64 `gorm:"primaryKey;autoIncrement:false"`
		CreatedAt time.Time
		UpdatedAt time.Time

		// idx_tasks_dequeue is a composite index matching the equality prefix of the
		// agent work-dequeue query (ClaimNextTask / GetNextTask): workspace_id, user_id,
		// status. Column order mirrors the query. assignee and sort_order are left out
		// deliberately to keep the index small.
		UserID      int64  `gorm:"index:idx_tasks_user_id;index:idx_tasks_dequeue,priority:2"`
		WorkspaceID int64  `gorm:"index:idx_tasks_workspace_id;index:idx_tasks_dequeue,priority:1"`
		CreatedBy   string `gorm:"type:varchar(16)"`                                       // "human" | "agent"
		Assignee    string `gorm:"type:varchar(16)"`                                       // "human" | "agent"
		Status      string `json:"status" gorm:"index;index:idx_tasks_dequeue,priority:3"` // notstarted, ongoing, completed, rejected, cron, blocked
		Title       string `gorm:"type:varchar(255)"`
		Body        string `gorm:"type:text"`
		Response    string `gorm:"type:text"`
		ReplyText   string `gorm:"type:text"`
		Attachments datatypes.JSON
		Messages    []Message  `gorm:"foreignKey:TaskID"`
		ToolCalls   []ToolCall `gorm:"foreignKey:TaskID"`

		CronSchedule     string  `gorm:"type:varchar(64)"`
		ParentID         int64   `gorm:"index:idx_tasks_parent_id"`
		SortOrder        float64 `gorm:"type:real;default:0"`
		AllowAllCommands bool    `gorm:"default:false"`
		TriggerID        int64   `gorm:"index:idx_tasks_trigger_id"` // event that caused this task
		EventID          int64   `gorm:"index:idx_tasks_event_id"`   // event this task emits on completion
		// WorkflowID carries workflow context through a run: when this task
		// publishes its event, the consumer routes the fan-out through this
		// workflow's steps instead of the global triggers, and stamps the same
		// ID on every task it spawns. Zero means "not part of a workflow run".
		WorkflowID int64 `gorm:"index:idx_tasks_workflow_id"`
		// WorkflowDepth is how many hops preceded this task in its run. It has
		// to be persisted rather than tracked in memory because every hop
		// crosses a task boundary — the chain is task → publish → task — so
		// without it the runaway-guard counter would reset to zero each hop and
		// never trip.
		WorkflowDepth int `gorm:"default:0"`
	}

	// ToolCall records a single tool-call permission decision for a task: either
	// auto-allowed (matched a stored auto-allow rule, or the task has
	// AllowAllCommands/"YOLO" enabled) or a manual request awaiting/resolved by a
	// human verdict. This is the only place auto-allowed calls are recorded —
	// they otherwise bypass the message thread entirely.
	ToolCall struct {
		ID           int64 `gorm:"primaryKey;autoIncrement:false"`
		CreatedAt    time.Time
		TaskID       int64  `gorm:"index:idx_tool_calls_task_id"`
		WorkspaceID  int64  `gorm:"index:idx_tool_calls_workspace_id"`
		ToolName     string `gorm:"type:varchar(128)"`
		Description  string `gorm:"type:text"`
		InputPreview string `gorm:"type:text"`
		Status       string `gorm:"type:varchar(16)"` // "auto_allowed" | "pending" | "allowed" | "denied"
	}

	// Event defines a named event that agents can publish after completing a task.
	// Other workspaces can subscribe to it via EventTrigger.
	Event struct {
		ID                int64 `gorm:"primaryKey;autoIncrement:false"`
		CreatedAt         time.Time
		UpdatedAt         time.Time
		UserID            int64  `gorm:"index:idx_events_user_id"`
		Name              string `gorm:"type:varchar(140);uniqueIndex:idx_events_name_user_id"`
		PayloadGuidelines string `gorm:"type:text"`
	}

	// EventTrigger subscribes a workspace to an Event; when the event fires, a task
	// is created in the target workspace using the stored template.
	EventTrigger struct {
		ID               int64 `gorm:"primaryKey;autoIncrement:false"`
		CreatedAt        time.Time
		UpdatedAt        time.Time
		EventID          int64  `gorm:"index:idx_event_triggers_event_id"`
		WorkspaceID      int64  `gorm:"index:idx_event_triggers_workspace_id"`
		UserID           int64  `gorm:"index:idx_event_triggers_user_id"`
		Title            string `gorm:"type:varchar(255)"`
		Body             string `gorm:"type:text"`
		Assignee         string `gorm:"type:varchar(16)"`
		CronSchedule     string `gorm:"type:varchar(64)"`
		AllowAllCommands bool   `gorm:"default:false"`
		EmitEventID      int64  `gorm:"index:idx_event_triggers_emit_event_id"` // event this trigger's task emits on completion
	}

	// Workflow is a named, self-contained graph of events and workspaces
	// (experimental). It composes the same edge shape as EventTrigger, but
	// scoped to one workflow so editing a workflow never changes global event
	// behavior, and two workflows may route the same event differently.
	//
	// A run starts when a task carrying this workflow's ID publishes
	// StartEventID; from there WorkflowStep rows decide the fan-out, and each
	// spawned task carries the workflow ID onward so the chain continues.
	Workflow struct {
		ID          int64 `gorm:"primaryKey;autoIncrement:false"`
		CreatedAt   time.Time
		UpdatedAt   time.Time
		UserID      int64  `gorm:"index:idx_workflows_user_id"`
		Name        string `gorm:"type:varchar(140);uniqueIndex:idx_workflows_name_user_id"`
		Description string `gorm:"type:text"`
		// StartEventID is the event that begins a run of this workflow.
		StartEventID int64 `gorm:"index:idx_workflows_start_event_id"`
		// Layout holds canvas node positions for the graph editor, keyed by
		// node id ("event:<base62>" / "workspace:<base62>"). Purely
		// presentational: the executable graph lives in WorkflowStep, so a
		// missing or stale layout degrades to auto-placement, never to wrong
		// routing.
		Layout datatypes.JSON `gorm:"type:text"`
	}

	// WorkflowStep is one edge of a Workflow: when EventID fires inside this
	// workflow, create a task in WorkspaceID from the stored template, and
	// optionally have that task publish EmitEventID on completion (which
	// advances the run to the next step).
	WorkflowStep struct {
		ID               int64 `gorm:"primaryKey;autoIncrement:false"`
		CreatedAt        time.Time
		UpdatedAt        time.Time
		WorkflowID       int64  `gorm:"index:idx_workflow_steps_workflow_id"`
		UserID           int64  `gorm:"index:idx_workflow_steps_user_id"`
		EventID          int64  `gorm:"index:idx_workflow_steps_event_id"`
		WorkspaceID      int64  `gorm:"index:idx_workflow_steps_workspace_id"`
		EmitEventID      int64  `gorm:"index:idx_workflow_steps_emit_event_id"`
		Title            string `gorm:"type:varchar(255)"`
		Body             string `gorm:"type:text"`
		Assignee         string `gorm:"type:varchar(16)"`
		AllowAllCommands bool   `gorm:"default:false"`
	}

	// Message is an entry in a task's chat history
	Message struct {
		ID          int64 `gorm:"primaryKey;autoIncrement:false"`
		CreatedAt   time.Time
		TaskID      int64  `gorm:"index:idx_messages_task_id"`
		UserID      int64  `gorm:"index:idx_messages_user_id"`
		Sender      string `gorm:"type:varchar(16)"` // "human" | "agent"
		Text        string `gorm:"type:text"`
		Attachments datatypes.JSON
		Metadata    datatypes.JSON
	}

	// Telemetry record for user and workspace actions
	Telemetry struct {
		UserID      int64 `gorm:"index:idx_telemetry_user_id"`
		WorkspaceID int64 `gorm:"index:idx_telemetry_workspace_id"`
		OccurredAt  int64 `gorm:"index:idx_telemetry_occurred_at"`
		Action      uint8 `gorm:"index:idx_telemetry_action"`
		Actor       uint8 `gorm:"index:idx_telemetry_actor"`
		ClientID    int64 `gorm:"index:idx_telemetry_client_id"` // xxhash64(name+version) of the MCP client (reinterpreted as int64; no unsigned bigint in Postgres), 0 if unknown; see MCPClient
	}

	// MCPClient is a lookup table of distinct MCP client identities seen on
	// requests, keyed by xxhash64(name+"@"+version) reinterpreted as int64 so
	// Telemetry rows can reference "which agent" (Claude Code, Codex, ...)
	// without repeating the raw name/version on every row.
	MCPClient struct {
		ID        int64  `gorm:"primaryKey;autoIncrement:false"`
		Name      string `gorm:"type:varchar(255)"`
		Version   string `gorm:"type:varchar(64)"`
		CreatedAt time.Time
	}

	// User represents a human user
	User struct {
		ID        int64 `gorm:"primaryKey;autoIncrement:false"`
		CreatedAt time.Time
		UpdatedAt time.Time
		Email     string `gorm:"type:varchar(255);uniqueIndex"`
		Name      string `gorm:"type:varchar(255)"`
		Picture   string `gorm:"type:text"`
	}

	// SlackWorkspaceLink stores the Slack channel assigned to a workspace.
	// One row per workspace; upserted whenever the channel is changed.
	SlackWorkspaceLink struct {
		WorkspaceID      int64  `gorm:"primaryKey;autoIncrement:false"`
		SlackChannelID   string `gorm:"type:varchar(32)"`
		SlackChannelName string `gorm:"type:varchar(80)"`
		AccessToken      string `gorm:"type:text"`
		TokenNonce       string `gorm:"type:varchar(32)"`
		TeamID           string `gorm:"type:varchar(32)"`
		BotUserID        string `gorm:"type:varchar(32)"`
		AutoCreated      bool   `gorm:"default:false"` // true if created automatically on workspace creation
	}

	// PushSubscription stores a Web Push subscription for a user per workspace.
	PushSubscription struct {
		ID          int64 `gorm:"primaryKey;autoIncrement:false"`
		CreatedAt   time.Time
		UserID      int64  `gorm:"index:idx_push_subscriptions_user_id"`
		WorkspaceID int64  `gorm:"index:idx_push_subscriptions_workspace_id;uniqueIndex:idx_push_endpoint_workspace"`
		Endpoint    string `gorm:"type:text;uniqueIndex:idx_push_endpoint_workspace"`
		P256dh      string `gorm:"type:text"`
		Auth        string `gorm:"type:varchar(64)"`
		UserAgent   string `gorm:"type:varchar(255)"`
		Types       string `gorm:"type:text"` // comma-separated; empty = all types
	}

	// SlackTaskThread maps an AgentRQ task to a Slack thread timestamp (ts).
	// One row per task; created when the first Slack message for the task is posted.
	SlackTaskThread struct {
		TaskID         int64  `gorm:"primaryKey;autoIncrement:false"`
		WorkspaceID    int64  `gorm:"index"`
		SlackChannelID string `gorm:"type:varchar(32)"`
		ThreadTS       string `gorm:"type:varchar(32)"` // Slack message ts that anchors the thread
	}
)

const (
	ActionIDUnknown uint8 = iota
	ActionIDWorkspaceCreate
	ActionIDWorkspaceUpdate
	ActionIDWorkspaceDelete
	ActionIDTaskCreate
	ActionIDTaskUpdate
	ActionIDTaskDelete
	ActionIDMessageCreate
	ActionIDMessageUpdate
	ActionIDMessageDelete
	ActionIDMCPToolCall
	ActionIDTaskApproveManual
	ActionIDMCPPermissionManual
	ActionIDMCPPermissionAuto
	ActionIDMCPPermissionDeny
	ActionIDTaskRejectManual
	ActionIDTaskComplete
	ActionIDTaskFromScheduled
	ActionIDUserCreate
	ActionIDMCPConnect
)
