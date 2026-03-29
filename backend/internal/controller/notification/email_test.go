package notification

import (
	"testing"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	mock_memq "github.com/agentrq/agentrq/backend/internal/service/mocks/memq"
	mock_repository "github.com/agentrq/agentrq/backend/internal/service/mocks/repository"
	"github.com/golang/mock/gomock"
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
			TaskCreated:          true,
			TaskStatusUpdated:    true,
			TaskReceivedMessage: true,
			WorkspaceArchived:    true,
			WorkspaceUnarchived:   true,
			Channels:             []string{"email"},
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
