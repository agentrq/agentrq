package notif

import (
	"context"
	"encoding/json"
	"fmt"

	zlog "github.com/rs/zerolog/log"

	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/memq"
	"github.com/agentrq/agentrq/backend/internal/service/smtp"
	"github.com/mustafaturan/monoflake"
)

type (
	Service interface {
		NotifyTaskCreated(workspace model.Workspace, task model.Task)
		NotifyTaskStatusUpdated(workspace model.Workspace, task model.Task)
		NotifyTaskReceivedMessage(workspace model.Workspace, task model.Task, msg model.Message)
		NotifyWorkspaceArchived(workspace model.Workspace)
		NotifyWorkspaceUnarchived(workspace model.Workspace)
	}

	service struct {
		repo    base.Repository
		memq    memq.Service
		smtp    smtp.Service
		queueID uint32
		baseURL string
	}

	NotificationSettings struct {
		TaskCreated         bool     `json:"task_created"`
		TaskStatusUpdated   bool     `json:"task_status_updated"`
		TaskReceivedMessage bool     `json:"task_received_message"`
		WorkspaceArchived   bool     `json:"workspace_archived"`
		WorkspaceUnarchived bool     `json:"workspace_unarchived"`
		Channels            []string `json:"channels"`
	}

	emailTask struct {
		To      string
		Subject string
		Body    string
	}
)

func New(repo base.Repository, mq memq.Service, s smtp.Service, baseURL string) (Service, error) {
	res, err := mq.Create(context.Background(), memq.CreateRequest{
		Name: "email_notifications",
		Size: 1000,
	})
	if err != nil {
		return nil, err
	}

	s1 := &service{
		repo:    repo,
		memq:    mq,
		smtp:    s,
		queueID: res.ID,
		baseURL: baseURL,
	}

	err = mq.AddWorkers(context.Background(), memq.AddWorkersRequest{
		QueueID: res.ID,
		Count:   2,
		Handle:  s1.handleEmailTask,
	})
	if err != nil {
		return nil, err
	}

	return s1, nil
}

func (s *service) handleEmailTask(ctx context.Context, t memq.Task) error {
	task, ok := t.Val.(emailTask)
	if !ok {
		return fmt.Errorf("invalid email task payload")
	}

	return s.smtp.Send(ctx, smtp.SendRequest{
		To:      []string{task.To},
		Subject: task.Subject,
		Body:    task.Body,
	})
}

func (s *service) NotifyTaskCreated(workspace model.Workspace, task model.Task) {
	settings := s.getSettings(workspace)
	if !settings.TaskCreated || !s.hasChannel(settings, "email") {
		return
	}

	s.enqueueEmail(monoflake.ID(workspace.UserID).String(), fmt.Sprintf("New Task: %s [%s]", task.Title, workspace.Name),
		fmt.Sprintf("Workspace: %s\n\nA new task has been created in your workspace:\n\nTitle: %s\nDetails: %s\n\nView Mission: %s/workspaces/%d",
			workspace.Name, task.Title, task.Body, s.baseURL, workspace.ID))
}

func (s *service) NotifyTaskStatusUpdated(workspace model.Workspace, task model.Task) {
	settings := s.getSettings(workspace)
	if !settings.TaskStatusUpdated || !s.hasChannel(settings, "email") {
		return
	}

	s.enqueueEmail(monoflake.ID(workspace.UserID).String(), fmt.Sprintf("Task Status Updated: %s [%s]", task.Title, workspace.Name),
		fmt.Sprintf("Workspace: %s\n\nThe status of task %s has been updated to: %s.\n\nView Mission: %s/workspaces/%d",
			workspace.Name, task.Title, task.Status, s.baseURL, workspace.ID))
}

func (s *service) NotifyTaskReceivedMessage(workspace model.Workspace, task model.Task, msg model.Message) {
	settings := s.getSettings(workspace)
	if !settings.TaskReceivedMessage || !s.hasChannel(settings, "email") || msg.Sender == "human" {
		return
	}

	s.enqueueEmail(monoflake.ID(workspace.UserID).String(), fmt.Sprintf("New Message in Task: %s [%s]", task.Title, workspace.Name),
		fmt.Sprintf("Workspace: %s\n\nAn agent sent a new message in task %s:\n\n%s\n\nReply to Mission: %s/workspaces/%d",
			workspace.Name, task.Title, msg.Text, s.baseURL, workspace.ID))
}

func (s *service) NotifyWorkspaceArchived(workspace model.Workspace) {
	settings := s.getSettings(workspace)
	if !settings.WorkspaceArchived || !s.hasChannel(settings, "email") {
		return
	}

	s.enqueueEmail(monoflake.ID(workspace.UserID).String(), fmt.Sprintf("Mission Vaulted [%s]", workspace.Name),
		fmt.Sprintf("Workspace: %s\n\nYour workspace has been archived and is now read-only.\n\nGo to Dashboard: %s/",
			workspace.Name, s.baseURL))
}

func (s *service) NotifyWorkspaceUnarchived(workspace model.Workspace) {
	settings := s.getSettings(workspace)
	if !settings.WorkspaceUnarchived || !s.hasChannel(settings, "email") {
		return
	}

	s.enqueueEmail(monoflake.ID(workspace.UserID).String(), fmt.Sprintf("Mission Restored [%s]", workspace.Name),
		fmt.Sprintf("Workspace: %s\n\nYour workspace has been restored and is now active for operations.\n\nView Mission: %s/workspaces/%d",
			workspace.Name, s.baseURL, workspace.ID))
}

func (s *service) getSettings(w model.Workspace) NotificationSettings {
	var ns NotificationSettings
	if len(w.NotificationSettings) > 0 {
		_ = json.Unmarshal(w.NotificationSettings, &ns)
	}
	return ns
}

func (s *service) hasChannel(ns NotificationSettings, channel string) bool {
	for _, c := range ns.Channels {
		if c == channel {
			return true
		}
	}
	return false
}

func (s *service) enqueueEmail(to, subject, body string) {
	err := s.memq.AddTask(context.Background(), memq.AddTaskRequest{
		QueueID: s.queueID,
		Task: memq.Task{
			Val: emailTask{
				To:      to, // assuming UserID is an email for now or needs to be resolved
				Subject: "[AgentRQ] " + subject,
				Body:    body,
			},
		},
	})
	if err != nil {
		zlog.Error().Err(err).Msg("[notif] failed to enqueue email")
	}
}
