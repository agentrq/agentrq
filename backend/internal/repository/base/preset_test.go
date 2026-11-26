// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package base

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func presetDB(t *testing.T) (*gorm.DB, Repository) {
	t.Helper()
	// TranslateError as the app opens its database, so a rename onto a
	// taken name reads as gorm.ErrDuplicatedKey.
	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&model.Preset{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db, New(&mockDB{db: db})
}

func preset(id, userID, workspaceID int64, name, body string) model.Preset {
	return model.Preset{ID: id, CreatedAt: time.Now(), UserID: userID, WorkspaceID: workspaceID, Name: name, Body: body}
}

func TestPresets_CreateListAndDelete(t *testing.T) {
	_, repo := presetDB(t)
	ctx := context.Background()

	for _, p := range []model.Preset{
		preset(1, memUserID, memWorkspaceID, "pull-n-push", "pull latest main, tag and push"),
		preset(2, memUserID, memWorkspaceID, "deploy", "deploy to staging"),
		preset(3, memUserID, memOtherWSID, "other-workspace", "not listed"),
		preset(4, memOtherUserID, memWorkspaceID, "other-account", "not listed"),
	} {
		if _, err := repo.CreatePreset(ctx, p); err != nil {
			t.Fatalf("CreatePreset(%s) returned %v, want no error", p.Name, err)
		}
	}

	got, err := repo.ListPresets(ctx, memUserID, memWorkspaceID)
	if err != nil {
		t.Fatalf("ListPresets returned %v", err)
	}
	if len(got) != 2 || got[0].Name != "deploy" || got[1].Name != "pull-n-push" {
		t.Fatalf("ListPresets returned %+v, want the workspace's deploy and pull-n-push, by name", got)
	}
	if got[1].Body != "pull latest main, tag and push" {
		t.Errorf("the pull-n-push body read back as %q", got[1].Body)
	}

	if err := repo.DeletePreset(ctx, memUserID, memWorkspaceID, "deploy"); err != nil {
		t.Fatalf("DeletePreset returned %v, want no error", err)
	}
	got, _ = repo.ListPresets(ctx, memUserID, memWorkspaceID)
	if len(got) != 1 || got[0].Name != "pull-n-push" {
		t.Fatalf("after deleting deploy the list is %+v, want only pull-n-push", got)
	}
}

func TestListPresets_AnEmptyWorkspaceIsAnEmptyList(t *testing.T) {
	_, repo := presetDB(t)
	got, err := repo.ListPresets(context.Background(), memUserID, memWorkspaceID)
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("ListPresets returned %v and error %v, want an empty, non-nil list", got, err)
	}
}

func TestCreatePreset_RefusesANameTheWorkspaceHas(t *testing.T) {
	_, repo := presetDB(t)
	ctx := context.Background()
	if _, err := repo.CreatePreset(ctx, preset(1, memUserID, memWorkspaceID, "deploy", "first")); err != nil {
		t.Fatal(err)
	}
	_, err := repo.CreatePreset(ctx, preset(2, memUserID, memWorkspaceID, "deploy", "second"))
	if !errors.Is(err, ErrPresetExists) {
		t.Fatalf("a second deploy preset returned %v, want ErrPresetExists", err)
	}
	got, _ := repo.ListPresets(ctx, memUserID, memWorkspaceID)
	if len(got) != 1 || got[0].Body != "first" {
		t.Fatalf("the list is %+v, want the first deploy preset untouched", got)
	}
}

func TestDeletePreset_ANameNotThereIsNotFound(t *testing.T) {
	_, repo := presetDB(t)
	err := repo.DeletePreset(context.Background(), memUserID, memWorkspaceID, "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleting a missing preset returned %v, want ErrNotFound", err)
	}
}

func TestPresets_DatabaseErrorsComeBack(t *testing.T) {
	db, repo := presetDB(t)
	if err := db.Migrator().DropTable(&model.Preset{}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := repo.ListPresets(ctx, memUserID, memWorkspaceID); err == nil {
		t.Error("ListPresets with no table returned no error")
	}
	if _, err := repo.CreatePreset(ctx, preset(1, memUserID, memWorkspaceID, "deploy", "x")); err == nil || errors.Is(err, ErrPresetExists) {
		t.Errorf("CreatePreset with no table returned %v, want the database's error", err)
	}
	if err := repo.DeletePreset(ctx, memUserID, memWorkspaceID, "deploy"); err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("DeletePreset with no table returned %v, want the database's error", err)
	}
	if _, err := repo.UpdatePreset(ctx, memUserID, memWorkspaceID, "deploy", preset(0, 0, 0, "deploy", "y")); err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("UpdatePreset with no table returned %v, want the database's error", err)
	}
}

func TestUpdatePreset_EditsTheBodyAndRenames(t *testing.T) {
	_, repo := presetDB(t)
	ctx := context.Background()
	for _, p := range []model.Preset{
		preset(1, memUserID, memWorkspaceID, "deploy", "deploy to staging"),
		preset(2, memUserID, memOtherWSID, "deploy", "another workspace's"),
	} {
		if _, err := repo.CreatePreset(ctx, p); err != nil {
			t.Fatal(err)
		}
	}

	got, err := repo.UpdatePreset(ctx, memUserID, memWorkspaceID, "deploy", model.Preset{Name: "deploy", Body: "deploy to prod"})
	if err != nil {
		t.Fatalf("UpdatePreset returned %v, want no error", err)
	}
	if got.ID != 1 || got.Name != "deploy" || got.Body != "deploy to prod" || got.CreatedAt.IsZero() {
		t.Fatalf("UpdatePreset returned %+v, want preset 1 with the new body", got)
	}

	got, err = repo.UpdatePreset(ctx, memUserID, memWorkspaceID, "deploy", model.Preset{Name: "ship", Body: "ship it"})
	if err != nil || got.ID != 1 || got.Name != "ship" || got.Body != "ship it" {
		t.Fatalf("renaming returned %+v, %v; want preset 1 named ship", got, err)
	}
	list, _ := repo.ListPresets(ctx, memUserID, memWorkspaceID)
	if len(list) != 1 || list[0].Name != "ship" {
		t.Fatalf("after the rename the list is %+v, want only ship", list)
	}
	other, _ := repo.ListPresets(ctx, memUserID, memOtherWSID)
	if len(other) != 1 || other[0].Body != "another workspace's" {
		t.Fatalf("the other workspace's presets are %+v, want them untouched", other)
	}
}

func TestUpdatePreset_ANameNotThereIsNotFound(t *testing.T) {
	_, repo := presetDB(t)
	_, err := repo.UpdatePreset(context.Background(), memUserID, memWorkspaceID, "missing", model.Preset{Name: "missing", Body: "x"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("editing a missing preset returned %v, want ErrNotFound", err)
	}
}

func TestUpdatePreset_RefusesANameTheWorkspaceHas(t *testing.T) {
	_, repo := presetDB(t)
	ctx := context.Background()
	for _, p := range []model.Preset{
		preset(1, memUserID, memWorkspaceID, "deploy", "first"),
		preset(2, memUserID, memWorkspaceID, "ship", "second"),
	} {
		if _, err := repo.CreatePreset(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	_, err := repo.UpdatePreset(ctx, memUserID, memWorkspaceID, "deploy", model.Preset{Name: "ship", Body: "changed"})
	if !errors.Is(err, ErrPresetExists) {
		t.Fatalf("renaming deploy to ship returned %v, want ErrPresetExists", err)
	}
	list, _ := repo.ListPresets(ctx, memUserID, memWorkspaceID)
	if len(list) != 2 || list[0].Body != "first" || list[1].Body != "second" {
		t.Fatalf("the list is %+v, want both presets untouched", list)
	}
}

func TestDeleteWorkspace_RemovesItsPresets(t *testing.T) {
	db, repo := forkDB(t)
	ctx := context.Background()
	if _, err := repo.CreatePreset(ctx, preset(1, wfUser, wfParent, "deploy", "x")); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteWorkspace(ctx, wfParent, wfUser); err != nil {
		t.Fatalf("DeleteWorkspace returned %v", err)
	}
	var n int64
	db.Model(&model.Preset{}).Count(&n)
	if n != 0 {
		t.Fatalf("%d presets are left after their workspace was deleted, want 0", n)
	}
}
