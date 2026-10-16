// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/mustafaturan/monoflake"

	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
	mock_repository "github.com/agentrq/agentrq/backend/internal/service/mocks/repository"
)

// toolCallEvent is what an open page reads off the stream.
type toolCallEvent struct {
	Type    string `json:"type"`
	Payload struct {
		WorkspaceID string `json:"workspaceId"`
		ToolCall    struct {
			ID       string `json:"id"`
			TaskID   string `json:"taskId"`
			ToolName string `json:"toolName"`
			Status   string `json:"status"`
		} `json:"toolCall"`
	} `json:"payload"`
}

func newToolCallRecorder(t *testing.T) (toolCallRecorder, *mock_repository.MockRepository, chan []byte) {
	t.Helper()
	repo := mock_repository.NewMockRepository(gomock.NewController(t))
	bus := eventbus.New()
	stream := bus.Subscribe(memParent, monoflake.ID(memUser).String())
	return toolCallRecorder{repo: repo, bus: bus, workspaceID: memParent, owner: monoflake.ID(memUser).String()}, repo, stream
}

func nextToolCallEvent(t *testing.T, stream chan []byte) toolCallEvent {
	t.Helper()
	select {
	case line := <-stream:
		var got toolCallEvent
		if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(string(line), "data: "))), &got); err != nil {
			t.Fatal(err)
		}
		return got
	default:
		t.Fatal("nothing was published")
		return toolCallEvent{}
	}
}

func assertNothingPublished(t *testing.T, stream chan []byte) {
	t.Helper()
	select {
	case line := <-stream:
		t.Fatalf("unexpected event: %s", line)
	default:
	}
}

// A tool call costs one insert, which checks the task itself, and goes out on
// its own: the strict mock fails on any read of the task or its history.
func TestToolCallRecorderCreatePublishesOnlyTheCall(t *testing.T) {
	r, repo, stream := newToolCallRecorder(t)
	ctx := context.Background()
	tc := model.ToolCall{ID: 5, TaskID: 101, WorkspaceID: memParent, ToolName: "Bash", Status: "pending"}

	repo.EXPECT().CreateToolCall(ctx, tc, memUser).Return(tc, nil)

	created, err := r.create(ctx, tc)
	if err != nil || created.ID != 5 {
		t.Fatalf("create = %+v, %v", created, err)
	}
	got := nextToolCallEvent(t, stream)
	if got.Type != "toolCall.updated" || got.Payload.WorkspaceID != monoflake.ID(memParent).String() {
		t.Fatalf("event = %+v", got)
	}
	call := got.Payload.ToolCall
	if call.ID != monoflake.ID(5).String() || call.TaskID != monoflake.ID(101).String() || call.ToolName != "Bash" || call.Status != "pending" {
		t.Errorf("tool call = %+v", call)
	}
	assertNothingPublished(t, stream)
}

func TestToolCallRecorderCreateRefusesATaskFromElsewhere(t *testing.T) {
	r, repo, stream := newToolCallRecorder(t)
	ctx := context.Background()

	repo.EXPECT().CreateToolCall(ctx, model.ToolCall{ID: 5, TaskID: 102, WorkspaceID: memParent}, memUser).Return(model.ToolCall{}, base.ErrNotFound)

	if _, err := r.create(ctx, model.ToolCall{ID: 5, TaskID: 102, WorkspaceID: memFork}); err == nil || !strings.Contains(err.Error(), "is not in this workspace") {
		t.Fatalf("err = %v, want the task refused as not in this workspace", err)
	}
	assertNothingPublished(t, stream)
}

func TestToolCallRecorderCreateReportsStorageFailures(t *testing.T) {
	r, repo, stream := newToolCallRecorder(t)
	ctx := context.Background()
	tc := model.ToolCall{ID: 5, TaskID: 101, WorkspaceID: memParent}

	repo.EXPECT().CreateToolCall(ctx, tc, memUser).Return(model.ToolCall{}, errRepo)

	if _, err := r.create(ctx, tc); err != errRepo {
		t.Fatalf("err = %v, want %v", err, errRepo)
	}
	assertNothingPublished(t, stream)
}

func TestToolCallRecorderAnswerPublishesOnlyTheCall(t *testing.T) {
	r, repo, stream := newToolCallRecorder(t)
	ctx := context.Background()

	repo.EXPECT().UpdateToolCallStatus(ctx, int64(5), "allowed").
		Return(model.ToolCall{ID: 5, TaskID: 101, ToolName: "Bash", Status: "allowed"}, nil)

	if err := r.answer(ctx, 5, "allowed"); err != nil {
		t.Fatalf("answer: %v", err)
	}
	got := nextToolCallEvent(t, stream)
	if got.Type != "toolCall.updated" || got.Payload.ToolCall.ID != monoflake.ID(5).String() || got.Payload.ToolCall.Status != "allowed" {
		t.Fatalf("event = %+v", got)
	}
}

func TestToolCallRecorderAnswerReportsStorageFailures(t *testing.T) {
	r, repo, stream := newToolCallRecorder(t)
	ctx := context.Background()

	repo.EXPECT().UpdateToolCallStatus(ctx, int64(5), "denied").Return(model.ToolCall{}, errRepo)

	if err := r.answer(ctx, 5, "denied"); err != errRepo {
		t.Fatalf("err = %v, want %v", err, errRepo)
	}
	assertNothingPublished(t, stream)
}
