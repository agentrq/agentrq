// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/mustafaturan/monoflake"
	"gorm.io/gorm"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
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
		{"id reset", entity.CRUDEvent{Action: entity.ActionWorkspaceResetID, ResourceType: entity.ResourceWorkspace}, true, true},
		// The fork's create is what announces it; a second event would read the list twice.
		{"fork create", entity.CRUDEvent{Action: entity.ActionWorkspaceForkCreate, ResourceType: entity.ResourceWorkspace}, true, false},
		{"update", entity.CRUDEvent{Action: entity.ActionWorkspaceUpdate, ResourceType: entity.ResourceWorkspace}, true, true},
		{"site share", entity.CRUDEvent{Action: entity.ActionSiteShare, ResourceType: entity.ResourceWorkspace}, true, false},
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

// A task deleted by the supervisor's MCP leaves the open pages too: the event
// names the task, on its workspace's stream and its owner's.
func TestPublishTaskDelete(t *testing.T) {
	const workspace, task = int64(42), int64(9)
	owner := monoflake.ID(7).String()
	for _, tc := range []struct {
		name string
		ev   entity.CRUDEvent
		want bool
	}{
		{"delete", entity.CRUDEvent{Action: entity.ActionTaskDelete, ResourceType: entity.ResourceTask}, true},
		{"update", entity.CRUDEvent{Action: entity.ActionTaskUpdate, ResourceType: entity.ResourceTask}, false},
		{"workspace delete", entity.CRUDEvent{Action: entity.ActionTaskDelete, ResourceType: entity.ResourceWorkspace}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bus := eventbus.New()
			page := bus.Subscribe(workspace, "")
			global := bus.Subscribe(0, owner)
			tc.ev.WorkspaceID, tc.ev.ResourceID = workspace, task

			if got := publishTaskDelete(bus, tc.ev, owner); got != tc.want {
				t.Fatalf("handled = %v, want %v", got, tc.want)
			}
			for stream, ch := range map[string]chan []byte{"workspace": page, "owner": global} {
				select {
				case line := <-ch:
					if !tc.want {
						t.Fatalf("%s stream got %s", stream, line)
					}
					if want := `{"type":"task.deleted","payload":{"id":"` + monoflake.ID(task).String() + `"}}`; !strings.Contains(string(line), want) {
						t.Fatalf("%s stream got %s, want %s", stream, line, want)
					}
				default:
					if tc.want {
						t.Fatalf("the %s stream was not told", stream)
					}
				}
			}
		})
	}
}

// Every CRUD event the web app reacts to reaches the owner's stream as the
// event it listens for, and nothing reaches it for what it cannot show.
func TestForwardCRUDEvent(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Workspace{}, &model.Task{}, &model.Message{}, &model.ToolCall{}); err != nil {
		t.Fatal(err)
	}
	const task, message = int64(101), int64(201)
	for _, row := range []any{
		&model.Workspace{ID: memParent, UserID: memUser, Name: "parent"},
		&model.Task{ID: task, WorkspaceID: memParent, UserID: memUser, Title: "a task", Status: "ongoing"},
		&model.Message{ID: message, TaskID: task, UserID: memUser, Sender: "agent", Text: "done"},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	repo := base.New(sqliteConn{db: db})

	ev := func(r entity.ResourceType, a entity.Action, id int64) entity.CRUDEvent {
		return entity.CRUDEvent{WorkspaceID: memParent, UserID: memUser, ResourceType: r, Action: a, ResourceID: id}
	}
	for _, tc := range []struct {
		name string
		ev   entity.CRUDEvent
		want string // the event type, or "" for none
	}{
		{"workspace created", ev(entity.ResourceWorkspace, entity.ActionWorkspaceCreate, memParent), "workspaces.changed"},
		{"task created", ev(entity.ResourceTask, entity.ActionTaskCreate, task), "task.created"},
		{"task completed", ev(entity.ResourceTask, entity.ActionTaskComplete, task), "status.updated"},
		{"task updated", ev(entity.ResourceTask, entity.ActionTaskUpdate, task), "task.updated"},
		{"task deleted", ev(entity.ResourceTask, entity.ActionTaskDelete, task), "task.deleted"},
		{"reply", ev(entity.ResourceMessage, entity.ActionMessageCreate, message), "reply.received"},
		{"reply that is gone", ev(entity.ResourceMessage, entity.ActionMessageCreate, 999), ""},
		{"task that is gone", ev(entity.ResourceTask, entity.ActionTaskUpdate, 999), ""},
		{"workspace that is gone", entity.CRUDEvent{WorkspaceID: 999, ResourceType: entity.ResourceTask, Action: entity.ActionTaskUpdate, ResourceID: task}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bus := eventbus.New()
			ch := bus.Subscribe(0, monoflake.ID(memUser).String())

			forwardCRUDEvent(context.Background(), repo, bus, tc.ev)

			select {
			case line := <-ch:
				var got struct {
					Type string `json:"type"`
				}
				if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(string(line), "data: "))), &got); err != nil {
					t.Fatal(err)
				}
				if got.Type != tc.want {
					t.Fatalf("event = %q, want %q", got.Type, tc.want)
				}
			default:
				if tc.want != "" {
					t.Fatalf("no event, want %q", tc.want)
				}
			}
		})
	}
}
