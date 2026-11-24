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

func TestFromHTTPRequestToSendA2uiActionRequestEntity(t *testing.T) {
	route := taskRoute + "/a2ui/actions"
	path := taskPath + "/a2ui/actions"
	msg := b62(77)
	checkRequest(t, route, FromHTTPRequestToSendA2uiActionRequestEntity, []requestCase[entity.SendA2uiActionRequest]{
		{"every field", path, `{"messageId":"` + msg + `","surfaceId":"deploy","name":"approve","sourceComponentId":"ok","context":{"env":"prod"},"userMessage":"Ship it"}`,
			&entity.SendA2uiActionRequest{WorkspaceID: 20, TaskID: 101, MessageID: 77, SurfaceID: "deploy", Name: "approve",
				SourceComponentID: "ok", Context: map[string]any{"env": "prod"}, UserMessage: "Ship it"}},
		{"no context or message", path, `{"messageId":"` + msg + `","surfaceId":"deploy","name":"approve","sourceComponentId":"ok"}`,
			&entity.SendA2uiActionRequest{WorkspaceID: 20, TaskID: 101, MessageID: 77, SurfaceID: "deploy", Name: "approve", SourceComponentID: "ok"}},
		{"no message id", path, `{"surfaceId":"deploy","name":"approve","sourceComponentId":"ok"}`, nil},
		{"no surface", path, `{"messageId":"` + msg + `","name":"approve","sourceComponentId":"ok"}`, nil},
		{"no name", path, `{"messageId":"` + msg + `","surfaceId":"deploy","sourceComponentId":"ok"}`, nil},
		{"no component", path, `{"messageId":"` + msg + `","surfaceId":"deploy","name":"approve"}`, nil},
		{"workspace 0", noWsPath + "/a2ui/actions", `{"messageId":"` + msg + `","surfaceId":"deploy","name":"approve","sourceComponentId":"ok"}`, nil},
		{"task 0", noTaskPath + "/a2ui/actions", `{"messageId":"` + msg + `","surfaceId":"deploy","name":"approve","sourceComponentId":"ok"}`, nil},
		{"not JSON", path, `{`, nil},
	})
}

func TestFromSendA2uiActionResponseEntityToHTTPResponse(t *testing.T) {
	task := entity.Task{ID: 101, WorkspaceID: 20, Title: "Deploy"}
	got := decodeResponse[view.A2uiActionResponse](t, FromSendA2uiActionResponseEntityToHTTPResponse(&entity.SendA2uiActionResponse{Task: task, MessageID: 9, Text: "Ship it"}))
	if want := FromEntityTaskToView(task); got.Task.ID != want.ID || got.Task.Title != "Deploy" {
		t.Errorf("task = %+v, want %+v", got.Task, want)
	}
}

func TestFromErrorToHTTPResponse_ARefusedA2uiAction(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want int
	}{
		{"already answered", &entity.A2uiActionError{Message: "no", Conflict: true}, http.StatusConflict},
		{"not on the card", &entity.A2uiActionError{Message: "no"}, http.StatusUnprocessableEntity},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, code := FromErrorToHTTPResponse(tc.err)
			got := decodeResponse[httpError](t, body)
			if code != tc.want || got.Error.Code != tc.want || got.Error.Message != "no" {
				t.Errorf("answered %d %s, want %d with \"no\"", code, body, tc.want)
			}
		})
	}
}
