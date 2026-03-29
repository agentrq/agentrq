package model

import (
	"time"

	"gorm.io/datatypes"
)

type (
	// Workspace hosts an agentrq workspace
	Workspace struct {
		ID          int64 `gorm:"primaryKey;autoIncrement:false"`
		CreatedAt   time.Time
		UpdatedAt   time.Time
		UserID      int64 `gorm:"index:idx_workspaces_user_id"`
		Name        string `gorm:"type:varchar(128)"`
		Description string `gorm:"type:text"`
		ArchivedAt           *time.Time
		Icon                 string         `gorm:"type:text"`
		NotificationSettings datatypes.JSON `gorm:"type:text"`
		TokenEncrypted       string         `gorm:"type:text"`
		TokenNonce           string         `gorm:"type:varchar(64)"`
		AutoAllowedTools     datatypes.JSON `gorm:"type:text"`
	}

	// Task hosts a task created by a human or an agent within a workspace
	Task struct {
		ID        int64 `gorm:"primaryKey;autoIncrement:false"`
		CreatedAt time.Time
		UpdatedAt time.Time

		UserID      int64 `gorm:"index:idx_tasks_user_id"`
		WorkspaceID int64  `gorm:"index:idx_tasks_workspace_id"`
		CreatedBy   string `gorm:"type:varchar(16)"` // "human" | "agent"
		Assignee    string `gorm:"type:varchar(16)"` // "human" | "agent"
		Status      string `gorm:"type:varchar(16)"` // "pending" | "done" | "rejected"
		Title       string `gorm:"type:varchar(255)"`
		Body        string `gorm:"type:text"`
		Response    string `gorm:"type:text"`
		ReplyText   string `gorm:"type:text"`
		Attachments datatypes.JSON
		Messages    []Message `gorm:"foreignKey:TaskID"`

		CronSchedule string `gorm:"type:varchar(64)"`
		ParentID     int64  `gorm:"index:idx_tasks_parent_id"`
		SortOrder    float64 `gorm:"type:real;default:0"`
	}

	// Message is an entry in a task's chat history
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
		UserID      int64  `gorm:"index:idx_telemetry_user_id"`
		WorkspaceID int64  `gorm:"index:idx_telemetry_workspace_id"`
		OccurredAt  int64  `gorm:"index:idx_telemetry_occurred_at"`
		Action      uint8  `gorm:"index:idx_telemetry_action"`
		Actor       uint8  `gorm:"index:idx_telemetry_actor"`
	}

	// User represents a human user
	User struct {
		ID         int64 `gorm:"primaryKey;autoIncrement:false"`
		CreatedAt  time.Time
		UpdatedAt  time.Time
		Email      string `gorm:"type:varchar(255);uniqueIndex"`
		Name       string `gorm:"type:varchar(255)"`
		Picture    string `gorm:"type:text"`
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
)
