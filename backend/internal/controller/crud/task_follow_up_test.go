// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package crud

import (
	"context"
	"errors"
	"testing"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	mock_repo "github.com/agentrq/agentrq/backend/internal/service/mocks/repository"
	"github.com/golang/mock/gomock"
	"gorm.io/datatypes"
)

var errFollowUpDBDown = errors.New("database unavailable")

func followUpRequest() entity.FollowUpTaskRequest {
	return entity.FollowUpTaskRequest{WorkspaceID: 1, TaskID: 10, Text: "Also cover the error path.", UserID: testUserIDStr}
}

// A finished task goes back to notstarted, so the poller hands it over again,
// with the text posted as a follow-up from the human.
func TestFollowUpTask_ReopensAFinishedTask(t *testing.T) {
	for _, status := range []string{"completed", "blocked", "rejected", "notstarted"} {
		t.Run(status, func(t *testing.T) {
			e := newTestController(t)
			task := model.Task{ID: 10, WorkspaceID: 1, UserID: testUserID, Status: status, Assignee: "agent"}
			e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(activeWorkspace(), nil)
			e.repo.EXPECT().GetTaskRow(gomock.Any(), int64(1), int64(10), testUserID).Return(task, nil)
			e.idgen.EXPECT().NextID().Return(int64(101))
			var posted model.Message
			e.repo.EXPECT().CreateMessage(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, m model.Message) error {
				posted = m
				return nil
			})
			e.repo.EXPECT().UpdateTask(gomock.Any(), gomock.Any(), "Status").DoAndReturn(func(_ context.Context, m model.Task, _ ...string) (model.Task, error) {
				return m, nil
			})

			res, err := e.controller.FollowUpTask(context.Background(), followUpRequest())
			if err != nil {
				t.Fatal(err)
			}
			if res.Task.Status != "notstarted" || res.MessageID != 101 {
				t.Errorf("task status %q, message %d; want notstarted and 101", res.Task.Status, res.MessageID)
			}
			if posted.ID != 101 || posted.TaskID != 10 || posted.Sender != "human" || posted.Text != "Also cover the error path." || !IsFollowUp(posted) {
				t.Errorf("posted %+v, want the human's follow-up", posted)
			}
		})
	}
}

// An agent still on the task keeps it: only the follow-up is posted.
func TestFollowUpTask_KeepsAnOngoingTaskOngoing(t *testing.T) {
	e := newTestController(t)
	task := model.Task{ID: 10, WorkspaceID: 1, Status: "ongoing", Assignee: "agent"}
	e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(activeWorkspace(), nil)
	e.repo.EXPECT().GetTaskRow(gomock.Any(), int64(1), int64(10), testUserID).Return(task, nil)
	e.idgen.EXPECT().NextID().Return(int64(101))
	e.repo.EXPECT().CreateMessage(gomock.Any(), gomock.Any()).Return(nil)
	e.repo.EXPECT().UpdateTask(gomock.Any(), gomock.Any(), "Status").DoAndReturn(func(_ context.Context, m model.Task, _ ...string) (model.Task, error) {
		return m, nil
	})

	res, err := e.controller.FollowUpTask(context.Background(), followUpRequest())
	if err != nil || res.Task.Status != "ongoing" {
		t.Fatalf("FollowUpTask = %+v, %v; want the task still ongoing", res, err)
	}
}

type denyMessages struct{ *stubLimiter }

func (denyMessages) AllowMessage(int64) bool { return false }

func TestFollowUpTask_Refuses(t *testing.T) {
	for name, tc := range map[string]struct {
		text   string
		expect func(e *testEnv)
		want   error
	}{
		"blank text": {text: " \n", want: errFollowUpText},
		"no workspace": {expect: func(e *testEnv) {
			e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(model.Workspace{}, errFollowUpDBDown)
		}, want: errFollowUpDBDown},
		"no task": {expect: func(e *testEnv) {
			e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(activeWorkspace(), nil)
			e.repo.EXPECT().GetTaskRow(gomock.Any(), int64(1), int64(10), testUserID).Return(model.Task{}, errFollowUpDBDown)
		}, want: errFollowUpDBDown},
		"the human's task": {expect: func(e *testEnv) {
			e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(activeWorkspace(), nil)
			e.repo.EXPECT().GetTaskRow(gomock.Any(), int64(1), int64(10), testUserID).Return(model.Task{ID: 10, Status: "completed", Assignee: "human"}, nil)
		}, want: errFollowUpAssignee},
		"a schedule": {expect: func(e *testEnv) {
			e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(activeWorkspace(), nil)
			e.repo.EXPECT().GetTaskRow(gomock.Any(), int64(1), int64(10), testUserID).Return(model.Task{ID: 10, Status: "cron", Assignee: "agent"}, nil)
		}, want: errFollowUpCron},
		"message not saved": {expect: func(e *testEnv) {
			e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(activeWorkspace(), nil)
			e.repo.EXPECT().GetTaskRow(gomock.Any(), int64(1), int64(10), testUserID).Return(model.Task{ID: 10, Status: "completed", Assignee: "agent"}, nil)
			e.idgen.EXPECT().NextID().Return(int64(101))
			e.repo.EXPECT().CreateMessage(gomock.Any(), gomock.Any()).Return(errFollowUpDBDown)
		}, want: errFollowUpDBDown},
		"status not saved": {expect: func(e *testEnv) {
			e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(activeWorkspace(), nil)
			e.repo.EXPECT().GetTaskRow(gomock.Any(), int64(1), int64(10), testUserID).Return(model.Task{ID: 10, Status: "completed", Assignee: "agent"}, nil)
			e.idgen.EXPECT().NextID().Return(int64(101))
			e.repo.EXPECT().CreateMessage(gomock.Any(), gomock.Any()).Return(nil)
			e.repo.EXPECT().UpdateTask(gomock.Any(), gomock.Any(), "Status").Return(model.Task{}, errFollowUpDBDown)
		}, want: errFollowUpDBDown},
	} {
		t.Run(name, func(t *testing.T) {
			e := newTestController(t)
			if tc.expect != nil {
				tc.expect(e)
			}
			req := followUpRequest()
			if tc.text != "" {
				req.Text = tc.text
			}
			if _, err := e.controller.FollowUpTask(context.Background(), req); !errors.Is(err, tc.want) {
				t.Errorf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

// A flood of follow-ups is held to the message limit, before any read.
func TestFollowUpTask_RateLimited(t *testing.T) {
	c := &controller{repository: mock_repo.NewMockRepository(gomock.NewController(t)), limiter: denyMessages{&stubLimiter{}}}
	if _, err := c.FollowUpTask(context.Background(), followUpRequest()); !errors.Is(err, errRateLimited) {
		t.Errorf("err = %v, want the rate limit", err)
	}
}

func TestIsFollowUp(t *testing.T) {
	for meta, want := range map[string]bool{
		`{"followUp":true}`:             true,
		`{"followUp":false}`:            false,
		`{"type":"permission_request"}`: false,
		`not json`:                      false,
		``:                              false,
	} {
		if got := IsFollowUp(model.Message{Metadata: datatypes.JSON(meta)}); got != want {
			t.Errorf("IsFollowUp(%q) = %v, want %v", meta, got, want)
		}
	}
}
