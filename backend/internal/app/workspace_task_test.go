// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/golang/mock/gomock"
	"github.com/mustafaturan/monoflake"
	"gorm.io/gorm"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	mock_pubsub "github.com/agentrq/agentrq/backend/internal/service/mocks/pubsub"
	mock_repository "github.com/agentrq/agentrq/backend/internal/service/mocks/repository"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
)

// The database only checks that a task exists, so this read is what stops one
// workspace's agent writing into a task that belongs to another workspace, or
// to somebody else.
func TestWorkspaceTaskRefusesATaskFromElsewhere(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Task{}, &model.Message{}, &model.ToolCall{}); err != nil {
		t.Fatal(err)
	}
	const otherUser = memUser + 1
	tasks := []model.Task{
		{ID: 101, WorkspaceID: memParent, UserID: memUser, Title: "mine", Status: "ongoing"},
		{ID: 102, WorkspaceID: memFork, UserID: memUser, Title: "my other workspace", Status: "ongoing"},
		{ID: 103, WorkspaceID: memParent, UserID: otherUser, Title: "somebody else's", Status: "ongoing"},
	}
	for _, task := range tasks {
		if err := db.Create(&task).Error; err != nil {
			t.Fatal(err)
		}
	}
	repo := base.New(sqliteConn{db: db})
	owner := monoflake.ID(memUser).String()
	ctx := context.Background()

	got, err := workspaceTask(ctx, repo, memParent, owner, 101)
	if err != nil || got.Title != "mine" {
		t.Fatalf("own task: %+v, %v", got, err)
	}

	for _, id := range []int64{102, 103, 999} {
		_, err := workspaceTask(ctx, repo, memParent, owner, id)
		want := "task " + monoflake.ID(id).String() + " is not in this workspace"
		if err == nil || err.Error() != want {
			t.Errorf("task %d: err = %v, want %q", id, err, want)
		}
	}
}

func TestWorkspaceTaskReportsStorageFailures(t *testing.T) {
	ctrl := gomock.NewController(t)
	repo := mock_repository.NewMockRepository(ctrl)
	repo.EXPECT().SystemGetTask(gomock.Any(), int64(101)).Return(model.Task{}, errRepo)

	if _, err := workspaceTask(context.Background(), repo, memParent, monoflake.ID(memUser).String(), 101); err != errRepo {
		t.Errorf("err = %v, want %v", err, errRepo)
	}
}

// An agent's status change reads and saves the task row only, and announces
// the change; a completion is announced as one too.
func TestSetAgentTaskStatus(t *testing.T) {
	owner := monoflake.ID(memUser).String()
	row := model.Task{ID: 101, WorkspaceID: memParent, UserID: memUser, Status: "ongoing"}
	published := func(ps *mock_pubsub.MockService) *[]entity.Action {
		var actions []entity.Action
		ps.EXPECT().Publish(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, req pubsub.PublishRequest) (*pubsub.PublishResponse, error) {
			ev := req.Event.(entity.CRUDEvent)
			if req.PubSubID != entity.PubSubTopicCRUD || ev.WorkspaceID != memParent || ev.UserID != memUser || ev.ResourceID != 101 || ev.Actor != entity.ActorAgent {
				t.Errorf("published %+v", req)
			}
			actions = append(actions, ev.Action)
			return nil, nil
		}).AnyTimes()
		return &actions
	}

	for _, tc := range []struct {
		status string
		want   []entity.Action
	}{
		{"completed", []entity.Action{entity.ActionTaskComplete, entity.ActionTaskUpdate}},
		{"done", []entity.Action{entity.ActionTaskComplete, entity.ActionTaskUpdate}},
		{"blocked", []entity.Action{entity.ActionTaskUpdate}},
	} {
		t.Run(tc.status, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			repo := mock_repository.NewMockRepository(ctrl)
			ps := mock_pubsub.NewMockService(ctrl)
			repo.EXPECT().GetTaskRow(gomock.Any(), memParent, int64(101), memUser).Return(row, nil)
			repo.EXPECT().UpdateTask(gomock.Any(), gomock.Any(), "Status").DoAndReturn(func(_ context.Context, m model.Task, _ ...string) (model.Task, error) {
				return m, nil
			})
			actions := published(ps)

			got, err := setAgentTaskStatus(context.Background(), repo, ps, memParent, owner, 101, tc.status)
			if err != nil || got.Status != tc.status {
				t.Fatalf("got %+v, %v", got, err)
			}
			if len(*actions) != len(tc.want) || (*actions)[0] != tc.want[0] {
				t.Errorf("published %v, want %v", *actions, tc.want)
			}
		})
	}

	t.Run("unchanged", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		repo := mock_repository.NewMockRepository(ctrl)
		repo.EXPECT().GetTaskRow(gomock.Any(), memParent, int64(101), memUser).Return(row, nil)
		got, err := setAgentTaskStatus(context.Background(), repo, mock_pubsub.NewMockService(ctrl), memParent, owner, 101, "ongoing")
		if err != nil || got.ID != 101 {
			t.Fatalf("got %+v, %v", got, err)
		}
	})

	t.Run("not read", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		repo := mock_repository.NewMockRepository(ctrl)
		repo.EXPECT().GetTaskRow(gomock.Any(), memParent, int64(101), memUser).Return(model.Task{}, errRepo)
		if _, err := setAgentTaskStatus(context.Background(), repo, mock_pubsub.NewMockService(ctrl), memParent, owner, 101, "completed"); err != errRepo {
			t.Errorf("err = %v, want %v", err, errRepo)
		}
	})

	t.Run("not saved", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		repo := mock_repository.NewMockRepository(ctrl)
		repo.EXPECT().GetTaskRow(gomock.Any(), memParent, int64(101), memUser).Return(row, nil)
		repo.EXPECT().UpdateTask(gomock.Any(), gomock.Any(), "Status").Return(model.Task{}, errRepo)
		if _, err := setAgentTaskStatus(context.Background(), repo, mock_pubsub.NewMockService(ctrl), memParent, owner, 101, "completed"); err != errRepo {
			t.Errorf("err = %v, want %v", err, errRepo)
		}
	})
}
