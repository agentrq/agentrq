// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"reflect"
	"testing"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/gofiber/fiber/v2"
)

func TestFromHTTPRequestToSavePushSubscriptionRequestEntity(t *testing.T) {
	keys := `"keys":{"p256dh":"pk","auth":"au"}`
	for _, tc := range []struct {
		name, body string
		want       *entity.SavePushSubscriptionRequest
	}{
		{"a workspace's chosen types", `{"endpoint":"https://push/1",` + keys + `,"workspaceId":"` + b62(20) + `","types":["task.created"]}`,
			&entity.SavePushSubscriptionRequest{Endpoint: "https://push/1", P256dh: "pk", Auth: "au", WorkspaceID: 20, UserAgent: "Firefox/140", Types: []string{"task.created"}}},
		// No workspace is every workspace: the id is 0, not a refusal.
		{"every workspace, every type", `{"endpoint":"https://push/1",` + keys + `}`,
			&entity.SavePushSubscriptionRequest{Endpoint: "https://push/1", P256dh: "pk", Auth: "au", UserAgent: "Firefox/140"}},
		{"no endpoint", `{` + keys + `}`, nil},
		{"no p256dh", `{"endpoint":"e","keys":{"auth":"au"}}`, nil},
		{"no auth", `{"endpoint":"e","keys":{"p256dh":"pk"}}`, nil},
		{"not JSON", `{`, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := jsonRequest("/push/subscribe", tc.body)
			req.Header.Set(fiber.HeaderUserAgent, "Firefox/140")
			got := mapRequest(t, "/push/subscribe", req, FromHTTPRequestToSavePushSubscriptionRequestEntity)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestFromHTTPRequestToDeletePushSubscriptionRequestEntity(t *testing.T) {
	checkRequest(t, "/push/unsubscribe", FromHTTPRequestToDeletePushSubscriptionRequestEntity, []requestCase[entity.DeletePushSubscriptionRequest]{
		{"an endpoint", "/push/unsubscribe", `{"endpoint":"https://push/1"}`, &entity.DeletePushSubscriptionRequest{Endpoint: "https://push/1"}},
		{"no endpoint", "/push/unsubscribe", `{}`, nil},
		{"not JSON", "/push/unsubscribe", `{`, nil},
	})
}

func TestFromHTTPRequestToDeletePushSubscriptionByWorkspaceRequestEntity(t *testing.T) {
	checkRequest(t, "/push/unsubscribe-workspace", FromHTTPRequestToDeletePushSubscriptionByWorkspaceRequestEntity, []requestCase[entity.DeletePushSubscriptionByWorkspaceRequest]{
		{"an endpoint in a workspace", "/push/unsubscribe-workspace", `{"endpoint":"https://push/1","workspaceId":"` + b62(20) + `"}`,
			&entity.DeletePushSubscriptionByWorkspaceRequest{Endpoint: "https://push/1", WorkspaceID: 20}},
		{"no endpoint", "/push/unsubscribe-workspace", `{"workspaceId":"` + b62(20) + `"}`, nil},
		{"no workspace", "/push/unsubscribe-workspace", `{"endpoint":"https://push/1"}`, nil},
		{"workspace 0", "/push/unsubscribe-workspace", `{"endpoint":"https://push/1","workspaceId":"0"}`, nil},
		{"not JSON", "/push/unsubscribe-workspace", `{`, nil},
	})
}

func TestPushResponses(t *testing.T) {
	for _, tc := range []struct {
		got  []byte
		want string
	}{
		{FromVAPIDPublicKeyToHTTPResponse("BPk"), `{"publicKey":"BPk"}`},
		{FromPushSubscriptionStatusToHTTPResponse(true), `{"subscribed":true}`},
		{FromPushSubscriptionStatusToHTTPResponse(false), `{"subscribed":false}`},
	} {
		if string(tc.got) != tc.want {
			t.Errorf("got %s, want %s", tc.got, tc.want)
		}
	}
}
