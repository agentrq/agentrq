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

	"gorm.io/gorm"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
)

// tcSelf is the supervisor workspace, whose own writes excludeSelf leaves out.
const tcSelf = tcDelegator

func tcBy(m model.Message, workspace int64) model.Message {
	m.ByWorkspaceID = workspace
	return m
}

func tcMove(id, task int64, from, to model.TaskState, at int, by int64) model.TaskStateTransition {
	return model.TaskStateTransition{ID: id, UserID: tcUser, WorkspaceID: tcWorkspace, TaskID: task, FromState: from, ToState: to, CreatedAt: tcAt(at), ByWorkspaceID: by}
}

// seedSelfChanges writes, for tcUser, tasks every one of which changed after
// since by its row; by the others, only:
//
//	20  the supervisor replied at +5: no
//	21  the supervisor replied at +5, then the human at +8
//	22  the supervisor completed it at +6: no
//	23  its agent completed it at +7, though the row was written at +30
//	24  its agent replied at +9, and the supervisor at +12
//	25  the human wrote before since, the supervisor after: no
//	26  another account's, the human's at +4: no
func seedSelfChanges(t *testing.T, db *gorm.DB) {
	t.Helper()
	tasks := []model.Task{tcTask(20, tcUser, tcWorkspace, 5), tcTask(21, tcUser, tcWorkspace, 8), tcTask(22, tcUser, tcWorkspace, 6),
		tcTask(23, tcUser, tcWorkspace, 30), tcTask(24, tcUser, tcWorkspace, 12), tcTask(25, tcUser, tcWorkspace, 5), tcTask(26, tcOther, tcWorkspace, 4)}
	msgs := []model.Message{
		tcBy(tcMessage(201, 20, tcUser, 5, "human", ""), tcSelf),
		tcBy(tcMessage(211, 21, tcUser, 5, "human", ""), tcSelf),
		tcMessage(212, 21, tcUser, 8, "human", ""),
		tcBy(tcMessage(241, 24, tcUser, 9, "agent", ""), tcWorkspace),
		tcBy(tcMessage(242, 24, tcUser, 12, "human", ""), tcSelf),
		// The agent's thought is no message of the conversation.
		tcBy(tcMessage(243, 24, tcUser, 13, "agent", "agent_thought"), tcWorkspace),
		tcMessage(251, 25, tcUser, -5, "human", ""),
		tcBy(tcMessage(252, 25, tcUser, 5, "human", ""), tcSelf),
		tcMessage(261, 26, tcOther, 4, "human", ""),
	}
	moves := []model.TaskStateTransition{
		tcMove(1, 22, model.TaskStateNone, model.TaskStateOngoing, -30, 0),
		tcMove(2, 22, model.TaskStateOngoing, model.TaskStateCompleted, 6, tcSelf),
		tcMove(3, 23, model.TaskStateNone, model.TaskStateOngoing, -30, 0),
		tcMove(4, 23, model.TaskStateOngoing, model.TaskStateCompleted, 7, tcWorkspace),
	}
	for _, rows := range []any{&tasks, &msgs, &moves} {
		if err := db.Create(rows).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
}

func TestListTaskChanges_ExcludeSelf(t *testing.T) {
	ctx := context.Background()
	for _, d := range historyDBs(t) {
		t.Run(d.name, func(t *testing.T) {
			seedSelfChanges(t, d.db)
			repo := New(&mockDB{db: d.db})
			list := func(limit int) []int64 {
				t.Helper()
				tasks, err := repo.ListTaskChanges(ctx, entity.ListTaskChangesRequest{Since: tcSince, Limit: limit}, tcUser, tcSelf)
				if err != nil {
					t.Fatal(err)
				}
				return taskIDs(tasks)
			}
			// In the order the others changed them: 23 at +7, not +30.
			if got, want := list(10), []int64{23, 21, 24}; !reflect.DeepEqual(got, want) {
				t.Errorf("changed by the others = %v, want %v", got, want)
			}
			if got, want := list(2), []int64{23, 21}; !reflect.DeepEqual(got, want) {
				t.Errorf("limit 2 = %v, want %v", got, want)
			}
			// Without excludeSelf, every one of them.
			all, err := repo.ListTaskChanges(ctx, entity.ListTaskChangesRequest{Since: tcSince, Limit: 10}, tcUser, 0)
			if got, want := taskIDs(all), []int64{20, 25, 22, 21, 24, 23}; err != nil || !reflect.DeepEqual(got, want) {
				t.Errorf("every change = %v, %v; want %v", got, err, want)
			}

			ids := []int64{20, 21, 22, 23, 24, 25}
			counts, err := repo.CountNewMessages(ctx, ids, tcSince, tcSelf)
			if want := map[int64]int{21: 1, 24: 1}; err != nil || !reflect.DeepEqual(counts, want) {
				t.Errorf("the others' new messages = %v, %v; want %v", counts, err, want)
			}
			newest, err := repo.ListNewestMessages(ctx, ids, tcSince, tcSelf)
			if err != nil {
				t.Fatal(err)
			}
			if len(newest) != 2 || newest[21].ID != 212 || newest[24].ID != 241 {
				t.Errorf("the others' newest messages = %+v, want 212 and 241", newest)
			}
			changed, moved, err := repo.ListStatusChanges(ctx, ids, tcSince, tcSelf)
			if err != nil {
				t.Fatal(err)
			}
			if len(changed) != 1 || !changed[23].Equal(tcAt(7)) || len(moved) != 1 || !moved[23].Equal(tcAt(7)) {
				t.Errorf("the others' status changes = %v, moves %v; want 23's at +7 only", changed, moved)
			}
			// Without excludeSelf, the supervisor's completion counts.
			if changed, _, err := repo.ListStatusChanges(ctx, ids, tcSince, 0); err != nil || len(changed) != 2 || !changed[22].Equal(tcAt(6)) {
				t.Errorf("every status change = %v, %v; want 22's and 23's", changed, err)
			}
		})
	}
}

// A move into a wait on the human is a change by whoever made it, though no
// change of status: the list orders it, and moved says when.
func TestListStatusChanges_ExcludeSelfMoves(t *testing.T) {
	ctx := context.Background()
	for _, d := range historyDBs(t) {
		t.Run(d.name, func(t *testing.T) {
			seedSelfChanges(t, d.db)
			if err := d.db.Create([]model.TaskStateTransition{
				tcMove(5, 20, model.TaskStateNone, model.TaskStateOngoing, -30, 0),
				tcMove(6, 20, model.TaskStateOngoing, model.TaskStateNeedsInput, 3, tcWorkspace),
			}).Error; err != nil {
				t.Fatal(err)
			}
			repo := New(&mockDB{db: d.db})
			changed, moved, err := repo.ListStatusChanges(ctx, []int64{20}, tcSince, tcSelf)
			if err != nil || len(changed) != 0 || !moved[20].Equal(tcAt(3)) {
				t.Errorf("ListStatusChanges = %v, %v, %v; want no change, a move at +3", changed, moved, err)
			}
			tasks, err := repo.ListTaskChanges(ctx, entity.ListTaskChangesRequest{Since: tcSince, Limit: 10}, tcUser, tcSelf)
			if got, want := taskIDs(tasks), []int64{20, 23, 21, 24}; err != nil || !reflect.DeepEqual(got, want) {
				t.Errorf("changed by the others = %v, %v; want %v", got, err, want)
			}
		})
	}
}

// Leaving out one workspace's writes reads each half by its index too.
func TestListTaskChanges_ExcludeSelfUsesTheIndexes(t *testing.T) {
	ctx := context.Background()
	for _, d := range historyDBs(t) {
		t.Run(d.name, func(t *testing.T) {
			var tasks []model.Task
			var msgs []model.Message
			var moves []model.TaskStateTransition
			for i := int64(1); i <= 3000; i++ {
				tasks = append(tasks, tcTask(i, tcUser, tcWorkspace, -30-int(i%60)))
				msgs = append(msgs, tcMessage(i, i, tcUser, -30-int(i%60), "agent", ""))
				moves = append(moves, tcMove(i, i, model.TaskStateNone, model.TaskStateOngoing, -30-int(i%60), 0))
			}
			for _, rows := range []any{tasks, msgs, moves} {
				if err := d.db.CreateInBatches(rows, 500).Error; err != nil {
					t.Fatal(err)
				}
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
			if err := d.db.Callback().Query().After("gorm:query").Register("test:self_plan", capture); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = d.db.Callback().Query().Remove("test:self_plan") })
			if _, err := New(&mockDB{db: d.db}).ListTaskChanges(ctx, entity.ListTaskChangesRequest{Since: tcSince, Limit: 10}, tcUser, tcSelf); err != nil {
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
			for _, index := range []string{"idx_messages_user_id_created_at", "idx_task_state_transitions_user_id_created_at"} {
				if !strings.Contains(plan, index) {
					t.Errorf("the plan does not use %s:\n%s", index, plan)
				}
			}
		})
	}
}

func TestListNewestMessages_DatabaseError(t *testing.T) {
	d := historyDBs(t)[0]
	boom := errors.New("database unavailable")
	if err := d.db.Callback().Query().Before("gorm:query").Register("test:fail_newest", func(tx *gorm.DB) { _ = tx.AddError(boom) }); err != nil {
		t.Fatal(err)
	}
	if _, err := New(&mockDB{db: d.db}).ListNewestMessages(context.Background(), []int64{1}, tcSince, tcSelf); !errors.Is(err, boom) {
		t.Errorf("ListNewestMessages = %v, want the database's error", err)
	}
}

// The Postgres statements are picked on Postgres, as without excludeSelf.
func TestTaskChanges_ExcludeSelfPostgresStatements(t *testing.T) {
	ctx := context.Background()
	db, repo := postgresNamed(t)
	seedSelfChanges(t, db)
	if _, err := repo.ListTaskChanges(ctx, entity.ListTaskChangesRequest{Since: tcSince, Limit: 10}, tcUser, tcSelf); err == nil || !strings.Contains(err.Error(), "GREATEST") {
		t.Errorf("ListTaskChanges = %v, want SQLite to refuse GREATEST", err)
	}
	newest, err := repo.ListNewestMessages(ctx, []int64{21, 24}, tcSince, tcSelf)
	if err != nil || len(newest) != 2 || newest[21].ID != 212 {
		t.Errorf("ListNewestMessages = %+v, %v; want 212 and 241", newest, err)
	}
}

// What an agent writes is put down to the workspace the context names.
func TestByWorkspaceIsStamped(t *testing.T) {
	d := historyDBs(t)[0]
	repo := New(&mockDB{db: d.db})
	ctx := entity.WithAgentWorkspace(context.Background(), tcWorkspace)
	task, err := repo.CreateTask(ctx, tcTask(1, tcUser, tcWorkspace, 0))
	if err != nil {
		t.Fatal(err)
	}

	if err := repo.CreateMessage(ctx, model.Message{ID: 1, TaskID: 1, UserID: tcUser, Sender: "agent"}); err != nil {
		t.Fatal(err)
	}
	// A message that names its workspace keeps it, and a person's names none.
	if err := repo.CreateMessage(ctx, model.Message{ID: 2, TaskID: 1, UserID: tcUser, ByWorkspaceID: tcSelf}); err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateMessage(context.Background(), model.Message{ID: 3, TaskID: 1, UserID: tcUser, Sender: "human"}); err != nil {
		t.Fatal(err)
	}
	var msgs []model.Message
	d.db.Order("id").Find(&msgs)
	if got := []int64{msgs[0].ByWorkspaceID, msgs[1].ByWorkspaceID, msgs[2].ByWorkspaceID}; !reflect.DeepEqual(got, []int64{tcWorkspace, tcSelf, 0}) {
		t.Errorf("messages put down to %v, want %v", got, []int64{tcWorkspace, tcSelf, 0})
	}

	task.Status = "completed"
	if _, err := repo.UpdateTask(context.Background(), task, "Status"); err != nil {
		t.Fatal(err)
	}
	var moves []model.TaskStateTransition
	d.db.Order("id").Find(&moves)
	if len(moves) != 2 || moves[0].ByWorkspaceID != tcWorkspace || moves[1].ByWorkspaceID != 0 {
		t.Errorf("status changes = %+v, want the agent's put down to %d, the person's to none", moves, tcWorkspace)
	}
}
