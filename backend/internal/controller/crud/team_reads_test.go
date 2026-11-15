// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package crud

import (
	"context"
	"testing"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/golang/mock/gomock"
)

// The team reads pass a failed read on, and read nothing more after it.
func TestTeamReads_Failures(t *testing.T) {
	ctx := context.Background()

	t.Run("tasks", func(t *testing.T) {
		e := newTestController(t)
		e.repo.EXPECT().ListTasksInWorkspaces(gomock.Any(), gomock.Any(), []int64{1}).Return(nil, errDBDown)
		_, err := e.controller.SystemListTasksInWorkspaces(ctx, entity.ListTasksRequest{}, []int64{1})
		wantErrIs(t, "SystemListTasksInWorkspaces", err, errDBDown)
	})
	t.Run("stats", func(t *testing.T) {
		e := newTestController(t)
		e.repo.EXPECT().GetDetailedStatsOfWorkspaces(gomock.Any(), []int64{1}, gomock.Any(), gomock.Any()).
			Return(entity.GetDetailedWorkspaceStatsResponse{}, errDBDown)
		_, err := e.controller.SystemGetDetailedStatsOfWorkspaces(ctx, entity.GetStatsOfWorkspacesRequest{WorkspaceIDs: []int64{1}})
		wantErrIs(t, "SystemGetDetailedStatsOfWorkspaces", err, errDBDown)
	})
}

// The statistics of several workspaces are read over the window the range
// names, and passed back as read.
func TestSystemGetDetailedStatsOfWorkspaces(t *testing.T) {
	e := newTestController(t)
	e.repo.EXPECT().GetDetailedStatsOfWorkspaces(gomock.Any(), []int64{1, 2}, int64(5), int64(9)).
		Return(entity.GetDetailedWorkspaceStatsResponse{Summary: entity.WorkspaceStatsSummary{Messages: 4}}, nil)
	rs, err := e.controller.SystemGetDetailedStatsOfWorkspaces(context.Background(), entity.GetStatsOfWorkspacesRequest{
		WorkspaceIDs: []int64{1, 2}, Range: "custom", From: 5, To: 9,
	})
	if err != nil || rs.Summary.Messages != 4 {
		t.Errorf("SystemGetDetailedStatsOfWorkspaces = %+v, %v", rs, err)
	}
}
