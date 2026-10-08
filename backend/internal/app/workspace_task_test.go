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

	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	mock_repository "github.com/agentrq/agentrq/backend/internal/service/mocks/repository"
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
	repo.EXPECT().GetTask(gomock.Any(), memParent, int64(101), memUser).Return(model.Task{}, errRepo)

	if _, err := workspaceTask(context.Background(), repo, memParent, monoflake.ID(memUser).String(), 101); err != errRepo {
		t.Errorf("err = %v, want %v", err, errRepo)
	}
}
