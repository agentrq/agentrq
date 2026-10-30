// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package crud

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/golang/mock/gomock"
)

var errDBDown = errors.New("database is down")

type denyMessageLimiter struct{ stubLimiter }

func (*denyMessageLimiter) AllowMessage(int64) bool { return false }

func wantErrIs(t *testing.T, what string, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Errorf("%s: err = %v, want %v", what, err, want)
	}
}

func wantErrText(t *testing.T, what string, err error, text string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), text) {
		t.Errorf("%s: err = %v, want one containing %q", what, err, text)
	}
}

func TestAppendSelfLearningNote_ToAnEmptyBody(t *testing.T) {
	if got := AppendSelfLearningNote("", "note"); got != "note" {
		t.Errorf("AppendSelfLearningNote(\"\", note) = %q, want the note alone", got)
	}
}

func TestCreateTask_Refusals(t *testing.T) {
	t.Run("rate limited before anything is read", func(t *testing.T) {
		e := newTestController(t)
		e.controller.(*controller).limiter = &denyTaskLimiter{}
		_, err := e.controller.CreateTask(context.Background(), entity.CreateTaskRequest{UserID: testUserIDStr, Task: entity.Task{WorkspaceID: 1}})
		wantErrText(t, "CreateTask", err, "rate limit exceeded")
	})
	t.Run("unknown status", func(t *testing.T) {
		e := newTestController(t)
		e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(activeWorkspace(), nil)
		_, err := e.controller.CreateTask(context.Background(), entity.CreateTaskRequest{UserID: testUserIDStr, Task: entity.Task{WorkspaceID: 1, Status: "done"}})
		wantErrText(t, "CreateTask", err, "invalid task status: done")
	})
	t.Run("cron without a schedule", func(t *testing.T) {
		e := newTestController(t)
		e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(activeWorkspace(), nil)
		_, err := e.controller.CreateTask(context.Background(), entity.CreateTaskRequest{UserID: testUserIDStr, Task: entity.Task{WorkspaceID: 1, Status: "cron"}})
		wantErrText(t, "CreateTask", err, "cron_schedule is required")
	})
}

// Choosing a workflow runs it on completion: the task publishes the
// workflow's start event. Choosing an event publishes that event.
func TestCreateTask_CompletionTriggers(t *testing.T) {
	create := func(t *testing.T, e *testEnv, task entity.Task) (model.Task, error) {
		t.Helper()
		var saved model.Task
		e.idgen.EXPECT().NextID().Return(int64(42))
		e.repo.EXPECT().CreateTask(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, m model.Task) (model.Task, error) {
			saved = m
			return m, nil
		})
		task.WorkspaceID = 1
		_, err := e.controller.CreateTask(context.Background(), entity.CreateTaskRequest{UserID: testUserIDStr, Task: task})
		return saved, err
	}

	t.Run("a workflow fires its start event", func(t *testing.T) {
		e := newTestController(t)
		e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(activeWorkspace(), nil)
		e.repo.EXPECT().GetWorkflow(gomock.Any(), int64(7), testUserID).Return(model.Workflow{ID: 7, Name: "release", StartEventID: 70}, nil)
		saved, err := create(t, e, entity.Task{WorkflowID: 7})
		if err != nil {
			t.Fatalf("CreateTask: %v", err)
		}
		if saved.EventID != 70 || saved.WorkflowID != 7 || saved.CompletionTriggerType != entity.CompletionTriggerWorkflow {
			t.Errorf("saved event %d, workflow %d, trigger %v; want 70, 7, workflow", saved.EventID, saved.WorkflowID, saved.CompletionTriggerType)
		}
	})
	t.Run("an event fires itself", func(t *testing.T) {
		e := newTestController(t)
		e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(activeWorkspace(), nil)
		saved, err := create(t, e, entity.Task{EventID: 80})
		if err != nil {
			t.Fatalf("CreateTask: %v", err)
		}
		if saved.EventID != 80 || saved.CompletionTriggerType != entity.CompletionTriggerEvent {
			t.Errorf("saved event %d, trigger %v; want 80, event", saved.EventID, saved.CompletionTriggerType)
		}
	})
	t.Run("a workflow that cannot be read", func(t *testing.T) {
		e := newTestController(t)
		e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(activeWorkspace(), nil)
		e.idgen.EXPECT().NextID().Return(int64(42))
		e.repo.EXPECT().GetWorkflow(gomock.Any(), int64(7), testUserID).Return(model.Workflow{}, base.ErrNotFound)
		_, err := e.controller.CreateTask(context.Background(), entity.CreateTaskRequest{UserID: testUserIDStr, Task: entity.Task{WorkspaceID: 1, WorkflowID: 7}})
		wantErrText(t, "CreateTask", err, "workflow not found")
	})
	t.Run("a workflow with no start event", func(t *testing.T) {
		e := newTestController(t)
		e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(activeWorkspace(), nil)
		e.idgen.EXPECT().NextID().Return(int64(42))
		e.repo.EXPECT().GetWorkflow(gomock.Any(), int64(7), testUserID).Return(model.Workflow{ID: 7, Name: "release"}, nil)
		_, err := e.controller.CreateTask(context.Background(), entity.CreateTaskRequest{UserID: testUserIDStr, Task: entity.Task{WorkspaceID: 1, WorkflowID: 7}})
		wantErrText(t, "CreateTask", err, `workflow "release" has no start event yet`)
	})
}

func TestListTasks_RepositoryError(t *testing.T) {
	e := newTestController(t)
	e.repo.EXPECT().ListTasks(gomock.Any(), gomock.Any(), testUserID).Return(nil, errDBDown)
	_, err := e.controller.ListTasks(context.Background(), entity.ListTasksRequest{UserID: testUserIDStr})
	wantErrIs(t, "ListTasks", err, errDBDown)
}

func TestRespondToTask_Failures(t *testing.T) {
	task := model.Task{ID: 10, WorkspaceID: 1, Status: "notstarted"}
	req := func(action, text string) entity.RespondToTaskRequest {
		return entity.RespondToTaskRequest{WorkspaceID: 1, TaskID: 10, Action: action, Text: text, UserID: testUserIDStr}
	}
	reads := func(e *testEnv) {
		e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(activeWorkspace(), nil)
		e.repo.EXPECT().GetTaskRow(gomock.Any(), int64(1), int64(10), testUserID).Return(task, nil)
	}

	t.Run("rate limited", func(t *testing.T) {
		e := newTestController(t)
		e.controller.(*controller).limiter = &denyMessageLimiter{}
		_, err := e.controller.RespondToTask(context.Background(), req("allow", ""))
		wantErrText(t, "RespondToTask", err, "rate limit exceeded")
	})
	t.Run("workspace not found", func(t *testing.T) {
		e := newTestController(t)
		e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(model.Workspace{}, base.ErrNotFound)
		_, err := e.controller.RespondToTask(context.Background(), req("allow", ""))
		wantErrIs(t, "RespondToTask", err, base.ErrNotFound)
	})
	t.Run("task not found", func(t *testing.T) {
		e := newTestController(t)
		e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(activeWorkspace(), nil)
		e.repo.EXPECT().GetTaskRow(gomock.Any(), int64(1), int64(10), testUserID).Return(model.Task{}, base.ErrNotFound)
		_, err := e.controller.RespondToTask(context.Background(), req("allow", ""))
		wantErrIs(t, "RespondToTask", err, base.ErrNotFound)
	})
	t.Run("another task is ongoing", func(t *testing.T) {
		e := newTestController(t)
		reads(e)
		e.repo.EXPECT().ListTasks(gomock.Any(), gomock.Any(), testUserID).Return([]model.Task{task, {ID: 11, Status: "ongoing"}}, nil)
		_, err := e.controller.RespondToTask(context.Background(), req("allow", ""))
		wantErrText(t, "RespondToTask", err, "another task is already ongoing")
	})
	t.Run("unknown action", func(t *testing.T) {
		e := newTestController(t)
		reads(e)
		_, err := e.controller.RespondToTask(context.Background(), req("deny", ""))
		wantErrText(t, "RespondToTask", err, "unknown action: deny")
	})
	t.Run("the message cannot be written", func(t *testing.T) {
		e := newTestController(t)
		reads(e)
		e.idgen.EXPECT().NextID().Return(int64(101))
		e.repo.EXPECT().CreateMessage(gomock.Any(), gomock.Any()).Return(errDBDown)
		_, err := e.controller.RespondToTask(context.Background(), req("reject", ""))
		wantErrIs(t, "RespondToTask", err, errDBDown)
	})
	t.Run("the task cannot be written", func(t *testing.T) {
		e := newTestController(t)
		reads(e)
		// A text action with no text writes no message.
		e.repo.EXPECT().UpdateTask(gomock.Any(), gomock.Any()).Return(model.Task{}, errDBDown)
		_, err := e.controller.RespondToTask(context.Background(), req("text", ""))
		wantErrIs(t, "RespondToTask", err, errDBDown)
	})
	t.Run("a failed reread answers with the written task", func(t *testing.T) {
		e := newTestController(t)
		reads(e)
		e.idgen.EXPECT().NextID().Return(int64(101))
		e.repo.EXPECT().CreateMessage(gomock.Any(), gomock.Any()).Return(nil)
		e.repo.EXPECT().UpdateTask(gomock.Any(), gomock.Any()).Return(model.Task{ID: 10, WorkspaceID: 1, Status: "notstarted", Title: "written"}, nil)
		e.repo.EXPECT().GetTask(gomock.Any(), int64(1), int64(10), testUserID).Return(model.Task{}, errDBDown)
		resp, err := e.controller.RespondToTask(context.Background(), req("text", "hello"))
		if err != nil {
			t.Fatalf("RespondToTask: %v", err)
		}
		if resp.Task.Title != "written" {
			t.Errorf("task = %+v, want the one written", resp.Task)
		}
	})
}

// Each status-like update passes on the failures of the reads and the write.
func TestTaskUpdates_PassOnFailures(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		call func(c Controller) error
	}{
		{"status", func(c Controller) error {
			_, err := c.UpdateTaskStatus(ctx, entity.UpdateTaskStatusRequest{WorkspaceID: 1, TaskID: 10, Status: "completed", UserID: testUserIDStr})
			return err
		}},
		{"order", func(c Controller) error {
			_, err := c.UpdateTaskOrder(ctx, entity.UpdateTaskOrderRequest{WorkspaceID: 1, TaskID: 10, SortOrder: 2, UserID: testUserIDStr})
			return err
		}},
		{"assignee", func(c Controller) error {
			_, err := c.UpdateTaskAssignee(ctx, entity.UpdateTaskAssigneeRequest{WorkspaceID: 1, TaskID: 10, Assignee: "human", UserID: testUserIDStr})
			return err
		}},
		{"schedule", func(c Controller) error {
			_, err := c.UpdateScheduledTask(ctx, entity.UpdateScheduledTaskRequest{WorkspaceID: 1, TaskID: 10, UserID: testUserIDStr})
			return err
		}},
	} {
		t.Run(tc.name+": workspace", func(t *testing.T) {
			e := newTestController(t)
			e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(model.Workspace{}, errDBDown)
			wantErrIs(t, tc.name, tc.call(e.controller), errDBDown)
		})
		t.Run(tc.name+": task", func(t *testing.T) {
			e := newTestController(t)
			e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(activeWorkspace(), nil)
			e.repo.EXPECT().GetTaskRow(gomock.Any(), int64(1), int64(10), testUserID).Return(model.Task{}, errDBDown)
			wantErrIs(t, tc.name, tc.call(e.controller), errDBDown)
		})
		t.Run(tc.name+": write", func(t *testing.T) {
			e := newTestController(t)
			e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(activeWorkspace(), nil)
			e.repo.EXPECT().GetTaskRow(gomock.Any(), int64(1), int64(10), testUserID).Return(model.Task{ID: 10, WorkspaceID: 1, Status: "notstarted", Assignee: "human"}, nil)
			e.repo.EXPECT().UpdateTask(gomock.Any(), gomock.Any(), gomock.Any()).Return(model.Task{}, errDBDown).AnyTimes()
			e.repo.EXPECT().UpdateTask(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(model.Task{}, errDBDown).AnyTimes()
			wantErrIs(t, tc.name, tc.call(e.controller), errDBDown)
		})
	}
}

func TestUpdateTaskAssignee_RefusesAnUnknownAssignee(t *testing.T) {
	e := newTestController(t)
	e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(activeWorkspace(), nil)
	e.repo.EXPECT().GetTaskRow(gomock.Any(), int64(1), int64(10), testUserID).Return(model.Task{ID: 10, WorkspaceID: 1}, nil)
	_, err := e.controller.UpdateTaskAssignee(context.Background(), entity.UpdateTaskAssigneeRequest{WorkspaceID: 1, TaskID: 10, Assignee: "robot", UserID: testUserIDStr})
	wantErrText(t, "UpdateTaskAssignee", err, "invalid assignee: robot")
}

// Clearing a schedule turns the template into an ordinary task, to be started.
func TestUpdateScheduledTask_ClearingTheScheduleMakesItNotStarted(t *testing.T) {
	e := newTestController(t)
	e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(activeWorkspace(), nil)
	e.repo.EXPECT().GetTaskRow(gomock.Any(), int64(1), int64(10), testUserID).Return(model.Task{ID: 10, WorkspaceID: 1, Status: "cron", CronSchedule: "0 * * * *"}, nil)
	var saved model.Task
	e.repo.EXPECT().UpdateTask(gomock.Any(), gomock.Any(), "Status", "CronSchedule", "Title", "Body", "Assignee", "AllowAllCommands", "ClearContext").
		DoAndReturn(func(_ context.Context, m model.Task, _ ...string) (model.Task, error) { saved = m; return m, nil })
	if _, err := e.controller.UpdateScheduledTask(context.Background(), entity.UpdateScheduledTaskRequest{WorkspaceID: 1, TaskID: 10, Title: "t", UserID: testUserIDStr}); err != nil {
		t.Fatalf("UpdateScheduledTask: %v", err)
	}
	if saved.Status != "notstarted" || saved.CronSchedule != "" {
		t.Errorf("saved status %q, schedule %q; want notstarted and none", saved.Status, saved.CronSchedule)
	}
}

func TestReplyToTask_Failures(t *testing.T) {
	req := entity.ReplyToTaskRequest{WorkspaceID: 1, TaskID: 10, Text: "hello", UserID: testUserIDStr}
	task := model.Task{ID: 10, WorkspaceID: 1, Status: "ongoing"}
	reads := func(e *testEnv) {
		e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(activeWorkspace(), nil)
		e.repo.EXPECT().GetTaskRow(gomock.Any(), int64(1), int64(10), testUserID).Return(task, nil)
	}

	t.Run("rate limited", func(t *testing.T) {
		e := newTestController(t)
		e.controller.(*controller).limiter = &denyMessageLimiter{}
		_, err := e.controller.ReplyToTask(context.Background(), req)
		wantErrText(t, "ReplyToTask", err, "rate limit exceeded")
	})
	t.Run("workspace", func(t *testing.T) {
		e := newTestController(t)
		e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(model.Workspace{}, errDBDown)
		_, err := e.controller.ReplyToTask(context.Background(), req)
		wantErrIs(t, "ReplyToTask", err, errDBDown)
	})
	t.Run("task", func(t *testing.T) {
		e := newTestController(t)
		e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(activeWorkspace(), nil)
		e.repo.EXPECT().GetTaskRow(gomock.Any(), int64(1), int64(10), testUserID).Return(model.Task{}, errDBDown)
		_, err := e.controller.ReplyToTask(context.Background(), req)
		wantErrIs(t, "ReplyToTask", err, errDBDown)
	})
	t.Run("message", func(t *testing.T) {
		e := newTestController(t)
		reads(e)
		e.idgen.EXPECT().NextID().Return(int64(101))
		e.repo.EXPECT().CreateMessage(gomock.Any(), gomock.Any()).Return(errDBDown)
		_, err := e.controller.ReplyToTask(context.Background(), req)
		wantErrIs(t, "ReplyToTask", err, errDBDown)
	})
	t.Run("task write", func(t *testing.T) {
		e := newTestController(t)
		reads(e)
		e.idgen.EXPECT().NextID().Return(int64(101))
		e.repo.EXPECT().CreateMessage(gomock.Any(), gomock.Any()).Return(nil)
		e.repo.EXPECT().UpdateTask(gomock.Any(), gomock.Any()).Return(model.Task{}, errDBDown)
		_, err := e.controller.ReplyToTask(context.Background(), req)
		wantErrIs(t, "ReplyToTask", err, errDBDown)
	})
}

// A reply from Slack is sent by "slack" and names the Slack user; a failed
// reread still answers with the task as written.
func TestReplyToTask_FromSlack(t *testing.T) {
	e := newTestController(t)
	e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(activeWorkspace(), nil)
	e.repo.EXPECT().GetTaskRow(gomock.Any(), int64(1), int64(10), testUserID).Return(model.Task{ID: 10, WorkspaceID: 1}, nil)
	e.idgen.EXPECT().NextID().Return(int64(101))
	var msg model.Message
	e.repo.EXPECT().CreateMessage(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, m model.Message) error { msg = m; return nil })
	e.repo.EXPECT().UpdateTask(gomock.Any(), gomock.Any()).Return(model.Task{ID: 10, WorkspaceID: 1, Title: "written"}, nil)
	e.repo.EXPECT().GetTask(gomock.Any(), int64(1), int64(10), testUserID).Return(model.Task{}, errDBDown)

	ctx := entity.WithOrigin(context.Background(), entity.OriginSlack)
	resp, err := e.controller.ReplyToTask(ctx, entity.ReplyToTaskRequest{WorkspaceID: 1, TaskID: 10, Text: "hi", UserID: testUserIDStr, SlackUser: "U123"})
	if err != nil {
		t.Fatalf("ReplyToTask: %v", err)
	}
	if resp.Task.Title != "written" {
		t.Errorf("task = %+v, want the one written", resp.Task)
	}
	if msg.Sender != "slack" {
		t.Errorf("sender = %q, want slack", msg.Sender)
	}
	var meta map[string]string
	if err := json.Unmarshal(msg.Metadata, &meta); err != nil || meta["slack_user"] != "U123" {
		t.Errorf("metadata = %s (%v), want the Slack user", msg.Metadata, err)
	}
}

func TestDeleteTask_Failures(t *testing.T) {
	req := entity.DeleteTaskRequest{WorkspaceID: 1, TaskID: 10, UserID: testUserIDStr}
	t.Run("workspace", func(t *testing.T) {
		e := newTestController(t)
		e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(model.Workspace{}, errDBDown)
		_, err := e.controller.DeleteTask(context.Background(), req)
		wantErrIs(t, "DeleteTask", err, errDBDown)
	})
	t.Run("task", func(t *testing.T) {
		e := newTestController(t)
		e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(activeWorkspace(), nil)
		e.repo.EXPECT().GetTask(gomock.Any(), int64(1), int64(10), testUserID).Return(model.Task{}, base.ErrNotFound)
		_, err := e.controller.DeleteTask(context.Background(), req)
		wantErrIs(t, "DeleteTask", err, base.ErrNotFound)
	})
	t.Run("delete", func(t *testing.T) {
		e := newTestController(t)
		e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(activeWorkspace(), nil)
		e.repo.EXPECT().GetTask(gomock.Any(), int64(1), int64(10), testUserID).Return(model.Task{ID: 10, WorkspaceID: 1}, nil)
		e.repo.EXPECT().ListPanoDocIDsOfTask(gomock.Any(), testUserID, int64(10)).Return(nil, nil)
		e.repo.EXPECT().DeleteTask(gomock.Any(), int64(1), int64(10), testUserID).Return(errDBDown)
		// Nothing is purged from storage when the row is still there.
		_, err := e.controller.DeleteTask(context.Background(), req)
		wantErrIs(t, "DeleteTask", err, errDBDown)
	})
}

func TestUpdateMessageMetadata_Failures(t *testing.T) {
	req := func(meta any) entity.UpdateMessageMetadataRequest {
		return entity.UpdateMessageMetadataRequest{WorkspaceID: 1, TaskID: 10, MessageID: 20, Metadata: meta, UserID: testUserIDStr}
	}
	t.Run("task", func(t *testing.T) {
		e := newTestController(t)
		e.repo.EXPECT().GetTaskRow(gomock.Any(), int64(1), int64(10), testUserID).Return(model.Task{}, base.ErrNotFound)
		wantErrIs(t, "UpdateMessageMetadata", e.controller.UpdateMessageMetadata(context.Background(), req(map[string]any{})), base.ErrNotFound)
	})
	t.Run("metadata that is not JSON", func(t *testing.T) {
		e := newTestController(t)
		e.repo.EXPECT().GetTaskRow(gomock.Any(), int64(1), int64(10), testUserID).Return(model.Task{ID: 10}, nil)
		var unsupported *json.UnsupportedTypeError
		if err := e.controller.UpdateMessageMetadata(context.Background(), req(func() {})); !errors.As(err, &unsupported) {
			t.Errorf("err = %v, want a JSON encoding error", err)
		}
	})
	t.Run("write", func(t *testing.T) {
		e := newTestController(t)
		e.repo.EXPECT().GetTaskRow(gomock.Any(), int64(1), int64(10), testUserID).Return(model.Task{ID: 10}, nil)
		e.repo.EXPECT().UpdateMessageMetadata(gomock.Any(), int64(10), int64(20), gomock.Any()).Return(errDBDown)
		wantErrIs(t, "UpdateMessageMetadata", e.controller.UpdateMessageMetadata(context.Background(), req(map[string]any{"a": 1})), errDBDown)
	})
}

func TestGetGlobalTaskStats(t *testing.T) {
	e := newTestController(t)
	e.repo.EXPECT().GetGlobalTaskStats(gomock.Any(), testUserID).Return(entity.GlobalTaskStatsResponse{PendingTasks: 3, ScheduledTasks: 2}, nil)
	got, err := e.controller.GetGlobalTaskStats(context.Background(), testUserIDStr)
	if err != nil || got.PendingTasks != 3 || got.ScheduledTasks != 2 {
		t.Errorf("GetGlobalTaskStats = %+v, %v", got, err)
	}

	e = newTestController(t)
	e.repo.EXPECT().GetGlobalTaskStats(gomock.Any(), testUserID).Return(entity.GlobalTaskStatsResponse{}, errDBDown)
	_, err = e.controller.GetGlobalTaskStats(context.Background(), testUserIDStr)
	wantErrIs(t, "GetGlobalTaskStats", err, errDBDown)
}
