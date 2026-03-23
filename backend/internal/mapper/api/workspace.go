package api

import (
	"encoding/json"

	"github.com/gofiber/fiber/v2"
	entity "github.com/hasmcp/agentrq/backend/internal/data/entity/crud"
	view "github.com/hasmcp/agentrq/backend/internal/data/view/api"
	"github.com/mustafaturan/monoflake"
)

func FromHTTPRequestToCreateWorkspaceRequestEntity(c *fiber.Ctx) *entity.CreateWorkspaceRequest {
	var payload view.CreateWorkspaceRequest
	if err := json.Unmarshal(c.BodyRaw(), &payload); err != nil {
		return nil
	}
	if payload.Workspace.Name == "" {
		return nil
	}
	return &entity.CreateWorkspaceRequest{
		Workspace: entity.Workspace{
			Name:        payload.Workspace.Name,
			Description: payload.Workspace.Description,
			Icon:        payload.Workspace.Icon,
			NotificationSettings: fromViewNotificationSettingsToEntity(payload.Workspace.NotificationSettings),
		},
	}
}

func FromCreateWorkspaceResponseEntityToHTTPResponse(rs *entity.CreateWorkspaceResponse, mcpURL string) []byte {
	payload, _ := json.Marshal(view.CreateWorkspaceResponse{
		Workspace: fromEntityWorkspaceToView(rs.Workspace, mcpURL),
	})
	return payload
}

func FromGetWorkspaceResponseEntityToHTTPResponse(rs *entity.GetWorkspaceResponse, mcpURL string) []byte {
	payload, _ := json.Marshal(view.GetWorkspaceResponse{
		Workspace: fromEntityWorkspaceToView(rs.Workspace, mcpURL),
	})
	return payload
}

func FromListWorkspacesResponseEntityToHTTPResponse(rs *entity.ListWorkspacesResponse, mcpURLFn func(int64) string) []byte {
	workspaces := make([]view.Workspace, len(rs.Workspaces))
	for i, p := range rs.Workspaces {
		workspaces[i] = fromEntityWorkspaceToView(p, mcpURLFn(p.ID))
	}
	payload, _ := json.Marshal(view.ListWorkspacesResponse{Workspaces: workspaces})
	return payload
}

func FromHTTPRequestToGetWorkspaceRequestEntity(c *fiber.Ctx) *entity.GetWorkspaceRequest {
	id := monoflake.IDFromBase62(c.Params("id")).Int64()
	if id == 0 {
		return nil
	}
	return &entity.GetWorkspaceRequest{ID: id}
}

func FromHTTPRequestToDeleteWorkspaceRequestEntity(c *fiber.Ctx) *entity.DeleteWorkspaceRequest {
	id := monoflake.IDFromBase62(c.Params("id")).Int64()
	if id == 0 {
		return nil
	}
	return &entity.DeleteWorkspaceRequest{ID: id}
}

func FromHTTPRequestToUpdateWorkspaceRequestEntity(c *fiber.Ctx) *entity.UpdateWorkspaceRequest {
	id := monoflake.IDFromBase62(c.Params("id")).Int64()
	if id == 0 {
		return nil
	}
	var payload view.UpdateWorkspaceRequest
	if err := json.Unmarshal(c.BodyRaw(), &payload); err != nil {
		return nil
	}
	return &entity.UpdateWorkspaceRequest{
		Workspace: entity.Workspace{
			ID:                   id,
			Name:                 payload.Workspace.Name,
			Description:          payload.Workspace.Description,
			Icon:                 payload.Workspace.Icon,
			NotificationSettings: fromViewNotificationSettingsToEntity(payload.Workspace.NotificationSettings),
			AutoAllowedTools:     payload.Workspace.AutoAllowedTools,
		},
	}
}

func FromUpdateWorkspaceResponseEntityToHTTPResponse(rs *entity.Workspace, mcpURL string) []byte {
	payload, _ := json.Marshal(view.GetWorkspaceResponse{
		Workspace: fromEntityWorkspaceToView(*rs, mcpURL),
	})
	return payload
}

func fromEntityWorkspaceToView(p entity.Workspace, mcpURL string) view.Workspace {
	return view.Workspace{
		ID:          monoflake.ID(p.ID).String(),
		CreatedAt:   p.CreatedAt,
		UpdatedAt:   p.UpdatedAt,
		Name:        p.Name,
		Description:          p.Description,
		Icon:                 p.Icon,
		ArchivedAt:           p.ArchivedAt,
		NotificationSettings: fromEntityNotificationSettingsToView(p.NotificationSettings),
		AgentConnected:       p.AgentConnected,
		MCPURL:               mcpURL,
		AutoAllowedTools:     p.AutoAllowedTools,
	}
}

func fromEntityNotificationSettingsToView(p *entity.NotificationSettings) *view.NotificationSettings {
	if p == nil {
		return nil
	}
	return &view.NotificationSettings{
		TaskCreated:         p.TaskCreated,
		TaskStatusUpdated:   p.TaskStatusUpdated,
		TaskReceivedMessage: p.TaskReceivedMessage,
		WorkspaceArchived:   p.WorkspaceArchived,
		WorkspaceUnarchived: p.WorkspaceUnarchived,
		Channels:            p.Channels,
	}
}

func fromViewNotificationSettingsToEntity(p *view.NotificationSettings) *entity.NotificationSettings {
	if p == nil {
		return nil
	}
	return &entity.NotificationSettings{
		TaskCreated:         p.TaskCreated,
		TaskStatusUpdated:   p.TaskStatusUpdated,
		TaskReceivedMessage: p.TaskReceivedMessage,
		WorkspaceArchived:   p.WorkspaceArchived,
		WorkspaceUnarchived: p.WorkspaceUnarchived,
		Channels:            p.Channels,
	}
}
