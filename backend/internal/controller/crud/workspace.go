package crud

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	entity "github.com/hasmcp/agentrq/backend/internal/data/entity/crud"
	"github.com/hasmcp/agentrq/backend/internal/data/model"
	"github.com/hasmcp/agentrq/backend/internal/service/security"
	"gorm.io/datatypes"
)

func (c *controller) CreateWorkspace(ctx context.Context, req entity.CreateWorkspaceRequest) (*entity.CreateWorkspaceResponse, error) {
	now := time.Now()
	m := model.Workspace{
		ID:          c.idgen.NextID(),
		CreatedAt:   now,
		UpdatedAt:   now,
		UserID:      req.UserID,
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
		icon, err := c.img.ResizeBase64(req.Workspace.Icon, 32, 32)
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
	c.telemetry.Record(ctx, req.UserID, created.ID, model.ActionIDWorkspaceCreate)
	return &entity.CreateWorkspaceResponse{
		Workspace: fromModelWorkspaceToEntity(created),
	}, nil
}

func (c *controller) GetWorkspace(ctx context.Context, req entity.GetWorkspaceRequest) (*entity.GetWorkspaceResponse, error) {
	m, err := c.repository.GetWorkspace(ctx, req.ID, req.UserID)
	if err != nil {
		return nil, err
	}
	return &entity.GetWorkspaceResponse{Workspace: fromModelWorkspaceToEntity(m)}, nil
}

func (c *controller) ListWorkspaces(ctx context.Context, req entity.ListWorkspacesRequest) (*entity.ListWorkspacesResponse, error) {
	ms, err := c.repository.ListWorkspaces(ctx, req.UserID, req.IncludeArchived)
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
	tasks, err := c.repository.ListTasks(ctx, entity.ListTasksRequest{WorkspaceID: req.ID}, req.UserID)
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
	if err := c.repository.DeleteWorkspace(ctx, req.ID, req.UserID); err != nil {
		return err
	}

	// 3. Purge storage files
	for _, id := range attachmentIDs {
		_ = c.storage.Delete(id)
	}

	c.telemetry.Record(ctx, req.UserID, req.ID, model.ActionIDWorkspaceDelete)
	return nil
}

func (c *controller) ArchiveWorkspace(ctx context.Context, req entity.ArchiveWorkspaceRequest) error {
	m, err := c.repository.GetWorkspace(ctx, req.ID, req.UserID)
	if err != nil {
		return err
	}
	now := time.Now()
	m.ArchivedAt = &now
	updated, err := c.repository.UpdateWorkspace(ctx, m)
	if err == nil {
		c.notif.NotifyWorkspaceArchived(updated)
		c.telemetry.Record(ctx, req.UserID, updated.ID, model.ActionIDWorkspaceUpdate)
	}
	return err
}

func (c *controller) UnarchiveWorkspace(ctx context.Context, req entity.UnarchiveWorkspaceRequest) error {
	m, err := c.repository.GetWorkspace(ctx, req.ID, req.UserID)
	if err != nil {
		return err
	}
	m.ArchivedAt = nil
	updated, err := c.repository.UpdateWorkspace(ctx, m)
	if err == nil {
		c.notif.NotifyWorkspaceUnarchived(updated)
		c.telemetry.Record(ctx, req.UserID, updated.ID, model.ActionIDWorkspaceUpdate)
	}
	return err
}

func (c *controller) UpdateWorkspace(ctx context.Context, req entity.UpdateWorkspaceRequest) (*entity.Workspace, error) {
	m, err := c.repository.GetWorkspace(ctx, req.Workspace.ID, req.UserID)
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
		icon, err := c.img.ResizeBase64(req.Workspace.Icon, 32, 32)
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
	c.telemetry.Record(ctx, req.UserID, updated.ID, model.ActionIDWorkspaceUpdate)
	res := fromModelWorkspaceToEntity(updated)
	return &res, nil
}

func (c *controller) UpdateWorkspaceAutoAllowedTools(ctx context.Context, req entity.UpdateWorkspaceAutoAllowedToolsRequest) error {
	m, err := c.repository.GetWorkspace(ctx, req.WorkspaceID, req.UserID)
	if err != nil {
		return err
	}
	b, _ := json.Marshal(req.Tools)
	m.AutoAllowedTools = datatypes.JSON(b)
	m.UpdatedAt = time.Now()
	_, err = c.repository.UpdateWorkspace(ctx, m)
	if err == nil {
		c.telemetry.Record(ctx, req.UserID, req.WorkspaceID, model.ActionIDWorkspaceUpdate)
	}
	return err
}

func (c *controller) GetWorkspaceStats(ctx context.Context, req entity.GetWorkspaceRequest) (*entity.GetWorkspaceStatsResponse, error) {
	return c.telemetry.GetWorkspaceStats(ctx, req.ID)
}

func fromModelWorkspaceToEntity(m model.Workspace) entity.Workspace {
	res := entity.Workspace{
		ID:          m.ID,
		CreatedAt:   m.CreatedAt,
		UpdatedAt:   m.UpdatedAt,
		UserID:      m.UserID,
		Name:        m.Name,
		Description: m.Description,
		Icon:        m.Icon,
		ArchivedAt:  m.ArchivedAt,
		TokenEncrypted: m.TokenEncrypted,
		TokenNonce:     m.TokenNonce,
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
