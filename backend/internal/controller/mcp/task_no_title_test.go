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

// A task reaches the agent by ID and body alone: its title may be the
// placeholder or a guess by a browser's local model, so it is never sent.
func TestTaskIDLine(t *testing.T) {
	if got := taskIDLine(model.Task{ID: 42, Title: "Fix the flake"}); got != "ID: "+monoflake.ID(42).String() {
		t.Errorf("got %q, want the ID line only", got)
	}
}

func TestStatusCheckMessage(t *testing.T) {
	want := "Status Check: You are currently working on task " + monoflake.ID(42).String() + ". Please provide a brief status update."
	for _, title := range []string{"Fix the flake", entity.UntitledTaskTitle} {
		if got := statusCheckMessage(model.Task{ID: 42, Title: title}); got != want {
			t.Errorf("title %q: got %q, want %q", title, got, want)
		}
	}
}

func TestHandleGetTask_LeavesOutTheTitle(t *testing.T) {
	for _, title := range []string{"Fix the flake", entity.UntitledTaskTitle} {
		ps := pushServer(t)
		ps.getNextTask = func(context.Context) (model.Task, error) {
			return model.Task{ID: 42, Title: title, Body: "Do the thing"}, nil
		}
		ps.getTask = func(context.Context, int64) (model.Task, error) {
			return model.Task{ID: 42, Title: title, Body: "Do the thing", Status: "ongoing"}, nil
		}

		for _, params := range []GetTaskParams{{}, {TaskID: monoflake.ID(42).String()}} {
			res, _, err := ps.handleGetTask(context.Background(), nil, params)
			if err != nil || res.IsError {
				t.Fatalf("getTask(%+v) failed: %v %+v", params, err, res)
			}
			text := res.Content[0].(*mcp.TextContent).Text
			if strings.Contains(text, "Title:") || strings.Contains(text, title) {
				t.Errorf("getTask(%+v) sent the title %q: %s", params, title, text)
			}
			if !strings.Contains(text, "ID: "+monoflake.ID(42).String()+"\n") || !strings.Contains(text, "Details: Do the thing") {
				t.Errorf("getTask(%+v) lost the ID or body: %s", params, text)
			}
		}
	}
}
