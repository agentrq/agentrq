// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package crud

import (
	"errors"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/mustafaturan/monoflake"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
)

func TestRecordSessionActivity(t *testing.T) {
	env := newTestController(t)
	sessions := []entity.SessionActivity{{SessionID: 9, UsageLimited: true}}
	env.repo.EXPECT().RecordSessionActivity(gomock.Any(), int64(3), sessions).Return(errors.New("database unavailable"))

	err := env.controller.RecordSessionActivity(t.Context(), entity.RecordSessionActivityRequest{MachineID: 3, Sessions: sessions})
	if err == nil || err.Error() != "database unavailable" {
		t.Errorf("err = %v", err)
	}
}

// Each workspace once, from its newest session, with its latest ongoing task,
// and a workspace with a task and no session listed too.
func TestListAgentActivity(t *testing.T) {
	env := newTestController(t)
	uid := monoflake.IDFromBase62(testUserBase62).Int64()
	seen := time.Now()

	env.repo.EXPECT().ListLiveSessionsByUser(gomock.Any(), uid).Return([]model.Session{
		{ID: 10, MachineID: 3, WorkspaceID: 7, Model: "opus", LastOutputAt: &seen, UsageLimited: true},
		{ID: 9, MachineID: 4, WorkspaceID: 7, Model: "sonnet"},
	}, nil)
	env.repo.EXPECT().LatestOngoingAgentTasks(gomock.Any(), uid).Return(map[int64]int64{7: 70, 8: 80}, nil)
	env.repo.EXPECT().WorkspaceNamesByID(gomock.Any(), []int64{7}, uid).Return(map[int64]string{7: "api"}, nil)

	rs, err := env.controller.ListAgentActivity(t.Context(), entity.ListAgentActivityRequest{UserID: testUserBase62})
	if err != nil {
		t.Fatal(err)
	}
	want := map[int64]entity.AgentActivity{
		7: {WorkspaceID: 7, WorkspaceName: "api", MachineID: 3, Model: "opus", LastOutputAt: &seen, UsageLimited: true, TaskID: 70},
		8: {WorkspaceID: 8, TaskID: 80},
	}
	if len(rs.Workspaces) != len(want) {
		t.Fatalf("workspaces = %+v", rs.Workspaces)
	}
	for id, w := range want {
		if got := rs.Workspaces[id]; got != w {
			t.Errorf("workspace %d = %+v, want %+v", id, got, w)
		}
	}
}

// The names are best effort: a failed lookup still answers.
func TestListAgentActivity_NamesAreBestEffort(t *testing.T) {
	env := newTestController(t)
	uid := monoflake.IDFromBase62(testUserBase62).Int64()
	env.repo.EXPECT().ListLiveSessionsByUser(gomock.Any(), uid).Return([]model.Session{{ID: 9, MachineID: 3, WorkspaceID: 7}}, nil)
	env.repo.EXPECT().LatestOngoingAgentTasks(gomock.Any(), uid).Return(nil, nil)
	env.repo.EXPECT().WorkspaceNamesByID(gomock.Any(), []int64{7}, uid).Return(nil, errors.New("database unavailable"))

	rs, err := env.controller.ListAgentActivity(t.Context(), entity.ListAgentActivityRequest{UserID: testUserBase62})
	if err != nil {
		t.Fatal(err)
	}
	if got := rs.Workspaces[7]; got.MachineID != 3 || got.WorkspaceName != "" {
		t.Errorf("workspace 7 = %+v", got)
	}
}

func TestListAgentActivity_Fails(t *testing.T) {
	uid := monoflake.IDFromBase62(testUserBase62).Int64()
	t.Run("no user", func(t *testing.T) {
		env := newTestController(t)
		if _, err := env.controller.ListAgentActivity(t.Context(), entity.ListAgentActivityRequest{}); err == nil {
			t.Error("no error")
		}
	})
	t.Run("sessions", func(t *testing.T) {
		env := newTestController(t)
		env.repo.EXPECT().ListLiveSessionsByUser(gomock.Any(), uid).Return(nil, errors.New("database unavailable"))
		if _, err := env.controller.ListAgentActivity(t.Context(), entity.ListAgentActivityRequest{UserID: testUserBase62}); err == nil {
			t.Error("no error")
		}
	})
	t.Run("tasks", func(t *testing.T) {
		env := newTestController(t)
		env.repo.EXPECT().ListLiveSessionsByUser(gomock.Any(), uid).Return(nil, nil)
		env.repo.EXPECT().LatestOngoingAgentTasks(gomock.Any(), uid).Return(nil, errors.New("database unavailable"))
		if _, err := env.controller.ListAgentActivity(t.Context(), entity.ListAgentActivityRequest{UserID: testUserBase62}); err == nil {
			t.Error("no error")
		}
	})
}
