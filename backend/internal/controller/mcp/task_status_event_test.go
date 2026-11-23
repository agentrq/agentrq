// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package mcp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/mustafaturan/monoflake"

	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
	mock_pubsub "github.com/agentrq/agentrq/backend/internal/service/mocks/pubsub"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
)

// statusServer is a workspace server whose task starts in `before`, or whose
// read of it fails when `before` is empty.
func statusServer(t *testing.T, before string) (*WorkspaceServer, chan []byte) {
	t.Helper()
	ctrl := gomock.NewController(t)
	mockPS := mock_pubsub.NewMockService(ctrl)
	mockPS.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(&pubsub.PublishResponse{}, nil).AnyTimes()

	const workspaceID int64 = 100
	bus := eventbus.New()
	sub := bus.Subscribe(workspaceID, "")
	t.Cleanup(func() { bus.Unsubscribe(workspaceID, "", sub) })

	ps := &WorkspaceServer{
		workspaceID: workspaceID,
		userID:      monoflake.ID(15264777).String(),
		pubsub:      mockPS,
		bus:         bus,
		getTask: func(ctx context.Context, taskID int64) (model.Task, error) {
			if before == "" {
				return model.Task{}, errors.New("task not found")
			}
			return model.Task{ID: taskID, Status: before, Title: "Approve DB migration script"}, nil
		},
		updateStatus: func(ctx context.Context, taskID int64, status string) (model.Task, error) {
			return model.Task{ID: taskID, Status: status, Title: "Approve DB migration script"}, nil
		},
	}
	return ps, sub
}

func setStatus(t *testing.T, ps *WorkspaceServer, status string) {
	t.Helper()
	res, _, err := ps.handleUpdateTaskStatus(context.Background(), nil, UpdateTaskStatusParams{
		TaskID: monoflake.ID(42).String(),
		Status: status,
	})
	if err != nil || res.IsError {
		t.Fatalf("handleUpdateTaskStatus: err=%v res=%v", err, res)
	}
}

func noMoreEvents(t *testing.T, sub chan []byte) {
	t.Helper()
	select {
	case line := <-sub:
		t.Fatalf("unexpected event: %s", line)
	case <-time.After(50 * time.Millisecond):
	}
}

// The browser announces an agent's status change from this event, so it has to
// say where the task came from as well as where it went.
func TestHandleUpdateTaskStatus_AnnouncesTheChange(t *testing.T) {
	ps, sub := statusServer(t, "ongoing")
	setStatus(t, ps, "blocked")

	if evt := readEvent(t, sub); evt.Type != "task.updated" {
		t.Fatalf("first event = %q, want task.updated", evt.Type)
	}
	evt := readEvent(t, sub)
	if evt.Type != "task.status" {
		t.Fatalf("second event = %q, want task.status", evt.Type)
	}
	want := map[string]any{
		"taskId":      monoflake.ID(42).String(),
		"workspaceId": monoflake.ID(100).String(),
		"title":       "Approve DB migration script",
		"from":        "ongoing",
		"to":          "blocked",
	}
	for k, v := range want {
		if evt.Payload[k] != v {
			t.Errorf("%s = %#v, want %#v", k, evt.Payload[k], v)
		}
	}
	noMoreEvents(t, sub)
}

// Setting the status a task already has is not news.
func TestHandleUpdateTaskStatus_QuietWhenNothingChanged(t *testing.T) {
	ps, sub := statusServer(t, "ongoing")
	setStatus(t, ps, "ongoing")

	readEvent(t, sub) // task.updated
	noMoreEvents(t, sub)
}

// Without the status it came from there is no transition to show.
func TestHandleUpdateTaskStatus_QuietWhenThePreviousStatusIsUnknown(t *testing.T) {
	ps, sub := statusServer(t, "")
	setStatus(t, ps, "completed")

	readEvent(t, sub) // task.updated
	noMoreEvents(t, sub)
}
