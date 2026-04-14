package mcp

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
	mock_idgen "github.com/agentrq/agentrq/backend/internal/service/mocks/idgen"
	mock_pubsub "github.com/agentrq/agentrq/backend/internal/service/mocks/pubsub"
	mock_storage "github.com/agentrq/agentrq/backend/internal/service/mocks/storage"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
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

	mockPS := mock_pubsub.NewMockService(ctrl)
	ps := &WorkspaceServer{
		workspaceID: 100,
		userID:      monoflake.ID(15264777).String(),
		name:        "My Workspace",
		description: "My Description",
		pubsub:      mockPS,
		listTasks: func(ctx context.Context) ([]model.Task, error) {
			return []model.Task{
				{Status: "ongoing"},
				{Status: "completed"},
			}, nil
		},
	}

	mockPS.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(&pubsub.PublishResponse{}, nil).AnyTimes()

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
	res, _, _ = ps.handleGetWorkspace(context.Background(), nil, nil)
	if !res.IsError || !contains(res.Content[0].(*mcp.TextContent).Text, "db error") {
		t.Errorf("expected error, got: %v", res)
	}
}

func TestWorkspaceServer_HandleDownloadAttachment(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockPS := mock_pubsub.NewMockService(ctrl)
	mockStor := mock_storage.NewMockService(ctrl)
	ps := &WorkspaceServer{
		workspaceID: 100,
		userID:      monoflake.ID(15264777).String(),
		pubsub:      mockPS,
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

	mockPS.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(&pubsub.PublishResponse{}, nil).AnyTimes()
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

	mockPS := mock_pubsub.NewMockService(ctrl)
	mockIdgen := mock_idgen.NewMockService(ctrl)
	ps := &WorkspaceServer{
		workspaceID: 100,
		userID:      monoflake.ID(15264777).String(),
		pubsub:      mockPS,
		idgen:       mockIdgen,
		bus:         eventbus.New(),
		createTask: func(ctx context.Context, task model.Task) (model.Task, error) {
			task.ID = 123
			return task, nil
		},
	}

	mockPS.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(&pubsub.PublishResponse{}, nil).AnyTimes()
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

	mockPS := mock_pubsub.NewMockService(ctrl)
	ps := &WorkspaceServer{
		workspaceID: 100,
		userID:      monoflake.ID(15264777).String(),
		pubsub:      mockPS,
		bus:         eventbus.New(),
		updateStatus: func(ctx context.Context, taskID int64, status string) (model.Task, error) {
			return model.Task{ID: taskID, Status: status}, nil
		},
	}

	mockPS.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(&pubsub.PublishResponse{}, nil).AnyTimes()

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

	mockPS := mock_pubsub.NewMockService(ctrl)
	ps := &WorkspaceServer{
		workspaceID: 100,
		userID:      monoflake.ID(15264777).String(),
		pubsub:      mockPS,
		reply: func(ctx context.Context, chatID string, text string, attachments []entity.Attachment, metadata any) (int64, error) {
			return 1, nil
		},
	}

	mockPS.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(&pubsub.PublishResponse{}, nil).AnyTimes()

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

	mockPS := mock_pubsub.NewMockService(ctrl)
	ps := &WorkspaceServer{
		workspaceID: 100,
		userID:      monoflake.ID(15264777).String(),
		pubsub:      mockPS,
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

	mockPS.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(&pubsub.PublishResponse{}, nil).AnyTimes()

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

func TestWorkspaceServer_HandleGetTaskMessages_FiltersPermissionRequests(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockPS := mock_pubsub.NewMockService(ctrl)
	ps := &WorkspaceServer{
		workspaceID: 100,
		userID:      monoflake.ID(15264777).String(),
		pubsub:      mockPS,
		getTask: func(ctx context.Context, taskID int64) (model.Task, error) {
			return model.Task{
				ID: 42,
				Messages: []model.Message{
					{ID: 1001, Sender: "human", Text: "please run the tests"},
					{ID: 1002, Sender: "agent", Text: "Permission requested for bash: run tests", Metadata: []byte(`{"type":"permission_request","request_id":"req-1","tool_name":"bash","status":"allow"}`)},
					{ID: 1003, Sender: "agent", Text: "tests passed"},
				},
			}, nil
		},
	}

	mockPS.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(&pubsub.PublishResponse{}, nil).AnyTimes()

	params := GetTaskMessagesParams{
		TaskID: monoflake.ID(42).String(),
		Limit:  10,
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
	// Should only see 2 messages (the permission_request one is filtered)
	if !contains(text, `"total":2`) {
		t.Errorf("expected total=2 (permission_request filtered), got: %s", text)
	}
	if contains(text, "permission_request") || contains(text, "Permission requested") {
		t.Errorf("expected permission_request messages to be filtered out, got: %s", text)
	}
	if !contains(text, "please run the tests") || !contains(text, "tests passed") {
		t.Errorf("expected regular messages to remain, got: %s", text)
	}
}

func TestWorkspaceServer_HandleCreateTask_Errors(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockPS := mock_pubsub.NewMockService(ctrl)
	now := time.Now()
	ps := &WorkspaceServer{
		workspaceID: 100,
		userID:      monoflake.ID(15264777).String(),
		pubsub:      mockPS,
		archivedAt:  &now,
	}

	mockPS.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(&pubsub.PublishResponse{}, nil).AnyTimes()

	// Case 1: Archived workspace
	res, _, _ := ps.handleCreateTask(context.Background(), nil, CreateTaskParams{Title: "T", Body: "B"})
	if !res.IsError || !contains(res.Content[0].(*mcp.TextContent).Text, "archived") {
		t.Errorf("expected archived error, got: %v", res)
	}

	// Case 2: Missing title
	ps.archivedAt = nil
	res, _, _ = ps.handleCreateTask(context.Background(), nil, CreateTaskParams{Body: "B"})
	if !res.IsError || !contains(res.Content[0].(*mcp.TextContent).Text, "title is required") {
		t.Errorf("expected missing title error, got: %v", res)
	}
}

func TestWorkspaceServer_HandleUpdateTaskStatus_Errors(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockPS := mock_pubsub.NewMockService(ctrl)
	now := time.Now()
	ps := &WorkspaceServer{
		workspaceID: 100,
		userID:      monoflake.ID(15264777).String(),
		pubsub:      mockPS,
		archivedAt:  &now,
	}

	mockPS.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(&pubsub.PublishResponse{}, nil).AnyTimes()

	// Case 1: Archived workspace
	res, _, _ := ps.handleUpdateTaskStatus(context.Background(), nil, UpdateTaskStatusParams{TaskID: "1", Status: "ongoing"})
	if !res.IsError || !contains(res.Content[0].(*mcp.TextContent).Text, "archived") {
		t.Errorf("expected archived error, got: %v", res)
	}

	// Case 2: Missing task_id
	ps.archivedAt = nil
	res, _, _ = ps.handleUpdateTaskStatus(context.Background(), nil, UpdateTaskStatusParams{Status: "ongoing"})
	if !res.IsError || !contains(res.Content[0].(*mcp.TextContent).Text, "task_id is required") {
		t.Errorf("expected missing task_id error, got: %v", res)
	}
}

func TestWorkspaceServer_HandleDownloadAttachment_Errors(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockPS := mock_pubsub.NewMockService(ctrl)
	ps := &WorkspaceServer{
		workspaceID: 100,
		userID:      monoflake.ID(15264777).String(),
		pubsub:      mockPS,
	}

	mockPS.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(&pubsub.PublishResponse{}, nil).AnyTimes()

	res, _, _ := ps.handleDownloadAttachment(context.Background(), nil, DownloadAttachmentParams{})
	if !res.IsError || !contains(res.Content[0].(*mcp.TextContent).Text, "required") {
		t.Errorf("expected missing attachment_id error, got: %v", res)
	}
}

func TestWorkspaceServer_HandleReply_Errors(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockPS := mock_pubsub.NewMockService(ctrl)
	now := time.Now()
	ps := &WorkspaceServer{
		workspaceID: 100,
		userID:      monoflake.ID(15264777).String(),
		pubsub:      mockPS,
		archivedAt:  &now,
	}

	mockPS.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(&pubsub.PublishResponse{}, nil).AnyTimes()

	// Case 1: Archived
	res, _, _ := ps.handleReply(context.Background(), nil, ReplyParams{ChatID: "1", Text: "hi"})
	if !res.IsError || !contains(res.Content[0].(*mcp.TextContent).Text, "archived") {
		t.Errorf("expected archived error, got: %v", res)
	}

	// Case 2: Missing content
	ps.archivedAt = nil
	res, _, _ = ps.handleReply(context.Background(), nil, ReplyParams{ChatID: "1"})
	if !res.IsError || !contains(res.Content[0].(*mcp.TextContent).Text, "required") {
		t.Errorf("expected missing content error, got: %v", res)
	}
}

func TestWorkspaceServer_HandleCreateTask_WithCronSchedule(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockPS := mock_pubsub.NewMockService(ctrl)
	mockIdgen := mock_idgen.NewMockService(ctrl)

	var capturedTask model.Task
	ps := &WorkspaceServer{
		workspaceID: 100,
		userID:      monoflake.ID(15264777).String(),
		pubsub:      mockPS,
		idgen:       mockIdgen,
		bus:         eventbus.New(),
		createTask: func(ctx context.Context, task model.Task) (model.Task, error) {
			capturedTask = task
			task.ID = 456
			return task, nil
		},
	}

	mockPS.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(&pubsub.PublishResponse{}, nil).AnyTimes()
	mockIdgen.EXPECT().NextID().Return(int64(456))

	params := CreateTaskParams{
		Title:        "Daily Report",
		Body:         "Generate daily report",
		CronSchedule: "0 9 * * *",
	}
	res, _, err := ps.handleCreateTask(context.Background(), nil, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("expected no error, got: %s", res.Content[0].(*mcp.TextContent).Text)
	}

	if capturedTask.Status != "cron" {
		t.Errorf("expected status=cron, got: %s", capturedTask.Status)
	}
	if capturedTask.CronSchedule != "0 9 * * *" {
		t.Errorf("expected cron_schedule='0 9 * * *', got: %s", capturedTask.CronSchedule)
	}

	text := res.Content[0].(*mcp.TextContent).Text
	if !contains(text, "task created with id=") {
		t.Errorf("unexpected content: %s", text)
	}
}

func TestWorkspaceServer_HandleCreateTask_InvalidCron(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockPS := mock_pubsub.NewMockService(ctrl)
	mockIdgen := mock_idgen.NewMockService(ctrl)
	ps := &WorkspaceServer{
		workspaceID: 100,
		userID:      monoflake.ID(15264777).String(),
		pubsub:      mockPS,
		idgen:       mockIdgen,
		bus:         eventbus.New(),
	}

	mockPS.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(&pubsub.PublishResponse{}, nil).AnyTimes()

	cases := []struct {
		name     string
		schedule string
		errFrag  string
	}{
		{
			name:     "every minute wildcard",
			schedule: "* * * * *",
			errFrag:  "granularity too fine",
		},
		{
			name:     "every 5 minutes step",
			schedule: "*/5 * * * *",
			errFrag:  "granularity too fine",
		},
		{
			name:     "comma list minutes",
			schedule: "0,30 * * * *",
			errFrag:  "granularity too fine",
		},
		{
			name:     "minute range",
			schedule: "0-30 * * * *",
			errFrag:  "granularity too fine",
		},
		{
			name:     "invalid syntax",
			schedule: "not-a-cron",
			errFrag:  "cron schedule must have exactly 5 fields",
		},
		{
			name:     "minute out of range",
			schedule: "60 * * * *",
			errFrag:  "minute field must be a valid integer",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			params := CreateTaskParams{
				Title:        "Test",
				Body:         "Body",
				CronSchedule: tc.schedule,
			}
			res, _, err := ps.handleCreateTask(context.Background(), nil, params)
			if err != nil {
				t.Fatalf("unexpected Go error: %v", err)
			}
			if !res.IsError {
				t.Fatalf("expected error result for schedule %q", tc.schedule)
			}
			text := res.Content[0].(*mcp.TextContent).Text
			if !contains(text, tc.errFrag) {
				t.Errorf("expected error containing %q, got: %s", tc.errFrag, text)
			}
		})
	}
}

func TestValidateCronGranularity(t *testing.T) {
	validCases := []string{
		"0 * * * *",   // every hour at :00
		"30 * * * *",  // every hour at :30
		"0 9 * * *",   // daily at 9am
		"0 9 * * 1",   // weekly Monday at 9am
		"0 9 1 * *",   // monthly on the 1st at 9am
		"59 23 * * *", // daily at 23:59
		"0 */2 * * *", // every 2 hours at :00
		"30 9 * * 1-5",// weekdays at 9:30
	}

	for _, s := range validCases {
		if err := validateCronGranularity(s); err != nil {
			t.Errorf("expected valid for %q, got error: %v", s, err)
		}
	}

	invalidCases := []struct {
		schedule string
		errFrag  string
	}{
		{"* * * * *", "granularity too fine"},
		{"*/5 * * * *", "granularity too fine"},
		{"*/1 * * * *", "granularity too fine"},
		{"0,30 * * * *", "granularity too fine"},
		{"0-5 * * * *", "granularity too fine"},
		{"60 * * * *", "must be a valid integer"},
		{"-1 * * * *", "granularity too fine"},    // "-" in minute field caught as range-like
		{"abc * * * *", "must be a valid integer"},
		{"not a cron at all", "must be a valid integer"}, // 5 tokens; "not" fails Atoi
	}

	for _, tc := range invalidCases {
		if err := validateCronGranularity(tc.schedule); err == nil {
			t.Errorf("expected error for %q, got nil", tc.schedule)
		} else if !contains(err.Error(), tc.errFrag) {
			t.Errorf("expected error containing %q for %q, got: %v", tc.errFrag, tc.schedule, err)
		}
	}
}

func TestWorkspaceServer_HandleGetNextTask(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockPS := mock_pubsub.NewMockService(ctrl)
	ps := &WorkspaceServer{
		workspaceID: 100,
		userID:      monoflake.ID(15264777).String(),
		pubsub:      mockPS,
	}

	mockPS.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(&pubsub.PublishResponse{}, nil).AnyTimes()

	// Case 1: Success
	ps.getNextTask = func(ctx context.Context) (model.Task, error) {
		return model.Task{
			ID:    42,
			Title: "Test Task",
			Body:  "Test Body",
		}, nil
	}

	res, _, err := ps.handleGetNextTask(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatal("expected no error")
	}
	text := res.Content[0].(*mcp.TextContent).Text
	if !contains(text, "Next assigned task:") || !contains(text, "ID: "+monoflake.ID(42).String()) || !contains(text, "Title: Test Task") {
		t.Errorf("unexpected content: %s", text)
	}

	// Case 1b: Success with attachments
	ps.getNextTask = func(ctx context.Context) (model.Task, error) {
		return model.Task{
			ID:          43,
			Title:       "Task with Attachments",
			Body:        "Body",
			Attachments: []byte(`[{"id":"att-1","filename":"file.txt"}]`),
		}, nil
	}
	res, _, _ = ps.handleGetNextTask(context.Background(), nil, nil)
	text = res.Content[0].(*mcp.TextContent).Text
	if !contains(text, "file.txt") {
		t.Errorf("expected attachments to be formatted, got: %s", text)
	}

	// Case 2: Not Found
	ps.getNextTask = func(ctx context.Context) (model.Task, error) {
		return model.Task{}, base.ErrNotFound
	}
	res, _, _ = ps.handleGetNextTask(context.Background(), nil, nil)
	if res.IsError {
		t.Fatal("expected no error result for NotFound")
	}
	text = res.Content[0].(*mcp.TextContent).Text
	if text != "no pending tasks exist" {
		t.Errorf("expected 'no pending tasks exist', got: %s", text)
	}

	// Case 3: Error
	ps.getNextTask = func(ctx context.Context) (model.Task, error) {
		return model.Task{}, fmt.Errorf("db error")
	}
	res, _, _ = ps.handleGetNextTask(context.Background(), nil, nil)
	if !res.IsError || !contains(res.Content[0].(*mcp.TextContent).Text, "db error") {
		t.Errorf("expected error result, got: %v", res)
	}
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
