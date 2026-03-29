package notification

import (
	"context"
	"fmt"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/service/memq"
	"github.com/agentrq/agentrq/backend/internal/service/smtp"
	zlog "github.com/rs/zerolog/log"
	"github.com/mustafaturan/monoflake"
)

type emailTask struct {
	To      string
	Subject string
	Body    string
}

func (c *controller) handleEmailTask(ctx context.Context, t memq.Task) error {
	task, ok := t.Val.(emailTask)
	if !ok {
		return fmt.Errorf("invalid email task payload")
	}

	return c.smtp.Send(ctx, smtp.SendRequest{
		To:      []string{task.To},
		Subject: task.Subject,
		Body:    task.Body,
	})
}

func (c *controller) NotifyTaskCreated(workspace entity.Workspace, task entity.Task) {
	settings := workspace.NotificationSettings
	if settings == nil || !settings.TaskCreated || !c.hasChannel(settings, "email") {
		return
	}

	c.enqueueEmail(monoflake.ID(workspace.UserID).String(), fmt.Sprintf("New Task: %s [%s]", task.Title, workspace.Name),
		fmt.Sprintf("Workspace: %s\n\nA new task has been created in your workspace:\n\nTitle: %s\nDetails: %s\n\nView Mission: %s/workspaces/%d",
			workspace.Name, task.Title, task.Body, c.baseURL, workspace.ID))
}

func (c *controller) NotifyTaskStatusUpdated(workspace entity.Workspace, task entity.Task) {
	settings := workspace.NotificationSettings
	if settings == nil || !settings.TaskStatusUpdated || !c.hasChannel(settings, "email") {
		return
	}

	c.enqueueEmail(monoflake.ID(workspace.UserID).String(), fmt.Sprintf("Task Status Updated: %s [%s]", task.Title, workspace.Name),
		fmt.Sprintf("Workspace: %s\n\nThe status of task %s has been updated to: %s.\n\nView Mission: %s/workspaces/%d",
			workspace.Name, task.Title, task.Status, c.baseURL, workspace.ID))
}

func (c *controller) NotifyTaskReceivedMessage(workspace entity.Workspace, task entity.Task, msg entity.Message) {
	settings := workspace.NotificationSettings
	if settings == nil || !settings.TaskReceivedMessage || !c.hasChannel(settings, "email") || msg.Sender == "human" {
		return
	}

	c.enqueueEmail(monoflake.ID(workspace.UserID).String(), fmt.Sprintf("New Message in Task: %s [%s]", task.Title, workspace.Name),
		fmt.Sprintf("Workspace: %s\n\nAn agent sent a new message in task %s:\n\n%s\n\nReply to Mission: %s/workspaces/%d",
			workspace.Name, task.Title, msg.Text, c.baseURL, workspace.ID))
}

func (c *controller) NotifyWorkspaceArchived(workspace entity.Workspace) {
	settings := workspace.NotificationSettings
	if settings == nil || !settings.WorkspaceArchived || !c.hasChannel(settings, "email") {
		return
	}

	c.enqueueEmail(monoflake.ID(workspace.UserID).String(), fmt.Sprintf("Mission Vaulted [%s]", workspace.Name),
		fmt.Sprintf("Workspace: %s\n\nYour workspace has been archived and is now read-only.\n\nGo to Dashboard: %s/",
			workspace.Name, c.baseURL))
}

func (c *controller) NotifyWorkspaceUnarchived(workspace entity.Workspace) {
	settings := workspace.NotificationSettings
	if settings == nil || !settings.WorkspaceUnarchived || !c.hasChannel(settings, "email") {
		return
	}

	c.enqueueEmail(monoflake.ID(workspace.UserID).String(), fmt.Sprintf("Mission Restored [%s]", workspace.Name),
		fmt.Sprintf("Workspace: %s\n\nYour workspace has been restored and is now active for operations.\n\nView Mission: %s/workspaces/%d",
			workspace.Name, c.baseURL, workspace.ID))
}

func (c *controller) hasChannel(ns *entity.NotificationSettings, channel string) bool {
	if ns == nil {
		return false
	}
	for _, c := range ns.Channels {
		if c == channel {
			return true
		}
	}
	return false
}

func (c *controller) enqueueEmail(to, subject, body string) {
	// Resolve user email if 'to' is a base62 ID
	email := to
	id := monoflake.IDFromBase62(to).Int64()
	if id != 0 {
		u, err := c.repo.SystemGetUser(context.Background(), id)
		if err == nil && u.Email != "" {
			email = u.Email
		}
	}

	err := c.memq.AddTask(context.Background(), memq.AddTaskRequest{
		QueueID: c.queueID,
		Task: memq.Task{
			Val: emailTask{
				To:      email,
				Subject: "[AgentRQ] " + subject,
				Body:    body,
			},
		},
	})
	if err != nil {
		zlog.Error().Err(err).Msg("[notification] failed to enqueue email")
	}
}
