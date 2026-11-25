// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/mustafaturan/monoflake"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/service/a2ui"
	"github.com/agentrq/agentrq/backend/internal/service/mcp"
)

// a2uiThread is a task's messages, edited as the repository edits them: one
// at a time, each edit reading the one before.
type a2uiThread struct {
	mu      sync.Mutex
	msgs    []model.Message
	edits   int
	editErr error
}

func (th *a2uiThread) message(id int64) model.Message {
	th.mu.Lock()
	defer th.mu.Unlock()
	for _, m := range th.msgs {
		if m.ID == id {
			return m
		}
	}
	return model.Message{}
}

// withThread gives e's server the thread: getTask reads it and editMessage
// edits it.
func (e *a2uiReplyEnv) withThread(msgs ...model.Message) *a2uiThread {
	th := &a2uiThread{msgs: msgs}
	e.ps.getTask = func(_ context.Context, taskID int64) (model.Task, error) {
		th.mu.Lock()
		defer th.mu.Unlock()
		return model.Task{ID: taskID, Messages: append([]model.Message(nil), th.msgs...)}, nil
	}
	e.ps.editMessage = func(_ context.Context, taskID, messageID int64, edit func(model.Message) (string, []byte, error)) error {
		th.mu.Lock()
		defer th.mu.Unlock()
		if th.editErr != nil {
			return th.editErr
		}
		for i, m := range th.msgs {
			if m.ID == messageID && m.TaskID == taskID {
				text, metadata, err := edit(m)
				if err != nil {
					return err
				}
				th.edits++
				th.msgs[i].Text, th.msgs[i].Metadata = text, metadata
				return nil
			}
		}
		return errors.New("message not found")
	}
	return th
}

// postedCard posts a2uiTestCard through reply and returns the message it
// made: the card as stored, its image resolved to the stored file.
func postedCard(t *testing.T, e *a2uiReplyEnv, id int64) model.Message {
	t.Helper()
	var atts []entity.Attachment
	reply := e.ps.reply
	e.ps.reply = func(ctx context.Context, chatID, text string, a []entity.Attachment, metadata any) (int64, error) {
		atts = a
		return reply(ctx, chatID, text, a, metadata)
	}
	t.Cleanup(func() { e.ps.reply = reply })
	if res, _, _ := e.ps.handleReply(context.Background(), nil, a2uiReplyParams(a2uiTestCard)); res.IsError {
		t.Fatalf("posting the card: %v", res.Content[0].(*mcp.TextContent).Text)
	}
	b, _ := json.Marshal(atts)
	md, _ := json.Marshal(e.metadata)
	e.events, e.replies = nil, 0
	return model.Message{ID: id, TaskID: 42, Sender: "agent", Text: "PR opened", Attachments: b, Metadata: md}
}

func updateParams(text, ui string) ReplyParams {
	return ReplyParams{TaskID: monoflake.ID(42).String(), Text: text, UI: json.RawMessage(ui)}
}

const retitle = `[{"version":"1.0","updateComponents":{"surfaceId":"pr-1","components":[{"id":"title","component":"Text","text":"PR merged"}]}}]`

func toolText(res *mcp.CallToolResult) string {
	return res.Content[0].(*mcp.TextContent).Text
}

func TestHandleReply_AnUpdateEditsTheCardInPlace(t *testing.T) {
	e := newA2UIReplyEnv(t)
	msg := postedCard(t, e, 555)
	th := e.withThread(msg)

	res, _, err := e.ps.handleReply(context.Background(), nil, updateParams("PR merged", retitle))
	if err != nil || res.IsError {
		t.Fatalf("update = %v, %v", res, err)
	}
	if got := toolText(res); got != `card "pr-1" updated in message `+monoflake.ID(555).String() {
		t.Errorf("answer = %q, want the message id", got)
	}
	got := th.message(555)
	var card A2UIMetadata
	if err := json.Unmarshal(got.Metadata, &card); err != nil {
		t.Fatal(err)
	}
	all := string(got.Metadata)
	if got.Text != "PR merged" || card.Type != "a2ui" || card.SurfaceID != "pr-1" || card.Version != "1.0" ||
		!strings.Contains(all, `"text":"PR merged"`) || strings.Contains(all, `"text":"PR opened"`) {
		t.Errorf("message = %q %s", got.Text, all)
	}
	// The image keeps the file the card's own reply attached.
	if !strings.Contains(all, `"url":"https://files.test/storage/`) || strings.Contains(all, "attachment:") {
		t.Errorf("the image lost its stored link: %s", all)
	}
	if e.replies != 0 {
		t.Error("an update posted a new message")
	}
	if len(e.events) != 1 || e.events[0].Action != entity.ActionA2UIUpdate || e.events[0].ResourceID != 555 || e.events[0].Actor != entity.ActorAgent {
		t.Errorf("events = %+v, want one a2ui_update for message 555", e.events)
	}
}

func TestHandleReply_AnUpdateEditsTheNewestCardWithItsSurface(t *testing.T) {
	e := newA2UIReplyEnv(t)
	older, newer := postedCard(t, e, 500), postedCard(t, e, 600)
	plain := model.Message{ID: 700, TaskID: 42, Text: "later", Metadata: []byte(`not json`)}
	th := e.withThread(older, newer, plain)
	if res, _, _ := e.ps.handleReply(context.Background(), nil, updateParams("PR merged", retitle)); res.IsError {
		t.Fatalf("update = %s", toolText(res))
	}
	if th.message(500).Text != "PR opened" || th.message(600).Text != "PR merged" {
		t.Errorf("edited %q and %q, want only the newer card", th.message(500).Text, th.message(600).Text)
	}
}

func TestHandleReply_DeleteSurfaceKeepsTheMessageAndDropsTheCard(t *testing.T) {
	e := newA2UIReplyEnv(t)
	th := e.withThread(postedCard(t, e, 555))
	res, _, _ := e.ps.handleReply(context.Background(), nil, updateParams("PR closed", `[{"version":"1.0","deleteSurface":{"surfaceId":"pr-1"}}]`))
	if res.IsError {
		t.Fatalf("delete = %s", toolText(res))
	}
	if got := th.message(555); got.Text != "PR closed" || got.Metadata != nil {
		t.Errorf("message = %q %s, want the new text and no card", got.Text, got.Metadata)
	}
}

// Updates sent at once are each applied to the card the one before left.
func TestHandleReply_ConcurrentUpdatesAreEachApplied(t *testing.T) {
	e := newA2UIReplyEnv(t)
	th := e.withThread(postedCard(t, e, 555))
	var mu sync.Mutex
	e.ps.pubsub = nil // the events are counted elsewhere; the fake is not safe for goroutines
	const updates = 10
	var wg sync.WaitGroup
	for i := range updates {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := "note" + string(rune('a'+i))
			ui := `[{"version":"1.0","updateDataModel":{"surfaceId":"pr-1","path":"/` + id + `","value":` + string(rune('0'+i)) + `}}]`
			res, _, _ := e.ps.handleReply(context.Background(), nil, updateParams("PR opened", ui))
			if res.IsError {
				mu.Lock()
				t.Errorf("update %d = %s", i, toolText(res))
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	all := string(th.message(555).Metadata)
	for i := range updates {
		if !strings.Contains(all, `"path":"/note`+string(rune('a'+i))+`"`) {
			t.Errorf("update %d was lost: %s", i, all)
		}
	}
	if th.edits != updates {
		t.Errorf("edits = %d, want %d", th.edits, updates)
	}
}

func TestHandleReply_AnUpdateIsRefused(t *testing.T) {
	question := model.Message{ID: 800, TaskID: 42, Metadata: []byte(`{"type":"elicitation_request","mode":"a2ui","surfaceId":"ask","status":"pending"}`)}
	for _, tc := range []struct {
		name    string
		thread  func(t *testing.T, e *a2uiReplyEnv) []model.Message
		params  ReplyParams
		editErr error
		want    string
	}{
		{
			name:   "an unknown surface",
			params: updateParams("x", `[{"version":"1.0","updateComponents":{"surfaceId":"nope","components":[]}}]`),
			want:   `no card with surfaceId "nope" in task ` + monoflake.ID(42).String() + `: a ui without createSurface updates a card already in the task; start a new one with createSurface`,
		},
		{
			name:   "a question",
			thread: func(*testing.T, *a2uiReplyEnv) []model.Message { return []model.Message{question} },
			params: updateParams("x", `[{"version":"1.0","updateComponents":{"surfaceId":"ask","components":[]}}]`),
			want:   `card "ask" is a question asked with elicit, which cannot be updated: send a new card that starts with createSurface`,
		},
		{
			name: "an answered card",
			thread: func(t *testing.T, e *a2uiReplyEnv) []model.Message {
				m := postedCard(t, e, 555)
				m.Metadata = []byte(strings.Replace(string(m.Metadata), `{`, `{"answered":{"name":"approve"},`, 1))
				return []model.Message{m}
			},
			params: updateParams("x", retitle),
			want:   `card "pr-1" was answered, so it cannot be updated: send a new card that starts with createSurface`,
		},
		{
			name: "files with an update",
			params: func() ReplyParams {
				p := updateParams("x", retitle)
				p.Attachments = []AttachmentParam{{Filename: "b.png", MimeType: "image/png", Data: "iVBORw0K"}}
				return p
			}(),
			want: `ui updates card "pr-1", which shows only the files its own reply attached: send the update without attachments, or a new card that starts with createSurface`,
		},
		{
			name:   "an update that breaks the card",
			thread: func(t *testing.T, e *a2uiReplyEnv) []model.Message { return []model.Message{postedCard(t, e, 555)} },
			params: updateParams("x", `[{"version":"1.0","updateComponents":{"surfaceId":"pr-1","components":[{"id":"col","component":"Column","children":["gone"]}]}}]`),
			want:   `ui is not valid A2UI v1.0: ui[1].updateComponents.components[1] "col": children[0] names "gone", which is not a component in this ui`,
		},
		{
			name:    "a message that cannot be edited",
			thread:  func(t *testing.T, e *a2uiReplyEnv) []model.Message { return []model.Message{postedCard(t, e, 555)} },
			params:  updateParams("x", retitle),
			editErr: errors.New("database unavailable"),
			want:    "database unavailable",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newA2UIReplyEnv(t)
			var msgs []model.Message
			if tc.thread != nil {
				msgs = tc.thread(t, e)
			}
			th := e.withThread(msgs...)
			th.editErr = tc.editErr
			res, _, _ := e.ps.handleReply(context.Background(), nil, tc.params)
			if !res.IsError || toolText(res) != tc.want {
				t.Errorf("update = %q, want %q", toolText(res), tc.want)
			}
			if th.edits != 0 || len(e.events) != 0 || e.replies != 0 {
				t.Errorf("a refused update changed something: %d edits, events %v, %d replies", th.edits, e.events, e.replies)
			}
		})
	}
}

// Between finding the card and holding its message, another update may have
// deleted it.
func TestHandleReply_ACardGoneBeforeItsLockIsRefused(t *testing.T) {
	e := newA2UIReplyEnv(t)
	msg := postedCard(t, e, 555)
	th := e.withThread(msg)
	edit := e.ps.editMessage
	e.ps.editMessage = func(ctx context.Context, taskID, messageID int64, f func(model.Message) (string, []byte, error)) error {
		th.mu.Lock()
		th.msgs[0].Metadata = nil
		th.mu.Unlock()
		return edit(ctx, taskID, messageID, f)
	}
	res, _, _ := e.ps.handleReply(context.Background(), nil, updateParams("x", retitle))
	if want := `card "pr-1" is no longer in message ` + monoflake.ID(555).String() + `: start a new card with createSurface`; !res.IsError || toolText(res) != want {
		t.Errorf("update = %q, want %q", toolText(res), want)
	}
}

func TestHandleReply_AnUpdateNeedsTheTask(t *testing.T) {
	e := newA2UIReplyEnv(t)
	e.ps.getTask = func(context.Context, int64) (model.Task, error) { return model.Task{}, errors.New("task not found") }
	res, _, _ := e.ps.handleReply(context.Background(), nil, updateParams("x", retitle))
	if !res.IsError || toolText(res) != "failed to get task: task not found" {
		t.Errorf("update = %v", res)
	}
}

func TestHandleReply_AnUpdateNeedsTheValidator(t *testing.T) {
	e := newA2UIReplyEnv(t)
	e.withThread(postedCard(t, e, 555))
	shared := sharedA2UI
	t.Cleanup(func() { sharedA2UI = shared })
	sharedA2UI = func() (a2ui.Service, error) { return nil, errors.New("schemas missing") }
	res, _, _ := e.ps.handleReply(context.Background(), nil, updateParams("x", retitle))
	if !res.IsError || toolText(res) != "ui is not available on this server (schemas missing): send the reply without it" {
		t.Errorf("update = %v", res)
	}
}
