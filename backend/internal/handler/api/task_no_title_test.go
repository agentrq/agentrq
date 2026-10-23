// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"testing"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/mustafaturan/monoflake"
)

// A task reaches the agent by ID and body alone: its title may be the
// placeholder or a guess by a browser's local model, so it is never sent.
func TestTaskPushContent(t *testing.T) {
	id := monoflake.ID(42).String()
	for _, title := range []string{"Fix the flake", entity.UntitledTaskTitle} {
		if got := taskPushContent(entity.Task{ID: 42, Title: title, Body: "Only on CI."}); got != "[Task "+id+"]\nOnly on CI." {
			t.Errorf("title %q: got %q", title, got)
		}
	}
}

func TestReassignedTaskContent(t *testing.T) {
	want := "[Task reassigned to agent] " + monoflake.ID(42).String()
	for _, title := range []string{"Fix the flake", entity.UntitledTaskTitle} {
		if got := reassignedTaskContent(entity.Task{ID: 42, Title: title}); got != want {
			t.Errorf("title %q: got %q, want %q", title, got, want)
		}
	}
}
