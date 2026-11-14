// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package coremcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	view "github.com/agentrq/agentrq/backend/internal/data/view/api"
)

// changesCrud answers ListTaskChanges with res, or err, and keeps the request.
type changesCrud struct {
	mockServerCrud
	req entity.ListTaskChangesRequest
	res *entity.ListTaskChangesResponse
	err error
}

func (m *changesCrud) ListTaskChanges(_ context.Context, req entity.ListTaskChangesRequest) (*entity.ListTaskChangesResponse, error) {
	m.req = req
	return m.res, m.err
}

var changesSince = time.Date(2026, 11, 14, 4, 0, 0, 0, time.UTC)

func changesAt(minutes int) time.Time { return changesSince.Add(time.Duration(minutes) * time.Minute) }

func callChanges(t *testing.T, fake *changesCrud, payload map[string]any) callResult {
	t.Helper()
	srv := &WorkspaceServer{crud: fake}
	return toolResult(srv.handleTask(authedContext(), nil, TaskParams{Action: "changes", Payload: payload}))
}

func taskChanges(t *testing.T, fake *changesCrud, payload map[string]any) (view.TaskChangesResponse, string) {
	t.Helper()
	text := textOf(t, callChanges(t, fake, payload))
	var got view.TaskChangesResponse
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatal(err)
	}
	return got, text
}

// asked is a task waiting on its agent's question, asked at +15 after the
// human wrote at +10; quiet is a task whose status alone changed, at +20.
func changedTasks() *entity.ListTaskChangesResponse {
	return &entity.ListTaskChangesResponse{
		Tasks: []entity.Task{
			{ID: 1, WorkspaceID: testWorkspace, Title: "Release", Status: "ongoing", UpdatedAt: changesAt(-30), Messages: []entity.Message{
				{ID: 11, TaskID: 1, CreatedAt: changesAt(15), Sender: "agent", Text: "  Which branch should I release from?\n", Metadata: map[string]any{"type": "elicitation_request", "status": "pending"}},
			}},
			{ID: 2, WorkspaceID: testWorkspace, Title: "Docs", Status: "completed", UpdatedAt: changesAt(20)},
		},
		NewMessages:     map[int64]int{1: 2},
		StatusChangedAt: map[int64]time.Time{2: changesAt(20)},
	}
}

func TestTaskChanges_AnswersAShortLineForEachChangedTask(t *testing.T) {
	fake := &changesCrud{res: changedTasks()}
	before := time.Now()
	got, _ := taskChanges(t, fake, map[string]any{
		"since":                changesSince.Format(time.RFC3339),
		"workspaceId":          base62(testWorkspace),
		"createdByWorkspaceId": base62(delegatingWorkspace),
	})

	want := entity.ListTaskChangesRequest{UserID: testUserID, Since: changesSince, WorkspaceID: testWorkspace, CreatedByWorkspaceID: delegatingWorkspace, Limit: defaultTaskChanges + 1}
	if fake.req != want {
		t.Errorf("asked %+v, want %+v", fake.req, want)
	}
	// now trails the clock by the lag, and is what to pass next.
	if lo, hi := before.Add(-changesLag-time.Microsecond), time.Now().Add(-changesLag); got.Now.Before(lo) || got.Now.After(hi) || got.HasMore {
		t.Errorf("now %v, hasMore %v; want between %v and %v, false", got.Now, got.HasMore, lo, hi)
	}
	if len(got.Changes) != 2 {
		t.Fatalf("changes = %+v, want 2", got.Changes)
	}
	asked, quiet := got.Changes[0], got.Changes[1]
	if asked.TaskID != base62(1) || asked.WorkspaceID != base62(testWorkspace) || asked.Title != "Release" || asked.Status != "ongoing" ||
		asked.StatusChangedAt != nil || asked.NewMessages != 2 || asked.LastMessageBy != "agent" ||
		asked.LastMessagePreview != "Which branch should I release from?" || !asked.AwaitingReply {
		t.Errorf("the asking task = %+v", asked)
	}
	if quiet.StatusChangedAt == nil || !quiet.StatusChangedAt.Equal(changesAt(20)) || quiet.NewMessages != 0 ||
		quiet.LastMessageBy != "" || quiet.LastMessagePreview != "" || quiet.AwaitingReply {
		t.Errorf("the quiet task = %+v", quiet)
	}
}

// A preview is the start of the newest message, never more than
// previewChars characters, and only of a message the supervisor has not read.
func TestTaskChanges_PreviewsOnlyANewMessagesStart(t *testing.T) {
	res := changedTasks()
	res.Tasks[0].Messages[0].Text = strings.Repeat("é", previewChars+5)
	got, _ := taskChanges(t, &changesCrud{res: res}, map[string]any{"since": changesSince.Format(time.RFC3339)})
	if want := strings.Repeat("é", previewChars) + "…"; got.Changes[0].LastMessagePreview != want {
		t.Errorf("preview = %q, want %q", got.Changes[0].LastMessagePreview, want)
	}

	res = changedTasks()
	res.NewMessages = nil
	got, _ = taskChanges(t, &changesCrud{res: res}, map[string]any{"since": changesSince.Format(time.RFC3339)})
	if c := got.Changes[0]; c.LastMessagePreview != "" || c.LastMessageBy != "agent" {
		t.Errorf("no new messages: preview %q, by %q; want no preview, by agent", c.LastMessagePreview, c.LastMessageBy)
	}

	// Counted, but gone by the time it was read.
	if p := preview(nil); p != "" {
		t.Errorf("preview of nothing = %q", p)
	}
}

// When the limit cuts the list, now is where it stopped: the last task's
// change time, its row's or its newest message's, whichever is later.
func TestTaskChanges_ACutListStopsAtItsLastTask(t *testing.T) {
	for _, tc := range []struct {
		name  string
		limit int
		want  time.Time
	}{
		{"its message", 1, changesAt(15)},
		{"its row", 2, changesAt(20)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := changedTasks()
			res.Tasks = append(res.Tasks, entity.Task{ID: 3, UpdatedAt: changesAt(25)})
			fake := &changesCrud{res: res}
			got, _ := taskChanges(t, fake, map[string]any{"since": changesSince.Format(time.RFC3339), "limit": tc.limit})
			if fake.req.Limit != tc.limit+1 || len(got.Changes) != tc.limit || !got.HasMore || !got.Now.Equal(tc.want) {
				t.Errorf("asked for %d; answered %d changes, hasMore %v, now %v; want %d, true, %v", fake.req.Limit, len(got.Changes), got.HasMore, got.Now, tc.limit, tc.want)
			}
		})
	}
}

func TestTaskChanges_LimitIsCapped(t *testing.T) {
	fake := &changesCrud{res: &entity.ListTaskChangesResponse{}}
	taskChanges(t, fake, map[string]any{"since": changesSince.Format(time.RFC3339), "limit": 1000})
	if fake.req.Limit != maxTaskChanges+1 {
		t.Errorf("limit 1000 asked for %d, want %d", fake.req.Limit, maxTaskChanges+1)
	}
}

// A poll that finds nothing is a few bytes.
func TestTaskChanges_NothingChangedIsAFewBytes(t *testing.T) {
	got, text := taskChanges(t, &changesCrud{res: &entity.ListTaskChangesResponse{}}, map[string]any{"since": changesSince.Format(time.RFC3339Nano)})
	if len(got.Changes) != 0 || len(text) > 64 || !strings.HasSuffix(text, `,"changes":[]}`) {
		t.Errorf("nothing changed = %s (%d bytes)", text, len(text))
	}
}

func TestTaskChanges_Errors(t *testing.T) {
	for name, tc := range map[string]struct {
		payload map[string]any
		err     error
		want    string
	}{
		"no since":       {map[string]any{}, nil, "needs since"},
		"an empty since": {map[string]any{"since": ""}, nil, "since is required"},
		"a bad since":    {map[string]any{"since": "yesterday"}, nil, "since must be an RFC3339 time"},
		"database error": {map[string]any{"since": changesSince.Format(time.RFC3339)}, errors.New("database unavailable"), "database unavailable"},
	} {
		t.Run(name, func(t *testing.T) {
			res := callChanges(t, &changesCrud{err: tc.err}, tc.payload)
			if !res.isError || !strings.Contains(res.text, tc.want) {
				t.Errorf("answered %+v, want an error saying %q", res, tc.want)
			}
		})
	}
}

// With excludeSelf, the preview is of the others' newest message, and a cut
// list stops where the others last changed its last task.
func TestTaskChanges_ExcludeSelf(t *testing.T) {
	res := changedTasks()
	// The supervisor wrote last on task 1; the human before it.
	res.LastMessages = map[int64]entity.Message{1: {ID: 10, TaskID: 1, CreatedAt: changesAt(10), Sender: "human", Text: "Release from main."}}
	res.NewMessages = map[int64]int{1: 1}
	res.ChangedAt = map[int64]time.Time{1: changesAt(10), 2: changesAt(20)}
	res.Tasks = append(res.Tasks, entity.Task{ID: 3})
	fake := &changesCrud{res: res}
	got, _ := taskChanges(t, fake, map[string]any{"since": changesSince.Format(time.RFC3339), "excludeSelf": true, "limit": 1})

	if !fake.req.ExcludeSelf {
		t.Error("excludeSelf was not asked for")
	}
	if len(got.Changes) != 1 || !got.HasMore || !got.Now.Equal(changesAt(10)) {
		t.Fatalf("answered %+v; want task 1, cut at the others' change at +10", got)
	}
	if c := got.Changes[0]; c.NewMessages != 1 || c.LastMessagePreview != "Release from main." || c.LastMessageBy != "agent" {
		t.Errorf("task 1 = %+v; want the human's message previewed, the newest by the agent", c)
	}

	// A task the others changed only by its status has no preview.
	got, _ = taskChanges(t, &changesCrud{res: res}, map[string]any{"since": changesSince.Format(time.RFC3339), "excludeSelf": true, "limit": 2})
	if c := got.Changes[1]; c.LastMessagePreview != "" || !got.Now.Equal(changesAt(20)) {
		t.Errorf("task 2 = %+v, now %v; want no preview, cut at +20", c, got.Now)
	}
}

// What the supervisor writes through its task tool is asked to be put down
// to its workspace.
func TestTaskWrites_AreBySupervisor(t *testing.T) {
	fake := &supervisorWritesCrud{}
	srv := &WorkspaceServer{crud: fake}
	ids := map[string]any{"workspaceId": base62(testWorkspace), "taskId": base62(42)}
	with := func(extra map[string]any) map[string]any {
		p := map[string]any{}
		for k, v := range ids {
			p[k] = v
		}
		for k, v := range extra {
			p[k] = v
		}
		return p
	}
	for action, payload := range map[string]map[string]any{
		"reply":        with(map[string]any{"text": "hi"}),
		"respond":      with(map[string]any{"action": "allow"}),
		"updateStatus": with(map[string]any{"status": "completed"}),
	} {
		if res := toolResult(srv.handleTask(authedContext(), nil, TaskParams{Action: action, Payload: payload})); res.isError {
			t.Fatalf("%s: %s", action, res.text)
		}
	}
	if want := []string{"reply", "respond", "updateStatus"}; len(fake.by) != 3 {
		t.Errorf("by the supervisor: %v, want %v", fake.by, want)
	}
}

// supervisorWritesCrud notes each write that came by the supervisor.
type supervisorWritesCrud struct {
	mockServerCrud
	by []string
}

func (m *supervisorWritesCrud) ReplyToTask(ctx context.Context, req entity.ReplyToTaskRequest) (*entity.ReplyToTaskResponse, error) {
	if req.BySupervisor {
		m.by = append(m.by, "reply")
	}
	return m.mockServerCrud.ReplyToTask(ctx, req)
}

func (m *supervisorWritesCrud) RespondToTask(ctx context.Context, req entity.RespondToTaskRequest) (*entity.RespondToTaskResponse, error) {
	if req.BySupervisor {
		m.by = append(m.by, "respond")
	}
	return m.mockServerCrud.RespondToTask(ctx, req)
}

func (m *supervisorWritesCrud) UpdateTaskStatus(ctx context.Context, req entity.UpdateTaskStatusRequest) (*entity.UpdateTaskStatusResponse, error) {
	if req.BySupervisor {
		m.by = append(m.by, "updateStatus")
	}
	return m.mockServerCrud.UpdateTaskStatus(ctx, req)
}
