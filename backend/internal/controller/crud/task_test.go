package crud

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/golang/mock/gomock"
)

// ── CreateTask ────────────────────────────────────────────────────────────────

func TestCreateTask_Success(t *testing.T) {
	e := newTestController(t)

	now := time.Now()
	created := model.Task{ID: 42, WorkspaceID: 1, Title: "My task", Status: "notstarted", CreatedAt: now, UpdatedAt: now}

	e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(activeWorkspace(), nil)
	e.idgen.EXPECT().NextID().Return(int64(42))
	e.repo.EXPECT().CreateTask(gomock.Any(), gomock.Any()).Return(created, nil)

	resp, err := e.controller.CreateTask(context.Background(), entity.CreateTaskRequest{
		UserID: testUserIDStr,
		Task:   entity.Task{WorkspaceID: 1, Title: "My task"},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Task.ID != 42 {
		t.Errorf("expected task ID 42, got %d", resp.Task.ID)
	}
	if resp.Task.Status != "notstarted" {
		t.Errorf("expected status notstarted, got %s", resp.Task.Status)
	}
}

func TestCreateTask_AgentInheritYOLO(t *testing.T) {
	e := newTestController(t)

	ws := activeWorkspace()
	ws.AllowAllCommands = true

	created := model.Task{ID: 43, WorkspaceID: 1, CreatedBy: "agent", AllowAllCommands: true}

	e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(ws, nil)
	e.idgen.EXPECT().NextID().Return(int64(43))
	e.repo.EXPECT().CreateTask(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, m model.Task) (model.Task, error) {
		if !m.AllowAllCommands {
			return model.Task{}, fmt.Errorf("expected AllowAllCommands to be inherited from workspace")
		}
		return created, nil
	})

	resp, err := e.controller.CreateTask(context.Background(), entity.CreateTaskRequest{
		UserID: testUserIDStr,
		Task:   entity.Task{WorkspaceID: 1, Title: "Agent Task", CreatedBy: "agent", AllowAllCommands: false},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.Task.AllowAllCommands {
		t.Errorf("expected task to have AllowAllCommands=true")
	}
}

func TestCreateTask_HumanOverrideYOLO(t *testing.T) {
	e := newTestController(t)

	ws := activeWorkspace()
	ws.AllowAllCommands = true

	// Human explicitly sets AllowAllCommands to false
	created := model.Task{ID: 44, WorkspaceID: 1, CreatedBy: "human", AllowAllCommands: false}

	e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(ws, nil)
	e.idgen.EXPECT().NextID().Return(int64(44))
	e.repo.EXPECT().CreateTask(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, m model.Task) (model.Task, error) {
		if m.AllowAllCommands {
			return model.Task{}, fmt.Errorf("expected AllowAllCommands to stay false as requested by human")
		}
		return created, nil
	})

	resp, err := e.controller.CreateTask(context.Background(), entity.CreateTaskRequest{
		UserID: testUserIDStr,
		Task:   entity.Task{WorkspaceID: 1, Title: "Human Task", CreatedBy: "human", AllowAllCommands: false},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Task.AllowAllCommands {
		t.Errorf("expected task to have AllowAllCommands=false")
	}
}

func TestCreateTask_AgentAppendLoopNote(t *testing.T) {
	e := newTestController(t)

	ws := activeWorkspace()
	ws.SelfLearningLoopNote = "Be concise."

	created := model.Task{ID: 45, WorkspaceID: 1, Assignee: "agent", Body: "Hello world.\n\nBe concise."}

	e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(ws, nil)
	e.idgen.EXPECT().NextID().Return(int64(45))
	e.repo.EXPECT().CreateTask(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, m model.Task) (model.Task, error) {
		if m.Body != "Hello world.\n\nBe concise." {
			return model.Task{}, fmt.Errorf("expected Body to have loop note appended, got %q", m.Body)
		}
		return created, nil
	})

	resp, err := e.controller.CreateTask(context.Background(), entity.CreateTaskRequest{
		UserID: testUserIDStr,
		Task:   entity.Task{WorkspaceID: 1, Title: "Agent Task", Body: "Hello world.", Assignee: "agent"},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Task.Body != "Hello world.\n\nBe concise." {
		t.Errorf("expected task body with note")
	}
}

func TestCreateTask_EmptyTitle(t *testing.T) {
	e := newTestController(t)

	e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(activeWorkspace(), nil)

	_, err := e.controller.CreateTask(context.Background(), entity.CreateTaskRequest{
		UserID: testUserIDStr,
		Task:   entity.Task{WorkspaceID: 1, Title: ""},
	})

	if err == nil || err.Error() != "title is required" {
		t.Fatalf("expected 'title is required' error, got %v", err)
	}
}

func TestCreateTask_ArchivedWorkspace(t *testing.T) {
	e := newTestController(t)

	archived := activeWorkspace()
	archivedAt := time.Now()
	archived.ArchivedAt = &archivedAt

	e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(archived, nil)

	_, err := e.controller.CreateTask(context.Background(), entity.CreateTaskRequest{
		UserID: testUserIDStr,
		Task:   entity.Task{WorkspaceID: 1, Title: "t"},
	})

	if err == nil {
		t.Fatal("expected error for archived workspace")
	}
}

func TestCreateTask_InvalidCronSchedule(t *testing.T) {
	e := newTestController(t)

	e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(activeWorkspace(), nil)

	_, err := e.controller.CreateTask(context.Background(), entity.CreateTaskRequest{
		UserID: testUserIDStr,
		Task:   entity.Task{WorkspaceID: 1, Title: "t", Status: "cron", CronSchedule: "not-a-cron"},
	})

	if err == nil {
		t.Fatal("expected error for invalid cron schedule")
	}
}

func TestCreateTask_ValidCronSchedule(t *testing.T) {
	e := newTestController(t)

	created := model.Task{ID: 5, WorkspaceID: 1, Title: "t", Status: "cron"}

	e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(activeWorkspace(), nil)
	e.idgen.EXPECT().NextID().Return(int64(5))
	e.repo.EXPECT().CreateTask(gomock.Any(), gomock.Any()).Return(created, nil)

	resp, err := e.controller.CreateTask(context.Background(), entity.CreateTaskRequest{
		UserID: testUserIDStr,
		Task:   entity.Task{WorkspaceID: 1, Title: "t", Status: "cron", CronSchedule: "0 9 * * 1"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Task.Status != "cron" {
		t.Errorf("expected cron status, got %s", resp.Task.Status)
	}
}

func TestCreateTask_RepositoryError(t *testing.T) {
	e := newTestController(t)

	e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(activeWorkspace(), nil)
	e.idgen.EXPECT().NextID().Return(int64(1))
	e.repo.EXPECT().CreateTask(gomock.Any(), gomock.Any()).Return(model.Task{}, fmt.Errorf("db error"))

	_, err := e.controller.CreateTask(context.Background(), entity.CreateTaskRequest{
		UserID: testUserIDStr,
		Task:   entity.Task{WorkspaceID: 1, Title: "t"},
	})
	if err == nil {
		t.Fatal("expected error from repository")
	}
}

// ── GetTask ───────────────────────────────────────────────────────────────────

func TestGetTask_Success(t *testing.T) {
	e := newTestController(t)

	task := model.Task{ID: 10, WorkspaceID: 1, Title: "hello", Status: "ongoing"}
	e.repo.EXPECT().GetTask(gomock.Any(), int64(1), int64(10), testUserID).Return(task, nil)

	resp, err := e.controller.GetTask(context.Background(), entity.GetTaskRequest{WorkspaceID: 1, TaskID: 10, UserID: testUserIDStr})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Task.Title != "hello" {
		t.Errorf("expected title hello, got %s", resp.Task.Title)
	}
}

func TestGetTask_NotFound(t *testing.T) {
	e := newTestController(t)

	e.repo.EXPECT().GetTask(gomock.Any(), int64(1), int64(99), testUserID).Return(model.Task{}, fmt.Errorf("not found"))

	_, err := e.controller.GetTask(context.Background(), entity.GetTaskRequest{WorkspaceID: 1, TaskID: 99, UserID: testUserIDStr})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestGetTask_Full(t *testing.T) {
	e := newTestController(t)

	now := time.Now()
	task := model.Task{
		ID:          10,
		WorkspaceID: 1,
		Title:       "t",
		Body:        "b",
		Status:      "ongoing",
		CreatedAt:   now,
		UpdatedAt:   now,
		Attachments: []byte(`[{"id":"a1"}]`),
		Messages: []model.Message{
			{ID: 101, Text: "m1", Attachments: []byte(`[{"id":"a2"}]`)},
		},
	}
	e.repo.EXPECT().GetTask(gomock.Any(), int64(1), int64(10), testUserID).Return(task, nil)

	resp, err := e.controller.GetTask(context.Background(), entity.GetTaskRequest{WorkspaceID: 1, TaskID: 10, UserID: testUserIDStr})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Task.Attachments) != 1 || len(resp.Task.Messages) != 1 {
		t.Errorf("expected attachments and messages to be mapped")
	}
}

// ── ListTasks ─────────────────────────────────────────────────────────────────

func TestListTasks_Success(t *testing.T) {
	e := newTestController(t)

	tasks := []model.Task{
		{ID: 1, Title: "a"},
		{ID: 2, Title: "b"},
	}
	e.repo.EXPECT().ListTasks(gomock.Any(), gomock.Any(), testUserID).Return(tasks, nil)

	resp, err := e.controller.ListTasks(context.Background(), entity.ListTasksRequest{WorkspaceID: 1, UserID: testUserIDStr})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Tasks) != 2 {
		t.Errorf("expected 2 tasks, got %d", len(resp.Tasks))
	}
}

func TestListTasks_Empty(t *testing.T) {
	e := newTestController(t)

	e.repo.EXPECT().ListTasks(gomock.Any(), gomock.Any(), testUserID).Return([]model.Task{}, nil)

	resp, err := e.controller.ListTasks(context.Background(), entity.ListTasksRequest{WorkspaceID: 1, UserID: testUserIDStr})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Tasks) != 0 {
		t.Errorf("expected 0 tasks")
	}
}

// ── RespondToTask ─────────────────────────────────────────────────────────────

func TestRespondToTask_Allow(t *testing.T) {
	e := newTestController(t)

	task := model.Task{ID: 10, WorkspaceID: 1, Status: "notstarted"}
	updated := model.Task{ID: 10, WorkspaceID: 1, Status: "ongoing"}

	e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(activeWorkspace(), nil)
	e.repo.EXPECT().GetTask(gomock.Any(), int64(1), int64(10), testUserID).Return(task, nil)
	e.repo.EXPECT().ListTasks(gomock.Any(), gomock.Any(), testUserID).Return([]model.Task{task}, nil)
	e.idgen.EXPECT().NextID().Return(int64(100))
	e.repo.EXPECT().CreateMessage(gomock.Any(), gomock.Any()).Return(nil)
	e.repo.EXPECT().UpdateTask(gomock.Any(), gomock.Any()).Return(updated, nil)
	e.repo.EXPECT().GetTask(gomock.Any(), int64(1), int64(10), testUserID).Return(updated, nil)
	e.storage.EXPECT().Save("att1", "data1").Return(nil)

	resp, err := e.controller.RespondToTask(context.Background(), entity.RespondToTaskRequest{
		WorkspaceID: 1, TaskID: 10, Action: "allow", UserID: testUserIDStr,
		Attachments: []entity.Attachment{{ID: "att1", Data: "data1"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Task.Status != "ongoing" {
		t.Errorf("expected status ongoing, got %s", resp.Task.Status)
	}
}

func TestRespondToTask_AllowAll(t *testing.T) {
	e := newTestController(t)

	task := model.Task{ID: 10, WorkspaceID: 1, Status: "notstarted"}
	updated := model.Task{ID: 10, WorkspaceID: 1, Status: "ongoing"}

	e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(activeWorkspace(), nil)
	e.repo.EXPECT().GetTask(gomock.Any(), int64(1), int64(10), testUserID).Return(task, nil)
	e.repo.EXPECT().ListTasks(gomock.Any(), gomock.Any(), testUserID).Return([]model.Task{task}, nil)
	e.idgen.EXPECT().NextID().Return(int64(100))
	e.repo.EXPECT().CreateMessage(gomock.Any(), gomock.Any()).Return(nil)
	e.repo.EXPECT().UpdateTask(gomock.Any(), gomock.Any()).Return(updated, nil)
	e.repo.EXPECT().GetTask(gomock.Any(), int64(1), int64(10), testUserID).Return(updated, nil)

	resp, err := e.controller.RespondToTask(context.Background(), entity.RespondToTaskRequest{
		WorkspaceID: 1, TaskID: 10, Action: "allow_all", UserID: testUserIDStr,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Task.Status != "ongoing" {
		t.Errorf("expected status ongoing")
	}
}

func TestRespondToTask_Reject(t *testing.T) {
	e := newTestController(t)

	task := model.Task{ID: 10, WorkspaceID: 1, Status: "notstarted"}
	updated := model.Task{ID: 10, WorkspaceID: 1, Status: "rejected"}

	e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(activeWorkspace(), nil)
	e.repo.EXPECT().GetTask(gomock.Any(), int64(1), int64(10), testUserID).Return(task, nil)
	e.idgen.EXPECT().NextID().Return(int64(100))
	e.repo.EXPECT().CreateMessage(gomock.Any(), gomock.Any()).Return(nil)
	e.repo.EXPECT().UpdateTask(gomock.Any(), gomock.Any()).Return(updated, nil)
	e.repo.EXPECT().GetTask(gomock.Any(), int64(1), int64(10), testUserID).Return(updated, nil)

	resp, err := e.controller.RespondToTask(context.Background(), entity.RespondToTaskRequest{
		WorkspaceID: 1, TaskID: 10, Action: "reject", UserID: testUserIDStr,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Task.Status != "rejected" {
		t.Errorf("expected status rejected, got %s", resp.Task.Status)
	}
}

// ── UpdateTaskStatus ──────────────────────────────────────────────────────────

func TestUpdateTaskStatus_Success(t *testing.T) {
	e := newTestController(t)

	task := model.Task{ID: 10, WorkspaceID: 1, Status: "ongoing"}
	updated := model.Task{ID: 10, WorkspaceID: 1, Status: "completed"}

	e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(activeWorkspace(), nil)
	e.repo.EXPECT().GetTask(gomock.Any(), int64(1), int64(10), testUserID).Return(task, nil)
	e.repo.EXPECT().UpdateTask(gomock.Any(), gomock.Any()).Return(updated, nil)

	resp, err := e.controller.UpdateTaskStatus(context.Background(), entity.UpdateTaskStatusRequest{
		WorkspaceID: 1, TaskID: 10, Status: "completed", UserID: testUserIDStr,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Task.Status != "completed" {
		t.Errorf("expected status completed, got %s", resp.Task.Status)
	}
}

func TestUpdateTaskStatus_CronRejected(t *testing.T) {
	e := newTestController(t)

	task := model.Task{ID: 10, WorkspaceID: 1, Status: "cron"}
	e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(activeWorkspace(), nil)
	e.repo.EXPECT().GetTask(gomock.Any(), int64(1), int64(10), testUserID).Return(task, nil)

	_, err := e.controller.UpdateTaskStatus(context.Background(), entity.UpdateTaskStatusRequest{
		WorkspaceID: 1, TaskID: 10, Status: "ongoing", UserID: testUserIDStr,
	})
	if err == nil {
		t.Fatal("expected error for chronic task")
	}
}

func TestUpdateTaskStatus_OngoingConflict(t *testing.T) {
	e := newTestController(t)

	task := model.Task{ID: 10, WorkspaceID: 1, Status: "notstarted"}
	ongoing := model.Task{ID: 11, WorkspaceID: 1, Status: "ongoing"}

	e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(activeWorkspace(), nil)
	e.repo.EXPECT().GetTask(gomock.Any(), int64(1), int64(10), testUserID).Return(task, nil)
	e.repo.EXPECT().ListTasks(gomock.Any(), gomock.Any(), testUserID).Return([]model.Task{ongoing}, nil)

	_, err := e.controller.UpdateTaskStatus(context.Background(), entity.UpdateTaskStatusRequest{
		WorkspaceID: 1, TaskID: 10, Status: "ongoing", UserID: testUserIDStr,
	})
	if err == nil || !strings.Contains(err.Error(), "another task is already ongoing") {
		t.Fatalf("expected ongoing conflict error, got %v", err)
	}
}

func TestUpdateTaskStatus_ToBlocked(t *testing.T) {
	e := newTestController(t)

	task := model.Task{ID: 10, WorkspaceID: 1, Status: "ongoing"}
	updated := model.Task{ID: 10, WorkspaceID: 1, Status: "blocked"}

	e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(activeWorkspace(), nil)
	e.repo.EXPECT().GetTask(gomock.Any(), int64(1), int64(10), testUserID).Return(task, nil)
	e.repo.EXPECT().UpdateTask(gomock.Any(), gomock.Any()).Return(updated, nil)
	// Default pubsub expectation is already in newTestController

	resp, err := e.controller.UpdateTaskStatus(context.Background(), entity.UpdateTaskStatusRequest{
		WorkspaceID: 1, TaskID: 10, Status: "blocked", UserID: testUserIDStr,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Task.Status != "blocked" {
		t.Errorf("expected status 'blocked', got %s", resp.Task.Status)
	}
}

func TestUpdateTaskStatus_InvalidStatus(t *testing.T) {
	e := newTestController(t)

	task := model.Task{ID: 10, WorkspaceID: 1, Status: "notstarted"}
	e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(activeWorkspace(), nil)
	e.repo.EXPECT().GetTask(gomock.Any(), int64(1), int64(10), testUserID).Return(task, nil)

	_, err := e.controller.UpdateTaskStatus(context.Background(), entity.UpdateTaskStatusRequest{
		WorkspaceID: 1, TaskID: 10, Status: "bad_status", UserID: testUserIDStr,
	})
	if err == nil || !strings.Contains(err.Error(), "invalid task status") {
		t.Fatalf("expected invalid status error, got %v", err)
	}
}

func TestUpdateTaskOrder_Success(t *testing.T) {
	e := newTestController(t)

	task := model.Task{ID: 10, WorkspaceID: 1, SortOrder: 1.0}
	updated := model.Task{ID: 10, WorkspaceID: 1, SortOrder: 2.5}

	e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(activeWorkspace(), nil)
	e.repo.EXPECT().GetTask(gomock.Any(), int64(1), int64(10), testUserID).Return(task, nil)
	e.repo.EXPECT().UpdateTask(gomock.Any(), gomock.Any()).Return(updated, nil)

	resp, err := e.controller.UpdateTaskOrder(context.Background(), entity.UpdateTaskOrderRequest{
		WorkspaceID: 1, TaskID: 10, SortOrder: 2.5, UserID: testUserIDStr,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Task.SortOrder != 2.5 {
		t.Errorf("expected sort order 2.5, got %f", resp.Task.SortOrder)
	}
}

// ── UpdateTaskAssignee ────────────────────────────────────────────────────────

func TestUpdateTaskAssignee_AgentAppendLoopNote(t *testing.T) {
	e := newTestController(t)

	ws := activeWorkspace()
	ws.SelfLearningLoopNote = "Be concise."

	task := model.Task{ID: 10, WorkspaceID: 1, Assignee: "human", Body: "Original body."}
	updated := model.Task{ID: 10, WorkspaceID: 1, Assignee: "agent", Body: "Original body.\n\nBe concise."}

	e.repo.EXPECT().GetTask(gomock.Any(), int64(1), int64(10), testUserID).Return(task, nil)
	e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(ws, nil)
	
	e.repo.EXPECT().UpdateTask(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, m model.Task) (model.Task, error) {
		if m.Body != "Original body.\n\nBe concise." {
			return model.Task{}, fmt.Errorf("expected Body to have loop note appended, got %q", m.Body)
		}
		return updated, nil
	})

	resp, err := e.controller.UpdateTaskAssignee(context.Background(), entity.UpdateTaskAssigneeRequest{
		WorkspaceID: 1, TaskID: 10, Assignee: "agent", UserID: testUserIDStr,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Task.Assignee != "agent" {
		t.Errorf("expected assignee agent, got %s", resp.Task.Assignee)
	}
	if resp.Task.Body != "Original body.\n\nBe concise." {
		t.Errorf("expected task body to be updated")
	}
}

// ── ReplyToTask ───────────────────────────────────────────────────────────────

func TestReplyToTask_Success(t *testing.T) {
	e := newTestController(t)

	task := model.Task{ID: 10, WorkspaceID: 1, Status: "ongoing"}
	e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(activeWorkspace(), nil)
	e.repo.EXPECT().GetTask(gomock.Any(), int64(1), int64(10), testUserID).Return(task, nil)
	e.idgen.EXPECT().NextID().Return(int64(101))
	e.repo.EXPECT().CreateMessage(gomock.Any(), gomock.Any()).Return(nil)
	e.repo.EXPECT().UpdateTask(gomock.Any(), gomock.Any()).Return(task, nil)
	e.repo.EXPECT().GetTask(gomock.Any(), int64(1), int64(10), testUserID).Return(task, nil)

	resp, err := e.controller.ReplyToTask(context.Background(), entity.ReplyToTaskRequest{
		WorkspaceID: 1, TaskID: 10, Text: "hello", UserID: testUserIDStr,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Task.ID != 10 {
		t.Errorf("unexpected task ID %d", resp.Task.ID)
	}
}

// ── UpdateScheduledTask ───────────────────────────────────────────────────────

func TestUpdateScheduledTask_Success(t *testing.T) {
	e := newTestController(t)

	task := model.Task{ID: 10, WorkspaceID: 1, Status: "cron", CronSchedule: "0 8 * * 1"}
	updated := model.Task{ID: 10, WorkspaceID: 1, Status: "cron", Title: "new title", CronSchedule: "0 9 * * 1"}

	e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(activeWorkspace(), nil)
	e.repo.EXPECT().GetTask(gomock.Any(), int64(1), int64(10), testUserID).Return(task, nil)
	e.repo.EXPECT().UpdateTask(gomock.Any(), gomock.Any()).Return(updated, nil)

	resp, err := e.controller.UpdateScheduledTask(context.Background(), entity.UpdateScheduledTaskRequest{
		WorkspaceID: 1, TaskID: 10, Title: "new title", CronSchedule: "0 9 * * 1", UserID: testUserIDStr,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Task.Title != "new title" {
		t.Errorf("expected 'new title', got %s", resp.Task.Title)
	}
}

func TestUpdateScheduledTask_NonCronTask_Rejected(t *testing.T) {
	e := newTestController(t)

	task := model.Task{ID: 10, WorkspaceID: 1, Status: "ongoing"}
	e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(activeWorkspace(), nil)
	e.repo.EXPECT().GetTask(gomock.Any(), int64(1), int64(10), testUserID).Return(task, nil)

	_, err := e.controller.UpdateScheduledTask(context.Background(), entity.UpdateScheduledTaskRequest{
		WorkspaceID: 1, TaskID: 10, CronSchedule: "0 9 * * 1", UserID: testUserIDStr,
	})
	if err == nil {
		t.Fatal("expected error for non-cron task")
	}
}

func TestUpdateScheduledTask_InvalidCron(t *testing.T) {
	e := newTestController(t)

	task := model.Task{ID: 10, WorkspaceID: 1, Status: "cron"}
	e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(activeWorkspace(), nil)
	e.repo.EXPECT().GetTask(gomock.Any(), int64(1), int64(10), testUserID).Return(task, nil)

	_, err := e.controller.UpdateScheduledTask(context.Background(), entity.UpdateScheduledTaskRequest{
		WorkspaceID: 1, TaskID: 10, CronSchedule: "bad cron", UserID: testUserIDStr,
	})
	if err == nil {
		t.Fatal("expected error for invalid cron schedule")
	}
}

func TestUpdateMessageMetadata_Success(t *testing.T) {
	e := newTestController(t)

	e.repo.EXPECT().GetTask(gomock.Any(), int64(1), int64(10), testUserID).Return(model.Task{}, nil)
	e.repo.EXPECT().UpdateMessageMetadata(gomock.Any(), int64(10), int64(500), gomock.Any()).Return(nil)

	err := e.controller.UpdateMessageMetadata(context.Background(), entity.UpdateMessageMetadataRequest{
		WorkspaceID: 1,
		TaskID:      10,
		MessageID:   500,
		Metadata:    map[string]string{"foo": "bar"},
		UserID:      testUserIDStr,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeleteTask_Success(t *testing.T) {
	e := newTestController(t)

	task := model.Task{
		ID:          10,
		WorkspaceID: 1,
		Attachments: []byte(`[{"id":"a1"}]`),
		Messages: []model.Message{
			{ID: 101, Attachments: []byte(`[{"id":"a2"}]`)},
		},
	}
	e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(activeWorkspace(), nil)
	e.repo.EXPECT().GetTask(gomock.Any(), int64(1), int64(10), testUserID).Return(task, nil)
	e.repo.EXPECT().DeleteTask(gomock.Any(), int64(1), int64(10), testUserID).Return(nil)
	e.storage.EXPECT().Delete("a1").Return(nil)
	e.storage.EXPECT().Delete("a2").Return(nil)

	_, err := e.controller.DeleteTask(context.Background(), entity.DeleteTaskRequest{WorkspaceID: 1, TaskID: 10, UserID: testUserIDStr})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGetAttachment_Success(t *testing.T) {
	e := newTestController(t)

	attsJSON := `[{"id":"att-1","filename":"f.txt"}]`
	e.repo.EXPECT().ListTasks(gomock.Any(), gomock.Any(), testUserID).Return([]model.Task{
		{ID: 1, Attachments: []byte(attsJSON)},
	}, nil)
	e.storage.EXPECT().LoadRaw("att-1").Return([]byte("content"), nil)

	resp, err := e.controller.GetAttachment(context.Background(), entity.GetAttachmentRequest{
		WorkspaceID:  1,
		AttachmentID: "att-1",
		UserID:       testUserIDStr,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(resp.Data) != "content" {
		t.Errorf("expected content")
	}
}
