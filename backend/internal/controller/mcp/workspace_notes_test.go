// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/mustafaturan/monoflake"
)

const testNotes = "**Important**\nKeep me posted."

// notesServer is a push server whose workspace has testNotes.
func notesServer(t *testing.T) *WorkspaceServer {
	t.Helper()
	ps := pushServer(t)
	ps.SetWorkspaceNotes(func(context.Context) string { return testNotes })
	return ps
}

func TestAgentTaskBodyAppendsTheNotes(t *testing.T) {
	if got := notesServer(t).AgentTaskBody(context.Background(), "Do it."); got != "Do it.\n\n"+testNotes {
		t.Errorf("AgentTaskBody = %q, want the body then the notes", got)
	}
}

// A body stored before the notes moved to the hand-off already ends with them;
// the agent is still handed them once.
func TestAgentTaskBodyDoesNotRepeatNotesAlreadyInTheBody(t *testing.T) {
	stored := "Do it.\n\n" + testNotes
	if got := notesServer(t).AgentTaskBody(context.Background(), stored); got != stored {
		t.Errorf("AgentTaskBody = %q, want the stored body unchanged", got)
	}
}

func TestAgentTaskBodyWithNoNotesSourceIsTheBody(t *testing.T) {
	if got := pushServer(t).AgentTaskBody(context.Background(), "Do it."); got != "Do it." {
		t.Errorf("AgentTaskBody = %q, want the body alone", got)
	}
}

func TestPollOnceHandsTheAgentTheNotes(t *testing.T) {
	ps := notesServer(t)
	l := attachLink(ps, "acp1:a")

	ps.pollOnce(pendingTaskRepo(model.Task{ID: 7, Status: "notstarted", Assignee: "agent", Body: "Do it."}))

	want := fmtTask(7, "Next assigned task:\n"+taskIDLine(model.Task{ID: 7})+"\nDetails: Do it.\n\n"+testNotes)
	if len(l.tasks) != 1 || l.tasks[0] != want {
		t.Fatalf("the link got %q, want %q", l.tasks, want)
	}
}

// task get hands the notes over with a task not yet started, the way the agent
// takes it, and leaves them out of a re-read once it has.
func TestHandleGetTaskCarriesTheNotesOnlyWhileTheTaskIsHandedOver(t *testing.T) {
	for status, want := range map[string]int{"notstarted": 1, "ongoing": 0, "blocked": 0} {
		task := model.Task{ID: 42, Body: "Do it.", Status: status}
		ps := notesServer(t)
		ps.getNextTask = func(context.Context) (model.Task, error) { return task, nil }
		ps.getTask = func(context.Context, int64) (model.Task, error) { return task, nil }

		for _, params := range []GetTaskParams{{}, {TaskID: monoflake.ID(42).String()}} {
			res, _, err := ps.handleGetTask(context.Background(), nil, params)
			if err != nil || res.IsError {
				t.Fatalf("get(%+v) failed: %v %+v", params, err, res)
			}
			text := res.Content[0].(*mcp.TextContent).Text
			if got := strings.Count(text, testNotes); got != want {
				t.Errorf("status %s, get(%+v): notes appear %d times, want %d: %s", status, params, got, want, text)
			}
		}
	}
}
