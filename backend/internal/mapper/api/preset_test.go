// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"net/http"
	"testing"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	view "github.com/agentrq/agentrq/backend/internal/data/view/api"
)

func TestPresetRequests(t *testing.T) {
	ws := "/workspaces/" + b62(42) + "/presets"
	checkRequest(t, "/workspaces/:id/presets", FromHTTPRequestToListPresetsRequestEntity, []requestCase[entity.ListPresetsRequest]{
		{name: "a workspace's presets", target: ws, want: &entity.ListPresetsRequest{WorkspaceID: 42}},
		{name: "no workspace id", target: "/workspaces/0/presets"},
	})
	checkRequest(t, "/workspaces/:id/presets", FromHTTPRequestToCreatePresetRequestEntity, []requestCase[entity.CreatePresetRequest]{
		{name: "a preset to save", target: ws, body: `{"name":"pull-n-push","body":"pull latest main, tag and push"}`,
			want: &entity.CreatePresetRequest{WorkspaceID: 42, Name: "pull-n-push", Body: "pull latest main, tag and push"}},
		{name: "no workspace id", target: "/workspaces/0/presets", body: `{}`},
		{name: "a body that is not JSON", target: ws, body: `{`},
	})
	checkRequest(t, "/workspaces/:id/presets/:name", FromHTTPRequestToDeletePresetRequestEntity, []requestCase[entity.DeletePresetRequest]{
		{name: "a preset by name", target: ws + "/pull-n-push", want: &entity.DeletePresetRequest{WorkspaceID: 42, Name: "pull-n-push"}},
		{name: "an escaped name", target: ws + "/a%20b", want: &entity.DeletePresetRequest{WorkspaceID: 42, Name: "a b"}},
		{name: "no workspace id", target: "/workspaces/0/presets/a"},
		{name: "a broken escape in the name", target: ws + "/a%zz"},
	})
}

func TestPresetResponses(t *testing.T) {
	p := entity.Preset{ID: 900, CreatedAt: at, WorkspaceID: 42, Name: "pull-n-push", Body: "pull latest main, tag and push"}
	want := view.Preset{ID: b62(900), CreatedAt: at, WorkspaceID: b62(42), Name: "pull-n-push", Body: "pull latest main, tag and push"}
	checkResponse(t, FromListPresetsResponseEntityToHTTPResponse(&entity.ListPresetsResponse{Presets: []entity.Preset{p}}),
		view.ListPresetsResponse{Presets: []view.Preset{want}})
	checkResponse(t, FromListPresetsResponseEntityToHTTPResponse(&entity.ListPresetsResponse{}),
		view.ListPresetsResponse{Presets: []view.Preset{}})
	checkResponse(t, FromCreatePresetResponseEntityToHTTPResponse(&entity.CreatePresetResponse{Preset: p}),
		view.CreatePresetResponse{Preset: want})
}

func TestFromErrorToHTTPResponse_ARefusedPreset(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want int
	}{
		{"a name taken", &entity.PresetError{Message: "taken", Conflict: true}, http.StatusConflict},
		{"anything else refused", &entity.PresetError{Message: "taken"}, http.StatusUnprocessableEntity},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, code := FromErrorToHTTPResponse(tc.err)
			got := decodeResponse[httpError](t, body)
			if code != tc.want || got.Error.Code != tc.want || got.Error.Message != "taken" {
				t.Errorf("answered %d %s, want %d with \"taken\"", code, body, tc.want)
			}
		})
	}
}
