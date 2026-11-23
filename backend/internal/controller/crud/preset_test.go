// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package crud

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/golang/mock/gomock"
)

const (
	presetWorkspace = int64(1)
	presetFork      = int64(2)
)

// expectPresetAccess lets the caller in to workspace id, which for a fork
// keeps its presets under its parent, presetWorkspace.
func expectPresetAccess(e *machineTelemetryEnv, id int64) {
	e.repo.EXPECT().CheckWorkspaceAccess(gomock.Any(), id, testUserID).Return(true, nil)
	w := model.Workspace{ID: id, UserID: testUserID}
	if id == presetFork {
		w.ForkOfID = presetWorkspace
	}
	e.repo.EXPECT().SystemGetWorkspace(gomock.Any(), id).Return(w, nil)
}

func storedPreset(name, body string) model.Preset {
	return model.Preset{ID: 900, CreatedAt: time.Now(), UserID: testUserID, WorkspaceID: presetWorkspace, Name: name, Body: body}
}

func presetErrorOf(t *testing.T, err error) *entity.PresetError {
	t.Helper()
	var pe *entity.PresetError
	if !errors.As(err, &pe) {
		t.Fatalf("returned %v, want a PresetError", err)
	}
	if pe.Error() != pe.Message {
		t.Errorf("the error reads %q, want its message %q", pe.Error(), pe.Message)
	}
	return pe
}

func TestListPresets_AForkListsItsParents(t *testing.T) {
	e := newMachineTelemetryEnv(t)
	expectPresetAccess(e, presetFork)
	e.repo.EXPECT().ListPresets(gomock.Any(), testUserID, presetWorkspace).Return([]model.Preset{storedPreset("pull-n-push", "pull latest main, tag and push")}, nil)

	rs, err := e.controller.ListPresets(context.Background(), entity.ListPresetsRequest{WorkspaceID: presetFork, UserID: testUserIDStr})
	if err != nil {
		t.Fatalf("ListPresets returned %v", err)
	}
	if len(rs.Presets) != 1 || rs.Presets[0].Name != "pull-n-push" || rs.Presets[0].Body != "pull latest main, tag and push" || rs.Presets[0].WorkspaceID != presetWorkspace {
		t.Fatalf("ListPresets returned %+v, want the parent's pull-n-push preset", rs.Presets)
	}
	if len(*e.events) != 0 {
		t.Errorf("a list published %+v, want nothing counted", *e.events)
	}
}

func TestListPresets_ReportsFailures(t *testing.T) {
	t.Run("another account's workspace is not found", func(t *testing.T) {
		e := newMachineTelemetryEnv(t)
		e.repo.EXPECT().CheckWorkspaceAccess(gomock.Any(), presetWorkspace, testUserID).Return(false, nil)
		_, err := e.controller.ListPresets(context.Background(), entity.ListPresetsRequest{WorkspaceID: presetWorkspace, UserID: testUserIDStr})
		if !errors.Is(err, base.ErrNotFound) {
			t.Fatalf("returned %v, want ErrNotFound", err)
		}
	})
	t.Run("a database failure comes back", func(t *testing.T) {
		e := newMachineTelemetryEnv(t)
		expectPresetAccess(e, presetWorkspace)
		e.repo.EXPECT().ListPresets(gomock.Any(), testUserID, presetWorkspace).Return(nil, errors.New("database unavailable"))
		_, err := e.controller.ListPresets(context.Background(), entity.ListPresetsRequest{WorkspaceID: presetWorkspace, UserID: testUserIDStr})
		if err == nil || err.Error() != "database unavailable" {
			t.Fatalf("returned %v, want \"database unavailable\"", err)
		}
	})
}

func TestCreatePreset_SavesUnderTheCanonicalNameAndCountsIt(t *testing.T) {
	e := newMachineTelemetryEnv(t)
	expectPresetAccess(e, presetFork)
	e.repo.EXPECT().ListPresets(gomock.Any(), testUserID, presetWorkspace).Return(nil, nil)
	e.idgen.EXPECT().NextID().Return(int64(900))
	e.repo.EXPECT().CreatePreset(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, p model.Preset) (model.Preset, error) {
		if p.ID != 900 || p.UserID != testUserID || p.WorkspaceID != presetWorkspace || p.Name != "pull-n-push" || p.Body != "pull latest main, tag and push" {
			t.Errorf("CreatePreset was asked to store %+v, want pull-n-push under the parent workspace", p)
		}
		return p, nil
	})

	rs, err := e.controller.CreatePreset(context.Background(), entity.CreatePresetRequest{
		WorkspaceID: presetFork, UserID: testUserIDStr, Name: " Pull-N-Push ", Body: "pull latest main, tag and push",
	})
	if err != nil {
		t.Fatalf("CreatePreset returned %v", err)
	}
	if rs.Preset.Name != "pull-n-push" {
		t.Errorf("the saved name is %q, want pull-n-push", rs.Preset.Name)
	}
	ev := e.only(t)
	if ev.Action != entity.ActionPresetCreate || ev.WorkspaceID != presetFork || ev.UserID != testUserID || ev.Actor != entity.ActorHuman {
		t.Errorf("published %+v, want a preset_create by the human in the fork it was made on", ev)
	}
}

func TestCreatePreset_RefusesWhatItCannotKeep(t *testing.T) {
	for _, tc := range []struct {
		name, preset, body, want string
	}{
		{"a name with a space", "pull n push", "x", "preset name \"pull n push\" is not usable"},
		{"an empty name", "", "x", "is not usable"},
		{"a blank body", "deploy", " \n", "preset body is empty"},
		{"a body over the limit", "deploy", strings.Repeat("x", MaxPresetBodyBytes+1), "the limit is 16384"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newMachineTelemetryEnv(t)
			_, err := e.controller.CreatePreset(context.Background(), entity.CreatePresetRequest{WorkspaceID: presetWorkspace, UserID: testUserIDStr, Name: tc.preset, Body: tc.body})
			pe := presetErrorOf(t, err)
			if pe.Conflict || !strings.Contains(pe.Message, tc.want) {
				t.Errorf("refused with %+v, want a message containing %q and no conflict", pe, tc.want)
			}
		})
	}
}

func TestCreatePreset_RefusesAFullWorkspace(t *testing.T) {
	e := newMachineTelemetryEnv(t)
	expectPresetAccess(e, presetWorkspace)
	e.repo.EXPECT().ListPresets(gomock.Any(), testUserID, presetWorkspace).Return(make([]model.Preset, MaxPresetsPerWorkspace), nil)
	_, err := e.controller.CreatePreset(context.Background(), entity.CreatePresetRequest{WorkspaceID: presetWorkspace, UserID: testUserIDStr, Name: "deploy", Body: "x"})
	if pe := presetErrorOf(t, err); !strings.Contains(pe.Message, "up to 100 presets") {
		t.Errorf("refused with %q, want the 100 preset cap named", pe.Message)
	}
}

func TestCreatePreset_ANameTakenIsAConflict(t *testing.T) {
	e := newMachineTelemetryEnv(t)
	expectPresetAccess(e, presetWorkspace)
	e.repo.EXPECT().ListPresets(gomock.Any(), testUserID, presetWorkspace).Return(nil, nil)
	e.idgen.EXPECT().NextID().Return(int64(900))
	e.repo.EXPECT().CreatePreset(gomock.Any(), gomock.Any()).Return(model.Preset{}, base.ErrPresetExists)
	_, err := e.controller.CreatePreset(context.Background(), entity.CreatePresetRequest{WorkspaceID: presetWorkspace, UserID: testUserIDStr, Name: "deploy", Body: "x"})
	if pe := presetErrorOf(t, err); !pe.Conflict || pe.Message != `a preset named "deploy" exists` {
		t.Errorf("refused with %+v, want a conflict naming deploy", pe)
	}
	if len(*e.events) != 0 {
		t.Errorf("a refused save published %+v, want nothing counted", *e.events)
	}
}

func TestCreatePreset_ReportsFailures(t *testing.T) {
	req := entity.CreatePresetRequest{WorkspaceID: presetWorkspace, UserID: testUserIDStr, Name: "deploy", Body: "x"}
	t.Run("another account's workspace is not found", func(t *testing.T) {
		e := newMachineTelemetryEnv(t)
		e.repo.EXPECT().CheckWorkspaceAccess(gomock.Any(), presetWorkspace, testUserID).Return(false, nil)
		if _, err := e.controller.CreatePreset(context.Background(), req); !errors.Is(err, base.ErrNotFound) {
			t.Fatalf("returned %v, want ErrNotFound", err)
		}
	})
	t.Run("the count cannot be read", func(t *testing.T) {
		e := newMachineTelemetryEnv(t)
		expectPresetAccess(e, presetWorkspace)
		e.repo.EXPECT().ListPresets(gomock.Any(), testUserID, presetWorkspace).Return(nil, errors.New("database unavailable"))
		if _, err := e.controller.CreatePreset(context.Background(), req); err == nil || err.Error() != "database unavailable" {
			t.Fatalf("returned %v, want \"database unavailable\"", err)
		}
	})
	t.Run("the save fails", func(t *testing.T) {
		e := newMachineTelemetryEnv(t)
		expectPresetAccess(e, presetWorkspace)
		e.repo.EXPECT().ListPresets(gomock.Any(), testUserID, presetWorkspace).Return(nil, nil)
		e.idgen.EXPECT().NextID().Return(int64(900))
		e.repo.EXPECT().CreatePreset(gomock.Any(), gomock.Any()).Return(model.Preset{}, errors.New("database unavailable"))
		if _, err := e.controller.CreatePreset(context.Background(), req); err == nil || err.Error() != "database unavailable" {
			t.Fatalf("returned %v, want \"database unavailable\"", err)
		}
	})
}

func TestDeletePreset_RemovesItFromTheParentAndCountsIt(t *testing.T) {
	e := newMachineTelemetryEnv(t)
	expectPresetAccess(e, presetFork)
	e.repo.EXPECT().DeletePreset(gomock.Any(), testUserID, presetWorkspace, "deploy").Return(nil)
	if err := e.controller.DeletePreset(context.Background(), entity.DeletePresetRequest{WorkspaceID: presetFork, UserID: testUserIDStr, Name: "deploy"}); err != nil {
		t.Fatalf("DeletePreset returned %v", err)
	}
	if ev := e.only(t); ev.Action != entity.ActionPresetDelete || ev.WorkspaceID != presetFork {
		t.Errorf("published %+v, want a preset_delete in the fork", ev)
	}
}

func TestDeletePreset_ReportsFailures(t *testing.T) {
	req := entity.DeletePresetRequest{WorkspaceID: presetWorkspace, UserID: testUserIDStr, Name: "deploy"}
	t.Run("another account's workspace is not found", func(t *testing.T) {
		e := newMachineTelemetryEnv(t)
		e.repo.EXPECT().CheckWorkspaceAccess(gomock.Any(), presetWorkspace, testUserID).Return(false, nil)
		if err := e.controller.DeletePreset(context.Background(), req); !errors.Is(err, base.ErrNotFound) {
			t.Fatalf("returned %v, want ErrNotFound", err)
		}
	})
	t.Run("a preset not there is not found, and not counted", func(t *testing.T) {
		e := newMachineTelemetryEnv(t)
		expectPresetAccess(e, presetWorkspace)
		e.repo.EXPECT().DeletePreset(gomock.Any(), testUserID, presetWorkspace, "deploy").Return(base.ErrNotFound)
		if err := e.controller.DeletePreset(context.Background(), req); !errors.Is(err, base.ErrNotFound) {
			t.Fatalf("returned %v, want ErrNotFound", err)
		}
		if len(*e.events) != 0 {
			t.Errorf("published %+v, want nothing counted", *e.events)
		}
	})
}
