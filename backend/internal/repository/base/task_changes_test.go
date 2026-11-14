// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package base

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
)

const (
	tcUser      = int64(41)
	tcOther     = int64(42)
	tcWorkspace = int64(510)
	tcElsewhere = int64(511)
	tcDelegator = int64(512)
)

// tcSince is the time the changes are asked about; the seed is around it.
var tcSince = time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)

func tcAt(minutes int) time.Time { return tcSince.Add(time.Duration(minutes) * time.Minute) }

func tcTask(id, user, workspace int64, updated int) model.Task {
	return model.Task{ID: id, UserID: user, WorkspaceID: workspace, Status: "ongoing", Title: "t", CreatedAt: tcAt(-30), UpdatedAt: tcAt(updated)}
}

func tcMessage(id, task, user int64, at int, sender, kind string) model.Message {
	m := model.Message{ID: id, TaskID: task, UserID: user, CreatedAt: tcAt(at), Sender: sender, Text: "message " + kind}
	if kind != "" {
		m.Metadata = datatypes.JSON(`{"type":"` + kind + `"}`)
	}
	return m
}

// seedTaskChanges writes, for tcUser unless said otherwise:
//
//	1  untouched since before since
//	2  untouched, but the human wrote at +10, the agent thought at +12 and
//	   asked at +15
//	3  untouched, with only the agent's thoughts after since
//	4  another account's, written at +30
//	5  in tcElsewhere, delegated by tcDelegator, written at +20
//	6  written at +30
func seedTaskChanges(t *testing.T, db *gorm.DB) {
	t.Helper()
	delegated := tcTask(5, tcUser, tcElsewhere, 20)
	delegated.CreatedByWorkspaceID = tcDelegator
	tasks := []model.Task{tcTask(1, tcUser, tcWorkspace, -30), tcTask(2, tcUser, tcWorkspace, -30), tcTask(3, tcUser, tcWorkspace, -30), tcTask(4, tcOther, tcWorkspace, 30), delegated, tcTask(6, tcUser, tcWorkspace, 30)}
	msgs := []model.Message{
		tcMessage(101, 1, tcUser, -20, "agent", ""),
		tcMessage(102, 2, tcUser, -20, "agent", ""),
		tcMessage(103, 2, tcUser, 10, "human", ""),
		tcMessage(104, 2, tcUser, 12, "agent", "agent_thought"),
		tcMessage(105, 2, tcUser, 15, "agent", "elicitation_request"),
		tcMessage(106, 3, tcUser, 5, "agent", "agent_thought"),
		tcMessage(107, 4, tcOther, 5, "human", ""),
	}
	if err := db.Create(&tasks).Error; err != nil {
		t.Fatalf("seed tasks: %v", err)
	}
	if err := db.Create(&msgs).Error; err != nil {
		t.Fatalf("seed messages: %v", err)
	}
}

func taskIDs(tasks []model.Task) []int64 {
	ids := make([]int64, len(tasks))
	for i, task := range tasks {
		ids[i] = task.ID
	}
	return ids
}

func TestListTaskChanges(t *testing.T) {
	ctx := context.Background()
	for _, d := range historyDBs(t) {
		t.Run(d.name, func(t *testing.T) {
			seedTaskChanges(t, d.db)
			repo := New(&mockDB{db: d.db})
			list := func(req entity.ListTaskChangesRequest) []model.Task {
				t.Helper()
				req.Since = tcSince
				if req.Limit == 0 {
					req.Limit = 10
				}
				tasks, err := repo.ListTaskChanges(ctx, req, tcUser, 0)
				if err != nil {
					t.Fatalf("ListTaskChanges(%+v): %v", req, err)
				}
				return tasks
			}

			// The least recently changed first: 2 by its message at +15.
			tasks := list(entity.ListTaskChangesRequest{})
			if got, want := taskIDs(tasks), []int64{2, 5, 6}; !reflect.DeepEqual(got, want) {
				t.Fatalf("changed tasks = %v, want %v", got, want)
			}
			// Each with the messages its reply state reads: the newest of its
			// conversation, the agent's question.
			if got := tasks[0].Messages; len(got) != 1 || got[0].ID != 105 {
				t.Errorf("task 2 came with messages %+v, want only its newest, 105", got)
			}
			if got, want := taskIDs(list(entity.ListTaskChangesRequest{Limit: 2})), []int64{2, 5}; !reflect.DeepEqual(got, want) {
				t.Errorf("limit 2 = %v, want %v", got, want)
			}
			if got, want := taskIDs(list(entity.ListTaskChangesRequest{WorkspaceID: tcElsewhere})), []int64{5}; !reflect.DeepEqual(got, want) {
				t.Errorf("in workspace %d = %v, want %v", tcElsewhere, got, want)
			}
			if got, want := taskIDs(list(entity.ListTaskChangesRequest{CreatedByWorkspaceID: tcDelegator})), []int64{5}; !reflect.DeepEqual(got, want) {
				t.Errorf("delegated by %d = %v, want %v", tcDelegator, got, want)
			}

			// Nothing after the newest change.
			tasks, err := repo.ListTaskChanges(ctx, entity.ListTaskChangesRequest{Since: tcAt(31), Limit: 10}, tcUser, 0)
			if err != nil || len(tasks) != 0 {
				t.Errorf("changes after the last = %v, %v; want none", taskIDs(tasks), err)
			}
		})
	}
}

func TestCountNewMessages(t *testing.T) {
	ctx := context.Background()
	for _, d := range historyDBs(t) {
		t.Run(d.name, func(t *testing.T) {
			seedTaskChanges(t, d.db)
			got, err := New(&mockDB{db: d.db}).CountNewMessages(ctx, []int64{1, 2, 3}, tcSince, 0)
			if err != nil {
				t.Fatal(err)
			}
			// Task 2's human message and question; never a thought.
			if want := map[int64]int{2: 2}; !reflect.DeepEqual(got, want) {
				t.Errorf("new messages = %v, want %v", got, want)
			}
		})
	}
}

func TestListStatusChanges(t *testing.T) {
	ctx := context.Background()
	const (
		none       = model.TaskStateNone
		notStarted = model.TaskStateNotStarted
		ongoing    = model.TaskStateOngoing
		completed  = model.TaskStateCompleted
		needsInput = model.TaskStateNeedsInput
	)
	type change struct {
		task     int64
		from, to model.TaskState
		at       int
	}
	history := []change{
		// 1 started after since.
		{1, none, notStarted, -30}, {1, notStarted, ongoing, 5},
		// 2 waited on the human and went back to work: no change.
		{2, none, ongoing, -30}, {2, ongoing, needsInput, 5}, {2, needsInput, ongoing, 8},
		// 3 was waiting at since and left the wait completed.
		{3, none, ongoing, -30}, {3, ongoing, needsInput, -10}, {3, needsInput, completed, 6},
		// 4 was created after since.
		{4, none, notStarted, 3},
		// 5 has not changed since.
		{5, none, ongoing, -30},
		// 6 began waiting: still ongoing.
		{6, none, ongoing, -30}, {6, ongoing, needsInput, 2},
	}
	for _, d := range historyDBs(t) {
		t.Run(d.name, func(t *testing.T) {
			for i, c := range history {
				row := model.TaskStateTransition{ID: int64(i + 1), UserID: tcUser, WorkspaceID: tcWorkspace, TaskID: c.task, FromState: c.from, ToState: c.to, CreatedAt: tcAt(c.at)}
				if err := d.db.Create(&row).Error; err != nil {
					t.Fatal(err)
				}
			}
			got, _, err := New(&mockDB{db: d.db}).ListStatusChanges(ctx, []int64{1, 2, 3, 4, 5, 6}, tcSince, 0)
			if err != nil {
				t.Fatal(err)
			}
			want := map[int64]time.Time{1: tcAt(5), 3: tcAt(6), 4: tcAt(3)}
			if len(got) != len(want) {
				t.Fatalf("status changes = %v, want %v", got, want)
			}
			for task, at := range want {
				if !got[task].Equal(at) {
					t.Errorf("task %d's status changed at %v, want %v", task, got[task], at)
				}
			}
		})
	}
}

// A poll finds what changed by an index on each half, the tasks written and
// the messages created after since, never by reading every task or message of
// the account.
func TestListTaskChangesUsesTheIndexes(t *testing.T) {
	ctx := context.Background()
	for _, d := range historyDBs(t) {
		t.Run(d.name, func(t *testing.T) {
			var tasks []model.Task
			var msgs []model.Message
			for i := int64(1); i <= 3000; i++ {
				tasks = append(tasks, tcTask(i, tcUser, tcWorkspace, -30-int(i%60)))
				msgs = append(msgs, tcMessage(i, i, tcUser, -30-int(i%60), "agent", ""))
			}
			if err := d.db.CreateInBatches(tasks, 500).Error; err != nil {
				t.Fatal(err)
			}
			if err := d.db.CreateInBatches(msgs, 500).Error; err != nil {
				t.Fatal(err)
			}
			if err := d.db.Exec("ANALYZE").Error; err != nil {
				t.Fatal(err)
			}
			var sent []string
			capture := func(tx *gorm.DB) {
				if tx.Statement.Table == "tasks" {
					sent = append(sent, tx.Dialector.Explain(tx.Statement.SQL.String(), tx.Statement.Vars...))
				}
			}
			if err := d.db.Callback().Query().After("gorm:query").Register("test:changes_plan", capture); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = d.db.Callback().Query().Remove("test:changes_plan") })
			if _, err := New(&mockDB{db: d.db}).ListTaskChanges(ctx, entity.ListTaskChangesRequest{Since: tcSince, Limit: 10}, tcUser, 0); err != nil {
				t.Fatal(err)
			}
			if len(sent) != 1 {
				t.Fatalf("sent %d statements to tasks, want 1", len(sent))
			}
			explain := "EXPLAIN "
			if d.name == "sqlite" {
				explain = "EXPLAIN QUERY PLAN "
			}
			plan := explainPlan(t, d.db, explain+sent[0])
			for _, index := range []string{"idx_tasks_user_id_updated_at", "idx_messages_user_id_created_at"} {
				if !strings.Contains(plan, index) {
					t.Errorf("the plan does not use %s:\n%s", index, plan)
				}
			}
		})
	}
}

// explainPlan runs an EXPLAIN and joins its steps, one a line: SQLite's
// detail, Postgres's line, each the row's last column.
func explainPlan(t *testing.T, db *gorm.DB, sql string) string {
	t.Helper()
	rows, err := db.Raw(sql).Rows()
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	var plan []string
	for rows.Next() {
		cells := make([]any, len(cols))
		for i := range cells {
			cells[i] = new(any)
		}
		if err := rows.Scan(cells...); err != nil {
			t.Fatal(err)
		}
		switch v := (*cells[len(cells)-1].(*any)).(type) {
		case string:
			plan = append(plan, v)
		case []byte:
			plan = append(plan, string(v))
		}
	}
	return strings.Join(plan, "\n")
}

// Each read reports the database's error rather than an empty answer.
func TestTaskChanges_DatabaseErrors(t *testing.T) {
	ctx := context.Background()
	d := historyDBs(t)[0]
	seedTaskChanges(t, d.db)
	repo := New(&mockDB{db: d.db})
	fail := func(t *testing.T, table string) {
		t.Helper()
		boom := errors.New("database unavailable")
		name := "test:fail_" + table
		if err := d.db.Callback().Query().Before("gorm:query").Register(name, func(tx *gorm.DB) {
			if tx.Statement.Table == table {
				_ = tx.AddError(boom)
			}
		}); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = d.db.Callback().Query().Remove(name) })
	}
	t.Run("tasks", func(t *testing.T) {
		fail(t, "tasks")
		if _, err := repo.ListTaskChanges(ctx, entity.ListTaskChangesRequest{Since: tcSince, Limit: 10}, tcUser, 0); err == nil {
			t.Error("ListTaskChanges hid the database's error")
		}
	})
	t.Run("transitions", func(t *testing.T) {
		fail(t, "task_state_transitions")
		if _, _, err := repo.ListStatusChanges(ctx, []int64{1}, tcSince, 0); err == nil {
			t.Error("ListStatusChanges hid the database's error")
		}
	})
	t.Run("messages", func(t *testing.T) {
		boom := errors.New("database unavailable")
		if err := d.db.Callback().Row().Before("gorm:row").Register("test:fail_count", func(tx *gorm.DB) { _ = tx.AddError(boom) }); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = d.db.Callback().Row().Remove("test:fail_count") })
		if _, err := repo.CountNewMessages(ctx, []int64{1}, tcSince, 0); err == nil {
			t.Error("CountNewMessages hid the database's error")
		}
	})
}

// The Postgres statements are picked on Postgres: SQLite answering to its
// name has no GREATEST, and reads the Postgres conversation filter's ->> as
// it would.
func TestTaskChanges_PostgresStatements(t *testing.T) {
	ctx := context.Background()
	db, repo := postgresNamed(t)
	seedTaskChanges(t, db)
	if _, err := repo.ListTaskChanges(ctx, entity.ListTaskChangesRequest{Since: tcSince, Limit: 10}, tcUser, 0); err == nil || !strings.Contains(err.Error(), "GREATEST") {
		t.Errorf("ListTaskChanges = %v, want SQLite to refuse GREATEST", err)
	}
	got, err := repo.CountNewMessages(ctx, []int64{2, 3}, tcSince, 0)
	if want := map[int64]int{2: 2}; err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("CountNewMessages = %v, %v; want %v", got, err, want)
	}
}
