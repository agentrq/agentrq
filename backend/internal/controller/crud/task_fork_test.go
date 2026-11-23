// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package crud

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/storage"
	"github.com/golang/mock/gomock"
	"github.com/mustafaturan/monoflake"
	"gorm.io/datatypes"
)

type denyTaskLimiter struct{ stubLimiter }

func (*denyTaskLimiter) AllowTask(int64) bool { return false }

func attJSON(t *testing.T, atts ...entity.Attachment) datatypes.JSON {
	t.Helper()
	b, err := json.Marshal(atts)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// forkSource is a task whose messages are stored out of order, as GetTask's
// preload may return them.
func forkSource(t *testing.T) model.Task {
	base := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)
	return model.Task{
		ID: 100, WorkspaceID: 1, UserID: testUserID, Assignee: "agent", Status: "ongoing",
		Title: "Build it", Body: "Do the thing", AllowAllCommands: true, ClearContext: true,
		Attachments: attJSON(t, entity.Attachment{ID: "src-task-att", Filename: "spec.md", MimeType: "text/markdown"}),
		Messages: []model.Message{
			{ID: 3, TaskID: 100, Sender: "agent", Text: "third", CreatedAt: base.Add(2 * time.Minute)},
			{ID: 1, TaskID: 100, Sender: "human", Text: "first", CreatedAt: base},
			// Same instant as the third: the ID breaks the tie.
			{ID: 4, TaskID: 100, Sender: "human", Text: "fourth", CreatedAt: base.Add(2 * time.Minute)},
			{ID: 2, TaskID: 100, Sender: "agent", Text: "second", CreatedAt: base.Add(time.Minute),
				Attachments: attJSON(t, entity.Attachment{ID: "src-msg-att", Filename: "shot.png", MimeType: "image/png"}),
				Metadata:    datatypes.JSON(`{"type":"elicitation_request","status":"accept"}`)},
		},
	}
}

func TestForkTask_CopiesConversationUpToTheMessage(t *testing.T) {
	e := newTestController(t)
	next := int64(500)
	e.idgen.EXPECT().NextID().DoAndReturn(func() int64 { next++; return next }).AnyTimes()
	e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(activeWorkspace(), nil)
	e.repo.EXPECT().GetTask(gomock.Any(), int64(1), int64(100), testUserID).Return(forkSource(t), nil)
	e.storage.EXPECT().LoadRaw(storage.AttachmentKey(1, 100, "src-task-att")).Return([]byte("spec"), nil)
	e.storage.EXPECT().LoadRaw(storage.AttachmentKey(1, 100, "src-msg-att")).Return([]byte("png"), nil)
	saved := map[string]string{}
	e.storage.EXPECT().Save(gomock.Any(), gomock.Any()).DoAndReturn(func(id, data string) error {
		saved[id] = data
		return nil
	}).Times(2)

	var gotTask model.Task
	var gotMsgs []model.Message
	e.repo.EXPECT().CreateTaskWithMessages(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, tk model.Task, msgs []model.Message) (model.Task, error) {
			gotTask, gotMsgs = tk, msgs
			return tk, nil
		})

	rs, err := e.controller.ForkTask(context.Background(), entity.ForkTaskRequest{
		WorkspaceID: 1, TaskID: 100, MessageID: 3, Status: "ongoing", UserID: testUserIDStr,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotTask.Status != "ongoing" || gotTask.CreatedBy != "human" || gotTask.Assignee != "agent" {
		t.Errorf("fork status/createdBy/assignee = %s/%s/%s", gotTask.Status, gotTask.CreatedBy, gotTask.Assignee)
	}
	if gotTask.Title != "Fork: Build it" {
		t.Errorf("title = %q", gotTask.Title)
	}
	if !strings.HasPrefix(gotTask.Body, "Do the thing\n\nForked from task "+monoflake.ID(100).String()) {
		t.Errorf("body = %q", gotTask.Body)
	}
	if !gotTask.AllowAllCommands || !gotTask.ClearContext || gotTask.ParentID != 0 {
		t.Errorf("settings not carried over as expected: %+v", gotTask)
	}

	var texts []string
	for _, m := range gotMsgs {
		texts = append(texts, m.Text)
		if m.TaskID != gotTask.ID {
			t.Errorf("message %q belongs to task %d, want %d", m.Text, m.TaskID, gotTask.ID)
		}
		if m.ID < 500 {
			t.Errorf("message %q kept its source ID %d", m.Text, m.ID)
		}
	}
	if strings.Join(texts, ",") != "first,second,third" {
		t.Errorf("copied messages = %v, want first..third in order", texts)
	}
	if string(gotMsgs[1].Metadata) != `{"type":"elicitation_request","status":"accept"}` {
		t.Errorf("metadata not copied: %s", gotMsgs[1].Metadata)
	}

	// Attachments point at fresh copies, never at the source's files.
	var taskAtts, msgAtts []entity.Attachment
	_ = json.Unmarshal(gotTask.Attachments, &taskAtts)
	_ = json.Unmarshal(gotMsgs[1].Attachments, &msgAtts)
	if len(taskAtts) != 1 || taskAtts[0].ID == "src-task-att" || taskAtts[0].Filename != "spec.md" {
		t.Errorf("task attachments = %+v", taskAtts)
	}
	if len(msgAtts) != 1 || msgAtts[0].ID == "src-msg-att" {
		t.Errorf("message attachments = %+v", msgAtts)
	}
	// The copy is filed under the fork, not the source task.
	if saved[storage.AttachmentKey(1, gotTask.ID, taskAtts[0].ID)] != base64.StdEncoding.EncodeToString([]byte("spec")) {
		t.Errorf("task attachment bytes not copied under the fork: %v", saved)
	}
	if len(rs.Task.Messages) != 3 {
		t.Errorf("response carries %d messages, want 3", len(rs.Task.Messages))
	}
}

func TestForkTask_SkipsAttachmentsWhoseFilesAreGone(t *testing.T) {
	e := newTestController(t)
	next := int64(500)
	e.idgen.EXPECT().NextID().DoAndReturn(func() int64 { next++; return next }).AnyTimes()
	e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(activeWorkspace(), nil)
	e.repo.EXPECT().GetTask(gomock.Any(), int64(1), int64(100), testUserID).Return(forkSource(t), nil)
	// Gone from both where it is filed now and where it was kept before.
	e.storage.EXPECT().LoadRaw(storage.AttachmentKey(1, 100, "src-task-att")).Return(nil, errors.New("attachment not found"))
	e.storage.EXPECT().LoadRaw("src-task-att").Return(nil, errors.New("attachment not found"))
	e.storage.EXPECT().LoadRaw(storage.AttachmentKey(1, 100, "src-msg-att")).Return([]byte("png"), nil)
	e.storage.EXPECT().Save(gomock.Any(), gomock.Any()).Return(errors.New("disk full"))

	var gotTask model.Task
	var gotMsgs []model.Message
	e.repo.EXPECT().CreateTaskWithMessages(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, tk model.Task, msgs []model.Message) (model.Task, error) {
			gotTask, gotMsgs = tk, msgs
			return tk, nil
		})

	if _, err := e.controller.ForkTask(context.Background(), entity.ForkTaskRequest{
		WorkspaceID: 1, TaskID: 100, MessageID: 2, Status: "notstarted", UserID: testUserIDStr,
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotTask.Attachments != nil || gotMsgs[1].Attachments != nil {
		t.Errorf("unreadable or uncopyable attachments should be left out: %s / %s", gotTask.Attachments, gotMsgs[1].Attachments)
	}
	if gotTask.Status != "notstarted" {
		t.Errorf("status = %s, want notstarted", gotTask.Status)
	}
}

func TestForkTask_RemovesCopiedFilesWhenTheWriteFails(t *testing.T) {
	e := newTestController(t)
	next := int64(500)
	e.idgen.EXPECT().NextID().DoAndReturn(func() int64 { next++; return next }).AnyTimes()
	e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(activeWorkspace(), nil)
	e.repo.EXPECT().GetTask(gomock.Any(), int64(1), int64(100), testUserID).Return(forkSource(t), nil)
	e.storage.EXPECT().LoadRaw(gomock.Any()).Return([]byte("x"), nil).Times(2)
	e.storage.EXPECT().Save(gomock.Any(), gomock.Any()).Return(nil).Times(2)
	e.storage.EXPECT().Delete(gomock.Any()).Return(nil).Times(2)
	e.repo.EXPECT().CreateTaskWithMessages(gomock.Any(), gomock.Any(), gomock.Any()).Return(model.Task{}, errors.New("database unavailable"))

	_, err := e.controller.ForkTask(context.Background(), entity.ForkTaskRequest{
		WorkspaceID: 1, TaskID: 100, MessageID: 4, Status: "ongoing", UserID: testUserIDStr,
	})
	if err == nil || !strings.Contains(err.Error(), "database unavailable") {
		t.Fatalf("err = %v, want the write failure", err)
	}
}

func TestForkTask_MessageNotInTask(t *testing.T) {
	e := newTestController(t)
	e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(activeWorkspace(), nil)
	e.repo.EXPECT().GetTask(gomock.Any(), int64(1), int64(100), testUserID).Return(forkSource(t), nil)

	_, err := e.controller.ForkTask(context.Background(), entity.ForkTaskRequest{
		WorkspaceID: 1, TaskID: 100, MessageID: 999, Status: "ongoing", UserID: testUserIDStr,
	})
	if !errors.Is(err, base.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestForkTask_Refusals(t *testing.T) {
	req := entity.ForkTaskRequest{WorkspaceID: 1, TaskID: 100, MessageID: 1, Status: "ongoing", UserID: testUserIDStr}

	t.Run("rate limited", func(t *testing.T) {
		e := newTestController(t)
		e.controller.(*controller).limiter = &denyTaskLimiter{}
		if _, err := e.controller.ForkTask(context.Background(), req); err == nil || err.Error() != "rate limit exceeded" {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("status other than ongoing or notstarted", func(t *testing.T) {
		e := newTestController(t)
		bad := req
		bad.Status = "completed"
		if _, err := e.controller.ForkTask(context.Background(), bad); err == nil {
			t.Fatal("expected an error")
		}
	})
	t.Run("archived workspace", func(t *testing.T) {
		e := newTestController(t)
		ws := activeWorkspace()
		now := time.Now()
		ws.ArchivedAt = &now
		e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(ws, nil)
		if _, err := e.controller.ForkTask(context.Background(), req); err == nil {
			t.Fatal("expected an error")
		}
	})
	t.Run("task not found", func(t *testing.T) {
		e := newTestController(t)
		e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(activeWorkspace(), nil)
		e.repo.EXPECT().GetTask(gomock.Any(), int64(1), int64(100), testUserID).Return(model.Task{}, base.ErrNotFound)
		if _, err := e.controller.ForkTask(context.Background(), req); !errors.Is(err, base.ErrNotFound) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestForkTitle(t *testing.T) {
	if got := forkTitle("Fork: Build it"); got != "Fork: Build it" {
		t.Errorf("a fork of a fork should not stack prefixes, got %q", got)
	}
	long := strings.Repeat("é", 300)
	if got := []rune(forkTitle(long)); len(got) != 255 {
		t.Errorf("title is %d runes, want 255", len(got))
	}
}

func TestForkBody(t *testing.T) {
	if got := forkBody("", 100); !strings.HasPrefix(got, "Forked from task "+monoflake.ID(100).String()) {
		t.Errorf("empty body: %q", got)
	}
}

func TestCopyAttachments_NothingToCopy(t *testing.T) {
	e := newTestController(t)
	c := e.controller.(*controller)
	for _, raw := range []datatypes.JSON{nil, datatypes.JSON(`not json`), datatypes.JSON(`[]`)} {
		if out, ids := c.copyAttachments(1, 2, 3, raw); out != nil || ids != nil {
			t.Errorf("copyAttachments(%s) = %s, %v", raw, out, ids)
		}
	}
}

// Only the conversation travels: what people and the agent wrote, and the
// agent's questions with their answers. A question nobody answered is closed,
// since the run waiting on it belongs to the source task.
func TestForkTask_CopiesOnlyTheConversation(t *testing.T) {
	e := newTestController(t)
	next := int64(500)
	e.idgen.EXPECT().NextID().DoAndReturn(func() int64 { next++; return next }).AnyTimes()
	e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(1), testUserID).Return(activeWorkspace(), nil)
	at := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)
	msg := func(id int64, text, meta string) model.Message {
		m := model.Message{ID: id, TaskID: 100, Sender: "agent", Text: text, CreatedAt: at.Add(time.Duration(id) * time.Minute)}
		if meta != "" {
			m.Metadata = datatypes.JSON(meta)
		}
		return m
	}
	src := model.Task{ID: 100, WorkspaceID: 1, UserID: testUserID, Assignee: "agent", Title: "t", Messages: []model.Message{
		msg(1, "hello", ""),
		msg(2, "plan", `{"type":"agent_plan","content":"1. draft"}`),
		msg(3, "may I run it?", `{"type":"permission_request","status":"pending"}`),
		msg(4, "which one?", `{"type":"elicitation_request","status":"pending","message":"which one?"}`),
		msg(5, "", `{"type":"agent_usage"}`),
		msg(8, "thinking", `{"type":"agent_thought"}`),
		msg(6, "odd", `not json`),
		msg(7, "which colour?", `{"type":"elicitation_request","status":"decline"}`),
	}}
	e.repo.EXPECT().GetTask(gomock.Any(), int64(1), int64(100), testUserID).Return(src, nil)
	var got []model.Message
	e.repo.EXPECT().CreateTaskWithMessages(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, tk model.Task, msgs []model.Message) (model.Task, error) {
			got = msgs
			return tk, nil
		})

	if _, err := e.controller.ForkTask(context.Background(), entity.ForkTaskRequest{
		WorkspaceID: 1, TaskID: 100, MessageID: 8, Status: "ongoing", UserID: testUserIDStr,
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var texts []string
	for _, m := range got {
		texts = append(texts, m.Text)
	}
	if strings.Join(texts, "|") != "hello|plan|which one?|odd|which colour?" {
		t.Fatalf("copied = %v", texts)
	}
	var closed map[string]any
	_ = json.Unmarshal(got[2].Metadata, &closed)
	if closed["status"] != "cancel" || closed["message"] != "which one?" {
		t.Errorf("pending question = %s, want it closed with its question kept", got[1].Metadata)
	}
	if string(got[4].Metadata) != `{"type":"elicitation_request","status":"decline"}` {
		t.Errorf("answered question changed: %s", got[4].Metadata)
	}
	if string(got[1].Metadata) != `{"type":"agent_plan","content":"1. draft"}` {
		t.Errorf("plan changed: %s", got[1].Metadata)
	}
}
