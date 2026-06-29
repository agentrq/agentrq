package slack

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/service/auth"
	mock_pubsub "github.com/agentrq/agentrq/backend/internal/service/mocks/pubsub"
	mock_repo "github.com/agentrq/agentrq/backend/internal/service/mocks/repository"
	"github.com/agentrq/agentrq/backend/internal/service/security"
	"github.com/golang/mock/gomock"
	slackapi "github.com/slack-go/slack"
)

// Thin stub for slacksvc.Service
type stubSlackService struct{}

func (s *stubSlackService) IsEnabled() bool    { return true }
func (s *stubSlackService) ClientID() string   { return "test-client-id" }
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
	createTaskFunc  func(ctx context.Context, req entity.CreateTaskRequest) (*entity.CreateTaskResponse, error)
	replyToTaskFunc func(ctx context.Context, req entity.ReplyToTaskRequest) (*entity.ReplyToTaskResponse, error)
}

func (m *mockCRUD) CreateTask(ctx context.Context, req entity.CreateTaskRequest) (*entity.CreateTaskResponse, error) {
	if m.createTaskFunc != nil {
		return m.createTaskFunc(ctx, req)
	}
	return &entity.CreateTaskResponse{}, nil
}
func (m *mockCRUD) RespondToTask(ctx context.Context, req entity.RespondToTaskRequest) (*entity.RespondToTaskResponse, error) {
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

type mockMCP struct{}

func (m *mockMCP) SendPermissionVerdict(ctx context.Context, workspaceID int64, userID string, taskID int64, requestID, behavior string) error {
	return nil
}

func (m *mockMCP) SendChannelNotification(ctx context.Context, workspaceID int64, userID string, taskID int64, content string) {}

type mockTokenSvc struct {
	auth.TokenService
}

func (m *mockTokenSvc) CreateOAuthStateToken(redirectURL, provider string) (string, error) {
	return redirectURL, nil
}

func (m *mockTokenSvc) ValidateOAuthStateToken(tokenStr, provider string) (string, error) {
	return tokenStr, nil
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
		TokenSvc:   &mockTokenSvc{}, TokenKey:   "test-key",
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
		TokenSvc:   &mockTokenSvc{}, TokenKey:   "test-key",
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
		TokenSvc:   &mockTokenSvc{}, TokenKey:   "test-key",
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
		TokenSvc:   &mockTokenSvc{}, TokenKey:   "test-key",
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
		TokenSvc:   &mockTokenSvc{}, TokenKey:   "test-key",
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
		TokenSvc:   &mockTokenSvc{}, TokenKey:   "test-key",
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
		TokenSvc:   &mockTokenSvc{}, TokenKey:   "test-key",
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
		TokenSvc:   &mockTokenSvc{}, TokenKey:   "12345678901234567890123456789012",
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
		TokenSvc:   &mockTokenSvc{}, TokenKey:   "0123456789abcdef0123456789abcdef", // 32-byte key
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
		TokenSvc:   &mockTokenSvc{}, TokenKey:   "0123456789abcdef0123456789abcdef", // 32-byte key
		BaseURL:    "https://app.agentrq.com",
	})

	decToken := "xoxb-test-token"
	encToken, nonceStr, err := security.Encrypt(decToken, "0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("failed to encrypt token: %v", err)
	}

	msg := entity.Message{
		ID:        1,
		TaskID:    99,
		UserID:    100,
		Sender:    "human",
		Text:      "Hello slack thread",
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
		TokenSvc:   &mockTokenSvc{}, TokenKey:   "0123456789abcdef0123456789abcdef", // 32-byte key
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
		TokenSvc:   &mockTokenSvc{}, TokenKey:   "0123456789abcdef0123456789abcdef", // 32-byte key
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

