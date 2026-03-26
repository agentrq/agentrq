package crud

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	entity "github.com/hasmcp/agentrq/backend/internal/data/entity/crud"
	"github.com/hasmcp/agentrq/backend/internal/data/model"
	mock_idgen "github.com/hasmcp/agentrq/backend/internal/service/mocks/idgen"
	mock_notif "github.com/hasmcp/agentrq/backend/internal/service/mocks/notif"
	mock_repo "github.com/hasmcp/agentrq/backend/internal/service/mocks/repository"
	mock_storage "github.com/hasmcp/agentrq/backend/internal/service/mocks/storage"
	mock_telemetry "github.com/hasmcp/agentrq/backend/internal/service/mocks/telemetry"
)

func newTestController(t *testing.T) (Controller, *mock_repo.MockRepository, *mock_idgen.MockService, *mock_storage.MockService, *mock_notif.MockService, *mock_telemetry.MockService) {
	t.Helper()
	ctrl := gomock.NewController(t)
	repo := mock_repo.NewMockRepository(ctrl)
	idgen := mock_idgen.NewMockService(ctrl)
	stor := mock_storage.NewMockService(ctrl)
	notifSvc := mock_notif.NewMockService(ctrl)
	telSvc := mock_telemetry.NewMockService(ctrl)

	c := New(Params{
		IDGen:      idgen,
		Repository: repo,
		Storage:    stor,
		Notif:      notifSvc,
		Telemetry:  telSvc,
	})
	return c, repo, idgen, stor, notifSvc, telSvc
}

func activeWorkspace() model.Workspace {
	return model.Workspace{ID: 1, UserID: "user1", Name: "ws"}
}

// ── CreateTask ────────────────────────────────────────────────────────────────

func TestCreateTask_Success(t *testing.T) {
	c, repo, idgen, _, notifSvc, telSvc := newTestController(t)

	now := time.Now()
	created := model.Task{ID: 42, WorkspaceID: 1, Title: "My task", Status: "notstarted", CreatedAt: now, UpdatedAt: now}

	repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), "user1").Return(activeWorkspace(), nil)
	idgen.EXPECT().NextID().Return(int64(42))
	repo.EXPECT().CreateTask(gomock.Any(), gomock.Any()).Return(created, nil)
	repo.EXPECT().SystemGetWorkspace(gomock.Any(), int64(1)).Return(activeWorkspace(), nil)
	notifSvc.EXPECT().NotifyTaskCreated(gomock.Any(), gomock.Any())
	telSvc.EXPECT().Record(gomock.Any(), "user1", int64(1), gomock.Any())

	resp, err := c.CreateTask(context.Background(), entity.CreateTaskRequest{
		UserID: "user1",
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

func TestCreateTask_EmptyTitle(t *testing.T) {
	c, repo, _, _, _, _ := newTestController(t)

	repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), "user1").Return(activeWorkspace(), nil)

	_, err := c.CreateTask(context.Background(), entity.CreateTaskRequest{
		UserID: "user1",
		Task:   entity.Task{WorkspaceID: 1, Title: ""},
	})

	if err == nil || err.Error() != "title is required" {
		t.Fatalf("expected 'title is required' error, got %v", err)
	}
}

func TestCreateTask_ArchivedWorkspace(t *testing.T) {
	c, repo, _, _, _, _ := newTestController(t)

	archived := activeWorkspace()
	archivedAt := time.Now()
	archived.ArchivedAt = &archivedAt

	repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), "user1").Return(archived, nil)

	_, err := c.CreateTask(context.Background(), entity.CreateTaskRequest{
		UserID: "user1",
		Task:   entity.Task{WorkspaceID: 1, Title: "t"},
	})

	if err == nil {
		t.Fatal("expected error for archived workspace")
	}
}

func TestCreateTask_InvalidCronSchedule(t *testing.T) {
	c, repo, _, _, _, _ := newTestController(t)

	repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), "user1").Return(activeWorkspace(), nil)

	_, err := c.CreateTask(context.Background(), entity.CreateTaskRequest{
		UserID: "user1",
		Task:   entity.Task{WorkspaceID: 1, Title: "t", Status: "cron", CronSchedule: "not-a-cron"},
	})

	if err == nil {
		t.Fatal("expected error for invalid cron schedule")
	}
}

func TestCreateTask_ValidCronSchedule(t *testing.T) {
	c, repo, idgen, _, _, telSvc := newTestController(t)

	created := model.Task{ID: 5, WorkspaceID: 1, Title: "t", Status: "cron"}

	repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), "user1").Return(activeWorkspace(), nil)
	idgen.EXPECT().NextID().Return(int64(5))
	repo.EXPECT().CreateTask(gomock.Any(), gomock.Any()).Return(created, nil)
	// cron tasks do not trigger notification
	telSvc.EXPECT().Record(gomock.Any(), "user1", int64(1), gomock.Any())

	resp, err := c.CreateTask(context.Background(), entity.CreateTaskRequest{
		UserID: "user1",
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
	c, repo, idgen, _, _, _ := newTestController(t)

	repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), "user1").Return(activeWorkspace(), nil)
	idgen.EXPECT().NextID().Return(int64(1))
	repo.EXPECT().CreateTask(gomock.Any(), gomock.Any()).Return(model.Task{}, fmt.Errorf("db error"))

	_, err := c.CreateTask(context.Background(), entity.CreateTaskRequest{
		UserID: "user1",
		Task:   entity.Task{WorkspaceID: 1, Title: "t"},
	})
	if err == nil {
		t.Fatal("expected error from repository")
	}
}

// ── GetTask ───────────────────────────────────────────────────────────────────

func TestGetTask_Success(t *testing.T) {
	c, repo, _, _, _, _ := newTestController(t)

	task := model.Task{ID: 10, WorkspaceID: 1, Title: "hello", Status: "ongoing"}
	repo.EXPECT().GetTask(gomock.Any(), int64(1), int64(10), "user1").Return(task, nil)

	resp, err := c.GetTask(context.Background(), entity.GetTaskRequest{WorkspaceID: 1, TaskID: 10, UserID: "user1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Task.Title != "hello" {
		t.Errorf("expected title hello, got %s", resp.Task.Title)
	}
}

func TestGetTask_NotFound(t *testing.T) {
	c, repo, _, _, _, _ := newTestController(t)

	repo.EXPECT().GetTask(gomock.Any(), int64(1), int64(99), "user1").Return(model.Task{}, fmt.Errorf("not found"))

	_, err := c.GetTask(context.Background(), entity.GetTaskRequest{WorkspaceID: 1, TaskID: 99, UserID: "user1"})
	if err == nil {
		t.Fatal("expected error")
	}
}

// ── ListTasks ─────────────────────────────────────────────────────────────────

func TestListTasks_Success(t *testing.T) {
	c, repo, _, _, _, _ := newTestController(t)

	tasks := []model.Task{
		{ID: 1, Title: "a"},
		{ID: 2, Title: "b"},
	}
	repo.EXPECT().ListTasks(gomock.Any(), gomock.Any(), "user1").Return(tasks, nil)

	resp, err := c.ListTasks(context.Background(), entity.ListTasksRequest{WorkspaceID: 1, UserID: "user1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Tasks) != 2 {
		t.Errorf("expected 2 tasks, got %d", len(resp.Tasks))
	}
}

func TestListTasks_Empty(t *testing.T) {
	c, repo, _, _, _, _ := newTestController(t)

	repo.EXPECT().ListTasks(gomock.Any(), gomock.Any(), "user1").Return([]model.Task{}, nil)

	resp, err := c.ListTasks(context.Background(), entity.ListTasksRequest{WorkspaceID: 1, UserID: "user1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Tasks) != 0 {
		t.Errorf("expected 0 tasks")
	}
}

// ── RespondToTask ─────────────────────────────────────────────────────────────

func TestRespondToTask_Allow(t *testing.T) {
	c, repo, idgen, _, _, telSvc := newTestController(t)

	task := model.Task{ID: 10, WorkspaceID: 1, Status: "notstarted"}
	updated := model.Task{ID: 10, WorkspaceID: 1, Status: "ongoing"}

	repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), "user1").Return(activeWorkspace(), nil)
	repo.EXPECT().GetTask(gomock.Any(), int64(1), int64(10), "user1").Return(task, nil)
	repo.EXPECT().ListTasks(gomock.Any(), gomock.Any(), "user1").Return([]model.Task{task}, nil)
	idgen.EXPECT().NextID().Return(int64(100))
	repo.EXPECT().CreateMessage(gomock.Any(), gomock.Any()).Return(nil)
	telSvc.EXPECT().Record(gomock.Any(), "user1", int64(1), model.ActionIDTaskApproveManual)
	telSvc.EXPECT().Record(gomock.Any(), "user1", int64(1), model.ActionIDMessageCreate)
	repo.EXPECT().UpdateTask(gomock.Any(), gomock.Any()).Return(updated, nil)
	telSvc.EXPECT().Record(gomock.Any(), "user1", int64(1), model.ActionIDTaskUpdate)
	repo.EXPECT().GetTask(gomock.Any(), int64(1), int64(10), "user1").Return(updated, nil)

	resp, err := c.RespondToTask(context.Background(), entity.RespondToTaskRequest{
		WorkspaceID: 1, TaskID: 10, Action: "allow", UserID: "user1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Task.Status != "ongoing" {
		t.Errorf("expected status ongoing, got %s", resp.Task.Status)
	}
}

func TestRespondToTask_Reject(t *testing.T) {
	c, repo, idgen, _, _, telSvc := newTestController(t)

	task := model.Task{ID: 10, WorkspaceID: 1, Status: "notstarted"}
	updated := model.Task{ID: 10, WorkspaceID: 1, Status: "rejected"}

	repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), "user1").Return(activeWorkspace(), nil)
	repo.EXPECT().GetTask(gomock.Any(), int64(1), int64(10), "user1").Return(task, nil)
	idgen.EXPECT().NextID().Return(int64(101))
	repo.EXPECT().CreateMessage(gomock.Any(), gomock.Any()).Return(nil)
	telSvc.EXPECT().Record(gomock.Any(), "user1", int64(1), model.ActionIDMessageCreate)
	repo.EXPECT().UpdateTask(gomock.Any(), gomock.Any()).Return(updated, nil)
	telSvc.EXPECT().Record(gomock.Any(), "user1", int64(1), model.ActionIDTaskUpdate)
	repo.EXPECT().GetTask(gomock.Any(), int64(1), int64(10), "user1").Return(updated, nil)

	resp, err := c.RespondToTask(context.Background(), entity.RespondToTaskRequest{
		WorkspaceID: 1, TaskID: 10, Action: "reject", UserID: "user1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Task.Status != "rejected" {
		t.Errorf("expected status rejected, got %s", resp.Task.Status)
	}
}

func TestRespondToTask_UnknownAction(t *testing.T) {
	c, repo, _, _, _, _ := newTestController(t)

	repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), "user1").Return(activeWorkspace(), nil)
	repo.EXPECT().GetTask(gomock.Any(), int64(1), int64(10), "user1").Return(model.Task{ID: 10}, nil)

	_, err := c.RespondToTask(context.Background(), entity.RespondToTaskRequest{
		WorkspaceID: 1, TaskID: 10, Action: "bogus", UserID: "user1",
	})
	if err == nil {
		t.Fatal("expected error for unknown action")
	}
}

func TestRespondToTask_Allow_BlockedByOngoingTask(t *testing.T) {
	c, repo, _, _, _, _ := newTestController(t)

	thisTask := model.Task{ID: 10, WorkspaceID: 1, Status: "notstarted"}
	otherTask := model.Task{ID: 99, WorkspaceID: 1, Status: "ongoing"}

	repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), "user1").Return(activeWorkspace(), nil)
	repo.EXPECT().GetTask(gomock.Any(), int64(1), int64(10), "user1").Return(thisTask, nil)
	repo.EXPECT().ListTasks(gomock.Any(), gomock.Any(), "user1").Return([]model.Task{thisTask, otherTask}, nil)

	_, err := c.RespondToTask(context.Background(), entity.RespondToTaskRequest{
		WorkspaceID: 1, TaskID: 10, Action: "allow", UserID: "user1",
	})
	if err == nil || err.Error() != "another task is already ongoing in this workspace" {
		t.Fatalf("expected conflict error, got %v", err)
	}
}

// ── UpdateTaskStatus ──────────────────────────────────────────────────────────

func TestUpdateTaskStatus_Success(t *testing.T) {
	c, repo, _, _, _, telSvc := newTestController(t)

	task := model.Task{ID: 10, WorkspaceID: 1, Status: "ongoing"}
	updated := model.Task{ID: 10, WorkspaceID: 1, Status: "completed"}

	repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), "user1").Return(activeWorkspace(), nil)
	repo.EXPECT().GetTask(gomock.Any(), int64(1), int64(10), "user1").Return(task, nil)
	repo.EXPECT().UpdateTask(gomock.Any(), gomock.Any()).Return(updated, nil)
	telSvc.EXPECT().Record(gomock.Any(), "user1", int64(1), model.ActionIDTaskComplete)
	telSvc.EXPECT().Record(gomock.Any(), "user1", int64(1), model.ActionIDTaskUpdate)

	resp, err := c.UpdateTaskStatus(context.Background(), entity.UpdateTaskStatusRequest{
		WorkspaceID: 1, TaskID: 10, Status: "completed", UserID: "user1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Task.Status != "completed" {
		t.Errorf("expected completed, got %s", resp.Task.Status)
	}
}

func TestUpdateTaskStatus_CronTask_Rejected(t *testing.T) {
	c, repo, _, _, _, _ := newTestController(t)

	task := model.Task{ID: 10, WorkspaceID: 1, Status: "cron"}
	repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), "user1").Return(activeWorkspace(), nil)
	repo.EXPECT().GetTask(gomock.Any(), int64(1), int64(10), "user1").Return(task, nil)

	_, err := c.UpdateTaskStatus(context.Background(), entity.UpdateTaskStatusRequest{
		WorkspaceID: 1, TaskID: 10, Status: "ongoing", UserID: "user1",
	})
	if err == nil {
		t.Fatal("expected error for cron task status update")
	}
}

func TestUpdateTaskStatus_Ongoing_BlockedByExistingOngoingTask(t *testing.T) {
	c, repo, _, _, _, _ := newTestController(t)

	thisTask := model.Task{ID: 10, WorkspaceID: 1, Status: "notstarted"}
	other := model.Task{ID: 99, WorkspaceID: 1, Status: "ongoing"}

	repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), "user1").Return(activeWorkspace(), nil)
	repo.EXPECT().GetTask(gomock.Any(), int64(1), int64(10), "user1").Return(thisTask, nil)
	repo.EXPECT().ListTasks(gomock.Any(), gomock.Any(), "user1").Return([]model.Task{thisTask, other}, nil)

	_, err := c.UpdateTaskStatus(context.Background(), entity.UpdateTaskStatusRequest{
		WorkspaceID: 1, TaskID: 10, Status: "ongoing", UserID: "user1",
	})
	if err == nil {
		t.Fatal("expected conflict error")
	}
}

// ── DeleteTask ────────────────────────────────────────────────────────────────

func TestDeleteTask_Success(t *testing.T) {
	c, repo, _, stor, _, telSvc := newTestController(t)

	task := model.Task{ID: 10, WorkspaceID: 1}
	repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), "user1").Return(activeWorkspace(), nil)
	repo.EXPECT().GetTask(gomock.Any(), int64(1), int64(10), "user1").Return(task, nil)
	repo.EXPECT().DeleteTask(gomock.Any(), int64(1), int64(10), "user1").Return(nil)
	telSvc.EXPECT().Record(gomock.Any(), "user1", int64(1), model.ActionIDTaskDelete)
	_ = stor // no attachments in this task

	_, err := c.DeleteTask(context.Background(), entity.DeleteTaskRequest{
		WorkspaceID: 1, TaskID: 10, UserID: "user1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeleteTask_RepositoryError(t *testing.T) {
	c, repo, _, _, _, _ := newTestController(t)

	task := model.Task{ID: 10, WorkspaceID: 1}
	repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), "user1").Return(activeWorkspace(), nil)
	repo.EXPECT().GetTask(gomock.Any(), int64(1), int64(10), "user1").Return(task, nil)
	repo.EXPECT().DeleteTask(gomock.Any(), int64(1), int64(10), "user1").Return(fmt.Errorf("db error"))

	_, err := c.DeleteTask(context.Background(), entity.DeleteTaskRequest{
		WorkspaceID: 1, TaskID: 10, UserID: "user1",
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

// ── UpdateTaskOrder ───────────────────────────────────────────────────────────

func TestUpdateTaskOrder_Success(t *testing.T) {
	c, repo, _, _, _, telSvc := newTestController(t)

	task := model.Task{ID: 10, WorkspaceID: 1, SortOrder: 1.0}
	updated := model.Task{ID: 10, WorkspaceID: 1, SortOrder: 5.5}

	repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), "user1").Return(activeWorkspace(), nil)
	repo.EXPECT().GetTask(gomock.Any(), int64(1), int64(10), "user1").Return(task, nil)
	repo.EXPECT().UpdateTask(gomock.Any(), gomock.Any()).Return(updated, nil)
	telSvc.EXPECT().Record(gomock.Any(), "user1", int64(1), model.ActionIDTaskUpdate)

	resp, err := c.UpdateTaskOrder(context.Background(), entity.UpdateTaskOrderRequest{
		WorkspaceID: 1, TaskID: 10, SortOrder: 5.5, UserID: "user1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Task.SortOrder != 5.5 {
		t.Errorf("expected sort order 5.5, got %f", resp.Task.SortOrder)
	}
}

// ── ReplyToTask ───────────────────────────────────────────────────────────────

func TestReplyToTask_Success(t *testing.T) {
	c, repo, idgen, _, _, telSvc := newTestController(t)

	task := model.Task{ID: 10, WorkspaceID: 1, Status: "ongoing"}
	updated := model.Task{ID: 10, WorkspaceID: 1, Status: "ongoing"}

	repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), "user1").Return(activeWorkspace(), nil)
	repo.EXPECT().GetTask(gomock.Any(), int64(1), int64(10), "user1").Return(task, nil)
	idgen.EXPECT().NextID().Return(int64(200))
	repo.EXPECT().CreateMessage(gomock.Any(), gomock.Any()).Return(nil)
	telSvc.EXPECT().Record(gomock.Any(), "user1", int64(1), model.ActionIDMessageCreate)
	repo.EXPECT().UpdateTask(gomock.Any(), gomock.Any()).Return(updated, nil)
	telSvc.EXPECT().Record(gomock.Any(), "user1", int64(1), model.ActionIDTaskUpdate)
	repo.EXPECT().GetTask(gomock.Any(), int64(1), int64(10), "user1").Return(updated, nil)

	resp, err := c.ReplyToTask(context.Background(), entity.ReplyToTaskRequest{
		WorkspaceID: 1, TaskID: 10, Text: "hello", UserID: "user1",
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
	c, repo, _, _, _, telSvc := newTestController(t)

	task := model.Task{ID: 10, WorkspaceID: 1, Status: "cron", CronSchedule: "0 8 * * 1"}
	updated := model.Task{ID: 10, WorkspaceID: 1, Status: "cron", Title: "new title", CronSchedule: "0 9 * * 1"}

	repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), "user1").Return(activeWorkspace(), nil)
	repo.EXPECT().GetTask(gomock.Any(), int64(1), int64(10), "user1").Return(task, nil)
	repo.EXPECT().UpdateTask(gomock.Any(), gomock.Any()).Return(updated, nil)
	telSvc.EXPECT().Record(gomock.Any(), "user1", int64(1), model.ActionIDTaskUpdate)

	resp, err := c.UpdateScheduledTask(context.Background(), entity.UpdateScheduledTaskRequest{
		WorkspaceID: 1, TaskID: 10, Title: "new title", CronSchedule: "0 9 * * 1", UserID: "user1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Task.Title != "new title" {
		t.Errorf("expected 'new title', got %s", resp.Task.Title)
	}
}

func TestUpdateScheduledTask_NonCronTask_Rejected(t *testing.T) {
	c, repo, _, _, _, _ := newTestController(t)

	task := model.Task{ID: 10, WorkspaceID: 1, Status: "ongoing"}
	repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), "user1").Return(activeWorkspace(), nil)
	repo.EXPECT().GetTask(gomock.Any(), int64(1), int64(10), "user1").Return(task, nil)

	_, err := c.UpdateScheduledTask(context.Background(), entity.UpdateScheduledTaskRequest{
		WorkspaceID: 1, TaskID: 10, CronSchedule: "0 9 * * 1", UserID: "user1",
	})
	if err == nil {
		t.Fatal("expected error for non-cron task")
	}
}

func TestUpdateScheduledTask_InvalidCron(t *testing.T) {
	c, repo, _, _, _, _ := newTestController(t)

	task := model.Task{ID: 10, WorkspaceID: 1, Status: "cron"}
	repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), "user1").Return(activeWorkspace(), nil)
	repo.EXPECT().GetTask(gomock.Any(), int64(1), int64(10), "user1").Return(task, nil)

	_, err := c.UpdateScheduledTask(context.Background(), entity.UpdateScheduledTaskRequest{
		WorkspaceID: 1, TaskID: 10, CronSchedule: "bad cron", UserID: "user1",
	})
	if err == nil {
		t.Fatal("expected error for invalid cron schedule")
	}
}
