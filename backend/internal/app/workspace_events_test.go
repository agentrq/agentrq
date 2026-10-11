// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mustafaturan/monoflake"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
)

// A workspace added or removed anywhere — the supervisor's forkWorkspace among
// them — reaches its owner's open web app, and only its owner's.
func TestPublishWorkspaceListChange(t *testing.T) {
	const owner, other, workspace = int64(7), int64(8), int64(42)
	cases := []struct {
		name      string
		ev        entity.CRUDEvent
		handled   bool
		published bool
	}{
		{"create", entity.CRUDEvent{Action: entity.ActionWorkspaceCreate, ResourceType: entity.ResourceWorkspace}, true, true},
		{"delete", entity.CRUDEvent{Action: entity.ActionWorkspaceDelete, ResourceType: entity.ResourceWorkspace}, true, true},
		{"fork merge", entity.CRUDEvent{Action: entity.ActionWorkspaceForkMerge, ResourceType: entity.ResourceWorkspace}, true, true},
		// The fork's create is what announces it; a second event would read the list twice.
		{"fork create", entity.CRUDEvent{Action: entity.ActionWorkspaceForkCreate, ResourceType: entity.ResourceWorkspace}, true, false},
		{"update", entity.CRUDEvent{Action: entity.ActionWorkspaceUpdate, ResourceType: entity.ResourceWorkspace}, true, false},
		{"task", entity.CRUDEvent{Action: entity.ActionTaskCreate, ResourceType: entity.ResourceTask}, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bus := eventbus.New()
			mine := bus.Subscribe(0, monoflake.ID(owner).String())
			theirs := bus.Subscribe(0, monoflake.ID(other).String())
			tc.ev.UserID, tc.ev.WorkspaceID, tc.ev.ResourceID = owner, workspace, workspace

			if got := publishWorkspaceListChange(bus, tc.ev); got != tc.handled {
				t.Fatalf("handled = %v, want %v", got, tc.handled)
			}
			select {
			case line := <-theirs:
				t.Fatalf("another account was told: %s", line)
			default:
			}
			select {
			case line := <-mine:
				if !tc.published {
					t.Fatalf("unexpected event: %s", line)
				}
				var got struct {
					Type    string            `json:"type"`
					Payload map[string]string `json:"payload"`
				}
				if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(string(line), "data: "))), &got); err != nil {
					t.Fatal(err)
				}
				if got.Type != "workspaces.changed" || got.Payload["workspaceId"] != monoflake.ID(workspace).String() {
					t.Fatalf("event = %+v", got)
				}
			default:
				if tc.published {
					t.Fatal("the owner was not told")
				}
			}
		})
	}
}
