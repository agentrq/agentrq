// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package crud

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/golang/mock/gomock"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
)

// supervisorWorkspaces is an account with a supervisor workspace, 9.
var supervisorWorkspaces = []model.Workspace{{ID: 8, Name: "app"}, {ID: 9, Name: "supervisor"}}

// supervisorWrite is one of the writes the supervisor's task tool makes:
// expect sets the repository up for it, checking each write's context with
// by, and call makes it.
type supervisorWrite struct {
	expect func(e *testEnv, by func(context.Context))
	call   func(e *testEnv, bySupervisor bool) error
}

func supervisorWrites() map[string]supervisorWrite {
	task := model.Task{ID: 10, WorkspaceID: 1, Status: "ongoing", Assignee: "agent"}
	message := func(e *testEnv, by func(context.Context)) {
		e.idgen.EXPECT().NextID().Return(int64(101))
		e.repo.EXPECT().CreateMessage(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, _ model.Message) error {
			by(ctx)
			return nil
		})
	}
	update := func(e *testEnv, by func(context.Context)) {
		e.repo.EXPECT().UpdateTask(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, m model.Task, _ ...string) (model.Task, error) {
			by(ctx)
			return m, nil
		})
	}
	return map[string]supervisorWrite{
		"reply": {
			expect: func(e *testEnv, by func(context.Context)) {
				message(e, by)
				update(e, by)
				e.repo.EXPECT().GetTask(gomock.Any(), int64(1), int64(10), testUserID).Return(task, nil)
			},
			call: func(e *testEnv, bySupervisor bool) error {
				_, err := e.controller.ReplyToTask(context.Background(), entity.ReplyToTaskRequest{WorkspaceID: 1, TaskID: 10, Text: "hi", UserID: testUserIDStr, BySupervisor: bySupervisor})
				return err
			},
		},
		"followUp": {
			expect: func(e *testEnv, by func(context.Context)) {
				message(e, by)
				update(e, by)
			},
			call: func(e *testEnv, bySupervisor bool) error {
				_, err := e.controller.FollowUpTask(context.Background(), entity.FollowUpTaskRequest{WorkspaceID: 1, TaskID: 10, Text: "more", UserID: testUserIDStr, BySupervisor: bySupervisor})
				return err
			},
		},
		"respond": {
			expect: func(e *testEnv, by func(context.Context)) {
				message(e, by)
				update(e, by)
				e.repo.EXPECT().GetTask(gomock.Any(), int64(1), int64(10), testUserID).Return(task, nil)
			},
			call: func(e *testEnv, bySupervisor bool) error {
				_, err := e.controller.RespondToTask(context.Background(), entity.RespondToTaskRequest{WorkspaceID: 1, TaskID: 10, Action: "text", Text: "go on", UserID: testUserIDStr, BySupervisor: bySupervisor})
				return err
			},
		},
		"updateStatus": {
			expect: update,
			call: func(e *testEnv, bySupervisor bool) error {
				_, err := e.controller.UpdateTaskStatus(context.Background(), entity.UpdateTaskStatusRequest{WorkspaceID: 1, TaskID: 10, Status: "completed", UserID: testUserIDStr, BySupervisor: bySupervisor})
				return err
			},
		},
	}
}

// What the supervisor's task tool writes is put down to the supervisor
// workspace; what anyone else writes, to none.
func TestSupervisorWrites_ArePutDownToTheSupervisorWorkspace(t *testing.T) {
	task := model.Task{ID: 10, WorkspaceID: 1, Status: "ongoing", Assignee: "agent"}
	for name, w := range supervisorWrites() {
		for _, bySupervisor := range []bool{true, false} {
			want := int64(0)
			if bySupervisor {
				want = 9
			}
			t.Run(name, func(t *testing.T) {
				e := newTestController(t)
				e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(activeWorkspace(), nil)
				e.repo.EXPECT().GetTaskRow(gomock.Any(), int64(1), int64(10), testUserID).Return(task, nil)
				if bySupervisor {
					e.repo.EXPECT().ListWorkspaces(gomock.Any(), testUserID, false).Return(supervisorWorkspaces, nil)
				}
				w.expect(e, func(ctx context.Context) {
					if got := entity.GetAgentWorkspace(ctx); got != want {
						t.Errorf("written as workspace %d, want %d", got, want)
					}
				})
				if err := w.call(e, bySupervisor); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

// A write whose author cannot be looked up is not made.
func TestSupervisorWrites_FailWhenTheLookupFails(t *testing.T) {
	task := model.Task{ID: 10, WorkspaceID: 1, Status: "ongoing", Assignee: "agent"}
	boom := errors.New("database unavailable")
	for name, w := range supervisorWrites() {
		t.Run(name, func(t *testing.T) {
			e := newTestController(t)
			e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(activeWorkspace(), nil)
			e.repo.EXPECT().GetTaskRow(gomock.Any(), int64(1), int64(10), testUserID).Return(task, nil)
			e.repo.EXPECT().ListWorkspaces(gomock.Any(), testUserID, false).Return(nil, boom)
			if err := w.call(e, true); !errors.Is(err, boom) {
				t.Errorf("err = %v, want the lookup's", err)
			}
		})
	}
}

func TestListTaskChanges_ExcludeSelf(t *testing.T) {
	e := newTestController(t)
	req := changesRequest()
	req.ExcludeSelf = true
	moveAt, msgAt := req.Since.Add(time.Minute), req.Since.Add(2*time.Minute)
	e.repo.EXPECT().ListWorkspaces(gomock.Any(), testUserID, false).Return(supervisorWorkspaces, nil)
	e.repo.EXPECT().ListTaskChanges(gomock.Any(), req, testUserID, int64(9)).Return([]model.Task{{ID: 1}, {ID: 2}, {ID: 3}}, nil)
	e.repo.EXPECT().CountNewMessages(gomock.Any(), []int64{1, 2, 3}, req.Since, int64(9)).Return(map[int64]int{1: 1, 3: 1}, nil)
	e.repo.EXPECT().ListStatusChanges(gomock.Any(), []int64{1, 2, 3}, req.Since, int64(9)).Return(map[int64]time.Time{2: moveAt}, map[int64]time.Time{2: moveAt, 3: msgAt.Add(time.Minute)}, nil)
	e.repo.EXPECT().ListNewestMessages(gomock.Any(), []int64{1, 2, 3}, req.Since, int64(9)).Return(map[int64]model.Message{
		1: {ID: 11, TaskID: 1, CreatedAt: msgAt, Sender: "human", Text: "Ship it", Metadata: []byte(`{"k":"v"}`), Attachments: []byte(`[{"id":"a1"}]`)},
		3: {ID: 31, TaskID: 3, CreatedAt: msgAt, Sender: "agent", Text: "Done"},
	}, nil)

	res, err := e.controller.ListTaskChanges(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if m := res.LastMessages[1]; m.ID != 11 || m.Text != "Ship it" || len(m.Attachments) != 1 || m.Metadata == nil || len(res.LastMessages) != 2 {
		t.Errorf("the others' newest messages = %+v", res.LastMessages)
	}
	// Each changed when the others last did: a message, or a move.
	want := map[int64]time.Time{1: msgAt, 2: moveAt, 3: msgAt.Add(time.Minute)}
	for id, at := range want {
		if !res.ChangedAt[id].Equal(at) {
			t.Errorf("task %d changed at %v, want %v", id, res.ChangedAt[id], at)
		}
	}
}

// An account with no supervisor workspace has nothing of its own to leave
// out: every change counts.
func TestListTaskChanges_ExcludeSelfWithNoSupervisorWorkspace(t *testing.T) {
	e := newTestController(t)
	req := changesRequest()
	req.ExcludeSelf = true
	e.repo.EXPECT().ListWorkspaces(gomock.Any(), testUserID, false).Return([]model.Workspace{{ID: 8, Name: "app"}}, nil)
	e.repo.EXPECT().ListTaskChanges(gomock.Any(), req, testUserID, int64(0)).Return(nil, nil)
	if res, err := e.controller.ListTaskChanges(context.Background(), req); err != nil || len(res.Tasks) != 0 {
		t.Errorf("ListTaskChanges = %+v, %v", res, err)
	}
}

func TestListTaskChanges_ExcludeSelfErrors(t *testing.T) {
	req := changesRequest()
	req.ExcludeSelf = true
	t.Run("lookup", func(t *testing.T) {
		e := newTestController(t)
		e.repo.EXPECT().ListWorkspaces(gomock.Any(), testUserID, false).Return(nil, errChangesDBDown)
		if res, err := e.controller.ListTaskChanges(context.Background(), req); !errors.Is(err, errChangesDBDown) || res != nil {
			t.Errorf("ListTaskChanges = %+v, %v; want the lookup's error", res, err)
		}
	})
	t.Run("newest messages", func(t *testing.T) {
		e := newTestController(t)
		e.repo.EXPECT().ListWorkspaces(gomock.Any(), testUserID, false).Return(supervisorWorkspaces, nil)
		e.repo.EXPECT().ListTaskChanges(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return([]model.Task{{ID: 1}}, nil)
		e.repo.EXPECT().CountNewMessages(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil)
		e.repo.EXPECT().ListStatusChanges(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil, nil)
		e.repo.EXPECT().ListNewestMessages(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errChangesDBDown)
		if res, err := e.controller.ListTaskChanges(context.Background(), req); !errors.Is(err, errChangesDBDown) || res != nil {
			t.Errorf("ListTaskChanges = %+v, %v; want the repository's error", res, err)
		}
	})
}
