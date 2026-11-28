// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package api

import (
	"net/http"
	"testing"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	view "github.com/agentrq/agentrq/backend/internal/data/view/api"
)

func TestAccountSettingsRequests(t *testing.T) {
	checkRequest(t, "/account/settings", FromHTTPRequestToUpdateAccountSettingsRequestEntity, []requestCase[entity.UpdateAccountSettingsRequest]{
		{name: "a clock to save", target: "/account/settings", body: `{"display":{"timeFormat":"12h"}}`,
			want: &entity.UpdateAccountSettingsRequest{Settings: entity.AccountSettings{entity.SettingsDisplay: {entity.SettingTimeFormat: "12h"}}}},
		{name: "a group with no field to save", target: "/account/settings", body: `{"display":{}}`,
			want: &entity.UpdateAccountSettingsRequest{Settings: entity.AccountSettings{entity.SettingsDisplay: {}}}},
		{name: "a group left out is not saved", target: "/account/settings", body: `{}`,
			want: &entity.UpdateAccountSettingsRequest{Settings: entity.AccountSettings{}}},
		{name: "a body that is not JSON", target: "/account/settings", body: `{`},
	})
}

func TestAccountSettingsResponse(t *testing.T) {
	checkResponse(t, FromAccountSettingsResponseEntityToHTTPResponse(&entity.AccountSettingsResponse{Settings: entity.AccountSettings{entity.SettingsDisplay: {entity.SettingTimeFormat: "browser"}}}),
		view.AccountSettingsResponse{Settings: view.AccountSettings{Display: view.DisplaySettings{TimeFormat: "browser"}}})
}

func TestAccountPresetRequests_HaveNoWorkspace(t *testing.T) {
	checkRequest(t, "/account/presets", FromHTTPRequestToListPresetsRequestEntity, []requestCase[entity.ListPresetsRequest]{
		{name: "the account's presets", target: "/account/presets", want: &entity.ListPresetsRequest{}},
	})
	checkRequest(t, "/account/presets", FromHTTPRequestToCreatePresetRequestEntity, []requestCase[entity.CreatePresetRequest]{
		{name: "an account preset to save", target: "/account/presets", body: `{"name":"standup","body":"post it"}`,
			want: &entity.CreatePresetRequest{Name: "standup", Body: "post it"}},
	})
	checkRequest(t, "/account/presets/:name", FromHTTPRequestToUpdatePresetRequestEntity, []requestCase[entity.UpdatePresetRequest]{
		{name: "an account preset edited", target: "/account/presets/standup", body: `{"body":"post it"}`,
			want: &entity.UpdatePresetRequest{Name: "standup", Body: "post it"}},
	})
	checkRequest(t, "/account/presets/:name", FromHTTPRequestToDeletePresetRequestEntity, []requestCase[entity.DeletePresetRequest]{
		{name: "an account preset by name", target: "/account/presets/standup", want: &entity.DeletePresetRequest{Name: "standup"}},
	})
}

func TestFromErrorToHTTPResponse_ARefusedAccountSetting(t *testing.T) {
	body, code := FromErrorToHTTPResponse(&entity.AccountSettingsError{Message: "refused"})
	got := decodeResponse[httpError](t, body)
	if code != http.StatusUnprocessableEntity || got.Error.Code != http.StatusUnprocessableEntity || got.Error.Message != "refused" {
		t.Errorf("answered %d %s, want 422 with \"refused\"", code, body)
	}
}
