package slack

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	mock_pubsub "github.com/agentrq/agentrq/backend/internal/service/mocks/pubsub"
	mock_repo "github.com/agentrq/agentrq/backend/internal/service/mocks/repository"
	"github.com/agentrq/agentrq/backend/internal/service/security"
	"github.com/golang/mock/gomock"
	"github.com/mustafaturan/monoflake"
	slackapi "github.com/slack-go/slack"
	"gorm.io/datatypes"
)

// Thin stub for slacksvc.Service
type stubSlackService struct{}

func (s *stubSlackService) IsEnabled() bool  { return true }
func (s *stubSlackService) ClientID() string { return "test-client-id" }
func (s *stubSlackService) CreatePrivateChannel(ctx context.Context, token, name string) (string, error) {
	return "", nil
}
func (s *stubSlackService) InviteUsersToChannel(ctx context.Context, token, channelID string, userIDs []string) error {
	return nil
}
func (s *stubSlackService) PostMessage(ctx context.Context, token, channelID string, blocks []slackapi.Block) (string, error) {
	return "", nil
}
func (s *stubSlackService) PostThreadReply(ctx context.Context, token, channelID, threadTS string, blocks []slackapi.Block) (string, error) {
	return "", nil
}
func (s *stubSlackService) UpdateMessage(ctx context.Context, token, channelID, ts string, blocks []slackapi.Block) error {
	return nil
}
func (s *stubSlackService) ExchangeCode(ctx context.Context, code, redirectURI string) (string, string, string, string, error) {
	return "", "", "", "", nil
}
func (s *stubSlackService) VerifyRequest(r *http.Request, body []byte) error {
	return nil
}

// Thin mock for CRUDRespondToTask
type mockCRUD struct {
	createTaskFunc    func(ctx context.Context, req entity.CreateTaskRequest) (*entity.CreateTaskResponse, error)
	replyToTaskFunc   func(ctx context.Context, req entity.ReplyToTaskRequest) (*entity.ReplyToTaskResponse, error)
	respondToTaskFunc func(ctx context.Context, req entity.RespondToTaskRequest) (*entity.RespondToTaskResponse, error)
}

func (m *mockCRUD) CreateTask(ctx context.Context, req entity.CreateTaskRequest) (*entity.CreateTaskResponse, error) {
	if m.createTaskFunc != nil {
		return m.createTaskFunc(ctx, req)
	}
	return &entity.CreateTaskResponse{}, nil
}
func (m *mockCRUD) RespondToTask(ctx context.Context, req entity.RespondToTaskRequest) (*entity.RespondToTaskResponse, error) {
	if m.respondToTaskFunc != nil {
		return m.respondToTaskFunc(ctx, req)
	}
	return nil, nil
}
func (m *mockCRUD) ReplyToTask(ctx context.Context, req entity.ReplyToTaskRequest) (*entity.ReplyToTaskResponse, error) {
	if m.replyToTaskFunc != nil {
		return m.replyToTaskFunc(ctx, req)
	}
	return &entity.ReplyToTaskResponse{}, nil
}
func (m *mockCRUD) CheckWorkspaceAccess(ctx context.Context, id int64, userID string) (bool, error) {
	return true, nil
}

type mockMCP struct {
	capturedVerdict *struct {
		workspaceID int64
		userID      string
		taskID      int64
		requestID   string
		behavior    string
	}
}

func (m *mockMCP) SendPermissionVerdict(ctx context.Context, workspaceID int64, userID string, taskID int64, requestID, behavior string) error {
	m.capturedVerdict = &struct {
		workspaceID int64
		userID      string
		taskID      int64
		requestID   string
		behavior    string
	}{workspaceID, userID, taskID, requestID, behavior}
	return nil
}

func (m *mockMCP) SendChannelNotification(ctx context.Context, workspaceID int64, userID string, taskID int64, content string) {
}

func TestHandleSlashCommand_ChannelNotFound(t *testing.T) {
	gomockCtrl := gomock.NewController(t)
	defer gomockCtrl.Finish()

	mockRepo := mock_repo.NewMockRepository(gomockCtrl)
	mockPubSub := mock_pubsub.NewMockService(gomockCtrl)
	crud := &mockCRUD{}
	mcp := &mockMCP{}

	c := New(Params{
		Repository: mockRepo,
		SlackSvc:   nil, // Not called
		Crud:       crud,
		MCPManager: mcp,
		PubSub:     mockPubSub,
		TokenKey:   "test-key",
		BaseURL:    "https://app.agentrq.com",
	})

	mockRepo.EXPECT().
		GetSlackWorkspaceLinkByChannel(gomock.Any(), "C123").
		Return(model.SlackWorkspaceLink{}, fmt.Errorf("not found"))

	msg, ephemeral, err := c.HandleSlashCommand(context.Background(), "C123", "some text")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ephemeral {
		t.Error("expected response to be ephemeral")
	}
	if !strings.Contains(msg, "not connected") {
		t.Errorf("expected warning message, got %q", msg)
	}
}

func TestHandleSlashCommand_WorkspaceNotFound(t *testing.T) {
	gomockCtrl := gomock.NewController(t)
	defer gomockCtrl.Finish()

	mockRepo := mock_repo.NewMockRepository(gomockCtrl)
	mockPubSub := mock_pubsub.NewMockService(gomockCtrl)
	crud := &mockCRUD{}
	mcp := &mockMCP{}

	c := New(Params{
		Repository: mockRepo,
		SlackSvc:   nil,
		Crud:       crud,
		MCPManager: mcp,
		PubSub:     mockPubSub,
		TokenKey:   "test-key",
		BaseURL:    "https://app.agentrq.com",
	})

	mockRepo.EXPECT().
		GetSlackWorkspaceLinkByChannel(gomock.Any(), "C123").
		Return(model.SlackWorkspaceLink{WorkspaceID: 1}, nil)

	mockRepo.EXPECT().
		SystemGetWorkspace(gomock.Any(), int64(1)).
		Return(model.Workspace{}, fmt.Errorf("db error"))

	_, _, err := c.HandleSlashCommand(context.Background(), "C123", "some text")
	if err == nil {
		t.Fatal("expected error when workspace is not found")
	}
}

func TestHandleSlashCommand_Success_Unquoted(t *testing.T) {
	gomockCtrl := gomock.NewController(t)
	defer gomockCtrl.Finish()

	mockRepo := mock_repo.NewMockRepository(gomockCtrl)
	mockPubSub := mock_pubsub.NewMockService(gomockCtrl)
	mcp := &mockMCP{}

	var capturedReq entity.CreateTaskRequest
	crud := &mockCRUD{
		createTaskFunc: func(ctx context.Context, req entity.CreateTaskRequest) (*entity.CreateTaskResponse, error) {
			capturedReq = req
			return &entity.CreateTaskResponse{}, nil
		},
	}

	c := New(Params{
		Repository: mockRepo,
		SlackSvc:   nil,
		Crud:       crud,
		MCPManager: mcp,
		PubSub:     mockPubSub,
		TokenKey:   "test-key",
		BaseURL:    "https://app.agentrq.com",
	})

	mockRepo.EXPECT().
		GetSlackWorkspaceLinkByChannel(gomock.Any(), "C123").
		Return(model.SlackWorkspaceLink{WorkspaceID: 42}, nil)

	mockRepo.EXPECT().
		SystemGetWorkspace(gomock.Any(), int64(42)).
		Return(model.Workspace{ID: 42, UserID: 100}, nil)

	mockRepo.EXPECT().
		ListTasks(gomock.Any(), entity.ListTasksRequest{WorkspaceID: int64(42)}, int64(100)).
		Return([]model.Task{}, nil)

	msg, ephemeral, err := c.HandleSlashCommand(context.Background(), "C123", "Write a binary search function")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ephemeral {
		t.Error("expected response not to be ephemeral")
	}
	if !strings.Contains(msg, "Write a binary search function") {
		t.Errorf("expected success message referencing task, got %q", msg)
	}

	// Verify the captured CRUD call
	if capturedReq.UserID != "0000000001c" { // monoflake base62 of 100 with zero padding
		t.Errorf("expected UserID to be 0000000001c, got %q", capturedReq.UserID)
	}
	if capturedReq.Task.Title != "Write a binary search function" {
		t.Errorf("expected Title to match input, got %q", capturedReq.Task.Title)
	}
	if capturedReq.Task.Body != "Write a binary search function" {
		t.Errorf("expected Body to match input, got %q", capturedReq.Task.Body)
	}
	if capturedReq.Task.CreatedBy != "human" {
		t.Errorf("expected CreatedBy human, got %q", capturedReq.Task.CreatedBy)
	}
	if capturedReq.Task.Assignee != "agent" {
		t.Errorf("expected Assignee agent, got %q", capturedReq.Task.Assignee)
	}
}

func TestHandleSlashCommand_Success_QuotedBoth(t *testing.T) {
	gomockCtrl := gomock.NewController(t)
	defer gomockCtrl.Finish()

	mockRepo := mock_repo.NewMockRepository(gomockCtrl)
	mockPubSub := mock_pubsub.NewMockService(gomockCtrl)
	mcp := &mockMCP{}

	var capturedReq entity.CreateTaskRequest
	crud := &mockCRUD{
		createTaskFunc: func(ctx context.Context, req entity.CreateTaskRequest) (*entity.CreateTaskResponse, error) {
			capturedReq = req
			return &entity.CreateTaskResponse{}, nil
		},
	}

	c := New(Params{
		Repository: mockRepo,
		SlackSvc:   nil,
		Crud:       crud,
		MCPManager: mcp,
		PubSub:     mockPubSub,
		TokenKey:   "test-key",
		BaseURL:    "https://app.agentrq.com",
	})

	mockRepo.EXPECT().
		GetSlackWorkspaceLinkByChannel(gomock.Any(), "C123").
		Return(model.SlackWorkspaceLink{WorkspaceID: 42}, nil)

	mockRepo.EXPECT().
		SystemGetWorkspace(gomock.Any(), int64(42)).
		Return(model.Workspace{ID: 42, UserID: 100}, nil)

	mockRepo.EXPECT().
		ListTasks(gomock.Any(), entity.ListTasksRequest{WorkspaceID: int64(42)}, int64(100)).
		Return([]model.Task{}, nil)

	msg, _, err := c.HandleSlashCommand(context.Background(), "C123", `"Task Title Here" "Description goes in second quotes here"`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(msg, "Task Title Here") {
		t.Errorf("expected success message to show title, got %q", msg)
	}

	if capturedReq.Task.Title != "Task Title Here" {
		t.Errorf("expected parsed Title 'Task Title Here', got %q", capturedReq.Task.Title)
	}
	if capturedReq.Task.Body != "Description goes in second quotes here" {
		t.Errorf("expected parsed Body, got %q", capturedReq.Task.Body)
	}
}

func TestHandleSlashCommand_Success_SmartQuotes(t *testing.T) {
	gomockCtrl := gomock.NewController(t)
	defer gomockCtrl.Finish()

	mockRepo := mock_repo.NewMockRepository(gomockCtrl)
	mockPubSub := mock_pubsub.NewMockService(gomockCtrl)
	mcp := &mockMCP{}

	var capturedReq entity.CreateTaskRequest
	crud := &mockCRUD{
		createTaskFunc: func(ctx context.Context, req entity.CreateTaskRequest) (*entity.CreateTaskResponse, error) {
			capturedReq = req
			return &entity.CreateTaskResponse{}, nil
		},
	}

	c := New(Params{
		Repository: mockRepo,
		SlackSvc:   nil,
		Crud:       crud,
		MCPManager: mcp,
		PubSub:     mockPubSub,
		TokenKey:   "test-key",
		BaseURL:    "https://app.agentrq.com",
	})

	mockRepo.EXPECT().
		GetSlackWorkspaceLinkByChannel(gomock.Any(), "C123").
		Return(model.SlackWorkspaceLink{WorkspaceID: 42}, nil)

	mockRepo.EXPECT().
		SystemGetWorkspace(gomock.Any(), int64(42)).
		Return(model.Workspace{ID: 42, UserID: 100}, nil)

	mockRepo.EXPECT().
		ListTasks(gomock.Any(), entity.ListTasksRequest{WorkspaceID: int64(42)}, int64(100)).
		Return([]model.Task{}, nil)

	// Test iOS/macOS smart quotes normalization
	msg, _, err := c.HandleSlashCommand(context.Background(), "C123", `“Smart Title” “Smart Description”`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(msg, "Smart Title") {
		t.Errorf("expected success message to contain title, got %q", msg)
	}

	if capturedReq.Task.Title != "Smart Title" {
		t.Errorf("expected parsed Title 'Smart Title', got %q", capturedReq.Task.Title)
	}
	if capturedReq.Task.Body != "Smart Description" {
		t.Errorf("expected parsed Body 'Smart Description', got %q", capturedReq.Task.Body)
	}
}

func TestHandleSlashCommand_Success_Truncation(t *testing.T) {
	gomockCtrl := gomock.NewController(t)
	defer gomockCtrl.Finish()

	mockRepo := mock_repo.NewMockRepository(gomockCtrl)
	mockPubSub := mock_pubsub.NewMockService(gomockCtrl)
	mcp := &mockMCP{}

	var capturedReq entity.CreateTaskRequest
	crud := &mockCRUD{
		createTaskFunc: func(ctx context.Context, req entity.CreateTaskRequest) (*entity.CreateTaskResponse, error) {
			capturedReq = req
			return &entity.CreateTaskResponse{}, nil
		},
	}

	c := New(Params{
		Repository: mockRepo,
		SlackSvc:   nil,
		Crud:       crud,
		MCPManager: mcp,
		PubSub:     mockPubSub,
		TokenKey:   "test-key",
		BaseURL:    "https://app.agentrq.com",
	})

	mockRepo.EXPECT().
		GetSlackWorkspaceLinkByChannel(gomock.Any(), "C123").
		Return(model.SlackWorkspaceLink{WorkspaceID: 42}, nil)

	mockRepo.EXPECT().
		SystemGetWorkspace(gomock.Any(), int64(42)).
		Return(model.Workspace{ID: 42, UserID: 100}, nil)

	mockRepo.EXPECT().
		ListTasks(gomock.Any(), entity.ListTasksRequest{WorkspaceID: int64(42)}, int64(100)).
		Return([]model.Task{}, nil)

	longTitle := "This is an extremely long title that spans over sixty characters to test if the truncation works correctly"
	// Title is 108 characters. First 60 characters is "This is an extremely long title that spans over sixty charac"
	expectedTruncatedTitle := "This is an extremely long title that spans over sixty charac..."

	_, _, err := c.HandleSlashCommand(context.Background(), "C123", fmt.Sprintf(`"%s" "Description"`, longTitle))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedReq.Task.Title != expectedTruncatedTitle {
		t.Errorf("expected truncated Title %q, got %q", expectedTruncatedTitle, capturedReq.Task.Title)
	}
	if capturedReq.Task.Body != "Description" {
		t.Errorf("expected Body to be untouched, got %q", capturedReq.Task.Body)
	}
}

func TestProcessEvent_OriginSlack(t *testing.T) {
	gomockCtrl := gomock.NewController(t)
	defer gomockCtrl.Finish()

	mockRepo := mock_repo.NewMockRepository(gomockCtrl)
	mockPubSub := mock_pubsub.NewMockService(gomockCtrl)
	crud := &mockCRUD{}
	mcp := &mockMCP{}

	c := New(Params{
		Repository: mockRepo,
		SlackSvc:   nil,
		Crud:       crud,
		MCPManager: mcp,
		PubSub:     mockPubSub,
		TokenKey:   "test-key",
		BaseURL:    "https://app.agentrq.com",
	})

	// If event.Origin is OriginSlack and it is a ResourceMessage, processEvent should immediately return and NOT query the DB or call handlers.
	// Since no mocks are expected here, any query would cause a test failure.
	event := entity.CRUDEvent{
		Action:       entity.ActionMessageCreate,
		ResourceType: entity.ResourceMessage,
		ResourceID:   123,
		Origin:       entity.OriginSlack,
	}

	c.(*controller).processEvent(context.Background(), event)
}

func TestHandleSlackEvent_WithFiles(t *testing.T) {
	gomockCtrl := gomock.NewController(t)
	defer gomockCtrl.Finish()

	mockRepo := mock_repo.NewMockRepository(gomockCtrl)
	mockPubSub := mock_pubsub.NewMockService(gomockCtrl)
	mcp := &mockMCP{}

	// Setup a mock HTTP test server to serve the private file download
	fileContent := "hello from slack attachment"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-decrypted-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(fileContent))
	}))
	defer server.Close()

	var capturedReq entity.ReplyToTaskRequest
	crud := &mockCRUD{
		replyToTaskFunc: func(ctx context.Context, req entity.ReplyToTaskRequest) (*entity.ReplyToTaskResponse, error) {
			capturedReq = req
			return &entity.ReplyToTaskResponse{}, nil
		},
	}

	c := New(Params{
		Repository: mockRepo,
		SlackSvc:   nil,
		Crud:       crud,
		MCPManager: mcp,
		PubSub:     mockPubSub,
		TokenKey:   "12345678901234567890123456789012",
		BaseURL:    "https://app.agentrq.com",
	})

	// Setup thread and workspace mocks
	mockRepo.EXPECT().
		GetSlackTaskThreadByChannel(gomock.Any(), "C_SLACK", "thread_123").
		Return(model.SlackTaskThread{
			TaskID:      99,
			WorkspaceID: 42,
		}, nil)

	// Setup mock encrypted link
	link := model.SlackWorkspaceLink{
		WorkspaceID: 42,
		BotUserID:   "U_BOT",
	}
	encToken, nonce, err := security.Encrypt("test-decrypted-token", "12345678901234567890123456789012")
	if err != nil {
		t.Fatalf("failed to encrypt: %v", err)
	}
	link.AccessToken = encToken
	link.TokenNonce = nonce

	mockRepo.EXPECT().
		GetSlackWorkspaceLink(gomock.Any(), int64(42)).
		Return(link, nil)

	mockRepo.EXPECT().
		SystemGetWorkspace(gomock.Any(), int64(42)).
		Return(model.Workspace{ID: 42, UserID: 100}, nil)

	// Send an event payload
	var payload SlackEventPayload
	payload.Event.Type = "app_mention"
	payload.Event.ThreadTS = "thread_123"
	payload.Event.Channel = "C_SLACK"
	payload.Event.User = "U_USER"
	payload.Event.Text = "<@U_BOT> check out this file"
	payload.Event.Files = []SlackFile{
		{
			ID:                 "F_123",
			Name:               "test_attachment.txt",
			MimeType:           "text/plain",
			URLPrivateDownload: server.URL,
		},
	}

	err = c.HandleSlackEvent(context.Background(), payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedReq.TaskID != 99 {
		t.Errorf("expected TaskID to be 99, got %d", capturedReq.TaskID)
	}
	if capturedReq.Text != "check out this file" {
		t.Errorf("expected text without bot mention, got %q", capturedReq.Text)
	}
	if len(capturedReq.Attachments) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(capturedReq.Attachments))
	}
	att := capturedReq.Attachments[0]
	if att.Filename != "test_attachment.txt" {
		t.Errorf("expected filename 'test_attachment.txt', got %q", att.Filename)
	}
	if att.MimeType != "text/plain" {
		t.Errorf("expected mimeType 'text/plain', got %q", att.MimeType)
	}

	decodedBytes, err := base64.StdEncoding.DecodeString(att.Data)
	if err != nil {
		t.Fatalf("failed to decode base64: %v", err)
	}
	if string(decodedBytes) != fileContent {
		t.Errorf("expected file content %q, got %q", fileContent, string(decodedBytes))
	}
}

type stubSlackServiceChannelNotFound struct {
	stubSlackService
}

func (s *stubSlackServiceChannelNotFound) PostMessage(ctx context.Context, token, channelID string, blocks []slackapi.Block) (string, error) {
	return "", fmt.Errorf("slack: post message to %s: channel_not_found", channelID)
}

func TestOnTaskCreated_ChannelNotFound(t *testing.T) {
	gomockCtrl := gomock.NewController(t)
	defer gomockCtrl.Finish()

	mockRepo := mock_repo.NewMockRepository(gomockCtrl)
	mockPubSub := mock_pubsub.NewMockService(gomockCtrl)
	crud := &mockCRUD{}
	mcp := &mockMCP{}

	stubSlack := &stubSlackServiceChannelNotFound{}

	c := New(Params{
		Repository: mockRepo,
		SlackSvc:   stubSlack,
		Crud:       crud,
		MCPManager: mcp,
		PubSub:     mockPubSub,
		TokenKey:   "0123456789abcdef0123456789abcdef", // 32-byte key
		BaseURL:    "https://app.agentrq.com",
	})

	decToken := "xoxb-test-token"
	encToken, nonce, err := security.Encrypt(decToken, "0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("failed to encrypt token: %v", err)
	}

	task := entity.Task{
		ID:          1,
		WorkspaceID: 42,
		Title:       "Test task",
		Body:        "Description",
		Assignee:    "human",
		Status:      "notstarted",
	}

	mockRepo.EXPECT().
		GetSlackWorkspaceLink(gomock.Any(), int64(42)).
		Return(model.SlackWorkspaceLink{
			WorkspaceID:    42,
			AccessToken:    encToken,
			TokenNonce:     nonce,
			SlackChannelID: "C123",
		}, nil)

	mockRepo.EXPECT().
		DeleteSlackWorkspaceLink(gomock.Any(), int64(42)).
		Return(nil)

	err = c.OnTaskCreated(context.Background(), task)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "channel_not_found") {
		t.Errorf("expected channel_not_found error, got %v", err)
	}
}

func TestOnMessageCreated_HumanUserName(t *testing.T) {
	gomockCtrl := gomock.NewController(t)
	defer gomockCtrl.Finish()

	mockRepo := mock_repo.NewMockRepository(gomockCtrl)
	mockPubSub := mock_pubsub.NewMockService(gomockCtrl)
	crud := &mockCRUD{}
	mcp := &mockMCP{}

	stubSlack := &stubSlackService{}

	c := New(Params{
		Repository: mockRepo,
		SlackSvc:   stubSlack,
		Crud:       crud,
		MCPManager: mcp,
		PubSub:     mockPubSub,
		TokenKey:   "0123456789abcdef0123456789abcdef", // 32-byte key
		BaseURL:    "https://app.agentrq.com",
	})

	decToken := "xoxb-test-token"
	encToken, nonceStr, err := security.Encrypt(decToken, "0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("failed to encrypt token: %v", err)
	}

	msg := entity.Message{
		ID:     1,
		TaskID: 99,
		UserID: 100,
		Sender: "human",
		Text:   "Hello slack thread",
	}

	task := entity.Task{
		ID:          99,
		WorkspaceID: 42,
	}

	// Expect SlackTaskThread lookup
	mockRepo.EXPECT().
		GetSlackTaskThreadByTask(gomock.Any(), int64(99)).
		Return(model.SlackTaskThread{
			TaskID:         99,
			WorkspaceID:    42,
			SlackChannelID: "C123",
			ThreadTS:       "12345678.90",
		}, nil)

	// Expect SlackWorkspaceLink lookup
	mockRepo.EXPECT().
		GetSlackWorkspaceLink(gomock.Any(), int64(42)).
		Return(model.SlackWorkspaceLink{
			WorkspaceID:    42,
			AccessToken:    encToken,
			TokenNonce:     nonceStr,
			SlackChannelID: "C123",
		}, nil)

	// Expect User lookup to resolve "human" name
	mockRepo.EXPECT().
		SystemGetUser(gomock.Any(), int64(100)).
		Return(model.User{
			ID:   100,
			Name: "Mustafa Turan",
		}, nil)

	err = c.OnMessageCreated(context.Background(), msg, task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

type stubSlackServiceWithUpdateTracking struct {
	stubSlackService
	updateMessageCalled bool
	capturedChannelID   string
	capturedTS          string
}

func (s *stubSlackServiceWithUpdateTracking) UpdateMessage(ctx context.Context, token, channelID, ts string, blocks []slackapi.Block) error {
	s.updateMessageCalled = true
	s.capturedChannelID = channelID
	s.capturedTS = ts
	return nil
}

type stubSlackServiceWithPostTracking struct {
	stubSlackService
	postMessageCalled bool
	capturedChannelID string
}

func (s *stubSlackServiceWithPostTracking) PostMessage(ctx context.Context, token, channelID string, blocks []slackapi.Block) (string, error) {
	s.postMessageCalled = true
	s.capturedChannelID = channelID
	return "thread-ts-123", nil
}

func TestOnMessageUpdated_Success(t *testing.T) {
	gomockCtrl := gomock.NewController(t)
	defer gomockCtrl.Finish()

	mockRepo := mock_repo.NewMockRepository(gomockCtrl)
	mockPubSub := mock_pubsub.NewMockService(gomockCtrl)
	crud := &mockCRUD{}
	mcp := &mockMCP{}

	stubSlack := &stubSlackServiceWithUpdateTracking{}

	c := New(Params{
		Repository: mockRepo,
		SlackSvc:   stubSlack,
		Crud:       crud,
		MCPManager: mcp,
		PubSub:     mockPubSub,
		TokenKey:   "0123456789abcdef0123456789abcdef", // 32-byte key
		BaseURL:    "https://app.agentrq.com",
	})

	decToken := "xoxb-test-token"
	encToken, nonceStr, err := security.Encrypt(decToken, "0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("failed to encrypt token: %v", err)
	}

	msg := entity.Message{
		ID:     1,
		TaskID: 99,
		Sender: "agent",
		Metadata: map[string]any{
			"type":             "permission_request",
			"status":           "allow",
			"slack_channel_id": "C_TEST",
			"slack_message_ts": "12345678.90",
		},
	}

	task := entity.Task{
		ID:          99,
		WorkspaceID: 42,
	}

	// Expect SlackWorkspaceLink lookup
	mockRepo.EXPECT().
		GetSlackWorkspaceLink(gomock.Any(), int64(42)).
		Return(model.SlackWorkspaceLink{
			WorkspaceID:    42,
			AccessToken:    encToken,
			TokenNonce:     nonceStr,
			SlackChannelID: "C123",
		}, nil)

	err = c.OnMessageUpdated(context.Background(), msg, task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !stubSlack.updateMessageCalled {
		t.Fatal("expected UpdateMessage to be called")
	}
	if stubSlack.capturedChannelID != "C_TEST" {
		t.Errorf("expected channel ID 'C_TEST', got %q", stubSlack.capturedChannelID)
	}
	if stubSlack.capturedTS != "12345678.90" {
		t.Errorf("expected message ts '12345678.90', got %q", stubSlack.capturedTS)
	}
}

func TestOnMessageUpdated_SkippedIfDecidedInSlack(t *testing.T) {
	gomockCtrl := gomock.NewController(t)
	defer gomockCtrl.Finish()

	mockRepo := mock_repo.NewMockRepository(gomockCtrl)
	mockPubSub := mock_pubsub.NewMockService(gomockCtrl)
	crud := &mockCRUD{}
	mcp := &mockMCP{}

	stubSlack := &stubSlackServiceWithUpdateTracking{}

	c := New(Params{
		Repository: mockRepo,
		SlackSvc:   stubSlack,
		Crud:       crud,
		MCPManager: mcp,
		PubSub:     mockPubSub,
		TokenKey:   "0123456789abcdef0123456789abcdef", // 32-byte key
		BaseURL:    "https://app.agentrq.com",
	})

	msg := entity.Message{
		ID:     1,
		TaskID: 99,
		Sender: "agent",
		Metadata: map[string]any{
			"type":             "permission_request",
			"status":           "allow",
			"decided_in_slack": true,
			"slack_channel_id": "C_TEST",
			"slack_message_ts": "12345678.90",
		},
	}

	task := entity.Task{
		ID:          99,
		WorkspaceID: 42,
	}

	err := c.OnMessageUpdated(context.Background(), msg, task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if stubSlack.updateMessageCalled {
		t.Fatal("expected UpdateMessage to be skipped because decided_in_slack is true")
	}
}

func TestOnTaskCreated_Success(t *testing.T) {
	gomockCtrl := gomock.NewController(t)
	defer gomockCtrl.Finish()

	mockRepo := mock_repo.NewMockRepository(gomockCtrl)
	mockPubSub := mock_pubsub.NewMockService(gomockCtrl)

	stubSlack := &stubSlackServiceWithPostTracking{}

	c := New(Params{
		Repository: mockRepo,
		SlackSvc:   stubSlack,
		Crud:       &mockCRUD{},
		MCPManager: &mockMCP{},
		PubSub:     mockPubSub,
		TokenKey:   "0123456789abcdef0123456789abcdef",
		BaseURL:    "https://app.agentrq.com",
	})

	decToken := "xoxb-test-token"
	encToken, nonce, err := security.Encrypt(decToken, "0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("failed to encrypt token: %v", err)
	}

	task := entity.Task{
		ID:          1,
		WorkspaceID: 42,
		Title:       "Fix the bug",
		Body:        "See issue #42",
		Assignee:    "agent",
		Status:      "notstarted",
		CreatedBy:   "human",
	}

	mockRepo.EXPECT().
		GetSlackWorkspaceLink(gomock.Any(), int64(42)).
		Return(model.SlackWorkspaceLink{
			WorkspaceID:    42,
			AccessToken:    encToken,
			TokenNonce:     nonce,
			SlackChannelID: "C_PROJ",
		}, nil)

	var capturedThread model.SlackTaskThread
	mockRepo.EXPECT().
		UpsertSlackTaskThread(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, th model.SlackTaskThread) error {
			capturedThread = th
			return nil
		})

	err = c.OnTaskCreated(context.Background(), task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !stubSlack.postMessageCalled {
		t.Error("expected PostMessage to be called")
	}
	if stubSlack.capturedChannelID != "C_PROJ" {
		t.Errorf("expected channel 'C_PROJ', got %q", stubSlack.capturedChannelID)
	}
	if capturedThread.TaskID != 1 || capturedThread.WorkspaceID != 42 ||
		capturedThread.SlackChannelID != "C_PROJ" || capturedThread.ThreadTS != "thread-ts-123" {
		t.Errorf("unexpected SlackTaskThread: %+v", capturedThread)
	}
}

func TestHandleTaskApproval_Success_Allow(t *testing.T) {
	gomockCtrl := gomock.NewController(t)
	defer gomockCtrl.Finish()

	mockRepo := mock_repo.NewMockRepository(gomockCtrl)
	mockPubSub := mock_pubsub.NewMockService(gomockCtrl)
	stubSlack := &stubSlackServiceWithUpdateTracking{}

	workspaceID := int64(42)
	taskID := int64(99)
	ownerUserID := monoflake.ID(int64(100)).String()

	var capturedReq entity.RespondToTaskRequest
	var capturedOrigin entity.Origin
	crud := &mockCRUD{
		respondToTaskFunc: func(ctx context.Context, req entity.RespondToTaskRequest) (*entity.RespondToTaskResponse, error) {
			capturedReq = req
			capturedOrigin = entity.GetOrigin(ctx)
			return &entity.RespondToTaskResponse{}, nil
		},
	}

	c := New(Params{
		Repository: mockRepo,
		SlackSvc:   stubSlack,
		Crud:       crud,
		MCPManager: &mockMCP{},
		PubSub:     mockPubSub,
		TokenKey:   "0123456789abcdef0123456789abcdef",
		BaseURL:    "https://app.agentrq.com",
	})

	workspaceID62 := monoflake.ID(workspaceID).String()
	taskID62 := monoflake.ID(taskID).String()
	actionID := "task_respond:" + workspaceID62 + ":" + taskID62 + ":allow"

	decToken := "xoxb-test-token"
	encToken, nonce, err := security.Encrypt(decToken, "0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("failed to encrypt token: %v", err)
	}

	mockRepo.EXPECT().
		SystemGetWorkspace(gomock.Any(), workspaceID).
		Return(model.Workspace{UserID: int64(100)}, nil)

	mockRepo.EXPECT().
		GetSlackWorkspaceLink(gomock.Any(), workspaceID).
		Return(model.SlackWorkspaceLink{
			WorkspaceID:    workspaceID,
			AccessToken:    encToken,
			TokenNonce:     nonce,
			SlackChannelID: "C123",
		}, nil)

	action := SlackBlockAction{
		ActionID:  actionID,
		ChannelID: "C123",
		MessageTS: "12345678.90",
		UserID:    "U_REVIEWER",
	}

	err = c.HandleTaskApproval(context.Background(), action)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedReq.WorkspaceID != workspaceID {
		t.Errorf("RespondToTask: expected workspaceID %d, got %d", workspaceID, capturedReq.WorkspaceID)
	}
	if capturedReq.TaskID != taskID {
		t.Errorf("RespondToTask: expected taskID %d, got %d", taskID, capturedReq.TaskID)
	}
	if capturedReq.Action != "allow" {
		t.Errorf("RespondToTask: expected action 'allow', got %q", capturedReq.Action)
	}
	if capturedReq.UserID != ownerUserID {
		t.Errorf("RespondToTask: expected userID %q, got %q", ownerUserID, capturedReq.UserID)
	}
	if capturedOrigin != entity.OriginSlack {
		t.Errorf("RespondToTask: expected OriginSlack in context, got %v", capturedOrigin)
	}
	if !stubSlack.updateMessageCalled {
		t.Error("expected UpdateMessage to be called after approval")
	}
	if stubSlack.capturedTS != "12345678.90" {
		t.Errorf("expected ts '12345678.90', got %q", stubSlack.capturedTS)
	}
}

func TestHandleTaskApproval_InvalidActionID(t *testing.T) {
	c := New(Params{
		TokenKey: "0123456789abcdef0123456789abcdef",
		BaseURL:  "https://app.agentrq.com",
	})

	action := SlackBlockAction{ActionID: "task_respond:only:two"}
	err := c.HandleTaskApproval(context.Background(), action)
	if err == nil {
		t.Fatal("expected error for malformed action_id")
	}
	if !strings.Contains(err.Error(), "invalid task_respond action_id") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestHandleTaskApproval_WorkspaceNotFound(t *testing.T) {
	gomockCtrl := gomock.NewController(t)
	defer gomockCtrl.Finish()

	mockRepo := mock_repo.NewMockRepository(gomockCtrl)
	mockPubSub := mock_pubsub.NewMockService(gomockCtrl)

	c := New(Params{
		Repository: mockRepo,
		SlackSvc:   &stubSlackService{},
		Crud:       &mockCRUD{},
		MCPManager: &mockMCP{},
		PubSub:     mockPubSub,
		TokenKey:   "0123456789abcdef0123456789abcdef",
		BaseURL:    "https://app.agentrq.com",
	})

	workspaceID := int64(42)
	workspaceID62 := monoflake.ID(workspaceID).String()
	taskID62 := monoflake.ID(int64(99)).String()
	actionID := "task_respond:" + workspaceID62 + ":" + taskID62 + ":allow"

	mockRepo.EXPECT().
		SystemGetWorkspace(gomock.Any(), workspaceID).
		Return(model.Workspace{}, fmt.Errorf("not found"))

	action := SlackBlockAction{ActionID: actionID, ChannelID: "C123", MessageTS: "ts"}
	err := c.HandleTaskApproval(context.Background(), action)
	if err == nil {
		t.Fatal("expected error when workspace is not found")
	}
}

func TestHandleMCPPermission_Success_Allow(t *testing.T) {
	gomockCtrl := gomock.NewController(t)
	defer gomockCtrl.Finish()

	mockRepo := mock_repo.NewMockRepository(gomockCtrl)
	mockPubSub := mock_pubsub.NewMockService(gomockCtrl)
	stubSlack := &stubSlackServiceWithUpdateTracking{}
	mcp := &mockMCP{}

	c := New(Params{
		Repository: mockRepo,
		SlackSvc:   stubSlack,
		Crud:       &mockCRUD{},
		MCPManager: mcp,
		PubSub:     mockPubSub,
		TokenKey:   "0123456789abcdef0123456789abcdef",
		BaseURL:    "https://app.agentrq.com",
	})

	workspaceID := int64(42)
	workspaceID62 := monoflake.ID(workspaceID).String()
	taskID := int64(99)
	taskID62 := monoflake.ID(taskID).String()
	requestID := "req-abc-123"
	ownerUserID := monoflake.ID(int64(100)).String()
	actionID := "task_permission:" + workspaceID62 + ":" + taskID62 + ":" + requestID + ":allow"

	decToken := "xoxb-test-token"
	encToken, nonce, err := security.Encrypt(decToken, "0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("failed to encrypt token: %v", err)
	}

	metaBytes, _ := json.Marshal(map[string]any{
		"type":       "permission_request",
		"request_id": requestID,
	})
	permMsg := model.Message{
		ID:       7,
		TaskID:   taskID,
		Metadata: datatypes.JSON(metaBytes),
	}

	mockRepo.EXPECT().
		SystemGetWorkspace(gomock.Any(), workspaceID).
		Return(model.Workspace{UserID: int64(100)}, nil)

	mockRepo.EXPECT().
		GetSlackWorkspaceLink(gomock.Any(), workspaceID).
		Return(model.SlackWorkspaceLink{
			WorkspaceID:    workspaceID,
			AccessToken:    encToken,
			TokenNonce:     nonce,
			SlackChannelID: "C123",
		}, nil)

	mockRepo.EXPECT().
		ListMessages(gomock.Any(), taskID).
		Return([]model.Message{permMsg}, nil)

	var capturedMeta []byte
	mockRepo.EXPECT().
		UpdateMessageMetadata(gomock.Any(), taskID, int64(7), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ int64, _ int64, b []byte) error {
			capturedMeta = b
			return nil
		})

	action := SlackBlockAction{
		ActionID:  actionID,
		ChannelID: "C123",
		MessageTS: "12345678.90",
		UserID:    "U_REVIEWER",
		UserName:  "John Doe",
	}

	err = c.HandleMCPPermission(context.Background(), action)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Assert metadata-tagging: decided_in_slack, slack_user_id, request_id
	var meta map[string]any
	if err := json.Unmarshal(capturedMeta, &meta); err != nil {
		t.Fatalf("failed to unmarshal captured metadata: %v", err)
	}
	if decided, _ := meta["decided_in_slack"].(bool); !decided {
		t.Error("expected decided_in_slack=true in updated metadata")
	}
	if uid, _ := meta["slack_user_id"].(string); uid != "U_REVIEWER" {
		t.Errorf("expected slack_user_id='U_REVIEWER', got %q", uid)
	}
	if name, _ := meta["slack_user_name"].(string); name != "John Doe" {
		t.Errorf("expected slack_user_name='John Doe', got %q", name)
	}
	if rid, _ := meta["request_id"].(string); rid != requestID {
		t.Errorf("expected request_id=%q, got %q", requestID, rid)
	}

	// Assert SendPermissionVerdict args via capturedVerdict
	if mcp.capturedVerdict == nil {
		t.Fatal("expected SendPermissionVerdict to be called")
	}
	if mcp.capturedVerdict.workspaceID != workspaceID {
		t.Errorf("SendPermissionVerdict: expected workspaceID %d, got %d", workspaceID, mcp.capturedVerdict.workspaceID)
	}
	if mcp.capturedVerdict.userID != ownerUserID {
		t.Errorf("SendPermissionVerdict: expected userID %q, got %q", ownerUserID, mcp.capturedVerdict.userID)
	}
	if mcp.capturedVerdict.taskID != taskID {
		t.Errorf("SendPermissionVerdict: expected taskID %d, got %d", taskID, mcp.capturedVerdict.taskID)
	}
	if mcp.capturedVerdict.requestID != requestID {
		t.Errorf("SendPermissionVerdict: expected requestID %q, got %q", requestID, mcp.capturedVerdict.requestID)
	}
	if mcp.capturedVerdict.behavior != "allow" {
		t.Errorf("SendPermissionVerdict: expected behavior 'allow', got %q", mcp.capturedVerdict.behavior)
	}
	if !stubSlack.updateMessageCalled {
		t.Error("expected UpdateMessage to be called after permission verdict")
	}
}

func TestHandleMCPPermission_InvalidActionID(t *testing.T) {
	c := New(Params{
		TokenKey: "0123456789abcdef0123456789abcdef",
		BaseURL:  "https://app.agentrq.com",
	})

	action := SlackBlockAction{ActionID: "task_permission:only:three:parts"}
	err := c.HandleMCPPermission(context.Background(), action)
	if err == nil {
		t.Fatal("expected error for malformed action_id")
	}
	if !strings.Contains(err.Error(), "invalid task_permission action_id") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestHandleMCPPermission_NoMCPManager(t *testing.T) {
	gomockCtrl := gomock.NewController(t)
	defer gomockCtrl.Finish()

	mockRepo := mock_repo.NewMockRepository(gomockCtrl)
	mockPubSub := mock_pubsub.NewMockService(gomockCtrl)

	decToken := "xoxb-test-token"
	encToken, nonce, err := security.Encrypt(decToken, "0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("failed to encrypt token: %v", err)
	}

	c := New(Params{
		Repository: mockRepo,
		SlackSvc:   &stubSlackServiceWithUpdateTracking{},
		Crud:       &mockCRUD{},
		MCPManager: nil,
		PubSub:     mockPubSub,
		TokenKey:   "0123456789abcdef0123456789abcdef",
		BaseURL:    "https://app.agentrq.com",
	})

	workspaceID := int64(42)
	workspaceID62 := monoflake.ID(workspaceID).String()
	taskID := int64(99)
	taskID62 := monoflake.ID(taskID).String()
	actionID := "task_permission:" + workspaceID62 + ":" + taskID62 + ":req-xyz:allow"

	mockRepo.EXPECT().
		SystemGetWorkspace(gomock.Any(), workspaceID).
		Return(model.Workspace{UserID: int64(100)}, nil)

	mockRepo.EXPECT().
		GetSlackWorkspaceLink(gomock.Any(), workspaceID).
		Return(model.SlackWorkspaceLink{
			WorkspaceID:    workspaceID,
			AccessToken:    encToken,
			TokenNonce:     nonce,
			SlackChannelID: "C123",
		}, nil)

	action := SlackBlockAction{ActionID: actionID, ChannelID: "C123", MessageTS: "ts"}
	err = c.HandleMCPPermission(context.Background(), action)
	if err == nil {
		t.Fatal("expected error when MCP manager is nil")
	}
	if !strings.Contains(err.Error(), "MCP manager not available") {
		t.Errorf("unexpected error message: %v", err)
	}
}
