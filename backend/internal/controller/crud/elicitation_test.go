// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package crud

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/golang/mock/gomock"
)

func elicitationMessage(id int64, askedAt time.Time, meta map[string]any) model.Message {
	meta["type"] = "elicitation_request"
	b, _ := json.Marshal(meta)
	return model.Message{ID: id, TaskID: 10, CreatedAt: askedAt, Metadata: b}
}

func closeRequest() entity.CloseExpiredElicitationRequest {
	return entity.CloseExpiredElicitationRequest{WorkspaceID: 1, TaskID: 10, UserID: testUserIDStr, RequestID: "req-1"}
}

func TestCloseExpiredElicitation_ClosesAQuestionPastItsDeadline(t *testing.T) {
	past := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	for name, msg := range map[string]model.Message{
		"its expiresAt has passed": elicitationMessage(500, time.Now(), map[string]any{"requestId": "req-1", "status": "pending", "expiresAt": past}),
		// Asked before questions carried a deadline: the longest wait applies.
		"asked over an hour ago without one":              elicitationMessage(500, time.Now().Add(-61*time.Minute), map[string]any{"requestId": "req-1", "status": "pending"}),
		"an unreadable expiresAt, asked over an hour ago": elicitationMessage(500, time.Now().Add(-61*time.Minute), map[string]any{"requestId": "req-1", "status": "pending", "expiresAt": "soon"}),
	} {
		t.Run(name, func(t *testing.T) {
			e := newTestController(t)
			other := elicitationMessage(499, time.Now().Add(-2*time.Hour), map[string]any{"requestId": "req-0", "status": "pending"})
			plain := model.Message{ID: 498, TaskID: 10, Text: "hello"}
			task := model.Task{ID: 10, WorkspaceID: 1, Messages: []model.Message{plain, other, msg}}
			e.repo.EXPECT().GetTask(gomock.Any(), int64(1), int64(10), testUserID).Return(task, nil).Times(2)

			var written map[string]any
			e.repo.EXPECT().UpdateMessageMetadata(gomock.Any(), int64(10), int64(500), gomock.Any()).
				DoAndReturn(func(ctx context.Context, taskID, messageID int64, metadata []byte) error {
					return json.Unmarshal(metadata, &written)
				})

			rs, err := e.controller.CloseExpiredElicitation(context.Background(), closeRequest())
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !rs.Closed || rs.Task.ID != 10 {
				t.Errorf("expected the question closed and the task returned, got %+v", rs)
			}
			// Closed as the timeout closes it, keeping the rest of the question.
			if written["status"] != "cancel" || written["requestId"] != "req-1" || written["type"] != "elicitation_request" {
				t.Errorf("unexpected metadata written: %+v", written)
			}
		})
	}
}

// Before its deadline the agent may still be waiting on another instance.
func TestCloseExpiredElicitation_LeavesOtherQuestionsAlone(t *testing.T) {
	future := time.Now().Add(time.Minute).UTC().Format(time.RFC3339)
	for name, msg := range map[string]model.Message{
		"not past its deadline":   elicitationMessage(500, time.Now().Add(-2*time.Hour), map[string]any{"requestId": "req-1", "status": "pending", "expiresAt": future}),
		"asked under an hour ago": elicitationMessage(500, time.Now().Add(-59*time.Minute), map[string]any{"requestId": "req-1", "status": "pending"}),
		"already answered":        elicitationMessage(500, time.Now().Add(-2*time.Hour), map[string]any{"requestId": "req-1", "status": "accept"}),
		"another request":         elicitationMessage(500, time.Now().Add(-2*time.Hour), map[string]any{"requestId": "req-2", "status": "pending"}),
		"a permission request":    {ID: 500, Metadata: []byte(`{"type":"permission_request","requestId":"req-1","status":"pending"}`)},
	} {
		t.Run(name, func(t *testing.T) {
			e := newTestController(t)
			e.repo.EXPECT().GetTask(gomock.Any(), int64(1), int64(10), testUserID).Return(model.Task{ID: 10, Messages: []model.Message{msg}}, nil)

			rs, err := e.controller.CloseExpiredElicitation(context.Background(), closeRequest())
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if rs.Closed {
				t.Error("expected the question left as it is")
			}
		})
	}
}

func TestCloseExpiredElicitation_Errors(t *testing.T) {
	expired := elicitationMessage(500, time.Now().Add(-2*time.Hour), map[string]any{"requestId": "req-1", "status": "pending"})
	task := model.Task{ID: 10, Messages: []model.Message{expired}}

	t.Run("task not readable", func(t *testing.T) {
		e := newTestController(t)
		e.repo.EXPECT().GetTask(gomock.Any(), int64(1), int64(10), testUserID).Return(model.Task{}, errors.New("database unavailable"))
		if _, err := e.controller.CloseExpiredElicitation(context.Background(), closeRequest()); err == nil {
			t.Fatal("expected an error")
		}
	})
	t.Run("metadata write fails", func(t *testing.T) {
		e := newTestController(t)
		e.repo.EXPECT().GetTask(gomock.Any(), int64(1), int64(10), testUserID).Return(task, nil)
		e.repo.EXPECT().UpdateMessageMetadata(gomock.Any(), int64(10), int64(500), gomock.Any()).Return(errors.New("database unavailable"))
		if _, err := e.controller.CloseExpiredElicitation(context.Background(), closeRequest()); err == nil {
			t.Fatal("expected an error")
		}
	})
	t.Run("task not readable after the write", func(t *testing.T) {
		e := newTestController(t)
		gomock.InOrder(
			e.repo.EXPECT().GetTask(gomock.Any(), int64(1), int64(10), testUserID).Return(task, nil),
			e.repo.EXPECT().UpdateMessageMetadata(gomock.Any(), int64(10), int64(500), gomock.Any()).Return(nil),
			e.repo.EXPECT().GetTask(gomock.Any(), int64(1), int64(10), testUserID).Return(model.Task{}, errors.New("database unavailable")),
		)
		if _, err := e.controller.CloseExpiredElicitation(context.Background(), closeRequest()); err == nil {
			t.Fatal("expected an error")
		}
	})
}
