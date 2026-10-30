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
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	crudUser  = int64(41)
	crudOther = int64(42)
	crudWS    = int64(4100)
)

var crudTables = []any{
	&model.Workspace{}, &model.Task{}, &model.Message{}, &model.ToolCall{}, &model.TaskPlan{},
	&model.TaskStateTransition{}, &model.TaskLatency{}, &model.PanoDocTask{}, &model.User{},
	&model.SlackWorkspaceLink{}, &model.SlackTaskThread{}, &model.PushSubscription{},
	&model.Event{}, &model.EventTrigger{}, &model.Workflow{}, &model.WorkflowStep{},
	&model.Telemetry{}, &model.Memory{},
}

// crudDB is a migrated in-memory database on one connection, so every
// statement sees the same tables.
func crudDB(t *testing.T) (*gorm.DB, Repository) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(crudTables...); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db, New(&mockDB{db: db})
}

func mustCreate(t *testing.T, db *gorm.DB, rows ...any) {
	t.Helper()
	for _, r := range rows {
		if err := db.Create(r).Error; err != nil {
			t.Fatalf("seed %T: %v", r, err)
		}
	}
}

// failAll makes every statement run on db fail.
func failAll(t *testing.T, db *gorm.DB) {
	t.Helper()
	fail := func(tx *gorm.DB) { _ = tx.AddError(errInjected) }
	cb := db.Callback()
	for _, err := range []error{
		cb.Create().Before("gorm:create").Register("test:fail-all", fail),
		cb.Query().Before("gorm:query").Register("test:fail-all", fail),
		cb.Update().Before("gorm:update").Register("test:fail-all", fail),
		cb.Delete().Before("gorm:delete").Register("test:fail-all", fail),
		cb.Row().Before("gorm:row").Register("test:fail-all", fail),
		cb.Raw().Before("gorm:raw").Register("test:fail-all", fail),
	} {
		if err != nil {
			t.Fatalf("register: %v", err)
		}
	}
}

// pgNamed answers to Postgres's name while running on SQLite, which takes a
// repository down the branches it writes for Postgres. SQLite cannot run their
// Postgres-only SQL, so each such statement fails.
type pgNamed struct{ gorm.Dialector }

func (pgNamed) Name() string { return "postgres" }

func postgresNamed(t *testing.T) (*gorm.DB, Repository) {
	t.Helper()
	db, repo := crudDB(t)
	db.Dialector = pgNamed{db.Dialector}
	return db, repo
}

func wantInjected(t *testing.T, what string, err error) {
	t.Helper()
	if !errors.Is(err, errInjected) {
		t.Errorf("%s: err = %v, want the injected failure", what, err)
	}
}

func TestRepository_WorkspaceLookups(t *testing.T) {
	ctx := context.Background()
	db, repo := crudDB(t)
	now := time.Now()
	archived := now.Add(-time.Hour)
	mustCreate(t, db,
		&model.Workspace{ID: crudWS, UserID: crudUser, Name: "live", CreatedAt: now},
		&model.Workspace{ID: crudWS + 1, UserID: crudUser, Name: "old", CreatedAt: now.Add(-time.Minute), ArchivedAt: &archived},
		&model.Workspace{ID: crudWS + 2, UserID: crudOther, Name: "theirs", CreatedAt: now},
	)

	if _, err := repo.GetWorkspace(ctx, crudWS+2, crudUser); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetWorkspace of another user's workspace = %v, want ErrNotFound", err)
	}
	if ok, err := repo.CheckWorkspaceAccess(ctx, crudWS, crudUser); err != nil || !ok {
		t.Errorf("CheckWorkspaceAccess(own) = %v, %v; want true", ok, err)
	}
	if ok, err := repo.CheckWorkspaceAccess(ctx, crudWS+2, crudUser); err != nil || ok {
		t.Errorf("CheckWorkspaceAccess(theirs) = %v, %v; want false", ok, err)
	}
	live, err := repo.ListWorkspaces(ctx, crudUser, false)
	if err != nil || len(live) != 1 || live[0].ID != crudWS {
		t.Errorf("ListWorkspaces(live) = %v, %v; want only %d", live, err, crudWS)
	}
	all, err := repo.ListWorkspaces(ctx, crudUser, true)
	if err != nil || len(all) != 2 || all[0].ID != crudWS || all[1].ID != crudWS+1 {
		t.Errorf("ListWorkspaces(all) = %v, %v; want both, newest first", all, err)
	}
	if w, err := repo.SystemGetWorkspace(ctx, crudWS+2); err != nil || w.Name != "theirs" {
		t.Errorf("SystemGetWorkspace = %v, %v", w, err)
	}
	if _, err := repo.SystemGetWorkspace(ctx, 1); !errors.Is(err, ErrNotFound) {
		t.Errorf("SystemGetWorkspace(missing) = %v, want ErrNotFound", err)
	}
}

func TestRepository_ListAndCountTasks_Filters(t *testing.T) {
	ctx := context.Background()
	db, repo := crudDB(t)
	now := time.Now()
	mustCreate(t, db,
		&model.Task{ID: 1, WorkspaceID: crudWS, UserID: crudUser, Status: "ongoing", CreatedBy: "human", CreatedAt: now, UpdatedAt: now},
		&model.Task{ID: 2, WorkspaceID: crudWS, UserID: crudUser, Status: "blocked", CreatedBy: "agent", CreatedAt: now, UpdatedAt: now.Add(time.Second)},
		&model.Task{ID: 3, WorkspaceID: crudWS, UserID: crudUser, Status: "notstarted", CreatedBy: "human", CreatedAt: now, UpdatedAt: now.Add(2 * time.Second)},
		&model.Task{ID: 4, WorkspaceID: crudWS, UserID: crudUser, Status: "completed", CreatedBy: "human", CreatedAt: now, UpdatedAt: now},
	)

	ids := func(ts []model.Task) []int64 {
		out := make([]int64, len(ts))
		for i, t := range ts {
			out[i] = t.ID
		}
		return out
	}
	// Mixed statuses list ongoing before blocked before notstarted.
	got, err := repo.ListTasks(ctx, entity.ListTasksRequest{WorkspaceID: crudWS, Status: []string{"notstarted", "blocked", "ongoing"}, Limit: 1000}, crudUser)
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if g := ids(got); len(g) != 3 || g[0] != 1 || g[1] != 2 || g[2] != 3 {
		t.Errorf("mixed statuses listed %v, want [1 2 3]", g)
	}
	got, err = repo.ListTasks(ctx, entity.ListTasksRequest{WorkspaceID: crudWS, CreatedBy: "agent"}, crudUser)
	if err != nil || len(got) != 1 || got[0].ID != 2 {
		t.Errorf("ListTasks(createdBy agent) = %v, %v; want [2]", ids(got), err)
	}

	n, err := repo.CountTasks(ctx, entity.ListTasksRequest{WorkspaceID: crudWS, CreatedBy: "human", Status: []string{"ongoing", "notstarted"}}, crudUser)
	if err != nil || n != 2 {
		t.Errorf("CountTasks = %d, %v; want 2", n, err)
	}

	failAll(t, db)
	if _, err := repo.ListTasks(ctx, entity.ListTasksRequest{}, crudUser); !errors.Is(err, errInjected) {
		t.Errorf("ListTasks on a failing database = %v", err)
	}
	if _, err := repo.CountTasks(ctx, entity.ListTasksRequest{}, crudUser); !errors.Is(err, errInjected) {
		t.Errorf("CountTasks on a failing database = %v", err)
	}
}

// The pending-approval filter keeps the tasks whose newest message is a
// permission request, oldest first.
func TestRepository_ListTasks_PendingApproval(t *testing.T) {
	ctx := context.Background()
	db, repo := crudDB(t)
	now := time.Now()
	request := []byte(`{"type":"permission_request","status":"pending"}`)
	mustCreate(t, db,
		&model.Task{ID: 1, WorkspaceID: crudWS, UserID: crudUser, Status: "ongoing", CreatedAt: now},
		&model.Task{ID: 2, WorkspaceID: crudWS, UserID: crudUser, Status: "ongoing", CreatedAt: now.Add(-time.Hour)},
		&model.Task{ID: 3, WorkspaceID: crudWS, UserID: crudUser, Status: "ongoing", CreatedAt: now},
		&model.Message{ID: 11, TaskID: 1, UserID: crudUser, CreatedAt: now, Metadata: request},
		&model.Message{ID: 12, TaskID: 2, UserID: crudUser, CreatedAt: now, Metadata: request},
		&model.Message{ID: 13, TaskID: 3, UserID: crudUser, CreatedAt: now.Add(-time.Minute), Metadata: request},
		&model.Message{ID: 14, TaskID: 3, UserID: crudUser, CreatedAt: now, Metadata: []byte(`{}`)},
	)
	got, err := repo.ListTasks(ctx, entity.ListTasksRequest{WorkspaceID: crudWS, Filter: "pending_approval", Limit: 501}, crudUser)
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(got) != 2 || got[0].ID != 2 || got[1].ID != 1 {
		t.Errorf("pending approval listed %+v, want tasks 2 then 1", got)
	}
}

// The Postgres-only SQL is chosen by the dialect's name. On SQLite it cannot
// run, which shows each branch was taken and that its failure is returned.
func TestRepository_PostgresBranches(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		call func(r Repository) error
	}{
		{"list pending approval", func(r Repository) error {
			_, err := r.ListTasks(ctx, entity.ListTasksRequest{Filter: "pending_approval"}, crudUser)
			return err
		}},
		{"list notstarted in order", func(r Repository) error {
			_, err := r.ListTasks(ctx, entity.ListTasksRequest{Status: []string{"notstarted"}}, crudUser)
			return err
		}},
		{"preload pending requests", func(r Repository) error {
			_, err := r.ListTasks(ctx, entity.ListTasksRequest{Status: []string{"ongoing"}, PreloadMessages: true}, crudUser)
			return err
		}},
		{"next task", func(r Repository) error {
			_, err := r.GetNextTask(ctx, crudWS, crudUser)
			return err
		}},
		{"workspace stats", func(r Repository) error {
			_, err := r.GetDetailedWorkspaceStats(ctx, crudWS, 0, 10)
			return err
		}},
		{"task counts by category", func(r Repository) error {
			_, err := r.GetWorkspaceTaskCountsByCategory(ctx, crudWS, crudUser)
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, repo := postgresNamed(t)
			now := time.Now()
			mustCreate(t, db,
				&model.Task{ID: 1, WorkspaceID: crudWS, UserID: crudUser, Status: "ongoing", Assignee: "agent", CreatedAt: now},
				&model.Task{ID: 2, WorkspaceID: crudWS, UserID: crudUser, Status: "notstarted", Assignee: "agent", CreatedAt: now},
				&model.Message{ID: 11, TaskID: 1, UserID: crudUser, CreatedAt: now},
				&model.Telemetry{UserID: crudUser, WorkspaceID: crudWS, OccurredAt: 5, Action: model.ActionIDTaskComplete},
			)
			err := tc.call(repo)
			if err == nil || errors.Is(err, ErrNotFound) {
				t.Errorf("err = %v, want SQLite to refuse the Postgres SQL", err)
			}
		})
	}
}

func TestRepository_DeleteTask_TaskDeleteFails(t *testing.T) {
	db, repo := crudDB(t)
	mustCreate(t, db, &model.Task{ID: 1, WorkspaceID: crudWS, UserID: crudUser, Status: "ongoing"})
	failOn(t, db, "delete", "tasks")
	wantInjected(t, "DeleteTask", repo.DeleteTask(context.Background(), crudWS, 1, crudUser))
}

func TestRepository_SystemGetters(t *testing.T) {
	ctx := context.Background()
	db, repo := crudDB(t)
	mustCreate(t, db,
		&model.Task{ID: 1, WorkspaceID: crudWS, UserID: crudUser, Status: "cron", ParentID: 0},
		&model.Task{ID: 2, WorkspaceID: crudWS, UserID: crudUser, Status: "notstarted", ParentID: 1},
		&model.Message{ID: 11, TaskID: 1, UserID: crudUser, Text: "hi"},
		&model.User{ID: crudUser, Email: "a@example.com", Name: "A"},
	)

	if task, err := repo.SystemGetTask(ctx, 2); err != nil || task.ParentID != 1 {
		t.Errorf("SystemGetTask = %+v, %v", task, err)
	}
	if _, err := repo.SystemGetTask(ctx, 99); !errors.Is(err, ErrNotFound) {
		t.Errorf("SystemGetTask(missing) = %v, want ErrNotFound", err)
	}
	if m, err := repo.SystemGetMessage(ctx, 11); err != nil || m.Text != "hi" {
		t.Errorf("SystemGetMessage = %+v, %v", m, err)
	}
	if _, err := repo.SystemGetMessage(ctx, 99); !errors.Is(err, ErrNotFound) {
		t.Errorf("SystemGetMessage(missing) = %v, want ErrNotFound", err)
	}
	if u, err := repo.SystemGetUser(ctx, crudUser); err != nil || u.Email != "a@example.com" {
		t.Errorf("SystemGetUser = %+v, %v", u, err)
	}
	if _, err := repo.SystemGetUser(ctx, 99); !errors.Is(err, ErrNotFound) {
		t.Errorf("SystemGetUser(missing) = %v, want ErrNotFound", err)
	}
	if ts, err := repo.SystemListTasksByStatus(ctx, "cron"); err != nil || len(ts) != 1 || ts[0].ID != 1 {
		t.Errorf("SystemListTasksByStatus = %+v, %v", ts, err)
	}
	if ok, err := repo.SystemCheckTaskExists(ctx, crudWS, 1, "notstarted"); err != nil || !ok {
		t.Errorf("SystemCheckTaskExists(run) = %v, %v; want true", ok, err)
	}
	if ok, err := repo.SystemCheckTaskExists(ctx, crudWS, 1, "completed"); err != nil || ok {
		t.Errorf("SystemCheckTaskExists(no run) = %v, %v; want false", ok, err)
	}
	if active, total, err := repo.GetWorkspaceTaskCounts(ctx, crudWS); err != nil || active != 2 || total != 2 {
		t.Errorf("GetWorkspaceTaskCounts = %d, %d, %v; want 2, 2", active, total, err)
	}
	if stats, err := repo.GetGlobalTaskStats(ctx, crudUser); err != nil || stats.PendingTasks != 1 || stats.ScheduledTasks != 1 {
		t.Errorf("GetGlobalTaskStats = %+v, %v; want 1 pending, 1 scheduled", stats, err)
	}
}

func TestRepository_TelemetryCounts(t *testing.T) {
	ctx := context.Background()
	db, repo := crudDB(t)
	for _, a := range []uint8{
		model.ActionIDTaskComplete, model.ActionIDTaskFromScheduled,
		model.ActionIDTaskApproveManual, model.ActionIDMCPPermissionManual,
		model.ActionIDMCPPermissionAuto, model.ActionIDTaskRejectManual,
		model.ActionIDMCPPermissionDeny, model.ActionIDMessageCreate, model.ActionIDMessageCreate,
	} {
		mustCreate(t, db, &model.Telemetry{UserID: crudUser, WorkspaceID: crudWS, OccurredAt: 100, Action: a})
	}

	stats, err := repo.GetDetailedWorkspaceStats(ctx, crudWS, 0, 1000)
	if err != nil {
		t.Fatalf("GetDetailedWorkspaceStats: %v", err)
	}
	s := stats.Summary
	if s.TasksCompleted != 1 || s.TasksScheduled != 1 || s.Messages != 2 || s.ManualApprovals != 2 || s.AutoApprovals != 1 || s.Denies != 2 {
		t.Errorf("summary = %+v", s)
	}

	counts, err := repo.GetTelemetryActionCounts(ctx)
	if err != nil || counts[model.ActionIDMessageCreate] != 2 || counts[model.ActionIDTaskComplete] != 1 {
		t.Errorf("GetTelemetryActionCounts = %v, %v", counts, err)
	}
}

// Each of the five statistics queries reports its own failure.
func TestRepository_DetailedStats_EachQueryFails(t *testing.T) {
	for n := 1; n <= 5; n++ {
		db, repo := crudDB(t)
		failNth(db, n)
		_, err := repo.GetDetailedWorkspaceStats(context.Background(), crudWS, 0, 1000)
		wantInjected(t, "query "+string(rune('0'+n)), err)
	}
}

func TestRepository_CountQueries_SecondStatementFails(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		call func(r Repository) error
	}{
		{"workspace task counts", func(r Repository) error {
			_, _, err := r.GetWorkspaceTaskCounts(ctx, crudWS)
			return err
		}},
		{"global task stats", func(r Repository) error {
			_, err := r.GetGlobalTaskStats(ctx, crudUser)
			return err
		}},
		{"task counts by category", func(r Repository) error {
			_, err := r.GetWorkspaceTaskCountsByCategory(ctx, crudWS, crudUser)
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for n := 1; n <= 2; n++ {
				db, repo := crudDB(t)
				failNth(db, n)
				wantInjected(t, tc.name, tc.call(repo))
			}
		})
	}
}

func TestRepository_Users(t *testing.T) {
	ctx := context.Background()
	db, repo := crudDB(t)
	u, err := repo.CreateUser(ctx, model.User{ID: crudUser, Email: "a@example.com", Name: "A"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	u.Name = "Renamed"
	if _, err := repo.UpdateUser(ctx, u); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	if got, err := repo.FindUserByEmail(ctx, "a@example.com"); err != nil || got.Name != "Renamed" {
		t.Errorf("FindUserByEmail = %+v, %v", got, err)
	}
	if _, err := repo.FindUserByEmail(ctx, "nobody@example.com"); !errors.Is(err, ErrNotFound) {
		t.Errorf("FindUserByEmail(missing) = %v, want ErrNotFound", err)
	}
	if _, err := repo.CreateUser(ctx, model.User{ID: crudOther, Email: "a@example.com"}); err == nil {
		t.Error("CreateUser accepted a taken email")
	}
	failAll(t, db)
	_, err = repo.UpdateUser(ctx, u)
	wantInjected(t, "UpdateUser", err)
	_, err = repo.FindUserByEmail(ctx, "a@example.com")
	wantInjected(t, "FindUserByEmail", err)
}

func TestRepository_Slack(t *testing.T) {
	ctx := context.Background()
	db, repo := crudDB(t)
	if err := repo.UpsertSlackWorkspaceLink(ctx, model.SlackWorkspaceLink{WorkspaceID: crudWS, SlackChannelID: "C1"}); err != nil {
		t.Fatalf("UpsertSlackWorkspaceLink: %v", err)
	}
	if l, err := repo.GetSlackWorkspaceLink(ctx, crudWS); err != nil || l.SlackChannelID != "C1" {
		t.Errorf("GetSlackWorkspaceLink = %+v, %v", l, err)
	}
	if l, err := repo.GetSlackWorkspaceLinkByChannel(ctx, "C1"); err != nil || l.WorkspaceID != crudWS {
		t.Errorf("GetSlackWorkspaceLinkByChannel = %+v, %v", l, err)
	}
	if err := repo.DeleteSlackWorkspaceLink(ctx, crudWS); err != nil {
		t.Fatalf("DeleteSlackWorkspaceLink: %v", err)
	}
	if _, err := repo.GetSlackWorkspaceLink(ctx, crudWS); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetSlackWorkspaceLink after delete = %v, want ErrNotFound", err)
	}
	if _, err := repo.GetSlackWorkspaceLinkByChannel(ctx, "C1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetSlackWorkspaceLinkByChannel after delete = %v, want ErrNotFound", err)
	}

	if err := repo.UpsertSlackTaskThread(ctx, model.SlackTaskThread{TaskID: 1, WorkspaceID: crudWS, SlackChannelID: "C1", ThreadTS: "1.2"}); err != nil {
		t.Fatalf("UpsertSlackTaskThread: %v", err)
	}
	if th, err := repo.GetSlackTaskThreadByTask(ctx, 1); err != nil || th.ThreadTS != "1.2" {
		t.Errorf("GetSlackTaskThreadByTask = %+v, %v", th, err)
	}
	if th, err := repo.GetSlackTaskThreadByChannel(ctx, "C1", "1.2"); err != nil || th.TaskID != 1 {
		t.Errorf("GetSlackTaskThreadByChannel = %+v, %v", th, err)
	}
	if _, err := repo.GetSlackTaskThreadByTask(ctx, 2); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetSlackTaskThreadByTask(missing) = %v, want ErrNotFound", err)
	}
	if _, err := repo.GetSlackTaskThreadByChannel(ctx, "C1", "9.9"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetSlackTaskThreadByChannel(missing) = %v, want ErrNotFound", err)
	}

	failAll(t, db)
	_, err := repo.GetSlackWorkspaceLink(ctx, crudWS)
	wantInjected(t, "GetSlackWorkspaceLink", err)
	_, err = repo.GetSlackWorkspaceLinkByChannel(ctx, "C1")
	wantInjected(t, "GetSlackWorkspaceLinkByChannel", err)
	_, err = repo.GetSlackTaskThreadByTask(ctx, 1)
	wantInjected(t, "GetSlackTaskThreadByTask", err)
	_, err = repo.GetSlackTaskThreadByChannel(ctx, "C1", "1.2")
	wantInjected(t, "GetSlackTaskThreadByChannel", err)
}

func TestRepository_PushSubscriptions(t *testing.T) {
	ctx := context.Background()
	_, repo := crudDB(t)
	sub := model.PushSubscription{ID: 1, UserID: crudUser, WorkspaceID: crudWS, Endpoint: "https://push.example/1", Auth: "a"}
	if err := repo.SavePushSubscription(ctx, sub); err != nil {
		t.Fatalf("SavePushSubscription: %v", err)
	}
	// Saving the same endpoint again updates it rather than adding a row.
	sub.Auth = "b"
	if err := repo.SavePushSubscription(ctx, sub); err != nil {
		t.Fatalf("SavePushSubscription again: %v", err)
	}
	if err := repo.SavePushSubscription(ctx, model.PushSubscription{ID: 2, UserID: crudUser, WorkspaceID: crudWS + 1, Endpoint: "https://push.example/1"}); err != nil {
		t.Fatalf("SavePushSubscription for another workspace: %v", err)
	}
	subs, err := repo.ListPushSubscriptionsByUserAndWorkspace(ctx, crudUser, crudWS)
	if err != nil || len(subs) != 1 || subs[0].Auth != "b" {
		t.Errorf("ListPushSubscriptionsByUserAndWorkspace = %+v, %v; want one, updated", subs, err)
	}
	if ok, err := repo.GetPushSubscriptionForWorkspace(ctx, crudUser, crudWS, sub.Endpoint); err != nil || !ok {
		t.Errorf("GetPushSubscriptionForWorkspace = %v, %v; want true", ok, err)
	}
	if err := repo.DeletePushSubscriptionByWorkspace(ctx, crudUser, crudWS, sub.Endpoint); err != nil {
		t.Fatalf("DeletePushSubscriptionByWorkspace: %v", err)
	}
	if ok, _ := repo.GetPushSubscriptionForWorkspace(ctx, crudUser, crudWS, sub.Endpoint); ok {
		t.Error("the subscription is still there after DeletePushSubscriptionByWorkspace")
	}
	if ok, _ := repo.GetPushSubscriptionForWorkspace(ctx, crudUser, crudWS+1, sub.Endpoint); !ok {
		t.Error("DeletePushSubscriptionByWorkspace removed another workspace's subscription")
	}
	if err := repo.DeletePushSubscription(ctx, crudUser, sub.Endpoint); err != nil {
		t.Fatalf("DeletePushSubscription: %v", err)
	}
	if ok, _ := repo.GetPushSubscriptionForWorkspace(ctx, crudUser, crudWS+1, sub.Endpoint); ok {
		t.Error("DeletePushSubscription left the endpoint subscribed")
	}
}

func TestRepository_DeleteMemory_Failures(t *testing.T) {
	ctx := context.Background()
	db, repo := crudDB(t)
	if err := repo.DeleteMemory(ctx, crudUser, crudWS, "MEMORY.md"); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteMemory(missing) = %v, want ErrNotFound", err)
	}
	failAll(t, db)
	wantInjected(t, "DeleteMemory", repo.DeleteMemory(ctx, crudUser, crudWS, "MEMORY.md"))
}

func TestRepository_Events(t *testing.T) {
	ctx := context.Background()
	_, repo := crudDB(t)
	if _, err := repo.CreateEvent(ctx, model.Event{ID: 1, UserID: crudUser, Name: "deploy"}); err != nil {
		t.Fatalf("CreateEvent: %v", err)
	}
	if _, err := repo.CreateEvent(ctx, model.Event{ID: 2, UserID: crudUser, Name: "deploy"}); err == nil {
		t.Error("CreateEvent accepted a second event of the same name")
	}
	if _, err := repo.GetEvent(ctx, 1, crudOther); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetEvent(another user's) = %v, want ErrNotFound", err)
	}
	if _, err := repo.GetEventByName(ctx, "deploy", crudOther); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetEventByName(another user's) = %v, want ErrNotFound", err)
	}
	if _, err := repo.UpdateEvent(ctx, 1, crudOther, "x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateEvent(another user's) = %v, want ErrNotFound", err)
	}
	e, err := repo.UpdateEvent(ctx, 1, crudUser, "send the version")
	if err != nil || e.PayloadGuidelines != "send the version" {
		t.Errorf("UpdateEvent = %+v, %v", e, err)
	}
	if err := repo.DeleteEvent(ctx, 1, crudOther); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteEvent(another user's) = %v, want ErrNotFound", err)
	}
}

func TestRepository_Events_StatementFailures(t *testing.T) {
	ctx := context.Background()
	seed := func(t *testing.T) (*gorm.DB, Repository) {
		db, repo := crudDB(t)
		mustCreate(t, db,
			&model.Event{ID: 1, UserID: crudUser, Name: "deploy"},
			&model.EventTrigger{ID: 2, UserID: crudUser, EventID: 1, WorkspaceID: crudWS},
			&model.Workflow{ID: 3, UserID: crudUser, Name: "wf", StartEventID: 1},
			&model.WorkflowStep{ID: 4, UserID: crudUser, WorkflowID: 3, EventID: 1, WorkspaceID: crudWS},
		)
		return db, repo
	}
	for _, tc := range []struct {
		name        string
		kind, table string
		call        func(r Repository) error
	}{
		{"create event", "create", "events", func(r Repository) error {
			_, err := r.CreateEvent(ctx, model.Event{ID: 9, UserID: crudUser, Name: "other"})
			return err
		}},
		{"get event", "query", "events", func(r Repository) error {
			_, err := r.GetEvent(ctx, 1, crudUser)
			return err
		}},
		{"get event by name", "query", "events", func(r Repository) error {
			_, err := r.GetEventByName(ctx, "deploy", crudUser)
			return err
		}},
		{"update event: read", "query", "events", func(r Repository) error {
			_, err := r.UpdateEvent(ctx, 1, crudUser, "x")
			return err
		}},
		{"update event: write", "update", "events", func(r Repository) error {
			_, err := r.UpdateEvent(ctx, 1, crudUser, "x")
			return err
		}},
		{"delete event", "delete", "events", func(r Repository) error { return r.DeleteEvent(ctx, 1, crudUser) }},
		{"delete event: triggers", "delete", "event_triggers", func(r Repository) error { return r.DeleteEvent(ctx, 1, crudUser) }},
		{"delete event: trigger emits", "update", "event_triggers", func(r Repository) error { return r.DeleteEvent(ctx, 1, crudUser) }},
		{"delete event: steps", "delete", "workflow_steps", func(r Repository) error { return r.DeleteEvent(ctx, 1, crudUser) }},
		{"delete event: step emits", "update", "workflow_steps", func(r Repository) error { return r.DeleteEvent(ctx, 1, crudUser) }},
		{"delete event: workflow start", "update", "workflows", func(r Repository) error { return r.DeleteEvent(ctx, 1, crudUser) }},
		{"create trigger", "create", "event_triggers", func(r Repository) error {
			_, err := r.CreateEventTrigger(ctx, model.EventTrigger{ID: 9, UserID: crudUser})
			return err
		}},
		{"update trigger: read", "query", "event_triggers", func(r Repository) error {
			_, err := r.UpdateEventTrigger(ctx, 2, crudUser, model.EventTrigger{})
			return err
		}},
		{"update trigger: write", "update", "event_triggers", func(r Repository) error {
			_, err := r.UpdateEventTrigger(ctx, 2, crudUser, model.EventTrigger{})
			return err
		}},
		{"delete trigger", "delete", "event_triggers", func(r Repository) error { return r.DeleteEventTrigger(ctx, 2, crudUser) }},
		{"create workflow", "create", "workflows", func(r Repository) error {
			_, err := r.CreateWorkflow(ctx, model.Workflow{ID: 9, UserID: crudUser, Name: "other"})
			return err
		}},
		{"get workflow", "query", "workflows", func(r Repository) error {
			_, err := r.GetWorkflow(ctx, 3, crudUser)
			return err
		}},
		{"list workflows", "query", "workflows", func(r Repository) error {
			_, err := r.ListWorkflowsByUser(ctx, crudUser)
			return err
		}},
		{"update workflow: read", "query", "workflows", func(r Repository) error {
			_, err := r.UpdateWorkflow(ctx, model.Workflow{ID: 3, UserID: crudUser})
			return err
		}},
		{"update workflow: write", "update", "workflows", func(r Repository) error {
			_, err := r.UpdateWorkflow(ctx, model.Workflow{ID: 3, UserID: crudUser})
			return err
		}},
		{"delete workflow", "delete", "workflows", func(r Repository) error { return r.DeleteWorkflow(ctx, 3, crudUser) }},
		{"create step", "create", "workflow_steps", func(r Repository) error {
			_, err := r.CreateWorkflowStep(ctx, model.WorkflowStep{ID: 9, UserID: crudUser})
			return err
		}},
		{"list steps", "query", "workflow_steps", func(r Repository) error {
			_, err := r.ListWorkflowStepsByWorkflow(ctx, 3, crudUser)
			return err
		}},
		{"delete step", "delete", "workflow_steps", func(r Repository) error { return r.DeleteWorkflowStep(ctx, 4, crudUser) }},
		{"list steps by event", "query", "workflow_steps", func(r Repository) error {
			_, err := r.SystemListWorkflowStepsByEvent(ctx, 3, 1)
			return err
		}},
		{"list workflow tasks", "query", "tasks", func(r Repository) error {
			_, err := r.ListTasksByWorkflowID(ctx, 3, crudUser)
			return err
		}},
		{"replace steps: delete", "delete", "workflow_steps", func(r Repository) error {
			return r.ReplaceWorkflowSteps(ctx, 3, crudUser, nil)
		}},
		{"replace steps: create", "create", "workflow_steps", func(r Repository) error {
			return r.ReplaceWorkflowSteps(ctx, 3, crudUser, []model.WorkflowStep{{ID: 9, UserID: crudUser, WorkflowID: 3}})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, repo := seed(t)
			failOn(t, db, tc.kind, tc.table)
			wantInjected(t, tc.name, tc.call(repo))
		})
	}
}

func TestRepository_EventTriggers(t *testing.T) {
	ctx := context.Background()
	_, repo := crudDB(t)
	if _, err := repo.CreateEventTrigger(ctx, model.EventTrigger{ID: 2, UserID: crudUser, EventID: 1, Title: "old"}); err != nil {
		t.Fatalf("CreateEventTrigger: %v", err)
	}
	if _, err := repo.UpdateEventTrigger(ctx, 2, crudOther, model.EventTrigger{}); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateEventTrigger(another user's) = %v, want ErrNotFound", err)
	}
	got, err := repo.UpdateEventTrigger(ctx, 2, crudUser, model.EventTrigger{
		WorkspaceID: crudWS, Title: "new", Body: "b", Assignee: "agent", CronSchedule: "0 * * * *", AllowAllCommands: true, EmitEventID: 7,
	})
	if err != nil {
		t.Fatalf("UpdateEventTrigger: %v", err)
	}
	if got.EventID != 1 || got.WorkspaceID != crudWS || got.Title != "new" || got.Body != "b" || got.Assignee != "agent" ||
		got.CronSchedule != "0 * * * *" || !got.AllowAllCommands || got.EmitEventID != 7 {
		t.Errorf("updated trigger = %+v", got)
	}
	stored, err := repo.GetEventTrigger(ctx, 2, crudUser)
	if err != nil || stored.Title != "new" {
		t.Errorf("GetEventTrigger after update = %+v, %v", stored, err)
	}
}

func TestRepository_Workflows(t *testing.T) {
	ctx := context.Background()
	db, repo := crudDB(t)
	now := time.Now()
	if _, err := repo.CreateWorkflow(ctx, model.Workflow{ID: 3, UserID: crudUser, Name: "release", CreatedAt: now}); err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	if _, err := repo.CreateWorkflow(ctx, model.Workflow{ID: 4, UserID: crudUser, Name: "release"}); err == nil {
		t.Error("CreateWorkflow accepted a second workflow of the same name")
	}
	if _, err := repo.CreateWorkflow(ctx, model.Workflow{ID: 5, UserID: crudUser, Name: "hotfix", CreatedAt: now.Add(time.Minute)}); err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	if _, err := repo.GetWorkflow(ctx, 3, crudOther); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetWorkflow(another user's) = %v, want ErrNotFound", err)
	}
	list, err := repo.ListWorkflowsByUser(ctx, crudUser)
	if err != nil || len(list) != 2 || list[0].ID != 5 {
		t.Errorf("ListWorkflowsByUser = %+v, %v; want newest first", list, err)
	}

	if _, err := repo.UpdateWorkflow(ctx, model.Workflow{ID: 3, UserID: crudOther}); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateWorkflow(another user's) = %v, want ErrNotFound", err)
	}
	updated, err := repo.UpdateWorkflow(ctx, model.Workflow{ID: 3, UserID: crudUser, Name: "ship", Description: "d", StartEventID: 1, Layout: []byte(`{}`)})
	if err != nil {
		t.Fatalf("UpdateWorkflow: %v", err)
	}
	if updated.Name != "ship" || updated.Description != "d" || updated.StartEventID != 1 || string(updated.Layout) != "{}" || !updated.CreatedAt.Equal(list[1].CreatedAt) {
		t.Errorf("updated workflow = %+v", updated)
	}
	if got, err := repo.GetWorkflow(ctx, 3, crudUser); err != nil || got.Name != "ship" {
		t.Errorf("GetWorkflow after update = %+v, %v", got, err)
	}

	// Steps.
	for i, id := range []int64{10, 11} {
		if _, err := repo.CreateWorkflowStep(ctx, model.WorkflowStep{ID: id, UserID: crudUser, WorkflowID: 3, EventID: 1, CreatedAt: now.Add(time.Duration(i) * time.Second)}); err != nil {
			t.Fatalf("CreateWorkflowStep: %v", err)
		}
	}
	steps, err := repo.ListWorkflowStepsByWorkflow(ctx, 3, crudUser)
	if err != nil || len(steps) != 2 || steps[0].ID != 10 {
		t.Errorf("ListWorkflowStepsByWorkflow = %+v, %v; want oldest first", steps, err)
	}
	if byEvent, err := repo.SystemListWorkflowStepsByEvent(ctx, 3, 1); err != nil || len(byEvent) != 2 {
		t.Errorf("SystemListWorkflowStepsByEvent = %+v, %v", byEvent, err)
	}
	if err := repo.DeleteWorkflowStep(ctx, 10, crudOther); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteWorkflowStep(another user's) = %v, want ErrNotFound", err)
	}
	if err := repo.DeleteWorkflowStep(ctx, 10, crudUser); err != nil {
		t.Errorf("DeleteWorkflowStep: %v", err)
	}
	if err := repo.ReplaceWorkflowSteps(ctx, 3, crudUser, []model.WorkflowStep{{ID: 12, UserID: crudUser, WorkflowID: 3}, {ID: 13, UserID: crudUser, WorkflowID: 3}}); err != nil {
		t.Fatalf("ReplaceWorkflowSteps: %v", err)
	}
	steps, _ = repo.ListWorkflowStepsByWorkflow(ctx, 3, crudUser)
	if len(steps) != 2 || steps[0].ID+steps[1].ID != 25 {
		t.Errorf("steps after replace = %+v, want 12 and 13", steps)
	}
	if err := repo.ReplaceWorkflowSteps(ctx, 3, crudUser, nil); err != nil {
		t.Fatalf("ReplaceWorkflowSteps(empty): %v", err)
	}
	if steps, _ = repo.ListWorkflowStepsByWorkflow(ctx, 3, crudUser); len(steps) != 0 {
		t.Errorf("steps after an empty replace = %+v, want none", steps)
	}

	mustCreate(t, db,
		&model.Task{ID: 20, UserID: crudUser, WorkflowID: 3, CreatedAt: now},
		&model.Task{ID: 21, UserID: crudUser, WorkflowID: 3, CreatedAt: now.Add(time.Second)},
		&model.WorkflowStep{ID: 14, UserID: crudUser, WorkflowID: 3},
	)
	if tasks, err := repo.ListTasksByWorkflowID(ctx, 3, crudUser); err != nil || len(tasks) != 2 || tasks[0].ID != 21 {
		t.Errorf("ListTasksByWorkflowID = %+v, %v; want newest first", tasks, err)
	}

	if err := repo.DeleteWorkflow(ctx, 3, crudOther); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteWorkflow(another user's) = %v, want ErrNotFound", err)
	}
	if err := repo.DeleteWorkflow(ctx, 3, crudUser); err != nil {
		t.Fatalf("DeleteWorkflow: %v", err)
	}
	if steps, _ = repo.ListWorkflowStepsByWorkflow(ctx, 3, crudUser); len(steps) != 0 {
		t.Errorf("DeleteWorkflow left steps %+v", steps)
	}
	// Tasks of past runs keep their workflow.
	if tasks, _ := repo.ListTasksByWorkflowID(ctx, 3, crudUser); len(tasks) != 2 {
		t.Errorf("DeleteWorkflow changed the tasks of past runs: %+v", tasks)
	}
}
