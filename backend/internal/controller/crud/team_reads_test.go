// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package crud

import (
	"context"
	"errors"
	"testing"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/golang/mock/gomock"
	"gorm.io/gorm"
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
	t.Run("skill file", func(t *testing.T) {
		e := newTestController(t)
		e.repo.EXPECT().GetSkill(gomock.Any(), int64(7), "deploy").Return(model.Skill{}, base.ErrNotFound)
		_, err := e.controller.SystemGetSkillFile(ctx, entity.SystemGetSkillFileRequest{UserID: 7, Name: "deploy", Path: "SKILL.md"})
		wantErrIs(t, "SystemGetSkillFile", err, base.ErrNotFound)
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

// LinkSkill refuses a request with no account or no skill, a workspace the
// caller does not own, a skill that is gone and a name they already have,
// and otherwise stores a link: no file, the source's metadata, on where asked.
func TestLinkSkill(t *testing.T) {
	ctx := context.Background()
	req := entity.LinkSkillRequest{UserID: testUserIDStr, SkillID: 70, WorkspaceIDs: []int64{1}}
	owned := func(e *testEnv) {
		e.repo.EXPECT().CheckWorkspaceAccess(gomock.Any(), int64(1), testUserID).Return(true, nil)
		e.repo.EXPECT().SystemGetWorkspace(gomock.Any(), int64(1)).Return(activeWorkspace(), nil)
	}
	src := model.Skill{ID: 70, UserID: 7, Name: "deploy", Description: "Ship it.", FileCount: 2, TotalBytes: 30}
	found := func(e *testEnv) {
		owned(e)
		e.repo.EXPECT().GetSkillByID(gomock.Any(), int64(70)).Return(src, nil)
	}
	free := func(e *testEnv) {
		found(e)
		e.repo.EXPECT().GetSkill(gomock.Any(), testUserID, "deploy").Return(model.Skill{}, base.ErrNotFound)
		e.idgen.EXPECT().NextID().Return(int64(100)).AnyTimes()
	}

	for _, tc := range []struct {
		name  string
		req   func(entity.LinkSkillRequest) entity.LinkSkillRequest
		setup func(e *testEnv)
		check func(t *testing.T, rs *entity.LinkSkillResponse, err error)
	}{
		{"no caller", func(r entity.LinkSkillRequest) entity.LinkSkillRequest { r.UserID = ""; return r }, func(*testEnv) {},
			func(t *testing.T, _ *entity.LinkSkillResponse, err error) { wantSkillKind(t, err, SkillInvalid) }},
		{"no skill", func(r entity.LinkSkillRequest) entity.LinkSkillRequest { r.SkillID = 0; return r }, func(*testEnv) {},
			func(t *testing.T, _ *entity.LinkSkillResponse, err error) { wantSkillKind(t, err, SkillInvalid) }},
		{"someone else's workspace", nil, func(e *testEnv) {
			e.repo.EXPECT().CheckWorkspaceAccess(gomock.Any(), int64(1), testUserID).Return(false, nil)
		}, func(t *testing.T, _ *entity.LinkSkillResponse, err error) {
			wantErrIs(t, "LinkSkill", err, base.ErrNotFound)
		}},
		{"the source gone", nil, func(e *testEnv) {
			owned(e)
			e.repo.EXPECT().GetSkillByID(gomock.Any(), int64(70)).Return(model.Skill{}, base.ErrNotFound)
		}, func(t *testing.T, _ *entity.LinkSkillResponse, err error) {
			wantErrIs(t, "LinkSkill", err, base.ErrNotFound)
		}},
		{"a name the caller has", nil, func(e *testEnv) {
			found(e)
			e.repo.EXPECT().GetSkill(gomock.Any(), testUserID, "deploy").Return(model.Skill{ID: 1}, nil)
		}, func(t *testing.T, _ *entity.LinkSkillResponse, err error) { wantSkillKind(t, err, SkillConflict) }},
		{"the caller's skills unreadable", nil, func(e *testEnv) {
			found(e)
			e.repo.EXPECT().GetSkill(gomock.Any(), testUserID, "deploy").Return(model.Skill{}, errDBDown)
		}, func(t *testing.T, _ *entity.LinkSkillResponse, err error) { wantErrIs(t, "LinkSkill", err, errDBDown) }},
		{"the name taken a moment ago", nil, func(e *testEnv) {
			free(e)
			e.repo.EXPECT().LinkSkill(gomock.Any(), gomock.Any(), gomock.Any()).Return(model.Skill{}, gorm.ErrDuplicatedKey)
		}, func(t *testing.T, _ *entity.LinkSkillResponse, err error) { wantSkillKind(t, err, SkillConflict) }},
		{"the write failed", nil, func(e *testEnv) {
			free(e)
			e.repo.EXPECT().LinkSkill(gomock.Any(), gomock.Any(), gomock.Any()).Return(model.Skill{}, errDBDown)
		}, func(t *testing.T, _ *entity.LinkSkillResponse, err error) { wantErrIs(t, "LinkSkill", err, errDBDown) }},
		{"linked", nil, func(e *testEnv) {
			free(e)
			e.repo.EXPECT().LinkSkill(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, link model.Skill, on []model.WorkspaceSkill) (model.Skill, error) {
					if link.LinkedSkillID != 70 || link.SourceType != skillSourceTeam || link.UserID != testUserID || link.Name != "deploy" ||
						link.Description != "Ship it." || link.FileCount != 2 || link.TotalBytes != 30 {
						t.Errorf("the link stored is %+v", link)
					}
					if len(on) != 1 || on[0].WorkspaceID != 1 || on[0].UserID != testUserID {
						t.Errorf("the link is turned on in %+v", on)
					}
					return link, nil
				})
		}, func(t *testing.T, rs *entity.LinkSkillResponse, err error) {
			if err != nil || rs.Skill.Name != "deploy" || rs.Skill.SourceType != skillSourceTeam || len(rs.Skill.WorkspaceIDs) != 1 {
				t.Errorf("LinkSkill = %+v, %v", rs, err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newTestController(t)
			tc.setup(e)
			r := req
			if tc.req != nil {
				r = tc.req(r)
			}
			rs, err := e.controller.LinkSkill(ctx, r)
			tc.check(t, rs, err)
		})
	}
}

func wantSkillKind(t *testing.T, err error, kind SkillErrorKind) {
	t.Helper()
	var se *SkillError
	if !errors.As(err, &se) || se.Kind != kind {
		t.Errorf("err = %v, want a skill error of kind %v", err, kind)
	}
}
