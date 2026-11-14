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
	t.Run("skill file", func(t *testing.T) {
		e := newTestController(t)
		e.repo.EXPECT().GetSkill(gomock.Any(), int64(7), "deploy").Return(model.Skill{}, base.ErrNotFound)
		_, err := e.controller.SystemGetSkillFile(ctx, entity.SystemGetSkillFileRequest{UserID: 7, Name: "deploy", Path: "SKILL.md"})
		wantErrIs(t, "SystemGetSkillFile", err, base.ErrNotFound)
	})
}

// CopySkill refuses a request with no accounts or a bad name, a workspace
// the caller does not own and a name they already have, and leaves no blob
// behind when a read or the write fails.
func TestCopySkill_Failures(t *testing.T) {
	ctx := context.Background()
	req := entity.CopySkillRequest{UserID: testUserIDStr, FromUserID: 7, Name: "deploy", WorkspaceIDs: []int64{1}}
	owned := func(e *testEnv) {
		e.repo.EXPECT().CheckWorkspaceAccess(gomock.Any(), int64(1), testUserID).Return(true, nil)
		e.repo.EXPECT().SystemGetWorkspace(gomock.Any(), int64(1)).Return(activeWorkspace(), nil)
	}
	newSkill := func(e *testEnv) {
		owned(e)
		e.repo.EXPECT().GetSkill(gomock.Any(), testUserID, "deploy").Return(model.Skill{}, base.ErrNotFound)
	}
	src := model.Skill{ID: 70, UserID: 7, Name: "deploy"}
	files := []model.SkillFile{{Path: "SKILL.md", StorageID: "s1"}, {Path: "run.sh", StorageID: "s2"}}

	for _, tc := range []struct {
		name  string
		req   func(entity.CopySkillRequest) entity.CopySkillRequest
		setup func(e *testEnv)
		check func(t *testing.T, err error)
	}{
		{"no caller", func(r entity.CopySkillRequest) entity.CopySkillRequest { r.UserID = ""; return r }, func(*testEnv) {},
			func(t *testing.T, err error) { wantSkillKind(t, err, SkillInvalid) }},
		{"no source", func(r entity.CopySkillRequest) entity.CopySkillRequest { r.FromUserID = 0; return r }, func(*testEnv) {},
			func(t *testing.T, err error) { wantSkillKind(t, err, SkillInvalid) }},
		{"a bad name", func(r entity.CopySkillRequest) entity.CopySkillRequest { r.Name = "Not A Name"; return r }, func(*testEnv) {},
			func(t *testing.T, err error) { wantSkillKind(t, err, SkillInvalid) }},
		{"someone else's workspace", nil, func(e *testEnv) {
			e.repo.EXPECT().CheckWorkspaceAccess(gomock.Any(), int64(1), testUserID).Return(false, nil)
		}, func(t *testing.T, err error) { wantErrIs(t, "CopySkill", err, base.ErrNotFound) }},
		{"a name the caller has", nil, func(e *testEnv) {
			owned(e)
			e.repo.EXPECT().GetSkill(gomock.Any(), testUserID, "deploy").Return(model.Skill{ID: 1}, nil)
		}, func(t *testing.T, err error) { wantSkillKind(t, err, SkillConflict) }},
		{"the caller's skills unreadable", nil, func(e *testEnv) {
			owned(e)
			e.repo.EXPECT().GetSkill(gomock.Any(), testUserID, "deploy").Return(model.Skill{}, errDBDown)
		}, func(t *testing.T, err error) { wantErrIs(t, "CopySkill", err, errDBDown) }},
		{"the source gone", nil, func(e *testEnv) {
			newSkill(e)
			e.repo.EXPECT().GetSkill(gomock.Any(), int64(7), "deploy").Return(model.Skill{}, base.ErrNotFound)
		}, func(t *testing.T, err error) { wantErrIs(t, "CopySkill", err, base.ErrNotFound) }},
		{"the source's files unreadable", nil, func(e *testEnv) {
			newSkill(e)
			e.repo.EXPECT().GetSkill(gomock.Any(), int64(7), "deploy").Return(src, nil)
			e.repo.EXPECT().ListSkillFiles(gomock.Any(), int64(70)).Return(nil, errDBDown)
		}, func(t *testing.T, err error) { wantErrIs(t, "CopySkill", err, errDBDown) }},
		{"a blob unreadable", nil, func(e *testEnv) {
			newSkill(e)
			e.repo.EXPECT().GetSkill(gomock.Any(), int64(7), "deploy").Return(src, nil)
			e.repo.EXPECT().ListSkillFiles(gomock.Any(), int64(70)).Return(files, nil)
			e.idgen.EXPECT().NextID().Return(int64(100)).AnyTimes()
			e.storage.EXPECT().LoadRaw("s1").Return([]byte("---\nname: deploy\n---\n"), nil)
			e.storage.EXPECT().Save(gomock.Any(), gomock.Any()).Return(nil)
			e.storage.EXPECT().LoadRaw("s2").Return(nil, errDBDown)
			e.storage.EXPECT().Delete(gomock.Any()).Return(nil) // the copy of SKILL.md
		}, func(t *testing.T, err error) { wantErrIs(t, "CopySkill", err, errDBDown) }},
		{"a blob unwritable", nil, func(e *testEnv) {
			newSkill(e)
			e.repo.EXPECT().GetSkill(gomock.Any(), int64(7), "deploy").Return(src, nil)
			e.repo.EXPECT().ListSkillFiles(gomock.Any(), int64(70)).Return(files[:1], nil)
			e.idgen.EXPECT().NextID().Return(int64(100)).AnyTimes()
			e.storage.EXPECT().LoadRaw("s1").Return([]byte("x"), nil)
			e.storage.EXPECT().Save(gomock.Any(), gomock.Any()).Return(errDBDown)
		}, func(t *testing.T, err error) { wantErrIs(t, "CopySkill", err, errDBDown) }},
		{"the write failed", nil, func(e *testEnv) {
			newSkill(e)
			e.repo.EXPECT().GetSkill(gomock.Any(), int64(7), "deploy").Return(src, nil)
			e.repo.EXPECT().ListSkillFiles(gomock.Any(), int64(70)).Return(files[:1], nil)
			e.idgen.EXPECT().NextID().Return(int64(100)).AnyTimes()
			e.storage.EXPECT().LoadRaw("s1").Return([]byte("x"), nil)
			e.storage.EXPECT().Save(gomock.Any(), gomock.Any()).Return(nil)
			e.repo.EXPECT().ReplaceSkill(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(model.Skill{}, nil, errDBDown)
			e.storage.EXPECT().Delete(gomock.Any()).Return(nil)
		}, func(t *testing.T, err error) { wantErrIs(t, "CopySkill", err, errDBDown) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newTestController(t)
			tc.setup(e)
			r := req
			if tc.req != nil {
				r = tc.req(r)
			}
			_, err := e.controller.CopySkill(ctx, r)
			tc.check(t, err)
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
