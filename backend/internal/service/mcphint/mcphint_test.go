// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package mcphint

import "testing"

func deref(t *testing.T, name string, b *bool) bool {
	t.Helper()
	if b == nil {
		t.Fatalf("%s was not declared", name)
	}
	return *b
}

func TestRead(t *testing.T) {
	a := Read("Get a workspace")

	if a.Title != "Get a workspace" {
		t.Errorf("title = %q", a.Title)
	}
	if !a.ReadOnlyHint {
		t.Error("a read is not marked read-only")
	}
	// Stated anyway: OpenAI's plugin review wants every hint explicit.
	if deref(t, "destructiveHint", a.DestructiveHint) {
		t.Error("a read claims to destroy")
	}
	if a.IdempotentHint {
		t.Error("a read declares idempotentHint")
	}
	if deref(t, "openWorldHint", a.OpenWorldHint) {
		t.Error("a read claims an open world")
	}
}

func TestWrite(t *testing.T) {
	a := Write("Create a task")

	if a.Title != "Create a task" {
		t.Errorf("title = %q", a.Title)
	}
	if a.ReadOnlyHint {
		t.Error("a write is marked read-only")
	}
	if deref(t, "destructiveHint", a.DestructiveHint) {
		t.Error("an additive write is marked destructive")
	}
	// Two calls leave two tasks, which is the whole difference from Update.
	if a.IdempotentHint {
		t.Error("a write claims to be idempotent")
	}
	if deref(t, "openWorldHint", a.OpenWorldHint) {
		t.Error("a write claims an open world")
	}
}

func TestUpdate(t *testing.T) {
	a := Update("Update a task")

	if a.Title != "Update a task" {
		t.Errorf("title = %q", a.Title)
	}
	if a.ReadOnlyHint {
		t.Error("an update is marked read-only")
	}
	if deref(t, "destructiveHint", a.DestructiveHint) {
		t.Error("a field update is marked destructive")
	}
	if !a.IdempotentHint {
		t.Error("a field update is not marked idempotent")
	}
	if deref(t, "openWorldHint", a.OpenWorldHint) {
		t.Error("an update claims an open world")
	}
}

func TestOverwrite(t *testing.T) {
	a := Overwrite("Delete a task")

	if a.Title != "Delete a task" {
		t.Errorf("title = %q", a.Title)
	}
	if a.ReadOnlyHint {
		t.Error("an overwrite is marked read-only")
	}
	if !deref(t, "destructiveHint", a.DestructiveHint) {
		t.Error("an overwrite is not marked destructive")
	}
	if !a.IdempotentHint {
		t.Error("an overwrite is not marked idempotent")
	}
	if deref(t, "openWorldHint", a.OpenWorldHint) {
		t.Error("an overwrite claims an open world")
	}
}

// Every annotation block owns its booleans. Sharing one package-level `false`
// across fifty-three tools would mean a single stray write through any of these
// pointers re-labelling all of them, which no test of one tool would catch.
func TestHintsAreNotShared(t *testing.T) {
	first, second := Write("First"), Write("Second")

	if first.DestructiveHint == second.DestructiveHint {
		t.Error("two tools share one destructiveHint")
	}
	if first.OpenWorldHint == second.OpenWorldHint {
		t.Error("two tools share one openWorldHint")
	}

	*first.DestructiveHint = true
	if *second.DestructiveHint {
		t.Error("writing through one tool's hint changed another's")
	}
}

func TestOpenWorld(t *testing.T) {
	a := OpenWorld(Write("Run a site's tool"))

	if !deref(t, "openWorldHint", a.OpenWorldHint) {
		t.Error("an open-world tool claims a closed world")
	}
	if a.Title != "Run a site's tool" || deref(t, "destructiveHint", a.DestructiveHint) {
		t.Errorf("OpenWorld changed the other hints: %+v", a)
	}
}
