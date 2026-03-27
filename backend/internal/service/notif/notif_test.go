package notif

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/hasmcp/agentrq/backend/internal/data/model"
	"github.com/hasmcp/agentrq/backend/internal/service/memq"
	memq_mock "github.com/hasmcp/agentrq/backend/internal/service/mocks/memq"
	"github.com/hasmcp/agentrq/backend/internal/service/mocks/repository"
	smtp_mock "github.com/hasmcp/agentrq/backend/internal/service/mocks/smtp"
)

func TestNotifyTaskCreated(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockRepo := repository.NewMockRepository(ctrl)
	mockMQ := memq_mock.NewMockService(ctrl)
	mockSMTP := smtp_mock.NewMockService(ctrl)

	mqResponse := &memq.CreateResponse{ID: 1}
	mockMQ.EXPECT().Create(gomock.Any(), gomock.Any()).Return(mqResponse, nil)
	mockMQ.EXPECT().AddWorkers(gomock.Any(), gomock.Any()).Return(nil)

	s, err := New(mockRepo, mockMQ, mockSMTP, "http://base.url")
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	workspace := model.Workspace{
		ID:     1,
		UserID: 1,
		Name:   "Test Workspace",
	}
	task := model.Task{
		ID:    1,
		Title: "Test Task",
		Body:  "Test Body",
	}

	t.Run("TaskCreatedEnabled", func(t *testing.T) {
		settings := NotificationSettings{
			TaskCreated: true,
			Channels:    []string{"email"},
		}
		settingsBytes, _ := json.Marshal(settings)
		workspace.NotificationSettings = settingsBytes

		mockMQ.EXPECT().AddTask(gomock.Any(), gomock.Any()).Return(nil)

		s.NotifyTaskCreated(workspace, task)
	})

	t.Run("TaskCreatedDisabled", func(t *testing.T) {
		workspace.NotificationSettings = nil // defaults to empty
		s.NotifyTaskCreated(workspace, task)
	})
}

func TestNotifyTaskStatusUpdated(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockRepo := repository.NewMockRepository(ctrl)
	mockMQ := memq_mock.NewMockService(ctrl)
	mockSMTP := smtp_mock.NewMockService(ctrl)

	mockMQ.EXPECT().Create(gomock.Any(), gomock.Any()).Return(&memq.CreateResponse{ID: 1}, nil)
	mockMQ.EXPECT().AddWorkers(gomock.Any(), gomock.Any()).Return(nil)

	s, _ := New(mockRepo, mockMQ, mockSMTP, "http://base.url")

	workspace := model.Workspace{
		UserID: 1,
	}
	settings := NotificationSettings{
		TaskStatusUpdated: true,
		Channels:          []string{"email"},
	}
	settingsBytes, _ := json.Marshal(settings)
	workspace.NotificationSettings = settingsBytes

	mockMQ.EXPECT().AddTask(gomock.Any(), gomock.Any()).Return(nil)
	s.NotifyTaskStatusUpdated(workspace, model.Task{Title: "T1", Status: "ongoing"})

	t.Run("StatusUpdatedDisabled", func(t *testing.T) {
		workspace.NotificationSettings = []byte(`{"task_status_updated":false}`)
		s.NotifyTaskStatusUpdated(workspace, model.Task{Title: "T1"})
	})

	t.Run("ChannelNotFound", func(t *testing.T) {
		workspace.NotificationSettings = []byte(`{"task_status_updated":true, "channels":["slack"]}`)
		s.NotifyTaskStatusUpdated(workspace, model.Task{Title: "T1"})
	})
}

func TestNotifyTaskReceivedMessage(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockRepo := repository.NewMockRepository(ctrl)
	mockMQ := memq_mock.NewMockService(ctrl)
	mockSMTP := smtp_mock.NewMockService(ctrl)

	mockMQ.EXPECT().Create(gomock.Any(), gomock.Any()).Return(&memq.CreateResponse{ID: 1}, nil)
	mockMQ.EXPECT().AddWorkers(gomock.Any(), gomock.Any()).Return(nil)

	s, _ := New(mockRepo, mockMQ, mockSMTP, "http://base.url")

	workspace := model.Workspace{
		UserID: 1,
	}
	settings := NotificationSettings{
		TaskReceivedMessage: true,
		Channels:            []string{"email"},
	}
	settingsBytes, _ := json.Marshal(settings)
	workspace.NotificationSettings = settingsBytes

	t.Run("AgentMessage", func(t *testing.T) {
		mockMQ.EXPECT().AddTask(gomock.Any(), gomock.Any()).Return(nil)
		s.NotifyTaskReceivedMessage(workspace, model.Task{Title: "T1"}, model.Message{Sender: "agent", Text: "Hello"})
	})

	t.Run("HumanMessage", func(t *testing.T) {
		// Should not enqueue
		s.NotifyTaskReceivedMessage(workspace, model.Task{Title: "T1"}, model.Message{Sender: "human", Text: "Hello"})
	})
}

func TestNotifyWorkspaceArchivedUnarchived(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockRepo := repository.NewMockRepository(ctrl)
	mockMQ := memq_mock.NewMockService(ctrl)
	mockSMTP := smtp_mock.NewMockService(ctrl)

	mockMQ.EXPECT().Create(gomock.Any(), gomock.Any()).Return(&memq.CreateResponse{ID: 1}, nil)
	mockMQ.EXPECT().AddWorkers(gomock.Any(), gomock.Any()).Return(nil)

	s, _ := New(mockRepo, mockMQ, mockSMTP, "http://base.url")

	workspace := model.Workspace{
		UserID: 1,
	}
	settings := NotificationSettings{
		WorkspaceArchived:   true,
		WorkspaceUnarchived: true,
		Channels:            []string{"email"},
	}
	settingsBytes, _ := json.Marshal(settings)
	workspace.NotificationSettings = settingsBytes

	mockMQ.EXPECT().AddTask(gomock.Any(), gomock.Any()).Return(nil).Times(2)
	s.NotifyWorkspaceArchived(workspace)
	s.NotifyWorkspaceUnarchived(workspace)

	t.Run("DisabledCases", func(t *testing.T) {
		settings = NotificationSettings{
			WorkspaceArchived:   false,
			WorkspaceUnarchived: false,
			Channels:            []string{"email"},
		}
		settingsBytes, _ = json.Marshal(settings)
		workspace.NotificationSettings = settingsBytes

		s.NotifyWorkspaceArchived(workspace)
		s.NotifyWorkspaceUnarchived(workspace)
	})
}

func TestNewErrors(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockMQ := memq_mock.NewMockService(ctrl)

	t.Run("MQCreateError", func(t *testing.T) {
		mockMQ.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil, fmt.Errorf("create error"))
		_, err := New(nil, mockMQ, nil, "")
		if err == nil {
			t.Errorf("expected error from New, got nil")
		}
	})

	t.Run("MQAddWorkersError", func(t *testing.T) {
		mockMQ.EXPECT().Create(gomock.Any(), gomock.Any()).Return(&memq.CreateResponse{ID: 1}, nil)
		mockMQ.EXPECT().AddWorkers(gomock.Any(), gomock.Any()).Return(fmt.Errorf("add workers error"))
		_, err := New(nil, mockMQ, nil, "")
		if err == nil {
			t.Errorf("expected error from New, got nil")
		}
	})
}

func TestEnqueueEmailError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockRepo := repository.NewMockRepository(ctrl)
	mockMQ := memq_mock.NewMockService(ctrl)
	mockSMTP := smtp_mock.NewMockService(ctrl)

	mockMQ.EXPECT().Create(gomock.Any(), gomock.Any()).Return(&memq.CreateResponse{ID: 1}, nil)
	mockMQ.EXPECT().AddWorkers(gomock.Any(), gomock.Any()).Return(nil)

	s, _ := New(mockRepo, mockMQ, mockSMTP, "http://base.url")

	workspace := model.Workspace{
		UserID: 1,
	}
	settings := NotificationSettings{
		TaskCreated: true,
		Channels:    []string{"email"},
	}
	settingsBytes, _ := json.Marshal(settings)
	workspace.NotificationSettings = settingsBytes

	// Mocking AddTask to return an error, which should be logged but not crash.
	mockMQ.EXPECT().AddTask(gomock.Any(), gomock.Any()).Return(fmt.Errorf("add task error"))

	s.NotifyTaskCreated(workspace, model.Task{Title: "T1"})
}

func TestHandleEmailTask(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockRepo := repository.NewMockRepository(ctrl)
	mockMQ := memq_mock.NewMockService(ctrl)
	mockSMTP := smtp_mock.NewMockService(ctrl)

	mockMQ.EXPECT().Create(gomock.Any(), gomock.Any()).Return(&memq.CreateResponse{ID: 1}, nil)
	mockMQ.EXPECT().AddWorkers(gomock.Any(), gomock.Any()).Return(nil)

	s, _ := New(mockRepo, mockMQ, mockSMTP, "http://base.url")

	t.Run("ValidTask", func(t *testing.T) {
		emailT := emailTask{
			To:      "to@ex.com",
			Subject: "Sub",
			Body:    "Body",
		}
		mockSMTP.EXPECT().Send(gomock.Any(), gomock.Any()).Return(nil)
		
		err := s.(*service).handleEmailTask(context.Background(), memq.Task{Val: emailT})
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("InvalidTaskType", func(t *testing.T) {
		err := s.(*service).handleEmailTask(context.Background(), memq.Task{Val: "not an email task"})
		if err == nil {
			t.Error("expected error for invalid task type, got nil")
		}
	})
}
