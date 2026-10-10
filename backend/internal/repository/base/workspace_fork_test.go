// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package base

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/service/tasklatency"
	"github.com/glebarez/sqlite"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

const (
	wfUser   = int64(482467435371298817)
	wfParent = int64(700000000000000001)
	wfFork   = int64(700000000000000002)
	wfOther  = int64(700000000000000003)
)

// forkDB is a database with every table a workspace delete touches, foreign
// keys enforced, and a parent with one fork.
func forkDB(t *testing.T) (*gorm.DB, Repository) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?_pragma=foreign_keys(1)"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&model.Workspace{}, &model.Task{}, &model.Message{}, &model.ToolCall{},
		&model.SlackTaskThread{}, &model.EventTrigger{}, &model.WorkflowStep{}, &model.TaskStateTransition{},
		&model.TaskLatency{}, &model.Skill{}, &model.SkillFile{}, &model.WorkspaceSkill{}, &model.SiteShare{}, &model.ForkFolder{},
		&model.Telemetry{}, &model.HourlyTelemetry{}, &model.DailyTelemetry{}, &model.MonthlyTelemetry{},
		&model.HourlyTaskLatency{}, &model.DailyTaskLatency{}, &model.MonthlyTaskLatency{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	now := time.Now()
	for _, w := range []model.Workspace{
		{ID: wfParent, CreatedAt: now, UserID: wfUser, Name: "parent"},
		{ID: wfFork, CreatedAt: now.Add(time.Second), UserID: wfUser, Name: "parent fork", ForkOfID: wfParent},
	} {
		if err := db.Create(&w).Error; err != nil {
			t.Fatalf("seed workspace: %v", err)
		}
	}
	return db, New(&mockDB{db: db})
}

// seedForkTask writes a task in ws with a message, a tool call, a Slack
// thread, a state transition and a latency row.
func seedForkTask(t *testing.T, db *gorm.DB, ws, id int64, status string) {
	t.Helper()
	now := time.Now()
	rows := []any{
		&model.Task{ID: id, CreatedAt: now, UpdatedAt: now, WorkspaceID: ws, UserID: wfUser, Status: status, Title: status},
		&model.Message{ID: id + 1, CreatedAt: now, TaskID: id, UserID: wfUser, Sender: "human", Text: "hello"},
		&model.ToolCall{ID: id + 2, CreatedAt: now, TaskID: id, WorkspaceID: ws, ToolName: "Bash", Status: "allowed"},
		&model.SlackTaskThread{TaskID: id, WorkspaceID: ws, SlackChannelID: "C1", ThreadTS: "1.2"},
		&model.TaskStateTransition{UserID: wfUser, WorkspaceID: ws, TaskID: id, ToState: model.TaskStateFromStatus(status), CreatedAt: now},
		&model.TaskLatency{TaskID: id, UserID: wfUser, WorkspaceID: ws, ClosedAt: now.Unix()},
	}
	for _, r := range rows {
		if err := db.Create(r).Error; err != nil {
			t.Fatalf("seed %T: %v", r, err)
		}
	}
}

func countIn(t *testing.T, db *gorm.DB, m any, ws int64) int64 {
	t.Helper()
	var n int64
	if err := db.Model(m).Where("workspace_id = ?", ws).Count(&n).Error; err != nil {
		t.Fatalf("count %T: %v", m, err)
	}
	return n
}

var followTask = []any{&model.Task{}, &model.ToolCall{}, &model.SlackTaskThread{}, &model.TaskStateTransition{}, &model.TaskLatency{}}

func TestMergeForkIntoParent_MovesEverythingAndDeletesTheFork(t *testing.T) {
	db, repo := forkDB(t)
	seedForkTask(t, db, wfParent, 100, "ongoing") // the parent's own
	seedForkTask(t, db, wfFork, 200, "completed")
	seedForkTask(t, db, wfFork, 300, "rejected")
	seedForkTask(t, db, wfFork, 400, "cron")
	if err := db.Create(&model.SiteShare{ID: 9, UserID: wfUser, WorkspaceID: wfFork, Origin: "https://a.example"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := repo.RecordForkFolder(context.Background(), wfFork, 5, wfUser); err != nil {
		t.Fatal(err)
	}

	moved, err := repo.MergeForkIntoParent(context.Background(), wfFork, wfParent)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if len(moved) != 3 || moved[0] != 200 || moved[1] != 300 || moved[2] != 400 {
		t.Fatalf("the merge moved tasks %v, want [200 300 400]", moved)
	}
	for _, m := range followTask {
		if n := countIn(t, db, m, wfParent); n != 4 {
			t.Errorf("the parent has %d rows of %T, want 4", n, m)
		}
		if n := countIn(t, db, m, wfFork); n != 0 {
			t.Errorf("the fork still has %d rows of %T, want 0", n, m)
		}
	}
	var msgs int64
	db.Model(&model.Message{}).Count(&msgs)
	if msgs != 4 {
		t.Errorf("there are %d messages, want 4: a moved task keeps its thread", msgs)
	}
	var cron model.Task
	db.First(&cron, 400)
	if cron.WorkspaceID != wfParent || cron.Status != "cron" {
		t.Errorf("the cron template is %+v, want it in the parent and still a schedule", cron)
	}
	var parent model.Task
	db.First(&parent, 100)
	if parent.Status != "ongoing" || parent.Title != "ongoing" {
		t.Errorf("the parent's own task changed: %+v", parent)
	}
	var remaining int64
	db.Model(&model.Workspace{}).Where("id = ?", wfFork).Count(&remaining)
	if remaining != 0 {
		t.Error("the fork row survived its merge")
	}
	db.Model(&model.SiteShare{}).Where("workspace_id = ?", wfFork).Count(&remaining)
	if remaining != 0 {
		t.Error("the fork's site share survived its merge")
	}
	if n := countIn(t, db, &model.ForkFolder{}, wfFork); n != 0 {
		t.Error("the record of the fork's folder survived its merge")
	}
}

func TestMergeForkIntoParent_RefusesAnUnfinishedTaskAndMovesNothing(t *testing.T) {
	for _, status := range []string{"ongoing", "notstarted", "blocked"} {
		t.Run(status, func(t *testing.T) {
			db, repo := forkDB(t)
			seedForkTask(t, db, wfFork, 200, "completed")
			seedForkTask(t, db, wfFork, 300, status)

			_, err := repo.MergeForkIntoParent(context.Background(), wfFork, wfParent)
			if !errors.Is(err, entity.ErrForkUnfinished) {
				t.Fatalf("the merge returned %v, want ErrForkUnfinished", err)
			}
			if err.Error() != "1 task in this fork is not finished" {
				t.Errorf("the error says %q, want \"1 task in this fork is not finished\"", err.Error())
			}
			for _, m := range followTask {
				if n := countIn(t, db, m, wfFork); n != 2 {
					t.Errorf("the fork has %d rows of %T, want 2: a refused merge moves nothing", n, m)
				}
			}
			var forks int64
			db.Model(&model.Workspace{}).Where("id = ?", wfFork).Count(&forks)
			if forks != 1 {
				t.Error("a refused merge deleted the fork")
			}
		})
	}
}

func TestMergeForkIntoParent_CountsSeveralUnfinished(t *testing.T) {
	db, repo := forkDB(t)
	seedForkTask(t, db, wfFork, 200, "ongoing")
	seedForkTask(t, db, wfFork, 300, "blocked")
	_, err := repo.MergeForkIntoParent(context.Background(), wfFork, wfParent)
	if err == nil || err.Error() != "2 tasks in this fork are not finished" {
		t.Fatalf("the merge returned %v, want \"2 tasks in this fork are not finished\"", err)
	}
}

func TestMergeForkIntoParent_EmptyForkIsJustDeleted(t *testing.T) {
	db, repo := forkDB(t)
	moved, err := repo.MergeForkIntoParent(context.Background(), wfFork, wfParent)
	if err != nil || len(moved) != 0 {
		t.Fatalf("the merge moved %v and returned %v, want nothing moved and no error", moved, err)
	}
	var remaining int64
	db.Model(&model.Workspace{}).Where("id = ?", wfFork).Count(&remaining)
	if remaining != 0 {
		t.Error("the empty fork survived its merge")
	}
}

// statsHour and statsLater are two hours of statistics: both workspaces have
// rows in the first, only the fork in the second.
const (
	statsHour  = int64(1790000000)
	statsLater = statsHour + 3600
)

// seedForkStats writes ws a telemetry event, and a telemetry row and a
// worked-time latency row in every rollup for each period, with n events and
// n tasks of seconds each.
func seedForkStats(t *testing.T, db *gorm.DB, ws, n, seconds int64, periods ...int64) {
	t.Helper()
	if err := db.Create(&model.Telemetry{UserID: wfUser, WorkspaceID: ws, OccurredAt: periods[0], Action: 1}).Error; err != nil {
		t.Fatalf("seed telemetry: %v", err)
	}
	var s tasklatency.Stats
	for range n {
		s.Add(seconds)
	}
	for _, p := range periods {
		for _, r := range []any{
			&model.HourlyTelemetry{PeriodStart: p, UserID: wfUser, WorkspaceID: ws, Action: 1, Count: n},
			&model.DailyTelemetry{PeriodStart: p, UserID: wfUser, WorkspaceID: ws, Action: 1, Count: n},
			&model.MonthlyTelemetry{PeriodStart: p, UserID: wfUser, WorkspaceID: ws, Action: 1, Count: n},
		} {
			if err := db.Create(r).Error; err != nil {
				t.Fatalf("seed %T: %v", r, err)
			}
		}
		for _, table := range []string{hourlyTaskLatencies, dailyTaskLatencies, monthlyTaskLatencies} {
			row := entity.TaskLatencyRollup{PeriodStart: p, UserID: wfUser, WorkspaceID: ws, Metric: uint8(model.LatencyMetricWorked),
				Count: s.Count, Sum: s.Sum, Min: s.Min, Max: s.Max, Histogram: tasklatency.EncodeHist(s.Hist)}
			if err := db.Table(table).Create(&row).Error; err != nil {
				t.Fatalf("seed %s: %v", table, err)
			}
		}
	}
}

// A merge hands the fork's statistics to the parent, so the stats page
// does not show them under a workspace that no longer exists. A period both
// have is added into the parent's row, since the workspace is part of the
// rollup's key.
func TestMergeForkIntoParent_GivesTheParentTheForksStatistics(t *testing.T) {
	db, repo := forkDB(t)
	seedForkStats(t, db, wfParent, 2, 30, statsHour)
	seedForkStats(t, db, wfFork, 3, 600, statsHour, statsLater)
	// Someone else's row in the same hour is left alone.
	seedForkStats(t, db, wfOther, 7, 60, statsHour)

	if _, err := repo.MergeForkIntoParent(context.Background(), wfFork, wfParent); err != nil {
		t.Fatalf("merge: %v", err)
	}

	if n := countIn(t, db, &model.Telemetry{}, wfParent); n != 2 {
		t.Errorf("the parent has %d telemetry events, want its own and the fork's", n)
	}
	for _, m := range []any{&model.Telemetry{}, &model.HourlyTelemetry{}, &model.DailyTelemetry{}, &model.MonthlyTelemetry{}} {
		if n := countIn(t, db, m, wfFork); n != 0 {
			t.Errorf("%d rows of %T still name the deleted fork", n, m)
		}
	}
	for _, table := range []string{"hourly_telemetries", "daily_telemetries", "monthly_telemetries"} {
		var rows []model.HourlyTelemetry
		db.Table(table).Where("workspace_id = ?", wfParent).Order("period_start").Find(&rows)
		if len(rows) != 2 || rows[0].Count != 5 || rows[1].Count != 3 {
			t.Errorf("the parent's %s are %+v, want 2+3 in the shared hour and the fork's 3 in the next", table, rows)
		}
		var other int64
		db.Table(table).Select("count").Where("workspace_id = ?", wfOther).Scan(&other)
		if other != 7 {
			t.Errorf("another workspace's %s count is %d, want 7", table, other)
		}
	}

	for _, table := range []string{hourlyTaskLatencies, dailyTaskLatencies, monthlyTaskLatencies} {
		var fork int64
		db.Table(table).Where("workspace_id = ?", wfFork).Count(&fork)
		if fork != 0 {
			t.Errorf("%d rows of %s still name the deleted fork", fork, table)
		}
		var rows []entity.TaskLatencyRollup
		db.Table(table).Where("workspace_id = ?", wfParent).Order("period_start").Scan(&rows)
		if len(rows) != 2 {
			t.Fatalf("the parent has %d rows of %s, want 2", len(rows), table)
		}
		shared := tasklatency.Decode(rows[0].Count, rows[0].Sum, rows[0].Min, rows[0].Max, rows[0].Histogram)
		if shared.Count != 5 || shared.Sum != 2*30+3*600 || shared.Min != 30 || shared.Max != 600 {
			t.Errorf("the parent's shared hour of %s is %+v, want both workspaces' tasks", table, shared)
		}
		if p50, _ := shared.Value("p50"); p50 < 600 {
			t.Errorf("the shared hour's p50 in %s is %d, want the fork's 600: its histogram was not merged", table, p50)
		}
		if rows[1].PeriodStart != statsLater || rows[1].Count != 3 || rows[1].Sum != 3*600 || rows[1].Metric != uint8(model.LatencyMetricWorked) {
			t.Errorf("the parent's later hour of %s is %+v, want the fork's row as it was", table, rows[1])
		}
		var other entity.TaskLatencyRollup
		db.Table(table).Where("workspace_id = ?", wfOther).Scan(&other)
		if other.Count != 7 || other.Sum != 7*60 {
			t.Errorf("another workspace's %s row is %+v, want it untouched", table, other)
		}
	}
}

// Only a fork of that parent merges into it: a workspace that is not a fork,
// or a fork of someone else, is not found.
func TestMergeForkIntoParent_NotAForkOfThatParent(t *testing.T) {
	_, repo := forkDB(t)
	if _, err := repo.MergeForkIntoParent(context.Background(), wfParent, wfFork); !errors.Is(err, ErrNotFound) {
		t.Fatalf("merging a workspace that is not a fork returned %v, want ErrNotFound", err)
	}
	if _, err := repo.MergeForkIntoParent(context.Background(), wfFork, wfOther); !errors.Is(err, ErrNotFound) {
		t.Fatalf("merging a fork into another workspace returned %v, want ErrNotFound", err)
	}
}

// A failure anywhere rolls the whole merge back.
func TestMergeForkIntoParent_ErrorsRollBack(t *testing.T) {
	for _, table := range []string{"workspaces", "tasks", "tool_calls", "site_shares"} {
		t.Run(table, func(t *testing.T) {
			db, repo := forkDB(t)
			seedForkTask(t, db, wfFork, 200, "completed")
			if err := db.Migrator().DropTable(table); err != nil {
				t.Fatal(err)
			}
			if _, err := repo.MergeForkIntoParent(context.Background(), wfFork, wfParent); err == nil {
				t.Fatalf("the merge with the %s table dropped returned no error", table)
			}
		})
	}
}

// Failing the Nth statement of the merge reaches every error return in turn.
func TestMergeForkIntoParent_EachStatementCanFail(t *testing.T) {
	errStatement := errors.New("statement failed")
	for n := 1; n <= 27; n++ {
		db, repo := forkDB(t)
		seedForkTask(t, db, wfFork, 200, "completed")
		seedForkStats(t, db, wfFork, 1, 60, statsHour)
		seen := 0
		failNth := func(tx *gorm.DB) {
			seen++
			if seen == n {
				_ = tx.AddError(errStatement)
			}
		}
		_ = db.Callback().Query().Before("gorm:query").Register("fail", failNth)
		_ = db.Callback().Update().Before("gorm:update").Register("fail", failNth)
		_ = db.Callback().Raw().Before("gorm:raw").Register("fail", failNth)
		_ = db.Callback().Row().Before("gorm:row").Register("fail", failNth)
		_ = db.Callback().Create().Before("gorm:create").Register("fail", failNth)
		_, err := repo.MergeForkIntoParent(context.Background(), wfFork, wfParent)
		if !errors.Is(err, errStatement) {
			t.Errorf("failing statement %d, the merge returned %v, want %v", n, err, errStatement)
		}
		if left := countIn(t, db, &model.Task{}, wfFork); left != 1 {
			t.Errorf("statement %d: the task moved anyway", n)
		}
	}
}

func TestUpdateWorkspace_WritesInheritedSettingsToForks(t *testing.T) {
	db, repo := forkDB(t)
	other := model.Workspace{ID: wfOther, UserID: wfUser, Name: "unrelated"}
	db.Create(&other)

	var parent model.Workspace
	db.First(&parent, wfParent)
	parent.Name = "renamed"
	parent.AllowAllCommands = true
	parent.ClearContextDefault = false
	parent.SelfLearningLoopNote = "note"
	parent.InputSendDelaySeconds = 5
	parent.AutoAllowedTools = datatypes.JSON(`["Bash"]`)
	parent.NotificationSettings = datatypes.JSON(`{"taskCreated":true}`)
	if _, err := repo.UpdateWorkspace(context.Background(), parent); err != nil {
		t.Fatal(err)
	}

	var fork model.Workspace
	db.First(&fork, wfFork)
	if fork.Name != "parent fork" {
		t.Errorf("the fork's name is %q, want \"parent fork\": a fork keeps its own name", fork.Name)
	}
	for k, v := range parent.ForkSettings() {
		if got := fork.ForkSettings()[k]; string(toBytes(got)) != string(toBytes(v)) {
			t.Errorf("the fork's %s is %v, want the parent's %v", k, got, v)
		}
	}
	db.First(&other, wfOther)
	if other.AllowAllCommands || other.SelfLearningLoopNote != "" {
		t.Errorf("an unrelated workspace inherited the settings: %+v", other)
	}

	// Saving a fork writes nothing to anyone else.
	fork.AllowAllCommands = false
	if _, err := repo.UpdateWorkspace(context.Background(), fork); err != nil {
		t.Fatal(err)
	}
	db.First(&parent, wfParent)
	if !parent.AllowAllCommands {
		t.Error("saving a fork changed its parent")
	}
}

func toBytes(v any) []byte {
	switch x := v.(type) {
	case datatypes.JSON:
		return x
	default:
		b, _ := json.Marshal(x)
		return b
	}
}

func TestUpdateWorkspace_ReturnsAFailedForksUpdateOrSave(t *testing.T) {
	db, repo := forkDB(t)
	var parent model.Workspace
	db.First(&parent, wfParent)
	db.Callback().Update().Before("gorm:update").Register("fail", func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Dest != nil {
			if _, ok := tx.Statement.Dest.(map[string]any); ok {
				_ = tx.AddError(errors.New("updating the forks failed"))
			}
		}
	})
	if _, err := repo.UpdateWorkspace(context.Background(), parent); err == nil || err.Error() != "updating the forks failed" {
		t.Fatalf("UpdateWorkspace returned %v, want the forks update's error", err)
	}
	if err := db.Migrator().DropTable(&model.Workspace{}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpdateWorkspace(context.Background(), parent); err == nil {
		t.Fatal("UpdateWorkspace with the workspaces table dropped returned no error")
	}
}

func TestListAndCountForks(t *testing.T) {
	db, repo := forkDB(t)
	db.Create(&model.Workspace{ID: wfOther, CreatedAt: time.Now().Add(time.Minute), UserID: wfUser, Name: "second", ForkOfID: wfParent})
	db.Create(&model.Workspace{ID: wfOther + 1, UserID: wfUser + 1, Name: "someone else's", ForkOfID: wfParent})

	forks, err := repo.ListForks(context.Background(), wfParent, wfUser)
	if err != nil || len(forks) != 2 || forks[0].ID != wfOther || forks[1].ID != wfFork {
		t.Fatalf("ListForks returned %+v and error %v, want the owner's 2 forks, newest first", forks, err)
	}
	count, err := repo.CountForks(context.Background(), wfParent, wfUser)
	if err != nil || count != 2 {
		t.Fatalf("CountForks returned %d and error %v, want 2 and no error", count, err)
	}
	count, _ = repo.CountForks(context.Background(), wfFork, wfUser)
	if count != 0 {
		t.Fatalf("a fork has %d forks, want 0", count)
	}
}

func TestCountUnfinishedTasks(t *testing.T) {
	db, repo := forkDB(t)
	seedForkTask(t, db, wfFork, 200, "ongoing")
	seedForkTask(t, db, wfFork, 300, "blocked")
	seedForkTask(t, db, wfFork, 400, "completed")
	seedForkTask(t, db, wfFork, 500, "cron")
	seedForkTask(t, db, wfParent, 600, "notstarted")

	got, err := repo.CountUnfinishedTasks(context.Background(), []int64{wfFork, wfParent, wfOther})
	if err != nil {
		t.Fatal(err)
	}
	if got[wfFork] != 2 || got[wfParent] != 1 || len(got) != 2 {
		t.Fatalf("the unfinished counts are %v, want 2 for the fork and 1 for the parent only", got)
	}
	got, err = repo.CountUnfinishedTasks(context.Background(), nil)
	if err != nil || len(got) != 0 {
		t.Fatalf("counting no workspaces returned %v and error %v, want nothing and no error", got, err)
	}
}

// The last two deletes of a workspace fail on their own, and roll it back.
func TestDeleteWorkspaceRows_LastDeletesCanFail(t *testing.T) {
	for _, table := range []string{"tasks", "workspaces"} {
		db, repo := forkDB(t)
		_ = db.Callback().Delete().Before("gorm:delete").Register("fail", func(tx *gorm.DB) {
			if tx.Statement.Table == table {
				_ = tx.AddError(errors.New("delete failed"))
			}
		})
		if err := repo.DeleteWorkspace(context.Background(), wfParent, wfUser); err == nil || err.Error() != "delete failed" {
			t.Errorf("with the %s delete failing, DeleteWorkspace returned %v, want \"delete failed\"", table, err)
		}
	}
	_, repo := forkDB(t)
	if err := repo.DeleteWorkspace(context.Background(), wfParent, wfUser+1); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleting someone else's workspace returned %v, want ErrNotFound", err)
	}
}
