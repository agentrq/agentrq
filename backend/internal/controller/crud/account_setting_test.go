// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package crud

import (
	"context"
	"errors"
	"testing"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/golang/mock/gomock"
)

const displayGroup = entity.SettingsDisplay

func displayRow(value string) model.AccountSetting {
	return model.AccountSetting{UserID: testUserID, Name: displayGroup, Value: value}
}

func timeFormatOf(t *testing.T, rs *entity.AccountSettingsResponse) string {
	t.Helper()
	if len(rs.Settings) != 1 || len(rs.Settings[displayGroup]) != 1 {
		t.Fatalf("the settings are %v, want only display_settings with only timeFormat", rs.Settings)
	}
	return rs.Settings[displayGroup][entity.SettingTimeFormat]
}

func TestGetAccountSettings_AnAccountThatNeverChoseFollowsTheBrowser(t *testing.T) {
	e := newMachineTelemetryEnv(t)
	e.repo.EXPECT().ListAccountSettings(gomock.Any(), testUserID).Return([]model.AccountSetting{}, nil)
	rs, err := e.controller.GetAccountSettings(context.Background(), entity.GetAccountSettingsRequest{UserID: testUserIDStr})
	if err != nil {
		t.Fatalf("GetAccountSettings returned %v", err)
	}
	if got := timeFormatOf(t, rs); got != entity.TimeFormatBrowser {
		t.Errorf("an account with no rows read timeFormat %q, want its default, browser", got)
	}
}

func TestGetAccountSettings_ReadsTheSavedGroupAndDropsWhatItDoesNotKnow(t *testing.T) {
	e := newMachineTelemetryEnv(t)
	e.repo.EXPECT().ListAccountSettings(gomock.Any(), testUserID).Return([]model.AccountSetting{
		displayRow(`{"timeFormat":"12h","retiredField":"on"}`),
		{UserID: testUserID, Name: "retired_settings", Value: `{"a":"b"}`},
	}, nil)
	rs, err := e.controller.GetAccountSettings(context.Background(), entity.GetAccountSettingsRequest{UserID: testUserIDStr})
	if err != nil {
		t.Fatalf("GetAccountSettings returned %v", err)
	}
	if got := timeFormatOf(t, rs); got != "12h" {
		t.Errorf("read timeFormat %q, want the saved 12h", got)
	}
}

func TestGetAccountSettings_AValueItCannotUseReadsAsTheDefault(t *testing.T) {
	for _, tc := range []struct{ name, stored string }{
		{"a value the field may no longer hold", `{"timeFormat":"13h"}`},
		{"a group that is not a JSON object of strings", `{"timeFormat":12}`},
		{"a group that is not JSON", `not json`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newMachineTelemetryEnv(t)
			e.repo.EXPECT().ListAccountSettings(gomock.Any(), testUserID).Return([]model.AccountSetting{displayRow(tc.stored)}, nil)
			rs, err := e.controller.GetAccountSettings(context.Background(), entity.GetAccountSettingsRequest{UserID: testUserIDStr})
			if err != nil {
				t.Fatalf("GetAccountSettings returned %v", err)
			}
			if got := timeFormatOf(t, rs); got != entity.TimeFormatBrowser {
				t.Errorf("read timeFormat %q from %s, want the default, browser", got, tc.stored)
			}
		})
	}
}

func TestGetAccountSettings_ReportsFailures(t *testing.T) {
	t.Run("no account", func(t *testing.T) {
		e := newMachineTelemetryEnv(t)
		if _, err := e.controller.GetAccountSettings(context.Background(), entity.GetAccountSettingsRequest{}); err == nil || err.Error() != "invalid request" {
			t.Fatalf("returned %v, want \"invalid request\"", err)
		}
	})
	t.Run("a database failure comes back", func(t *testing.T) {
		e := newMachineTelemetryEnv(t)
		e.repo.EXPECT().ListAccountSettings(gomock.Any(), testUserID).Return(nil, errors.New("database unavailable"))
		if _, err := e.controller.GetAccountSettings(context.Background(), entity.GetAccountSettingsRequest{UserID: testUserIDStr}); err == nil || err.Error() != "database unavailable" {
			t.Fatalf("returned %v, want \"database unavailable\"", err)
		}
	})
}

func displayUpdate(timeFormat string) entity.UpdateAccountSettingsRequest {
	return entity.UpdateAccountSettingsRequest{UserID: testUserIDStr, Settings: entity.AccountSettings{
		displayGroup: {entity.SettingTimeFormat: timeFormat},
	}}
}

func TestUpdateAccountSettings_StoresEachClockAsTheGroupsJSON(t *testing.T) {
	for _, format := range []string{entity.TimeFormat12h, entity.TimeFormat24h, entity.TimeFormatBrowser} {
		t.Run(format, func(t *testing.T) {
			e := newMachineTelemetryEnv(t)
			e.repo.EXPECT().ListAccountSettings(gomock.Any(), testUserID).Return(nil, nil)
			e.repo.EXPECT().SaveAccountSetting(gomock.Any(), displayRow(`{"timeFormat":"`+format+`"}`)).Return(nil)
			rs, err := e.controller.UpdateAccountSettings(context.Background(), displayUpdate(format))
			if err != nil {
				t.Fatalf("UpdateAccountSettings returned %v", err)
			}
			if got := timeFormatOf(t, rs); got != format {
				t.Errorf("saving %q answered %q, want it back as sent", format, got)
			}
			ev := e.only(t)
			if ev.Action != entity.ActionAccountSettingsUpdate || ev.UserID != testUserID || ev.WorkspaceID != 0 || ev.Actor != entity.ActorHuman || ev.ResourceType != entity.ResourceUser {
				t.Errorf("published %+v, want an account_settings_update by the human, with no workspace", ev)
			}
		})
	}
}

func TestUpdateAccountSettings_KeepsTheGroupsOtherFields(t *testing.T) {
	e := newMachineTelemetryEnv(t)
	e.repo.EXPECT().ListAccountSettings(gomock.Any(), testUserID).Return([]model.AccountSetting{
		displayRow(`{"timeFormat":"12h","laterField":"kept"}`),
	}, nil)
	e.repo.EXPECT().SaveAccountSetting(gomock.Any(), displayRow(`{"laterField":"kept","timeFormat":"24h"}`)).Return(nil)
	rs, err := e.controller.UpdateAccountSettings(context.Background(), displayUpdate(entity.TimeFormat24h))
	if err != nil || timeFormatOf(t, rs) != "24h" {
		t.Fatalf("UpdateAccountSettings returned %+v, %v; want 24h", rs, err)
	}
}

func TestUpdateAccountSettings_NothingToSaveSavesNothingAndCountsNothing(t *testing.T) {
	for _, tc := range []struct {
		name     string
		settings entity.AccountSettings
	}{
		{"no groups", nil},
		{"a group with no fields", entity.AccountSettings{displayGroup: {}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newMachineTelemetryEnv(t)
			e.repo.EXPECT().ListAccountSettings(gomock.Any(), testUserID).Return([]model.AccountSetting{displayRow(`{"timeFormat":"12h"}`)}, nil)
			rs, err := e.controller.UpdateAccountSettings(context.Background(), entity.UpdateAccountSettingsRequest{UserID: testUserIDStr, Settings: tc.settings})
			if err != nil || timeFormatOf(t, rs) != "12h" {
				t.Fatalf("an empty save returned %+v, %v; want the settings as they are", rs, err)
			}
			if len(*e.events) != 0 {
				t.Errorf("an empty save published %+v, want nothing counted", *e.events)
			}
		})
	}
}

func TestUpdateAccountSettings_RefusesWhatItCannotKeep(t *testing.T) {
	for _, tc := range []struct {
		name     string
		settings entity.AccountSettings
		want     string
	}{
		{"a clock it cannot show", entity.AccountSettings{displayGroup: {entity.SettingTimeFormat: "13h"}}, `timeFormat "13h" is not one of "browser", "12h" or "24h"`},
		{"a field the group does not have", entity.AccountSettings{displayGroup: {"theme": "dark"}}, `"theme" is not one of the display_settings`},
		{"a group it does not have", entity.AccountSettings{"chat_settings": {}, displayGroup: {entity.SettingTimeFormat: "12h"}}, `"chat_settings" is not a group of account settings`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newMachineTelemetryEnv(t)
			_, err := e.controller.UpdateAccountSettings(context.Background(), entity.UpdateAccountSettingsRequest{UserID: testUserIDStr, Settings: tc.settings})
			var se *entity.AccountSettingsError
			if !errors.As(err, &se) {
				t.Fatalf("returned %v, want an AccountSettingsError", err)
			}
			if se.Error() != se.Message || se.Message != tc.want {
				t.Errorf("refused with %q, want %q", se.Message, tc.want)
			}
			if len(*e.events) != 0 {
				t.Errorf("a refused save published %+v, want nothing counted", *e.events)
			}
		})
	}
}

func TestUpdateAccountSettings_ReportsFailures(t *testing.T) {
	t.Run("no account", func(t *testing.T) {
		e := newMachineTelemetryEnv(t)
		req := displayUpdate("12h")
		req.UserID = ""
		if _, err := e.controller.UpdateAccountSettings(context.Background(), req); err == nil || err.Error() != "invalid request" {
			t.Fatalf("returned %v, want \"invalid request\"", err)
		}
	})
	t.Run("the stored settings cannot be read", func(t *testing.T) {
		e := newMachineTelemetryEnv(t)
		e.repo.EXPECT().ListAccountSettings(gomock.Any(), testUserID).Return(nil, errors.New("database unavailable"))
		if _, err := e.controller.UpdateAccountSettings(context.Background(), displayUpdate("12h")); err == nil || err.Error() != "database unavailable" {
			t.Fatalf("returned %v, want \"database unavailable\"", err)
		}
	})
	t.Run("the save fails", func(t *testing.T) {
		e := newMachineTelemetryEnv(t)
		e.repo.EXPECT().ListAccountSettings(gomock.Any(), testUserID).Return(nil, nil)
		e.repo.EXPECT().SaveAccountSetting(gomock.Any(), gomock.Any()).Return(errors.New("database unavailable"))
		if _, err := e.controller.UpdateAccountSettings(context.Background(), displayUpdate("12h")); err == nil || err.Error() != "database unavailable" {
			t.Fatalf("returned %v, want \"database unavailable\"", err)
		}
		if len(*e.events) != 0 {
			t.Errorf("a failed save published %+v, want nothing counted", *e.events)
		}
	})
}

func TestOneOf_ListsValuesAsASentence(t *testing.T) {
	for _, tc := range []struct {
		values []string
		want   string
	}{
		{[]string{"a"}, `"a"`},
		{[]string{"a", "b"}, `"a" or "b"`},
		{[]string{"a", "b", "c"}, `"a", "b" or "c"`},
	} {
		if got := oneOf(tc.values); got != tc.want {
			t.Errorf("oneOf(%v) = %s, want %s", tc.values, got, tc.want)
		}
	}
}

func TestAccountPresets_NeedNoWorkspaceAndKeepToWorkspaceZero(t *testing.T) {
	ctx := context.Background()
	t.Run("list", func(t *testing.T) {
		e := newMachineTelemetryEnv(t)
		e.repo.EXPECT().ListPresets(gomock.Any(), testUserID, int64(0)).Return([]model.Preset{{UserID: testUserID, Name: "standup", Body: "post the standup"}}, nil)
		rs, err := e.controller.ListPresets(ctx, entity.ListPresetsRequest{UserID: testUserIDStr})
		if err != nil || len(rs.Presets) != 1 || rs.Presets[0].Name != "standup" || rs.Presets[0].WorkspaceID != 0 {
			t.Fatalf("ListPresets returned %+v, %v; want the account's standup preset", rs, err)
		}
	})
	t.Run("create", func(t *testing.T) {
		e := newMachineTelemetryEnv(t)
		e.repo.EXPECT().ListPresets(gomock.Any(), testUserID, int64(0)).Return(nil, nil)
		e.idgen.EXPECT().NextID().Return(int64(900))
		e.repo.EXPECT().CreatePreset(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, p model.Preset) (model.Preset, error) {
			if p.WorkspaceID != 0 || p.UserID != testUserID {
				t.Errorf("stored %+v, want the account's own, workspace 0", p)
			}
			return p, nil
		})
		if _, err := e.controller.CreatePreset(ctx, entity.CreatePresetRequest{UserID: testUserIDStr, Name: "standup", Body: "post the standup"}); err != nil {
			t.Fatalf("CreatePreset returned %v", err)
		}
		if ev := e.only(t); ev.Action != entity.ActionPresetCreate || ev.WorkspaceID != 0 || ev.UserID != testUserID {
			t.Errorf("published %+v, want a preset_create with no workspace", ev)
		}
	})
	t.Run("an account at the cap is told it is the account's", func(t *testing.T) {
		e := newMachineTelemetryEnv(t)
		e.repo.EXPECT().ListPresets(gomock.Any(), testUserID, int64(0)).Return(make([]model.Preset, MaxPresetsPerWorkspace), nil)
		_, err := e.controller.CreatePreset(ctx, entity.CreatePresetRequest{UserID: testUserIDStr, Name: "standup", Body: "x"})
		if pe := presetErrorOf(t, err); pe.Message != "an account keeps up to 100 presets; remove one first" {
			t.Errorf("refused with %q, want the account's cap named", pe.Message)
		}
	})
	t.Run("update", func(t *testing.T) {
		e := newMachineTelemetryEnv(t)
		e.repo.EXPECT().UpdatePreset(gomock.Any(), testUserID, int64(0), "standup", gomock.Any()).Return(model.Preset{UserID: testUserID, Name: "standup", Body: "y"}, nil)
		if _, err := e.controller.UpdatePreset(ctx, entity.UpdatePresetRequest{UserID: testUserIDStr, Name: "standup", Body: "y"}); err != nil {
			t.Fatalf("UpdatePreset returned %v", err)
		}
	})
	t.Run("delete", func(t *testing.T) {
		e := newMachineTelemetryEnv(t)
		e.repo.EXPECT().DeletePreset(gomock.Any(), testUserID, int64(0), "standup").Return(nil)
		if err := e.controller.DeletePreset(ctx, entity.DeletePresetRequest{UserID: testUserIDStr, Name: "standup"}); err != nil {
			t.Fatalf("DeletePreset returned %v", err)
		}
	})
	t.Run("no account is refused", func(t *testing.T) {
		e := newMachineTelemetryEnv(t)
		if _, err := e.controller.ListPresets(ctx, entity.ListPresetsRequest{}); err == nil || err.Error() != "invalid request" {
			t.Fatalf("returned %v, want \"invalid request\"", err)
		}
	})
}
