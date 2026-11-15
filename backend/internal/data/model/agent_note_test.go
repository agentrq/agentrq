// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package model

import "testing"

func TestAgentNoteID(t *testing.T) {
	a := AgentNoteID(1, "Keep me posted.")
	if a == 0 {
		t.Fatal("a note's id is zero, which means none")
	}
	if AgentNoteID(1, "Keep me posted.") != a {
		t.Error("the same note in the same account got two ids")
	}
	if AgentNoteID(2, "Keep me posted.") == a {
		t.Error("two accounts share a note row")
	}
	if AgentNoteID(1, "Be concise.") == a {
		t.Error("two notes share a row")
	}
	// The empty note is a note like any other: a hand-off with none is recorded.
	if AgentNoteID(1, "") == 0 {
		t.Error("the empty note's id is zero")
	}
}
