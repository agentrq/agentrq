// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/mustafaturan/monoflake"

	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
	mock_repository "github.com/agentrq/agentrq/backend/internal/service/mocks/repository"
)

// taskPlanEvent is what an open page reads off the stream.
type taskPlanEvent struct {
	Type    string `json:"type"`
	Payload struct {
		WorkspaceID string `json:"workspaceId"`
		TaskID      string `json:"taskId"`
		Plan        *struct {
			ID      string `json:"id"`
			Content string `json:"content"`
			Version int64  `json:"version"`
		} `json:"plan"`
	} `json:"payload"`
}

func newTaskPlanStore(t *testing.T) (taskPlanStore, *mock_repository.MockRepository, chan []byte) {
	t.Helper()
	repo := mock_repository.NewMockRepository(gomock.NewController(t))
	bus := eventbus.New()
	stream := bus.Subscribe(memParent, monoflake.ID(memUser).String())
	return taskPlanStore{repo: repo, bus: bus, workspaceID: memParent, owner: monoflake.ID(memUser).String()}, repo, stream
}

func nextTaskPlanEvent(t *testing.T, stream chan []byte) taskPlanEvent {
	t.Helper()
	select {
	case line := <-stream:
		var got taskPlanEvent
		if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(string(line), "data: "))), &got); err != nil {
			t.Fatal(err)
		}
		return got
	default:
		t.Fatal("nothing was published")
		return taskPlanEvent{}
	}
}

// A plan costs one write, scoped to this workspace's owner, and goes out as
// the task's plan.
func TestTaskPlanStoreSavePublishesThePlan(t *testing.T) {
	s, repo, stream := newTaskPlanStore(t)
	ctx := context.Background()

	repo.EXPECT().SaveTaskPlan(ctx, gomock.Any(), memParent, memUser).DoAndReturn(
		func(_ context.Context, p model.TaskPlan, _, _ int64) (model.TaskPlan, error) {
			if p.ID != 101 || p.Content != "- [ ] Read" || time.Since(p.UpdatedAt) > time.Minute {
				t.Errorf("saved %+v, want task 101's plan, stamped now", p)
			}
			return model.TaskPlan{ID: 101, Content: p.Content, Version: 3}, nil
		})

	if err := s.Save(ctx, 101, "- [ ] Read"); err != nil {
		t.Fatal(err)
	}
	got := nextTaskPlanEvent(t, stream)
	if got.Type != "taskPlan.updated" || got.Payload.WorkspaceID != monoflake.ID(memParent).String() || got.Payload.TaskID != monoflake.ID(101).String() {
		t.Fatalf("event = %+v", got)
	}
	if p := got.Payload.Plan; p == nil || p.ID != monoflake.ID(101).String() || p.Content != "- [ ] Read" || p.Version != 3 {
		t.Errorf("plan = %+v", got.Payload.Plan)
	}
	assertNothingPublished(t, stream)
}

func TestTaskPlanStoreSaveFailures(t *testing.T) {
	s, repo, stream := newTaskPlanStore(t)
	ctx := context.Background()

	repo.EXPECT().SaveTaskPlan(ctx, gomock.Any(), memParent, memUser).Return(model.TaskPlan{}, base.ErrNotFound)
	if err := s.Save(ctx, 102, "x"); err == nil || !strings.Contains(err.Error(), "is not in this workspace") {
		t.Errorf("a task from elsewhere: err = %v", err)
	}
	errRepo := errors.New("database unavailable")
	repo.EXPECT().SaveTaskPlan(ctx, gomock.Any(), memParent, memUser).Return(model.TaskPlan{}, errRepo)
	if err := s.Save(ctx, 101, "x"); !errors.Is(err, errRepo) {
		t.Errorf("a failed write: err = %v, want %v", err, errRepo)
	}
	assertNothingPublished(t, stream)
}

// A withdrawn plan goes out as no plan.
func TestTaskPlanStoreDelete(t *testing.T) {
	s, repo, stream := newTaskPlanStore(t)
	ctx := context.Background()

	repo.EXPECT().DeleteTaskPlan(ctx, int64(101), memParent, memUser).Return(nil)
	if err := s.Delete(ctx, 101); err != nil {
		t.Fatal(err)
	}
	got := nextTaskPlanEvent(t, stream)
	if got.Type != "taskPlan.updated" || got.Payload.TaskID != monoflake.ID(101).String() || got.Payload.Plan != nil {
		t.Fatalf("event = %+v, want task 101 with no plan", got)
	}

	errRepo := errors.New("database unavailable")
	repo.EXPECT().DeleteTaskPlan(ctx, int64(101), memParent, memUser).Return(errRepo)
	if err := s.Delete(ctx, 101); !errors.Is(err, errRepo) {
		t.Errorf("a failed delete: err = %v, want %v", err, errRepo)
	}
	assertNothingPublished(t, stream)
}
