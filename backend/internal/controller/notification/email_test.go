// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package notification

import (
	"context"
	"strconv"
	"strings"
	"testing"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/service/memq"
	mock_memq "github.com/agentrq/agentrq/backend/internal/service/mocks/memq"
	mock_repository "github.com/agentrq/agentrq/backend/internal/service/mocks/repository"
	"github.com/golang/mock/gomock"
	"github.com/mustafaturan/monoflake"
)

func TestEmailNotifications(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockRepo := mock_repository.NewMockRepository(ctrl)
	mockMemQ := mock_memq.NewMockService(ctrl)

	c := &controller{
		repo:    mockRepo,
		memq:    mockMemQ,
		queueID: 1,
		baseURL: "http://test.com",
	}

	ws := entity.Workspace{
		ID:     1,
		Name:   "Test Workspace",
		UserID: 100,
		NotificationSettings: &entity.NotificationSettings{
			TaskCreated:         true,
			TaskStatusUpdated:   true,
			TaskReceivedMessage: true,
			WorkspaceArchived:   true,
			WorkspaceUnarchived: true,
			Channels:            []string{"email"},
		},
	}

	task := entity.Task{
		ID:    42,
		Title: "Test Task",
		Body:  "Test Body",
	}

	t.Run("NotifyTaskCreated", func(t *testing.T) {
		mockRepo.EXPECT().SystemGetUser(gomock.Any(), gomock.Any()).Return(model.User{Email: "user@test.com"}, nil)
		mockMemQ.EXPECT().AddTask(gomock.Any(), gomock.Any()).Return(nil)

		c.NotifyTaskCreated(ws, task)
	})

	t.Run("NotifyTaskCreated_Disabled", func(t *testing.T) {
		disabledWS := ws
		disabledWS.NotificationSettings = &entity.NotificationSettings{
			TaskCreated: false,
			Channels:    []string{"email"},
		}

		// Expect NO AddTask call
		c.NotifyTaskCreated(disabledWS, task)
	})

	t.Run("NotifyTaskStatusUpdated", func(t *testing.T) {
		mockRepo.EXPECT().SystemGetUser(gomock.Any(), gomock.Any()).Return(model.User{Email: "user@test.com"}, nil)
		mockMemQ.EXPECT().AddTask(gomock.Any(), gomock.Any()).Return(nil)

		c.NotifyTaskStatusUpdated(ws, task)
	})

	t.Run("NotifyTaskReceivedMessage", func(t *testing.T) {
		msg := entity.Message{
			Text:   "Hello",
			Sender: "agent",
		}
		mockRepo.EXPECT().SystemGetUser(gomock.Any(), gomock.Any()).Return(model.User{Email: "user@test.com"}, nil)
		mockMemQ.EXPECT().AddTask(gomock.Any(), gomock.Any()).Return(nil)

		c.NotifyTaskReceivedMessage(ws, task, msg)
	})

	t.Run("NotifyTaskReceivedMessage_FromHuman", func(t *testing.T) {
		msg := entity.Message{
			Text:   "Hello",
			Sender: "human",
		}
		// Expect NO AddTask call for human-sent messages
		c.NotifyTaskReceivedMessage(ws, task, msg)
	})

	t.Run("NotifyWorkspaceArchived", func(t *testing.T) {
		mockRepo.EXPECT().SystemGetUser(gomock.Any(), gomock.Any()).Return(model.User{Email: "user@test.com"}, nil)
		mockMemQ.EXPECT().AddTask(gomock.Any(), gomock.Any()).Return(nil)

		c.NotifyWorkspaceArchived(ws)
	})

	t.Run("NotifyWorkspaceUnarchived", func(t *testing.T) {
		mockRepo.EXPECT().SystemGetUser(gomock.Any(), gomock.Any()).Return(model.User{Email: "user@test.com"}, nil)
		mockMemQ.EXPECT().AddTask(gomock.Any(), gomock.Any()).Return(nil)

		c.NotifyWorkspaceUnarchived(ws)
	})

	t.Run("EnqueueEmail_UserNotFound", func(t *testing.T) {
		mockRepo.EXPECT().SystemGetUser(gomock.Any(), gomock.Any()).Return(model.User{}, nil)
		mockMemQ.EXPECT().AddTask(gomock.Any(), gomock.Any()).Return(nil)

		c.enqueueEmail("not_an_id", "Sub", "Body")
	})
}

func TestEmailLinksUseBase62IDs(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockRepo := mock_repository.NewMockRepository(ctrl)
	mockMemQ := mock_memq.NewMockService(ctrl)
	c := &controller{repo: mockRepo, memq: mockMemQ, queueID: 1, baseURL: "http://test.com"}

	wsID := int64(1234567890123)
	taskID := int64(9876543210987)
	ws := entity.Workspace{
		ID:     wsID,
		Name:   "WS",
		UserID: 100,
		NotificationSettings: &entity.NotificationSettings{
			TaskCreated:         true,
			TaskStatusUpdated:   true,
			TaskReceivedMessage: true,
			WorkspaceArchived:   true,
			WorkspaceUnarchived: true,
			Channels:            []string{"email"},
		},
	}
	task := entity.Task{ID: taskID, Title: "T"}

	wsURL := "http://test.com/workspaces/" + monoflake.ID(wsID).String()
	taskLink := wsURL + "/tasks/" + monoflake.ID(taskID).String()

	cases := []struct {
		name string
		send func()
		want string
	}{
		{"TaskCreated", func() { c.NotifyTaskCreated(ws, task) }, taskLink},
		{"TaskStatusUpdated", func() { c.NotifyTaskStatusUpdated(ws, task) }, taskLink},
		{"AllowAllCommandsToggled", func() { c.NotifyTaskAllowAllCommandsToggled(ws, task) }, taskLink},
		{"TaskReceivedMessage", func() { c.NotifyTaskReceivedMessage(ws, task, entity.Message{Text: "hi", Sender: "agent"}) }, taskLink},
		{"WorkspaceUnarchived", func() { c.NotifyWorkspaceUnarchived(ws) }, wsURL},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var body string
			mockRepo.EXPECT().SystemGetUser(gomock.Any(), gomock.Any()).Return(model.User{Email: "u@test.com"}, nil)
			mockMemQ.EXPECT().AddTask(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, req memq.AddTaskRequest) error {
				body = req.Task.Val.(emailTask).Body
				return nil
			})

			tc.send()

			if !strings.HasSuffix(body, tc.want) {
				t.Fatalf("body does not end with %q:\n%s", tc.want, body)
			}
			if strings.Contains(body, strconv.FormatInt(wsID, 10)) {
				t.Fatalf("body carries the raw numeric workspace ID:\n%s", body)
			}
		})
	}
}
