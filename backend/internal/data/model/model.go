// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package model

import (
	"time"

	"gorm.io/datatypes"
)

type (
	// Workspace hosts an agentrq workspace
	Workspace struct {
		ID                    int64 `gorm:"primaryKey;autoIncrement:false"`
		CreatedAt             time.Time
		UpdatedAt             time.Time
		UserID                int64  `gorm:"index:idx_workspaces_user_id"`
		Name                  string `gorm:"type:varchar(128)"`
		Description           string `gorm:"type:text"`
		ArchivedAt            *time.Time
		Icon                  string         `gorm:"type:text"`
		NotificationSettings  datatypes.JSON `gorm:"type:text"`
		AutoAllowedTools      datatypes.JSON `gorm:"type:text"`
		AllowAllCommands      bool           `gorm:"default:false"`
		SelfLearningLoopNote  string         `gorm:"type:text"`
		InputSendDelaySeconds int            `gorm:"default:0"`
		WorkingDirectory      string         `gorm:"type:text"`
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
		// CompletionTriggerType records what the author actually chose on the
		// task form: CompletionTriggerNone/Event/Workflow. Strictly it is
		// derivable (a non-zero WorkflowID implies a workflow), but choosing a
		// workflow also sets EventID to that workflow's start event — so
		// without this the UI cannot tell "they picked workflow new_feature"
		// from "they picked event code_changed" and would label it wrong.
		CompletionTriggerType int16 `gorm:"default:0"`
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
		ID        int64 `gorm:"primaryKey;autoIncrement:false"`
		CreatedAt time.Time
		UpdatedAt time.Time
		// The unique index spans (name, user_id): both fields carry the same
		// index name, which is what makes it composite. Tagging only Name —
		// even with a name mentioning user_id — silently produces a unique
		// index on `name` alone, making event names global across every
		// account, so one user taking "deploy" locks it for everyone.
		UserID            int64  `gorm:"index:idx_events_user_id;uniqueIndex:idx_events_name_user_id,priority:2"`
		Name              string `gorm:"type:varchar(140);uniqueIndex:idx_events_name_user_id,priority:1"`
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
		ID        int64 `gorm:"primaryKey;autoIncrement:false"`
		CreatedAt time.Time
		UpdatedAt time.Time
		// Composite with Name, for the same reason as Event above.
		UserID      int64  `gorm:"index:idx_workflows_user_id;uniqueIndex:idx_workflows_name_user_id,priority:2"`
		Name        string `gorm:"type:varchar(140);uniqueIndex:idx_workflows_name_user_id,priority:1"`
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
	// Memory is what an agent has chosen to remember about a workspace.
	//
	// Scoped to the workspace's owner rather than to whichever agent wrote it:
	// this is the workspace's memory, so every agent working there reads and
	// writes the same rows. Keyed per (owner, workspace, name), so several
	// named memories can live side by side; MEMORY.md is the one agents read
	// first and is where the index to the others belongs.
	//
	// The unique index spans all three columns, and every one of them carries
	// the same index name with a priority — see the note on Event for what
	// happens otherwise. Here the consequence would be a memory name that is
	// unique across every workspace and every account, so one workspace saving
	// "MEMORY.md" would take the name from everybody else.
	//
	// Content is capped at 16 KiB by the tools that write it, which is where
	// there is a caller to refuse. The column is deliberately larger: the cap
	// is counted in bytes of UTF-8, and 64000 characters is comfortably above
	// what 16 KiB can encode.
	Memory struct {
		ID          int64 `gorm:"primaryKey;autoIncrement:false"`
		CreatedAt   time.Time
		UpdatedAt   time.Time
		UserID      int64  `gorm:"index:idx_memories_user_id;uniqueIndex:uk_user_id_workspace_id_name,priority:1"`
		WorkspaceID int64  `gorm:"index:idx_memories_workspace_id;uniqueIndex:uk_user_id_workspace_id_name,priority:2"`
		Name        string `gorm:"type:varchar(32);uniqueIndex:uk_user_id_workspace_id_name,priority:3"`
		Content     string `gorm:"type:varchar(64000)"`
	}

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

	// Machine is one enrolled computer, as seen by one account.
	//
	// A physical box enrolled against two accounts is two rows, because the
	// token is account-scoped. That is correct rather than a limitation:
	// sharing one row would tell account B that account A's machine exists.
	Machine struct {
		ID        int64 `gorm:"primaryKey;autoIncrement:false"`
		CreatedAt time.Time
		UpdatedAt time.Time
		UserID    int64 `gorm:"index:idx_machines_user_id"`

		// Reported by the daemon at enrolment. None of it is a secret; it is
		// what the control panel shows so a person can tell one machine from
		// another.
		Name     string `gorm:"type:varchar(128)"`
		Hostname string `gorm:"type:varchar(255)"`
		OS       string `gorm:"type:varchar(32)"`
		Arch     string `gorm:"type:varchar(32)"`
		Version  string `gorm:"type:varchar(64)"`

		// TokenHash is the SHA-256 of the machine token, hex encoded. The
		// token itself is returned once at enrolment and never stored: a
		// database that leaks must not hand over the ability to act as every
		// enrolled machine. Indexed because every daemon request looks a
		// machine up by it.
		TokenHash string `gorm:"type:varchar(64);uniqueIndex:idx_machines_token_hash"`

		// Enabled is the kill switch. Disabling closes the socket server-side
		// and takes effect without the daemon's cooperation.
		Enabled bool `gorm:"default:true"`

		// LastSeenAt drives online/offline, which is **derived** and never
		// stored: a stored flag says "online" forever when a daemon is killed,
		// which is exactly when the answer matters.
		LastSeenAt *time.Time

		// InstanceID names the backend process currently holding this
		// machine's socket, so an attach arriving at a different instance
		// knows where to relay. Cleared on disconnect. Treated as a lease
		// rather than a fact, because an instance killed outright never gets
		// to clear it — LastSeenAt is what makes a stale row harmless.
		InstanceID  string `gorm:"type:varchar(128);index:idx_machines_instance_id"`
		ConnectedAt *time.Time

		// AvailableVersion is set when the daemon reports an update it has
		// found. Null means it is current.
		AvailableVersion string `gorm:"type:varchar(64)"`

		// The latest metrics snapshot, and only the latest. Storing every
		// heartbeat would be a table that grows forever to power a widget
		// nobody has asked for.
		MemTotal     int64
		MemAvailable int64
		CPUPercent   float64
		LoadAvg      string `gorm:"type:varchar(64)"` // JSON array; absent on Windows
		UptimeSec    int64
		Disks        datatypes.JSON `gorm:"type:text"` // per mount, never one number
		MetricsAt    *time.Time
	}

	// EnrolmentCode is a short, single-use secret a person types on the
	// machine being enrolled.
	//
	// Stored hashed like any other credential even though it lives for
	// minutes: it is short enough to be guessable if the table ever leaks, and
	// hashing it costs nothing.
	EnrolmentCode struct {
		ID        int64 `gorm:"primaryKey;autoIncrement:false"`
		CreatedAt time.Time
		UserID    int64  `gorm:"index:idx_enrolment_codes_user_id"`
		CodeHash  string `gorm:"type:varchar(64);uniqueIndex:idx_enrolment_codes_hash"`
		ExpiresAt time.Time
		// UsedAt makes the code single-use. Kept rather than deleted so a
		// second attempt can be told "already used" instead of "unknown",
		// which is the difference between a person retrying and a person
		// wondering whether they mistyped it.
		UsedAt    *time.Time
		MachineID int64
	}

	// Session is one agent process in one pseudo-terminal on one machine.
	Session struct {
		ID        int64 `gorm:"primaryKey;autoIncrement:false"`
		CreatedAt time.Time
		UpdatedAt time.Time
		MachineID int64 `gorm:"index:idx_sessions_machine_id"`
		UserID    int64 `gorm:"index:idx_sessions_user_id"`

		// Kind is claude-code or acp-gateway. Never a command line: the
		// daemon resolves a kind to an executable from its own config, so a
		// backend that has been taken over cannot ask for /bin/sh.
		Kind        string `gorm:"type:varchar(32)"`
		WorkspaceID int64  `gorm:"index:idx_sessions_workspace_id"`

		Status   string `gorm:"type:varchar(16)"` // starting|running|exited|killed|failed
		ExitCode *int
		// Restored marks a session re-spawned after a daemon update. Surfaced
		// in the UI so nobody wonders why their scrollback is empty: what is
		// restored is the intent, not the state.
		Restored bool `gorm:"default:false"`

		Cols      int
		Rows      int
		StartedAt *time.Time
		EndedAt   *time.Time
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
	// These values are stored in telemetry.action, so new ones only ever go on
	// the end: inserting above would silently reinterpret every existing row.
	ActionIDLocalAITitleGenerate
	ActionIDLocalAIRecordingEnd
	// Interface usage, also browser-reported: which of the shortcuts, the
	// finder and the copy affordances people actually reach for. Nothing
	// server-side sees any of them.
	ActionIDUIShortcutUse
	ActionIDUISearch
	ActionIDUISearchOpen
	ActionIDUICopyLink
	ActionIDUICopyMarkdown
	ActionIDUITrajectoryView
	// A model chosen from the interface, emitted by the backend when it asks
	// the agent to switch. On the end like everything else here, for the reason
	// recorded above.
	ActionIDAgentModelSelect
	// A permission request answered by an installed desktop extension the user
	// consented to, rather than by the user. Their own pair rather than folded
	// into the manual or automatic counts: nobody stopped to decide these, and
	// they are not a standing rule the user wrote either, so counting them as
	// either would make one of those numbers mean something it does not.
	ActionIDMCPPermissionExtensionAllow
	ActionIDMCPPermissionExtensionDeny
)
