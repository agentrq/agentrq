// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package crud

import (
	"context"
	"errors"
	"testing"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/golang/mock/gomock"
)

var errChangesDBDown = errors.New("database unavailable")

func changesRequest() entity.ListTaskChangesRequest {
	return entity.ListTaskChangesRequest{UserID: testUserIDStr, Since: time.Date(2026, 11, 14, 4, 0, 0, 0, time.UTC), WorkspaceID: 7, Limit: 21}
}

func TestListTaskChanges(t *testing.T) {
	e := newTestController(t)
	req := changesRequest()
	at := req.Since.Add(time.Minute)
	e.repo.EXPECT().ListTaskChanges(gomock.Any(), req, testUserID).Return([]model.Task{
		{ID: 1, Title: "a", Messages: []model.Message{{ID: 10, TaskID: 1, Sender: "agent", Text: "Done?"}}},
		{ID: 2, Title: "b"},
	}, nil)
	e.repo.EXPECT().CountNewMessages(gomock.Any(), []int64{1, 2}, req.Since).Return(map[int64]int{1: 3}, nil)
	e.repo.EXPECT().ListStatusChanges(gomock.Any(), []int64{1, 2}, req.Since).Return(map[int64]time.Time{2: at}, nil)

	res, err := e.controller.ListTaskChanges(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Tasks) != 2 || res.Tasks[0].Title != "a" || len(res.Tasks[0].Messages) != 1 || res.Tasks[0].Messages[0].Text != "Done?" {
		t.Errorf("tasks = %+v, want both, the first with its message", res.Tasks)
	}
	if res.NewMessages[1] != 3 || !res.StatusChangedAt[2].Equal(at) {
		t.Errorf("new messages %v, status changes %v", res.NewMessages, res.StatusChangedAt)
	}
}

// Nothing changed asks nothing more of the database.
func TestListTaskChanges_NoneChanged(t *testing.T) {
	e := newTestController(t)
	e.repo.EXPECT().ListTaskChanges(gomock.Any(), gomock.Any(), testUserID).Return(nil, nil)
	res, err := e.controller.ListTaskChanges(context.Background(), changesRequest())
	if err != nil || res == nil || len(res.Tasks) != 0 {
		t.Errorf("ListTaskChanges = %+v, %v; want an empty answer", res, err)
	}
}

func TestListTaskChanges_RepositoryErrors(t *testing.T) {
	changed := []model.Task{{ID: 1}}
	for name, expect := range map[string]func(e *testEnv){
		"tasks": func(e *testEnv) {
			e.repo.EXPECT().ListTaskChanges(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errChangesDBDown)
		},
		"messages": func(e *testEnv) {
			e.repo.EXPECT().ListTaskChanges(gomock.Any(), gomock.Any(), gomock.Any()).Return(changed, nil)
			e.repo.EXPECT().CountNewMessages(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errChangesDBDown)
		},
		"status": func(e *testEnv) {
			e.repo.EXPECT().ListTaskChanges(gomock.Any(), gomock.Any(), gomock.Any()).Return(changed, nil)
			e.repo.EXPECT().CountNewMessages(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil)
			e.repo.EXPECT().ListStatusChanges(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errChangesDBDown)
		},
	} {
		t.Run(name, func(t *testing.T) {
			e := newTestController(t)
			expect(e)
			if res, err := e.controller.ListTaskChanges(context.Background(), changesRequest()); !errors.Is(err, errChangesDBDown) || res != nil {
				t.Errorf("ListTaskChanges = %+v, %v; want the repository's error", res, err)
			}
		})
	}
}
