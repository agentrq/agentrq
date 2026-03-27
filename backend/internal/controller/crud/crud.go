package crud

import (
	"context"

	entity "github.com/hasmcp/agentrq/backend/internal/data/entity/crud"
	"github.com/hasmcp/agentrq/backend/internal/repository/base"
	"github.com/hasmcp/agentrq/backend/internal/service/idgen"
	"github.com/hasmcp/agentrq/backend/internal/service/image"
	"github.com/hasmcp/agentrq/backend/internal/service/notif"
	"github.com/hasmcp/agentrq/backend/internal/service/storage"
	"github.com/hasmcp/agentrq/backend/internal/service/telemetry"
)

type (
	Params struct {
		IDGen      idgen.Service
		Repository base.Repository
		Storage    storage.Service
		Image      image.Service
		Notif      notif.Service
		TokenKey   string
		Telemetry  telemetry.Service
	}

	Controller interface {
		WorkspaceController
		TaskController
		UserController
	}

	controller struct {
		idgen      idgen.Service
		repository base.Repository
		storage    storage.Service
		img        image.Service
		notif      notif.Service
		tokenKey   string
		telemetry  telemetry.Service
	}
)

func New(p Params) Controller {
	return &controller{
		idgen:      p.IDGen,
		repository: p.Repository,
		storage:    p.Storage,
		img:        p.Image,
		notif:      p.Notif,
		tokenKey:   p.TokenKey,
		telemetry:  p.Telemetry,
	}
}

// WorkspaceController defines workspace operations.
type WorkspaceController interface {
	CreateWorkspace(ctx context.Context, req entity.CreateWorkspaceRequest) (*entity.CreateWorkspaceResponse, error)
	GetWorkspace(ctx context.Context, req entity.GetWorkspaceRequest) (*entity.GetWorkspaceResponse, error)
	ListWorkspaces(ctx context.Context, req entity.ListWorkspacesRequest) (*entity.ListWorkspacesResponse, error)
	DeleteWorkspace(ctx context.Context, req entity.DeleteWorkspaceRequest) error
	ArchiveWorkspace(ctx context.Context, req entity.ArchiveWorkspaceRequest) error
	UnarchiveWorkspace(ctx context.Context, req entity.UnarchiveWorkspaceRequest) error
	UpdateWorkspace(ctx context.Context, req entity.UpdateWorkspaceRequest) (*entity.Workspace, error)
	UpdateWorkspaceAutoAllowedTools(ctx context.Context, req entity.UpdateWorkspaceAutoAllowedToolsRequest) error
	GetWorkspaceStats(ctx context.Context, req entity.GetWorkspaceRequest) (*entity.GetWorkspaceStatsResponse, error)
}

// TaskController defines task operations.
type TaskController interface {
	CreateTask(ctx context.Context, req entity.CreateTaskRequest) (*entity.CreateTaskResponse, error)
	GetTask(ctx context.Context, req entity.GetTaskRequest) (*entity.GetTaskResponse, error)
	ListTasks(ctx context.Context, req entity.ListTasksRequest) (*entity.ListTasksResponse, error)
	RespondToTask(ctx context.Context, req entity.RespondToTaskRequest) (*entity.RespondToTaskResponse, error)
	UpdateTaskStatus(ctx context.Context, req entity.UpdateTaskStatusRequest) (*entity.UpdateTaskStatusResponse, error)
	UpdateTaskOrder(ctx context.Context, req entity.UpdateTaskOrderRequest) (*entity.UpdateTaskOrderResponse, error)
	ReplyToTask(ctx context.Context, req entity.ReplyToTaskRequest) (*entity.ReplyToTaskResponse, error)
	DeleteTask(ctx context.Context, req entity.DeleteTaskRequest) (*entity.DeleteTaskResponse, error)
	UpdateMessageMetadata(ctx context.Context, req entity.UpdateMessageMetadataRequest) error
	GetAttachment(ctx context.Context, req entity.GetAttachmentRequest) (*entity.GetAttachmentResponse, error)
	UpdateScheduledTask(ctx context.Context, req entity.UpdateScheduledTaskRequest) (*entity.UpdateScheduledTaskResponse, error)
}
