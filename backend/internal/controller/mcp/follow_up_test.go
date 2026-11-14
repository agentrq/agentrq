// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/mustafaturan/monoflake"
	"gorm.io/datatypes"
)

var followUpMeta = datatypes.JSON(`{"followUp":true}`)

// followedUpTask is a reopened task with two follow-ups, stored out of order,
// among the rest of its conversation.
func followedUpTask() model.Task {
	return model.Task{ID: 7, Status: "notstarted", Assignee: "agent", Body: "Do it.", Messages: []model.Message{
		{ID: 30, Sender: "human", Text: "Then tag it.", Metadata: followUpMeta},
		{ID: 10, Sender: "agent", Text: "Done."},
		{ID: 20, Sender: "human", Text: "Also cover the error path.", Metadata: followUpMeta},
		{ID: 25, Sender: "human", Text: "Thanks!"},
	}}
}

const wantFollowUps = "\n" + followUpHeader + "\nAlso cover the error path.\nThen tag it."

// The poller lists a task without its messages, so the hand-off reads them,
// and puts every follow-up, oldest first, before the body.
func TestPollOnceHandsTheFollowUpsOverBeforeTheBody(t *testing.T) {
	ps := pushServer(t)
	l := attachLink(ps, "acp1:a")
	full := followedUpTask()
	ps.getTask = func(_ context.Context, id int64) (model.Task, error) {
		if id != 7 {
			t.Errorf("read task %d, want 7", id)
		}
		return full, nil
	}
	listed := full
	listed.Messages = nil

	ps.pollOnce(pendingTaskRepo(listed))

	want := fmtTask(7, "Next assigned task:\n"+taskIDLine(listed)+wantFollowUps+"\nDetails: Do it.")
	if len(l.tasks) != 1 || l.tasks[0] != want {
		t.Fatalf("the link got %q, want %q", l.tasks, want)
	}
}

// task get puts the follow-ups first too, whether the agent dequeues the task
// or reads it by id.
func TestHandleGetTaskPutsTheFollowUpsFirst(t *testing.T) {
	ps := pushServer(t)
	full := followedUpTask()
	dequeued := full
	dequeued.Messages = nil
	ps.getNextTask = func(context.Context) (model.Task, error) { return dequeued, nil }
	ps.getTask = func(context.Context, int64) (model.Task, error) { return full, nil }

	for _, params := range []GetTaskParams{{}, {TaskID: monoflake.ID(7).String()}} {
		res, _, err := ps.handleGetTask(context.Background(), nil, params)
		if err != nil || res.IsError {
			t.Fatalf("get(%+v) failed: %v %+v", params, err, res)
		}
		text := res.Content[0].(*mcp.TextContent).Text
		if !strings.Contains(text, "\nStatus: notstarted"+wantFollowUps+"\nDetails: Do it.") {
			t.Errorf("get(%+v) = %q, want the follow-ups between the status and the body", params, text)
		}
	}
}

func TestFollowUpBlockIsEmptyWithoutFollowUps(t *testing.T) {
	ps := pushServer(t)
	plain := model.Task{ID: 7, Messages: []model.Message{{ID: 1, Sender: "human", Text: "Thanks!"}}}
	if got := ps.followUpBlock(context.Background(), plain); got != "" {
		t.Errorf("followUpBlock = %q, want none", got)
	}
	// Nothing to read the messages with, or a read that fails: no block,
	// and the task is still handed over.
	if got := ps.followUpBlock(context.Background(), model.Task{ID: 7}); got != "" {
		t.Errorf("followUpBlock with no reader = %q, want none", got)
	}
	ps.getTask = func(context.Context, int64) (model.Task, error) {
		return model.Task{}, errors.New("database unavailable")
	}
	if got := ps.followUpBlock(context.Background(), model.Task{ID: 7}); got != "" {
		t.Errorf("followUpBlock after a failed read = %q, want none", got)
	}
}
