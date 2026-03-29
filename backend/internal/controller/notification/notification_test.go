package notification

import (
	"context"
	"testing"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	mock_memq "github.com/agentrq/agentrq/backend/internal/service/mocks/memq"
	mock_pubsub "github.com/agentrq/agentrq/backend/internal/service/mocks/pubsub"
	mock_repository "github.com/agentrq/agentrq/backend/internal/service/mocks/repository"
	mock_smtp "github.com/agentrq/agentrq/backend/internal/service/mocks/smtp"
	"github.com/agentrq/agentrq/backend/internal/service/memq"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
	"github.com/golang/mock/gomock"
)

func TestNotificationController(t *testing.T) {
	t.Run("NewAndStart", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockRepo := mock_repository.NewMockRepository(ctrl)
		mockPubSub := mock_pubsub.NewMockService(ctrl)
		mockMemQ := mock_memq.NewMockService(ctrl)
		mockSMTP := mock_smtp.NewMockService(ctrl)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// New expectation
		mockMemQ.EXPECT().Create(gomock.Any(), gomock.Any()).Return(&memq.CreateResponse{ID: 1}, nil)
		mockMemQ.EXPECT().AddWorkers(gomock.Any(), gomock.Any()).Return(nil)

		c, err := New(Params{
			Repository: mockRepo,
			PubSub:     mockPubSub,
			MemQ:       mockMemQ,
			SMTP:       mockSMTP,
			BaseURL:    "http://test.com",
		})
		if err != nil {
			t.Fatalf("failed to create controller: %v", err)
		}

		// Start expectation
		eventsChan := make(chan any, 10)
		mockPubSub.EXPECT().Subscribe(gomock.Any(), pubsub.SubscribeRequest{PubSubID: 0}).Return(&pubsub.SubscribeResponse{Events: eventsChan}, nil)

		if err := c.Start(ctx); err != nil {
			t.Fatalf("failed to start: %v", err)
		}

		// Inject event
		taskID := int64(42)
		wsID := int64(1)
		mockRepo.EXPECT().SystemGetTask(gomock.Any(), taskID).Return(model.Task{ID: taskID, WorkspaceID: wsID, Title: "Test Task"}, nil)
		
		ws := model.Workspace{
			ID:                   wsID,
			Name:                 "Test WS",
			UserID:               1,
			NotificationSettings: []byte(`{"taskCreated":true,"channels":["email"]}`),
		}
		mockRepo.EXPECT().SystemGetWorkspace(gomock.Any(), wsID).Return(ws, nil)
		mockRepo.EXPECT().SystemGetUser(gomock.Any(), int64(1)).Return(model.User{ID: 1, Email: "user@test.com", Name: "Test User"}, nil)
		
		// ExpectAddTask
		mockMemQ.EXPECT().AddTask(gomock.Any(), gomock.Any()).Return(nil)

		eventsChan <- entity.CRUDEvent{
			Action:       entity.ActionTaskCreate,
			ResourceType: entity.ResourceTask,
			ResourceID:   taskID,
			WorkspaceID:  wsID,
		}

		// Wait briefly
		time.Sleep(200 * time.Millisecond)
	})

	t.Run("HandleEmailTask", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockMemQ := mock_memq.NewMockService(ctrl)
		mockSMTP := mock_smtp.NewMockService(ctrl)

		mockMemQ.EXPECT().Create(gomock.Any(), gomock.Any()).Return(&memq.CreateResponse{ID: 1}, nil)
		mockMemQ.EXPECT().AddWorkers(gomock.Any(), gomock.Any()).Return(nil)

		c, _ := New(Params{MemQ: mockMemQ, SMTP: mockSMTP})

		// Test handleEmailTask directly via the interface if possible, or Mocking MemQ to call it
		// But handleEmailTask is a private method passed to AddWorkers.
		// We can test it by calling it directly since we are in the same package.
		
		emailTask := emailTask{
			To:      "to@test.com",
			Subject: "Sub",
			Body:    "Body",
		}
		mockSMTP.EXPECT().Send(gomock.Any(), gomock.Any()).Return(nil)

		err := c.(*controller).handleEmailTask(context.Background(), memq.Task{Val: emailTask})
		if err != nil {
			t.Errorf("handleEmailTask failed: %v", err)
		}
	})
}
