// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"testing"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/mustafaturan/monoflake"
)

// An untitled task reaches the agent by ID and body alone: the placeholder
// title is not something anyone wrote, so it is never sent.
func TestTaskPushContent(t *testing.T) {
	id := monoflake.ID(42).String()
	if got := taskPushContent(entity.Task{ID: 42, Title: "Fix the flake", Body: "Only on CI."}); got != "[Task "+id+"] Fix the flake\nOnly on CI." {
		t.Errorf("titled task: got %q", got)
	}
	if got := taskPushContent(entity.Task{ID: 42, Title: entity.UntitledTaskTitle, Body: "Only on CI."}); got != "[Task "+id+"]\nOnly on CI." {
		t.Errorf("untitled task: got %q", got)
	}
}

func TestReassignedTaskContent(t *testing.T) {
	if got := reassignedTaskContent(entity.Task{ID: 42, Title: "Fix the flake"}); got != "[Task reassigned to agent] Fix the flake" {
		t.Errorf("titled task: got %q", got)
	}
	want := "[Task reassigned to agent] " + monoflake.ID(42).String()
	if got := reassignedTaskContent(entity.Task{ID: 42, Title: entity.UntitledTaskTitle}); got != want {
		t.Errorf("untitled task: got %q, want %q", got, want)
	}
}
