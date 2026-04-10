package notification

import (
	"context"
	"encoding/json"
	"fmt"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/memq"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
	"github.com/agentrq/agentrq/backend/internal/service/smtp"
	zlog "github.com/rs/zerolog/log"
)

type (
	Params struct {
		Repository base.Repository
		PubSub     pubsub.Service
		MemQ       memq.Service
		SMTP       smtp.Service
		BaseURL    string
	}

	Controller interface {
		Start(ctx context.Context) error
	}

	controller struct {
		repo    base.Repository
		pubsub  pubsub.Service
		memq    memq.Service
		smtp    smtp.Service
		baseURL string
		queueID uint32
	}
)

func New(p Params) (Controller, error) {
	// Create email queue
	res, err := p.MemQ.Create(context.Background(), memq.CreateRequest{
		Name: "email_notifications",
		Size: 1000,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create email queue: %w", err)
	}

	c := &controller{
		repo:    p.Repository,
		pubsub:  p.PubSub,
		memq:    p.MemQ,
		smtp:    p.SMTP,
		baseURL: p.BaseURL,
		queueID: res.ID,
	}

	// Add workers for email queue
	err = p.MemQ.AddWorkers(context.Background(), memq.AddWorkersRequest{
		QueueID: res.ID,
		Count:   2,
		Handle:  c.handleEmailTask,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to add email workers: %w", err)
	}

	return c, nil
}

func (c *controller) Start(ctx context.Context) error {
	res, err := c.pubsub.Subscribe(ctx, pubsub.SubscribeRequest{PubSubID: entity.PubSubTopicCRUD})
	if err != nil {
		return fmt.Errorf("failed to subscribe to global topic: %w", err)
	}

	zlog.Info().Msg("[notification] started controller")

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-res.Events:
				if !ok {
					zlog.Warn().Msg("[notification] pubsub channel closed")
					return
				}

				event, ok := msg.(entity.CRUDEvent)
				if !ok {
					zlog.Error().Msg("[notification] received invalid event type")
					continue
				}

				c.processEvent(ctx, event)
			}
		}
	}()

	return nil
}

func (c *controller) processEvent(ctx context.Context, event entity.CRUDEvent) {
	switch event.ResourceType {
	case entity.ResourceTask:
		c.handleTaskEvent(ctx, event)
	case entity.ResourceWorkspace:
		c.handleWorkspaceEvent(ctx, event)
	case entity.ResourceMessage:
		c.handleMessageEvent(ctx, event)
	}
}

func (c *controller) handleTaskEvent(ctx context.Context, event entity.CRUDEvent) {
	t, err := c.repo.SystemGetTask(ctx, event.ResourceID)
	if err != nil {
		zlog.Error().Err(err).Int64("taskID", event.ResourceID).Msg("[notification] failed to get task")
		return
	}
	w, err := c.repo.SystemGetWorkspace(ctx, t.WorkspaceID)
	if err != nil {
		zlog.Error().Err(err).Int64("workspaceID", t.WorkspaceID).Msg("[notification] failed to get workspace")
		return
	}

	workspace := c.fromModelWorkspaceToEntity(w)
	task := c.fromModelTaskToEntity(t)

	switch event.Action {
	case entity.ActionTaskCreate:
		c.NotifyTaskCreated(workspace, task)
	case entity.ActionTaskUpdate, entity.ActionTaskComplete:
		c.NotifyTaskStatusUpdated(workspace, task)
	case entity.ActionTaskAllowAllCommandsToggle:
		c.NotifyTaskAllowAllCommandsToggled(workspace, task)
	}
}

func (c *controller) handleWorkspaceEvent(ctx context.Context, event entity.CRUDEvent) {
	w, err := c.repo.SystemGetWorkspace(ctx, event.ResourceID)
	if err != nil {
		zlog.Error().Err(err).Int64("workspaceID", event.ResourceID).Msg("[notification] failed to get workspace")
		return
	}

	workspace := c.fromModelWorkspaceToEntity(w)

	switch event.Action {
	case entity.ActionWorkspaceUpdate:
		if workspace.ArchivedAt != nil {
			c.NotifyWorkspaceArchived(workspace)
		} else {
			c.NotifyWorkspaceUnarchived(workspace)
		}
	}
}

func (c *controller) handleMessageEvent(ctx context.Context, event entity.CRUDEvent) {
	m, err := c.repo.SystemGetMessage(ctx, event.ResourceID)
	if err != nil {
		zlog.Error().Err(err).Int64("messageID", event.ResourceID).Msg("[notification] failed to get message")
		return
	}
	t, err := c.repo.SystemGetTask(ctx, m.TaskID)
	if err != nil {
		zlog.Error().Err(err).Int64("taskID", m.TaskID).Msg("[notification] failed to get task")
		return
	}
	w, err := c.repo.SystemGetWorkspace(ctx, t.WorkspaceID)
	if err != nil {
		zlog.Error().Err(err).Int64("workspaceID", t.WorkspaceID).Msg("[notification] failed to get workspace")
		return
	}

	workspace := c.fromModelWorkspaceToEntity(w)
	task := c.fromModelTaskToEntity(t)
	message := c.fromModelMessageToEntity(m)

	switch event.Action {
	case entity.ActionMessageCreate:
		c.NotifyTaskReceivedMessage(workspace, task, message)
	}
}

// Mappers (Internal)

func (c *controller) fromModelWorkspaceToEntity(m model.Workspace) entity.Workspace {
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

func (c *controller) fromModelTaskToEntity(m model.Task) entity.Task {
	var atts []entity.Attachment
	if len(m.Attachments) > 0 {
		_ = json.Unmarshal(m.Attachments, &atts)
	}

	return entity.Task{
		ID:           m.ID,
		CreatedAt:    m.CreatedAt,
		UpdatedAt:    m.UpdatedAt,
		WorkspaceID:  m.WorkspaceID,
		UserID:       m.UserID,
		CreatedBy:    m.CreatedBy,
		Assignee:     m.Assignee,
		Status:       m.Status,
		Title:        m.Title,
		Body:         m.Body,
		Response:     m.Response,
		ReplyText:    m.ReplyText,
		Attachments:  atts,
		CronSchedule: m.CronSchedule,
		ParentID:     m.ParentID,
		SortOrder:    m.SortOrder,
		AllowAllCommands: m.AllowAllCommands,
	}
}

func (c *controller) fromModelMessageToEntity(m model.Message) entity.Message {
	var atts []entity.Attachment
	if len(m.Attachments) > 0 {
		_ = json.Unmarshal(m.Attachments, &atts)
	}
	var metadata any
	if len(m.Metadata) > 0 {
		_ = json.Unmarshal(m.Metadata, &metadata)
	}
	return entity.Message{
		ID:          m.ID,
		CreatedAt:   m.CreatedAt,
		TaskID:      m.TaskID,
		UserID:      m.UserID,
		Sender:      m.Sender,
		Text:        m.Text,
		Attachments: atts,
		Metadata:    metadata,
	}
}
