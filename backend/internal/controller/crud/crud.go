package crud

import (
	"context"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/idgen"
	"github.com/agentrq/agentrq/backend/internal/service/image"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
	"github.com/agentrq/agentrq/backend/internal/service/storage"
)

type (
	Params struct {
		IDGen      idgen.Service
		Repository base.Repository
		Storage    storage.Service
		Image      image.Service
		PubSub     pubsub.Service
		TokenKey   string
	}

	Controller interface {
		WorkspaceController
		UserController
		TaskController
	}

	controller struct {
		idgen      idgen.Service
		repository base.Repository
		storage    storage.Service
		image      image.Service
		pubsub     pubsub.Service
		tokenKey   string
	}
)

func New(p Params) Controller {
	return &controller{
		idgen:      p.IDGen,
		repository: p.Repository,
		storage:    p.Storage,
		image:      p.Image,
		pubsub:     p.PubSub,
		tokenKey:   p.TokenKey,
	}
}

func (c *controller) emitEvent(ctx context.Context, e entity.CRUDEvent) {
	if c.pubsub == nil {
		return
	}
	_, _ = c.pubsub.Publish(ctx, pubsub.PublishRequest{
		PubSubID: 0,
		Event:    e,
	})
}

// WorkspaceController defines workspace operations.
type WorkspaceController interface {
	CreateWorkspace(ctx context.Context, req entity.CreateWorkspaceRequest) (*entity.CreateWorkspaceResponse, error)
	DeleteWorkspace(ctx context.Context, req entity.DeleteWorkspaceRequest) error
	GetWorkspace(ctx context.Context, req entity.GetWorkspaceRequest) (*entity.GetWorkspaceResponse, error)
	ListWorkspaces(ctx context.Context, req entity.ListWorkspacesRequest) (*entity.ListWorkspacesResponse, error)
	ArchiveWorkspace(ctx context.Context, req entity.ArchiveWorkspaceRequest) error
	UnarchiveWorkspace(ctx context.Context, req entity.UnarchiveWorkspaceRequest) error
	UpdateWorkspace(ctx context.Context, req entity.UpdateWorkspaceRequest) (*entity.UpdateWorkspaceResponse, error)
	UpdateWorkspaceAutoAllowedTools(ctx context.Context, req entity.UpdateWorkspaceAutoAllowedToolsRequest) error
	GetWorkspaceStats(ctx context.Context, req entity.GetWorkspaceRequest) (*entity.GetWorkspaceStatsResponse, error)
}

// UserController defines user operations.
type UserController interface {
	CreateUser(ctx context.Context, u entity.User) (entity.User, error)
	FindUserByEmail(ctx context.Context, email string) (entity.User, error)
	FindOrCreateUser(ctx context.Context, req entity.FindOrCreateUserRequest) (*entity.FindOrCreateUserResponse, error)
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
	UpdateScheduledTask(ctx context.Context, req entity.UpdateScheduledTaskRequest) (*entity.UpdateScheduledTaskResponse, error)
	UpdateMessageMetadata(ctx context.Context, req entity.UpdateMessageMetadataRequest) error
	DeleteTask(ctx context.Context, req entity.DeleteTaskRequest) (*entity.DeleteTaskResponse, error)
	GetAttachment(ctx context.Context, req entity.GetAttachmentRequest) (*entity.GetAttachmentResponse, error)
}
