// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/mustafaturan/monoflake"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/service/a2ui"
	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
	"github.com/agentrq/agentrq/backend/internal/service/storage"
)

// asResource is ui as A2UI's MCP binding carries it: an embedded resource
// whose text is the array of messages.
func asResource(ui string) string {
	b, _ := json.Marshal(map[string]any{
		"type":     "resource",
		"resource": map[string]any{"uri": "a2ui://pr-1", "mimeType": "application/a2ui+json", "text": ui},
	})
	return string(b)
}

func actions(events []entity.CRUDEvent) []entity.Action {
	var got []entity.Action
	for _, ev := range events {
		got = append(got, ev.Action)
	}
	return got
}

func TestHandleReply_ACardMayComeAsAnEmbeddedResource(t *testing.T) {
	asArray := newA2UIReplyEnv(t)
	if res, _, _ := asArray.ps.handleReply(context.Background(), nil, a2uiReplyParams(a2uiTestCard)); res.IsError {
		t.Fatalf("the array: %s", toolText(res))
	}
	blob, _ := json.Marshal(map[string]any{
		"type":     "resource",
		"resource": map[string]any{"uri": "a2ui://pr-1", "mimeType": "application/a2ui+json", "blob": base64.StdEncoding.EncodeToString([]byte(a2uiTestCard))},
	})
	for name, ui := range map[string]string{"text": " \n" + asResource(a2uiTestCard), "blob": string(blob)} {
		t.Run(name, func(t *testing.T) {
			e := newA2UIReplyEnv(t)
			res, _, err := e.ps.handleReply(context.Background(), nil, a2uiReplyParams(ui))
			if err != nil || res.IsError {
				t.Fatalf("reply = %v, %v", res, err)
			}
			if !reflect.DeepEqual(e.metadata, asArray.metadata) {
				t.Errorf("stored %#v, want what the array stores: %#v", e.metadata, asArray.metadata)
			}
			if want := []entity.Action{entity.ActionA2UIReply, entity.ActionA2UIResource}; !reflect.DeepEqual(actions(e.events), want) {
				t.Errorf("events = %v, want %v", actions(e.events), want)
			}
			if e.events[1].ResourceID != 555 {
				t.Errorf("a2ui_resource counted message %d, want 555", e.events[1].ResourceID)
			}
		})
	}
	if got := actions(asArray.events); !reflect.DeepEqual(got, []entity.Action{entity.ActionA2UIReply}) {
		t.Errorf("an array counted %v, want only a2ui_reply", got)
	}
}

func TestHandleReply_AnUpdateMayComeAsAnEmbeddedResource(t *testing.T) {
	e := newA2UIReplyEnv(t)
	th := e.withThread(postedCard(t, e, 555))

	res, _, err := e.ps.handleReply(context.Background(), nil, updateParams("PR merged", asResource(retitle)))
	if err != nil || res.IsError {
		t.Fatalf("update = %v, %v", res, err)
	}
	if !strings.Contains(string(th.message(555).Metadata), `"text":"PR merged"`) {
		t.Errorf("the card was not updated: %s", th.message(555).Metadata)
	}
	if want := []entity.Action{entity.ActionA2UIUpdate, entity.ActionA2UIResource}; !reflect.DeepEqual(actions(e.events), want) {
		t.Errorf("events = %v, want %v", actions(e.events), want)
	}
}

func TestHandleReply_AnObjectThatIsNoA2UIResourceIsRefused(t *testing.T) {
	for name, tc := range map[string]struct{ ui, want string }{
		"another mime type": {
			ui:   strings.Replace(asResource(a2uiTestCard), "application/a2ui+json", "application/json", 1),
			want: `ui is a JSON array of A2UI messages, or an embedded resource {"type": "resource", "resource": {"mimeType": "application/a2ui+json", "text": "<that array>"}}`,
		},
		"a bare message": {
			ui:   `{"version":"1.0","deleteSurface":{"surfaceId":"pr-1"}}`,
			want: "ui is a JSON array of A2UI messages, or an embedded resource",
		},
		"not JSON": {ui: `{"type":`, want: "ui is a JSON array of A2UI messages"},
		"no text": {
			ui:   `{"type":"resource","resource":{"uri":"a2ui://x","mimeType":"application/a2ui+json"}}`,
			want: "ui's resource has no text: its text is the JSON array of A2UI messages",
		},
	} {
		t.Run(name, func(t *testing.T) {
			e := newA2UIReplyEnv(t)
			res, _, _ := e.ps.handleReply(context.Background(), nil, a2uiReplyParams(tc.ui))
			if !res.IsError || !strings.HasPrefix(toolText(res), tc.want) {
				t.Fatalf("answer = %q, want a refusal starting %q", toolText(res), tc.want)
			}
			if e.replies != 0 || len(e.events) != 0 {
				t.Errorf("a refused card was posted (%d) or counted (%v)", e.replies, e.events)
			}
			if _, err := storage.LoadAttachment(e.store, 100, 42, monoflake.ID(900).String()); err == nil {
				t.Error("the refused reply's file was stored")
			}
		})
	}
}

// An agent framework learns on initialize that this server draws version 1.0
// of the basic catalog, in the shape A2UI's MCP binding gives it.
func TestInitialize_AdvertisesA2UI(t *testing.T) {
	ps := NewWorkspaceServer(
		100, 0, "user", "http://localhost",
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		eventbus.New(), nil, nil, "icon", "name", "desc", nil, nil, nil, nil,
	)
	cs := connectTestServer(t, ps.MCPServer())
	b, _ := json.Marshal(cs.InitializeResult().Capabilities.A2UI)
	want := `{"rendererCapabilities":{"1.0":{"supportedCatalogIds":["` + a2ui.BasicCatalogID + `","` + a2ui.AgentRQCatalogID + `"]}}}`
	if string(b) != want {
		t.Errorf("capabilities.a2ui = %s, want %s", b, want)
	}
}
