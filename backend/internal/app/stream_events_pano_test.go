// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mustafaturan/monoflake"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
)

// A Pano doc changed by any workspace reaches its owner's open board, and
// only its owner's, naming the doc and what was done to it.
func TestPublishPanoChange(t *testing.T) {
	const owner, other, doc, workspace = int64(7), int64(8), int64(4242), int64(42)
	for _, tc := range []struct {
		name    string
		ev      entity.CRUDEvent
		handled bool
		action  string
	}{
		{"a doc created by an agent is announced", entity.CRUDEvent{Action: entity.ActionPanoCreate, ResourceType: entity.ResourcePano, WorkspaceID: workspace}, true, "pano_create"},
		{"a doc commented on by the person is announced", entity.CRUDEvent{Action: entity.ActionPanoComment, ResourceType: entity.ResourcePano}, true, "pano_comment"},
		{"a task event is not a Pano event", entity.CRUDEvent{Action: entity.ActionTaskCreate, ResourceType: entity.ResourceTask, WorkspaceID: workspace}, false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bus := eventbus.New()
			mine := bus.Subscribe(0, monoflake.ID(owner).String())
			theirs := bus.Subscribe(0, monoflake.ID(other).String())
			tc.ev.UserID, tc.ev.ResourceID = owner, doc

			if got := publishPanoChange(bus, tc.ev); got != tc.handled {
				t.Fatalf("publishPanoChange reported handled = %v, want %v", got, tc.handled)
			}
			select {
			case line := <-theirs:
				t.Fatalf("another account's stream got %s, want nothing", line)
			default:
			}
			select {
			case line := <-mine:
				if !tc.handled {
					t.Fatalf("the owner's stream got %s, want nothing", line)
				}
				var got struct {
					Type    string            `json:"type"`
					Payload map[string]string `json:"payload"`
				}
				if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(string(line), "data: "))), &got); err != nil {
					t.Fatal(err)
				}
				if got.Type != "pano.changed" || got.Payload["id"] != monoflake.ID(doc).String() || got.Payload["action"] != tc.action {
					t.Fatalf("the owner's stream got %+v, want pano.changed for doc %s and action %s", got, monoflake.ID(doc).String(), tc.action)
				}
			default:
				if tc.handled {
					t.Fatal("the owner's stream got nothing, want pano.changed")
				}
			}
		})
	}
}

// forwardCRUDEvent hands a Pano event to the owner's stream without looking
// up its workspace, which the person's (workspace 0) does not have.
func TestForwardCRUDEvent_PanoNeedsNoWorkspace(t *testing.T) {
	const owner = int64(7)
	bus := eventbus.New()
	ch := bus.Subscribe(0, monoflake.ID(owner).String())
	// A nil repository: a lookup would panic.
	forwardCRUDEvent(context.Background(), nil, bus, entity.CRUDEvent{Action: entity.ActionPanoDelete, ResourceType: entity.ResourcePano, UserID: owner, ResourceID: 9})
	select {
	case line := <-ch:
		if !strings.Contains(string(line), `"pano.changed"`) {
			t.Fatalf("the owner's stream got %s, want pano.changed", line)
		}
	default:
		t.Fatal("the owner's stream got nothing, want pano.changed")
	}
}
