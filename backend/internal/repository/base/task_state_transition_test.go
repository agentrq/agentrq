// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package base

import (
	"context"
	"errors"
	"testing"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func transitionsOf(t *testing.T, repo Repository, taskID int64) []model.TaskStateTransition {
	t.Helper()
	rows, err := repo.ListTaskStateTransitions(context.Background(), taskID)
	if err != nil {
		t.Fatalf("list transitions: %v", err)
	}
	return rows
}

func TestTaskStateTransitions_RecordedOnCreateAndStatusChange(t *testing.T) {
	db := deleteTaskDB(t)
	repo := New(&mockDB{db: db})
	ctx := context.Background()
	now := time.Now()

	task, err := repo.CreateTask(ctx, model.Task{ID: dtTaskID, CreatedAt: now, UpdatedAt: now, WorkspaceID: dtWorkspaceID, UserID: dtUserID, Status: "notstarted"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	task.Status = "ongoing"
	if task, err = repo.UpdateTask(ctx, task); err != nil {
		t.Fatalf("update: %v", err)
	}
	// A save that leaves the status alone is not a transition.
	task.Title = "renamed"
	if task, err = repo.UpdateTask(ctx, task); err != nil {
		t.Fatalf("update: %v", err)
	}
	task.Status = "blocked"
	if _, err = repo.UpdateTask(ctx, task); err != nil {
		t.Fatalf("update: %v", err)
	}

	rows := transitionsOf(t, repo, dtTaskID)
	want := [][2]model.TaskState{
		{model.TaskStateNone, model.TaskStateNotStarted},
		{model.TaskStateNotStarted, model.TaskStateOngoing},
		{model.TaskStateOngoing, model.TaskStateBlocked},
	}
	if len(rows) != len(want) {
		t.Fatalf("transitions = %+v, want %d", rows, len(want))
	}
	for i, w := range want {
		r := rows[i]
		if r.FromState != w[0] || r.ToState != w[1] {
			t.Errorf("transition %d = %v→%v, want %v→%v", i, r.FromState, r.ToState, w[0], w[1])
		}
		if r.TaskID != dtTaskID || r.WorkspaceID != dtWorkspaceID || r.UserID != dtUserID || r.CreatedAt.IsZero() {
			t.Errorf("transition %d = %+v, want it keyed to the task and dated", i, r)
		}
	}
}

// A change made under a registered agent records its IDs; one made without
// records none.
func TestTaskStateTransitions_RecordTheAgentIDs(t *testing.T) {
	db := deleteTaskDB(t)
	repo := New(&mockDB{db: db})
	now := time.Now()

	task, err := repo.CreateTask(context.Background(), model.Task{ID: dtTaskID, CreatedAt: now, UpdatedAt: now, WorkspaceID: dtWorkspaceID, UserID: dtUserID, Status: "notstarted"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	task.Status = "ongoing"
	ctx := entity.WithTaskAgent(context.Background(), entity.TaskAgent{Name: "gemini", ID: 7, ModelID: 9})
	if _, err = repo.UpdateTask(ctx, task); err != nil {
		t.Fatalf("update: %v", err)
	}

	rows := transitionsOf(t, repo, dtTaskID)
	if len(rows) != 2 {
		t.Fatalf("transitions = %+v, want the creation and the change to ongoing", rows)
	}
	if rows[0].AgentID != 0 || rows[0].AgentModelID != 0 {
		t.Errorf("creation = %+v, want no agent IDs", rows[0])
	}
	if rows[1].AgentID != 7 || rows[1].AgentModelID != 9 {
		t.Errorf("change to ongoing = %+v, want agent ID 7 and model ID 9", rows[1])
	}
}

// A name is stored once: storing it again leaves the first row as it was.
func TestAgentAndModelNameTables(t *testing.T) {
	db := deleteTaskDB(t)
	repo := New(&mockDB{db: db})
	ctx := context.Background()

	if err := repo.CreateAgents(ctx, []model.Agent{{ID: 1, Name: "claude-code"}, {ID: 2, Name: "gemini"}}); err != nil {
		t.Fatalf("create agents: %v", err)
	}
	if err := repo.CreateAgents(ctx, []model.Agent{{ID: 1, Name: "renamed"}}); err != nil {
		t.Fatalf("create agents again: %v", err)
	}
	for _, m := range []model.AgentModel{{ID: 3, Name: "Opus"}, {ID: 3, Name: "renamed"}} {
		if err := repo.CreateAgentModels(ctx, []model.AgentModel{m}); err != nil {
			t.Fatalf("create model: %v", err)
		}
	}

	all, err := repo.ListAgents(ctx, nil)
	if err != nil || len(all) != 2 {
		t.Fatalf("all agents = %+v, %v; want claude-code and gemini", all, err)
	}
	some, err := repo.ListAgents(ctx, []int64{1, 42})
	if err != nil || len(some) != 1 || some[0].Name != "claude-code" {
		t.Errorf("agents 1 and 42 = %+v, %v; want only agent 1, still named claude-code", some, err)
	}
	models, err := repo.ListAgentModels(ctx, nil)
	if err != nil || len(models) != 1 || models[0].Name != "Opus" {
		t.Errorf("all models = %+v, %v; want only Opus, not renamed", models, err)
	}
	if none, err := repo.ListAgentModels(ctx, []int64{42}); err != nil || len(none) != 0 {
		t.Errorf("model 42 = %+v, %v; want no rows", none, err)
	}
}

func TestTaskStateTransitions_RecordedWhenCreatedWithMessages(t *testing.T) {
	db := deleteTaskDB(t)
	repo := New(&mockDB{db: db})
	now := time.Now()

	task := model.Task{ID: dtTaskID, CreatedAt: now, UpdatedAt: now, WorkspaceID: dtWorkspaceID, UserID: dtUserID, Status: "ongoing"}
	if _, err := repo.CreateTaskWithMessages(context.Background(), task, nil); err != nil {
		t.Fatalf("create: %v", err)
	}
	rows := transitionsOf(t, repo, dtTaskID)
	if len(rows) != 1 || rows[0].FromState != model.TaskStateNone || rows[0].ToState != model.TaskStateOngoing {
		t.Fatalf("transitions = %+v, want one none→ongoing", rows)
	}
}

// Save upserts, so an UpdateTask of a task that is not there yet creates it,
// and that is its first state.
func TestTaskStateTransitions_UpdateThatInserts(t *testing.T) {
	db := deleteTaskDB(t)
	repo := New(&mockDB{db: db})
	now := time.Now()

	task := model.Task{ID: dtTaskID, CreatedAt: now, UpdatedAt: now, WorkspaceID: dtWorkspaceID, UserID: dtUserID, Status: "cron"}
	if _, err := repo.UpdateTask(context.Background(), task); err != nil {
		t.Fatalf("update: %v", err)
	}
	rows := transitionsOf(t, repo, dtTaskID)
	if len(rows) != 1 || rows[0].FromState != model.TaskStateNone || rows[0].ToState != model.TaskStateCron {
		t.Fatalf("transitions = %+v, want one none→cron", rows)
	}
}

func TestTaskStateTransitions_MoveTakesHistoryAlong(t *testing.T) {
	db := deleteTaskDB(t)
	repo := New(&mockDB{db: db})
	ctx := context.Background()
	now := time.Now()
	const otherWorkspace = int64(7)

	task, err := repo.CreateTask(ctx, model.Task{ID: dtTaskID, CreatedAt: now, UpdatedAt: now, WorkspaceID: dtWorkspaceID, UserID: dtUserID, Status: "notstarted"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	task.WorkspaceID = otherWorkspace
	if _, err := repo.UpdateTask(ctx, task); err != nil {
		t.Fatalf("move: %v", err)
	}
	rows := transitionsOf(t, repo, dtTaskID)
	if len(rows) != 1 || rows[0].WorkspaceID != otherWorkspace {
		t.Fatalf("transitions = %+v, want the one row moved to workspace %d", rows, otherWorkspace)
	}
}

func TestTaskStateTransitions_GoneWithTheirTaskAndWorkspace(t *testing.T) {
	for _, tc := range []struct {
		name   string
		delete func(Repository) error
	}{
		{"task", func(r Repository) error { return r.DeleteTask(context.Background(), dtWorkspaceID, dtTaskID, dtUserID) }},
		{"workspace", func(r Repository) error { return r.DeleteWorkspace(context.Background(), dtWorkspaceID, dtUserID) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := deleteTaskDB(t)
			if err := db.AutoMigrate(&model.Workspace{}, &model.Skill{}, &model.SkillFile{}, &model.WorkspaceSkill{}, &model.SiteShare{}, &model.ForkFolder{}); err != nil {
				t.Fatalf("migrate: %v", err)
			}
			if err := db.Create(&model.Workspace{ID: dtWorkspaceID, UserID: dtUserID, Name: "w"}).Error; err != nil {
				t.Fatalf("seed workspace: %v", err)
			}
			repo := New(&mockDB{db: db})
			now := time.Now()
			if _, err := repo.CreateTask(context.Background(), model.Task{ID: dtTaskID, CreatedAt: now, UpdatedAt: now, WorkspaceID: dtWorkspaceID, UserID: dtUserID, Status: "notstarted"}); err != nil {
				t.Fatalf("create: %v", err)
			}
			if err := tc.delete(repo); err != nil {
				t.Fatalf("delete: %v", err)
			}
			if rows := transitionsOf(t, repo, dtTaskID); len(rows) != 0 {
				t.Fatalf("transitions = %+v, want none left", rows)
			}
		})
	}
}

// A failed history write must not leave a status change behind without it.
func TestTaskStateTransitions_WriteFailureRollsBack(t *testing.T) {
	db := deleteTaskDB(t)
	repo := New(&mockDB{db: db})
	ctx := context.Background()
	now := time.Now()

	task, err := repo.CreateTask(ctx, model.Task{ID: dtTaskID, CreatedAt: now, UpdatedAt: now, WorkspaceID: dtWorkspaceID, UserID: dtUserID, Status: "notstarted"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := db.Migrator().DropTable(&model.TaskStateTransition{}); err != nil {
		t.Fatalf("drop: %v", err)
	}

	task.Status = "ongoing"
	if _, err := repo.UpdateTask(ctx, task); err == nil {
		t.Fatal("update: want the history write's error")
	}
	var got model.Task
	if err := db.First(&got, dtTaskID).Error; err != nil || got.Status != "notstarted" {
		t.Fatalf("status = %q, %v; want the change rolled back", got.Status, err)
	}

	if _, err := repo.CreateTask(ctx, model.Task{ID: dtTaskID + 1, WorkspaceID: dtWorkspaceID, UserID: dtUserID, Status: "notstarted"}); err == nil {
		t.Fatal("create: want the history write's error")
	}
	if _, err := repo.CreateTaskWithMessages(ctx, model.Task{ID: dtTaskID + 2, WorkspaceID: dtWorkspaceID, UserID: dtUserID, Status: "notstarted"}, nil); err == nil {
		t.Fatal("create with messages: want the history write's error")
	}
	if err := db.First(&model.Task{}, dtTaskID+1).Error; err == nil {
		t.Fatal("a task was created without its history")
	}
}

func TestUpdateTask_LookupFailure(t *testing.T) {
	db := deleteTaskDB(t)
	repo := New(&mockDB{db: db})
	if err := db.Migrator().DropTable(&model.Message{}, &model.ToolCall{}, &model.Task{}); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if _, err := repo.UpdateTask(context.Background(), model.Task{ID: dtTaskID, Status: "ongoing"}); err == nil {
		t.Fatal("update: want the lookup's error")
	}
}

func TestTaskState_RoundTrip(t *testing.T) {
	for _, status := range []string{"notstarted", "ongoing", "blocked", "completed", "rejected", "cron"} {
		if got := model.TaskStateFromStatus(status).String(); got != status {
			t.Errorf("%q round-trips to %q", status, got)
		}
	}
	if got := model.TaskStateFromStatus("archived"); got != model.TaskStateNone {
		t.Errorf("an unknown status is %v, want none", got)
	}
	if got := model.TaskStateNone.String(); got != "" {
		t.Errorf("none is %q, want empty", got)
	}
}

// failOn makes every statement of one kind against one table fail.
func failOn(t *testing.T, db *gorm.DB, kind, table string) {
	t.Helper()
	fail := func(tx *gorm.DB) {
		if tx.Statement.Table == table {
			_ = tx.AddError(errInjected)
		}
	}
	var err error
	switch kind {
	case "create":
		err = db.Callback().Create().Before("gorm:create").Register("test:fail-"+table, fail)
	case "update":
		err = db.Callback().Update().Before("gorm:update").Register("test:fail-"+table, fail)
	case "delete":
		err = db.Callback().Delete().Before("gorm:delete").Register("test:fail-"+table, fail)
	case "query":
		err = db.Callback().Query().Before("gorm:query").Register("test:fail-"+table, fail)
	case "row":
		err = db.Callback().Row().Before("gorm:row").Register("test:fail-row-"+table, fail)
	}
	if err != nil {
		t.Fatalf("register: %v", err)
	}
}

func TestTaskStateTransitions_StatementFailures(t *testing.T) {
	now := time.Now()
	seed := func(t *testing.T, repo Repository) model.Task {
		t.Helper()
		task, err := repo.CreateTask(context.Background(), model.Task{ID: dtTaskID, CreatedAt: now, UpdatedAt: now, WorkspaceID: dtWorkspaceID, UserID: dtUserID, Status: "notstarted"})
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		return task
	}
	for _, tc := range []struct {
		name        string
		kind, table string
		run         func(Repository, model.Task) error
	}{
		{"create task", "create", "tasks", func(r Repository, _ model.Task) error {
			_, err := r.CreateTask(context.Background(), model.Task{ID: dtTaskID + 1, WorkspaceID: dtWorkspaceID, UserID: dtUserID})
			return err
		}},
		{"save task", "update", "tasks", func(r Repository, task model.Task) error {
			task.Status = "ongoing"
			_, err := r.UpdateTask(context.Background(), task)
			return err
		}},
		{"move history", "update", "task_state_transitions", func(r Repository, task model.Task) error {
			task.WorkspaceID = 7
			_, err := r.UpdateTask(context.Background(), task)
			return err
		}},
		{"delete task's history", "delete", "task_state_transitions", func(r Repository, _ model.Task) error {
			return r.DeleteTask(context.Background(), dtWorkspaceID, dtTaskID, dtUserID)
		}},
		{"delete workspace's history", "delete", "task_state_transitions", func(r Repository, _ model.Task) error {
			return r.DeleteWorkspace(context.Background(), dtWorkspaceID, dtUserID)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := deleteTaskDB(t)
			repo := New(&mockDB{db: db})
			task := seed(t, repo)
			failOn(t, db, tc.kind, tc.table)
			if err := tc.run(repo, task); !errors.Is(err, errInjected) {
				t.Fatalf("got %v, want the injected failure", err)
			}
			if rows := transitionsOf(t, repo, dtTaskID); len(rows) != 1 || rows[0].WorkspaceID != dtWorkspaceID {
				t.Fatalf("transitions = %+v, want the seeded one untouched", rows)
			}
		})
	}
}

func statesOf(t *testing.T, repo Repository, taskID int64) []model.TaskState {
	t.Helper()
	var out []model.TaskState
	for _, r := range transitionsOf(t, repo, taskID) {
		out = append(out, r.ToState)
	}
	return out
}

func sameStates(a, b []model.TaskState) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestTaskStateTransitions_NeedsInput(t *testing.T) {
	db := deleteTaskDB(t)
	repo := New(&mockDB{db: db})
	ctx := context.Background()
	now := time.Now()

	task, err := repo.CreateTask(ctx, model.Task{ID: dtTaskID, CreatedAt: now, UpdatedAt: now, WorkspaceID: dtWorkspaceID, UserID: dtUserID, Status: "ongoing"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	msg := func(id int64, metadata string) {
		t.Helper()
		m := model.Message{ID: id, CreatedAt: now, TaskID: dtTaskID, UserID: dtUserID, Sender: "agent", Text: "x"}
		if metadata != "" {
			m.Metadata = []byte(metadata)
		}
		if err := repo.CreateMessage(ctx, m); err != nil {
			t.Fatalf("message %d: %v", id, err)
		}
	}
	meta := func(id int64, metadata string) {
		t.Helper()
		if err := repo.UpdateMessageMetadata(ctx, dtTaskID, id, []byte(metadata)); err != nil {
			t.Fatalf("metadata %d: %v", id, err)
		}
	}
	want := func(states ...model.TaskState) {
		t.Helper()
		if got := statesOf(t, repo, dtTaskID); !sameStates(got, states) {
			t.Fatalf("states = %v, want %v", got, states)
		}
	}
	const (
		ongoing = model.TaskStateOngoing
		needs   = model.TaskStateNeedsInput
		blocked = model.TaskStateBlocked
		done    = model.TaskStateCompleted
	)

	// A plain reply, and a plan whose entries are pending, ask nobody anything.
	msg(1, "")
	msg(2, `{"type":"plan","entries":[{"content":"a","status":"pending"}]}`)
	meta(2, `{"type":"plan","entries":[{"content":"a","status":"completed"}]}`)
	want(ongoing)

	// Two questions open: the task needs input from the first until the last is answered.
	msg(3, `{"type":"permission_request","status":"pending"}`)
	msg(4, `{"type":"elicitation_request","status":"pending"}`)
	want(ongoing, needs)
	meta(3, `{"type":"permission_request","status":"allow"}`)
	want(ongoing, needs)
	meta(4, `{"type":"elicitation_request","status":"cancelled"}`)
	want(ongoing, needs, ongoing)

	var stored model.Task
	if err := db.First(&stored, dtTaskID).Error; err != nil || stored.Status != "ongoing" {
		t.Fatalf("status = %q, %v; needing input must not change it", stored.Status, err)
	}

	// A real status change ends the wait, from needs-input.
	msg(5, `{"type":"permission_request","status":"pending"}`)
	task.Status = "blocked"
	if task, err = repo.UpdateTask(ctx, task); err != nil {
		t.Fatalf("update: %v", err)
	}
	want(ongoing, needs, ongoing, needs, blocked)
	rows := transitionsOf(t, repo, dtTaskID)
	if rows[4].FromState != needs {
		t.Fatalf("last transition from %v, want from needs-input", rows[4].FromState)
	}
	// Answering it now changes nothing: the task already left needs-input.
	meta(5, `{"type":"permission_request","status":"deny"}`)
	want(ongoing, needs, ongoing, needs, blocked)

	// A closed task is not waiting on anyone, whatever is left in its thread.
	task.Status = "completed"
	if _, err = repo.UpdateTask(ctx, task); err != nil {
		t.Fatalf("update: %v", err)
	}
	msg(6, `{"type":"permission_request","status":"pending"}`)
	want(ongoing, needs, ongoing, needs, blocked, done)
}

// A message for a task that is not there has no history to add to.
func TestSyncNeedsInput_NoTask(t *testing.T) {
	db := deleteTaskDB(t)
	if err := db.Exec("PRAGMA foreign_keys = OFF").Error; err != nil {
		t.Fatalf("pragma: %v", err)
	}
	repo := New(&mockDB{db: db})
	if err := repo.CreateMessage(context.Background(), model.Message{ID: 1, TaskID: 42, Metadata: []byte(`{"status":"pending"}`)}); err != nil {
		t.Fatalf("message: %v", err)
	}
	if rows := transitionsOf(t, repo, 42); len(rows) != 0 {
		t.Fatalf("transitions = %+v, want none", rows)
	}
}

// A task from before the history began needs input from the state its status names.
func TestSyncNeedsInput_TaskWithoutHistory(t *testing.T) {
	db := deleteTaskDB(t)
	repo := New(&mockDB{db: db})
	if err := db.Create(&model.Task{ID: dtTaskID, WorkspaceID: dtWorkspaceID, UserID: dtUserID, Status: "ongoing"}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := repo.CreateMessage(context.Background(), model.Message{ID: 1, TaskID: dtTaskID, Metadata: []byte(`{"status":"pending"}`)}); err != nil {
		t.Fatalf("message: %v", err)
	}
	rows := transitionsOf(t, repo, dtTaskID)
	if len(rows) != 1 || rows[0].FromState != model.TaskStateOngoing || rows[0].ToState != model.TaskStateNeedsInput {
		t.Fatalf("transitions = %+v, want one ongoing→needsinput", rows)
	}
}

func TestSyncNeedsInput_StatementFailures(t *testing.T) {
	for _, tc := range []struct {
		name string
		fail func(*testing.T, *gorm.DB)
		run  func(Repository) error
	}{
		{"message insert", func(t *testing.T, db *gorm.DB) { failOn(t, db, "create", "messages") }, func(r Repository) error {
			return r.CreateMessage(context.Background(), model.Message{ID: 9, TaskID: dtTaskID, Metadata: []byte(`{"status":"pending"}`)})
		}},
		{"metadata update", func(t *testing.T, db *gorm.DB) { failOn(t, db, "update", "messages") }, func(r Repository) error {
			return r.UpdateMessageMetadata(context.Background(), dtTaskID, 1, []byte(`{"status":"allow"}`))
		}},
		{"history write", func(t *testing.T, db *gorm.DB) { failOn(t, db, "create", "task_state_transitions") }, func(r Repository) error {
			return r.CreateMessage(context.Background(), model.Message{ID: 9, TaskID: dtTaskID, Metadata: []byte(`{"status":"pending"}`)})
		}},
		{"task lookup", func(t *testing.T, db *gorm.DB) { failQuery(t, db, "tasks") }, func(r Repository) error {
			return r.CreateMessage(context.Background(), model.Message{ID: 9, TaskID: dtTaskID, Metadata: []byte(`{"status":"pending"}`)})
		}},
		{"history lookup", func(t *testing.T, db *gorm.DB) { failQuery(t, db, "task_state_transitions") }, func(r Repository) error {
			return r.CreateMessage(context.Background(), model.Message{ID: 9, TaskID: dtTaskID, Metadata: []byte(`{"status":"pending"}`)})
		}},
		{"pending count", func(t *testing.T, db *gorm.DB) { failQuery(t, db, "messages") }, func(r Repository) error {
			return r.CreateMessage(context.Background(), model.Message{ID: 9, TaskID: dtTaskID, Metadata: []byte(`{"status":"pending"}`)})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := deleteTaskDB(t)
			repo := New(&mockDB{db: db})
			if err := db.Create(&model.Task{ID: dtTaskID, WorkspaceID: dtWorkspaceID, UserID: dtUserID, Status: "ongoing"}).Error; err != nil {
				t.Fatalf("seed: %v", err)
			}
			if err := db.Create(&model.Message{ID: 1, TaskID: dtTaskID, Metadata: []byte(`{"status":"pending"}`)}).Error; err != nil {
				t.Fatalf("seed message: %v", err)
			}
			tc.fail(t, db)
			if err := tc.run(repo); !errors.Is(err, errInjected) {
				t.Fatalf("got %v, want the injected failure", err)
			}
		})
	}
}

// A task status change reads the history's current state, and fails with it.
func TestUpdateTask_HistoryLookupFailure(t *testing.T) {
	db := deleteTaskDB(t)
	repo := New(&mockDB{db: db})
	task, err := repo.CreateTask(context.Background(), model.Task{ID: dtTaskID, WorkspaceID: dtWorkspaceID, UserID: dtUserID, Status: "notstarted"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	failQuery(t, db, "task_state_transitions")
	task.Status = "ongoing"
	if _, err := repo.UpdateTask(context.Background(), task); !errors.Is(err, errInjected) {
		t.Fatalf("got %v, want the injected failure", err)
	}
}

func failQuery(t *testing.T, db *gorm.DB, table string) {
	t.Helper()
	if err := db.Callback().Query().Before("gorm:query").Register("test:fail-query-"+table, func(tx *gorm.DB) {
		if tx.Statement.Table == table {
			_ = tx.AddError(errInjected)
		}
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
}

func TestHasRequestStatus(t *testing.T) {
	for in, want := range map[string]bool{
		``:                           false,
		`not json`:                   false,
		`{"type":"plan"}`:            false,
		`{"status":"pending"}`:       true,
		`{"status":"allow"}`:         true,
		`{"entries":[{"status":1}]}`: false,
	} {
		if got := hasRequestStatus([]byte(in)); got != want {
			t.Errorf("hasRequestStatus(%q) = %v, want %v", in, got, want)
		}
	}
}

// Postgres stores metadata as jsonb, where ->> reads the top-level status; the
// SQLite form is exercised by every other test here.
func TestPendingRequestClause_Postgres(t *testing.T) {
	db, err := gorm.Open(postgres.New(postgres.Config{DSN: "host=127.0.0.1 port=1"}), &gorm.Config{DryRun: true, DisableAutomaticPing: true})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if got := pendingRequestClause(db); got != "metadata->>'status' = 'pending'" {
		t.Fatalf("clause = %q", got)
	}
}
