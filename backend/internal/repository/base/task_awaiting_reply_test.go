// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package base

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"gorm.io/datatypes"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

const (
	arUserID      = int64(70)
	arOtherUserID = int64(71)
	arWorkspaceID = int64(7)
)

// awaitingReplyCases are tasks and their conversations, oldest message first,
// with whether each awaits a reply. coremcp's TestReplyState runs the same
// conversations through the rule in Go.
var awaitingReplyCases = []struct {
	name     string
	msgs     []model.Message
	awaiting bool
}{
	{name: "an agent's question", awaiting: true, msgs: []model.Message{
		{Sender: "human", Text: "Fix the build."},
		{Sender: "agent", Text: "Should I also bump the version?\n \t"},
	}},
	{name: "answered by the human", msgs: []model.Message{
		{Sender: "agent", Text: "Should I also bump the version?"},
		{Sender: "human", Text: "No."},
	}},
	{name: "answered from Slack", msgs: []model.Message{
		{Sender: "agent", Text: "Should I also bump the version?"},
		{Sender: "slack", Text: "No.", Metadata: datatypes.JSON(`{"slack_user":"U1"}`)},
	}},
	{name: "the agent at work after its question", awaiting: true, msgs: []model.Message{
		{Sender: "agent", Text: "Which branch?"},
		{Sender: "agent", Text: "1200 tokens", Metadata: datatypes.JSON(`{"type":"usage"}`)},
	}},
	{name: "a statement", msgs: []model.Message{
		{Sender: "agent", Text: "Done. What else? Nothing, it is merged."},
	}},
	{name: "a pending permission request", awaiting: true, msgs: []model.Message{
		{Sender: "agent", Text: "Run git push", Metadata: datatypes.JSON(`{"type":"permission_request","status":"pending"}`)},
		{Sender: "agent", Text: "Waiting for approval."},
	}},
	{name: "a pending elicitation", awaiting: true, msgs: []model.Message{
		{Sender: "agent", Text: "Pick a name", Metadata: datatypes.JSON(`{"type":"elicitation_request","status":"pending"}`)},
	}},
	{name: "an answered elicitation", msgs: []model.Message{
		{Sender: "agent", Text: "Merge it?", Metadata: datatypes.JSON(`{"type":"elicitation_request","status":"accept"}`)},
	}},
	{name: "an allowed permission request", msgs: []model.Message{
		{Sender: "agent", Text: "Run git push?", Metadata: datatypes.JSON(`{"type":"permission_request","status":"allow"}`)},
	}},
	{name: "no messages"},
}

// seedAwaitingReply creates one task per case, IDs 1 up, and a task of
// another user awaiting a reply, which no list of this user's may show.
func seedAwaitingReply(t *testing.T, repo *repository) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	msgID := int64(1000)
	seed := func(taskID, userID int64, msgs []model.Message) {
		if _, err := repo.CreateTask(ctx, model.Task{ID: taskID, CreatedAt: now, UpdatedAt: now, WorkspaceID: arWorkspaceID, UserID: userID, Status: "ongoing"}); err != nil {
			t.Fatalf("create task: %v", err)
		}
		for i, m := range msgs {
			msgID++
			m.ID, m.TaskID, m.UserID, m.CreatedAt = msgID, taskID, userID, now.Add(time.Duration(i)*time.Second)
			if err := repo.CreateMessage(ctx, m); err != nil {
				t.Fatalf("create message: %v", err)
			}
		}
	}
	for i, c := range awaitingReplyCases {
		seed(int64(i+1), arUserID, c.msgs)
	}
	seed(500, arOtherUserID, awaitingReplyCases[0].msgs)
}

func TestListTasks_AwaitingReply(t *testing.T) {
	for _, d := range historyDBs(t) {
		t.Run(d.name, func(t *testing.T) {
			repo := New(&mockDB{db: d.db}).(*repository)
			seedAwaitingReply(t, repo)

			tasks, err := repo.ListTasks(context.Background(), entity.ListTasksRequest{AwaitingReply: true}, arUserID)
			if err != nil {
				t.Fatalf("list: %v", err)
			}
			var got []int64
			for _, task := range tasks {
				got = append(got, task.ID)
			}
			slices.Sort(got)
			var want []int64
			for i, c := range awaitingReplyCases {
				if c.awaiting {
					want = append(want, int64(i+1))
				}
			}
			if !slices.Equal(got, want) {
				t.Errorf("tasks awaiting a reply = %v, want %v", got, want)
			}

			page, err := repo.ListTasks(context.Background(), entity.ListTasksRequest{AwaitingReply: true, Status: []string{"ongoing"}, Limit: 2, Offset: 3}, arUserID)
			if err != nil {
				t.Fatalf("list a page: %v", err)
			}
			if len(page) != 1 {
				t.Errorf("the second page of 2 has %d tasks, want the 4th and last one", len(page))
			}
		})
	}
}

func TestListTasks_PreloadReplyMessages(t *testing.T) {
	for _, d := range historyDBs(t) {
		t.Run(d.name, func(t *testing.T) {
			repo := New(&mockDB{db: d.db}).(*repository)
			seedAwaitingReply(t, repo)

			tasks, err := repo.ListTasks(context.Background(), entity.ListTasksRequest{PreloadReplyMessages: true}, arUserID)
			if err != nil {
				t.Fatalf("list: %v", err)
			}
			texts := map[int64][]string{}
			for _, task := range tasks {
				for _, m := range task.Messages {
					texts[task.ID] = append(texts[task.ID], m.Text)
				}
			}
			want := map[int64][]string{
				1: {"Should I also bump the version?\n \t"},
				2: {"No."},
				3: {"No."},
				4: {"Which branch?"},
				5: {"Done. What else? Nothing, it is merged."},
				6: {"Run git push", "Waiting for approval."},
				7: {"Pick a name"},
				8: {"Merge it?"},
			}
			if len(tasks) != len(awaitingReplyCases) {
				t.Fatalf("listed %d tasks, want %d", len(tasks), len(awaitingReplyCases))
			}
			for id, w := range want {
				if !slices.Equal(texts[id], w) {
					t.Errorf("task %d preloaded %q, want %q", id, texts[id], w)
				}
			}
			for _, id := range []int64{9, 10} {
				if len(texts[id]) != 0 {
					t.Errorf("task %d preloaded %q, want nothing", id, texts[id])
				}
			}
		})
	}
}

func TestListTasks_PreloadReplyMessagesFails(t *testing.T) {
	db := historyDBs(t)[0].db
	repo := New(&mockDB{db: db}).(*repository)
	seedAwaitingReply(t, repo)
	if err := db.Migrator().DropTable(&model.Message{}); err != nil {
		t.Fatalf("drop messages: %v", err)
	}

	if _, err := repo.ListTasks(context.Background(), entity.ListTasksRequest{PreloadReplyMessages: true}, arUserID); err == nil {
		t.Error("listing with the messages table gone succeeded, want the database's error")
	}
}

// Without a Postgres to run on, the Postgres forms are at least the ones a
// Postgres connection picks; TestListTasks_AwaitingReply runs them when
// AGENTRQ_TEST_POSTGRES_DSN is set.
func TestAwaitingReply_PostgresPicksItsOwnSQL(t *testing.T) {
	db, err := gorm.Open(postgres.New(postgres.Config{DSN: "host=127.0.0.1 port=1"}), &gorm.Config{DryRun: true, DisableAutomaticPing: true})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if got := awaitingReplyClause(db); got != pgAwaitingReply {
		t.Errorf("clause = %q, want the Postgres one", got)
	}
	var stmt string
	if err := db.Callback().Query().After("gorm:query").Register("awaiting_reply_sql", func(tx *gorm.DB) { stmt = tx.Statement.SQL.String() }); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := New(&mockDB{db: db}).(*repository).awaitingReplyMessages(context.Background(), []model.Task{{ID: 1}}); err != nil {
		t.Fatalf("awaitingReplyMessages: %v", err)
	}
	if !strings.Contains(stmt, "c.metadata->>'type'") {
		t.Errorf("query = %s, want the Postgres form", stmt)
	}
}
