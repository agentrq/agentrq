// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package model

import (
	"reflect"
	"sync"
	"testing"

	"gorm.io/datatypes"
	"gorm.io/gorm/schema"
)

// ForkSettings is written to the database by column, so every key must be one
// of the workspace's columns, carrying that column's value.
func TestWorkspaceForkSettings(t *testing.T) {
	w := Workspace{ID: 20, Name: "fork's own", NotificationSettings: datatypes.JSON(`{"taskCreated":true}`),
		AutoAllowedTools: datatypes.JSON(`["Bash"]`), AllowAllCommands: true, ClearContextDefault: true,
		SelfLearningLoopNote: "n", InputSendDelaySeconds: 3, WorkingDirectory: "/fork"}
	got := w.ForkSettings()
	want := map[string]any{
		"notification_settings":    datatypes.JSON(`{"taskCreated":true}`),
		"auto_allowed_tools":       datatypes.JSON(`["Bash"]`),
		"allow_all_commands":       true,
		"clear_context_default":    true,
		"self_learning_loop_note":  "n",
		"input_send_delay_seconds": 3,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}

	s, err := schema.Parse(&Workspace{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	for column := range got {
		if s.LookUpField(column) == nil {
			t.Errorf("%q is not a workspace column", column)
		}
	}
}

func TestWorkspaceContentID(t *testing.T) {
	if got := (Workspace{ID: 20}).ContentID(); got != 20 {
		t.Errorf("a workspace's content is %d, want its own", got)
	}
	if got := (Workspace{ID: 21, ForkOfID: 20}).ContentID(); got != 20 {
		t.Errorf("a fork's content is %d, want its parent's", got)
	}
}

func TestTaskState(t *testing.T) {
	for status, want := range map[string]TaskState{
		"notstarted": TaskStateNotStarted,
		"ongoing":    TaskStateOngoing,
		"blocked":    TaskStateBlocked,
		"completed":  TaskStateCompleted,
		"rejected":   TaskStateRejected,
		"cron":       TaskStateCron,
		"needsinput": TaskStateNeedsInput,
	} {
		if got := TaskStateFromStatus(status); got != want {
			t.Errorf("%q = %d, want %d", status, got, want)
		}
		if got := want.String(); got != status {
			t.Errorf("%d = %q, want %q", want, got, status)
		}
	}
	// An unrecognised status is recorded as none, which names no status.
	if got := TaskStateFromStatus("archived"); got != TaskStateNone {
		t.Errorf("an unknown status = %d, want none", got)
	}
	if got := TaskStateNone.String(); got != "" {
		t.Errorf("none = %q, want blank", got)
	}
}
