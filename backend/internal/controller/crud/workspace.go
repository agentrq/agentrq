package crud

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/service/security"
	"github.com/mustafaturan/monoflake"
	"gorm.io/datatypes"
)

func (c *controller) CreateWorkspace(ctx context.Context, req entity.CreateWorkspaceRequest) (*entity.CreateWorkspaceResponse, error) {
	now := time.Now()
	m := model.Workspace{
		ID:          c.idgen.NextID(),
		CreatedAt:   now,
		UpdatedAt:   now,
		UserID:      monoflake.IDFromBase62(req.UserID).Int64(),
		Name:        req.Workspace.Name,
		Description: req.Workspace.Description,
	}

	// Generate and encrypt token for new workspace
	token, _ := security.GenerateSecret(16)
	if token != "" && c.tokenKey != "" {
		enc, nonce, err := security.Encrypt(token, c.tokenKey)
		if err == nil {
			m.TokenEncrypted = enc
			m.TokenNonce = nonce
		}
	}

	if req.Workspace.NotificationSettings != nil {
		b, _ := json.Marshal(req.Workspace.NotificationSettings)
		m.NotificationSettings = datatypes.JSON(b)
	}
	if req.Workspace.Icon != "" {
		icon, err := c.image.ResizeBase64(req.Workspace.Icon, 32, 32)
		if err == nil {
			m.Icon = icon
		} else {
			// Fallback to original if resize fails (maybe not base64)
			m.Icon = req.Workspace.Icon
		}
	}
	created, err := c.repository.CreateWorkspace(ctx, m)
	if err != nil {
		return nil, fmt.Errorf("create workspace: %w", err)
	}
	c.emitEvent(ctx, entity.CRUDEvent{
		Action:       entity.ActionWorkspaceCreate,
		WorkspaceID:  created.ID,
		UserID:       created.UserID,
		ResourceType: entity.ResourceWorkspace,
		ResourceID:   created.ID,
		Actor:        entity.ActorHuman,
	})
	return &entity.CreateWorkspaceResponse{
		Workspace: fromModelWorkspaceToEntity(created),
	}, nil
}

func (c *controller) GetWorkspace(ctx context.Context, req entity.GetWorkspaceRequest) (*entity.GetWorkspaceResponse, error) {
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	m, err := c.repository.GetWorkspace(ctx, req.ID, uid)
	if err != nil {
		return nil, err
	}
	return &entity.GetWorkspaceResponse{Workspace: fromModelWorkspaceToEntity(m)}, nil
}

func (c *controller) ListWorkspaces(ctx context.Context, req entity.ListWorkspacesRequest) (*entity.ListWorkspacesResponse, error) {
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	ms, err := c.repository.ListWorkspaces(ctx, uid, req.IncludeArchived)
	if err != nil {
		return nil, err
	}
	workspaces := make([]entity.Workspace, len(ms))
	for i, m := range ms {
		workspaces[i] = fromModelWorkspaceToEntity(m)
	}
	return &entity.ListWorkspacesResponse{Workspaces: workspaces}, nil
}

func (c *controller) DeleteWorkspace(ctx context.Context, req entity.DeleteWorkspaceRequest) error {
	// 1. Get all tasks for this workspace to collect attachment IDs
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	tasks, err := c.repository.ListTasks(ctx, entity.ListTasksRequest{WorkspaceID: req.ID}, uid)
	if err != nil {
		// If workspace doesn't exist etc, repository delete will handle it below
	}

	var attachmentIDs []string
	for _, t := range tasks {
		var atts []entity.Attachment
		if len(t.Attachments) > 0 {
			if err := json.Unmarshal(t.Attachments, &atts); err == nil {
				for _, a := range atts {
					if a.ID != "" {
						attachmentIDs = append(attachmentIDs, a.ID)
					}
				}
			}
		}
		for _, m := range t.Messages {
			var mAtts []entity.Attachment
			if len(m.Attachments) > 0 {
				if err := json.Unmarshal(m.Attachments, &mAtts); err == nil {
					for _, a := range mAtts {
						if a.ID != "" {
							attachmentIDs = append(attachmentIDs, a.ID)
						}
					}
				}
			}
		}
	}

	// 2. Delete from DB (repository handles cascaded DB delete)
	if err := c.repository.DeleteWorkspace(ctx, req.ID, uid); err != nil {
		return err
	}
	c.emitEvent(ctx, entity.CRUDEvent{
		Action:       entity.ActionWorkspaceDelete,
		WorkspaceID:  req.ID,
		UserID:       uid,
		ResourceType: entity.ResourceWorkspace,
		ResourceID:   req.ID,
		Actor:        entity.ActorHuman,
	})

	// 3. Purge storage files
	for _, id := range attachmentIDs {
		_ = c.storage.Delete(id)
	}

	return nil
}

func (c *controller) ArchiveWorkspace(ctx context.Context, req entity.ArchiveWorkspaceRequest) error {
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	m, err := c.repository.GetWorkspace(ctx, req.ID, uid)
	if err != nil {
		return err
	}
	now := time.Now()
	m.ArchivedAt = &now
	updated, err := c.repository.UpdateWorkspace(ctx, m)
	if err == nil {
		c.emitEvent(ctx, entity.CRUDEvent{
			Action:       entity.ActionWorkspaceUpdate,
			WorkspaceID:  updated.ID,
			UserID:       updated.UserID,
			ResourceType: entity.ResourceWorkspace,
			ResourceID:   updated.ID,
			Actor:        entity.ActorHuman,
		})
	}
	return err
}

func (c *controller) UnarchiveWorkspace(ctx context.Context, req entity.UnarchiveWorkspaceRequest) error {
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	m, err := c.repository.GetWorkspace(ctx, req.ID, uid)
	if err != nil {
		return err
	}
	m.ArchivedAt = nil
	updated, err := c.repository.UpdateWorkspace(ctx, m)
	if err == nil {
		c.emitEvent(ctx, entity.CRUDEvent{
			Action:       entity.ActionWorkspaceUpdate,
			WorkspaceID:  updated.ID,
			UserID:       updated.UserID,
			ResourceType: entity.ResourceWorkspace,
			ResourceID:   updated.ID,
			Actor:        entity.ActorHuman,
		})
	}
	return err
}

func (c *controller) UpdateWorkspace(ctx context.Context, req entity.UpdateWorkspaceRequest) (*entity.UpdateWorkspaceResponse, error) {
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	m, err := c.repository.GetWorkspace(ctx, req.Workspace.ID, uid)
	if err != nil {
		return nil, err
	}
	if m.ArchivedAt != nil {
		return nil, fmt.Errorf("cannot update archived workspace")
	}

	m.Name = req.Workspace.Name
	m.Description = req.Workspace.Description
	if req.Workspace.NotificationSettings != nil {
		b, _ := json.Marshal(req.Workspace.NotificationSettings)
		m.NotificationSettings = datatypes.JSON(b)
	}
	if req.Workspace.AutoAllowedTools != nil {
		b, _ := json.Marshal(req.Workspace.AutoAllowedTools)
		m.AutoAllowedTools = datatypes.JSON(b)
	}
	if req.Workspace.Icon != "" {
		icon, err := c.image.ResizeBase64(req.Workspace.Icon, 32, 32)
		if err == nil {
			m.Icon = icon
		} else {
			m.Icon = req.Workspace.Icon
		}
	}
	m.UpdatedAt = time.Now()

	updated, err := c.repository.UpdateWorkspace(ctx, m)
	if err != nil {
		return nil, err
	}
	c.emitEvent(ctx, entity.CRUDEvent{
		Action:       entity.ActionWorkspaceUpdate,
		WorkspaceID:  updated.ID,
		UserID:       updated.UserID,
		ResourceType: entity.ResourceWorkspace,
		ResourceID:   updated.ID,
		Actor:        entity.ActorHuman,
	})
	return &entity.UpdateWorkspaceResponse{
		Workspace: fromModelWorkspaceToEntity(updated),
	}, nil
}

func (c *controller) UpdateWorkspaceAutoAllowedTools(ctx context.Context, req entity.UpdateWorkspaceAutoAllowedToolsRequest) error {
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	m, err := c.repository.GetWorkspace(ctx, req.WorkspaceID, uid)
	if err != nil {
		return err
	}
	b, _ := json.Marshal(req.Tools)
	m.AutoAllowedTools = datatypes.JSON(b)
	m.UpdatedAt = time.Now()
	_, err = c.repository.UpdateWorkspace(ctx, m)
	if err == nil {
		c.emitEvent(ctx, entity.CRUDEvent{
			Action:       entity.ActionWorkspaceUpdate,
			WorkspaceID:  req.WorkspaceID,
			UserID:       uid,
			ResourceType: entity.ResourceWorkspace,
			ResourceID:   req.WorkspaceID,
			Actor:        entity.ActorHuman,
		})
	}
	return err
}

func (c *controller) GetWorkspaceStats(ctx context.Context, req entity.GetWorkspaceRequest) (*entity.GetWorkspaceStatsResponse, error) {
	stats, err := c.repository.GetDailyStats(ctx, req.ID, 30)
	if err != nil {
		return nil, err
	}

	active, total, err := c.repository.GetWorkspaceTaskCounts(ctx, req.ID)
	if err != nil {
		return nil, err
	}

	return &entity.GetWorkspaceStatsResponse{
		Stats:       stats,
		Total:       0, // Aggregated from stats if needed
		ActiveTasks: active,
		TotalTasks:  total,
	}, nil
}

func fromModelWorkspaceToEntity(m model.Workspace) entity.Workspace {
	res := entity.Workspace{
		ID:               m.ID,
		CreatedAt:        m.CreatedAt,
		UpdatedAt:        m.UpdatedAt,
		UserID:           m.UserID,
		Name:             m.Name,
		Description:      m.Description,
		Icon:             m.Icon,
		ArchivedAt:       m.ArchivedAt,
		TokenEncrypted:   m.TokenEncrypted,
		TokenNonce:       m.TokenNonce,
		AutoAllowedTools: make([]string, 0),
	}
	if len(m.AutoAllowedTools) > 0 {
		_ = json.Unmarshal(m.AutoAllowedTools, &res.AutoAllowedTools)
	}
	if len(m.NotificationSettings) > 0 {
		var ns entity.NotificationSettings
		if err := json.Unmarshal(m.NotificationSettings, &ns); err == nil {
			res.NotificationSettings = &ns
		}
	}
	return res
}
