// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"context"
	"strings"
	"testing"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/mustafaturan/monoflake"
)

// An untitled task reaches the agent by ID and body alone: the placeholder
// title is not something anyone wrote, so it is never sent.
func TestTaskIDAndTitle(t *testing.T) {
	id := monoflake.ID(42).String()
	if got := taskIDAndTitle(model.Task{ID: 42, Title: "Fix the flake"}); got != "ID: "+id+"\nTitle: Fix the flake" {
		t.Errorf("titled task: got %q", got)
	}
	if got := taskIDAndTitle(model.Task{ID: 42, Title: entity.UntitledTaskTitle}); got != "ID: "+id {
		t.Errorf("untitled task: got %q, want the ID line only", got)
	}
}

func TestStatusCheckMessage(t *testing.T) {
	id := monoflake.ID(42).String()
	want := "Status Check: You are currently working on task " + id + ". Please provide a brief status update for the mission: Fix the flake"
	if got := statusCheckMessage(model.Task{ID: 42, Title: "Fix the flake"}); got != want {
		t.Errorf("titled task: got %q, want %q", got, want)
	}
	want = "Status Check: You are currently working on task " + id + ". Please provide a brief status update."
	if got := statusCheckMessage(model.Task{ID: 42, Title: entity.UntitledTaskTitle}); got != want {
		t.Errorf("untitled task: got %q, want %q", got, want)
	}
}

func TestHandleGetTask_LeavesOutAnUntitledTasksTitle(t *testing.T) {
	ps := pushServer(t)
	ps.getNextTask = func(context.Context) (model.Task, error) {
		return model.Task{ID: 42, Title: entity.UntitledTaskTitle, Body: "Do the thing"}, nil
	}
	ps.getTask = func(context.Context, int64) (model.Task, error) {
		return model.Task{ID: 42, Title: entity.UntitledTaskTitle, Body: "Do the thing", Status: "ongoing"}, nil
	}

	for _, params := range []GetTaskParams{{}, {TaskID: monoflake.ID(42).String()}} {
		res, _, err := ps.handleGetTask(context.Background(), nil, params)
		if err != nil || res.IsError {
			t.Fatalf("getTask(%+v) failed: %v %+v", params, err, res)
		}
		text := res.Content[0].(*mcp.TextContent).Text
		if strings.Contains(text, "Title:") || strings.Contains(text, entity.UntitledTaskTitle) {
			t.Errorf("getTask(%+v) sent the placeholder title: %s", params, text)
		}
		if !strings.Contains(text, "ID: "+monoflake.ID(42).String()+"\n") || !strings.Contains(text, "Details: Do the thing") {
			t.Errorf("getTask(%+v) lost the ID or body: %s", params, text)
		}
	}
}
