// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package crud

import (
	"context"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/idgen"
	"github.com/agentrq/agentrq/backend/internal/service/image"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
	"github.com/agentrq/agentrq/backend/internal/service/ratelimit"
	"github.com/agentrq/agentrq/backend/internal/service/skillimport"
	"github.com/agentrq/agentrq/backend/internal/service/storage"
)

type (
	Params struct {
		IDGen      idgen.Service
		Repository base.Repository
		Storage    storage.Service
		// SkillStorage holds skill file content. Nil falls back to Storage.
		SkillStorage storage.Service
		Image        image.Service
		PubSub       pubsub.Service
		Limiter      ratelimit.Limiter
		// SkillImport reads skills from GitHub. Nil turns importing off.
		SkillImport skillimport.Service
		// TaskAgents names the agents in a task's history. Nil leaves them
		// unnamed.
		TaskAgents TaskAgentNames
	}

	// TaskAgentNames is the part of the taskagent controller a task's
	// history reads.
	TaskAgentNames interface {
		Names(ctx context.Context, agentIDs, modelIDs []int64) (agents, models map[int64]string, err error)
	}

	Controller interface {
		WorkspaceController
		UserController
		TaskController
		EventController
		MemoryController
		SkillController
		SiteShareController
		OAuthConsentController
		EventTriggerController
		WorkflowController
		WorkflowStepController
		WorkflowTextController
		TelemetryController
		MachineController
		MachineManageController
		SessionController
		PublicFileController
	}

	controller struct {
		idgen      idgen.Service
		repository base.Repository
		storage    storage.Service
		image      image.Service
		pubsub     pubsub.Service
		limiter    ratelimit.Limiter

		skillStorage storage.Service
		skillImport  skillimport.Service
		taskAgents   TaskAgentNames
	}
)

func New(p Params) Controller {
	skillStorage := p.SkillStorage
	if skillStorage == nil {
		skillStorage = p.Storage
	}
	return &controller{
		idgen:      p.IDGen,
		repository: p.Repository,
		storage:    p.Storage,
		image:      p.Image,
		pubsub:     p.PubSub,
		limiter:    p.Limiter,

		skillStorage: skillStorage,
		skillImport:  p.SkillImport,
		taskAgents:   p.TaskAgents,
	}
}

func (c *controller) emitEvent(ctx context.Context, e entity.CRUDEvent) {
	if c.pubsub == nil {
		return
	}
	if e.Origin == entity.OriginInvalid {
		e.Origin = entity.GetOrigin(ctx)
		if e.Origin == entity.OriginInvalid {
			e.Origin = entity.OriginAPI
		}
	}
	_, _ = c.pubsub.Publish(ctx, pubsub.PublishRequest{
		PubSubID: entity.PubSubTopicCRUD,
		Event:    e,
	})
}

// WorkspaceController defines workspace operations.
type WorkspaceController interface {
	CreateWorkspace(ctx context.Context, req entity.CreateWorkspaceRequest) (*entity.CreateWorkspaceResponse, error)
	DeleteWorkspace(ctx context.Context, req entity.DeleteWorkspaceRequest) error
	GetWorkspace(ctx context.Context, req entity.GetWorkspaceRequest) (*entity.GetWorkspaceResponse, error)
	CheckWorkspaceAccess(ctx context.Context, id int64, userID string) (bool, error)
	ListWorkspaces(ctx context.Context, req entity.ListWorkspacesRequest) (*entity.ListWorkspacesResponse, error)
	ArchiveWorkspace(ctx context.Context, req entity.ArchiveWorkspaceRequest) error
	UnarchiveWorkspace(ctx context.Context, req entity.UnarchiveWorkspaceRequest) error
	UpdateWorkspace(ctx context.Context, req entity.UpdateWorkspaceRequest) (*entity.UpdateWorkspaceResponse, error)
	UpdateWorkspaceAutoAllowedTools(ctx context.Context, req entity.UpdateWorkspaceAutoAllowedToolsRequest) error
	ForkWorkspace(ctx context.Context, req entity.ForkWorkspaceRequest) (*entity.ForkWorkspaceResponse, error)
	CheckForkMerge(ctx context.Context, req entity.MergeForkRequest) error
	MergeFork(ctx context.Context, req entity.MergeForkRequest) (*entity.MergeForkResponse, error)
	RecordForkDirectory(ctx context.Context, req entity.RecordForkDirectoryRequest) error
	GetDetailedWorkspaceStats(ctx context.Context, req entity.GetWorkspaceStatsRequest) (*entity.GetDetailedWorkspaceStatsResponse, error)
	GetDetailedUserStats(ctx context.Context, req entity.GetUserStatsRequest) (*entity.GetDetailedUserStatsResponse, error)
	GetTaskLatencyStats(ctx context.Context, req entity.GetTaskLatencyStatsRequest) (*entity.GetTaskLatencyStatsResponse, error)
	SystemGetWorkspace(ctx context.Context, id int64) (entity.Workspace, error)
}

// UserController defines user operations.
type UserController interface {
	CreateUser(ctx context.Context, u entity.User) (entity.User, error)
	FindUserByEmail(ctx context.Context, email string) (entity.User, error)
	FindUserByID(ctx context.Context, id int64) (entity.User, error)
	FindOrCreateUser(ctx context.Context, req entity.FindOrCreateUserRequest) (*entity.FindOrCreateUserResponse, error)
}

// TaskController defines task operations.
type TaskController interface {
	CreateTask(ctx context.Context, req entity.CreateTaskRequest) (*entity.CreateTaskResponse, error)
	GetTask(ctx context.Context, req entity.GetTaskRequest) (*entity.GetTaskResponse, error)
	ListTasks(ctx context.Context, req entity.ListTasksRequest) (*entity.ListTasksResponse, error)
	RespondToTask(ctx context.Context, req entity.RespondToTaskRequest) (*entity.RespondToTaskResponse, error)
	ForkTask(ctx context.Context, req entity.ForkTaskRequest) (*entity.ForkTaskResponse, error)
	UpdateTaskStatus(ctx context.Context, req entity.UpdateTaskStatusRequest) (*entity.UpdateTaskStatusResponse, error)
	UpdateTaskOrder(ctx context.Context, req entity.UpdateTaskOrderRequest) (*entity.UpdateTaskOrderResponse, error)
	UpdateTaskAssignee(ctx context.Context, req entity.UpdateTaskAssigneeRequest) (*entity.UpdateTaskAssigneeResponse, error)
	MoveTask(ctx context.Context, req entity.MoveTaskRequest) (*entity.MoveTaskResponse, error)
	UpdateTaskAllowAllCommands(ctx context.Context, req entity.UpdateTaskAllowAllCommandsRequest) (*entity.UpdateTaskAllowAllCommandsResponse, error)
	ReplyToTask(ctx context.Context, req entity.ReplyToTaskRequest) (*entity.ReplyToTaskResponse, error)
	UpdateScheduledTask(ctx context.Context, req entity.UpdateScheduledTaskRequest) (*entity.UpdateScheduledTaskResponse, error)
	UpdateMessageMetadata(ctx context.Context, req entity.UpdateMessageMetadataRequest) error
	CloseExpiredElicitation(ctx context.Context, req entity.CloseExpiredElicitationRequest) (*entity.CloseExpiredElicitationResponse, error)
	GetGlobalTaskStats(ctx context.Context, userID string) (*entity.GlobalTaskStatsResponse, error)
	DeleteTask(ctx context.Context, req entity.DeleteTaskRequest) (*entity.DeleteTaskResponse, error)
	GetAttachment(ctx context.Context, req entity.GetAttachmentRequest) (*entity.GetAttachmentResponse, error)
	GetWorkspaceTaskCounts(ctx context.Context, req entity.GetWorkspaceTaskCountsRequest) (map[string]int64, error)
}
