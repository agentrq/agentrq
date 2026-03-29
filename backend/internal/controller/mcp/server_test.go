package mcp

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
	mock_idgen "github.com/agentrq/agentrq/backend/internal/service/mocks/idgen"
	mock_storage "github.com/agentrq/agentrq/backend/internal/service/mocks/storage"
	mock_telemetry "github.com/agentrq/agentrq/backend/internal/service/mocks/telemetry"
	"github.com/golang/mock/gomock"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/mustafaturan/monoflake"
)

func TestWorkspaceServer_Metadata(t *testing.T) {
	ps := &WorkspaceServer{
		name:        "old name",
		icon:        "old icon",
		description: "old desc",
	}

	// UpdateMetadata
	ps.UpdateMetadata("new name", "new desc", "new icon")
	if ps.name != "new name" || ps.icon != "new icon" || ps.description != "new desc" {
		t.Errorf("Metadata not updated correctly: name=%s, description=%s, icon=%s", ps.name, ps.description, ps.icon)
	}

	// UpdateArchivedAt
	now := time.Now()
	ps.UpdateArchivedAt(&now)
	if ps.archivedAt == nil || !ps.archivedAt.Equal(now) {
		t.Error("ArchivedAt not updated correctly")
	}

	ps.UpdateArchivedAt(nil)
	if ps.archivedAt != nil {
		t.Error("ArchivedAt should be nil")
	}

	// UpdateAutoAllowedTools
	ps.UpdateAutoAllowedTools([]string{"tool1", "tool2"})
	ps.autoAllowedToolsMu.RLock()
	tools := ps.autoAllowedTools
	ps.autoAllowedToolsMu.RUnlock()
	if len(tools) != 2 || tools[0] != "tool1" || tools[1] != "tool2" {
		t.Errorf("AutoAllowedTools not updated correctly: %v", tools)
	}
}

func TestWorkspaceServer_AgentConnected(t *testing.T) {
	ps := &WorkspaceServer{}
	if ps.IsAgentConnected() {
		t.Error("expected initially not connected")
	}
	ps.agentConnections.Store(1)
	if !ps.IsAgentConnected() {
		t.Error("expected connected")
	}
	ps.agentConnections.Store(0)
	if ps.IsAgentConnected() {
		t.Error("expected disconnected")
	}
}

func TestWorkspaceServer_HandleGetWorkspace(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockTel := mock_telemetry.NewMockService(ctrl)
	ps := &WorkspaceServer{
		workspaceID: 100,
		userID:      "15264777",
		name:        "My Workspace",
		description: "My Description",
		telemetry:   mockTel,
		listTasks: func(ctx context.Context) ([]model.Task, error) {
			return []model.Task{
				{Status: "ongoing"},
				{Status: "completed"},
			}, nil
		},
	}

	mockTel.EXPECT().Record(gomock.Any(), gomock.Any(), int64(100), model.ActionIDMCPToolCall)

	res, _, err := ps.handleGetWorkspace(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatal("expected no error")
	}

	text := res.Content[0].(*mcp.TextContent).Text
	if !contains(text, "Workspace: My Workspace") || !contains(text, "Ongoing: 1") || !contains(text, "Completed: 1") {
		t.Errorf("unexpected content: %s", text)
	}

	// Error case: listTasks fails
	ps.listTasks = func(ctx context.Context) ([]model.Task, error) {
		return nil, fmt.Errorf("db error")
	}
	mockTel.EXPECT().Record(gomock.Any(), gomock.Any(), int64(100), model.ActionIDMCPToolCall)
	res, _, _ = ps.handleGetWorkspace(context.Background(), nil, nil)
	if !res.IsError || !contains(res.Content[0].(*mcp.TextContent).Text, "db error") {
		t.Errorf("expected error, got: %v", res)
	}
}

func TestWorkspaceServer_HandleDownloadAttachment(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockTel := mock_telemetry.NewMockService(ctrl)
	mockStor := mock_storage.NewMockService(ctrl)
	ps := &WorkspaceServer{
		workspaceID: 100,
		userID:      "15264777",
		telemetry:   mockTel,
		storage:     mockStor,
		listTasks: func(ctx context.Context) ([]model.Task, error) {
			return []model.Task{
				{
					ID:          1,
					Attachments: []byte(`[{"id":"att-1","filename":"test.txt"}]`),
				},
			}, nil
		},
	}

	mockTel.EXPECT().Record(gomock.Any(), gomock.Any(), int64(100), model.ActionIDMCPToolCall)
	mockStor.EXPECT().Load("att-1").Return("content in base64", nil)

	params := DownloadAttachmentParams{AttachmentID: "att-1"}
	res, _, err := ps.handleDownloadAttachment(context.Background(), nil, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatal("expected no error")
	}

	text := res.Content[0].(*mcp.TextContent).Text
	if text != "content in base64" {
		t.Errorf("expected content in base64, got %s", text)
	}
}

func TestWorkspaceServer_HandleCreateTask(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockTel := mock_telemetry.NewMockService(ctrl)
	mockIdgen := mock_idgen.NewMockService(ctrl)
	ps := &WorkspaceServer{
		workspaceID: 100,
		userID:      "15264777",
		telemetry:   mockTel,
		idgen:       mockIdgen,
		bus:         eventbus.New(),
		createTask: func(ctx context.Context, task model.Task) (model.Task, error) {
			task.ID = 123
			return task, nil
		},
	}

	mockTel.EXPECT().Record(gomock.Any(), gomock.Any(), int64(100), model.ActionIDMCPToolCall)
	mockIdgen.EXPECT().NextID().Return(int64(123))

	params := CreateTaskParams{
		Title: "New Task",
		Body:  "Task Body",
	}
	res, _, err := ps.handleCreateTask(context.Background(), nil, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("expected no error, got: %s", res.Content[0].(*mcp.TextContent).Text)
	}

	text := res.Content[0].(*mcp.TextContent).Text
	if !contains(text, "task created with id=") || !contains(text, monoflake.ID(123).String()) {
		t.Errorf("unexpected content: %s", text)
	}
}

func TestWorkspaceServer_HandleUpdateTaskStatus(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockTel := mock_telemetry.NewMockService(ctrl)
	ps := &WorkspaceServer{
		workspaceID: 100,
		userID:      "15264777",
		telemetry:   mockTel,
		bus:         eventbus.New(),
		updateStatus: func(ctx context.Context, taskID int64, status string) (model.Task, error) {
			return model.Task{ID: taskID, Status: status}, nil
		},
	}

	mockTel.EXPECT().Record(gomock.Any(), gomock.Any(), int64(100), model.ActionIDMCPToolCall)

	taskIDStr := monoflake.ID(42).String()
	params := UpdateTaskStatusParams{
		TaskID: taskIDStr,
		Status: "completed",
	}
	res, _, err := ps.handleUpdateTaskStatus(context.Background(), nil, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatal("expected no error")
	}

	text := res.Content[0].(*mcp.TextContent).Text
	if !contains(text, "updated to status=completed") {
		t.Errorf("unexpected content: %s", text)
	}
}

func TestWorkspaceServer_HandleReply(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockTel := mock_telemetry.NewMockService(ctrl)
	ps := &WorkspaceServer{
		workspaceID: 100,
		userID:      "15264777",
		telemetry:   mockTel,
		reply: func(ctx context.Context, chatID string, text string, attachments []entity.Attachment, metadata any) (int64, error) {
			return 1, nil
		},
	}

	mockTel.EXPECT().Record(gomock.Any(), gomock.Any(), int64(100), model.ActionIDMCPToolCall)

	params := ReplyParams{
		ChatID: monoflake.ID(42).String(),
		Text:   "hello",
	}
	res, _, err := ps.handleReply(context.Background(), nil, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatal("expected no error")
	}
}

func TestWorkspaceServer_HandleGetTaskMessages(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockTel := mock_telemetry.NewMockService(ctrl)
	ps := &WorkspaceServer{
		workspaceID: 100,
		userID:      "15264777",
		telemetry:   mockTel,
		getTask: func(ctx context.Context, taskID int64) (model.Task, error) {
			return model.Task{
				ID: 42,
				Messages: []model.Message{
					{ID: 1001, Sender: "human", Text: "hi"},
					{ID: 1002, Sender: "agent", Text: "hello"},
				},
			}, nil
		},
	}

	mockTel.EXPECT().Record(gomock.Any(), gomock.Any(), int64(100), model.ActionIDMCPToolCall)

	params := GetTaskMessagesParams{
		TaskID: monoflake.ID(42).String(),
		Limit:  1,
		Cursor: 0,
	}
	res, _, err := ps.handleGetTaskMessages(context.Background(), nil, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatal("expected no error")
	}

	text := res.Content[0].(*mcp.TextContent).Text
	if !contains(text, `"total":2`) || !contains(text, `"text":"hi"`) {
		t.Errorf("unexpected content: %s", text)
	}
}

func TestWorkspaceServer_HandleCreateTask_Errors(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockTel := mock_telemetry.NewMockService(ctrl)
	now := time.Now()
	ps := &WorkspaceServer{
		workspaceID: 100,
		userID:      "15264777",
		telemetry:   mockTel,
		archivedAt:  &now,
	}

	// Case 1: Archived workspace
	mockTel.EXPECT().Record(gomock.Any(), gomock.Any(), int64(100), model.ActionIDMCPToolCall)
	res, _, _ := ps.handleCreateTask(context.Background(), nil, CreateTaskParams{Title: "T", Body: "B"})
	if !res.IsError || !contains(res.Content[0].(*mcp.TextContent).Text, "archived") {
		t.Errorf("expected archived error, got: %v", res)
	}

	// Case 2: Missing title
	ps.archivedAt = nil
	mockTel.EXPECT().Record(gomock.Any(), gomock.Any(), int64(100), model.ActionIDMCPToolCall)
	res, _, _ = ps.handleCreateTask(context.Background(), nil, CreateTaskParams{Body: "B"})
	if !res.IsError || !contains(res.Content[0].(*mcp.TextContent).Text, "title is required") {
		t.Errorf("expected missing title error, got: %v", res)
	}
}

func TestWorkspaceServer_HandleUpdateTaskStatus_Errors(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockTel := mock_telemetry.NewMockService(ctrl)
	now := time.Now()
	ps := &WorkspaceServer{
		workspaceID: 100,
		userID:      "15264777",
		telemetry:   mockTel,
		archivedAt:  &now,
	}

	// Case 1: Archived workspace
	mockTel.EXPECT().Record(gomock.Any(), gomock.Any(), int64(100), model.ActionIDMCPToolCall)
	res, _, _ := ps.handleUpdateTaskStatus(context.Background(), nil, UpdateTaskStatusParams{TaskID: "1", Status: "ongoing"})
	if !res.IsError || !contains(res.Content[0].(*mcp.TextContent).Text, "archived") {
		t.Errorf("expected archived error, got: %v", res)
	}

	// Case 2: Missing task_id
	ps.archivedAt = nil
	mockTel.EXPECT().Record(gomock.Any(), gomock.Any(), int64(100), model.ActionIDMCPToolCall)
	res, _, _ = ps.handleUpdateTaskStatus(context.Background(), nil, UpdateTaskStatusParams{Status: "ongoing"})
	if !res.IsError || !contains(res.Content[0].(*mcp.TextContent).Text, "task_id is required") {
		t.Errorf("expected missing task_id error, got: %v", res)
	}
}

func TestWorkspaceServer_HandleDownloadAttachment_Errors(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockTel := mock_telemetry.NewMockService(ctrl)
	ps := &WorkspaceServer{
		workspaceID: 100,
		userID:      "15264777",
		telemetry:   mockTel,
	}

	mockTel.EXPECT().Record(gomock.Any(), gomock.Any(), int64(100), model.ActionIDMCPToolCall)
	res, _, _ := ps.handleDownloadAttachment(context.Background(), nil, DownloadAttachmentParams{})
	if !res.IsError || !contains(res.Content[0].(*mcp.TextContent).Text, "required") {
		t.Errorf("expected missing attachment_id error, got: %v", res)
	}
}

func TestWorkspaceServer_HandleReply_Errors(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockTel := mock_telemetry.NewMockService(ctrl)
	now := time.Now()
	ps := &WorkspaceServer{
		workspaceID: 100,
		userID:      "15264777",
		telemetry:   mockTel,
		archivedAt:  &now,
	}

	// Case 1: Archived
	mockTel.EXPECT().Record(gomock.Any(), gomock.Any(), int64(100), model.ActionIDMCPToolCall)
	res, _, _ := ps.handleReply(context.Background(), nil, ReplyParams{ChatID: "1", Text: "hi"})
	if !res.IsError || !contains(res.Content[0].(*mcp.TextContent).Text, "archived") {
		t.Errorf("expected archived error, got: %v", res)
	}

	// Case 2: Missing content
	ps.archivedAt = nil
	mockTel.EXPECT().Record(gomock.Any(), gomock.Any(), int64(100), model.ActionIDMCPToolCall)
	res, _, _ = ps.handleReply(context.Background(), nil, ReplyParams{ChatID: "1"})
	if !res.IsError || !contains(res.Content[0].(*mcp.TextContent).Text, "required") {
		t.Errorf("expected missing content error, got: %v", res)
	}
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
