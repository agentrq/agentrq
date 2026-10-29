// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/mustafaturan/monoflake"
	"gorm.io/gorm"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/auth"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
)

// fakeTaskListeners records the changes it is told of.
type fakeTaskListeners struct {
	listening bool
	changes   [][3]int64
}

func (f *fakeTaskListeners) Listening() bool { return f.listening }

func (f *fakeTaskListeners) TaskChanged(workspaceID, taskID, delegatedBy int64) {
	f.changes = append(f.changes, [3]int64{workspaceID, taskID, delegatedBy})
}

// A task's own change and a message in it, a question of its agent included,
// reach the supervisor's listens with the workspace that delegated the task.
func TestNotifyTaskListeners(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Task{}, &model.Message{}); err != nil {
		t.Fatal(err)
	}
	const supervisor, task, message, question = int64(77), int64(101), int64(201), int64(202)
	for _, row := range []any{
		&model.Task{ID: task, WorkspaceID: memParent, UserID: memUser, Title: "a task", Status: "ongoing", CreatedByWorkspaceID: supervisor},
		&model.Message{ID: message, TaskID: task, UserID: memUser, Sender: "agent", Text: "done"},
		&model.Message{ID: question, TaskID: task, UserID: memUser, Sender: "agent", Text: "Allow Bash?", Metadata: []byte(`{"type":"permission_request","status":"pending"}`)},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	repo := base.New(sqliteConn{db: db})

	ev := func(r entity.ResourceType, a entity.Action, id int64) entity.CRUDEvent {
		return entity.CRUDEvent{WorkspaceID: memParent, UserID: memUser, ResourceType: r, Action: a, ResourceID: id}
	}
	changed := [][3]int64{{memParent, task, supervisor}}
	for _, tc := range []struct {
		name string
		ev   entity.CRUDEvent
		want [][3]int64
	}{
		{"status changed", ev(entity.ResourceTask, entity.ActionTaskUpdate, task), changed},
		{"completed", ev(entity.ResourceTask, entity.ActionTaskComplete, task), changed},
		{"new message", ev(entity.ResourceMessage, entity.ActionMessageCreate, message), changed},
		{"permission request", ev(entity.ResourceMessage, entity.ActionMessageCreate, question), changed},
		{"permission answered", ev(entity.ResourceMessage, entity.ActionMessageUpdate, question), changed},
		// Its row is gone: the event names its workspace, and nobody delegated it any more.
		{"deleted", ev(entity.ResourceTask, entity.ActionTaskDelete, 999), [][3]int64{{memParent, 999, 0}}},
		{"message that is gone", ev(entity.ResourceMessage, entity.ActionMessageCreate, 999), nil},
		{"not a task", ev(entity.ResourceWorkspace, entity.ActionWorkspaceUpdate, memParent), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			listeners := &fakeTaskListeners{listening: true}
			notifyTaskListeners(context.Background(), repo, listeners, tc.ev)
			if !slices.Equal(listeners.changes, tc.want) {
				t.Errorf("changes = %v, want %v", listeners.changes, tc.want)
			}
		})
	}
}

// Nobody listening, nothing read: a nil repository would panic otherwise.
func TestNotifyTaskListeners_ReadsNothingWhileNobodyListens(t *testing.T) {
	listeners := &fakeTaskListeners{}
	notifyTaskListeners(context.Background(), nil, listeners, entity.CRUDEvent{ResourceType: entity.ResourceMessage, ResourceID: 1})
	if listeners.changes != nil {
		t.Errorf("changes = %v, want none", listeners.changes)
	}
}

type orderedListeners struct{ steps *steps }

func (l orderedListeners) Close() { *l.steps = append(*l.steps, "complete task listens") }

// A listen is a POST that stays open, so it would hold the drain open until
// the deadline: each is completed first.
func TestShutdownCompletesTaskListensBeforeTheDrain(t *testing.T) {
	var got steps
	a := &App{
		server:        &orderedServer{steps: &got},
		taskListeners: orderedListeners{steps: &got},
		cancel:        func() {},
	}
	if err := a.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if want := (steps{"complete task listens", "drain http"}); !slices.Equal(got, want) {
		t.Errorf("shutdown did %v, want %v", got, want)
	}
}

// openAppListen opens a supervisor listen on uri through the app's real
// endpoint, with a CoreMCP token of user, and returns the stream's messages
// one at a time.
func openAppListen(t *testing.T, srv *httptest.Server, user int64, uri string) func() string {
	t.Helper()
	token, err := auth.NewTokenService(auth.TokenConfig{JWTSecret: "acp-test-secret"}).CreateMCPToken(monoflake.ID(user).String(), "coremcp", "access")
	if err != nil {
		t.Fatal(err)
	}
	body := `{"jsonrpc":"2.0","id":1,"method":"subscriptions/listen","params":{"_meta":{` +
		`"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}},` +
		`"notifications":{"resourceSubscriptions":["` + uri + `"]}}}`
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+"/mcp", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", "2026-07-28")
	req.Header.Set("Mcp-Method", "subscriptions/listen")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	lines := bufio.NewScanner(resp.Body)
	return func() string {
		for lines.Scan() {
			if data, ok := strings.CutPrefix(lines.Text(), "data: "); ok {
				return data
			}
		}
		return ""
	}
}

// The whole path, as main wires it: a supervisor's listen on the real
// endpoint hears of a task change published on the CRUD topic. Another
// account's token cannot follow that workspace at all.
func TestTheAppTellsAListenOfATaskChange(t *testing.T) {
	a := newTestApp(t, "false")
	const owner, stranger, workspace, task = int64(31), int64(32), int64(41), int64(51)
	db, err := gorm.Open(sqlite.Open(os.Getenv("AGENTRQ_SQLITE_DSN")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Workspace{ID: workspace, UserID: owner, Name: "worker"}).Error; err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(a.server.(http.Handler))
	t.Cleanup(srv.Close)
	uri := "agentrq://workspaces/" + monoflake.ID(workspace).String() + "/tasks/" + monoflake.ID(task).String()

	// The stranger's listen keeps nothing, so it ends right after its acknowledgement.
	strangers := openAppListen(t, srv, stranger, uri)
	if ack := strangers(); !strings.Contains(ack, "notifications/subscriptions/acknowledged") || strings.Contains(ack, uri) {
		t.Fatalf("another account's listen was acknowledged with %s", ack)
	}

	owners := openAppListen(t, srv, owner, uri)
	if ack := owners(); !strings.Contains(ack, "notifications/subscriptions/acknowledged") || !strings.Contains(ack, uri) {
		t.Fatalf("expected the acknowledgement of %s, got %s", uri, ack)
	}
	a.pubsub.Publish(context.Background(), pubsub.PublishRequest{PubSubID: entity.PubSubTopicCRUD, Event: entity.CRUDEvent{
		Action: entity.ActionTaskUpdate, WorkspaceID: workspace, UserID: owner, ResourceType: entity.ResourceTask, ResourceID: task,
	}})
	if notice := owners(); !strings.Contains(notice, "notifications/resources/updated") || !strings.Contains(notice, uri) {
		t.Fatalf("expected a resources/updated notice of %s, got %s", uri, notice)
	}
	if rest := strangers(); strings.Contains(rest, "resources/updated") {
		t.Errorf("another account heard of the change: %s", rest)
	}
}
